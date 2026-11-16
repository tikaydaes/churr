// Access keys (ruling R80, docs/AUTH.md section 5.6) in Chromium against the
// real server:
//
// - in a browser with no WebAuthn at all, an invite offers the access key
//   first; the key is shown once and the account is made only once its
//   last group is typed again;
// - signing out and in with it, a typing mistake caught in the page;
// - the lock screen takes it in place of a passkey;
// - in a browser with a passkey, the person signs in with the key, adds a
//   passkey, signs in with that, and makes a new key: the old one stops
//   working.

import { step, shot, assert, noSidewaysScroll, person, PHONE, DESKTOP, consoleErrors } from './lib.mjs';

// noWebAuthn is a browser context in which the page sees no passkey API at
// all, as in an old browser.
async function noWebAuthn(browser, name) {
  const context = await browser.newContext({ viewport: DESKTOP, timezoneId: 'UTC', locale: 'en-GB', colorScheme: 'dark' });
  context.setDefaultTimeout(8000);
  context.setDefaultNavigationTimeout(25000);
  await context.addInitScript(() => {
    delete window.PublicKeyCredential;
    Object.defineProperty(Navigator.prototype, 'credentials', { get: () => undefined, configurable: true });
  });
  const page = await context.newPage();
  page.on('console', (m) => {
    if (m.type() !== 'error') return;
    // Asking "am I signed in?" is answered 401 when the answer is no, and
    // a wrong key is answered 401 on purpose below.
    if (/status of (401|429)/.test(m.text())) return;
    consoleErrors.push(`[${name}] ${m.text()} (${m.location().url || ''})`);
  });
  page.on('pageerror', (e) => consoleErrors.push(`[${name}] PAGEERROR ${e.message}`));
  return { name, context, page };
}

// typo changes one character of a key to another that is allowed, keeping
// the case pattern: a typing mistake the checksum catches.
function typo(key) {
  const chars = [...key];
  const i = chars.findIndex((c, n) => n > 10 && /[a-z]/.test(c) && c !== 'z');
  chars[i] = String.fromCharCode(chars[i].charCodeAt(0) + 1);
  return chars.join('');
}

// tryKey asks the server whether a key signs in, from a page of the server,
// deriving the value as the pages do.
async function tryKey(page, username, key) {
  return page.evaluate(async ([u, k]) => {
    const { authFor } = await import('/shared/accesskey.js');
    const r = await fetch('/api/v1/access-key/login', { method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username: u, access_key_auth: await authFor(k) }) });
    return r.status;
  }, [username, key]);
}

