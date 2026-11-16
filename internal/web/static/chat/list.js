// The sidebar: where conversations are listed (docs/CLIENTS.md sections
// 9.1, 9.2 and 9.25), drawn as the style guide's channel list
// (docs/STYLE.md, "The channel list"). The web chat is one server's page,
// so this is the rail layout with no rail to draw: Starred, the admin's
// sections with their channels and sub-channels, Chats, and Sealed.
//
// Which rows exist at all is decided by each membership's `show`
// (store.hasRow); "Show hidden" brings back the hidden ones. How a row is
// drawn follows its `mode`: bold with a count while unread, dimmed when it
// notifies nothing.
//
// Folding, which is kept in this browser (store.prefs.collapsed):
//  - A folded section keeps its unread rows and the open one in view.
//  - A folded parent says "N more" and keeps its unread sub-channels, and
//    the open one, in view under it.
//  - A sub-channel whose parent has no row of its own there is drawn at
//    the parent's level as "parent › name".
// Every row lines up in one column: the fold chevron sits in the margin.

import { h, icon, iconButton, fill, openMenu, avatar, day, toast, dateTime, plural } from '/shared/dom.js';
import { channelIcon, detailLine } from '/shared/pick.js';
import { store, conversation, hasRow, title, isAdHoc, isSealed, isDirectChat, isMuted, showOf, savePrefs, markRead, account, isAdmin, live } from './store.js';
import { look } from './env.js';
import { ui } from './ui.js';
import { openModes, updateMembership, openChatSettings } from './modes.js';
import { openStatus, openDnd, statusLine } from './profile.js';
import { presenceDot } from './conversation.js';
import { put } from '/shared/api.js';
import { newSealedChat } from './sealed/index.js';
import { chatStateTag, closeChat } from './requests.js';

const byName = (a, b) => title(a).localeCompare(title(b), undefined, { sensitivity: 'base' }) || String(a.created_at).localeCompare(String(b.created_at));
const quietOf = (c) => c.membership.mode === 'nothing' || isMuted(c);
// A row a fold keeps in view: the open conversation, and anything unread
// that notifies.
const kept = (c) => store.current === c.id || (c.membership.unread_count > 0 && !quietOf(c));

// tree works out the groups of the sidebar and the rows to draw in each,
// with folding applied. A group is { id, name, shut, total, rows }; a row
// is { c, depth, prefix, label, kids, open, more } where prefix is
// "parent › " when the row stands without its parent, kids the number of
// sub-channels it has rows for, and more how many of them a fold hides.
export function tree() {
  const all = [...store.conversations.values()].filter((c) => !c.archived_at || store.current === c.id);
  const rows = new Set(all.filter(hasRow));
  const collapsed = store.prefs.collapsed || {};
  const named = all.filter((c) => c.kind === 'channel' && c.name);
  const kidsOf = new Map();
  for (const c of named) if (c.parent_id) kidsOf.set(c.parent_id, [...(kidsOf.get(c.parent_id) || []), c]);

  // Each group is first a list of items { c, kids }, then drawn.
  const listed = [];
  const starred = [...rows].filter((c) => c.membership.starred).sort(byName);
  if (starred.length) listed.push({ id: 'starred', name: 'Starred', items: starred.map((c) => ({ c, kids: [] })) });

  // A sub-channel takes its parent's section while it has a parent the
  // person can see, and sits under it while the parent's row is there.
  const sectionOf = (c) => { const p = c.parent_id && conversation(c.parent_id); return (p ? p.section_id : c.section_id) || ''; };
  const bySection = new Map();
  for (const c of named) {
    if (!rows.has(c) || c.membership.starred) continue;
    const parent = c.parent_id && store.conversations.get(c.parent_id);
    if (parent && rows.has(parent) && !parent.membership.starred) continue;
    const s = sectionOf(c);
    bySection.set(s, [...(bySection.get(s) || []), c]);
  }
  for (const [sid, name] of [...store.sections.map((s) => [s.id, s.name]), ['', 'Channels']]) {
    const tops = (bySection.get(sid) || []).sort(byName);
    if (!tops.length) continue;
    listed.push({ id: 'section-' + sid, name, items: tops.map((c) => ({ c, kids: (kidsOf.get(c.id) || []).filter((k) => rows.has(k) && !k.membership.starred).sort(byName) })) });
  }

  const recent = (a, b) => String(b.last_message_at || b.created_at).localeCompare(String(a.last_message_at || a.created_at));
  const chats = [...rows].filter((c) => isAdHoc(c) && !c.membership.starred).sort(recent);
  if (chats.length) listed.push({ id: 'chats', name: 'Chats', items: chats.map((c) => ({ c, kids: [] })) });
  const sealed = [...rows].filter((c) => isSealed(c) && !c.membership.starred).sort(recent);
  if (sealed.length) listed.push({ id: 'sealed', name: 'Sealed', items: sealed.map((c) => ({ c, kids: [] })) });

  return listed.map((g) => {
    const shut = !!collapsed['g:' + g.id];
    const out = [];
    let total = 0;
    for (const { c, kids } of g.items) {
      total += 1 + kids.length;
      const folded = kids.length > 0 && !!collapsed[c.id];
      const subs = kids.filter((k) => (!shut && !folded) || kept(k));
      const [prefix, label] = c.parent_id ? pathParts(c) : ['', title(c)];
      if (!shut || kept(c)) {
        out.push({ c, depth: 0, prefix, label, kids: kids.length, open: !folded, more: folded ? kids.length - subs.length : 0 });
        for (const k of subs) out.push({ c: k, depth: 1, prefix: '', label: title(k) });
      } else {
        // The parent's row is folded away with its section: what stays is
        // drawn at its level and names it.
        for (const k of subs) { const [p, l] = pathParts(k); out.push({ c: k, depth: 0, prefix: p, label: l }); }
      }
    }
    return { id: g.id, name: g.name, shut, total, rows: out };
  });
}

