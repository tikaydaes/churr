// The lock screen (ruling R75, docs/CLIENTS.md section 9.27, docs/AUTH.md
// section 10.6). It is a page of its own, so arriving here unloads the
// chat and everything it held. It draws the product mark, "Locked" and one
// Unlock button, over a blurred stand-in of the layout made of empty
// blocks: no name, message, avatar, title or count of the account is
// fetched or drawn, and the tab says only "Churr".
//
// Two ways in:
//   /locked/?next=<path>     this browser's own session is locked; Unlock
//                            runs a passkey assertion with the PRF input,
//                            unlocks the session, then the sealed chats
//                            (the copy locking left for this browser, or
//                            the same touch's PRF output), and goes back
//   /locked/#ceremony=<id>   an app's unlock (ruling R43): the app cannot
//                            use a passkey, so it opens this page in the
//                            system browser; the page needs no session
//
// An account with an access key (docs/AUTH.md section 5.6) may type it here
// instead: the only way for an account with no passkey, and "use your access
// key instead" under the Unlock button for one with both. Wrong keys count
// with wrong passkey assertions.

import { api, onLocked, safeNext } from '/shared/api.js';
import { forgetDrafts, browserStorage } from '/shared/drafts.js';
import { h, fill } from '/shared/dom.js';
import { passkeysSupported } from '/shared/passkey.js';
import { keyInput, keyField, readKey } from '/shared/accesskeyui.js';
import * as S from '/shared/sealed/index.js';

onLocked(null);
document.title = 'Churr';
// However the page got here, the web chat's drafts are not kept while
// locked (a tab that locked may have closed before it could remove them).
forgetDrafts(browserStorage);

const params = new URLSearchParams(location.search);
const ceremony = new URLSearchParams(location.hash.slice(1)).get('ceremony');
const next = safeNext(params.get('next'), '/chat/');
const card = document.getElementById('door');
const b64 = S.toB64;
const unb64 = (s) => S.fromB64(s) || new Uint8Array(0);

let tabs = null;
try { tabs = new BroadcastChannel('churr-lock'); } catch { /* one tab only */ }
// Unlocked in another tab of this browser: every tab goes back.
if (tabs && !ceremony) tabs.onmessage = (e) => { if (e.data === 'unlocked') location.replace(next); };

function standIn() {
  // Fixed widths, so the stand-in is the same every time and says nothing.
  const side = [70, 55, 82, 60, 48, 75, 66, 52, 80, 58];
  const msgs = [[62, 40], [85], [48, 70, 30], [76], [55, 35]];
  fill(document.getElementById('stand-in'),
    h('div', { class: 'si-side' }, side.map((w, i) => h('div', { class: 'si-row' + (i === 2 ? ' on' : ''), style: { width: w + '%' } }))),
    h('div', { class: 'si-main' },
      msgs.map((lines) => h('div', { class: 'si-msg' }, h('div', { class: 'si-face' }), h('div', { class: 'si-lines' }, lines.map((w) => h('div', { class: 'si-line', style: { width: w + '%' } }))))),
      h('div', { class: 'si-box' })));
}

const closedPrompt = 'The passkey prompt was closed before it finished. Try again.';

// draw shows the lock screen with the ways this account unlocks: ways is
// { passkey, accessKey }; onPasskey and onKey do the unlocking.
function draw(problem, ways, onPasskey, onKey) {
  const msg = h('p', { class: 'problem', role: 'alert', hidden: !problem }, problem || '');
  const show = (err) => {
    msg.textContent = err.name === 'NotAllowedError' || err.name === 'AbortError' ? closedPrompt : err.message || 'That did not work. Try again.';
    msg.hidden = false;
  };
  const usePasskey = ways.passkey && passkeysSupported();
  const go = usePasskey && h('button', { type: 'button', class: 'btn primary', id: 'unlock' }, 'Unlock');
  if (go) {
    go.addEventListener('click', async () => {
      go.disabled = true;
      msg.hidden = true;
      try { await onPasskey(); } catch (err) { show(err); } finally { go.disabled = false; }
    });
  }
  let keyPart = null;
  if (ways.accessKey) {
    const key = keyInput({ id: 'unlock-key' });
    const send = h('button', { type: 'submit', class: 'btn' + (usePasskey ? '' : ' primary') }, 'Unlock with the access key');
    const form = h('form', { class: 'stack', 'data-form': 'unlock-access-key', onsubmit: async (e) => {
      e.preventDefault();
      send.disabled = true;
      msg.hidden = true;
      try { await onKey(await readKey(key)); } catch (err) { show(err); } finally { send.disabled = false; }
    } }, keyField(key, 'Your access key'), h('p', { class: 'row' }, send));
    keyPart = usePasskey ? h('details', { class: 'fold' }, h('summary', {}, 'Use your access key instead'), h('div', { class: 'stack' }, form)) : form;
  }
  fill(card,
    h('p', { class: 'brand' }, h('img', { src: '/icons/sticker.png', alt: '' }), h('span', {}, 'Churr')),
    h('h1', {}, 'Locked'),
    ways.passkey && !passkeysSupported() && h('p', { class: 'note warn' }, ways.accessKey
      ? 'This browser cannot use passkeys. Unlock with your access key.'
      : 'This browser cannot use passkeys. Use a current version of Firefox, Chrome or Safari.'),
    msg,
    go && h('p', { class: 'row' }, go),
    keyPart);
  if (go) go.focus({ preventScroll: true });
  else if (keyPart) card.querySelector('#unlock-key').focus({ preventScroll: true });
}

