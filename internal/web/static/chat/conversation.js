// One conversation: its header, its messages in the threaded or the flat
// view with the moving marker, and its composer (docs/CLIENTS.md section
// 9.3).

import { get, post, put } from '/shared/api.js';
import { h, icon, iconButton, button, fill, toast, openMenu, openDialog, confirmDialog, copyText, dayName, sameDay, plural, busy } from '/shared/dom.js';
import { renderMessage, firstLine } from '/shared/message/message.js';
import { renderMarkdown } from '/shared/md/render.js';
import { channelIcon, detailLine } from '/shared/pick.js';
import { presence } from '/shared/icons.js';
import { knocksBar } from './cmddialogs.js';
import { store, conversation, room, openRoom, loadOlder, loadNewer, markRead, title, isSealed, isAdHoc, actsAsManager, isMuted, account, nameOf, canPost, isLarge, onlineIn } from './store.js';
import { makeEnv, look, mayEdit, deleteMessage, togglePin, mdContext } from './env.js';
import { Composer } from './composer.js';
import { ui, linkTo } from './ui.js';
import { layout } from './order.js';
import { openModes, updateMembership } from './modes.js';
import { openSettings } from './settings.js';
import { callButton } from './call/index.js';
import { mountSealed } from './sealed/index.js';

// What a bot said to this person alone, by conversation. It is not a
// message: it is kept only while the page is open.
export const privateResponses = new Map();

// reconcile makes a container hold exactly these rows, in order, building
// only the ones that changed. rows are { key, sig, make }.
export function reconcile(container, cache, rows) {
  const next = new Map();
  const els = [];
  for (const row of rows) {
    const old = cache.get(row.key);
    const entry = old && old.sig === row.sig ? old : { sig: row.sig, el: row.make() };
    next.set(row.key, entry);
    els.push(entry.el);
  }
  let cur = container.firstChild;
  for (const el of els) {
    if (cur === el) cur = cur.nextSibling; else container.insertBefore(el, cur);
  }
  while (cur) { const gone = cur; cur = cur.nextSibling; gone.remove(); }
  cache.clear();
  for (const [k, v] of next) cache.set(k, v);
}

// Names, emoji and groups arrive after the messages that use them; when
// they do, every row is drawn again.
let epoch = 0;
store.on('accounts', () => { epoch++; });
store.on('lists', () => { epoch++; });
store.on('seen', () => { epoch++; });
// A hub's record of a channel changed: who is a moderator may have too.
store.on('hub', () => { epoch++; });
export const drawEpoch = () => epoch;

const FIVE_MINUTES = 5 * 60 * 1000;