function pathParts(c) {
  const p = c.parent_id && conversation(c.parent_id);
  return p ? [title(p) + ' › ', title(c)] : ['', title(c)];
}

// The order of rows as drawn, for moving to the previous or next one.
export function rowOrder() {
  return tree().flatMap((g) => g.rows.map((r) => r.c));
}

// The seconds left until the live connection tries again, as the status
// line says them: " in 7 seconds", or nothing when no attempt is waiting.
function retryIn() {
  const s = live.retrySeconds();
  return s == null ? '' : ' in ' + plural(s, 'second');
}

export class Sidebar {
  constructor() {
    this.top = h('div', { class: 'side-top' });
    this.nav = h('div', { class: 'side-nav' });
    this.groups = h('div', { class: 'side-groups' });
    this.hidden = h('div', { class: 'side-hidden' });
    this.bottom = h('div', { class: 'side-bottom' });
    this.me = h('div', { class: 'side-me' });
    this.el = h('div', { class: 'side' }, this.top, this.nav, this.groups, this.hidden, this.bottom, this.me);
    this.groups.addEventListener('keydown', (e) => this.key(e));
    let queued = false;
    const later = () => { if (queued) return; queued = true; requestAnimationFrame(() => { queued = false; this.draw(); }); };
    for (const name of ['conversations', 'accounts', 'me', 'lists', 'connection', 'notifications', 'threads', 'requests']) store.on(name, later);
    this.draw();
    // While the connection waits to try again the status line counts down
    // the wait it chose. Only the number is touched, four times a second
    // at most and only when it changes, so nothing else is redrawn.
    setInterval(() => {
      if (!this.retryIn || !this.retryIn.isConnected) return;
      const text = retryIn();
      if (this.retryIn.textContent !== text) this.retryIn.textContent = text;
    }, 250);
  }

