// What the browser tests share: starting Chromium, a browser context with
// a virtual passkey authenticator, steps that report, and screenshots.

import { chromium } from 'playwright';
import { mkdirSync } from 'node:fs';

export const SHOTS = new URL('./shots/', import.meta.url).pathname;
mkdirSync(SHOTS, { recursive: true });

export const DESKTOP = { width: 1280, height: 860 };
export const PHONE = { width: 390, height: 844 };

let failures = 0;
const budgets = new Map();

// breathe waits until the account behind a page has requests to spare.
async function breathe(page) {
  const b = budgets.get(page);
  if (!b || page.isClosed() || b.remaining >= 60) return;
  const wait = Math.ceil((60 - b.remaining) / 2) * 1000;
  await page.waitForTimeout(wait);
  b.remaining = 60;
}
export const failed = () => failures;

// Everything a page logs as an error is kept and reported at the end: a
// content security policy violation shows up here and nowhere else.
export const consoleErrors = [];

export async function launch() {
  return chromium.launch();
}

// person opens a browser context of its own, as a separate person would
// have: its own cookies and its own virtual authenticator, which makes
// and uses passkeys without anyone touching anything.
export async function person(browser, name, origin, viewport = DESKTOP) {
  const context = await browser.newContext({ viewport, timezoneId: 'UTC', locale: 'en-GB', colorScheme: 'dark' });
  await context.grantPermissions(['clipboard-read', 'clipboard-write', 'notifications'], { origin });
  context.setDefaultTimeout(8000);
  // Loading a page waits on the machine as much as on the server, and the
  // machine is shared with other builds.
  context.setDefaultNavigationTimeout(25000);
  const page = await context.newPage();
  page.on('console', (m) => {
    if (m.type() !== 'error') return;
    // Asking "am I signed in?" is answered 401 when the answer is no, and
    // the browser logs every failed request. Those are expected.
    if (/status of 401/.test(m.text())) return;
    // Operations of areas the server has not built answer 503 `unavailable`,
    // and the pages show that plainly. A member asking for an admin
    // operation is answered 403, which one test does on purpose.
    if (/status of (503|403)/.test(m.text())) return;
    // The test drives the pages faster than a person could, and sometimes
    // passes the server's request limit; the pages wait and ask again.
    if (/status of 429/.test(m.text())) return;
    // "No sealed-chat backup yet" is answered 404, and the account page
    // says so in words.
    if (/status of 404/.test(m.text()) && /\/me\/backup$/.test(m.location().url || '')) return;
    // Two devices making the same sealed-chat epoch at once: the loser is
    // answered 409 and uses the winner's record (docs/SEALED.md 5.5).
    if (/status of 409/.test(m.text()) && /\/sealed\/epochs$/.test(m.location().url || '')) return;
    // The admin test names a saved reply /ban on purpose: a protected
    // built-in, refused with 409 name_taken and said in words.
    if (/status of 409/.test(m.text()) && /\/admin\/saved-replies$/.test(m.location().url || '')) return;
    // A backup record locked just before another device replaced the
    // backup key is refused (400); the browser catches up and locks it
    // again under the new key (session.js uploadSecrets).
    if (/status of 400/.test(m.text()) && /\/me\/backup\/records\/[0-9a-f-]+\/\d+$/.test(m.location().url || '')) return;
    // The theme tests save a theme that fails the contrast check on
    // purpose (400, shown in the server's words) and open the page of a
    // theme that does not exist (404, said in words).
    if (/status of 400/.test(m.text()) && /\/api\/v1\/me\/themes$/.test(m.location().url || '')) return;
    if (/status of 404/.test(m.text()) && /\/api\/v1\/themes\/[0-9a-f-]+dead$/.test(m.location().url || '')) return;
    // The admin test names a saved reply /ban on purpose: a protected
    // built-in, refused with 409 and said in words.
    if (/status of 409/.test(m.text()) && /\/api\/v1\/admin\/saved-replies$/.test(m.location().url || '')) return;
    consoleErrors.push(`[${name}] ${m.text()} (${m.location().url || ''})`);
  });
  page.on('pageerror', (e) => consoleErrors.push(`[${name}] PAGEERROR ${e.message}`));
  // The server allows each account a burst of requests and then two a
  // second. A test moves faster than a person, so it keeps count of what
  // is left and waits between steps when it is running low (see step).
  budgets.set(page, { remaining: 120 });
  page.on('response', (r) => {
    const left = r.headers()['ratelimit-remaining'];
    if (left !== undefined) budgets.get(page).remaining = Number(left);
  });
  const cdp = await context.newCDPSession(page);
  await cdp.send('WebAuthn.enable');
  const options = {
    protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true,
    isUserVerified: true, automaticPresenceSimulation: true, hasPrf: true,
  };
  const { authenticatorId } = await cdp.send('WebAuthn.addVirtualAuthenticator', { options });
  const who = { name, context, page, cdp, authenticatorId };
  // withSecondAuthenticator runs fn while a second authenticator is the
  // only one that answers, as when a person adds a passkey on another
  // device: the first one would refuse to hold two passkeys for one
  // account.
  who.withSecondAuthenticator = async (fn) => {
    const second = await cdp.send('WebAuthn.addVirtualAuthenticator', { options: { ...options, transport: 'usb' } });
    await cdp.send('WebAuthn.setAutomaticPresenceSimulation', { authenticatorId, enabled: false });
    try { await fn(); } finally {
      await cdp.send('WebAuthn.removeVirtualAuthenticator', { authenticatorId: second.authenticatorId });
      await cdp.send('WebAuthn.setAutomaticPresenceSimulation', { authenticatorId, enabled: true });
    }
  };
  return who;
}

