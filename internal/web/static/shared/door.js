// What the sign-in, setup, join and recovery pages share: the frame they
// draw in, and the passkey and second-factor steps of docs/AUTH.md.

import { api, post, safeNext } from './api.js';
import { h, fill, button, field, check, busy, copyText } from './dom.js';
import { createPasskey, getPasskey, passkeyName, passkeysSupported } from './passkey.js';
import { qrSvg } from './qr.js';

export const root = () => document.getElementById('door');

// frame draws the page: the brand line, then whatever the step shows.
export function frame(server, ...kids) {
  const where = server && server.community_name && server.community_name !== 'Churr' ? server.community_name : (server && server.name) || '';
  fill(root(),
    h('p', { class: 'brand' }, h('img', { src: '/icons/sticker.png', alt: '' }), h('span', {}, 'Churr'),
      where && h('span', { class: 'where' }, where)),
    kids);
  const first = root().querySelector('h1');
  if (first) { first.tabIndex = -1; first.focus({ preventScroll: true }); }
}

export function problemBox() {
  const el = h('p', { class: 'problem', role: 'alert', hidden: true });
  el.show = (err) => {
    if (!err) { el.hidden = true; return; }
    if (err.name === 'NotAllowedError' || err.name === 'AbortError') el.textContent = 'The passkey prompt was closed before it finished. Try again.';
    else if (err.name === 'InvalidStateError') el.textContent = 'This device already holds a passkey for this account.';
    else el.textContent = err.message || 'Something went wrong.';
    el.hidden = false;
  };
  return el;
}

// noPasskeys is shown where a passkey is needed and the browser has none
// to offer.
export function noPasskeys() {
  return passkeysSupported() ? null : h('p', { class: 'note warn' }, 'This browser cannot use passkeys. Use a current version of Firefox, Chrome or Safari.');
}

// register runs one passkey creation between a begin and a finish
// operation, and returns the finish answer.
export async function register(beginPath, beginBody, finishPath) {
  const begin = await post(beginPath, beginBody);
  const made = await createPasskey(begin.public_key);
  return api('POST', finishPath, {
    ceremony_id: begin.ceremony_id, credential: made.credential, passkey_name: passkeyName(), prf_enabled: made.prfEnabled,
  });
}

// assert runs one passkey sign-in or check.
export async function assert(beginPath, finishPath) {
  const begin = await api('POST', beginPath);
  const credential = await getPasskey(begin.public_key);
  return api('POST', finishPath, { ceremony_id: begin.ceremony_id, credential });
}

// codesStep shows recovery codes, which the server gives once, and waits
// until the person says they have kept them.
export function codesStep(server, codes, title = 'Keep your recovery codes') {
  return new Promise((resolve) => {
    const tick = h('input', { type: 'checkbox', onchange: () => { go.disabled = !tick.checked; } });
    const go = button('Continue', () => resolve(), 'primary');
    go.disabled = true;
    frame(server,
      h('h1', {}, title),
      h('p', {}, 'Each code gets you back in once if you lose every passkey. They are shown now and never again. Keep them somewhere that is not this device.'),
      h('ul', { class: 'codes', 'aria-label': 'Recovery codes' }, codes.map((c) => h('li', {}, c))),
      h('p', { class: 'row' }, button('Copy them', () => copyText(codes.join('\n'), 'Recovery codes copied'))),
      check('I have saved these codes', tick),
      h('p', { class: 'row' }, go));
  });
}

// totpEnrol shows the secret of a new authenticator enrolment as a QR code
// and as text, and confirms it with a first code. It resolves with the
// answer of confirmTotp.
export function totpEnrol(enrolment, onDone) {
  const problem = problemBox();
  const code = h('input', { inputMode: 'numeric', autocomplete: 'one-time-code', required: true, maxLength: 12 });
  const send = h('button', { type: 'submit', class: 'btn primary' }, 'Confirm');
  return h('form', { class: 'stack', onsubmit: (e) => {
    e.preventDefault();
    busy(send, async () => {
      problem.show(null);
      try { onDone(await post('/me/totp/confirm', { code: code.value.replace(/\s+/g, '') })); } catch (err) { problem.show(err); }
    });
  } },
  h('p', {}, 'Scan this with your authenticator app, or type the secret into it. Then enter the code the app shows.'),
  h('div', { class: 'qr' }, qrSvg(enrolment.otpauth_url, 'QR code for your authenticator app')),
  h('p', {}, h('span', { class: 'label' }, 'Secret '), h('code', { class: 'wrap' }, enrolment.secret)),
  problem, field('Code', code), h('p', { class: 'row' }, send));
}