  draw() {
    if (!store.me) return;
    // A redraw replaces the rows; whatever had keyboard focus gets it back.
    const had = this.el.contains(document.activeElement) && document.activeElement.dataset.key;
    const me = store.me;
    const dnd = me.dnd_state && me.dnd_state.on;
    const conn = store.connection;
    // The countdown is kept out of what a screen reader is told: the line
    // is a status, and would otherwise be read out again every second.
    this.retryIn = conn === 'live' || conn === 'catching-up' || conn === 'signed-out' ? null : h('span', { class: 'conn-in', 'aria-hidden': 'true' }, retryIn());
    fill(this.top,
      h('div', { class: 'side-brand' },
        h('div', { class: 'grow' }, h('strong', { class: 'clip' }, store.server.community_name || 'Churr'),
          h('small', { class: 'conn ' + conn, role: 'status' }, h('i', { 'aria-hidden': 'true' }),
            conn === 'live' ? 'connected' : conn === 'catching-up' ? 'catching up…' : conn === 'signed-out' ? 'signed out' : h('span', {}, 'reconnecting', this.retryIn, '…'))),
        iconButton('settings', 'Web chat settings', () => openChatSettings(), 'small')));

    const unreadNotes = store.notifications.filter((n) => !n.cleared).length || store.total.badge || 0;
    const link = (path, name, iconName, count) => h('a', { class: 'side-link', href: path, 'aria-current': location.pathname === path ? 'page' : null, dataset: { key: 'nav:' + path },
      onclick: (e) => { if (e.metaKey || e.ctrlKey) return; e.preventDefault(); ui.go(path); } },
    h('span', { class: 'g' }, icon(iconName, null, 'small')), h('span', { class: 'grow' }, name), count ? h('span', { class: 'badge', 'aria-label': `${count} new` }, String(count)) : null);
    fill(this.nav,
      h('button', { type: 'button', class: 'side-switch', onclick: () => ui.switcher() }, h('span', { class: 'g' }, icon('search', null, 'small')), h('span', { class: 'grow' }, 'Jump to…'), h('kbd', {}, 'Ctrl K')),
      link('/chat/notifications', 'Notifications', 'bell', unreadNotes),
      link('/chat/requests', 'Requests', 'person', store.requests.length),
      link('/chat/threads', 'Threads', 'thread', store.total.unread_threads),
      link('/chat/bookmarks', 'Bookmarks', 'bookmark'),
      link('/chat/search', 'Search', 'search'),
      link('/chat/scheduled', 'Scheduled', 'clock'));

    const groups = tree();
    const collapsed = store.prefs.collapsed;
    fill(this.groups, groups.length ? groups.map((g) => {
      // Same-named rows side by side each get their creation date.
      const names = new Map();
      const nameKey = (r) => r.depth + '|' + (r.prefix + r.label).toLowerCase();
      for (const r of g.rows) names.set(nameKey(r), (names.get(nameKey(r)) || 0) + 1);
      return h('section', { class: 'side-group' + (g.shut ? ' shut' : ''), 'aria-label': g.name },
        h('h2', {}, h('button', { type: 'button', class: 'sec', 'aria-expanded': String(!g.shut), dataset: { key: 'sec:' + g.id },
          title: g.shut ? 'Show every row of ' + g.name : 'Fold ' + g.name + ': unread rows stay',
          onclick: () => { collapsed['g:' + g.id] = !g.shut; savePrefs(); } },
        h('span', { class: 'grow' }, g.name), h('em', { 'aria-label': plural(g.total, 'conversation') }, String(g.total)))),
        g.rows.length ? h('ul', {}, g.rows.map((r) => this.row(r, names.get(nameKey(r)) > 1))) : null);
    }) : h('p', { class: 'dim side-empty' }, 'No conversations to show. Browse channels, or start a chat.'));

    fill(this.hidden,
      h('label', { class: 'hid-t' }, h('input', { type: 'checkbox', class: 'switch', checked: store.prefs.showHidden, dataset: { key: 'hidden' },
        onchange: (e) => { store.prefs.showHidden = e.target.checked; savePrefs(); } }), h('span', {}, 'Show hidden')));

    fill(this.bottom,
      h('button', { type: 'button', class: 'side-link', onclick: () => ui.go('/chat/browse') }, h('span', { class: 'g' }, icon('hash', null, 'small')), h('span', { class: 'grow' }, 'Browse channels')),
      h('button', { type: 'button', class: 'side-link', onclick: (e) => openMenu(e.currentTarget, [
        { label: 'New chat', icon: 'chat', onclick: () => ui.newChat() },
        store.me.role !== 'guest' && { label: 'New channel', icon: 'hash', onclick: () => ui.newChannel() },
        store.server.features.sealed_chats && { label: 'New sealed chat', icon: 'sealed', onclick: () => newSealedChat() },
      ], { above: true, label: 'New' }) }, h('span', { class: 'g' }, icon('plus', null, 'small')), h('span', { class: 'grow' }, 'New…')));

    fill(this.me,
      h('button', { type: 'button', class: 'me-status', onclick: () => openStatus(), title: 'Set your status' },
        avatar(me), h('span', { class: 'grow clip' }, h('strong', {}, me.display_name || me.username),
          h('small', {}, presenceDot({ presence: me.presence || 'online', dnd }), statusLine(me) || 'Set a status'))),
      h('button', { type: 'button', class: 'icon-btn small' + (dnd ? ' on' : ''), 'aria-pressed': String(!!dnd), 'aria-label': dnd ? 'Do not disturb is on' : 'Do not disturb', title: dnd ? 'Do not disturb is on' : 'Do not disturb', onclick: () => openDnd() }, icon('moon')),
      h('button', { type: 'button', class: 'icon-btn small', 'aria-label': 'Account menu', title: 'Account', onclick: (e) => openMenu(e.currentTarget, [
        { label: 'Account page', icon: 'person', href: '/account' },
        isAdmin() && { label: 'Admin console', icon: 'settings', href: '/admin' },
        { label: 'Colour themes', icon: 'palette', onclick: () => ui.go('/chat/themes') },
        { label: 'Keyboard shortcuts', icon: 'info', onclick: () => ui.shortcuts() },
        { sep: true },
        { label: 'Sign out', icon: 'sign-out', onclick: () => ui.signOut() },
      ], { alignRight: true, above: true, label: 'Account' }) }, icon('more')));

    if (had) {
      const back = this.el.querySelector(`[data-key="${CSS.escape(had)}"]`);
      if (back) back.focus({ preventScroll: true });
    }
  }