// shot takes a screenshot in the dark theme and again in the light one:
// name-dark.png and name-light.png. With opts.phone it also takes both at
// the width of a phone, name-phone-dark.png and name-phone-light.png, and
// puts the window back. The pages follow the system theme, which is what
// is switched here.
export async function shot(page, name, opts = {}) {
  const take = async (suffix) => {
    for (const scheme of ['dark', 'light']) {
      await page.emulateMedia({ colorScheme: scheme });
      await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
      await page.screenshot({ path: `${SHOTS}${name}${suffix}-${scheme}.png`, fullPage: opts.fullPage !== false, animations: 'disabled' });
    }
    await page.emulateMedia({ colorScheme: 'dark' });
  };
  await take('');
  if (opts.phone) {
    const was = page.viewportSize();
    await page.setViewportSize(PHONE);
    try { await take('-phone'); } finally { await page.setViewportSize(was); }
  }
}

// step runs one named part of a flow. A failure is reported with a
// screenshot and the flow carries on, so one broken screen does not hide
// the state of the rest.
export async function step(name, fn, page) {
  if (page) for (const p of budgets.keys()) await breathe(p);
  const started = Date.now();
  try {
    await fn();
    console.log(`ok   ${name} (${Date.now() - started} ms)`);
    return true;
  } catch (e) {
    failures++;
    console.log(`FAIL ${name} - ${String(e.message).split('\n').slice(0, 3).join(' | ')}`);
    if (page) {
      // every open page, so the other person's side of a failure is seen too
      let i = 0;
      for (const p of budgets.keys()) {
        if (p.isClosed()) continue;
        await p.screenshot({ path: `${SHOTS}fail-${name.replace(/\W+/g, '-').toLowerCase()}${p === page ? '' : '-' + ++i}.png`, fullPage: true }).catch(() => {});
      }
    }
    return false;
  }
}

// noSidewaysScroll fails if the page is wider than the window.
export async function noSidewaysScroll(page) {
  const over = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  if (over > 0) throw new Error(`the page scrolls sideways by ${over}px`);
}

// until asks the page again and again until fn's answer is true. Playwright's
// own waitForFunction takes the promise an async function returns as a yes
// at once, so it cannot wait on anything asynchronous (connection
// statistics, a module import): use this for those.
export async function until(page, what, fn, arg, timeout = 30000) {
  const end = Date.now() + timeout;
  for (;;) {
    if (await page.evaluate(fn, arg)) return;
    if (Date.now() > end) throw new Error('never happened: ' + what);
    await page.waitForTimeout(400);
  }
}

export function assert(cond, message) {
  if (!cond) throw new Error(message);
}

// countReels starts counting the spins that begin on a page: one begins
// when something matching selector turns while nothing did. reels reads
// the count.
export async function countReels(page, selector) {
  await page.evaluate((sel) => {
    const w = window.__reels = { starts: 0, turning: false };
    const look = () => {
      const now = !!document.querySelector(sel);
      if (now && !w.turning) w.starts++;
      w.turning = now;
    };
    new MutationObserver(look).observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ['class'] });
  }, selector);
}
export const reels = (page) => page.evaluate(() => window.__reels.starts);
