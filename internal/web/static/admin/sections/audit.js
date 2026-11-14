// Audit log: every admin and owner action, filtered by actor, action and
// date.

import { get } from '/shared/api.js';
import { h, button, toast, dateTime, fill, fromLocalInput } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { pickAccounts } from '/shared/pick.js';
import { people, name, pager } from '../kit.js';

export default async function audit(ctx) {
  let actor = null;
  const actorBtn = button('Anyone', async () => {
    const chosen = await pickAccounts('Whose actions', { yes: 'Show' });
    if (chosen) { actor = chosen[0]; actorBtn.textContent = '@' + actor.username; clear.hidden = false; reload(); }
  });
  const clear = button('Clear', () => { actor = null; actorBtn.textContent = 'Anyone'; clear.hidden = true; reload(); }, 'small quiet');
  clear.hidden = true;
  const action = h('input', { placeholder: 'for example invite.created', autocomplete: 'off', spellcheck: false, onchange: () => reload() });
  const since = h('input', { type: 'datetime-local', onchange: () => reload() });
  const until = h('input', { type: 'datetime-local', onchange: () => reload() });
  const body = h('tbody', {});
  const holder = h('div', {});
  const detail = (d) => Object.entries(d || {}).map(([k, v]) => `${k}: ${typeof v === 'object' ? JSON.stringify(v) : v}`).join('\n');
  function reload() {
    fill(body);
    const p = pager(async (cursor) => {
      const out = await get('/admin/audit', { actor_id: actor && actor.id, action: action.value.trim(), since: fromLocalInput(since.value), until: fromLocalInput(until.value), limit: 100, cursor });
      await people(out.entries.map((e) => e.actor_id));
      return { items: out.entries, next: out.next_cursor };
    }, (entries) => {
      for (const e of entries) {
        body.append(h('tr', {},
          h('td', { class: 'mono' }, dateTime(e.at)),
          h('td', {}, e.actor_kind === 'account' ? name(e.actor_id) : h('span', { class: 'tag line' }, e.actor_kind)),
          h('td', {}, h('code', {}, e.action)),
          h('td', {}, e.target_kind ? h('span', { class: 'dim small' }, e.target_kind) : '', h('div', { class: 'audit-detail' }, detail(e.detail)))));
      }
      if (!body.children.length) body.append(h('tr', {}, h('td', { colSpan: 4, class: 'dim' }, 'Nothing matches.')));
    });
    holder.replaceChildren(p.node);
    p.first().catch((err) => toast(err.message, true));
  }
  reload();
  return card(null,
    h('div', { class: 'filters' },
      h('div', { class: 'field narrow' }, h('span', {}, 'By'), h('span', { class: 'row' }, actorBtn, clear)),
      h('label', { class: 'field' }, h('span', {}, 'Action'), action),
      h('label', { class: 'field narrow' }, h('span', {}, 'Since'), since),
      h('label', { class: 'field narrow' }, h('span', {}, 'Until'), until)),
    h('div', { class: 'table-wrap' }, h('table', { class: 'data' },
      h('thead', {}, h('tr', {}, h('th', { scope: 'col' }, 'When'), h('th', { scope: 'col' }, 'Who'), h('th', { scope: 'col' }, 'What'), h('th', { scope: 'col' }, 'Detail'))), body)),
    holder);
}