export class ConversationView {
  constructor(id, opts = {}) {
    this.id = id;
    this.target = opts.messageId || null;
    this.cache = new Map();
    this.first = true;
    this.stick = true;
    const self = this;
    this.env = makeEnv({
      reply: (m, quote) => ui.openThread(this.id, m.parent_id || m.id, { quote: quote || (m.parent_id ? { message_id: m.id } : null), quoteOf: m }),
      edit: (m) => this.composer && this.composer.startEdit(m),
      redraw: () => this.draw(),
      retry: (m) => this.composer && this.composer.retry(m),
      get flat() { return self.view() === 'flat'; },
    });
    this.header = h('header', { class: 'conv-head' });
    this.list = h('div', { class: 'messages', role: 'log', 'aria-label': 'Messages', tabIndex: -1 });
    this.scroller = h('div', { class: 'conv-scroll' }, this.list);
    this.jump = h('button', { type: 'button', class: 'btn small jump-newest', hidden: true, onclick: () => this.toNewest() }, icon('arrow-down', null, 'small'), h('span', {}, 'Newest'));
    this.foot = h('div', { class: 'conv-foot' });
    this.dropNote = h('div', { class: 'drop-note', hidden: true }, icon('attach'), ' Drop files to attach them');
    this.el = h('section', { class: 'conv', 'aria-label': 'Conversation' }, this.header, h('div', { class: 'conv-body' }, this.scroller, this.jump, this.dropNote), this.foot);

    this.scroller.addEventListener('scroll', () => this.scrolled(), { passive: true });
    this.list.addEventListener('keydown', (e) => this.keys(e));
    this.el.addEventListener('dragover', (e) => { if (this.composer && e.dataTransfer && [...e.dataTransfer.types].includes('Files')) { e.preventDefault(); this.dropNote.hidden = false; } });
    this.el.addEventListener('dragleave', (e) => { if (!this.el.contains(e.relatedTarget)) this.dropNote.hidden = true; });
    this.el.addEventListener('drop', (e) => { this.dropNote.hidden = true; if (this.composer && e.dataTransfer && e.dataTransfer.files.length) { e.preventDefault(); this.composer.addFiles(e.dataTransfer.files); } });

    this.onFocus = () => this.maybeRead();
    window.addEventListener('focus', this.onFocus);
    document.addEventListener('visibilitychange', this.onFocus);
    this.off = [
      store.on('messages', (cid) => { if (cid === this.id) this.draw(); }),
      store.on('conversations', () => { this.drawHeader(); this.drawFoot(); }),
      store.on('counts', () => this.drawHeader()),
      store.on('accounts', () => { this.drawHeader(); this.draw(); }),
      store.on('seen', () => { this.drawHeader(); this.draw(); }),
      store.on('lists', () => this.draw()),
      store.on('threads', () => this.draw()),
      store.on('reload', (cid) => { if (cid === this.id) this.load(); }),
      store.on('message.created', (e) => {
        if (e.conversation_id !== this.id || e.message.author_id === store.me.id) return;
        // New messages in the open conversation are announced politely.
        if (!e.message.parent_id || this.view() === 'flat') ui.announce(`${nameOf(e.message.author_id)}: ${firstLine(e.message, this.env, 160)}`);
      }),
      // `/clear` (ruling R74): to the newest message, which then reads it.
      store.on('toNewest', (cid) => { if (cid === this.id) this.toNewest(); }),
    ];
    // Requests to join a private channel, for its managers (ruling R74).
    const knocks = knocksBar(id);
    this.el.insertBefore(knocks.el, this.el.children[1]);
    this.off.push(knocks.stop);
    this.drawHeader();
    this.drawFoot();
    this.load();
  }

  destroy() {
    for (const off of this.off) off();
    window.removeEventListener('focus', this.onFocus);
    document.removeEventListener('visibilitychange', this.onFocus);
    if (this.composer) this.composer.destroy();
    if (this.sealed) this.sealed.destroy();
  }

  conv() { return conversation(this.id); }

  // The view is the person's preference, with an override for this
  // conversation; both are kept on the server.
  view() {
    const c = this.conv();
    return (c && c.membership && c.membership.thread_view) || (store.me.prefs && store.me.prefs.thread_view) || 'threaded';
  }

  async load() {
    const c = this.conv();
    // A sealed chat draws its own messages and composer (chat/sealed).
    if (c && isSealed(c)) { mountSealed(this, c); return; }
    fill(this.list, h('p', { class: 'loading' }, 'Loading'));
    this.cache.clear();
    try {
      let around;
      if (this.target) {
        try { around = (await get('/messages/' + this.target)).message.seq; } catch { this.target = null; toast('That message can no longer be read.', true); }
      }
      await openRoom(this.id, around);
      this.draw();
    } catch (err) {
      fill(this.list, h('p', { class: 'problem' }, err.code === 'not_found' ? 'This conversation does not exist, or you may not see it.' : err.message));
    }
  }

  // jumpTo shows one message of this conversation: scrolled to if it is
  // loaded, fetched with its neighbours if it is not.
  jumpTo(messageId) {
    const el = this.list.querySelector(`[data-id="${messageId}"]`);
    if (el) { el.scrollIntoView({ block: 'center' }); el.classList.remove('flash'); requestAnimationFrame(() => el.classList.add('flash')); return; }
    this.target = messageId;
    this.first = true;
    room(this.id).loaded = false;
    this.load();
  }

  // --- the header ---

