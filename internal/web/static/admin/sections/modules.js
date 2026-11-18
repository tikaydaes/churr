// Modules: the built-in modules (docs/DESIGN-MODULES.md section 6.5), each
// with what it declares and a switch for the whole server. Off, its commands
// do nothing anywhere and what it made shows as its text; a channel's
// managers may also switch it off in their channel.

import { get, put } from '/shared/api.js';
import { h, toast } from '/shared/dom.js';
import { card } from '/shared/console.js';

export default async function modules(ctx) {
  const mods = (await get('/admin/modules')).modules;
  const row = (m) => {
    const sw = h('input', { type: 'checkbox', class: 'switch', 'aria-label': m.title, checked: m.on, onchange: async (e) => {
      const on = e.target.checked;
      try { await put('/admin/modules/' + m.name, { on }); toast(on ? `${m.title} is on.` : `${m.title} is off on this server.`); } catch (err) { if (!err.silent) toast(err.message, true); }
      ctx.redraw();
    } });
    const declares = [
      ...m.commands.map((c) => h('span', {}, h('code', {}, '/' + c.name + (c.hint ? ' ' + c.hint : '')), ' ', c.summary)),
      ...m.claims.map((k) => h('span', {}, 'Reads ', h('code', {}, k), ' in messages as they are sent')),
    ];
    return h('div', { class: 'setting' }, h('div', { class: 'what' }, h('strong', {}, m.title, ' ', h('code', { class: 'dim' }, m.name)),
      ...declares.map((d) => h('small', { class: 'dim' }, d))), h('div', { class: 'control' }, sw));
  };
  return card('Built-in modules',
    h('p', { class: 'dim' }, 'Parts of Churr that answer commands and draw cards. Off, a module’s commands do nothing and what it made shows as its text; on again, its cards come back. A module that fails is switched off and the audit log says so.'),
    mods.length ? mods.map(row) : h('p', { class: 'empty' }, 'This server has no built-in modules.'));
}
