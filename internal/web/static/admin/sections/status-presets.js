// Status presets: the community's ready-made statuses, offered to
// everyone in the status picker.

import { get, post, patch, del } from '/shared/api.js';
import { h, button, busy, confirmDialog, duration } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { formDialog } from '/shared/form.js';

const LASTS = [['', 'Until cleared'], ['1800', '30 minutes'], ['3600', 'An hour'], ['14400', '4 hours'], ['86400', 'A day'], ['604800', 'A week']];

export default async function statusPresets(ctx) {
  const presets = (await get('/status-presets')).presets.filter((p) => p.community).sort((a, b) => a.position - b.position);
  const fields = (p) => [
    { name: 'emoji', label: 'Emoji', value: p && p.emoji, maxLength: 40, hint: 'One emoji, or :name: for a custom one.' },
    { name: 'text', label: 'Text', required: true, value: p && p.text, maxLength: 100 },
    { name: 'duration', label: 'Lasts', type: 'select', value: p && p.duration_seconds ? String(p.duration_seconds) : '', options: LASTS },
    { name: 'pause_notifications', label: 'Pauses notifications', type: 'checkbox', value: p && p.pause_notifications },
    { name: 'appear_away', label: 'Appears away', type: 'checkbox', value: p && p.appear_away },
    { name: 'availability', label: 'Also sets availability', type: 'select', value: (p && p.availability) || '', options: [['', 'No'], ['meeting', 'In a meeting'], ['out_of_office', 'Out of office']] },
  ];
  const clean = (v) => ({ emoji: v.emoji || undefined, text: v.text, duration_seconds: v.duration ? Number(v.duration) : undefined,
    pause_notifications: v.pause_notifications, appear_away: v.appear_away, availability: v.availability || undefined });
  const add = button('New preset', async () => { if (await formDialog('New status preset', fields(null), { yes: 'Create', submit: (v) => post('/admin/status-presets', { ...clean(v), position: presets.length }) })) ctx.redraw(); }, 'small primary');
  return card(h('span', { class: 'row grow between' }, 'Community status presets', add),
    presets.length ? h('ul', { class: 'rows' }, presets.map((p) => {
      const edit = button('Change', async () => { if (await formDialog('Change preset', fields(p), { submit: (v) => patch('/admin/status-presets/' + p.id, clean(v)) })) ctx.redraw(); }, 'small');
      const remove = button('Delete', async () => {
        if (!(await confirmDialog('Delete this preset?', `“${p.text}” is no longer offered. Statuses already set from it stay.`, { yes: 'Delete', danger: true }))) return;
        await busy(remove, async () => { await del('/admin/status-presets/' + p.id); ctx.redraw(); });
      }, 'small danger');
      return h('li', {}, h('div', { class: 'what' }, h('strong', {}, p.emoji ? p.emoji + ' ' : '', p.text),
        h('small', {}, [p.duration_seconds ? 'for ' + duration(p.duration_seconds) : 'until cleared', p.pause_notifications && 'pauses notifications', p.appear_away && 'appears away',
          p.availability && (p.availability === 'meeting' ? 'in a meeting' : 'out of office')].filter(Boolean).join(' · '))), h('div', { class: 'does' }, edit, remove));
    })) : h('p', { class: 'empty' }, 'No community presets. People can still write a status of their own.'));
}
