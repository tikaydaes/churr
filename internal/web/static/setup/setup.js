// First-run setup, opened from the one-time link the server prints when it
// starts with no owner (docs/CLIENTS.md section 15, docs/FEATURES.md
// section 26). The steps: the owner's account and passkey; the recovery
// codes, shown once; the table of outside connections with its switches;
// who may join; and a first invite.
//
// The link works once: it is spent when the owner's account is made. The
// steps after that are ordinary settings calls by the signed-in owner. If
// the page is closed part way, the owner signs in and finds the same
// switches in the admin console.

import { get, patch, post } from '/shared/api.js';
import { h, button, field, check, busy, copyText, day } from '/shared/dom.js';
import { frame, problemBox, noPasskeys, register, codesStep, loadServer, whoAmI } from '/shared/door.js';
import { installFreshCheck } from '/shared/fresh.js';

installFreshCheck();

const token = decodeURIComponent(location.pathname.split('/')[2] || '');
const STEP_KEY = 'churr.setup.step';
const STEPS = ['Owner', 'Recovery codes', 'Outside connections', 'Joining', 'Done'];
let server;

// Where the owner had got to, so a reload carries on. It holds a number
// and nothing else.
const savedStep = () => { try { return Number(sessionStorage.getItem(STEP_KEY)) || 0; } catch { return 0; } };
const saveStep = (n) => { try { sessionStorage.setItem(STEP_KEY, String(n)); } catch { /* private mode */ } };

function steps(current) {
  return h('ol', { class: 'steps', 'aria-label': 'Setup steps' }, STEPS.map((name, i) =>
    h('li', { class: i < current ? 'done' : '', 'aria-current': i === current ? 'step' : null }, `${i + 1} ${name}`)));
}

async function start() {
  server = await loadServer();
  if (!server) { frame(null, h('h1', {}, 'The server did not answer'), h('p', {}, 'Try again in a moment.')); return; }
  if (server.setup_needed) { ownerStep(); return; }
  // An owner exists. The owner who just made the account carries on;
  // anyone else is told the link is spent.
  const who = await whoAmI();
  if (who.account && who.account.role === 'owner' && savedStep() >= 2) {
    if (savedStep() === 2) connectionsStep(); else if (savedStep() === 3) joiningStep(); else doneStep();
    return;
  }
  frame(server, h('h1', {}, 'Already ', h('em', {}, 'set up')),
    h('p', {}, 'This server has its owner, so the setup link no longer works.'),
    h('p', { class: 'row' }, h('a', { class: 'btn primary', href: '/login' }, 'Sign in'),
      who.account && who.account.role === 'owner' && h('a', { class: 'btn', href: '/admin' }, 'Admin console')));
}

function ownerStep() {
  const problem = problemBox();
  const community = h('input', { required: true, maxLength: 64, autocomplete: 'organization', placeholder: 'Friends of the river' });
  const username = h('input', { required: true, minLength: 2, maxLength: 32, autocomplete: 'username', autocapitalize: 'none', spellcheck: false });
  const display = h('input', { required: true, maxLength: 64, autocomplete: 'name' });
  const go = h('button', { type: 'submit', class: 'btn primary' }, 'Create the owner’s passkey');
  frame(server,
    steps(0),
    h('h1', {}, 'Set up ', h('em', {}, 'this server')),
    h('p', {}, 'This makes the owner: the one account that can do everything here. The link you opened works once, and stops working as soon as the owner exists.'),
    problem, noPasskeys(),
    h('form', { class: 'stack', onsubmit: (e) => {
      e.preventDefault();
      busy(go, async () => {
        problem.show(null);
        try {
          const out = await register('/setup/begin',
            { setup_token: token, username: username.value.trim().toLowerCase(), display_name: display.value.trim() }, '/setup/finish');
          saveStep(1);
          // The community's name is a setting like any other, and the
          // owner is signed in now. A failure here is not worth losing
          // the recovery codes over; the name can be set in the console.
          try { await patch('/admin/settings', { 'server.name': community.value.trim() }); } catch { /* set later */ }
          server = (await loadServer()) || server;
          await codesStep(server, out.recovery_codes || [], 'Keep the owner’s recovery codes');
          saveStep(2);
          connectionsStep();
        } catch (err) {
          if (err.code === 'not_found') err.message = 'This setup link is no longer the current one. Each start of the server without an owner prints a new link; use the newest.';
          problem.show(err);
        }
      });
    } },
    field('Name of the community', community, 'Shown to everyone who signs in. It can be changed later.'),
    h('div', { class: 'cols-2' },
      field('Your username', username, 'Lowercase letters, digits, dots, dashes and underscores.'),
      field('Your display name', display)),
    h('p', { class: 'dim small' }, 'The owner signs in with a passkey: this device’s fingerprint, face or PIN. There is no password to choose.'),
    h('p', { class: 'row' }, go)));
}

// The table of DESIGN.md section 17: every connection the server can make
// to the outside, when it happens, and its switch. Rows without a switch
// are decided when the server is started or set up later; they are shown
// so that nothing the server may do is left unsaid.
const CONNECTIONS = [
  { name: 'Certificate renewal', when: 'Fetching a certificate, when the server answers HTTPS itself.', info: 'certificate', fixed: 'Decided when the server starts (-behind-proxy).' },
  { name: 'Push relay', when: 'Waking a phone for a notification. The relay sees which device, never what was said.', key: 'push.relay_enabled', def: true },
  { name: 'UnifiedPush', when: 'Waking an Android phone at an address the phone registered.', key: 'push.unifiedpush_enabled', def: false },
  { name: 'Link previews', when: 'Fetching a page when someone posts a link to it.', key: 'previews.enabled', def: false },
  { name: 'Object storage', when: 'Reading and writing uploaded files in an S3-compatible store.', info: 'object_storage', fixed: 'Set up in the admin console. Off until then.' },
  { name: 'Identity provider', when: 'Company sign-in: discovery, keys and token exchange.', info: 'identity_provider', fixed: 'Set up in the admin console. Off until then.' },
  { name: 'Links to other servers', when: 'A link or hub the owner accepts.', info: 'peers', fixed: 'Made in the admin console. Off until then.' },
];

