// A call in a sealed chat between two Chromium browsers, against the real
// server and its real media port, with the real keys: each browser's
// sealed-chat session derives the call sender keys from the chat's epoch
// secret, and frames-worker.js encrypts and decrypts every frame in the
// format of docs/CALL-FRAMES.md.
//
// - Kaity and Sam set sealed chats up, Kaity starts a sealed chat and a
//   call in it, Sam joins: each decodes the other's video and audio, and
//   the worker's own counts say every frame was opened with the key of
//   the device the server named.
// - A new key epoch in the middle of the call (Kaity unlocks a second
//   browser, so her device list changes): both browsers re-key, send under
//   the new key index, and still decode once the old keys are gone.
// - A device without the keys (a second, locked browser of Sam's, made to
//   join with keys of its own invention) receives the packets and gets
//   nothing: no frame opens, no video frame is decoded, and it says so.
//   Kaity cannot decrypt it either and is told in words.
//
// The whole run uses one browser started with fake cameras and
// microphones (run.mjs starts it so for this mode).

import { step, shot, assert, person, until } from './lib.mjs';
import { seesVideo } from './calls-web.mjs';
import { box, msg, say, copyPasskey, signInWithPasskey, inPage, settles, openSealedChat } from './web-sealed.mjs';

const call = (page, fn, arg) => page.evaluate(fn, arg);

// What the page's call says about itself, and what its worker has done.
const cryptoState = (page) => page.evaluate(async () => {
  const c = window.churrCall.current;
  if (!c || !c.crypto) return null;
  const stats = await c.crypto.stats();
  const rtp = { videoDecoded: 0, audioEnergy: 0, bytes: 0 };
  if (c.recv) (await c.recv.getStats()).forEach((s) => {
    if (s.type !== 'inbound-rtp') return;
    rtp.bytes += s.bytesReceived || 0;
    if (s.kind === 'video') rtp.videoDecoded += s.framesDecoded || 0;
    if (s.kind === 'audio') rtp.audioEnergy += s.totalAudioEnergy || 0;
  });
  return { sealed: c.state.sealed, phase: c.phase, keyEpoch: Number(c.crypto.keyEpoch), keyIndex: c.crypto.keyIndex, device: c.crypto.deviceId,
    lines: [...c.mids.values()].map((m) => ({ account: m.account_id, device: m.device_id })), sent: stats.sent, opened: stats.opened, failed: stats.failed, rtp,
    notes: c.notes.map((n) => n.text) };
});
const sum = (o) => Object.values(o || {}).reduce((a, b) => a + b, 0);

async function mediaAllowed(who, origin) {
  await who.context.grantPermissions(['camera', 'microphone', 'notifications', 'clipboard-read', 'clipboard-write'], { origin });
  who.context.setDefaultTimeout(12000);
}

// Through the dialog shown before a call, with the camera on.
async function throughPrejoin(page, title, go) {
  const dlg = page.getByRole('dialog', { name: title });
  await dlg.waitFor();
  await dlg.getByRole('button', { name: 'Camera off' }).click();
  await dlg.locator('.call-preview video').waitFor();
  await dlg.getByRole('button', { name: go, exact: true }).click();
}

