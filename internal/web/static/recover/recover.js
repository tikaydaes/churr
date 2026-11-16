// Opening a one-time recovery link (docs/AUTH.md section 6.2). Opening the
// page uses nothing up. The button does: it makes a recovery session, and
// the sign-in page then walks the person through making a passkey or an
// access key, or linking a company identity.

import { post } from '/shared/api.js';
import { h, button, busy } from '/shared/dom.js';
import { frame, problemBox, loadServer } from '/shared/door.js';

const token = decodeURIComponent(location.pathname.split('/')[2] || '');

async function start() {
  const server = await loadServer();
  const problem = problemBox();
  if (!token) {
    frame(server, h('h1', {}, 'This link is incomplete'), h('p', {}, 'Open the whole link you were given.'));
    return;
  }
  const go = button('Recover my account', () => busy(go, async () => {
    problem.show(null);
    try {
      await post('/recovery/link', { token });
      // The sign-in page sees the recovery session and takes it from here.
      location.assign('/login');
    } catch (err) { problem.show(err); }
  }), 'primary');
  frame(server,
    h('h1', {}, 'Recover your ', h('em', {}, 'account')),
    h('p', {}, 'This link works once. It signs you in for an hour, only far enough to make a new passkey or access key.'),
    h('p', { class: 'dim' }, 'If you did not ask for it, close this page and tell an admin.'),
    problem, h('p', { class: 'row' }, go));
}

start();
