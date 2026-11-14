// Sections: the headings channels are listed under. Create, rename,
// reorder.

import { get, post, patch, del } from '/shared/api.js';
import { h, button, busy, toast, confirmDialog, promptDialog, iconButton } from '/shared/dom.js';
import { card } from '/shared/console.js';

export default async function sectionsSection(ctx) {
  const sections = (await get('/sections')).sections.sort((a, b) => a.position - b.position);
  const make = button('New section', async () => {
    const name = await promptDialog('New section', 'Name', { maxLength: 64, yes: 'Create' });
    if (name) { try { await post('/admin/sections', { name, position: sections.length }); ctx.redraw(); } catch (err) { if (!err.silent) toast(err.message, true); } }
  }, 'small primary');
  // Moving a section swaps its position with its neighbour's.
  const move = async (i, by) => {
    const a = sections[i], b = sections[i + by];
    if (!a || !b) return;
    try {
      await patch('/admin/sections/' + a.id, { position: i + by });
      await patch('/admin/sections/' + b.id, { position: i });
      ctx.redraw();
    } catch (err) { toast(err.message, true); }
  };
  return card(h('span', { class: 'row grow between' }, 'Sections', make),
    h('p', { class: 'dim' }, 'Everyone sees the same sections in the same order. A channel in no section is listed under “Channels”. Put a channel in a section under Channels.'),
    sections.length ? h('ol', { class: 'rows' }, sections.map((s, i) => {
      const rename = button('Rename', async () => {
        const name = await promptDialog('Rename section', 'Name', { value: s.name, maxLength: 64 });
        if (name) await busy(rename, async () => { await patch('/admin/sections/' + s.id, { name }); ctx.redraw(); });
      }, 'small');
      const remove = button('Delete', async () => {
        if (!(await confirmDialog(`Delete the section “${s.name}”?`, 'Its channels stay, outside any section.', { yes: 'Delete', danger: true }))) return;
        await busy(remove, async () => { await del('/admin/sections/' + s.id); ctx.redraw(); });
      }, 'small danger');
      const up = iconButton('arrow-up', `Move ${s.name} up`, () => move(i, -1), 'small');
      const down = iconButton('arrow-down', `Move ${s.name} down`, () => move(i, 1), 'small');
      up.disabled = i === 0; down.disabled = i === sections.length - 1;
      return h('li', {}, h('div', { class: 'what' }, h('strong', {}, s.name)), h('div', { class: 'does' }, up, down, rename, remove));
    })) : h('p', { class: 'empty' }, 'No sections. Every channel is listed under “Channels”.'));
}
