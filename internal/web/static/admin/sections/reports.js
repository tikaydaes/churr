// Reports: open reports from public channels, made by people or filed by
// the word filter. Dismiss, delete the message, or time the author out.

import { get, post, all } from '/shared/api.js';
import { h, button, busy, toast, dateTime } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { formDialog } from '/shared/form.js';
import { people, name } from '../kit.js';
import { quotedMessage } from '../quoted.js';

export default async function reports(ctx) {
  const show = h('select', { 'aria-label': 'Which reports', onchange: () => draw() }, h('option', { value: 'true' }, 'Waiting'), h('option', { value: 'false' }, 'Handled'));
  const list = h('div', { class: 'stack' });
  const channels = new Map();
  const channelName = async (id) => {
    if (!channels.has(id)) { try { channels.set(id, (await get('/conversations/' + id)).conversation.name || 'a chat'); } catch { channels.set(id, 'a channel you cannot read'); } }
    return channels.get(id);
  };
  async function draw() {
    let items;
    try { items = await all('/reports', 'reports', { waiting: show.value }); } catch (err) { list.replaceChildren(h('p', { class: 'problem' }, err.message)); return; }
    await people(items.flatMap((r) => [r.reported_by_id, r.handled_by_id, r.message && r.message.author_id]));
    const rows = [];
    for (const r of items) {
      const where = await channelName(r.conversation_id);
      const handle = (outcome, extra) => post(`/reports/${r.id}/handle`, { outcome, ...extra }).then(() => { toast('Handled.'); draw(); }).catch((err) => { if (!err.silent) toast(err.message, true); });
      const dismiss = button('Dismiss', () => busy(dismiss, () => handle('dismissed')), 'small');
      const remove = button('Delete the message', () => busy(remove, () => handle('deleted')), 'small danger');
      const timeout = button('Time the author out', async () => {
        const v = await formDialog('Time the author out', [
          { name: 'seconds', label: 'For', type: 'select', value: '3600', options: [['600', '10 minutes'], ['3600', 'An hour'], ['86400', 'A day'], ['604800', 'A week']] },
        ], { yes: 'Time out', intro: `In ${where}. They can still read.` });
        if (v) handle('timed_out', { timeout_seconds: Number(v.seconds) });
      }, 'small');
      rows.push(h('section', { class: 'console-card' },
        h('p', {}, r.source === 'filter' ? [h('span', { class: 'tag yellow' }, 'word filter'), ' flagged this'] : [h('strong', {}, name(r.reported_by_id)), ' reported this'],
          ` on ${dateTime(r.created_at)}`, r.reason ? [': “', r.reason, '”'] : '.'),
        quotedMessage(r.message, where),
        r.handled_at ? h('p', { class: 'dim small' }, `${r.outcome === 'dismissed' ? 'Dismissed' : r.outcome === 'deleted' ? 'Message deleted' : 'Author timed out'} by ${name(r.handled_by_id)} on ${dateTime(r.handled_at)}.`)
          : h('p', { class: 'row' }, dismiss, remove, timeout)));
    }
    list.replaceChildren(...(rows.length ? rows : [h('p', { class: 'empty' }, show.value === 'true' ? 'Nothing is waiting.' : 'Nothing has been handled yet.')]));
  }
  await draw();
  return [card(null, h('div', { class: 'filters' }, h('label', { class: 'field narrow' }, h('span', {}, 'Show'), show)),
    h('p', { class: 'dim small' }, 'Reports from private and ad-hoc channels go to those channels’ moderators and managers only, and are not listed here unless you are one of them.')), list];
}