export async function sealedCallFlow(kaity, sam, browser, origin) {
  const a = kaity.page, b = sam.page;
  let convId = '', kaityPhrase = '', kaityDevice = '', samDevice = '', kaityId = '', samId = '';
  await mediaAllowed(kaity, origin);
  await mediaAllowed(sam, origin);

  await step('sealed call: both set sealed chats up, and Kaity starts a sealed chat with Sam', async () => {
    await b.goto(origin + '/account/sealed');
    await b.getByRole('button', { name: 'Set up sealed chats' }).click();
    await settles(b, b.getByRole('list', { name: 'Recovery phrase' }));
    await b.getByLabel('I have written these words down').check();
    await b.getByRole('dialog').getByRole('button', { name: 'Done' }).click();
    await b.getByText('This browser is unlocked').waitFor();

    await a.goto(origin + '/chat/');
    await a.getByRole('button', { name: 'New…' }).click();
    await a.getByRole('menuitem', { name: 'New sealed chat' }).click();
    await a.getByRole('dialog').getByPlaceholder('Search by name').fill('sam');
    await a.getByRole('dialog').getByRole('button', { name: /Sam Okafor/ }).click();
    await a.getByRole('button', { name: 'Set up sealed chats' }).click();
    await settles(a, a.getByRole('list', { name: 'Recovery phrase' }));
    const words = await a.getByRole('list', { name: 'Recovery phrase' }).getByRole('listitem').allInnerTexts();
    kaityPhrase = words.map((w) => w.replace(/^\d+\.\s*/, '').trim()).join(' ');
    await a.getByLabel('I have written these words down').check();
    await a.getByRole('dialog').getByRole('button', { name: 'Done' }).click();
    await a.waitForURL(/\/chat\/c\//);
    convId = a.url().split('/c/')[1].split('/')[0];
    await box(a).waitFor();
    await say(a, 'shall we talk?');
    await openSealedChat(b, origin, 'Kaity');
    await msg(b, 'shall we talk?').waitFor();
    kaityDevice = await inPage(a, (s) => s.device.id);
    samDevice = await inPage(b, (s) => s.device.id);
    kaityId = await inPage(a, (s) => s.me.id);
    samId = await inPage(b, (s) => s.me.id);
    assert(kaityDevice && samDevice && kaityDevice !== samDevice, 'each browser is a device of its own');
  }, a);

  await step('sealed call: Kaity starts a call in the sealed chat and Sam joins; the server names each one\'s device', async () => {
    await a.locator('.conn.live').waitFor();
    await b.locator('.conn.live').waitFor();
    await b.locator('#main .sealed-input').click();
    await a.getByRole('button', { name: 'Start a call' }).click();
    await throughPrejoin(a, 'Start a call', 'Start the call');
    await a.waitForFunction(() => window.churrCall.current && window.churrCall.current.phase === 'live' && window.churrCall.current.myTracks.camera);
    await b.getByRole('button', { name: /^Join the call/ }).first().click();
    await throughPrejoin(b, 'Join the call', 'Join');
    await b.waitForFunction(() => window.churrCall.current && window.churrCall.current.phase === 'live' && window.churrCall.current.myTracks.camera);
    const seen = await call(a, async (conv) => (await (await fetch('/api/v1/conversations/' + conv)).json()).conversation.call, convId);
    assert(seen && seen.sealed === true, 'the call is a sealed chat\'s: ' + JSON.stringify(seen));
    const devices = Object.fromEntries(seen.participants.map((p) => [p.account_id, p.device_id]));
    assert(devices[kaityId] === kaityDevice && devices[samId] === samDevice, 'the participants\' devices: ' + JSON.stringify(devices));
  }, a);

  await step('sealed call: each decodes the other\'s video and audio, every frame opened with the key of the device the server named', async () => {
    await seesVideo(a);
    await seesVideo(b);
    for (const [p, mine, theirs] of [[a, kaityDevice, samDevice], [b, samDevice, kaityDevice]]) {
      await until(p, 'video decoded, audio heard and frames opened', async (theirs) => {
        const c = window.churrCall.current;
        const s = await c.crypto.stats();
        let energy = 0, decoded = 0;
        (await c.recv.getStats()).forEach((x) => { if (x.type === 'inbound-rtp' && x.kind === 'audio') energy += x.totalAudioEnergy || 0; if (x.type === 'inbound-rtp' && x.kind === 'video') decoded += x.framesDecoded || 0; });
        return energy > 0 && decoded > 10 && !!s.opened[theirs] && Object.values(s.opened[theirs]).reduce((x, y) => x + y, 0) > 100;
      }, theirs);
      const st = await cryptoState(p);
      assert(st.sealed && st.keyEpoch === 1 && st.keyIndex === 1 && st.device === mine, 'the call\'s keys: ' + JSON.stringify(st));
      assert(st.lines.length >= 2 && st.lines.every((l) => l.device === theirs), 'every line is routed to the other device: ' + JSON.stringify(st.lines));
      assert(Object.keys(st.sent).join() === '1' && Object.keys(st.opened).join() === theirs && Object.keys(st.opened[theirs]).join() === '1', 'frames under key index 1 only: ' + JSON.stringify(st));
      // A few frames may arrive before the keys do, and are dropped.
      assert(st.failed < 20, 'frames that did not open: ' + st.failed);
      assert(!st.notes.some((n) => /Cannot decrypt/.test(n)), 'the page says it cannot decrypt');
    }
    const [sa, sb] = [await cryptoState(a), await cryptoState(b)];
    console.log(`     kaity sent ${sum(sa.sent)} opened ${sum(sa.opened[samDevice])} dropped ${sa.failed}; sam sent ${sum(sb.sent)} opened ${sum(sb.opened[kaityDevice])} dropped ${sb.failed}`);
    await shot(a, '90-sealed-call', { fullPage: false });
  }, a);

  const second = await person(browser, 'kaity-second', origin);
  await step('sealed call: a new key epoch in the middle of the call re-keys both browsers', async () => {
    await mediaAllowed(second, origin);
    await copyPasskey(kaity, second);
    await signInWithPasskey(second, origin);
    await openSealedChat(second.page, origin, 'Sam');
    const p = second.page;
    await p.getByRole('heading', { name: 'Unlock this browser for sealed chats' }).waitFor();
    await p.getByRole('button', { name: 'Use the recovery phrase' }).click();
    await p.getByRole('textbox', { name: 'Recovery phrase' }).fill(kaityPhrase);
    await p.getByRole('dialog').getByRole('button', { name: 'Unlock' }).click();
    await settles(p, msg(p, 'shall we talk?'));
    // Kaity's device list changed, so somebody made epoch 2, sealed to
    // three devices. Both browsers in the call take its keys.
    for (const q of [a, b]) {
      await q.waitForFunction(() => { const c = window.churrCall.current; return c && c.crypto && Number(c.crypto.keyEpoch) >= 2; }, null, { timeout: 30000, polling: 250 });
    }
    const [sa, sb] = [await cryptoState(a), await cryptoState(b)];
    assert(sa.keyEpoch === sb.keyEpoch && sa.keyIndex === sa.keyEpoch % 16 && sb.keyIndex === sa.keyIndex && sa.keyIndex !== 1, `both in the same new epoch: ${sa.keyEpoch}/${sa.keyIndex} and ${sb.keyEpoch}/${sb.keyIndex}`);
    assert(sa.phase === 'live' && sb.phase === 'live', 'the call went on');
    const recipients = await inPage(a, (s, lib, conv) => s.device.chats.get(conv).current.record.recipients.length, convId);
    assert(recipients === 3, 'the new epoch is sealed to three devices: ' + recipients);
    console.log(`     now in key epoch ${sa.keyEpoch}, key index ${sa.keyIndex}`);
  }, a);

  await step('sealed call: after the old keys are dropped, frames go out and open under the new key index only', async () => {
    // Ten seconds on, the receivers hold nothing of epoch 1.
    await a.waitForTimeout(11500);
    const index = String((await cryptoState(a)).keyIndex);
    const before = { a: await cryptoState(a), b: await cryptoState(b) };
    await a.waitForTimeout(3000);
    const after = { a: await cryptoState(a), b: await cryptoState(b) };
    for (const [who, mineDev, theirs] of [['a', kaityDevice, samDevice], ['b', samDevice, kaityDevice]]) {
      const x = before[who], y = after[who];
      assert((y.sent[index] || 0) - (x.sent[index] || 0) > 50, `${who} sends under index ${index}: ${JSON.stringify(x.sent)} then ${JSON.stringify(y.sent)}`);
      assert((y.sent['1'] || 0) === (x.sent['1'] || 0), `${who} still sends under the old index`);
      assert((y.opened[theirs][index] || 0) - (x.opened[theirs][index] || 0) > 50, `${who} opens frames under index ${index}: ${JSON.stringify(y.opened)}`);
      assert((y.opened[theirs]['1'] || 0) === (x.opened[theirs]['1'] || 0), `${who} still opens frames under the old index`);
      assert(y.failed - x.failed === 0, `${who} dropped ${y.failed - x.failed} frames after the change`);
      assert(y.rtp.videoDecoded > x.rtp.videoDecoded && y.rtp.audioEnergy > x.rtp.audioEnergy, `${who} still decodes video and audio`);
      assert(y.device === mineDev && !y.notes.some((n) => /Cannot decrypt/.test(n)), 'no complaint');
    }
    console.log(`     kaity dropped ${after.a.failed} frames in all, sam ${after.b.failed}`);
  }, a);

  const locked = await person(browser, 'sam-locked', origin);
  await step('sealed call: a device without the keys receives the packets and gets nothing usable', async () => {
    await mediaAllowed(locked, origin);
    await copyPasskey(sam, locked);
    await signInWithPasskey(locked, origin);
    const p = locked.page;
    await p.goto(origin + '/chat/c/' + convId);
    await p.getByRole('heading', { name: 'Unlock this browser for sealed chats' }).waitFor();
    await p.locator('.conn.live').waitFor();
    // As it is, this browser refuses to join: it has no keys.
    const refusal = await p.evaluate(async (conv) => {
      const { sealedRefusal } = await import('/chat/call/frame-keys.js');
      const { conversation } = await import('/chat/store.js');
      return sealedRefusal(conversation(conv).call);
    }, convId);
    assert(/not unlocked/.test(refusal || ''), 'a locked browser is refused: ' + refusal);
    // Made to join all the same, with keys of its own invention: one for
    // itself, and a guess at the key of Kaity's device under the index
    // the call is using.
    const index = (await cryptoState(a)).keyIndex;
    await p.evaluate(async ({ conv, kaityDevice, index }) => {
      const fk = await import('/chat/call/frame-keys.js');
      const invented = () => crypto.getRandomValues(new Uint8Array(32));
      fk.registerFrameKeys(async () => ({ keyEpoch: 999, keyIndex: index, deviceId: 'no-such-device', keys: new Map([['no-such-device', invented()], [kaityDevice, invented()]]) }));
      const calls = await import('/chat/call/index.js');
      calls.startCall(conv);
    }, { conv: convId, kaityDevice, index });
    await throughPrejoin(p, 'Join the call', 'Join');
    await p.waitForFunction(() => window.churrCall.current && window.churrCall.current.phase === 'live');
    // The call moved to this browser: Sam's first one is out of it.
    await b.waitForFunction(() => !window.churrCall.current || window.churrCall.current.over, null, { timeout: 15000 });
    await until(p, 'two hundred frames dropped', async () => { const s = await window.churrCall.current.crypto.stats(); return s.failed > 200; });
    await p.locator('.call-notes').getByText('Cannot decrypt this call').waitFor({ timeout: 15000 });
    const st = await cryptoState(p);
    assert(st.rtp.bytes > 10000, 'the packets did arrive: ' + st.rtp.bytes + ' bytes');
    assert(Object.keys(st.opened).length === 0, 'a frame opened without the key: ' + JSON.stringify(st.opened));
    assert(st.rtp.videoDecoded === 0, 'video frames were decoded without the key: ' + st.rtp.videoDecoded);
    assert(st.lines.length >= 1 && st.lines.every((l) => l.device === kaityDevice), 'the lines are Kaity\'s device\'s: ' + JSON.stringify(st.lines));
    const none = await p.evaluate(() => [...document.querySelectorAll('.call-tile:not(.self) video')].every((v) => !(v.videoWidth > 0)));
    assert(none, 'a remote picture is showing');
    console.log(`     the device without keys received ${st.rtp.bytes} bytes, dropped ${st.failed} frames, opened 0, decoded 0 video frames`);
    // And nothing it sends decrypts for Kaity, who is told so.
    await a.locator('.call-notes').getByText('Cannot decrypt this call').waitFor({ timeout: 20000 });
    await shot(a, '91-sealed-call-cannot-decrypt', { fullPage: false });
    await p.locator('.call-controls').getByRole('button', { name: 'Leave the call' }).click();
    await a.locator('.call-controls').getByRole('button', { name: 'Leave the call' }).click();
    await a.locator('#call').waitFor({ state: 'detached' });
  }, a);
}
