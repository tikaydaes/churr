// The sidebar: where conversations are listed (docs/CLIENTS.md sections
// 9.1, 9.2 and 9.25), drawn as the style guide's channel list
// (docs/STYLE.md, "The channel list"). The web chat is one server's page,
// so this is the rail layout with no rail to draw. The headings, their
// order and the order of rows are the person's own, kept by the server
// (ruling R83, arrange.js): their sections, the admin's, Channels, Chats
// and Sealed, starred rows at the top of each.
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

import { h, icon, iconButton, fill, openMenu, avatar, day, toast, dateTime, plural, promptDialog, confirmDialog } from '/shared/dom.js';
import { channelIcon, detailLine } from '/shared/pick.js';
import { store, conversation, hasRow, title, isAdHoc, isSealed, isDirectChat, isMuted, showOf, savePrefs, markRead, account, isAdmin, live, changeSidebar } from './store.js';
import { look } from './env.js';
import { ui } from './ui.js';
import { openModes, updateMembership, openChatSettings } from './modes.js';
import { openStatus, openDnd, statusLine } from './profile.js';
import { presenceDot } from './conversation.js';
import { put } from '/shared/api.js';
import { newSealedChat } from './sealed/index.js';
import { chatStateTag, closeChat, offersList } from './requests.js';
import { STRINGS as CMD } from '/shared/commands.js';
import { approvalCount, mayApproveAny } from '/shared/roles.js';
import { arrange, homeKey, keyOf, isOwn, isSection, SORTS, reorderOps, moveOps } from './arrange.js';

const quietOf = (c) => c.membership.mode === 'nothing' || isMuted(c);
// A row a fold keeps in view: the open conversation, and anything unread
// that notifies.
const kept = (c) => store.current === c.id || (c.membership.unread_count > 0 && !quietOf(c));

// How arrange.js sees a conversation of this page.
const shape = {
  kind: (c) => (isSealed(c) ? 'sealed' : isAdHoc(c) ? 'chat' : 'channel'),
  parent: (c) => (c.parent_id && conversation(c.parent_id)) || null,
  get sections() { return store.sections; },
};

