// The fresh check: one dialog, shared by the account page, the admin
// console and the web chat (docs/CLIENTS.md section 15, docs/AUTH.md
// section 10.4).
//
// Some actions need the session to have been checked in the last ten
// minutes. When the API answers `fresh_check_required`, api.js calls the
// function installed here. It asks for a passkey touch, the access key or
// an authenticator code, or sends the person back through company sign-in
// if they have none, and says whether the check passed so the action can be
// repeated. With both a passkey and an access key, the passkey comes first
// and the key is offered in its place.

import { api, get, post, onFreshCheck, safeNext } from './api.js';
import { h, openDialog, button, field, busy } from './dom.js';
import { getPasskey, passkeysSupported } from './passkey.js';
import { keyInput, keyField, readKey } from './accesskeyui.js';

let open = null;

export function freshCheck() {
  // Several requests can fail at once; they share the one dialog.
  if (open) return open;
  open = run().finally(() => { open = null; });
  return open;
}

async function run() {
  let me;
  try { me = (await get('/me')).account; } catch { return false; }
  const hasPasskey = me.passkey_count > 0 && passkeysSupported();
  const hasTotp = me.has_totp;
  const hasKey = me.has_access_key;
  const result = await openDialog('Check it is you', (close) => {
    const problem = h('p', { class: 'problem', role: 'alert', hidden: true });
    const fail = (err) => {
      if (err && (err.name === 'NotAllowedError' || err.name === 'AbortError')) problem.textContent = 'The passkey prompt was closed. Try again.';
      else problem.textContent = (err && err.message) || 'That did not work.';
      problem.hidden = false;
    };
    const body = h('div', { class: 'stack' },
      h('p', {}, 'This changes how you sign in, or hands out a credential, so the server wants a check from the last ten minutes.'),
      problem);
    if (hasPasskey) {
      const btn = button('Use a passkey', () => busy(btn, async () => {
        try {
          const begin = await api('POST', '/checks/passkey/begin', undefined, { noFresh: true });
          const credential = await getPasskey(begin.public_key);
          await api('POST', '/checks/passkey/finish', { ceremony_id: begin.ceremony_id, credential }, { noFresh: true });
          close(true);
        } catch (err) { fail(err); }
      }), 'primary');
      body.append(h('p', { class: 'row' }, btn));
    }
    if (hasKey) {
      const key = keyInput();
      const send = h('button', { type: 'submit', class: 'btn' + (hasPasskey ? '' : ' primary') }, 'Check the key');
      const form = h('form', { class: 'stack', 'data-form': 'access-key-check', onsubmit: (e) => {
        e.preventDefault();
        busy(send, async () => {
          try {
            await api('POST', '/checks/access-key', { access_key_auth: await readKey(key) }, { noFresh: true });
            close(true);
          } catch (err) { fail(err); }
        });
      } }, keyField(key, 'Your access key'), h('p', { class: 'row' }, send));
      body.append(hasPasskey ? h('details', { class: 'fold' }, h('summary', {}, 'Use your access key instead'), h('div', { class: 'stack' }, form)) : form);
    }
    if (hasTotp) {
      const code = h('input', { inputMode: 'numeric', autocomplete: 'one-time-code', required: true, maxLength: 12 });
      const send = h('button', { type: 'submit', class: 'btn' + (hasPasskey || hasKey ? '' : ' primary') }, 'Check the code');
      body.append(h('form', { class: 'stack', onsubmit: (e) => {
        e.preventDefault();
        busy(send, async () => {
          try { await post('/checks/totp', { code: code.value.replace(/\s+/g, '') }); close(true); } catch (err) { fail(err); }
        });
      } }, field('Code from your authenticator app', code), h('p', { class: 'row' }, send)));
    }
    if (!hasPasskey && !hasTotp && !hasKey) {
      const next = safeNext(location.pathname + location.search);
      body.append(
        h('p', {}, me.passkey_count > 0
          ? 'This browser cannot use passkeys, so the check is to sign in again. You will come back to this page; then repeat what you were doing.'
          : 'Your account has no passkey, access key or authenticator app, so the check is to sign in again through your provider. You will come back to this page; then repeat what you were doing.'),
        h('p', { class: 'row' }, h('a', { class: 'btn primary', href: '/login?fresh=1&next=' + encodeURIComponent(next) }, 'Sign in again')));
    }
    return { body, foot: [button('Cancel', () => close(false))] };
  });
  return result === true;
}

// install makes the dialog the answer to every `fresh_check_required`.
export function installFreshCheck() { onFreshCheck(freshCheck); }
