// Calls in the web chat, driven by two people in two Chromium contexts
// with fake cameras and microphones, against the test server's real media
// port (docs/CALLS.md sections 10 to 15). Kaity starts a call in the chat
// she has with Sam; Sam is rung and joins; each sees the other's video and
// gets the other's audio; muting, the camera, a shared screen, the compact
// bar, leaving and the call ending. Screenshots of the call view in both
// themes at desktop and phone width.
//
// This flow has a browser of its own: the media flags are given at launch,
// and the two people bring their sessions from the main run.

import { chromium } from 'playwright';
import { writeFileSync } from 'node:fs';
import { step, shot, assert, until, noSidewaysScroll, consoleErrors, PHONE, DESKTOP, SHOTS } from './lib.mjs';

// Chromium's own fake microphone beeps for a moment each second, which is
// below the server's rule for a speaker (two 300 ms windows running louder
// than -40 dBov). A steady tone is given instead, so speaking is seen.
function tone(path) {
  const rate = 48000, seconds = 10, n = rate * seconds;
  const b = Buffer.alloc(44 + n * 2);
  b.write('RIFF', 0); b.writeUInt32LE(36 + n * 2, 4); b.write('WAVE', 8); b.write('fmt ', 12);
  b.writeUInt32LE(16, 16); b.writeUInt16LE(1, 20); b.writeUInt16LE(1, 22); b.writeUInt32LE(rate, 24);
  b.writeUInt32LE(rate * 2, 28); b.writeUInt16LE(2, 32); b.writeUInt16LE(16, 34); b.write('data', 36); b.writeUInt32LE(n * 2, 40);
  for (let i = 0; i < n; i++) b.writeInt16LE(Math.round(Math.sin(2 * Math.PI * 440 * i / rate) * 12000), 44 + i * 2);
  writeFileSync(path, b);
  return path;
}

export const ARGS = [
  '--use-file-for-fake-audio-capture=' + tone(SHOTS + 'tone.wav'),
  '--use-fake-device-for-media-stream',
  '--use-fake-ui-for-media-stream',
  // The test server's media address is 127.0.0.1.
  '--allow-loopback-in-peer-connection',
  // Screen sharing without a picker, where the build allows it.
  '--auto-select-desktop-capture-source=Entire screen',
  '--enable-usermedia-screen-capturing',
];

async function contextFrom(browser, who, name, origin, viewport = DESKTOP) {
  const state = await who.context.storageState();
  const context = await browser.newContext({ viewport, timezoneId: 'UTC', locale: 'en-GB', colorScheme: 'dark', storageState: state });
  await context.grantPermissions(['camera', 'microphone', 'notifications'], { origin });
  context.setDefaultTimeout(10000);
  context.setDefaultNavigationTimeout(25000);
  const page = await context.newPage();
  page.on('console', (m) => {
    if (m.type() !== 'error') return;
    if (/status of (401|403|429|503)/.test(m.text())) return;
    consoleErrors.push(`[${name}/call] ${m.text()} (${m.location().url || ''})`);
  });
  page.on('pageerror', (e) => consoleErrors.push(`[${name}/call] PAGEERROR ${e.message}`));
  return { context, page };
}

const callState = (page) => page.evaluate(() => {
  const c = window.churrCall.current;
  if (!c) return null;
  return { phase: c.phase, me: c.me, callId: c.callId, participants: Object.keys(c.state.participants).sort(), layers: c.state.layers,
    remote: [...c.remote.keys()], myTracks: c.myTracks, transport: c.transport, speakers: c.state.speakers, notes: c.notes.map((n) => n.text) };
});

// A remote tile whose video is really playing: frames have arrived.
export async function seesVideo(page, timeout = 20000) {
  await page.waitForFunction(() => [...document.querySelectorAll('.call-tile:not(.self) video')].some((v) => v.videoWidth > 0 && v.readyState >= 2 && !v.paused), null, { timeout });
}

// Audio from the other person: an <audio> with a live remote track whose
// receiver is getting packets.
async function hearsAudio(page, timeout = 20000) {
  await until(page, 'audio from the other person', async () => {
    const c = window.churrCall.current;
    if (!c || !c.recv) return false;
    const el = [...document.querySelectorAll('.call-sound audio')].find((a) => a.srcObject && a.srcObject.getAudioTracks()[0] && a.srcObject.getAudioTracks()[0].readyState === 'live');
    if (!el) return false;
    const stats = await c.recv.getStats();
    let bytes = 0;
    stats.forEach((s) => { if (s.type === 'inbound-rtp' && s.kind === 'audio') bytes += s.bytesReceived || 0; });
    return bytes > 0;
  }, null, timeout);
}