// tree works out the headings of the sidebar and the rows to draw under
// each, in the person's own arrangement (arrange.js), with folding
// applied. A group is { id, key, name, own, sort, shut, total, rows,
// items }; a row is { c, depth, prefix, label, kids, open, more } where
// prefix is "parent › " when the row stands without its parent, kids the
// number of sub-channels it has rows for, and more how many of them a fold
// hides. A heading with no rows is left out, except the person's own
// sections, which stay to be moved into.
export function tree() {
  const all = [...store.conversations.values()].filter((c) => !c.archived_at || store.current === c.id);
  const rows = all.filter(hasRow);
  const collapsed = store.prefs.collapsed || {};
  return arrange(rows, store.sidebar, store.sections, shape, title).filter((g) => g.items.length || g.own).map((g) => {
    const shut = !!collapsed['g:' + g.key];
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
    return { ...g, id: g.key, shut, total, rows: out };
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
    sidebar = this;
    this.drawn = [];
    this.reordering = false;
    dragAndDrop(this);
    let queued = false;
    const later = () => { if (queued) return; queued = true; requestAnimationFrame(() => { queued = false; this.draw(); }); };
    for (const name of ['conversations', 'accounts', 'me', 'lists', 'connection', 'notifications', 'threads', 'requests', 'approvals', 'offers']) store.on(name, later);
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
      // Only for those who may decide some kind of request.
      mayApproveAny(store.approvals) ? link('/chat/approvals', 'Approvals', 'check', approvalCount(store.approvals)) : null,
      link('/chat/threads', 'Threads', 'thread', store.total.unread_threads),
      link('/chat/bookmarks', 'Bookmarks', 'bookmark'),
      link('/chat/search', 'Search', 'search'),
      link('/chat/scheduled', 'Scheduled', 'clock'));

    const groups = tree();
    this.drawn = groups;
    const collapsed = store.prefs.collapsed;
    fill(this.groups, groups.length ? groups.map((g) => {
      // Same-named rows side by side each get their creation date.
      const names = new Map();
      const nameKey = (r) => r.depth + '|' + (r.prefix + r.label).toLowerCase();
      for (const r of g.rows) names.set(nameKey(r), (names.get(nameKey(r)) || 0) + 1);
      const sortName = (SORTS.find(([v]) => v === g.sort) || [])[1];
      const movable = g.key !== 'chats' && g.key !== 'sealed';
      return h('section', { class: 'side-group' + (g.shut ? ' shut' : '') + (g.own ? ' own' : ''), 'aria-label': g.name, dataset: { group: g.key },
        draggable: movable ? 'true' : null },
      h('h2', {}, h('button', { type: 'button', class: 'sec', 'aria-expanded': String(!g.shut), dataset: { key: 'sec:' + g.key },
        title: g.shut ? 'Show every row of ' + g.name : 'Fold ' + g.name + ': unread rows stay',
        'aria-description': 'Sorted ' + (sortName || '').toLowerCase() + (movable ? '. Alt+Up and Alt+Down move this heading.' : ''),
        onclick: () => { collapsed['g:' + g.key] = !g.shut; savePrefs(); } },
      h('span', { class: 'grow' }, g.name), h('em', { 'aria-label': plural(g.total, 'conversation') }, String(g.total))),
      h('button', { type: 'button', class: 'sec-menu', 'aria-label': 'Menu for the heading ' + g.name, dataset: { key: 'secmenu:' + g.key }, onclick: (e) => this.headingMenu(g, e.currentTarget) }, icon('more', null, 'small'))),
      g.rows.length ? h('ul', {}, g.rows.map((r) => this.row(r, names.get(nameKey(r)) > 1, g)))
        : g.own ? h('p', { class: 'dim side-drop' }, 'Empty. Drag conversations here, or use Move… in a row’s menu.') : null);
    }) : store.offers.length ? null : h('p', { class: 'dim side-empty' }, 'No conversations to show. Browse channels, or start a chat.'));
    // Channels offered to the person (ruling R103), above the rest.
    const offers = offersList();
    if (offers) this.groups.prepend(offers);
    if (this.reordering) {
      this.groups.prepend(h('div', { class: 'reorder-bar', role: 'status' }, h('span', { class: 'grow' }, 'Reordering: use the arrows on each row.'),
        h('button', { type: 'button', class: 'btn small primary', dataset: { key: 'reorder-done' }, onclick: () => { this.reordering = false; this.draw(); } }, 'Done')));
    }

    fill(this.hidden,
      h('label', { class: 'hid-t' }, h('input', { type: 'checkbox', class: 'switch', checked: store.prefs.showHidden, dataset: { key: 'hidden' },
        onchange: (e) => { store.prefs.showHidden = e.target.checked; savePrefs(); } }), h('span', {}, 'Show hidden')));

    fill(this.bottom,
      h('button', { type: 'button', class: 'side-link', onclick: () => ui.go('/chat/browse') }, h('span', { class: 'g' }, icon('hash', null, 'small')), h('span', { class: 'grow' }, 'Browse channels')),
      h('button', { type: 'button', class: 'side-link', onclick: (e) => openMenu(e.currentTarget, [
        { label: 'New chat', icon: 'chat', onclick: () => ui.newChat() },
        store.me.role !== 'guest' && { label: 'New channel', icon: 'hash', onclick: () => ui.newChannel() },
        store.server.features.sealed_chats && { label: 'New sealed chat', icon: 'sealed', onclick: () => newSealedChat() },
        { sep: true },
        { label: 'New section', icon: 'plus', onclick: () => newSection() },
      ], { above: true, label: 'New' }) }, h('span', { class: 'g' }, icon('plus', null, 'small')), h('span', { class: 'grow' }, 'New…')));

    fill(this.me,
      h('button', { type: 'button', class: 'me-status', onclick: () => openStatus(), title: 'Set your status' },
        avatar(me), h('span', { class: 'grow clip' }, h('strong', {}, me.display_name || me.username),
          h('small', {}, presenceDot({ presence: me.presence || 'online', dnd }), statusLine(me) || 'Set a status'))),
      h('button', { type: 'button', class: 'icon-btn small' + (dnd ? ' on' : ''), 'aria-pressed': String(!!dnd), 'aria-label': dnd ? 'Do not disturb is on' : 'Do not disturb', title: dnd ? 'Do not disturb is on' : 'Do not disturb', onclick: () => openDnd() }, icon('moon')),
      h('button', { type: 'button', class: 'icon-btn small', 'aria-label': 'Account menu', title: 'Account', onclick: (e) => openMenu(e.currentTarget, [
        { label: 'Account page', icon: 'person', href: '/account' },
        isAdmin() && { label: 'Admin console', icon: 'settings', href: '/admin' },
        mayApproveAny(store.approvals) && { label: approvalCount(store.approvals) ? `Approvals (${approvalCount(store.approvals)})` : 'Approvals', icon: 'check', onclick: () => ui.go('/chat/approvals') },
        { label: 'Colour themes', icon: 'palette', onclick: () => ui.go('/chat/themes') },
        { label: 'Keyboard shortcuts', icon: 'info', onclick: () => ui.shortcuts() },
        { sep: true },
        { label: 'Lock', icon: 'padlock', onclick: () => ui.lock({ ask: true }) },
        { label: 'Sign out', icon: 'sign-out', onclick: () => ui.signOut() },
      ], { alignRight: true, above: true, label: 'Account' }) }, icon('more')));

    if (had) {
      const back = this.el.querySelector(`[data-key="${CSS.escape(had)}"]`);
      if (back) back.focus({ preventScroll: true });
    }
  }

  headingMenu(g, anchor) { headingMenu(g, anchor); }

  row(r, twin, g) {
    const c = r.c;
    const m = c.membership;
    const muted = isMuted(c);
    const quiet = m.mode === 'nothing' || muted;
    const unread = m.unread_count > 0 && !quiet;
    const others = isAdHoc(c) || isSealed(c) ? [...(c.member_ids || []), ...(c.invited_ids || [])].filter((x) => x !== store.me.id) : [];
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
    store.knocks.get(c.id) ? h('span', { class: 'tag yellow', title: CMD.knocksWaiting(store.knocks.get(c.id)) }, CMD.knockAsking(store.knocks.get(c.id))) : null,
    r.more ? h('span', { class: 'row-kids', onclick: unfold, title: 'Show the sub-channels of ' + title(c) }, `${r.more} more`) : null,
    m.badge_count > 0 ? h('span', { class: 'badge', 'aria-label': `${m.badge_count} to see` }, String(m.badge_count)) : null);
    const toggle = r.kids ? h('button', { type: 'button', class: 'row-toggle', 'aria-expanded': String(!!r.open), dataset: { key: 'tog:' + c.id },
      'aria-label': (r.open ? 'Hide' : 'Show') + ' the sub-channels of ' + title(c),
      onclick: () => { store.prefs.collapsed[c.id] = !!r.open; savePrefs(); } }) : null;
    const menu = h('button', { type: 'button', class: 'row-menu', 'aria-label': 'Menu for ' + title(c), dataset: { key: 'menu:' + c.id }, onclick: (e) => rowMenu(c, e.currentTarget) }, icon('more', null, 'small'));
    const top = !r.depth;
    const steps = this.reordering && top ? [
      h('button', { type: 'button', class: 'row-step', 'aria-label': 'Move ' + title(c) + ' up', onclick: () => moveBy(c, -1) }, icon('arrow-up', null, 'small')),
      h('button', { type: 'button', class: 'row-step', 'aria-label': 'Move ' + title(c) + ' down', onclick: () => moveBy(c, 1) }, icon('arrow-down', null, 'small')),
    ] : [];
    const li = h('li', {
      class: 'side-row' + (r.depth ? ' child' : '') + (r.prefix ? ' alone' : '') + (current ? ' current' : '') + (unread ? ' unread' : '') + (quiet ? ' quiet' : '') + (hidden ? ' hidden-row' : '') + (r.kids ? ' has-kids' : '') + (m.starred ? ' starred' : ''),
      dataset: { conversation: c.id, group: g.key }, draggable: top ? 'true' : null,
    }, toggle, a, ...steps, menu);
    li.addEventListener('contextmenu', (e) => { e.preventDefault(); rowMenu(c, { x: e.clientX, y: e.clientY }); });
    // On a touch screen a long press starts reordering.
    let press = null;
    li.addEventListener('touchstart', () => { press = setTimeout(() => { press = null; this.reordering = true; this.draw(); }, 550); }, { passive: true });
    for (const end of ['touchend', 'touchmove', 'touchcancel']) li.addEventListener(end, () => { if (press) clearTimeout(press); press = null; }, { passive: true });
    return li;
  }

  // The list by keyboard. Tab reaches every control; within the list the
  // arrows move between section names and rows, Right opens and Left folds
  // (a section from its name, a parent from its row), and Left on a
  // sub-channel goes to its parent. Home and End go to the ends.
  key(e) {
    // Alt+Up and Alt+Down move the focused row within its heading, or the
    // focused heading among the headings. Elsewhere they go to the
    // previous or next conversation.
    if (e.altKey && !e.ctrlKey && !e.metaKey && !e.shiftKey && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) {
      const by = e.key === 'ArrowUp' ? -1 : 1;
      const t = e.target;
      if (t.matches && t.matches('.row-link')) {
        const li = t.closest('.side-row');
        if (li.classList.contains('child')) return;
        e.preventDefault(); e.stopPropagation();
        moveBy(conversation(li.dataset.conversation), by);
      } else if (t.matches && t.matches('.sec')) {
        e.preventDefault(); e.stopPropagation();
        moveHeadingBy(t.closest('.side-group').dataset.group, by);
      }
      return;
    }
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

// --- the person's own arrangement (ruling R83) ---

// Worked out afresh: the drawn list is redrawn on the next frame.
const groupOfRow = (id) => tree().find((g) => g.items.some((x) => x.c.id === id));
let sidebar = null;

// send changes the person's sidebar and says what happened.
async function send(ops, said) {
  try {
    await changeSidebar(ops);
    if (said) ui.announce(typeof said === 'function' ? said() : said);
  } catch (err) { toast(err.message, true); }
}

function placeText(c) {
  const g = groupOfRow(c.id);
  if (!g) return title(c) + ' moved';
  const i = g.items.findIndex((x) => x.c.id === c.id);
  return `${title(c)} is in ${g.name}, ${i + 1} of ${g.items.length}`;
}

// moveBy moves a row one place up or down under its heading.
export function moveBy(c, by) {
  const g = groupOfRow(c.id);
  if (!g) return;
  const i = g.items.findIndex((x) => x.c.id === c.id);
  const to = i + by;
  if (to < 0 || to >= g.items.length) { ui.announce(by < 0 ? 'Already first' : 'Already last'); return; }
  // A starred row moves among the stars only.
  const starred = (x) => !!x.c.membership.starred;
  if (starred(g.items[i]) !== starred(g.items[to])) { ui.announce(starred(g.items[i]) ? 'Starred rows stay above the rest' : 'Already first after the starred rows'); return; }
  send(reorderOps(g, c.id, to, store.sidebar), () => placeText(c)).then(() => refocus(c.id));
}

function refocus(id) {
  requestAnimationFrame(() => { const el = document.querySelector(`.side-row[data-conversation="${CSS.escape(id)}"] .row-link`); if (el) el.focus({ preventScroll: false }); });
}

// moveHeadingBy moves a channel heading one place among the channel
// headings. Chats and Sealed stay last.
export function moveHeadingBy(key, by) {
  if (key === 'chats' || key === 'sealed') { ui.announce('Chats and Sealed stay at the bottom'); return; }
  const keys = arrange([], store.sidebar, store.sections, shape, title).map((x) => x.key).filter((k) => k !== 'chats' && k !== 'sealed');
  const i = keys.indexOf(key);
  const to = i + by;
  if (i < 0 || to < 0 || to >= keys.length) { ui.announce(by < 0 ? 'Already first' : 'Already last'); return; }
  const name = (arrange([], store.sidebar, store.sections, shape, title).find((x) => x.key === key) || {}).name;
  send([{ op: 'move_section', key, index: to }], `${name} moved to ${to + 1} of ${keys.length}`).then(() => {
    requestAnimationFrame(() => { const el = document.querySelector(`[data-key="${CSS.escape('sec:' + key)}"]`); if (el) el.focus(); });
  });
}

// moveTo puts a conversation under another heading: any section, the
// person's own or an admin's or Channels, or back where the server lists
// it.
function moveTo(c, key) {
  const target = tree().find((g) => g.key === key) || { key, items: [] };
  const name = (arrange([], store.sidebar, store.sections, shape, title).find((x) => x.key === key) || {}).name;
  send(moveOps(c, target, null, store.sidebar), `${title(c)} moved to ${name}`).then(() => refocus(c.id));
}

export async function newSection(then) {
  const name = await promptDialog('New section', 'Name', { maxLength: 64, hint: 'Only you see your sections. They are kept on this server for all your devices.', yes: 'Create' });
  if (!name || !name.trim()) return;
  await send([{ op: 'create_section', name: name.trim() }], `Section ${name.trim()} made`);
  const made = store.sidebar.sections[store.sidebar.sections.length - 1];
  if (made && then) then('p:' + made.id);
}

// moveMenu lists where a conversation can go: every section, the person's
// own, the admin's and Channels, in the person's order, and where the
// server lists it.
function moveMenu(c, anchor) {
  const here = keyOf(c, store.sidebar, shape);
  const home = homeKey(c, shape);
  const all = arrange([], store.sidebar, store.sections, shape, title).filter((x) => isSection(x.key) || x.key === home);
  openMenu(anchor, [
    { head: 'Move ' + title(c) + ' to' },
    ...all.map((x) => ({ label: x.key === home ? x.name + ' (where the server lists it)' : x.name, checked: here === x.key, onclick: () => moveTo(c, x.key) })),
    { sep: true },
    { label: 'New section…', icon: 'plus', onclick: () => newSection((key) => moveTo(c, key)) },
    { label: 'Move up', hint: 'Alt+Up', onclick: () => moveBy(c, -1) },
    { label: 'Move down', hint: 'Alt+Down', onclick: () => moveBy(c, 1) },
    { label: 'Reorder…', onclick: () => { if (sidebar) { sidebar.reordering = true; sidebar.draw(); refocus(c.id); } } },
  ], { label: 'Move ' + title(c), alignRight: !!anchor.getBoundingClientRect });
}

// headingMenu: how the heading sorts, and for the person's own sections
// rename and delete; then moving it, a new section, and resetting.
function headingMenu(g, anchor) {
  const sortTo = (sort) => send([{ op: 'set_sort', key: g.key, sort }], `${g.name} sorted by ${SORTS.find(([v]) => v === sort)[1].toLowerCase()}`);
  const movable = g.key !== 'chats' && g.key !== 'sealed';
  openMenu(anchor, [
    { head: 'Sort by' },
    ...SORTS.map(([v, label]) => ({ label, checked: g.sort === v, onclick: () => sortTo(v) })),
    { sep: true },
    g.own && { label: 'Rename…', icon: 'edit', onclick: async () => {
      const name = await promptDialog('Rename section', 'Name', { value: g.name, maxLength: 64 });
      if (name && name.trim()) send([{ op: 'rename_section', key: g.key, name: name.trim() }], `Renamed to ${name.trim()}`);
    } },
    g.own && { label: 'Delete section…', icon: 'trash', danger: true, onclick: async () => {
      if (await confirmDialog('Delete ' + g.name + '?', 'Its conversations go back to where the server lists them. Nothing else is deleted.', { yes: 'Delete', danger: true })) send([{ op: 'delete_section', key: g.key }], `${g.name} deleted`);
    } },
    movable && { label: 'Move up', hint: 'Alt+Up', onclick: () => moveHeadingBy(g.key, -1) },
    movable && { label: 'Move down', hint: 'Alt+Down', onclick: () => moveHeadingBy(g.key, 1) },
    { label: g.key === 'chats' || g.key === 'sealed' ? 'Reset to last activity' : 'Reset to alphabetical', onclick: () => send([{ op: 'reset_section', key: g.key }], `${g.name} reset`) },
    { sep: true },
    { label: 'New section…', icon: 'plus', onclick: () => newSection() },
    { label: 'Reset my sidebar…', onclick: async () => {
      if (await confirmDialog('Reset your sidebar?', 'Your own sections go, and every heading goes back to the server’s default order and sort. Stars stay.', { yes: 'Reset' })) send([{ op: 'reset_all' }], 'Sidebar reset to the server’s default');
    } },
  ], { label: 'Menu for the heading ' + g.name, alignRight: true });
}

// Drag and drop: a row onto another row puts it before that one (under
// that row's heading), onto a heading's empty space at its end; a heading
// onto another heading puts it before that one.
function dragAndDrop(side) {
  const box = side.groups;
  let drag = null; // { kind: 'row'|'heading', id }
  const clear = () => box.querySelectorAll('.drop-before, .drop-into').forEach((el) => el.classList.remove('drop-before', 'drop-into'));
  const targetOf = (e) => {
    if (!drag) return null;
    const row = e.target.closest && e.target.closest('.side-row:not(.child)');
    const sec = e.target.closest && e.target.closest('.side-group');
    if (!sec) return null;
    const g = side.drawn.find((x) => x.key === sec.dataset.group);
    if (!g) return null;
    if (drag.kind === 'heading') return g.key === 'chats' || g.key === 'sealed' || g.key === drag.id ? null : { el: sec.querySelector('h2'), g };
    const c = conversation(drag.id);
    // A row goes under any section, the person's own or an admin's or
    // Channels, under its own heading, or back home; Chats and Sealed take
    // only their own.
    if (!(isSection(g.key) || g.key === keyOf(c, store.sidebar, shape) || g.key === homeKey(c, shape))) return null;
    if (row && row.dataset.conversation !== drag.id) {
      const index = g.items.findIndex((x) => x.c.id === row.dataset.conversation);
      return { el: row, g, index };
    }
    return { el: sec, g, index: g.items.length, into: true };
  };
  box.addEventListener('dragstart', (e) => {
    const row = e.target.closest && e.target.closest('.side-row[draggable="true"]');
    const sec = !row && e.target.closest && e.target.closest('.side-group[draggable="true"]');
    if (!row && !sec) return;
    drag = row ? { kind: 'row', id: row.dataset.conversation } : { kind: 'heading', id: sec.dataset.group };
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', drag.id);
  });
  box.addEventListener('dragover', (e) => {
    const t = targetOf(e);
    clear();
    if (!t) return;
    e.preventDefault();
    t.el.classList.add(t.into ? 'drop-into' : 'drop-before');
  });
  box.addEventListener('dragleave', (e) => { if (!box.contains(e.relatedTarget)) clear(); });
  box.addEventListener('dragend', () => { clear(); drag = null; });
  box.addEventListener('drop', (e) => {
    const t = targetOf(e);
    clear();
    if (!t) return;
    e.preventDefault();
    const d = drag;
    drag = null;
    if (d.kind === 'heading') {
      const keys = side.drawn.map((x) => x.key).filter((k) => k !== 'chats' && k !== 'sealed');
      const all = arrange([], store.sidebar, store.sections, shape, title).map((x) => x.key).filter((k) => k !== 'chats' && k !== 'sealed');
      const without = all.filter((k) => k !== d.id);
      const index = Math.max(0, without.indexOf(t.g.key));
      if (keys.includes(t.g.key)) send([{ op: 'move_section', key: d.id, index }], `Heading moved to ${index + 1} of ${all.length}`);
      return;
    }
    const c = conversation(d.id);
    const from = keyOf(c, store.sidebar, shape);
    if (t.g.key === from) {
      const at = t.g.items.findIndex((x) => x.c.id === c.id);
      const index = t.index > at ? t.index - 1 : t.index;
      if (index === at) return;
      send(reorderOps(t.g, c.id, index, store.sidebar), () => placeText(c));
    } else {
      send(moveOps(c, t.g, t.index, store.sidebar), () => placeText(c));
    }
  });
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
    { label: m.starred ? 'Remove star' : 'Star', hint: m.starred ? '' : 'keeps it at the top of its heading', icon: 'star', onclick: () => set({ starred: !m.starred }) },
    { label: 'Move…', icon: 'chevron-right', onclick: () => moveMenu(c, anchor) },
    isDirectChat(c) && c.chat_state !== 'closed' && { label: c.chat_state === 'waiting' ? 'Withdraw request…' : 'Close chat…', icon: 'lock', onclick: () => closeChat(c) },
    m.unread_count > 0
      ? { label: 'Mark read', icon: 'check', onclick: () => markRead(c.id) }
      : { label: 'Mark unread', icon: 'dot', onclick: async () => {
        try { const out = await put(`/conversations/${c.id}/read`, { seq: Math.max(0, c.last_seq - 1) }); c.membership = out.membership; store.emit('conversations'); } catch (err) { toast(err.message, true); } } },
  ], { label: 'Menu for ' + title(c), alignRight: !!anchor.getBoundingClientRect });
}
