// Sealed chats in the web chat, in Chromium, against the real server:
//
// - the library passes every test vector and the whole transcript in the
//   browser's own Web Crypto (the same files as under Node);
// - two accounts in two browser contexts set sealed chats up, start a
//   sealed chat, and exchange messages, a reaction, an edit, a tick, a
//   delete and a file, decrypted in the other browser;
// - a second browser for one account is set up three ways (a passkey with
//   the WebAuthn PRF extension of the virtual authenticator, the recovery
//   phrase, and approval from another device by its code) and reads the
//   earlier messages; the unlock page an app opens works in a browser;
// - several tabs of one browser do not spend one counter twice;
// - a removed device is left out of the next epoch and can read nothing
//   sent after;
// - signing out wipes the browser's keys and takes it out of the list.
//
// Nothing about PRF is stubbed: the installed Chromium's virtual
// authenticator (WebAuthn.addVirtualAuthenticator with hasPrf) returns PRF
// output at creation and at every assertion, the same each time for one
// credential. A credential copied to another authenticator with
// WebAuthn.addCredential signs in but gives no PRF output (the PRF secret
// does not travel), which is why the passkey way below uses a passkey made
// in that browser and allowed from an unlocked one, as section 13.4 has it.

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { step, shot, assert, person, noSidewaysScroll, PHONE, DESKTOP } from './lib.mjs';

const ROOT = fileURLToPath(new URL('..', import.meta.url));
const IMAGE = fileURLToPath(new URL('../internal/web/static/icons/icon-192.png', import.meta.url));

// serveTests lets a page load the test files and the vectors from the
// server's own origin (so the page's content security policy allows them).
async function serveTests(page) {
  await page.route('**/__sealed-test__/**', (route) => {
    const path = new URL(route.request().url()).pathname.replace('/__sealed-test__/', '');
    const file = path.startsWith('testdata/') ? ROOT + 'testdata/sealed/' + path.slice(9) : ROOT + 'e2e/' + path;
    route.fulfill({ body: readFileSync(file), contentType: path.endsWith('.json') ? 'application/json' : 'text/javascript' });
  });
}

export const box = (page) => page.locator('#main .sealed-input');
export async function say(page, text) {
  await box(page).fill(text);
  await box(page).press('Enter');
}
export const msg = (page, text) => page.locator('#main .sealed-msg').filter({ hasText: text }).last();

// copyPasskey puts one context's passkey into another's authenticator, so
// the second can sign in as the same person. (It cannot carry PRF.)
// The copy's signature counter starts well above the original's, as a
// second authenticator's would look to the server.
let copies = 0;
export async function copyPasskey(from, to) {
  const { credentials } = await from.cdp.send('WebAuthn.getCredentials', { authenticatorId: from.authenticatorId });
  copies++;
  for (const c of credentials) await to.cdp.send('WebAuthn.addCredential', { authenticatorId: to.authenticatorId, credential: { ...c, signCount: c.signCount + 1000 * copies } });
}

