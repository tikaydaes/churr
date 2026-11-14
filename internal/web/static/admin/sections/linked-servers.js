// Linked servers (owner): create a link invitation, accept one, join a
// hub; each peer's name, key fingerprint and state; pause or remove; and
// the clock figures each link has measured (ruling R57): offset, latency,
// jitter each way and the uncertainty of the offset.

import { get, post, patch, put, del } from '/shared/api.js';
import { h, button, toast, confirmDialog, dateTime, ago } from '/shared/dom.js';
import { card, tryLoad } from '/shared/console.js';
import { formDialog, secretDialog } from '/shared/form.js';
import { clockFigures, clockWarning } from '/shared/hub.js';

// clockBlock draws one PeerClock: who measured whom, and the figures.
function clockBlock(title, c) {
  if (!c) return null;
  const warn = clockWarning(c);
  return h('div', { class: 'clock' },
    h('small', { class: 'label' }, title),
    h('dl', { class: 'clock-figures' }, clockFigures(c).map(([k, v]) => h('div', {}, h('dt', {}, k), h('dd', { class: 'mono' }, v)))),
    h('small', { class: 'dim' }, 'Measured ' + ago(c.measured_at)),
    warn ? h('small', { class: 'note warn' }, warn) : null);
}

export default async function linkedServers(ctx) {
  const [peersGot, serversGot] = await Promise.all([tryLoad(() => get('/admin/peers')), tryLoad(() => get('/admin/servers'))]);
  const run = async (fn, said) => { try { await fn(); if (said) toast(said); ctx.redraw(); } catch (err) { if (!err.silent) toast(err.message, true); } };

  const invite = button('Make an invitation', async () => {
    const out = await formDialog('Make a link invitation', [
      { name: 'expires', label: 'Works for', type: 'select', value: '604800', options: [['86400', 'A day'], ['604800', 'A week']] },
    ], { yes: 'Make it', intro: 'One string holding this server’s name, address, key fingerprint and a one-time secret. This server listens; the one that accepts dials. A direct link joins exactly two servers.',
      submit: (v) => post('/admin/peers/invitations', { expires_in_seconds: Number(v.expires) }) });
    if (out) { await secretDialog('Link invitation', [['Give this to the other server’s owner', out.invitation]], `Shown once. It works until ${dateTime(out.expires_at)}.`); ctx.redraw(); }
  }, 'small primary');

  const accept = button('Accept an invitation', async () => {
    const v = await formDialog('Accept a link invitation', [{ name: 'invitation', label: 'Invitation', type: 'textarea', required: true, rows: 3, hint: 'It starts with churr_link_. A hub’s invitation is accepted the same way.' }], { yes: 'Look at it' });
    if (!v) return;
    let seen;
    try { seen = await post('/admin/peers/invitations/inspect', { invitation: v.invitation.trim() }); } catch (err) { if (!err.silent) toast(err.message, true); return; }
    const yes = await confirmDialog(`Link to ${seen.name}?`, [
      h('p', {}, 'This is a ', h('strong', {}, seen.kind === 'hub' ? 'hub' : 'server'), ' called ', h('strong', {}, seen.name), '.'),
      h('p', {}, 'Its key fingerprint: ', h('code', { class: 'wrap' }, seen.fingerprint)),
      'Check the fingerprint with its owner by some other means before going on. Once linked, the two servers can offer each other channels to share.',
    ], { yes: 'Link' });
    if (yes) run(() => post('/admin/peers', { invitation: v.invitation.trim() }), 'Linked.');
  }, 'small');

  const peers = card(h('span', { class: 'row grow between' }, 'Links', h('span', { class: 'row' }, accept, invite)),
    h('p', { class: 'dim' }, 'A link lets this server share chosen channels with another. Nothing is shared by the link itself.'),
    peersGot.problem || (peersGot.ok.peers.length ? h('ul', { class: 'rows' }, peersGot.ok.peers.map((p) => {
      const pause = p.state === 'active' ? button('Pause', () => run(() => patch('/admin/peers/' + p.id, { state: 'paused' }), 'Paused.'), 'small')
        : p.state === 'paused' ? button('Resume', () => run(() => patch('/admin/peers/' + p.id, { state: 'active' }), 'Resumed.'), 'small') : null;
      const remove = button('Remove', async () => {
        if (await confirmDialog(`Remove the link to ${p.name || 'this server'}?`, ['Each shared channel stays here as an archived local channel with the history it has.', 'Channels this server owns must be handed over or closed first.'], { yes: 'Remove', danger: true })) run(() => del('/admin/peers/' + p.id), 'Removed.');
      }, 'small danger');
      return h('li', {}, h('div', { class: 'what' },
        h('strong', {}, p.name || 'Invitation not yet accepted', ' ', h('span', { class: 'tag line' }, p.kind), ' ', h('span', { class: 'tag ' + (p.state === 'active' ? 'green' : p.state === 'paused' ? 'yellow' : 'grey') }, p.state)),
        p.fingerprint && h('small', {}, 'Key ', h('code', { class: 'wrap' }, p.fingerprint)),
        h('small', {}, [p.dials ? 'This server dials' : 'This server listens', p.connected_at ? 'connected ' + ago(p.connected_at) : 'not connected', p.invite_expires_at && 'invitation until ' + dateTime(p.invite_expires_at)].filter(Boolean).join(' · ')),
        p.last_error && h('small', { class: 'note warn' }, p.last_error),
        clockBlock(p.kind === 'hub' ? 'This server’s clock, as the hub measures it' : 'This server’s clock, as ' + (p.name || 'the other server') + ' measures it', p.told_clock),
        clockBlock((p.name || 'The other server') + '’s clock, as this server measures it', p.measured_clock),
        p.state === 'active' && !p.told_clock && !p.measured_clock ? h('small', { class: 'dim' }, 'No clock figures yet: they come with the first exchanges once connected.') : null),
      h('div', { class: 'does' }, p.kind === 'hub' && p.state === 'active' ? h('a', { class: 'btn small', href: '/admin/hubs/' + p.id, onclick: (e) => { e.preventDefault(); ctx.go('hubs', p.id); } }, 'Directory') : null, pause, remove));
    })) : h('p', { class: 'empty' }, 'This server is linked to no other.')));

  const known = card('Servers this one has met',
    serversGot.problem || (serversGot.ok.servers.length ? h('ul', { class: 'rows' }, serversGot.ok.servers.map((s) => {
      const block = s.blocked_at ? button('Unblock', () => run(() => del(`/admin/servers/${s.id}/block`), 'Unblocked.'), 'small')
        : button('Block', async () => { if (await confirmDialog(`Block ${s.name}?`, 'Nothing more is accepted from it, through any link or hub.', { yes: 'Block', danger: true })) run(() => put(`/admin/servers/${s.id}/block`), 'Blocked.'); }, 'small danger');
      return h('li', {}, h('div', { class: 'what' }, h('strong', {}, s.name, ' ', s.blocked_at && h('span', { class: 'tag red' }, 'blocked')),
        h('small', {}, 'Key ', h('code', { class: 'wrap' }, s.fingerprint)), h('small', {}, 'First seen ' + dateTime(s.first_seen_at))), h('div', { class: 'does' }, block));
    })) : h('p', { class: 'empty' }, 'None yet.')));
  return [peers, known];
}