  drawHeader() {
    const c = this.conv();
    if (!c) { fill(this.header, iconButton('back', 'Back to the list', () => ui.go('/chat/'), 'only-compact'), h('h1', {}, 'Conversation')); return; }
    const parent = c.parent_id && conversation(c.parent_id);
    const others = isAdHoc(c) || isSealed(c) ? (c.member_ids || []).filter((x) => x !== store.me.id) : [];
    const one = others.length === 1 ? account(others[0]) : null;
    const menuBtn = iconButton('more', 'Conversation menu', (e) => this.menu(e.currentTarget));
    fill(this.header,
      iconButton('back', 'Back to the list', () => ui.go('/chat/'), 'only-compact'),
      h('div', { class: 'conv-title' },
        h('h1', { title: detailLine(c, look) },
          channelIcon(c),
          parent ? [h('button', { type: 'button', class: 'link-btn parent', onclick: () => ui.openConversation(parent.id) }, h('bdi', {}, title(parent))), h('span', { class: 'dim', 'aria-hidden': 'true' }, ' › ')] : null,
          h('bdi', {}, title(c)),
          c.locked_at ? h('span', { class: 'tag line', title: 'Locked: nobody can be added or join' }, 'locked') : null,
          c.archived_at ? h('span', { class: 'tag grey' }, 'archived') : null,
          c.shared ? h('span', { class: 'tag cyan', title: 'Shared with ' + (c.shared.servers || []).join(', ') }, 'shared') : null,
          isMuted(c) ? icon('bell-off', 'Muted', 'small') : null),
        h('p', { class: 'conv-topic clip' },
          one ? [presenceDot(one, isSealed(c)), ' ', one.status ? `${one.status.emoji || ''} ${one.status.text || ''}`.trim() : (one.presence || 'offline'), one.dnd ? ' · do not disturb' : ''] : null,
          !one && c.topic ? c.topic : null,
          !one && !c.topic && c.kind === 'channel' ? plural(c.member_count || 0, 'member') : null,
          // A large channel shows how many are online, not who.
          !one && isLarge(c) && onlineIn(c.id) != null ? h('span', { class: 'dim online-count' }, `${c.topic || c.kind === 'channel' ? ' · ' : ''}${onlineIn(c.id)} online`) : null)),
      h('div', { class: 'conv-tools' },
        callButton(c),
        c.kind === 'channel' ? iconButton('search', 'Search in this conversation', () => ui.go('/chat/search?in=' + c.id)) : null,
        c.kind === 'channel' ? iconButton('pin', 'Pinned messages', () => this.pins()) : null,
        // An announcement channel shows its members to its managers only.
        c.kind === 'channel' && (!c.announcement || actsAsManager(c)) ? iconButton('people', `Members (${c.member_count || 0})`, () => openSettings(c.id, 'members')) : null,
        menuBtn));
  }

  menu(anchor) {
    const c = this.conv();
    if (!c) return;
    const m = c.membership;
    const view = this.view();
    const setView = (v) => updateMembership(c.id, { thread_view: v });
    openMenu(anchor, [
      m && { label: 'Notifications and sidebar…', icon: 'bell', onclick: () => openModes(c.id) },
      m && { head: 'Thread view' },
      m && { label: 'Threaded', checked: view === 'threaded', onclick: () => setView('threaded') },
      m && { label: 'Flat', checked: view === 'flat', onclick: () => setView('flat') },
      m && m.thread_view && { label: 'Use my default', onclick: () => setView(null) },
      { sep: true },
      m && { label: m.starred ? 'Remove star' : 'Star', icon: 'star', onclick: () => updateMembership(c.id, { starred: !m.starred }) },
      m && { label: 'Mark read', icon: 'check', onclick: () => markRead(c.id) },
      c.kind === 'channel' && { label: 'Pinned messages', icon: 'pin', onclick: () => this.pins() },
      c.kind === 'channel' && { label: 'Files posted here', icon: 'file', onclick: () => ui.go(`/chat/search?in=${c.id}&has_file=1`) },
      c.kind === 'channel' && c.name && !c.parent_id && { label: 'Sub-channels', icon: 'hash', onclick: () => offerSubChannels(c, true) },
      { label: 'Copy link', icon: 'link', onclick: () => copyText(linkTo(c.id), 'Link copied') },
      { sep: true },
      c.kind === 'channel' && m && { label: actsAsManager(c) ? 'Settings' : 'About and members', icon: 'settings', onclick: () => openSettings(c.id) },
      m && { label: 'Leave…', icon: 'sign-out', danger: true, onclick: () => leave(c) },
    ], { alignRight: true, label: 'Conversation menu' });
  }