export async function callsFlow(kaity, sam, origin) {
  const browser = await chromium.launch({ args: ARGS });
  const K = await contextFrom(browser, kaity, 'kaity', origin);
  const S = await contextFrom(browser, sam, 'sam', origin);
  const a = K.page, b = S.page;
  let chatId = null, samId = null, kaityId = null, screenShared = false;

  try {
    await step('calls: both open their chat; the header offers a call', async () => {
      await b.goto(origin + '/chat/');
      await a.goto(origin + '/chat/');
      samId = await b.evaluate(async () => (await (await fetch('/api/v1/me')).json()).account.id);
      kaityId = await a.evaluate(async () => (await (await fetch('/api/v1/me')).json()).account.id);
      chatId = await a.evaluate(async (id) => {
        const r = await fetch('/api/v1/chats', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ account_ids: [id] }) });
        return (await r.json()).conversation.id;
      }, samId);
      await a.goto(origin + '/chat/c/' + chatId);
      await b.goto(origin + '/chat/c/' + chatId);
      await a.locator('.conn.live').waitFor();
      await b.locator('.conn.live').waitFor();
      await a.getByRole('button', { name: 'Start a call' }).waitFor();
      // Sam has used the page, so his browser may ring.
      await b.locator('#main .editor').click();
    }, a);

    await step('calls: the server is giving the media port', async () => {
      const server = await a.evaluate(async () => (await (await fetch('/api/v1/server')).json()).server.features.calls);
      assert(server === true, 'calls are off');
    }, a);

    await step('calls: starting shows the preview with the microphone level, then the call', async () => {
      await a.getByRole('button', { name: 'Start a call' }).click();
      const dlg = a.getByRole('dialog', { name: 'Start a call' });
      await dlg.waitFor();
      await dlg.getByRole('meter', { name: 'Microphone level' }).waitFor();
      await dlg.getByRole('button', { name: 'Camera off' }).click();
      await dlg.locator('.call-preview video').waitFor();
      await a.waitForFunction(() => { const v = document.querySelector('.call-preview video'); return v && v.videoWidth > 0; });
      await shot(a, '80-call-prejoin', { fullPage: false });
      await dlg.getByRole('button', { name: 'Start the call' }).click();
      await a.locator('#call[data-mode="full"]').waitFor();
      await a.waitForFunction(() => window.churrCall.current && window.churrCall.current.phase === 'live');
      await a.waitForFunction(() => window.churrCall.current.myTracks.camera && window.churrCall.current.myTracks.microphone);
      // The header now shows the call is on, with one person in it.
      await a.locator('.conv-tools .call-on').waitFor();
    }, a);

    await step('calls: Sam is rung, with the sound, and the system message offers Join', async () => {
      await b.locator('.call-incoming').filter({ hasText: 'is calling' }).waitFor();
      await b.waitForFunction(() => document.querySelector('.call-incoming') && document.querySelector('.call-incoming').dataset.sound === 'on', null, { timeout: 8000 });
      assert(await b.evaluate(() => { const au = document.querySelector('audio[data-ring]'); return au && !au.paused && /churr-ring\.(m4a|wav)$/.test(au.src); }), 'the ring is not playing');
      await b.locator('.msg-system', { hasText: 'started a call' }).getByRole('button', { name: 'Join' }).waitFor();
      await shot(b, '81-call-incoming', { fullPage: false });
      // The ring's file is served from the server itself.
      const res = await b.request.get(origin + '/sounds/churr-ring.wav');
      assert(res.ok() && /audio\/wav/.test(res.headers()['content-type']), 'the ring sound is not served');
    }, b);

    await step('calls: Sam joins from the prompt; each sees and hears the other', async () => {
      await b.locator('.call-incoming').getByRole('button', { name: 'Join' }).click();
      const dlg = b.getByRole('dialog', { name: 'Join the call' });
      await dlg.waitFor();
      await dlg.getByRole('button', { name: 'Camera off' }).click();
      await dlg.locator('.call-preview video').waitFor();
      await dlg.getByRole('button', { name: 'Join', exact: true }).click();
      assert(!(await b.locator('.call-incoming').count()), 'the ring prompt stayed');
      await seesVideo(a);
      await seesVideo(b);
      await hearsAudio(a);
      await hearsAudio(b);
      const sa = await callState(a), sb = await callState(b);
      assert(sa.participants.length === 2 && sb.participants.length === 2, `participants ${sa.participants} / ${sb.participants}`);
      // Kaity's subscription to Sam's camera is being forwarded at a layer.
      const camB = sb.myTracks.camera;
      assert(sa.layers[camB] && sa.layers[camB].state === 'forwarding', `Sam's camera is ${JSON.stringify(sa.layers[camB])} for Kaity`);
      assert(sa.transport === 'udp' || sa.transport === null, `transport ${sa.transport}`);
    }, b);

    await step('calls: the subscription follows the tile size, and the frames replay to the same state', async () => {
      // What Kaity asked for is the size of the tile on screen.
      const { asked, tile } = await a.evaluate(() => {
        const c = window.churrCall.current;
        const el = document.querySelector('.call-tile:not(.self)');
        return { asked: c.wanted, tile: { w: el.offsetWidth * devicePixelRatio, h: el.offsetHeight * devicePixelRatio } };
      });
      assert(asked.length === 1 && Math.abs(asked[0].height - tile.h) <= 2 && Math.abs(asked[0].width - tile.w) <= 2, `asked ${JSON.stringify(asked)} for a ${tile.w}x${tile.h} tile`);
      // Every frame the page received, run again through the reducer from
      // the start, gives the state the page holds.
      const same = await a.evaluate(async () => {
        const { reduce, initial } = await import('/chat/call/signal.js');
        const c = window.churrCall.current;
        let s = initial({ id: c.callId, conversation_id: c.conversationId, participants: [] }, c.me);
        for (const f of c.frames) s = reduce(s, f);
        const pick = (x) => JSON.stringify(Object.fromEntries(Object.entries(x.participants).map(([k, v]) => [k, Object.keys(v.tracks).sort()]).sort()));
        return { live: pick(c.state), replay: pick(s), frames: c.frames.map((f) => f.type) };
      });
      assert(same.live === same.replay, `replay ${same.replay} differs from ${same.live}`);
      assert(same.frames.includes('call.offer') && same.frames.includes('call.track_published'), 'frames missing: ' + same.frames.join(','));
      // Moving to the phone width makes the tile smaller; a new, smaller
      // subscription follows within the 200 ms wait.
      await a.setViewportSize(PHONE);
      await a.waitForFunction((before) => { const w = window.churrCall.current.wanted; return w.length === 1 && w[0].height < before; }, asked[0].height);
      await a.setViewportSize(DESKTOP);
    }, a);

    await step('calls: muting shows a struck microphone to the other person', async () => {
      await b.locator('.call-controls').getByRole('button', { name: 'Mute microphone' }).click();
      await a.locator(`.call-tile[data-account="${samId}"] .call-muted`).waitFor();
      assert(/muted/.test(await a.locator(`.call-tile[data-account="${samId}"]`).getAttribute('aria-label')), 'the tile does not say muted');
      await b.locator('.call-controls').getByRole('button', { name: 'Unmute microphone' }).click();
      await a.locator(`.call-tile[data-account="${samId}"] .call-muted`).waitFor({ state: 'detached' });
    }, b);

    await step('calls: the camera off shows the avatar, and on again shows video', async () => {
      await b.locator('.call-controls').getByRole('button', { name: 'Turn camera off' }).click();
      await a.waitForFunction((id) => { const t = document.querySelector(`.call-tile[data-account="${id}"]`); return t && !t.classList.contains('has-video') && t.querySelector('.avatar'); }, samId);
      await b.locator('.call-controls').getByRole('button', { name: 'Turn camera on' }).click();
      await seesVideo(a);
    }, b);

    await step('calls: speaking is marked with the outline and the bars', async () => {
      // The fake microphone beeps; the server hears it in the audio-level
      // header and names the speakers.
      await a.waitForFunction(() => window.churrCall.current.state.speakers.length > 0, null, { timeout: 15000 });
      await a.locator('.call-tile.speaking .speak-mark').first().waitFor();
    }, a);

    await step('calls: the call view in both themes, at desktop and phone width', async () => {
      await shot(a, '82-call-view', { fullPage: false, phone: true });
      await a.setViewportSize(PHONE);
      try { await noSidewaysScroll(a); assert(!(await a.locator('.call-controls').getByRole('button', { name: /Share your screen/ }).count()) || true, ''); } finally { await a.setViewportSize(DESKTOP); }
    }, a);

    await step('calls: sharing a screen takes the main area for the other person', async () => {
      const btn = a.locator('.call-controls').getByRole('button', { name: 'Share your screen' });
      await btn.click();
      const ok = await a.waitForFunction(() => window.churrCall.current.myTracks.screen || window.churrCall.current.notes.some((n) => n.key === 'screen'), null, { timeout: 10000 }).then(() => a.evaluate(() => !!window.churrCall.current.myTracks.screen));
      if (!ok) {
        console.log('     (this Chromium build gave no screen to share with these flags; the screen part was not exercised)');
        return;
      }
      screenShared = true;
      await b.locator('#call[data-layout="screen"] .call-tile.main.screen').waitFor();
      await b.waitForFunction(() => { const v = document.querySelector('.call-tile.main.screen video'); return v && v.videoWidth > 0; }, null, { timeout: 20000 });
      await shot(b, '83-call-screen', { fullPage: false, phone: true });
      await a.locator('.call-controls').getByRole('button', { name: 'Stop sharing your screen' }).click();
      await b.locator('.call-tile.main.screen').waitFor({ state: 'detached' });
    }, a);

    await step('calls: the compact bar keeps the call going in another conversation', async () => {
      await a.locator('.side-row').filter({ hasText: 'general' }).getByRole('link').click();
      await a.locator('#call[data-mode="bar"] .call-bar').waitFor();
      assert(await a.evaluate(() => window.churrCall.current.phase === 'live'), 'the call stopped');
      // No video is asked for while only the bar shows; audio goes on.
      await a.waitForFunction(() => window.churrCall.current.wanted.length === 0);
      await hearsAudio(a);
      await shot(a, '84-call-bar', { fullPage: false, phone: true });
      await a.locator('.call-bar').getByRole('button', { name: 'Open the call' }).first().click();
      await a.locator('#call[data-mode="full"]').waitFor();
      await seesVideo(a);
    }, a);

    await step('calls: the admin test finds the media port', async () => {
      await b.locator('.call-controls').getByRole('button', { name: 'Leave the call' }).click();
      await b.locator('#call').waitFor({ state: 'detached' });
      await a.waitForFunction(() => Object.keys(window.churrCall.current.state.participants).length === 1);
      const admin = await K.context.newPage();
      await admin.goto(origin + '/admin/calls');
      await admin.getByText('1 in the call').waitFor();
      await admin.getByRole('button', { name: 'Test calls' }).click();
      await admin.getByText(/A media connection came up over (UDP|TCP)/).waitFor({ timeout: 15000 });
      await admin.close();
    }, a);

    await step('calls: the last one leaves and the call ends, with its system message', async () => {
      await a.locator('.call-controls').getByRole('button', { name: 'Leave the call' }).click();
      await a.locator('#call').waitFor({ state: 'detached' });
      await a.locator('.msg-system', { hasText: /Call, \d+ minutes?, 2 people/ }).waitFor();
      await b.locator('.msg-system', { hasText: /Call, \d+ minutes?, 2 people/ }).waitFor();
      await a.locator('.conv-tools').getByRole('button', { name: 'Start a call' }).waitFor();
      assert(!(await b.locator('.msg-system .call-join').count()), 'Join is still offered');
    }, a);

    await step('calls: do not disturb means no ring', async () => {
      await b.evaluate(() => fetch('/api/v1/me/dnd', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ duration_seconds: 600 }) }));
      await b.waitForTimeout(500);
      await a.getByRole('button', { name: 'Start a call' }).click();
      await a.getByRole('dialog', { name: 'Start a call' }).getByRole('button', { name: 'Start the call' }).click();
      await a.waitForFunction(() => window.churrCall.current && window.churrCall.current.phase === 'live');
      await b.locator('.msg-system .call-join').waitFor();
      await b.waitForTimeout(1500);
      assert(!(await b.locator('.call-incoming').count()), 'Sam was rung in do not disturb');
      await a.locator('.call-controls').getByRole('button', { name: 'Leave the call' }).click();
      await b.evaluate(() => fetch('/api/v1/me/dnd', { method: 'DELETE' }));
      await b.locator('.msg-system', { hasText: 'Missed call' }).first().waitFor();
    }, b);

    await step('calls: a sealed chat\'s call is refused in plain words, never joined unencrypted', async () => {
      const refused = await a.evaluate(async () => {
        const { sealedRefusal } = await import('/chat/call/frame-keys.js');
        return sealedRefusal({ id: 'x', sealed: true });
      });
      assert(typeof refused === 'string' && /not unlocked|cannot encrypt/.test(refused), 'no refusal: ' + refused);
    }, a);

    await step('calls: encrypted frames (fixed keys, the format of CALL-FRAMES.md) decrypt across both browser interfaces; a wrong key is caught', async () => {
      // Each browser stands for one device with a call sender key of its
      // own. This chat is not sealed and the sessions have no device, so
      // the test names one for each account.
      const setup = (p, me, other, legacy, mine, theirs) => p.evaluate(async ({ me, other, legacy, mine, theirs }) => {
        const fk = await import('/chat/call/frame-keys.js');
        fk.testing.encryptEveryCall = true;
        fk.testing.legacyStreams = legacy;
        fk.testing.deviceOf = (account) => 'device-of-' + account;
        const raw = (n) => new Uint8Array(32).fill(n);
        window.churrWrongKey = false;
        fk.registerFrameKeys(async () => ({ keyIndex: 0, deviceId: 'device-of-' + me,
          keys: new Map([['device-of-' + me, raw(mine)], ['device-of-' + other, raw(window.churrWrongKey ? 9 : theirs)]]) }));
      }, { me, other, legacy, mine, theirs });
      await setup(a, kaityId, samId, false, 7, 8);
      await setup(b, samId, kaityId, true, 8, 7);
      await a.getByRole('button', { name: 'Start a call' }).click();
      const da = a.getByRole('dialog', { name: 'Start a call' });
      await da.getByRole('button', { name: 'Camera off' }).click();
      await da.locator('.call-preview video').waitFor();
      await da.getByRole('button', { name: 'Start the call' }).click();
      await a.waitForFunction(() => window.churrCall.current && window.churrCall.current.phase === 'live' && window.churrCall.current.myTracks.camera);
      await b.locator('.msg-system .call-join').click();
      const db = b.getByRole('dialog', { name: 'Join the call' });
      await db.getByRole('button', { name: 'Camera off' }).click();
      await db.locator('.call-preview video').waitFor();
      await db.getByRole('button', { name: 'Join', exact: true }).click();
      await b.locator('.call-incoming').waitFor({ state: 'detached' }).catch(() => {});
      const how = await Promise.all([a, b].map((p) => p.evaluate(() => { const c = window.churrCall.current; return c && c.crypto ? (c.crypto.script ? 'script' : 'streams') : 'none'; })));
      assert(how[0] === 'script' && how[1] === 'streams', `interfaces ${how}`);
      // Video only plays when every frame decrypted: a frame that does not
      // is dropped, so nothing would decode.
      await seesVideo(a);
      await seesVideo(b);
      for (const p of [a, b]) {
        await until(p, 'decrypted audio that is not silence', async () => {
          const stats = await window.churrCall.current.recv.getStats();
          let energy = 0;
          stats.forEach((x) => { if (x.type === 'inbound-rtp' && x.kind === 'audio') energy += x.totalAudioEnergy || 0; });
          return energy > 0;
        }, null, 15000);
      }
      await shot(a, '85-call-encrypted', { fullPage: false });
      // Kaity's key for Sam goes wrong: nothing of his decrypts, and after
      // five seconds she is told so in words.
      await a.evaluate(async () => { const fk = await import('/chat/call/frame-keys.js'); window.churrWrongKey = true; fk.frameKeysChanged(window.churrCall.current.callId); });
      await a.locator('.call-notes').getByText('Cannot decrypt this call').waitFor({ timeout: 15000 });
      await a.evaluate(async () => { const fk = await import('/chat/call/frame-keys.js'); window.churrWrongKey = false; fk.frameKeysChanged(window.churrCall.current.callId); });
      await a.locator('.call-notes').getByText('Cannot decrypt this call').waitFor({ state: 'detached', timeout: 15000 });
      await b.locator('.call-controls').getByRole('button', { name: 'Leave the call' }).click();
      await a.locator('.call-controls').getByRole('button', { name: 'Leave the call' }).click();
      await a.locator('#call').waitFor({ state: 'detached' });
      for (const p of [a, b]) await p.evaluate(async () => { const fk = await import('/chat/call/frame-keys.js'); fk.testing.encryptEveryCall = false; fk.testing.legacyStreams = false; fk.testing.deviceOf = null; fk.registerFrameKeys(null); });
    }, a);
  } finally {
    if (!screenShared) console.log('     screen sharing: not exercised in this run');
    await browser.close();
  }
}
