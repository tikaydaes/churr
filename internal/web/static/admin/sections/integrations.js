// Server integrations: create, tick what it may do, show the token once,
// last used, revoke.

import { get, post, del } from '/shared/api.js';
import { h, button, busy, confirmDialog, dateTime, ago } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { formDialog, secretDialog } from '/shared/form.js';
import { people, name } from '../kit.js';

export default async function integrations(ctx) {
  const list = (await get('/admin/integrations')).integrations;
  await people(list.map((i) => i.created_by_id));
  const add = button('New integration', async () => {
    const out = await formDialog('New integration', [
      { name: 'name', label: 'Name', required: true, maxLength: 60, placeholder: 'Calendar' },
      { name: 'allows', label: 'It may', type: 'checks', value: ['availability'], options: [['availability', 'Set people’s availability', 'In a meeting or out of office, with an end time. For a calendar.']] },
    ], { yes: 'Create', intro: 'An integration is in no conversation and can read no messages. It can do only what is ticked.',
      submit: (v) => { if (!v.allows.length) throw new Error('Tick at least one thing it may do.'); return post('/admin/integrations', { name: v.name, allows: v.allows }); } });
    if (out) { await secretDialog('Integration token', [['Token for ' + out.integration.name, out.secret]], 'Shown once. A lost token is replaced by making a new integration.'); ctx.redraw(); }
  }, 'small primary');
  return card(h('span', { class: 'row grow between' }, 'Server integrations', add),
    list.length ? h('ul', { class: 'rows' }, list.map((i) => {
      const revoke = button('Revoke', async () => {
        if (!(await confirmDialog(`Revoke ${i.name}?`, 'Its token stops working at once.', { yes: 'Revoke', danger: true }))) return;
        await busy(revoke, async () => { await del('/admin/integrations/' + i.id); ctx.redraw(); });
      }, 'small danger');
      return h('li', {}, h('div', { class: 'what' }, h('strong', {}, i.name, ' ', i.revoked_at && h('span', { class: 'tag grey' }, 'revoked')),
        h('small', {}, 'May: ' + i.allows.join(', ')), h('small', {}, `Made by ${name(i.created_by_id)} on ${dateTime(i.created_at)} · last used ${ago(i.last_used_at)}`)),
      !i.revoked_at && h('div', { class: 'does' }, revoke));
    })) : h('p', { class: 'empty' }, 'No integrations.'));
}