async function connectionsStep() {
  const problem = problemBox();
  let settings = {}, outside = [];
  try { settings = (await get('/admin/settings')).settings; } catch (err) { problem.show(err); }
  try { outside = (await get('/admin/overview')).overview.outside || []; } catch { /* the fixed rows fall back to their text */ }
  const switches = new Map();
  const rows = CONNECTIONS.map((c) => {
    let control;
    if (c.key) {
      const on = settings[c.key] != null ? settings[c.key] : c.def;
      const input = h('input', { type: 'checkbox', class: 'switch', checked: on, 'aria-label': c.name });
      switches.set(c.key, input);
      // The state is said in words beside the switch, not by its colour
      // and position alone.
      const state = h('strong', {}, on ? 'On' : 'Off');
      input.addEventListener('change', () => { state.textContent = input.checked ? 'On' : 'Off'; });
      control = h('label', { class: 'row' }, input, h('span', { class: 'small' }, state, h('span', { class: 'dim' }, c.def ? ' · on unless you turn it off' : ' · off unless you turn it on')));
    } else {
      const state = outside.find((o) => o.name === c.info);
      control = h('span', { class: 'small' }, h('span', { class: 'tag ' + (state && state.enabled ? 'green' : 'line') }, state && state.enabled ? 'on' : 'off'), ' ', h('span', { class: 'dim' }, c.fixed));
    }
    return h('tr', {}, h('th', { scope: 'row' }, c.name), h('td', {}, c.when), h('td', {}, control));
  });
  const go = button('Confirm and continue', () => busy(go, async () => {
    problem.show(null);
    try {
      const body = {};
      for (const [key, input] of switches) body[key] = input.checked;
      await patch('/admin/settings', body);
      saveStep(3);
      joiningStep();
    } catch (err) { problem.show(err); }
  }), 'primary');
  frame(server,
    steps(2),
    h('h1', {}, 'What this server may ', h('em', {}, 'reach')),
    h('p', {}, 'A Churr server talks to nothing outside itself unless a row here is on. These are all of them. You must confirm this page to go on; every switch is in the admin console afterwards.'),
    problem,
    h('div', { class: 'table-wrap' }, h('table', { class: 'data outside' },
      h('thead', {}, h('tr', {}, h('th', { scope: 'col' }, 'Connection'), h('th', { scope: 'col' }, 'When it happens'), h('th', { scope: 'col' }, 'Switch'))),
      h('tbody', {}, rows))),
    h('p', { class: 'row' }, go));
}

function joiningStep() {
  const problem = problemBox();
  const mode = (value, label, hint, checked) => check(label, h('input', { type: 'radio', name: 'mode', value, checked }), hint);
  const invite = h('input', { type: 'checkbox' });
  const form = h('form', { class: 'stack', onsubmit: (e) => {
    e.preventDefault();
    busy(go, async () => {
      problem.show(null);
      try {
        await patch('/admin/settings', { 'registration.mode': new FormData(form).get('mode'), 'invites.members_may_invite': invite.checked });
        saveStep(4);
        doneStep();
      } catch (err) { problem.show(err); }
    });
  } });
  const go = h('button', { type: 'submit', class: 'btn primary' }, 'Save and finish');
  form.append(
    h('fieldset', { class: 'stack tight plain' }, h('legend', { class: 'label' }, 'Who can make an account'),
      mode('invite', 'By invite', 'Only someone holding an invite link. The usual choice.', true),
      mode('open', 'Open', 'Anyone who can reach this server.'),
      mode('closed', 'Closed', 'Nobody. People who have accounts still sign in.')),
    check('Members may invite', invite, 'Each member may make single-use invites for new members. Otherwise only admins invite.'),
    h('p', { class: 'row' }, go));
  frame(server, steps(3),
    h('h1', {}, 'Who may ', h('em', {}, 'join')),
    h('p', {}, 'Both can be changed later under Settings, Sign-in in the admin console.'),
    problem, form);
}

function doneStep() {
  const problem = problemBox();
  const slot = h('div', { class: 'stack' });
  const make = button('Make an invite link', () => busy(make, async () => {
    problem.show(null);
    try {
      const { invite } = await post('/invites', { role: 'member' });
      slot.replaceChildren(
        h('code', { class: 'secret', tabIndex: 0 }, invite.url),
        h('p', { class: 'dim small' }, `Works once${invite.expires_at ? ', until ' + day(invite.expires_at) : ''}. Send it to one person.`),
        h('p', { class: 'row' }, button('Copy the link', () => copyText(invite.url, 'Invite link copied'))));
    } catch (err) { problem.show(err); }
  }));
  frame(server, steps(4),
    h('h1', {}, 'The server is ', h('em', {}, 'ready')),
    h('p', {}, 'There is one channel, general, and you are in it. Invite the first person, or go straight in.'),
    problem, h('p', { class: 'row' }, make), slot,
    h('p', { class: 'row' },
      h('a', { class: 'btn primary', href: '/chat/', onclick: () => saveStep(0) }, 'Open the chat'),
      h('a', { class: 'btn', href: '/admin', onclick: () => saveStep(0) }, 'Admin console')),
    h('p', { class: 'dim small' }, 'The apps sign in to this server by its address: ', h('code', {}, location.host), '.'));
}

start();