// assertion runs navigator.credentials.get on the server's options, with the
// PRF input when asked, and returns the credential to send (never the PRF
// output) and the PRF output, which stays in this page.
async function assertion(publicKey, withPrf) {
  const opts = structuredClone(publicKey);
  opts.challenge = unb64(opts.challenge);
  for (const c of opts.allowCredentials || []) c.id = unb64(c.id);
  if (opts.extensions) delete opts.extensions.prf;
  if (withPrf) opts.extensions = { ...(opts.extensions || {}), prf: { eval: { first: await S.prfInput() } } };
  const cred = await navigator.credentials.get({ publicKey: opts });
  const ext = cred.getClientExtensionResults ? cred.getClientExtensionResults() : {};
  const first = ext.prf && ext.prf.results && ext.prf.results.first;
  return {
    prf: first ? { credentialId: new Uint8Array(cred.rawId), prfOutput: new Uint8Array(first) } : null,
    credential: {
      id: cred.id, rawId: b64(new Uint8Array(cred.rawId)), type: cred.type,
      authenticatorAttachment: cred.authenticatorAttachment || undefined,
      clientExtensionResults: {},
      response: {
        clientDataJSON: b64(new Uint8Array(cred.response.clientDataJSON)),
        authenticatorData: b64(new Uint8Array(cred.response.authenticatorData)),
        signature: b64(new Uint8Array(cred.response.signature)),
        userHandle: cred.response.userHandle ? b64(new Uint8Array(cred.response.userHandle)) : undefined,
      },
    },
  };
}

function toSignIn() {
  location.replace('/login?next=' + encodeURIComponent(next));
}

// unlockSealed unlocks this browser's sealed chats after the session is
// unlocked: with the copy locking left for its box key, or else with the
// PRF output of the touch just made. Failing both, the sealed chats ask as
// they always do; nothing is lost.
async function unlockSealed(prf) {
  const [{ account: me }, { server }] = await Promise.all([api('GET', '/me'), api('GET', '/server')]);
  if (!server.features || !server.features.sealed_chats) return;
  const { Session } = await import('/shared/sealed/session.js');
  const s = await Session.open(me, server);
  if (s.state !== 'locked') return;
  if (await s.hasDeviceCopy()) { await s.unlockWithDeviceCopy(); return; }
  if (prf) await s.unlockWithPrf(async () => prf);
}

// beginOwn starts an unlock of this browser's own session, or leaves the
// page when there is nothing to unlock. It returns null when it left.
async function beginOwn() {
  try {
    return await api('POST', '/session/unlock/begin', {});
  } catch (err) {
    // Signed out or revoked elsewhere while locked: the sign-in page.
    if (err.status === 401) { toSignIn(); return null; }
    // Unlocked meanwhile (from another tab): go back.
    if (err.code === 'conflict') { location.replace(next); return null; }
    throw err;
  }
}

async function unlocked(prf) {
  try { await unlockSealed(prf); } catch (err) { console.warn('sealed chats stay locked:', err.message); }
  if (tabs) tabs.postMessage('unlocked');
  location.replace(next);
}

async function ownSession() {
  const begin = await beginOwn();
  if (!begin) return;
  const got = await assertion(begin.public_key, true);
  await api('POST', '/session/unlock/finish', { ceremony_id: begin.ceremony_id, credential: got.credential });
  await unlocked(got.prf);
}

async function ownSessionWithKey(auth) {
  const begin = await beginOwn();
  if (!begin) return;
  await api('POST', '/session/unlock/access-key', { ceremony_id: begin.ceremony_id, access_key_auth: auth });
  await unlocked(null);
}

async function appOptions() {
  try {
    return await api('POST', '/session/unlock/options', { ceremony_id: ceremony }, { quiet401: true });
  } catch (err) {
    if (err.status === 404) throw new Error('That took too long. Go back to the app and press Unlock again.');
    throw err;
  }
}

async function forApp() {
  const opts = await appOptions();
  const got = await assertion(opts.public_key, false);
  await api('POST', '/session/unlock/finish', { ceremony_id: ceremony, credential: got.credential }, { quiet401: true });
  appDone();
}

async function forAppWithKey(auth) {
  await api('POST', '/session/unlock/access-key', { ceremony_id: ceremony, access_key_auth: auth }, { quiet401: true });
  appDone();
}

function appDone() {
  fill(card,
    h('p', { class: 'brand' }, h('img', { src: '/icons/sticker.png', alt: '' }), h('span', {}, 'Churr')),
    h('h1', {}, 'Unlocked'),
    h('p', {}, 'Go back to the app. You can close this page.'));
}

const waysOf = (o) => ({ passkey: !!o.public_key, accessKey: !!o.access_key });

async function start() {
  standIn();
  if (ceremony) {
    let opts;
    try { opts = await appOptions(); } catch (err) {
      draw(err.message, { passkey: true, accessKey: false }, forApp, forAppWithKey);
      return;
    }
    draw(null, waysOf(opts), forApp, forAppWithKey);
    return;
  }
  // This browser's own session: make sure the server has it locked (the
  // chat may not have reached it), find out whether it still exists, and
  // which ways unlock it.
  const fallback = { passkey: true, accessKey: true };
  try {
    await api('POST', '/session/lock', {}, { quiet401: true });
  } catch (err) {
    if (err.status === 401) { toSignIn(); return; }
    draw('The server could not be reached. Unlock when it is back.', fallback, ownSession, ownSessionWithKey);
    return;
  }
  let ways = fallback;
  try {
    const begin = await beginOwn();
    if (!begin) return;
    ways = waysOf(begin);
  } catch (err) {
    draw(err.message, fallback, ownSession, ownSessionWithKey);
    return;
  }
  draw(null, ways, ownSession, ownSessionWithKey);
}

start();