  pins() {
    const c = this.conv();
    openDialog('Pinned in ' + title(c), (close) => {
      const body = h('div', { class: 'stack' }, h('p', { class: 'loading' }, 'Loading'));
      get(`/conversations/${c.id}/pins`).then(({ pins }) => {
        fill(body, pins.length ? pins.map((p) => h('div', { class: 'pin' },
          p.message ? renderMessage(p.message, this.env, { bare: true }) : h('p', { class: 'dim' }, 'A message that can no longer be read.'),
          h('p', { class: 'row' }, h('span', { class: 'dim small grow' }, `Pinned by ${nameOf(p.pinned_by_id)}`),
            button('Go to it', () => { close(); ui.openConversation(c.id, p.message && p.message.parent_id ? { threadId: p.message.parent_id } : { messageId: p.message_id }); }, 'small'))))
          : h('p', { class: 'dim' }, 'Nothing is pinned here. A manager pins a message from its menu.'));
      }).catch((err) => fill(body, h('p', { class: 'problem' }, err.message)));
      return { body, foot: [button('Close', () => close())] };
    }, { wide: true });
  }

  // --- the composer, or what stands in for it ---

  drawFoot() {
    const c = this.conv();
    if (!c) { fill(this.foot); return; }
    const m = c.membership;
    let kind = 'composer', locked = null;
    if (isSealed(c)) kind = 'sealed';
    else if (!m) kind = 'join';
    else if (c.archived_at) locked = 'This channel is archived. It can be read and searched, and takes no more posts.';
    else if (c.read_only) locked = 'This conversation is read-only: the link to the other server has gone.';
    else if (m.level === 'reader') locked = 'This is an announcement channel for you: you can read and react here, and not post.';
    else if (!canPost(c)) locked = c.announcement_replies ? 'This is an announcement channel: only its managers and the people they allow post here. You can read, react and reply in threads.'
      : 'This is an announcement channel: only its managers and the people they allow post here. You can read and react.';
    else if (m.timed_out_until && new Date(m.timed_out_until) > new Date()) locked = `You are timed out here until ${new Date(m.timed_out_until).toLocaleString()}. You can still read.`;
    const sig = kind + '|' + (locked || '');
    if (this.footSig === sig) return;
    this.footSig = sig;
    if (kind !== 'composer' && this.composer) { this.composer.destroy(); this.composer = null; }
    if (kind === 'sealed') return; // drawn by chat/sealed
    if (kind === 'join') {
      // A public channel opened without joining can be read and searched
      // and nothing more.
      const join = button('Join to take part', () => busy(join, async () => {
        const out = await post(`/conversations/${c.id}/join`);
        store.conversations.set(c.id, out.conversation); store.others.delete(c.id);
        store.emit('conversations');
        offerSubChannels(out.conversation, false);
      }), 'primary');
      fill(this.foot, h('div', { class: 'join-bar' }, h('span', {}, 'You are reading this channel without being in it.'), join));
      return;
    }
    if (!this.composer) {
      this.composer = new Composer({
        conversationId: this.id, label: 'Message ' + title(c),
        onSent: () => { this.stick = true; },
        onEditLast: () => {
          const r = room(this.id);
          const last = [...r.list].reverse().find((x) => x.author_id === store.me.id && !x.parent_id && mayEdit(x));
          if (last) this.composer.startEdit(last);
        },
      });
      fill(this.foot, this.composer.el);
    }
    this.composer.setLocked(locked);
  }

  // --- the messages ---

