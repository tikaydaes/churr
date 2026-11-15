// Channels: every public channel, and every named private channel with
// its name, members and managers only. An admin can appoint a manager,
// archive, set a retention period, move a channel to a section or under a
// parent, make a public channel private and lock a named private channel.
// The last two are one-way and say so. Ad-hoc channels and sealed chats
// never appear here.

import { get, post, patch, put, del, all } from '/shared/api.js';
import { h, button, busy, toast, confirmDialog, openDialog, openMenu, avatar, fill, debounce, plural, day, duration } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { formDialog } from '/shared/form.js';
import { channelIcon } from '/shared/pick.js';
import { people, name } from '../kit.js';

const RETENTION = [['0', 'The server’s period'], ['86400', 'A day'], ['604800', 'A week'], ['2592000', '30 days'], ['7776000', '90 days'], ['31536000', 'A year']];

export default async function channelsSection(ctx) {
  const sections = (await get('/sections')).sections;
  const q = h('input', { type: 'search', placeholder: 'Channel name', autocomplete: 'off' });
  const kind = h('select', {}, [['', 'Public and private'], ['false', 'Public'], ['true', 'Private']].map(([v, t]) => h('option', { value: v }, t)));
  const archived = h('select', {}, [['false', 'In use'], ['true', 'Archived']].map(([v, t]) => h('option', { value: v }, t)));
  const list = h('ul', { class: 'rows' });
  let channels = [];

  const reload = async () => {
    try {
      channels = await all('/admin/channels', 'channels', { q: q.value.trim(), private: kind.value || undefined, archived: archived.value });
      await people(channels.flatMap((c) => c.manager_ids));
    } catch (err) { fill(list, h('li', {}, err.message)); return; }
    // Sub-channels are listed under their parent, as in the sidebar.
    const byParent = new Map();
    const ids = new Set(channels.map((c) => c.id));
    for (const c of channels) { const p = c.parent_id && ids.has(c.parent_id) ? c.parent_id : ''; byParent.set(p, [...(byParent.get(p) || []), c]); }
    const sort = (xs) => xs.sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }) || a.created_at.localeCompare(b.created_at));
    const rows = [];
    for (const c of sort(byParent.get('') || [])) { rows.push(row(c, false)); for (const k of sort(byParent.get(c.id) || [])) rows.push(row(k, true)); }
    fill(list, rows.length ? rows : h('li', { class: 'dim' }, 'No channel matches.'));
  };

  const run = async (fn, said) => { try { await fn(); if (said) toast(said); reload(); } catch (err) { if (!err.silent) toast(err.message, true); } };

  const membersDialog = (c) => openDialog(`Members of ${c.name}`, (close) => {
    const body = h('ul', { class: 'rows' });
    const load = async () => {
      const out = await get('/admin/channels/' + c.id);
      const known = await people(out.members.map((m) => m.account_id));
      fill(body, out.members.map((m) => {
        const a = known.get(m.account_id) || { id: m.account_id, username: '?' };
        const appoint = button('Appoint as manager', () => busy(appoint, async () => { await put(`/admin/channels/${c.id}/managers/${m.account_id}`); toast('Appointed.'); load(); }), 'small');
        return h('li', {}, avatar(a), h('div', { class: 'what' }, h('strong', {}, a.display_name || a.username), h('small', {}, '@' + a.username + (m.via_group ? ' · here through a group' : ''))),
          h('span', { class: 'tag ' + (m.level === 'manager' ? 'lavender' : 'line') }, m.level), m.level !== 'manager' && h('div', { class: 'does' }, appoint));
      }));
    };
    load().catch((err) => fill(body, h('li', {}, err.message)));
    return { body: [c.private && h('p', { class: 'note' }, 'This channel is private. An admin sees who is in it and who manages it, and nothing that was said. Appointing a manager from among its members is the one change an admin can make to them.'), body],
      foot: [button('Done', () => close(true))] };
  }, { wide: true });

  const menu = (c, anchor) => openMenu(anchor, [
    { head: c.name },
    { label: 'Members and managers', onclick: () => membersDialog(c).then(reload) },
    { label: 'Section and parent…', onclick: async () => {
      const tops = channels.filter((x) => !x.parent_id && x.id !== c.id && !x.archived_at);
      const out = await formDialog(`Where ${c.name} is listed`, [
        { name: 'section_id', label: 'Section', type: 'select', value: c.section_id || '', options: [['', 'None'], ...sections.map((s) => [s.id, s.name])], hint: 'Ignored while the channel has a parent: it is listed in its parent’s section.' },
        { name: 'parent_id', label: 'Parent channel', type: 'select', value: c.parent_id || '', options: [['', 'None: a top-level channel'], ...tops.map((x) => [x.id, x.name + (x.private ? ' (private)' : '')])],
          hint: 'One level only. This changes where the channel is listed and nothing else: not who is in it, not whether it is private.' },
      ], { submit: (v) => patch('/admin/channels/' + c.id, { section_id: v.section_id || undefined, parent_id: v.parent_id || null }) });
      if (out) { toast('Saved.'); reload(); }
    } },
    { label: 'Retention period…', onclick: async () => {
      const cur = String(c.retention_seconds || 0);
      const out = await formDialog(`How long ${c.name} keeps messages`, [
        { name: 'seconds', label: 'Keep messages for', type: 'select', value: cur, options: RETENTION.some((r) => r[0] === cur) ? RETENTION : [...RETENTION, [cur, duration(Number(cur))]] },
      ], { intro: 'Messages older than this are removed from the channel for good.', submit: (v) => patch('/admin/channels/' + c.id, { retention_seconds: Number(v.seconds) }) });
      if (out) { toast('Saved.'); reload(); }
    } },
    c.archived_at
      ? { label: 'Unarchive', onclick: () => run(() => del(`/admin/channels/${c.id}/archive`), 'Unarchived.') }
      : { label: 'Archive', onclick: async () => {
        if (await confirmDialog(`Archive ${c.name}?`, 'It can still be read and searched, and takes no more posts.', { yes: 'Archive' })) run(() => post(`/admin/channels/${c.id}/archive`), 'Archived.');
      } },
    // An admin acts as manager of a public channel. A private one is
    // converted by its own managers, in the chat.
    !c.private && !c.shared && (c.announcement
      ? { label: 'Make it an ordinary channel', onclick: () => run(() => patch('/conversations/' + c.id, { announcement: false }), 'Everyone at level member posts there again.') }
      : { label: 'Make it an announcement channel…', onclick: async () => {
        if (await confirmDialog(`Make ${c.name} an announcement channel?`, [
          'Only its managers and the people they allow will post. Everyone else reads and reacts.',
          'Who is in the channel is then shown to its managers only, and nobody is shown typing or online there. Nobody’s level changes, and this can be undone.'], { yes: 'Make it one' })) run(() => patch('/conversations/' + c.id, { announcement: true }), 'It is an announcement channel.');
      } }),
    { sep: true },
    !c.private && { label: 'Make private…', danger: true, onclick: async () => {
      const yes = await confirmDialog(`Make ${c.name} private?`, [
        'People who are not members lose the channel at once: its history and its search results too.',
        'A private channel can never be made public again.'], { type: c.name, yes: 'Make it private', danger: true });
      if (yes) run(() => patch('/conversations/' + c.id, { private: true }), 'The channel is private.');
    } },
    c.private && { label: 'Lock…', danger: true, onclick: async () => {
      const yes = await confirmDialog(`Lock ${c.name}?`, [
        'After locking nobody can be added or join, and a group attached to the channel brings in nobody new.',
        'People can still leave, and someone who leaves cannot come back.',
        'Locking cannot be undone.'], { type: 'lock', yes: 'Lock it', danger: true });
      if (yes) run(() => post(`/admin/channels/${c.id}/lock`), 'The channel is locked.');
    } },
  ], { alignRight: true, label: 'Actions for ' + c.name });

  function row(c, child) {
    const section = sections.find((s) => s.id === c.section_id);
    const more = h('button', { type: 'button', class: 'btn small', onclick: (e) => menu(c, e.currentTarget) }, 'Actions');
    return h('li', { class: child ? 'tree-child' : '' }, channelIcon(c),
      h('div', { class: 'what' },
        h('strong', {}, h('bdi', {}, c.name), ' ', h('span', { class: 'tag ' + (c.private ? 'pink' : 'line') }, c.private ? 'private' : 'public'), ' ',
          c.announcement && h('span', { class: 'tag lavender' }, 'announcements'), ' ',
          c.archived_at && h('span', { class: 'tag grey' }, 'archived'), ' ', c.shared && h('span', { class: 'tag cyan' }, 'shared')),
        !c.private && c.topic && h('small', {}, c.topic),
        h('small', {}, [plural(c.member_count, 'member'), c.manager_ids.length ? 'managed by ' + c.manager_ids.slice(0, 3).map(name).join(', ') : 'no manager',
          section && !child && 'in ' + section.name, c.retention_seconds ? 'keeps ' + duration(c.retention_seconds) : null, 'since ' + day(c.created_at)].filter(Boolean).join(' · '))),
      h('div', { class: 'does' }, more));
  }

  q.addEventListener('input', debounce(reload, 200));
  kind.addEventListener('change', reload);
  archived.addEventListener('change', reload);
  // The one kind of channel made here: a place for the admins to announce.
  const create = button('New announcement channel', async () => {
    const out = await formDialog('New announcement channel', [
      { name: 'name', label: 'Name', required: true, maxLength: 64 },
      { name: 'private', label: 'Private', type: 'checkbox', hint: 'Only people who are added can see it. A private channel can never be made public.' },
      { name: 'announcement_replies', label: 'Replies in threads', type: 'checkbox', hint: 'Let members who may not post reply in threads.' },
    ], { yes: 'Create', intro: 'Only its managers and the people they allow post. You are its first manager.',
      submit: (v) => post('/channels', { name: v.name, private: !!v.private, announcement: true, announcement_replies: !!v.announcement_replies }) });
    if (out) { toast('Created.'); reload(); }
  }, 'small');
  await reload();
  return card(null,
    h('div', { class: 'filters' },
      h('label', { class: 'field' }, h('span', {}, 'Search'), q),
      h('label', { class: 'field narrow' }, h('span', {}, 'Kind'), kind),
      h('label', { class: 'field narrow' }, h('span', {}, 'Show'), archived),
      create),
    h('p', { class: 'dim small' }, 'Names are not unique: the section, the parent, the managers and the date tell same-named channels apart. Other channels are made in the chat.'),
    list);
}
