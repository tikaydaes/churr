// Joining by invite link, registering where registration is open, and
// claiming an imported account (docs/AUTH.md section 4).

import { api } from '/shared/api.js';
import { h, button, field, busy } from '/shared/dom.js';
import { frame, problemBox, noPasskeys, register, codesStep, loadServer, whoAmI, providerLinks } from '/shared/door.js';

const code = decodeURIComponent(location.pathname.split('/')[2] || '');

async function start() {
  const server = await loadServer();
  if (!server) { frame(null, h('h1', {}, 'The server did not answer'), h('p', {}, 'Try again in a moment.')); return; }
  document.title = `Join · ${server.community_name || 'Churr'}`;
  if (server.setup_needed) {
    frame(server, h('h1', {}, 'Not set up yet'), h('p', {}, 'This server has no owner yet, so nobody can join it.'));
    return;
  }
  const who = await whoAmI();
  if (who.account) {
    frame(server, h('h1', {}, 'You are already ', h('em', {}, 'in')),
      h('p', {}, `You are signed in as @${who.account.username}. An invite makes a new account; sign out first if that is what you want.`),
      h('p', { class: 'row' }, h('a', { class: 'btn primary', href: '/chat/' }, 'Open the chat')));
    return;
  }
  let invite = null;
  if (code) {
    try {
      invite = await api('GET', '/invite-codes/' + encodeURIComponent(code), undefined, { quiet401: true });
    } catch (err) {
      frame(server, h('h1', {}, 'This invite does not ', h('em', {}, 'work')),
        h('p', {}, err.code === 'not_found' ? 'It may have been used, withdrawn or run out. Ask whoever sent it for a new one.' : err.message),
        h('p', { class: 'row' }, h('a', { class: 'btn', href: '/login' }, 'Sign in instead')));
      return;
    }
  } else if (server.sign_in.registration !== 'open') {
    frame(server, h('h1', {}, 'Joining needs an ', h('em', {}, 'invite')),
      h('p', {}, server.sign_in.registration === 'closed' ? 'This server is not taking new accounts.' : 'Ask someone here for an invite link.'),
      h('p', { class: 'row' }, h('a', { class: 'btn', href: '/login' }, 'Sign in')));
    return;
  }
  form(server, invite);
}

function form(server, invite) {
  const problem = problemBox();
  const claiming = !!(invite && invite.claims_username);
  const policy = server.sign_in.member_sign_in;
  const username = h('input', { autocomplete: 'username', autocapitalize: 'none', spellcheck: false, required: true, minLength: 2, maxLength: 32, value: claiming ? invite.claims_username : '' });
  const display = h('input', { autocomplete: 'name', maxLength: 64, required: true });
  const go = h('button', { type: 'submit', class: 'btn primary' }, 'Create a passkey and join');

  // Where members must use company sign-in, an invite for a member or a
  // guest cannot make a passkey account.
  const passkeyAllowed = policy !== 'sso';
  const providers = policy === 'passkey' ? [] : providerLinks(server, code ? { invite_code: code, return_to: '/chat/' } : { return_to: '/chat/' });

  const passkeyForm = h('form', { class: 'stack', onsubmit: (e) => {
    e.preventDefault();
    busy(go, async () => {
      problem.show(null);
      try {
        const out = await register('/passkey/register/begin',
          { invite_code: code || undefined, username: username.value.trim().toLowerCase(), display_name: display.value.trim() },
          '/passkey/register/finish');
        if (out.recovery_codes && out.recovery_codes.length) await codesStep(server, out.recovery_codes);
        welcome(server, out.account);
      } catch (err) {
        if (err.code === 'name_taken') err.message = 'That username is taken. If the passkey prompt already finished, delete the passkey it made on this device, then choose another name.';
        problem.show(err);
      }
    });
  } },
  field('Username', username, 'Lowercase letters, digits, dots, dashes and underscores. People mention you with it.'),
  field('Display name', display, 'How your name is shown beside your messages.'),
  h('p', { class: 'dim small' }, 'A passkey signs you in with this device’s fingerprint, face or PIN. There is no password.'),
  h('p', { class: 'row' }, go));

  frame(server,
    claiming
      ? h('h1', {}, 'Claim ', h('em', {}, '@' + invite.claims_username))
      : h('h1', {}, 'Join ', h('em', {}, server.community_name || 'Churr')),
    claiming
      ? h('p', {}, 'This link is for an account that was imported with its history. Whoever opens it takes that account over, so only go on if it is yours.')
      : invite && h('p', {}, invite.role === 'guest' ? 'You have been invited as a guest: you will see the channels you were invited to.' : 'You have been invited as a member.'),
    problem, noPasskeys(),
    passkeyAllowed ? passkeyForm : h('p', { class: 'note' }, 'People here join through their company account.'),
    providers.length ? [passkeyAllowed && h('p', { class: 'or' }, 'or'), providers] : null,
    h('p', { class: 'dim' }, 'Already have an account? ', h('a', { href: '/login' }, 'Sign in'), '.'));
}

function welcome(server, account) {
  frame(server,
    h('h1', {}, 'You are ', h('em', {}, 'in')),
    h('p', {}, account ? `Welcome, ${account.display_name || account.username}.` : 'Welcome.'),
    h('p', { class: 'row' },
      h('a', { class: 'btn primary', href: '/chat/' }, 'Open the chat'),
      // The apps take the server's address from this link and sign in
      // through the browser themselves.
      h('a', { class: 'btn', href: 'churr://' + location.host + '/' }, 'Open in the app')));
}

start();