  rows() {
    const r = room(this.id);
    const c = this.conv();
    const view = this.view();
    const flat = view === 'flat';
    const rows = [];
    // What is drawn, in order, and where the "New" line goes (order.js).
    const items = layout({ list: r.list, flat, threadStates: store.threadStates, readAt: r.readAtOpen, meId: store.me.id, member: !!(c && c.membership), lastSeq: c ? c.last_seq : 0 });

    if (r.hasOlder) rows.push({ key: 'older', sig: String(!!r.loadingOlder), make: () => h('div', { class: 'load-more' }, button('Load older messages', () => this.older(), 'small')) });
    else if (c) rows.push({ key: 'start', sig: epoch + (c.name || ''), make: () => this.beginning(c) });

    let prev = null;
    for (const it of items) {
      const m = it.m || it.marker;
      const at = it.m ? m.created_at : m.thread.last_reply_at;
      if (!prev || !sameDay(prev.at, at)) {
        rows.push({ key: 'day-' + new Date(at).toDateString(), sig: '', make: () => h('div', { class: 'day', role: 'separator', 'aria-label': dayName(at) }, h('span', {}, dayName(at))) });
        prev = null;
      }
      if (it.newBefore) {
        if (rows.some((x) => x.key.startsWith('m-'))) rows.push({ key: 'new', sig: '', make: () => h('div', { class: 'new-line', role: 'separator', 'aria-label': 'New messages' }, h('span', {}, 'New')) });
        prev = null;
      }
      if (it.marker) {
        rows.push({ key: 'marker-' + m.id, sig: `${it.unread}|${m._v}|${epoch}`, make: () => this.marker(m, it.unread) });
        prev = null;
        continue;
      }
      const system = !!(m.system || (m.via && m.via.kind === 'system'));
      // A message tagged as late starts a group of its own, so the tag sits
      // beside its author's name.
      const grouped = !!(prev && !it.late && !system && !prev.system && prev.author === m.author_id && (prev.parent || null) === (m.parent_id || null)
        && new Date(m.created_at) - new Date(prev.at) < FIVE_MINUTES && !m.via && !prev.via);
      const st = store.threadStates.get(m.id);
      const late = !!it.late;
      rows.push({
        key: 'm-' + m.id,
        sig: `${m._v}|${grouped}|${late}|${view}|${epoch}|${st ? st.state + st.unread_count : ''}|${store.bookmarks.has(m.id)}|${store.blocked.has(m.author_id)}|${m.quote ? (this.env.quoted(m.quote.message_id) || {})._v : ''}`,
        make: () => renderMessage(m, this.env, { grouped, flat, late }),
      });
      prev = { author: m.author_id, at: m.created_at, system, parent: m.parent_id, via: m.via };
    }
    for (const p of privateResponses.get(this.id) || []) {
      rows.push({ key: 'private-' + p.id, sig: String(epoch), make: () => privateRow(p, this.env) });
    }
    if (r.hasNewer) rows.push({ key: 'newer', sig: '', make: () => h('div', { class: 'load-more' }, button('Load newer messages', () => loadNewer(this.id), 'small')) });
    return rows;
  }

  beginning(c) {
    return h('div', { class: 'conv-start' },
      h('h2', {}, channelIcon(c), ' ', h('bdi', {}, title(c))),
      h('p', { class: 'dim' }, isAdHoc(c) ? 'This is the start of this chat. Anyone added later sees the whole history.'
        : `This is the start of ${title(c)}.${c.private ? ' It is private: only its members can read it.' : ''}`),
      c.welcome ? renderMarkdown(c.welcome, [], mdContext({ conversation_id: c.id })) : null);
  }

  marker(parent, unread) {
    const text = `${unread ? plural(unread, 'new reply', 'new replies') : 'New replies'} to: ${firstLine(parent, this.env, 70)}`;
    const setState = async (state) => {
      try {
        const out = await put(`/messages/${parent.id}/thread/state`, { state });
        if (out.thread_state) store.threadStates.set(parent.id, out.thread_state); else store.threadStates.delete(parent.id);
        store.emit('threads', parent.id);
      } catch (err) { toast(err.message, true); }
    };
    return h('div', { class: 'marker', role: 'note', dataset: { parent: parent.id } },
      h('button', { type: 'button', class: 'marker-open', onclick: () => ui.openThread(this.id, parent.id) }, icon('thread', null, 'small'), h('span', {}, text)),
      iconButton('more', 'Follow or ignore this thread', (e) => openMenu(e.currentTarget, [
        { label: 'Follow thread', icon: 'bell', onclick: () => setState('following') },
        { label: 'Ignore thread', icon: 'eye-off', onclick: () => setState('ignored') },
      ], { alignRight: true, label: 'Thread' }), 'small'));
  }

