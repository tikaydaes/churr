// The sign-in page: a passkey, each company sign-in provider as a button,
// a recovery code, and then the second step when the policy asks for one
// (docs/CLIENTS.md section 15, docs/AUTH.md sections 5 to 9).

import { api, post, get } from '/shared/api.js';
import { h, button, field, busy, plural, dateTime } from '/shared/dom.js';
import { frame, problemBox, noPasskeys, register, assert, codesStep, secondFactor, nextAddress, loadServer, whoAmI, providerLinks } from '/shared/door.js';

const params = new URLSearchParams(location.search);
const next = nextAddress();
let server;

const done = () => location.replace(next);

async function start() {
  server = await loadServer();
  if (!server) {
    frame(null, h('h1', {}, 'The server did not answer'), h('p', {}, 'Try again in a moment.'),
      h('p', { class: 'row' }, button('Try again', () => location.reload(), 'primary')));
    return;
  }
  document.title = `Sign in · ${server.community_name || 'Churr'}`;
  if (server.setup_needed) {
    frame(server, h('h1', {}, 'Not ', h('em', {}, 'set up'), ' yet'),
      h('p', {}, 'This server has no owner. Whoever installed it should open the setup link printed in its log when it started.'));
    return;
  }
  const who = await whoAmI();
  if (who.pending) { await secondFactor(server); done(); return; }
  if (who.account) {
    // A recovery session is signed in only far enough to make a passkey.
    const current = await currentSession();
    if (current && current.recovery) { recoveryStep(who.account); return; }
    // Someone sent here for a fresh check has to sign in again; anyone
    // else already is, and goes on.
    if (!params.has('fresh')) { done(); return; }
  }
  signInStep(who.account);
}

async function currentSession() {
  try {
    const { sessions } = await api('GET', '/me/sessions', undefined, { quiet401: true });
    return sessions.find((s) => s.current);
  } catch { return null; }
}

function signInStep(account) {
  const problem = problemBox();
  const policy = server.sign_in.member_sign_in;
  const passkey = button('Sign in with a passkey', () => busy(passkey, async () => {
    problem.show(null);
    try {
      await assert('/passkey/login/begin', '/passkey/login/finish');
      done();
    } catch (err) { problem.show(err); }
  }), 'primary');

  // Company sign-in is offered whenever the policy allows it for anyone.
  const providers = policy === 'passkey' ? [] : providerLinks(server, { return_to: next });

  const username = h('input', { autocomplete: 'username', autocapitalize: 'none', spellcheck: false, required: true, maxLength: 32 });
  const code = h('input', { autocomplete: 'off', autocapitalize: 'characters', spellcheck: false, required: true, maxLength: 24, placeholder: '7K2M-9QXD-04TV-HB3N' });
  const send = h('button', { type: 'submit', class: 'btn' }, 'Use this code');
  const recovery = h('details', { class: 'fold' },
    h('summary', {}, 'Lost your passkey? Use a recovery code'),
    h('form', { class: 'stack', onsubmit: (e) => {
      e.preventDefault();
      busy(send, async () => {
        problem.show(null);
        try {
          const out = await post('/recovery/code', { username: username.value.trim().toLowerCase(), code: code.value.trim() });
          recoveryStep(out.account);
        } catch (err) { problem.show(err); }
      });
    } },
    h('p', { class: 'dim small' }, 'A recovery code signs you in for an hour, only far enough to make a new passkey.'),
    field('Username', username), field('Recovery code', code), h('p', { class: 'row' }, send)));

  frame(server,
    h('h1', {}, 'Sign ', h('em', {}, 'in')),
    account && h('p', { class: 'note' }, `You are signed in as @${account.username}. Sign in again to confirm it is you.`),
    policy === 'sso' && h('p', { class: 'dim' }, 'Members here sign in through their company account. Owners and admins may also use a passkey.'),
    problem, noPasskeys(),
    h('p', { class: 'row' }, passkey),
    providers.length ? [h('p', { class: 'or' }, 'or'), providers] : null,
    recovery,
    server.sign_in.registration === 'open' && h('p', { class: 'dim' }, 'New here? ', h('a', { href: '/join' }, 'Create an account'), '.'));
}

// recoveryStep is what a recovery session can do: make a passkey, or, where
// the person's role must use company sign-in, link an identity
// (docs/AUTH.md section 6).
async function recoveryStep(account) {
  const problem = problemBox();
  const role = account && account.role;
  const mustSso = server.sign_in.member_sign_in === 'sso' && (role === 'member' || role === 'guest');
  if (mustSso) {
    frame(server,
      h('h1', {}, 'Link your ', h('em', {}, 'company account')),
      h('p', {}, 'People here sign in through their company account. Link yours to get back in.'),
      problem, providerLinks(server, { purpose: 'link', return_to: next }),
      h('p', { class: 'row' }, button('Sign out', signOut, 'quiet')));
    return;
  }
  const make = button('Create a new passkey', () => busy(make, async () => {
    problem.show(null);
    try {
      const out = await register('/me/passkeys/begin', undefined, '/me/passkeys/finish');
      const codes = out.sign_in && out.sign_in.recovery_codes;
      if (codes && codes.length) await codesStep(server, codes);
      afterRecovery(out.passkey);
    } catch (err) { problem.show(err); }
  }), 'primary');
  frame(server,
    h('h1', {}, 'Make a ', h('em', {}, 'new passkey')),
    h('p', {}, `You are in as @${account ? account.username : 'you'} for one hour, and only far enough to make a passkey. Creating one signs you in properly.`),
    problem, noPasskeys(), h('p', { class: 'row' }, make, button('Sign out', signOut, 'quiet')));
}

// After recovery: how many codes are left, and the chance to remove
// passkeys the person no longer has.
async function afterRecovery(made) {
  const problem = problemBox();
  let remaining = null, passkeys = [];
  try { remaining = (await get('/me/recovery-codes')).remaining; } catch { /* shown as unknown */ }
  try { passkeys = (await get('/me/passkeys')).passkeys; } catch { /* the list is optional here */ }
  const list = h('ul', { class: 'rows' });
  const draw = () => {
    list.replaceChildren(...passkeys.map((p) => {
      const isNew = made && p.id === made.id;
      const remove = button('Remove', () => busy(remove, async () => {
        problem.show(null);
        try { await api('DELETE', '/me/passkeys/' + p.id); passkeys = passkeys.filter((x) => x.id !== p.id); draw(); } catch (err) { problem.show(err); }
      }), 'danger small');
      return h('li', {}, h('div', { class: 'what' }, h('strong', {}, p.name, isNew && [' ', h('span', { class: 'tag cyan' }, 'new')]),
        h('small', {}, `Made ${dateTime(p.created_at)} · last used ${p.last_used_at ? dateTime(p.last_used_at) : 'never'}`)),
      !isNew && h('div', { class: 'does' }, remove));
    }));
  };
  draw();
  frame(server,
    h('h1', {}, 'You are ', h('em', {}, 'back in')),
    remaining != null && h('p', {}, `${plural(remaining, 'recovery code')} left. You can make a new set on your account page.`),
    passkeys.length > 1 && h('p', {}, 'Remove any passkey you no longer have, so nobody who finds that device can use it.'),
    problem, passkeys.length > 1 && list,
    h('p', { class: 'row' }, button('Continue', done, 'primary'), h('a', { class: 'btn', href: '/account' }, 'Account page')));
}

async function signOut() {
  try { await api('POST', '/logout', {}, { quiet401: true }); } catch { /* already out */ }
  location.assign('/login');
}

start();