  row(r, twin) {
    const c = r.c;
    const m = c.membership;
    const muted = isMuted(c);
    const quiet = m.mode === 'nothing' || muted;
    const unread = m.unread_count > 0 && !quiet;
    const others = isAdHoc(c) || isSealed(c) ? (c.member_ids || []).filter((x) => x !== store.me.id) : [];
    const one = others.length === 1 ? account(others[0]) : null;
    const current = store.current === c.id;
    const hidden = showOf(c) === 'hidden';
    const unfold = (e) => { e.preventDefault(); e.stopPropagation(); store.prefs.collapsed[c.id] = false; savePrefs(); };
    const a = h('a', {
      class: 'row-link', href: '/chat/c/' + c.id, 'aria-current': current ? 'page' : null, title: detailLine(c, look) || title(c),
      dataset: { key: 'row:' + c.id },
      onclick: (e) => { if (e.metaKey || e.ctrlKey || e.shiftKey) return; e.preventDefault(); ui.openConversation(c.id); },
    },
    h('span', { class: 'g' }, one || isSealed(c) ? presenceDot(one, isSealed(c)) : channelIcon(c, 'glyph')),
    h('span', { class: 'row-name' }, r.prefix ? h('span', { class: 'pp' }, h('bdi', {}, r.prefix)) : null, h('bdi', {}, r.label), twin ? h('small', { class: 'dim' }, ' ' + day(c.created_at)) : null),
    c.shared ? icon('link', 'Shared with other servers', 'small') : null,
    c.call ? icon('call', 'A call is on', 'small') : null,
    muted ? h('span', { class: 'row-quiet', title: 'Muted until ' + dateTime(m.muted_until) }, icon('bell-off', 'Muted', 'small'))
      : quiet ? h('span', { class: 'row-quiet', title: 'Notifies nothing' }, icon('bell-off', 'Notifies nothing', 'small')) : null,
    hidden ? h('span', { class: 'tag', role: 'img', 'aria-label': 'Hidden' }, 'hidden') : null,
    chatStateTag(c),
    r.more ? h('span', { class: 'row-kids', onclick: unfold, title: 'Show the sub-channels of ' + title(c) }, `${r.more} more`) : null,
    m.badge_count > 0 ? h('span', { class: 'badge', 'aria-label': `${m.badge_count} to see` }, String(m.badge_count)) : null);
    const toggle = r.kids ? h('button', { type: 'button', class: 'row-toggle', 'aria-expanded': String(!!r.open), dataset: { key: 'tog:' + c.id },
      'aria-label': (r.open ? 'Hide' : 'Show') + ' the sub-channels of ' + title(c),
      onclick: () => { store.prefs.collapsed[c.id] = !!r.open; savePrefs(); } }) : null;
    const menu = h('button', { type: 'button', class: 'row-menu', 'aria-label': 'Menu for ' + title(c), dataset: { key: 'menu:' + c.id }, onclick: (e) => rowMenu(c, e.currentTarget) }, icon('more', null, 'small'));
    const li = h('li', {
      class: 'side-row' + (r.depth ? ' child' : '') + (r.prefix ? ' alone' : '') + (current ? ' current' : '') + (unread ? ' unread' : '') + (quiet ? ' quiet' : '') + (hidden ? ' hidden-row' : '') + (r.kids ? ' has-kids' : ''),
      dataset: { conversation: c.id },
    }, toggle, a, menu);
    li.addEventListener('contextmenu', (e) => { e.preventDefault(); rowMenu(c, { x: e.clientX, y: e.clientY }); });
    return li;
  }