  draw() {
    const r = room(this.id);
    if (!r.loaded) return;
    const sc = this.scroller;
    const wasBottom = this.stick || this.atBottom();
    const beforeHeight = sc.scrollHeight, beforeTop = sc.scrollTop;
    if (this.cache.size === 0) fill(this.list);
    reconcile(this.list, this.cache, this.rows());

    if (this.first) {
      this.first = false;
      const target = this.target && this.list.querySelector(`[data-id="${this.target}"]`);
      const fresh = this.list.querySelector('.new-line');
      if (target) { target.scrollIntoView({ block: 'center' }); target.classList.add('flash'); this.stick = false; }
      else if (fresh && fresh.offsetTop > sc.clientHeight / 2) { sc.scrollTop = fresh.offsetTop - 60; this.stick = false; }
      else sc.scrollTop = sc.scrollHeight;
    } else if (this.keepOffset) {
      // Older messages were added above: what was on screen stays put.
      sc.scrollTop = beforeTop + (sc.scrollHeight - beforeHeight);
      this.keepOffset = false;
    } else if (wasBottom && !r.hasNewer) {
      sc.scrollTop = sc.scrollHeight;
    }
    this.scrolled();
  }

  atBottom() {
    const sc = this.scroller;
    return sc.scrollHeight - sc.scrollTop - sc.clientHeight < 48;
  }

  scrolled() {
    const r = room(this.id);
    this.stick = this.atBottom();
    this.jump.hidden = this.stick && !r.hasNewer;
    if (this.scroller.scrollTop < 120 && r.loaded && r.hasOlder && !r.loadingOlder && !this.first) this.older();
    this.maybeRead();
  }

  async older() {
    this.keepOffset = true;
    try { if (!(await loadOlder(this.id))) this.keepOffset = false; } catch (err) { this.keepOffset = false; toast(err.message, true); }
  }

  async toNewest() {
    const r = room(this.id);
    if (r.hasNewer) { this.target = null; this.first = true; r.loaded = false; await this.load(); return; }
    this.scroller.scrollTop = this.scroller.scrollHeight;
  }

  // The read position moves when the newest message has been on screen
  // with the page in front.
  maybeRead() {
    const c = this.conv();
    const r = room(this.id);
    if (!c || !c.membership || !r.loaded || r.hasNewer) return;
    if (document.visibilityState !== 'visible' || !document.hasFocus()) return;
    if (this.atBottom()) markRead(this.id);
  }

  // With a message focused: R, E, T, P and Delete act on it, and the
  // arrows move between messages.
  keys(e) {
    const el = e.target.closest && e.target.closest('.msg');
    if (!el || e.target !== el || e.metaKey || e.ctrlKey || e.altKey) return;
    const m = room(this.id).byId.get(el.dataset.id);
    if (!m) return;
    const k = e.key.toLowerCase();
    const move = (dir) => { let n = el; do { n = dir > 0 ? n.nextElementSibling : n.previousElementSibling; } while (n && !n.classList.contains('msg')); if (n) n.focus(); };
    if (e.key === 'ArrowDown') { e.preventDefault(); move(1); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); move(-1); }
    else if (k === 'r') { e.preventDefault(); this.env.on.react(m, el); }
    else if (k === 'e') { e.preventDefault(); if (mayEdit(m) && this.composer) this.composer.startEdit(m); }
    else if (k === 't') { e.preventDefault(); this.env.on.reply(m); }
    else if (k === 'p') { e.preventDefault(); togglePin(m); }
    else if (e.key === 'Delete' || e.key === 'Backspace') { e.preventDefault(); deleteMessage(m); }
    else if (e.key === 'Enter' && m.thread && m.thread.reply_count) { e.preventDefault(); ui.openThread(this.id, m.id); }
  }
}

