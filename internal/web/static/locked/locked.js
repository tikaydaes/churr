// The lock screen (ruling R75, docs/CLIENTS.md section 9.29, docs/AUTH.md
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

import { api, onLocked, safeNext } from '/shared/api.js';
import { h, fill } from '/shared/dom.js';
import { passkeysSupported } from '/shared/passkey.js';
import * as S from '/shared/sealed/index.js';

onLocked(null);
document.title = 'Churr';

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

function draw(problem, onUnlock, label = 'Unlock') {
  const go = h('button', { type: 'button', class: 'btn primary', id: 'unlock' }, label);
  const msg = h('p', { class: 'problem', role: 'alert', hidden: !problem }, problem || '');
  go.addEventListener('click', async () => {
    go.disabled = true;
    msg.hidden = true;
    try { await onUnlock(); } catch (err) {
      msg.textContent = err.name === 'NotAllowedError' || err.name === 'AbortError'
        ? 'The passkey prompt was closed before it finished. Try again.'
        : err.message || 'That did not work. Try again.';
      msg.hidden = false;
    } finally { go.disabled = false; }
  });
  fill(card,
    h('p', { class: 'brand' }, h('img', { src: '/icons/sticker.png', alt: '' }), h('span', {}, 'Churr')),
    h('h1', {}, 'Locked'),
    passkeysSupported() ? null : h('p', { class: 'note warn' }, 'This browser cannot use passkeys. Use a current version of Firefox, Chrome or Safari.'),
    msg,
    h('p', { class: 'row' }, go));
  go.focus({ preventScroll: true });
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

async function ownSession() {
  let begin;
  try {
    begin = await api('POST', '/session/unlock/begin', {});
  } catch (err) {
    // Signed out or revoked elsewhere while locked: the sign-in page.
    if (err.status === 401) { toSignIn(); return; }
    // Unlocked meanwhile (from another tab): go back.
    if (err.code === 'conflict') { location.replace(next); return; }
    throw err;
  }
  const got = await assertion(begin.public_key, true);
  await api('POST', '/session/unlock/finish', { ceremony_id: begin.ceremony_id, credential: got.credential });
  try { await unlockSealed(got.prf); } catch (err) { console.warn('sealed chats stay locked:', err.message); }
  if (tabs) tabs.postMessage('unlocked');
  location.replace(next);
}

async function forApp() {
  let opts;
  try {
    opts = await api('POST', '/session/unlock/options', { ceremony_id: ceremony }, { quiet401: true });
  } catch (err) {
    if (err.status === 404) throw new Error('That took too long. Go back to the app and press Unlock again.');
    throw err;
  }
  const got = await assertion(opts.public_key, false);
  await api('POST', '/session/unlock/finish', { ceremony_id: ceremony, credential: got.credential }, { quiet401: true });
  fill(card,
    h('p', { class: 'brand' }, h('img', { src: '/icons/sticker.png', alt: '' }), h('span', {}, 'Churr')),
    h('h1', {}, 'Unlocked'),
    h('p', {}, 'Go back to the app. You can close this page.'));
}

async function start() {
  standIn();
  if (ceremony) { draw(null, forApp); return; }
  // This browser's own session: make sure the server has it locked (the
  // chat may not have reached it), and find out whether it still exists.
  try {
    await api('POST', '/session/lock', {}, { quiet401: true });
  } catch (err) {
    if (err.status === 401) { toSignIn(); return; }
    draw('The server could not be reached. Unlock when it is back.', ownSession);
    return;
  }
  draw(null, ownSession);
}

start();
