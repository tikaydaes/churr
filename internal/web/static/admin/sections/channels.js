// Channels: every public channel, and every named private channel with
// its name, members and managers only. An admin moves a channel to a
// section or under a parent and sets its retention period; nothing else:
// an admin's role makes nobody a manager (ruling R99). An owner also
// appoints a manager, in a channel they need not be in, and adds someone
// to a private channel by owner override, which every member is shown.
// Ad-hoc channels and sealed chats never appear here.

import { get, post, patch, put, all } from '/shared/api.js';
import { h, button, busy, toast, confirmDialog, openDialog, openMenu, avatar, fill, debounce, plural, day, duration } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { formDialog } from '/shared/form.js';
import { channelIcon, pickAccounts } from '/shared/pick.js';
import { ownerOverrideWarning, levelTag } from '/shared/roles.js';
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

  const isOwner = ctx.me.role === 'owner';
  const who = (a) => a.display_name || a.username;
  // closed: a private channel that takes no requests, or a locked one.
  // Someone not in it is appointed only after being added by owner
  // override. A knock channel that is not locked adds them directly.
  const closed = (c) => (c.private && !c.knock) || !!c.locked_at;

  // appoint makes someone a manager of c, as an owner (appointManager). In
  // a public or knock channel they join with it; in a private or locked
  // one they must be in it, and the server answers conflict for anyone
  // else: the owner is then asked whether to add them by owner override
  // first, with what that means.
  const appoint = async (c, a) => {
    try {
      await put(`/admin/channels/${c.id}/managers/${a.id}`, {});
      toast(`${who(a)} is a manager of ${c.name}.`);
      return true;
    } catch (err) {
      if (err.code !== 'conflict' || !closed(c)) { if (!err.silent) toast(err.message, true); return false; }
      const yes = await confirmDialog(`Add ${who(a)} to ${c.name} by owner override?`, [
        err.message, ...ownerOverrideWarning(c.name, who(a)), `They are then made its manager.`], { type: c.name, yes: 'Add and appoint', danger: true });
      if (!yes) return false;
      try {
        await put(`/admin/channels/${c.id}/managers/${a.id}`, { override: true });
        toast(`${who(a)} was added by owner override and is a manager of ${c.name}.`);
        return true;
      } catch (err2) { if (!err2.silent) toast(err2.message, true); return false; }
    }
  };

  // overrideAdd adds someone to a private or knock channel by owner
  // override (ownerOverrideAdd), after a confirmation naming what follows.
  const overrideAdd = async (c, exclude) => {
    const chosen = await pickAccounts(`Add to ${c.name} by owner override`, { exclude: new Set([ctx.me.id, ...exclude]), roles: ['owner', 'admin', 'member', 'guest'],
      note: 'Someone of this instance who is not in the channel. Every member is shown that you added them.' });
    if (!chosen || !chosen[0]) return false;
    const a = chosen[0];
    if (!(await confirmDialog(`Add ${who(a)} to ${c.name} by owner override?`, ownerOverrideWarning(c.name, who(a)), { type: c.name, yes: 'Add by owner override', danger: true }))) return false;
    try {
      await put(`/admin/channels/${c.id}/members/${a.id}`);
      toast(`${who(a)} was added to ${c.name} by owner override.`);
      return true;
    } catch (err) { if (!err.silent) toast(err.message, true); return false; }
  };

  // pickManager chooses whom to appoint: anyone of the instance who may
  // be one (not a guest, a bot or someone of another server).
  const pickManager = async (c, exclude) => {
    const chosen = await pickAccounts(`Appoint a manager of ${c.name}`, { exclude: new Set(exclude), roles: ['owner', 'admin', 'member'],
      note: closed(c) ? 'A member of the channel, or anyone else of this instance: someone not in it is added by owner override first, after you confirm.'
        : c.knock ? 'Anyone of this instance: someone not in it is added, as any add is announced, and made its manager.'
          : 'Anyone of this instance: they join the channel as its manager.' });
    return chosen && chosen[0] && appoint(c, chosen[0]);
  };

  const membersDialog = (c) => openDialog(`Members of ${c.name}`, (close) => {
    const body = h('ul', { class: 'rows' });
    let ids = [];
    const load = async () => {
      const out = await get('/admin/channels/' + c.id);
      const known = await people(out.members.map((m) => m.account_id));
      ids = out.members.map((m) => m.account_id);
      fill(body, out.members.map((m) => {
        const a = known.get(m.account_id) || { id: m.account_id, username: '?' };
        const mayBe = isOwner && m.level !== 'manager' && a.role !== 'guest' && a.role !== 'bot' && a.role !== 'remote';
        const go = button('Appoint as manager', () => busy(go, async () => { if (await appoint(c, a)) load(); }), 'small');
        const tag = levelTag(m.level, a.role);
        return h('li', { dataset: { account: m.account_id } }, avatar(a), h('div', { class: 'what' }, h('strong', {}, a.display_name || a.username), h('small', {}, '@' + a.username + (m.via_group ? ' · here through a group' : ''))),
          h('span', { class: 'tag ' + tag.cls }, tag.text), mayBe && h('div', { class: 'does' }, go));
      }));
    };
    load().catch((err) => fill(body, h('li', {}, err.message)));
    const someoneElse = isOwner && !c.shared && button('Appoint someone else…', async () => { if (await pickManager(c, ids)) load(); }, 'small');
    const override = isOwner && c.private && !c.shared && button('Add by owner override…', async () => { if (await overrideAdd(c, ids)) load(); }, 'small danger');
    return { body: [
      c.private && h('p', { class: 'note' }, 'This channel is private. An admin sees who is in it and who manages it, and nothing that was said.'),
      isOwner ? h('p', { class: 'dim small' }, 'As an owner you appoint a manager for a channel that has lost its managers or has one acting against it. Moderators and managers are otherwise set by the channel’s own managers, in the chat.')
        : h('p', { class: 'dim small' }, 'Only an owner appoints a manager here. Moderators and managers are otherwise set by the channel’s own managers, in the chat.'),
      (someoneElse || override) && h('p', { class: 'row' }, someoneElse, override), body],
    foot: [button('Done', () => close(true))] };
  }, { wide: true });

  const menu = (c, anchor) => openMenu(anchor, [
    { head: c.name },
    { label: 'Members and managers', onclick: () => membersDialog(c).then(reload) },
    isOwner && !c.shared && { label: 'Appoint a manager…', onclick: async () => { if (await pickManager(c, c.manager_ids)) reload(); } },
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
    isOwner && c.private && !c.shared && { sep: true },
    isOwner && c.private && !c.shared && { label: 'Add by owner override…', danger: true, onclick: async () => {
      let ids = [];
      try { ids = (await get('/admin/channels/' + c.id)).members.map((m) => m.account_id); } catch { /* the server refuses a member */ }
      if (await overrideAdd(c, ids)) reload();
    } },
  ], { alignRight: true, label: 'Actions for ' + c.name });

  function row(c, child) {
    const section = sections.find((s) => s.id === c.section_id);
    const more = h('button', { type: 'button', class: 'btn small', onclick: (e) => menu(c, e.currentTarget) }, 'Actions');
    return h('li', { class: child ? 'tree-child' : '' }, channelIcon(c),
      h('div', { class: 'what' },
        h('strong', {}, h('bdi', {}, c.name), ' ', h('span', { class: 'tag ' + (c.private ? 'pink' : 'line') }, c.knock ? 'knock' : c.private ? 'private' : 'public'), ' ',
          c.locked_at && h('span', { class: 'tag grey' }, 'locked'), ' ',
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
    h('p', { class: 'dim small' }, 'Names are not unique: the section, the parent, the managers and the date tell same-named channels apart. Other channels are made in the chat. Archiving, locking and a channel’s other settings are its managers’, in the chat.'),
    list);
}
