// Choosing people and channels by name: one dialog, used wherever someone
// has to be picked. It searches as the person types and shows enough about
// each result to tell one from another.

import { get } from './api.js';
import { h, openDialog, button, avatar, debounce, fill, icon, plural, monthYear } from './dom.js';

// pickAccounts resolves with the chosen accounts (a list), or undefined.
//   multiple   choose several
//   exclude    Set of account ids left out
//   roles      only these roles
//   note       a line shown above the search box
export function pickAccounts(title, opts = {}) {
  return openDialog(title, (close) => {
    const chosen = new Map();
    const input = h('input', { type: 'search', placeholder: 'Search by name', autocomplete: 'off', 'aria-label': 'Search people' });
    const results = h('ul', { class: 'rows pick-list', 'aria-label': 'People found' });
    const picked = h('p', { class: 'row' });
    const done = button(opts.yes || 'Choose', () => close([...chosen.values()]), 'primary');
    const drawPicked = () => {
      fill(picked, [...chosen.values()].map((a) => h('button', { type: 'button', class: 'tag', title: 'Remove', onclick: () => { chosen.delete(a.id); drawPicked(); } }, '@' + a.username + ' ×')));
      done.disabled = chosen.size === 0;
    };
    const search = async () => {
      let accounts = [];
      try { accounts = (await get('/accounts', { q: input.value.trim(), limit: 30 })).accounts; } catch (err) { fill(results, h('li', {}, err.message)); return; }
      accounts = accounts.filter((a) => !(opts.exclude && opts.exclude.has(a.id)) && (!opts.roles || opts.roles.includes(a.role)) && !a.removed_at);
      fill(results, accounts.length ? accounts.map((a) => h('li', {}, h('button', { type: 'button', class: 'pick', onclick: () => {
        if (!opts.multiple) { close([a]); return; }
        chosen.set(a.id, a); drawPicked();
      } }, avatar(a), h('span', { class: 'what' }, h('strong', {}, a.display_name || a.username),
        h('small', {}, '@' + a.username + (a.server_name ? '@' + a.server_name : '') + ' · ' + a.role + (a.dnd ? ' · do not disturb' : ''))))))
        : h('li', { class: 'dim' }, 'Nobody by that name.'));
    };
    input.addEventListener('input', debounce(search, 180));
    search();
    drawPicked();
    return {
      body: [opts.note && h('p', { class: 'note' }, opts.note), input, opts.multiple && picked, results],
      foot: opts.multiple ? [button('Cancel', () => close(undefined)), done] : [button('Cancel', () => close(undefined))],
    };
  });
}

// detailLine is the one line that tells same-named channels apart, built
// in the same order everywhere (docs/CLIENTS.md section 9.26): the parent,
// the section, the member count and whether the reader is in it, up to two
// managers, and the date it was made. look gives what is known:
//   conversation(id), section(id), account(id), managers(conv) → [ids]
export function detailLine(conv, look = {}) {
  const parts = [];
  const parent = conv.parent_id && look.conversation && look.conversation(conv.parent_id);
  if (parent) parts.push((parent.name || 'channel') + ' ›');
  const sectionId = parent ? parent.section_id : conv.section_id;
  const section = sectionId && look.section && look.section(sectionId);
  if (section) parts.push(section.name);
  if (conv.member_count != null) parts.push(plural(conv.member_count, 'member') + (conv.membership ? ', you’re in it' : ''));
  const managers = ((look.managers && look.managers(conv)) || conv.manager_ids || []).slice(0, 2)
    .map((id) => look.account && look.account(id)).filter(Boolean).map((a) => a.display_name || a.username);
  if (managers.length) parts.push('managed by ' + managers.join(', '));
  if (conv.created_at) parts.push('since ' + monthYear(conv.created_at));
  return parts.join(' · ');
}

export function channelIcon(conv, cls) {
  return icon(conv.kind === 'sealed' ? 'sealed' : conv.locked_at ? 'padlock' : conv.private ? 'lock' : 'hash', null, cls);
}

// pickChannel resolves with one channel, or undefined. channels is the
// list to choose from; look is as for detailLine.
export function pickChannel(title, channels, look = {}, opts = {}) {
  return openDialog(title, (close) => {
    const input = h('input', { type: 'search', placeholder: 'Search channels', autocomplete: 'off', 'aria-label': 'Search channels' });
    const results = h('ul', { class: 'rows pick-list', 'aria-label': 'Channels' });
    const draw = () => {
      const q = input.value.trim().toLowerCase();
      const list = channels.filter((c) => !q || (c.name || '').toLowerCase().includes(q)).slice(0, 60);
      fill(results, list.length ? list.map((c) => h('li', {}, h('button', { type: 'button', class: 'pick', onclick: () => close(c) },
        channelIcon(c), h('span', { class: 'what' }, h('strong', {}, h('bdi', {}, c.name || 'chat')), h('small', {}, detailLine(c, look))))))
        : h('li', { class: 'dim' }, 'No channel by that name.'));
    };
    input.addEventListener('input', draw);
    draw();
    return { body: [opts.note && h('p', { class: 'note' }, opts.note), input, results], foot: [opts.none && button(opts.none, () => close(null)), button('Cancel', () => close(undefined))] };
  });
}