export function presenceDot(a, sealed) {
  // Not by colour alone: each state has its own shape, and in a sealed
  // chat the square becomes a shield.
  const p = a && a.dnd ? 'dnd' : (a && a.presence) || 'offline';
  return presence(p, { sealed: !!sealed });
}

function privateRow(p, env) {
  const bot = account(p.botId) || { id: p.botId, display_name: 'Bot' };
  return h('div', { class: 'msg private', role: 'note' },
    h('div', { class: 'msg-gutter' }, icon('eye')),
    h('div', { class: 'msg-main' },
      h('header', { class: 'msg-head' }, h('span', { class: 'msg-author' }, bot.display_name || bot.username), h('span', { class: 'tag yellow' }, 'only you can see this')),
      p.response.text ? renderMarkdown(p.response.text, p.response.entities, env.md({ conversation_id: p.conversationId })) : null));
}

// leave says what leaving this particular conversation means before it
// happens.
async function leave(c) {
  const m = c.membership;
  if (isSealed(c)) { toast('A sealed chat cannot be left. Hide it from the sidebar instead.', true); return; }
  if (m && m.via_group && !m.direct) {
    toast('You are in this channel through a group, so you cannot leave it by yourself. Leave the group, or ask a manager.', true);
    return;
  }
  const notes = [`You will stop getting messages from ${title(c)}.`];
  if (c.locked_at) notes.push('This channel is locked: once you leave, there is no way back.');
  if ((c.private || isAdHoc(c)) && c.member_count <= 1) notes.push('You are the last one here. The channel and everything in it will be deleted.');
  else if (c.private) notes.push('It is private: to come back, someone in it has to add you.');
  if (!(await confirmDialog('Leave ' + title(c) + '?', notes, { yes: 'Leave', danger: true }))) return;
  try {
    await post(`/conversations/${c.id}/leave`);
    store.conversations.delete(c.id);
    store.emit('conversations');
    ui.go('/chat/');
  } catch (err) { toast(err.message, true); }
}

// After joining a channel that has public sub-channels, they are shown in
// a short list with their member counts and topics, none ticked, and
// "Join selected". Skipping it joins none. The same list is in the
// parent's menu afterwards (docs/CLIENTS.md section 9.25).
export async function offerSubChannels(parent, always) {
  let subs = [];
  try { subs = (await get('/channels', { parent_id: parent.id, limit: 100 })).conversations.filter((c) => !c.private || c.membership); } catch { return; }
  const open = subs.filter((c) => !c.membership);
  if (!subs.length && !always) return;
  openDialog('Sub-channels of ' + title(parent), (close) => {
    const boxes = open.map((c) => ({ c, input: h('input', { type: 'checkbox' }) }));
    const join = button('Join selected', () => busy(join, async () => {
      for (const b of boxes.filter((x) => x.input.checked)) {
        try { const out = await post(`/conversations/${b.c.id}/join`); store.conversations.set(b.c.id, out.conversation); } catch (err) { toast(`${b.c.name}: ${err.message}`, true); }
      }
      store.emit('conversations');
      close();
    }), 'primary');
    const mine = subs.filter((c) => c.membership);
    return {
      body: [
        open.length ? h('p', {}, 'Joining a channel does not join the channels under it. These are the public ones:') : h('p', { class: 'dim' }, subs.length ? 'You are in every public sub-channel.' : 'This channel has no public sub-channels.'),
        open.length ? h('ul', { class: 'rows' }, boxes.map((b) => h('li', {}, h('label', { class: 'check grow' }, b.input, h('span', {}, h('bdi', {}, b.c.name), h('small', {}, [plural(b.c.member_count, 'member'), b.c.topic].filter(Boolean).join(' · '))))))) : null,
        mine.length ? h('p', { class: 'dim small' }, 'You are in: ' + mine.map((c) => c.name).join(', ')) : null,
      ],
      foot: [button(open.length ? 'Skip' : 'Close', () => close()), open.length ? join : null],
    };
  });
}

