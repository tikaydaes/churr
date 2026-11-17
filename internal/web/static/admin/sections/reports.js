// Reports: the reports the reader may handle, made by people or filed by
// the word filter: from public channels, from channels they moderate, and
// those sent to the admins (from a chat, of a person, or about a private
// channel's moderators). Each carries a snapshot of what was reported.
// Dismiss, delete the message, or time the author out.

import { get, post, all } from '/shared/api.js';
import { h, button, busy, toast, dateTime } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { formDialog } from '/shared/form.js';
import { people, name } from '../kit.js';
import { quotedMessage, snapshotView } from '../quoted.js';

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
    await people(items.flatMap((r) => [r.reported_by_id, r.handled_by_id, r.account_id, r.message && r.message.author_id]));
    const rows = [];
    for (const r of items) {
      const snap = r.snapshot || {};
      const where = r.kind === 'account' ? '' : snap.place === 'chat' ? 'a direct or group chat' : r.message ? await channelName(r.conversation_id) : 'a private channel';
      const handle = (outcome, extra) => post(`/reports/${r.id}/handle`, { outcome, ...extra }).then(() => { toast('Handled.'); draw(); }).catch((err) => { if (!err.silent) toast(err.message, true); });
      const dismiss = button('Dismiss', () => busy(dismiss, () => handle('dismissed')), 'small');
      const remove = button('Delete the message', () => busy(remove, () => handle('deleted')), 'small danger');
      // Without the message to hand (a chat, a private channel), deleting
      // is not the console's, and a time-out is on the whole instance.
      const everywhere = !r.message;
      const timeout = button(everywhere ? 'Time them out everywhere' : 'Time the author out', async () => {
        const v = await formDialog(everywhere ? 'Time them out everywhere' : 'Time the author out', [
          { name: 'seconds', label: 'For', type: 'select', value: '3600', options: [['600', '10 minutes'], ['3600', 'An hour'], ['86400', 'A day'], ['604800', 'A week']] },
        ], { yes: 'Time out', intro: everywhere ? 'On the whole instance. They can still read.' : `In ${where}. They can still read.` });
        if (v) handle('timed_out', { timeout_seconds: Number(v.seconds) });
      }, 'small');
      rows.push(h('section', { class: 'console-card', dataset: { report: r.id, kind: r.kind || 'message' } },
        h('p', {}, r.source === 'filter' ? [h('span', { class: 'tag yellow' }, 'word filter'), ' flagged this'] : [h('strong', {}, name(r.reported_by_id)), r.kind === 'account' ? [' reported ', h('strong', {}, name(r.account_id))] : ' reported this'],
          ` on ${dateTime(r.created_at)}`, r.reason ? [': “', r.reason, '”'] : '.', r.to_admins ? [' ', h('span', { class: 'tag lavender' }, 'sent to the admins')] : null),
        r.message ? quotedMessage(r.message, where) : snapshotView(r, where),
        r.handled_at ? h('p', { class: 'dim small' }, `${r.outcome === 'dismissed' ? 'Dismissed' : r.outcome === 'deleted' ? 'Message deleted' : 'Timed out'} by ${name(r.handled_by_id)} on ${dateTime(r.handled_at)}.`)
          : h('p', { class: 'row' }, dismiss, r.message && !r.message.deleted_at ? remove : null, timeout)));
    }
    list.replaceChildren(...(rows.length ? rows : [h('p', { class: 'empty' }, show.value === 'true' ? 'Nothing is waiting.' : 'Nothing has been handled yet.')]));
  }
  await draw();
  return [card(null, h('div', { class: 'filters' }, h('label', { class: 'field narrow' }, h('span', {}, 'Show'), show)),
    h('p', { class: 'dim small' }, 'A private channel’s reports go to its moderators and managers. Reports from direct and group chats, reports of people, and reports about a private channel’s own moderators or managers are sent to the admins: you are shown the reported message as it was (its text, author, time and file names) or the person’s name, status and picture, and nothing else of the conversation.')), list];
}