export async function signInWithPasskey(who, origin) {
  await who.page.goto(origin + '/login');
  await who.page.getByRole('button', { name: 'Sign in with a passkey' }).click();
  await who.page.waitForURL(/\/chat\//);
}

// inPage runs fn(s, lib, arg) in a chat page, on the page's own sealed
// session, for checks the screen does not show (epoch recipients, the
// database). The page's content security policy allows no eval, so the
// call is handed to the browser's DevTools evaluation as one expression.
export function inPage(page, fn, arg) {
  return page.evaluate(`(async () => {
    const s = await (await import('/chat/sealed/state.js')).session();
    const lib = await import('/shared/sealed/index.js');
    return (${fn.toString()})(s, lib, ${JSON.stringify(arg === undefined ? null : arg)});
  })()`);
}

// done waits for what shows success, and fails at once with the page's
// own words if it shows a problem instead.
export async function settles(page, locator, timeout = 20000) {
  const problem = page.locator('.toast.error, .dialog-body .problem:not([hidden]), p.problem[role=alert]:not([hidden])').first();
  await Promise.race([locator.waitFor({ timeout }), problem.waitFor({ timeout }).then(async () => { throw new Error('the page says: ' + (await problem.innerText())); })]);
}

// openSealedChat opens the sealed chat from the list. Several browsers of
// one account share its request budget; a page the server turned away for
// that is loaded again after a pause, as a person would.
export async function openSealedChat(page, origin, withName) {
  const row = page.locator('.side-row').filter({ hasText: withName }).filter({ has: page.locator('.shield-p') }).getByRole('link');
  for (let i = 0; ; i++) {
    await page.goto(origin + '/chat/');
    const busy = page.getByRole('heading', { name: 'The chat could not load' });
    await Promise.race([row.waitFor({ timeout: 15000 }), busy.waitFor({ timeout: 15000 })]).catch(() => {});
    if (i < 3 && await busy.isVisible()) { await page.waitForTimeout(15000); continue; }
    break;
  }
  await row.click();
}

export async function sealedFlow(kaity, sam, browser, origin) {
  const a = kaity.page, b = sam.page;
  let samPhrase = '', kaityPhrase = '', convId = '', phraseDevice = '';

  await step('sealed: the library passes every vector and the whole transcript in Chromium', async () => {
    await serveTests(a);
    await a.goto(origin + '/chat/');
    await a.locator('#sidebar .side-link').first().waitFor();
    const out = await a.evaluate(async () => {
      const lib = await import('/shared/sealed/index.js');
      const { runVectors, VECTOR_FILES } = await import('/__sealed-test__/sealed-vectors.mjs');
      const { runTranscript } = await import('/__sealed-test__/sealed-transcript.mjs');
      const vectors = {};
      for (const f of [...VECTOR_FILES, 'transcript.json']) vectors[f] = await (await fetch('/__sealed-test__/testdata/' + f)).json();
      const v = await runVectors(lib, (n) => vectors[n], () => {});
      const t = await runTranscript(lib, vectors['transcript.json'], () => {});
      return { v: { passed: v.passed, total: v.total, failures: v.failures.slice(0, 5) }, t: { matched: t.matched, total: t.total, failures: t.failures.slice(0, 5) } };
    });
    console.log(`     in Chromium: vectors ${out.v.passed}/${out.v.total}, transcript ${out.t.matched}/${out.t.total} steps`);
    assert(out.v.passed === out.v.total && !out.v.failures.length, 'vectors in Chromium: ' + out.v.failures.join(' | '));
    assert(out.t.matched === out.t.total && !out.t.failures.length, 'transcript in Chromium: ' + out.t.failures.join(' | '));
  }, a);

  await step('sealed: Sam sets sealed chats up on the account page and is shown the phrase once', async () => {
    await b.goto(origin + '/account/sealed');
    await b.getByRole('heading', { name: 'Set up sealed chats on this account' }).waitFor();
    await shot(b, '70-sealed-setup', { phone: true });
    await b.getByRole('button', { name: 'Set up sealed chats' }).click();
    await settles(b, b.getByRole('list', { name: 'Recovery phrase' }));
    const words = await b.getByRole('list', { name: 'Recovery phrase' }).getByRole('listitem').allInnerTexts();
    assert(words.length === 12, 'twelve words, saw ' + words.length);
    samPhrase = words.map((w) => w.replace(/^\d+\.\s*/, '').trim()).join(' ');
    await shot(b, '71-sealed-phrase');
    const done = b.getByRole('dialog').getByRole('button', { name: 'Done' });
    assert(await done.isDisabled(), 'Done waits for the tick');
    await b.getByLabel('I have written these words down').check();
    await done.click();
    await b.getByText('This browser is unlocked').waitFor();
    await b.getByText('(this browser)').waitFor();
    await b.getByText('A browser · in your sealed-chat device list').waitFor();
  }, b);

  await step('sealed: Kaity starts a sealed chat with Sam: the browser is set up, the conversation id and epoch 1 are made here', async () => {
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
    assert(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab]/.test(convId), 'the conversation id is a UUIDv7 made in the browser: ' + convId);
    await box(a).waitFor();
    await a.getByText('A sealed chat').first().waitFor();
    const e = await inPage(a, (s, lib, conv) => { const c = s.device.chats.get(conv); return c.epochs.map((x) => ({ k: x.keyEpoch, ok: x.accepted, n: x.record.recipients.length })); }, convId);
    assert(e.length >= 1 && e[0].k === 1 && e[0].ok && e[0].n === 2, 'epoch 1 sealed to both browsers: ' + JSON.stringify(e));
  }, a);

  await step('sealed: the shield in the channel list, and messages both ways with Markdown', async () => {
    const row = a.locator('.side-row').filter({ has: a.locator('.shield-p') });
    await row.first().waitFor();
    await say(a, 'Hello **Sam**, this is sealed');
    await msg(a, 'this is sealed').waitFor();
    await openSealedChat(b, origin, 'Kaity');
    await msg(b, 'this is sealed').waitFor();
    assert(await msg(b, 'this is sealed').locator('strong').filter({ hasText: 'Sam' }).count() === 1, 'the Markdown is drawn');
    await say(b, 'Hello Kaity\n- [ ] bring the keys\n- [ ] lock up');
    await msg(a, 'bring the keys').waitFor();
    await shot(a, '72-sealed-chat', { phone: true });
    await a.setViewportSize(PHONE);
    try { await noSidewaysScroll(a); } finally { await a.setViewportSize(DESKTOP); }
  }, a);

  await step('sealed: a reaction, a tick, an edit, a reply and a delete travel as sealed messages', async () => {
    const hello = msg(b, 'this is sealed');
    await hello.hover();
    await hello.getByRole('button', { name: 'React' }).click();
    await b.locator('.picker').waitFor();
    await b.locator('.picker input').first().fill('thumbs');
    await b.locator('.picker').getByRole('button').filter({ hasText: '👍' }).first().click();
    await msg(a, 'this is sealed').locator('.reaction').filter({ hasText: '👍' }).waitFor();
    await msg(a, 'bring the keys').getByRole('checkbox').first().check();
    await b.locator('#main .sealed-msg li.task.done').filter({ hasText: 'bring the keys' }).waitFor();
    await say(a, 'See you at sevn');
    await msg(b, 'See you at sevn').waitFor();
    const typo = msg(a, 'See you at sevn');
    await typo.hover();
    await typo.getByRole('button', { name: 'More actions' }).click();
    await a.getByRole('menuitem', { name: 'Edit' }).click();
    await box(a).fill('See you at seven');
    await box(a).press('Enter');
    await msg(b, 'See you at seven').getByText('edited').waitFor();
    const keys = msg(b, 'bring the keys');
    await keys.hover();
    await keys.getByRole('button', { name: 'Reply' }).click();
    await say(b, 'I will bring them');
    await msg(a, 'I will bring them').locator('.msg-replyto').waitFor();
    await say(a, 'wrong chat, sorry');
    await msg(b, 'wrong chat, sorry').waitFor();
    const oops = msg(a, 'wrong chat, sorry');
    await oops.hover();
    await oops.getByRole('button', { name: 'More actions' }).click();
    await a.getByRole('menuitem', { name: 'Delete' }).click();
    await a.getByRole('dialog').getByRole('button', { name: 'Delete' }).click();
    await a.locator('#main .sealed-msg').filter({ hasText: 'wrong chat, sorry' }).waitFor({ state: 'detached' });
    await b.locator('#main .sealed-msg').filter({ hasText: 'wrong chat, sorry' }).waitFor({ state: 'detached' });
    await shot(b, '73-sealed-actions');
  }, a);

  await step('sealed: a file is encrypted in the browser with progress, and decrypted and checked in the other', async () => {
    const want = readFileSync(IMAGE);
    await a.locator('#main .sealed-composer input[type=file]').setInputFiles(IMAGE);
    await a.locator('#main .composer-files').getByText('icon-192.png').waitFor();
    await say(a, 'the icon');
    await msg(b, 'the icon').locator('.sealed-file').waitFor();
    // The image is drawn from its encrypted thumbnail.
    await msg(b, 'the icon').locator('img').waitFor();
    const dl = b.waitForEvent('download');
    await msg(b, 'the icon').getByRole('button', { name: 'Download' }).click();
    const file = await dl;
    const got = readFileSync(await file.path());
    assert(Buffer.compare(got, want) === 0, `the decrypted file differs (${got.length} bytes, want ${want.length})`);
    assert(file.suggestedFilename() === 'icon-192.png', 'the real name came inside the message: ' + file.suggestedFilename());
    // The server holds an unnamed blob, the file's size plus 16 bytes per chunk.
    const blob = await b.evaluate(async (id) => { const r = await fetch(`/api/v1/files/${id}/blob`); return { type: r.headers.get('content-type'), size: (await r.arrayBuffer()).byteLength }; },
      await msg(b, 'the icon').locator('.sealed-file').first().getAttribute('data-file'));
    assert(blob.size === want.length + 16 * Math.max(1, Math.ceil(want.length / 65536)), 'blob size ' + blob.size);
    assert(!/png/.test(blob.type || ''), 'the server knows nothing of the type: ' + blob.type);
    await shot(b, '74-sealed-file');
  }, b);

  await step('sealed: the 60-digit comparison code is the same in both browsers', async () => {
    await a.getByRole('button', { name: 'Compare codes' }).click();
    const ca = (await a.locator('.sealed-digits').innerText()).replace(/\s+/g, ' ').trim();
    await shot(a, '75-sealed-compare');
    await b.getByRole('button', { name: 'Compare codes' }).click();
    const cb = (await b.locator('.sealed-digits').innerText()).replace(/\s+/g, ' ').trim();
    assert(/^(\d{5} ){11}\d{5}$/.test(ca), 'sixty digits: ' + ca);
    assert(ca === cb, `the codes differ: ${ca} / ${cb}`);
    await a.getByRole('button', { name: 'The codes match' }).click();
    await a.getByText('codes compared').waitFor();
    await b.getByRole('dialog').getByRole('button', { name: 'Close' }).first().click();
  }, a);

  await step('sealed: several tabs of one browser never spend one counter twice', async () => {
    const tab = await kaity.context.newPage();
    await tab.goto(origin + '/chat/c/' + convId);
    await box(tab).waitFor();
    const samId = await inPage(b, (s) => s.me.id);
    const kaityId = await inPage(a, (s) => s.me.id);
    const send = (p, word) => inPage(p, async (s, lib, [conv, w, other]) => {
      for (let i = 0; i < 4; i++) await s.send(conv, other, lib.messageContent(w + ' ' + i));
    }, [convId, word, samId]);
    await Promise.all([send(a, 'tab one'), send(tab, 'tab two')]);
    const counters = await a.evaluate(async ([conv, me]) => {
      const out = [];
      let after = 0;
      for (;;) {
        let res;
        while ((res = await fetch(`/api/v1/conversations/${conv}/messages?after=${after}&limit=200`)).status === 429) await new Promise((r) => setTimeout(r, 1000));
        const page = await res.json();
        for (const m of page.messages) { after = m.seq; if (m.sealed) out.push(m.sealed.ciphertext); }
        if (!page.has_more) break;
      }
      const lib = await import('/shared/sealed/index.js');
      return out.map((c) => lib.decodeHeader(lib.fromB64(c))).filter((h) => h.accountId === me).map((h) => `${h.deviceId}/${h.counter}`);
    }, [convId, kaityId]);
    assert(new Set(counters).size === counters.length, 'a counter was used twice: ' + counters.join(' '));
    await msg(b, 'tab two 3').waitFor();
    await msg(b, 'tab one 3').waitFor();
    await tab.close();
  }, a);

  // --- a second browser for Kaity, three ways ---

  const viaPhrase = await person(browser, 'kaity-phrase', origin);
  await step('sealed: a second browser unlocks with the recovery phrase and reads earlier messages', async () => {
    await copyPasskey(kaity, viaPhrase);
    await signInWithPasskey(viaPhrase, origin);
    await openSealedChat(viaPhrase.page, origin, 'Sam');
    const p = viaPhrase.page;
    await p.getByRole('heading', { name: 'Unlock this browser for sealed chats' }).waitFor();
    await shot(p, '76-sealed-unlock', { phone: true });
    await p.getByRole('button', { name: 'Use the recovery phrase' }).click();
    await p.getByRole('textbox', { name: 'Recovery phrase' }).fill(kaityPhrase.split(' ').map((w) => w.slice(0, 4)).join(' ').toUpperCase());
    await p.getByRole('dialog').getByRole('button', { name: 'Unlock' }).click();
    await settles(p, msg(p, 'this is sealed'));
    await msg(p, 'See you at seven').waitFor();
    await msg(p, 'the icon').waitFor();
    // Sam is told Kaity added a device, a browser.
    await b.getByText('Kaity added a device: a browser.').first().waitFor({ timeout: 15000 });
    phraseDevice = await inPage(p, (s) => s.device.id);
  }, viaPhrase.page);

  const viaApproval = await person(browser, 'kaity-approval', origin);
  await step('sealed: a third browser is approved from an unlocked one by its 20-character code', async () => {
    await copyPasskey(kaity, viaApproval);
    await signInWithPasskey(viaApproval, origin);
    const p = viaApproval.page;
    await p.goto(origin + '/account/sealed');
    await p.getByRole('button', { name: 'Approve from another device' }).click();
    const code = (await p.locator('.sealed-code').innerText()).trim();
    assert(/^[0-9A-Z]{5}(-[0-9A-Z]{5}){3}$/.test(code), 'a 20-character code: ' + code);
    await p.locator('.qr svg').waitFor();
    await shot(p, '77-sealed-approval-code');
    await a.goto(origin + '/account/sealed');
    await a.getByRole('button', { name: 'Approve…' }).click();
    await a.getByLabel('Code').fill('AAAAA-AAAAA-AAAAA-AAAAA');
    await a.getByRole('dialog').getByRole('button', { name: 'Approve' }).click();
    await a.getByText('That code does not match the request').waitFor();
    await a.getByLabel('Code').fill(code.toLowerCase().replace(/-/g, ' '));
    await a.getByRole('dialog').getByRole('button', { name: 'Approve' }).click();
    await a.getByText('The new device is approved.').waitFor();
    await p.getByText('This browser is unlocked').first().waitFor({ timeout: 15000 });
    await openSealedChat(p, origin, 'Sam');
    await msg(p, 'this is sealed').waitFor({ timeout: 20000 });
    await msg(p, 'I will bring them').waitFor();
  }, viaApproval.page);

  const viaPasskey = await person(browser, 'kaity-passkey', origin);
  await step('sealed: a fourth browser unlocks with a passkey (PRF), allowed from an unlocked browser', async () => {
    // A passkey made in this browser, through a recovery code.
    const p = viaPasskey.page;
    await p.goto(origin + '/login');
    await p.getByText('Lost your passkey? Use a recovery code').click();
    await p.locator('details', { hasText: 'Lost your passkey' }).getByLabel('Username').fill('kaity');
    await p.getByLabel('Recovery code').fill(kaity.recoveryCodes[kaity.recoveryCodes.length - 1].toLowerCase());
    await p.getByRole('button', { name: 'Use this code' }).click();
    await p.getByRole('button', { name: 'Create a new passkey' }).click();
    await p.getByRole('heading', { name: 'You are back in' }).waitFor();
    await p.goto(origin + '/account/sealed');
    await p.getByRole('button', { name: 'Give its unlock key' }).first().click();
    await p.getByText('has given its unlock key').waitFor();
    // The unlocked browser lets it unlock, after saying what that means.
    await a.goto(origin + '/account/sealed');
    await a.locator('.sealed-list li').filter({ hasText: 'can be allowed to unlock sealed chats' }).getByRole('button', { name: 'Let it unlock' }).click();
    await a.getByRole('dialog').getByText(/passes through a page this server supplies/).waitFor();
    await a.getByRole('dialog').getByRole('button', { name: 'Let it unlock' }).click();
    await settles(a, a.getByText('That passkey can now unlock sealed chats.'));
    await p.reload();
    await p.getByText(/passes through a page this server supplies, in the apps as well as here/).first().waitFor({ timeout: 25000 });
    await p.getByRole('button', { name: 'Unlock with a passkey' }).click();
    await settles(p, p.getByText('This browser is unlocked').first());
    await openSealedChat(p, origin, 'Sam');
    await msg(p, 'this is sealed').waitFor({ timeout: 20000 });
  }, viaPasskey.page);

  const forApp = await person(browser, 'kaity-unlock-page', origin);
  await step('sealed: the unlock page (/oauth/unlock) leaves a key for a locked device after one passkey touch', async () => {
    await copyPasskey(kaity, forApp);
    await signInWithPasskey(forApp, origin);
    const p = forApp.page;
    await p.goto(origin + '/account/sealed');
    // Asking for approval registers the browser's keys; it is then
    // cancelled, and the unlock page is used instead.
    await p.getByRole('button', { name: 'Approve from another device' }).click();
    await p.locator('.sealed-code').waitFor();
    await p.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
    const id = await inPageAccount(p);
    const u = viaPasskey.page;
    await u.goto(origin + '/oauth/unlock?device_id=' + id);
    await u.getByRole('heading', { name: /Unlock sealed/ }).waitFor();
    await u.getByText(/passes through a page the server supplies/).waitFor();
    await shot(u, '78-unlock-page', { phone: true });
    await u.getByRole('button', { name: 'Unlock with a passkey' }).click();
    await settles(u, u.getByRole('heading', { name: /unlocked/ }));
    await p.reload();
    await p.getByRole('button', { name: 'Use the key left for this browser' }).click();
    await settles(p, p.getByText('This browser is unlocked').first());
  }, forApp.page);

  // Every browser of one account shares its request budget; those done
  // with are closed.
  await viaPasskey.context.close();
  await forApp.context.close();

  await step('sealed: a removed device is left out of the next epoch and reads nothing sent after', async () => {
    const removedId = phraseDevice;
    assert(removedId, 'the phrase browser never became a device');
    await a.goto(origin + '/account/sealed');
    await a.locator(`.sealed-list li[data-device="${removedId}"]`).getByRole('button', { name: 'Remove' }).click();
    await a.getByRole('dialog').getByText('they are replaced').waitFor();
    await a.getByRole('dialog').getByRole('button', { name: 'Remove' }).click();
    await settles(a, a.getByText('Removed, and the keys replaced.'));
    await openSealedChat(a, origin, 'Sam');
    await say(a, 'after the removal');
    await msg(b, 'after the removal').waitFor({ timeout: 15000 });
    const latest = await inPage(a, (s, lib, conv) => { const c = s.device.chats.get(conv); const e = c.current; return { k: e.keyEpoch, ids: e.record.recipients.map((r) => r.deviceId) }; }, convId);
    assert(!latest.ids.includes(removedId), 'the removed device is still a recipient of epoch ' + latest.k);
    // The removed browser's session is gone, and its keys open nothing new.
    const r = await viaPhrase.page.evaluate(async (conv) => (await fetch(`/api/v1/conversations/${conv}/sealed/epochs`)).status, convId);
    assert(r === 401, 'the removed device still reaches the server: ' + r);
    await b.getByText('Kaity’s security key changed. Their old key signed the change.').first().waitFor({ timeout: 15000 });
  }, a);

  await step('sealed: signing out wipes the browser\'s keys and takes it out of the device list', async () => {
    const p = viaApproval.page;
    const id = await inPage(p, (s) => s.device.id);
    const before = await p.evaluate(async () => (await indexedDB.databases()).map((d) => d.name));
    assert(before.some((n) => n.startsWith('churr-sealed-')), 'no sealed database before signing out');
    await p.goto(origin + '/account');
    await p.getByRole('button', { name: 'Sign out' }).first().click();
    await p.waitForURL(/\/login/);
    const after = await p.evaluate(async () => (await indexedDB.databases()).map((d) => d.name));
    assert(!after.some((n) => n.startsWith('churr-sealed-')), 'the sealed database is still there: ' + after.join(','));
    await openSealedChat(a, origin, 'Sam');
    const listed = await inPage(a, async (s, lib, devId) => { await s.locked((d) => s.syncLists(d, d.account)); return !!lib.listDevice(s.device.pins.get(s.me.id).newestList.list, devId); }, id);
    assert(!listed, 'the signed-out browser is still in the device list');
  }, viaApproval.page);

  for (const who of [viaPhrase, viaApproval]) await who.context.close();
}

// inPageAccount registers nothing: it reads the id of the device this
// browser registered, from the account page's session.
function inPageAccount(page) {
  return page.evaluate(async () => {
    const { Session } = await import('/shared/sealed/session.js');
    const me = (await (await fetch('/api/v1/me')).json()).account;
    const server = (await (await fetch('/api/v1/server')).json()).server;
    const s = await Session.open(me, server);
    return s.device.id;
  });
}
