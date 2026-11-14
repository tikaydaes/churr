// Groups: create, edit handle, name, colour and whether it is open;
// members; map to a provider group. The two built-in groups are shown
// read-only.

import { get, post, patch, put, del } from '/shared/api.js';
import { h, button, busy, toast, confirmDialog, openDialog, avatar, fill, plural } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { formDialog } from '/shared/form.js';
import { pickAccounts } from '/shared/pick.js';
import { people, COLOURS } from '../kit.js';

export default async function groupsSection(ctx) {
  const [groups, providers] = await Promise.all([get('/groups').then((r) => r.groups), get('/admin/sso').then((r) => r.providers).catch(() => [])]);
  const fields = (g) => [
    { name: 'handle', label: 'Handle', required: true, code: true, value: g && g.handle, minLength: 2, maxLength: 32, hint: 'People mention the group as @handle. Lowercase letters, digits, dots, dashes, underscores.' },
    { name: 'name', label: 'Name', required: true, value: g && g.name, maxLength: 64 },
    { name: 'description', label: 'What it is for', value: g && g.description, maxLength: 200 },
    { name: 'color', label: 'Colour of its label', type: 'select', value: (g && g.color) || '', options: [['', 'None'], ...COLOURS.map((c) => [c, c])] },
    { name: 'open', label: 'Open', type: 'checkbox', value: g && g.open, hint: 'Members may join and leave it themselves.' },
    providers.length && { name: 'provider_id', label: 'Fill it from a provider', type: 'select', value: (g && g.provider_id) || '', options: [['', 'No: members are added here'], ...providers.map((p) => [p.id, p.name])] },
    providers.length && { name: 'provider_group', label: 'The provider’s group', code: true, value: g && g.provider_group, when: (v) => !!v.provider_id, hint: 'Whoever the provider lists in this group is in the Churr group, as of their last sign-in.' },
  ];
  const clean = (v) => ({ handle: v.handle, name: v.name, description: v.description, color: v.color || undefined, open: v.open,
    provider_id: v.provider_id || undefined, provider_group: v.provider_id ? v.provider_group : undefined });

  const members = (g) => openDialog('Members of @' + g.handle, (close) => {
    const list = h('ul', { class: 'rows' });
    const fixed = !!g.builtin || !!g.provider_id;
    const load = async () => {
      const ids = (await get(`/groups/${g.id}/members`, { limit: 200 })).account_ids;
      const known = await people(ids);
      fill(list, ids.length ? ids.map((id) => {
        const a = known.get(id) || { id, username: '?', display_name: 'Someone' };
        const rm = button('Take out', () => busy(rm, async () => { await del(`/admin/groups/${g.id}/members/${id}`); load(); }), 'small danger');
        return h('li', {}, avatar(a), h('div', { class: 'what' }, h('strong', {}, a.display_name || a.username), h('small', {}, '@' + a.username)), !fixed && h('div', { class: 'does' }, rm));
      }) : h('li', { class: 'dim' }, 'Nobody is in this group.'));
      return new Set(ids);
    };
    let current = new Set();
    load().then((s) => { current = s; }).catch((err) => toast(err.message, true));
    const add = button('Add people', async () => {
      const chosen = await pickAccounts('Add to @' + g.handle, { multiple: true, exclude: current, yes: 'Add' });
      if (!chosen) return;
      for (const a of chosen) { try { await put(`/admin/groups/${g.id}/members/${a.id}`); } catch (err) { toast(`${a.username}: ${err.message}`, true); } }
      current = await load();
    }, 'primary');
    return { body: [fixed && h('p', { class: 'note' }, g.builtin ? 'A built-in group keeps itself.' : 'This group is filled from the provider, so nobody is added here.'), list], foot: [!fixed && add, button('Done', () => close(true))] };
  }, { wide: true });

  const make = button('New group', async () => {
    if (await formDialog('New group', fields(null), { yes: 'Create', submit: (v) => post('/admin/groups', clean(v)) })) ctx.redraw();
  }, 'small primary');
  return card(h('span', { class: 'row grow between' }, 'Groups', make),
    h('p', { class: 'dim' }, 'A group is a set of people that can be mentioned by its handle and added to a channel as one.'),
    h('ul', { class: 'rows' }, groups.map((g) => {
      const edit = button('Change', async () => { if (await formDialog('Change @' + g.handle, fields(g), { submit: (v) => patch('/admin/groups/' + g.id, clean(v)) })) ctx.redraw(); }, 'small');
      const remove = button('Delete', async () => {
        if (!(await confirmDialog(`Delete @${g.handle}?`, 'Its members leave the channels they were in only through it. Mentions of it in old messages stay as written.', { yes: 'Delete', danger: true }))) return;
        await busy(remove, async () => { await del('/admin/groups/' + g.id); ctx.redraw(); });
      }, 'small danger');
      return h('li', {},
        h('div', { class: 'what' },
          h('strong', {}, g.color && h('span', { class: 'swatch ' + g.color }), '@' + g.handle, ' ', g.builtin && h('span', { class: 'tag line' }, 'built in'), ' ',
            g.open && h('span', { class: 'tag cyan' }, 'open'), ' ', g.provider_id && h('span', { class: 'tag yellow' }, 'from provider')),
          h('small', {}, [g.name, g.description, plural(g.member_count, 'member'), g.provider_group && 'provider group “' + g.provider_group + '”'].filter(Boolean).join(' · '))),
        h('div', { class: 'does' }, button('Members', () => members(g).then(() => ctx.redraw()), 'small'), !g.builtin && edit, !g.builtin && remove));
    })));
}
