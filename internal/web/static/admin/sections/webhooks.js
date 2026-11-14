// Webhooks: requests waiting, with who asked and for which channel
// (approve or decline); existing webhooks (revoke); and who approves
// requests.

import { get, post, del, all } from '/shared/api.js';
import { h, button, busy, toast, confirmDialog, dateTime, ago } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { people, name, settingsCard } from '../kit.js';

export default async function webhooksSection(ctx) {
  const [hooks, settings] = await Promise.all([all('/admin/webhooks', 'webhooks'), get('/admin/settings').then((r) => r.settings)]);
  await people(hooks.flatMap((w) => [w.requested_by_id, w.decided_by_id]));
  const channels = new Map();
  try { for (const c of await all('/admin/channels', 'channels')) channels.set(c.id, c.name); } catch { /* names fall back */ }
  const where = (w) => channels.get(w.conversation_id) || 'a channel';
  const row = (w, does) => h('li', {},
    h('div', { class: 'what' }, h('strong', {}, w.name, ' ', h('small', { class: 'dim' }, 'into ' + where(w))),
      w.purpose && h('small', {}, '“' + w.purpose + '”'),
      h('small', {}, `Asked for by ${name(w.requested_by_id)} on ${dateTime(w.requested_at)}`
        + (w.decided_at ? ` · ${w.state} by ${name(w.decided_by_id)}` : '') + (w.state === 'approved' ? ` · ${w.has_secret ? 'last used ' + ago(w.last_used_at) : 'address not collected yet'}` : ''))),
    h('div', { class: 'does' }, does));
  const waiting = hooks.filter((w) => w.state === 'waiting');
  const live = hooks.filter((w) => w.state === 'approved');
  const decide = (w, what) => { const b = button(what === 'approve' ? 'Approve' : 'Decline', () => busy(b, async () => { await post(`/webhooks/${w.id}/${what}`); toast(what === 'approve' ? 'Approved.' : 'Declined.'); ctx.redraw(); }), 'small' + (what === 'approve' ? ' primary' : '')); return b; };
  return [
    card('Waiting',
      h('p', { class: 'dim' }, 'A webhook is an address that posts into one named channel and can read nothing. A member asks for one; once approved, they collect the address, once.'),
      waiting.length ? h('ul', { class: 'rows' }, waiting.map((w) => row(w, [decide(w, 'approve'), decide(w, 'decline')]))) : h('p', { class: 'empty' }, 'No requests are waiting.')),
    card('In use', live.length ? h('ul', { class: 'rows' }, live.map((w) => {
      const revoke = button('Revoke', async () => {
        if (!(await confirmDialog(`Revoke “${w.name}”?`, 'Its address stops working at once.', { yes: 'Revoke', danger: true }))) return;
        await busy(revoke, async () => { await del('/webhooks/' + w.id); ctx.redraw(); });
      }, 'small danger');
      return row(w, revoke);
    })) : h('p', { class: 'empty' }, 'No webhooks.')),
    settingsCard('Who approves', null, [
      { key: 'webhooks.managers_may_approve', label: 'Channel managers may approve', type: 'switch', fallback: false, hint: 'Otherwise only admins approve requests.' },
    ], settings),
  ];
}
