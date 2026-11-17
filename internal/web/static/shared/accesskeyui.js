// What the pages show of access keys (docs/AUTH.md section 5.6): a new key,
// once, with copy and download and the re-typed last group that confirms it
// was kept; and the field a key is typed into. The sign-in, setup, join,
// recovery, lock and account pages and the fresh check share these.

import { h, button, field, copyText } from './dom.js';
import { api } from './api.js';
import { newAccessKey, authOf, authFor, parseAccessKey, accessKeyWrap } from './accesskey.js';

// keyInput is the field an access key is typed or pasted into. Case
// matters, so nothing may change what is typed. A password manager may
// fill it: that is where the person was told to keep the key.
export function keyInput(attrs = {}) {
  return h('input', {
    type: 'text', class: 'access-key-input', autocomplete: 'current-password', autocapitalize: 'none', autocorrect: 'off',
    spellcheck: false, maxLength: 120, required: true, placeholder: 'xxxx xxxx xxxx …', ...attrs,
  });
}

// readKey turns what was typed into the value to send. A typing mistake is
// thrown here, with words to show, before anything is sent.
export const readKey = (input) => authFor(input.value);

// makeKey makes a new key in this page: { random, text }. Only the derived
// value of it is ever sent.
export const makeKey = newAccessKey;
export const keyAuth = (key) => authOf(key.random);

// readKeyRandom reads what was typed back to the key's 32 random bytes,
// which never leave this page; keyAuth-like values are derived from them.
export const readKeyRandom = (input) => parseAccessKey(input.value);

// keyUnlockPublic is the unlock public key for sealed chats derived from a
// key (docs/SEALED.md section 13.3, ruling R85), in base64url, sent with
// the key's auth value when the key is made.
export async function keyUnlockPublic(random) {
  const S = await import('./sealed/index.js');
  return S.toB64(await S.x25519Public(await S.unlockKeyFromAccessKey(await accessKeyWrap(random))));
}

// sealedSession opens this browser's sealed-chat state for the signed-in
// account, or gives null where the server has no sealed chats.
async function sealedSession() {
  const [{ account: me }, { server }] = await Promise.all([api('GET', '/me'), api('GET', '/server')]);
  if (!me || !server.features || !server.features.sealed_chats) return null;
  const { Session } = await import('./sealed/session.js');
  return Session.open(me, server);
}

// unlockSealedWithKey unlocks this browser's sealed chats with the access
// key just typed to sign in or unlock (ruling R85): the copy left for this
// browser if there is one, otherwise the access key's own copy. Failing
// that the sealed chats ask as they always do; nothing is lost.
export async function unlockSealedWithKey(random) {
  try {
    const s = await sealedSession();
    if (!s || s.state === 'unlocked') return;
    const backup = await s.backup();
    if (!backup) return;
    if (s.state === 'locked' && await s.hasDeviceCopy()) { await s.unlockWithDeviceCopy(); return; }
    if (backup.locked_copies.some((c) => c.kind === 'access_key' && c.key_version === backup.key_version)) await s.unlockWithAccessKey(random);
  } catch (err) { console.warn('sealed chats stay locked:', err.message); }
}

// allowNewKey gives a key this page just made its copy of the sealed-chat
// backup key, when this browser is unlocked (ruling R85). Anywhere else an
// unlocked device offers it later.
export async function allowNewKey(random) {
  try {
    const s = await sealedSession();
    if (s && s.state === 'unlocked') await s.allowAccessKey(random);
  } catch (err) { console.warn('the new access key does not unlock sealed chats yet:', err.message); }
}

function download(text, username, where) {
  const body = [
    'Churr access key',
    '',
    `Server: ${where}`,
    username ? `Username: ${username}` : null,
    `Access key: ${text}`,
    '',
    'Anyone who has this key can sign in as you and read your sealed chats. Keep it in a password manager, and delete this file once it is there.',
    '',
  ].filter((l) => l != null).join('\n');
  const url = URL.createObjectURL(new Blob([body], { type: 'text/plain;charset=utf-8' }));
  const a = h('a', { href: url, download: `churr-access-key-${(username || 'account').replace(/[^a-z0-9._-]/gi, '')}.txt` });
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

// keepKey shows a new key once: the warning, the key in its twelve groups,
// copy and download, and the last group typed again. onConfirm runs when
// the typed group matches; what it throws is shown here. A form with the
// username and the key lets a password manager offer to save it.
export function keepKey(key, { username, onConfirm, confirmLabel = 'I have kept it — continue', problem }) {
  const groups = key.text.split(' ');
  const last = groups[groups.length - 1];
  const again = h('input', {
    type: 'text', class: 'access-key-input short', autocomplete: 'off', autocapitalize: 'none', autocorrect: 'off',
    spellcheck: false, maxLength: 8, required: true, 'aria-describedby': 'ak-again-hint',
  });
  const go = h('button', { type: 'submit', class: 'btn primary' }, confirmLabel);
  const where = location.host;
  const form = h('form', { class: 'stack', autocomplete: 'on', onsubmit: async (e) => {
    e.preventDefault();
    if (again.value.replace(/[\s-]/g, '') !== last) {
      problem.show(new Error('That is not the last group of your key. Look again: capital and small letters are different.'));
      again.focus();
      return;
    }
    go.disabled = true;
    problem.show(null);
    try { await onConfirm(); } catch (err) { problem.show(err); } finally { go.disabled = false; }
  } },
  // For a password manager: the account and the key, as a sign-in form.
  h('input', { type: 'text', name: 'username', autocomplete: 'username', value: username || '', hidden: true, readOnly: true }),
  h('input', { type: 'password', name: 'access-key', autocomplete: 'new-password', value: key.text, hidden: true, readOnly: true }),
  field('Type the last group of your key', again, h('span', { id: 'ak-again-hint' }, 'The four characters at the end. This shows you have kept it.')),
  h('p', { class: 'row' }, go));
  return h('div', { class: 'stack' },
    h('ul', { class: 'note warn plain-list' },
      h('li', {}, 'Anyone who has this key can sign in as you and read your sealed chats.'),
      h('li', {}, 'It is shown now and never again. Nobody, not even this server, can show it to you later.'),
      h('li', {}, 'Keep it in a password manager. You type it on each device you sign in on.')),
    h('div', { class: 'access-key', 'aria-label': 'Your access key', role: 'group', 'data-key': key.text },
      groups.map((g) => h('span', {}, g))),
    h('p', { class: 'row' },
      button('Copy the key', () => copyText(key.text, 'Access key copied')),
      button('Download as a text file', () => download(key.text, username, where))),
    form);
}

// keyField is a labelled access key input with its hint.
export function keyField(input, label = 'Access key') {
  return field(label, input, 'The 48 characters, with or without the spaces. Capital and small letters are different.');
}
