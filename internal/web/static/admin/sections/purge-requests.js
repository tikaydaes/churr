// Purge requests: an author has asked for a message's history to be
// erased from a public channel. Until an admin decides, the history is
// hidden from everyone but admins. Show it again, keep it hidden, or erase.

import { get, post, all } from '/shared/api.js';
import { h, button, busy, toast, openDialog, dateTime, confirmDialog } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { renderMarkdown } from '/shared/md/render.js';
import { people, name } from '../kit.js';
import { quotedMessage } from '../quoted.js';

const SAID = { shown: 'History shown again', hidden: 'Kept hidden', erased: 'Erased' };

export default async function purgeRequests(ctx) {
  const show = h('select', { 'aria-label': 'Which requests', onchange: () => draw() }, h('option', { value: 'true' }, 'Waiting'), h('option', { value: 'false' }, 'Decided'));
  const list = h('div', { class: 'stack' });
  const history = (m) => openDialog('History of the message', (close) => {
    const body = h('div', { class: 'stack' }, h('p', { class: 'loading' }, 'Loading'));
    get(`/messages/${m.id}/history`).then(async ({ versions }) => {
      await people(versions.map((v) => v.replaced_by_id));
      body.replaceChildren(...(versions.length ? versions.map((v) => h('div', { class: 'quoted-message' },
        h('div', { class: 'by' }, `Version ${v.version} · ${v.reason === 'delete' ? 'deleted' : 'replaced'} by ${name(v.replaced_by_id)} on ${dateTime(v.replaced_at)}`),
        renderMarkdown(v.text, v.entities, {}))) : [h('p', { class: 'dim' }, 'There is no history left.')]));
    }).catch((err) => body.replaceChildren(h('p', { class: 'problem' }, err.message)));
    return { body, foot: [button('Close', () => close(true))] };
  }, { wide: true });
  async function draw() {
    let items;
    try { items = await all('/admin/purge-requests', 'purge_requests', { waiting: show.value }); } catch (err) { list.replaceChildren(h('p', { class: 'problem' }, err.message)); return; }
    await people(items.flatMap((r) => [r.requested_by_id, r.decided_by_id, r.message && r.message.author_id]));
    const decide = (r, outcome) => post(`/admin/purge-requests/${r.id}/decide`, { outcome }).then(() => { toast(SAID[outcome] + '.'); draw(); }).catch((err) => { if (!err.silent) toast(err.message, true); });
    list.replaceChildren(...(items.length ? items.map((r) => {
      const shown = button('Show the history again', () => busy(shown, () => decide(r, 'shown')), 'small');
      const hidden = button('Keep it hidden', () => busy(hidden, () => decide(r, 'hidden')), 'small');
      const erase = button('Erase', async () => {
        if (await confirmDialog('Erase the history?', 'The earlier versions are deleted from the database, the search index and the file store. This cannot be undone.', { yes: 'Erase', danger: true })) decide(r, 'erased');
      }, 'small danger');
      return h('section', { class: 'console-card' },
        h('p', {}, h('strong', {}, name(r.requested_by_id)), ` asked on ${dateTime(r.requested_at)} for this message’s history to be erased.`),
        quotedMessage(r.message),
        h('p', { class: 'row' }, r.message && button('Read the history', () => history(r.message), 'small'),
          ...(r.decided_at ? [h('span', { class: 'dim small' }, `${SAID[r.outcome] || r.outcome} by ${name(r.decided_by_id)} on ${dateTime(r.decided_at)}.`)] : [shown, hidden, erase])));
    }) : [h('p', { class: 'empty' }, show.value === 'true' ? 'Nothing is waiting.' : 'Nothing has been decided yet.')]));
  }
  await draw();
  return [card(null, h('div', { class: 'filters' }, h('label', { class: 'field narrow' }, h('span', {}, 'Show'), show)),
    h('p', { class: 'dim small' }, 'The author is not told what you decide. “Show again” stops them asking again until the message changes. In private and ad-hoc channels a purge happens at once and never comes here.')), list];
}