  // The list by keyboard. Tab reaches every control; within the list the
  // arrows move between section names and rows, Right opens and Left folds
  // (a section from its name, a parent from its row), and Left on a
  // sub-channel goes to its parent. Home and End go to the ends.
  key(e) {
    if (e.altKey || e.ctrlKey || e.metaKey) return;
    const t = e.target;
    if (!(t.matches && t.matches('.sec, .row-link'))) return;
    const stops = [...this.groups.querySelectorAll('.sec, .row-link')];
    const at = stops.indexOf(t);
    const move = (i) => { e.preventDefault(); const s = stops[Math.max(0, Math.min(stops.length - 1, i))]; if (s) s.focus(); };
    const li = t.closest('.side-row');
    const toggle = li && li.querySelector('.row-toggle');
    switch (e.key) {
      case 'ArrowDown': return move(at + 1);
      case 'ArrowUp': return move(at - 1);
      case 'Home': return move(0);
      case 'End': return move(stops.length - 1);
      case 'ArrowRight':
        if (t.matches('.sec') && t.getAttribute('aria-expanded') === 'false') { e.preventDefault(); t.click(); }
        else if (toggle && toggle.getAttribute('aria-expanded') === 'false') { e.preventDefault(); toggle.click(); }
        return undefined;
      case 'ArrowLeft':
        if (t.matches('.sec') && t.getAttribute('aria-expanded') === 'true') { e.preventDefault(); t.click(); }
        else if (toggle && toggle.getAttribute('aria-expanded') === 'true') { e.preventDefault(); toggle.click(); }
        else if (li && li.classList.contains('child')) {
          let p = li.previousElementSibling;
          while (p && p.classList.contains('child')) p = p.previousElementSibling;
          if (p) { e.preventDefault(); p.querySelector('.row-link').focus(); }
        }
        return undefined;
      case ' ':
        if (t.matches('.row-link')) { e.preventDefault(); t.click(); }
        return undefined;
      default: return undefined;
    }
  }
}

// The row's menu: what notifies, show in sidebar, mute, star, mark read or
// unread, leave.
export function rowMenu(c, anchor) {
  const m = c.membership;
  const set = (change) => updateMembership(c.id, change);
  const show = m.show || '';
  openMenu(anchor, [
    { head: 'What notifies' },
    { label: 'Any activity', checked: m.mode === 'any', onclick: () => set({ mode: 'any' }) },
    !isSealed(c) && { label: 'Mentions', checked: m.mode === 'mentions', onclick: () => set({ mode: 'mentions' }) },
    { label: 'Nothing', checked: m.mode === 'nothing', onclick: () => set({ mode: 'nothing' }) },
    isMuted(c) ? { label: 'Unmute', icon: 'bell', onclick: () => set({ muted_until: null }) } : { label: 'Mute for an hour', icon: 'bell-off', onclick: () => set({ muted_until: new Date(Date.now() + 3600e3).toISOString() }) },
    { head: 'Show in sidebar' },
    { label: 'This browser’s default', checked: show === '', onclick: () => set({ show: null }) },
    { label: 'Always', checked: show === 'always', onclick: () => set({ show: 'always' }) },
    { label: 'When there is activity', checked: show === 'activity', onclick: () => set({ show: 'activity' }) },
    { label: 'When it notifies', checked: show === 'notification', onclick: () => set({ show: 'notification' }) },
    { label: 'Hidden', checked: show === 'hidden', onclick: () => set({ show: 'hidden' }) },
    { sep: true },
    { label: 'More settings…', icon: 'settings', onclick: () => openModes(c.id) },
    { label: m.starred ? 'Remove star' : 'Star', icon: 'star', onclick: () => set({ starred: !m.starred }) },
    isDirectChat(c) && c.chat_state !== 'closed' && { label: c.chat_state === 'waiting' ? 'Withdraw request…' : 'Close chat…', icon: 'lock', onclick: () => closeChat(c) },
    m.unread_count > 0
      ? { label: 'Mark read', icon: 'check', onclick: () => markRead(c.id) }
      : { label: 'Mark unread', icon: 'dot', onclick: async () => {
        try { const out = await put(`/conversations/${c.id}/read`, { seq: Math.max(0, c.last_seq - 1) }); c.membership = out.membership; store.emit('conversations'); } catch (err) { toast(err.message, true); } } },
  ], { label: 'Menu for ' + title(c), alignRight: !!anchor.getBoundingClientRect });
}