export async function accessKeyFlow(owner, browser, origin) {
  const ana = await noWebAuthn(browser, 'ana-no-webauthn');
  const p = ana.page;
  let key = '';
  let invite = '';

  await step('access key: an invite in a browser without passkeys offers the access key', async () => {
    invite = await owner.page.evaluate(async () => {
      const r = await fetch('/api/v1/invites', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
      return (await r.json()).invite.url;
    });
    await p.goto(invite);
    await p.getByText('This browser cannot use passkeys, so you get an access key instead').waitFor();
    assert(await p.getByRole('button', { name: 'Create a passkey and join' }).count() === 0, 'a passkey is offered with no passkey API');
    await p.getByLabel('Username').fill('ana');
    await p.getByLabel('Display name').fill('Ana Lima');
    await shot(p, '60-access-key-join', { phone: true });
    await p.getByRole('button', { name: 'Make an access key and join' }).click();
    await p.getByRole('heading', { name: 'Your access key' }).waitFor();
    key = await p.locator('.access-key').getAttribute('data-key');
    assert(/^([2-9A-HJ-NP-Za-z]{4} ){11}[2-9A-HJ-NP-Za-z]{4}$/.test(key), 'the key is not twelve groups of four: ' + key);
    await p.getByText('Anyone who has this key can sign in as you.').waitFor();
    await p.getByRole('button', { name: 'Download as a text file' }).waitFor();
    await shot(p, '61-access-key-shown', { phone: true });
    await p.setViewportSize(PHONE);
    try { await noSidewaysScroll(p); } finally { await p.setViewportSize(DESKTOP); }
  }, p);

  await step('access key: the account is made only once the last group is typed again', async () => {
    const last = key.split(' ').pop();
    const wrong = last === 'zzzz' ? 'yyyy' : 'zzzz';
    await p.getByLabel('Type the last group of your key').fill(wrong);
    await p.getByRole('button', { name: /I have kept it/ }).click();
    await p.getByText('That is not the last group of your key').waitFor();
    // Nothing was made: the name is still free.
    const free = await p.evaluate(async () => (await fetch('/api/v1/me')).status);
    assert(free === 401, 'signed in before confirming');
    await p.getByLabel('Type the last group of your key').fill(last);
    await p.getByRole('button', { name: /I have kept it/ }).click();
    await p.getByRole('heading', { name: 'You are in' }).waitFor();
    const me = await p.evaluate(async () => (await (await fetch('/api/v1/me')).json()).account);
    assert(me.username === 'ana' && me.has_access_key && me.passkey_count === 0, 'the account: ' + JSON.stringify(me).slice(0, 200));
  }, p);

  await step('access key: sign out, then in with the key; a typing mistake is caught in the page', async () => {
    await p.evaluate(() => fetch('/api/v1/logout', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }));
    await p.goto(origin + '/login');
    const form = p.locator('form[data-form="access-key"]');
    await form.waitFor();
    assert(await p.getByRole('button', { name: 'Sign in with a passkey' }).count() === 0, 'a passkey button with no passkey API');
    await form.getByLabel('Username').fill('ana');
    // An excluded character, then a wrong letter: both said before anything is sent.
    let sent = 0;
    const count = (r) => { if (r.url().endsWith('/access-key/login')) sent++; };
    p.on('request', count);
    await form.getByLabel('Access key').fill(key.replace(/[2-9]/, '0'));
    await form.getByRole('button', { name: 'Sign in with the access key' }).click();
    await p.getByText('An access key never has 0, 1, capital O or capital I').waitFor();
    await form.getByLabel('Access key').fill(typo(key));
    await form.getByRole('button', { name: 'Sign in with the access key' }).click();
    await p.getByText('That key has a typing mistake').waitFor();
    p.off('request', count);
    assert(sent === 0, `a mistyped key was sent ${sent} times`);
    await shot(p, '62-access-key-login', { phone: true });
    // Typed without spaces, as from a note.
    await form.getByLabel('Access key').fill(key.replace(/ /g, ''));
    await form.getByRole('button', { name: 'Sign in with the access key' }).click();
    await p.waitForURL(/\/chat\//);
  }, p);

  await step('access key: the lock screen unlocks with the key', async () => {
    await p.locator('#sidebar').waitFor({ timeout: 20000 });
    await p.keyboard.press('Control+Shift+L');
    await p.waitForURL(/\/locked\//);
    await p.getByRole('heading', { name: 'Locked' }).waitFor();
    assert(await p.getByRole('button', { name: 'Unlock', exact: true }).count() === 0, 'a passkey Unlock with no passkey');
    const form = p.locator('form[data-form="unlock-access-key"]');
    await form.waitFor();
    await shot(p, '63-access-key-locked', { phone: true });
    const r = await p.evaluate(async () => (await fetch('/api/v1/me')).status);
    assert(r === 401, 'the session is not locked');
    await form.getByLabel('Your access key').fill(key);
    await form.getByRole('button', { name: 'Unlock with the access key' }).click();
    await p.waitForURL(/\/chat\//, { timeout: 20000 });
    const after = await p.evaluate(async () => (await fetch('/api/v1/me')).status);
    assert(after === 200, 'not unlocked: ' + after);
  }, p);
  await ana.context.close();

  // Ana on another device, which has a passkey authenticator.
  const laptop = await person(browser, 'ana-laptop', origin);
  const q = laptop.page;
  await step('access key: in a browser with passkeys, sign in with the key and add a passkey', async () => {
    await q.goto(origin + '/login');
    await q.getByText('Sign in with an access key').click();
    const form = q.locator('form[data-form="access-key"]');
    await form.getByLabel('Username').fill('ana');
    await form.getByLabel('Access key').fill(key);
    await form.getByRole('button', { name: 'Sign in with the access key' }).click();
    await q.waitForURL(/\/chat\//);
    await q.goto(origin + '/account/sign-in');
    await q.getByRole('heading', { name: /^Access key/ }).waitFor();
    await q.getByText('You have an access key.').waitFor();
    await q.getByRole('button', { name: 'Add a passkey' }).click();
    // The first passkey brings recovery codes.
    await q.getByRole('dialog', { name: 'Your recovery codes' }).waitFor();
    await q.getByLabel('I have saved these codes').check();
    await q.getByRole('button', { name: 'Done' }).click();
    await q.getByText('Passkey added.').waitFor();
    await shot(q, '64-access-key-account');
    // Signed out, the passkey signs in.
    await q.evaluate(() => fetch('/api/v1/logout', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }));
    await q.goto(origin + '/login');
    await q.getByRole('button', { name: 'Sign in with a passkey' }).click();
    await q.waitForURL(/\/chat\//);
  }, q);

  await step('access key: a new key replaces the old one, which stops working', async () => {
    await q.goto(origin + '/account/sign-in');
    await q.getByRole('button', { name: 'Make a new access key' }).click();
    await q.getByRole('button', { name: 'Make a new key' }).click();
    const dlg = q.getByRole('dialog', { name: 'Your new access key' });
    await dlg.waitFor();
    const fresh = await dlg.locator('.access-key').getAttribute('data-key');
    assert(fresh && fresh !== key, 'the same key again');
    await dlg.getByLabel('Type the last group of your key').fill(fresh.split(' ').pop());
    await dlg.getByRole('button', { name: 'Save this key' }).click();
    await q.getByText('New access key saved. The old one no longer works.').waitFor();
    assert(await tryKey(q, 'ana', key) === 401, 'the old key still signs in');
    assert(await tryKey(q, 'ana', fresh) === 200, 'the new key does not sign in');
  }, q);
  await laptop.context.close();
}