// secondFactor draws the second sign-in step for a session that still owes
// one, and resolves once the session is complete (docs/AUTH.md section 9).
export function secondFactor(server) {
  return new Promise((resolve) => {
    const problem = problemBox();
    const draw = async () => {
      let st;
      try { st = (await api('GET', '/second-factor', undefined, { quiet401: true })).second_factor; } catch (err) { problem.show(err); st = { accepted: [], set_up: [] }; }
      const done = async (answer) => {
        const codes = answer && (answer.recovery_codes || (answer.sign_in && answer.sign_in.recovery_codes));
        if (codes && codes.length) await codesStep(server, codes);
        resolve();
      };
      const has = (list, v) => (list || []).includes(v);
      const parts = [];
      if (has(st.set_up, 'passkey')) {
        const b = button('Use a passkey', () => busy(b, async () => {
          problem.show(null);
          try { done(await assert('/checks/passkey/begin', '/checks/passkey/finish')); } catch (err) { problem.show(err); }
        }), 'primary');
        parts.push(h('p', { class: 'row' }, b));
      }
      if (has(st.set_up, 'totp')) {
        const code = h('input', { inputMode: 'numeric', autocomplete: 'one-time-code', required: true, maxLength: 12 });
        const send = h('button', { type: 'submit', class: 'btn' + (has(st.set_up, 'passkey') ? '' : ' primary') }, 'Continue');
        parts.push(h('form', { class: 'stack', onsubmit: (e) => {
          e.preventDefault();
          busy(send, async () => {
            problem.show(null);
            try { done(await post('/checks/totp', { code: code.value.replace(/\s+/g, '') })); } catch (err) { problem.show(err); }
          });
        } }, field('Code from your authenticator app', code), h('p', { class: 'row' }, send)));
      }
      if (!(st.set_up || []).length) {
        parts.push(h('p', {}, 'This server needs a second sign-in step and your account has none yet. Set one up now.'));
        const slot = h('div', { class: 'stack' });
        const row = h('p', { class: 'row' });
        if (has(st.accepted, 'passkey')) {
          const b = button('Create a passkey', () => busy(b, async () => {
            problem.show(null);
            try { done(await register('/me/passkeys/begin', undefined, '/me/passkeys/finish')); } catch (err) { problem.show(err); }
          }), 'primary');
          row.append(b);
        }
        if (has(st.accepted, 'totp')) {
          const b = button('Set up an authenticator app', () => busy(b, async () => {
            problem.show(null);
            try { fill(slot, totpEnrol(await post('/me/totp'), done)); } catch (err) { problem.show(err); }
          }));
          row.append(b);
        }
        if (!row.children.length) parts.push(h('p', { class: 'note warn' }, 'No second step you could set up is accepted here. Ask an admin of this server.'));
        parts.push(row, slot);
      }
      frame(server,
        h('h1', {}, 'One more step'),
        h('p', {}, 'Your provider signed you in. This server asks for a second proof before it lets you through.'),
        problem, noPasskeys(), parts,
        h('p', { class: 'row' }, button('Sign out and start again', async () => {
          try { await api('POST', '/logout', {}, { quiet401: true }); } catch { /* the session may already be gone */ }
          location.assign('/login');
        }, 'quiet')));
    };
    draw();
  });
}

// next is where the page goes when it is done: the address it was given,
// if that is on this server.
export function nextAddress(fallback = '/chat/') {
  return safeNext(new URLSearchParams(location.search).get('next'), fallback);
}

export async function loadServer() {
  try { return (await api('GET', '/server', undefined, { quiet401: true })).server; } catch { return null; }
}

// me reads the signed-in account without sending anyone to sign in. It
// returns { account }, or { pending: true } for a session that owes its
// second step, or {} when nobody is signed in.
export async function whoAmI() {
  try {
    return { account: (await api('GET', '/me', undefined, { quiet401: true })).account };
  } catch (err) {
    if (err.code === 'second_factor_required') return { pending: true };
    if (err.status === 401) return {};
    return { error: err };
  }
}

// providerLinks are the company sign-in buttons. The browser goes to the
// provider and comes back to `next`.
export function providerLinks(server, params) {
  const providers = (server && server.sign_in && server.sign_in.providers) || [];
  return providers.map((p) => {
    const q = new URLSearchParams(params);
    return h('p', { class: 'row' }, h('a', { class: 'btn', href: `/api/v1/sso/${encodeURIComponent(p.id)}/start?${q}` }, 'Sign in through ' + p.name));
  });
}

