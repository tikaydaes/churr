// The thread pane: the parent message, then its replies oldest first, each
// with its quote if it has one; a header with Follow or Ignore and "Move
// to a channel"; and a composer at the bottom (docs/CLIENTS.md section
// 9.4).

import { get, put } from '/shared/api.js';
import { h, icon, iconButton, button, fill, toast, plural } from '/shared/dom.js';
import { renderMessage } from '/shared/message/message.js';
import { store, conversation, account, applyToRoom, title, canPost, actsAsManager, isSealed, findMessage } from './store.js';
import { makeEnv, mayEdit, moveThread } from './env.js';
import { Composer } from './composer.js';
import { reconcile, drawEpoch } from './conversation.js';
import { ui } from './ui.js';
import { sortByOrder, seqRange } from './order.js';

export class ThreadView {
  constructor(conversationId, parentId, opts = {}) {
    this.cid = conversationId;
    this.pid = parentId;
    // The pane keeps its own copy of the thread, and follows the live
    // connection for it: a thread can be longer than what the
    // conversation has loaded.
    this.r = { list: [], byId: new Map(), synced: 0, pending: [] };
    this.cache = new Map();
    this.hasOlder = false;
    this.loaded = false;
    this.env = makeEnv({
      inThread: true,
      // A reply to a reply stays in this thread and quotes that reply.
      reply: (m, quote) => { if (this.composer) { this.composer.setQuote(quote || (m.id !== this.pid ? { message_id: m.id } : null), m); this.composer.focus(); } },
      edit: (m) => this.composer && this.composer.startEdit(m),
      redraw: () => this.draw(),
      retry: (m) => this.composer && this.composer.retry(m),
    });
    this.header = h('header', { class: 'conv-head thread-head' });
    this.tools = h('div', { class: 'thread-tools', role: 'toolbar', 'aria-label': 'This thread' });
    this.list = h('div', { class: 'messages', role: 'log', 'aria-label': 'Thread' });
    this.scroller = h('div', { class: 'conv-scroll' }, this.list);
    this.foot = h('div', { class: 'conv-foot' });
    this.el = h('section', { class: 'conv thread', 'aria-label': 'Thread' }, this.header, this.tools, h('div', { class: 'conv-body' }, this.scroller), this.foot);
    this.off = [
      store.on('event', (e) => this.event(e)),
      store.on('messages', (cid) => { if (cid === this.cid) this.adopt(); }),
      store.on('threads', (pid) => { if (pid === this.pid) this.drawHeader(); }),
      store.on('accounts', () => this.draw()),
      store.on('seen', () => this.draw()),
      store.on('conversations', () => { this.drawHeader(); this.drawFoot(); }),
    ];
    this.onFocus = () => this.read();
    window.addEventListener('focus', this.onFocus);
    this.pendingQuote = opts.quote ? opts : null;
    this.drawHeader();
    this.load();
  }

  destroy() {
    for (const off of this.off) off();
    window.removeEventListener('focus', this.onFocus);
    if (this.composer) this.composer.destroy();
  }

  parent() { return this.r.byId.get(this.pid); }
  // Replies are shown by `order`; they are paged by `seq`.
  replies() { return sortByOrder(this.r.list.filter((m) => m.id !== this.pid)); }
  put(m) { if (this.r.byId.has(m.id)) { const old = this.r.byId.get(m.id); const v = (old._v || 0) + 1; Object.assign(old, m); old._v = v; } else { m._v = m._v || 0; this.r.byId.set(m.id, m); this.r.list.push(m); } }

  async load() {
    fill(this.list, h('p', { class: 'loading' }, 'Loading'));
    try {
      const out = await get(`/messages/${this.pid}/thread`, { limit: 100 });
      this.pid = out.parent.id; // the address may have named a reply
      this.put({ ...out.parent });
      for (const m of out.messages) this.put({ ...m });
      if (out.thread_state) {
        // The position may already be past the conversation's marker (read
        // on another device, or just now): the conversation is drawn again.
        store.threadStates.set(this.pid, out.thread_state);
        store.emit('threads', this.pid);
        store.emit('messages', this.cid);
      }
      this.hasOlder = out.has_more;
      this.loaded = true;
      for (const m of this.r.list) { account(m.author_id); }
      this.drawHeader(); this.drawFoot(); this.draw(true);
      if (this.pendingQuote && this.composer) { this.composer.setQuote(this.pendingQuote.quote, this.pendingQuote.quoteOf); this.pendingQuote = null; }
      if (this.composer && window.matchMedia('(min-width: 600px)').matches) this.composer.focus();
      this.read();
    } catch (err) {
      fill(this.list, h('p', { class: 'problem' }, err.code === 'not_found' ? 'This thread does not exist, or you may not see it.' : err.message));
    }
  }

  async older() {
    const range = seqRange(this.replies());
    try {
      const out = await get(`/messages/${this.pid}/thread`, { limit: 100, before: range ? range.min : undefined });
      for (const m of out.messages) this.put({ ...m });
      this.hasOlder = out.has_more;
      this.draw();
    } catch (err) { toast(err.message, true); }
  }

  // Events of this conversation that concern this thread are applied to
  // the pane's own copy.
  event(e) {
    if (!this.loaded || e.conversation_id !== this.cid) return;
    const m = e.message;
    const mine = (m && (m.id === this.pid || m.parent_id === this.pid)) || (e.message_id && this.r.byId.has(e.message_id))
      || (e.pin && this.r.byId.has(e.pin.message_id)) || (e.type === 'thread.moved' && e.parent && e.parent.id === this.pid);
    if (!mine) return;
    if (e.type === 'message.created' && m.parent_id !== this.pid && m.id !== this.pid) return;
    applyToRoom(this.r, { ...e, seq: undefined, message: m ? { ...m } : m, parent: e.parent ? { ...e.parent } : undefined, pointer: e.pointer ? { ...e.pointer } : undefined });
    const stick = this.atBottom();
    this.draw();
    this.drawFoot();
    if (stick) this.scroller.scrollTop = this.scroller.scrollHeight;
    this.read();
  }

  // What the person just posted is in the conversation's room before its
  // event arrives; the pane takes it from there, pending copies included.
  adopt() {
    if (!this.loaded) return;
    const room = store.rooms.get(this.cid);
    if (!room) return;
    let changed = false;
    const live = new Set();
    for (const m of room.list) {
      if (m.parent_id !== this.pid && m.id !== this.pid) continue;
      live.add(m.id);
      const mine = this.r.byId.get(m.id);
      if (!mine) { this.put({ ...m }); changed = true; } else if ((m._pending || m._failed) && mine._v !== m._v) { Object.assign(mine, m); changed = true; }
    }
    // A pending copy that the room has dropped has been sent.
    for (const m of [...this.r.list]) if (m._pending && !live.has(m.id)) { this.r.list.splice(this.r.list.indexOf(m), 1); this.r.byId.delete(m.id); changed = true; }
    if (changed) { const stick = this.atBottom(); this.draw(); if (stick) this.scroller.scrollTop = this.scroller.scrollHeight; }
  }

  moved() { return this.replies().find((m) => m.system && m.system.kind === 'thread_moved'); }

  drawHeader() {
    const c = conversation(this.cid);
    const st = store.threadStates.get(this.pid);
    const parent = this.parent();
    const member = !!(c && c.membership);
    const set = async (state) => {
      try {
        const out = await put(`/messages/${this.pid}/thread/state`, { state });
        if (out.thread_state) store.threadStates.set(this.pid, out.thread_state); else store.threadStates.delete(this.pid);
        store.emit('threads', this.pid);
        store.emit('messages', this.cid);
      } catch (err) { toast(err.message, true); }
    };
    const following = st && st.state === 'following', ignored = st && st.state === 'ignored';
    const mayMove = parent && c && member && !isSealed(c) && !c.shared && !c.archived_at && !this.moved() && (actsAsManager(c) || parent.author_id === store.me.id) && (this.replies().length > 0);
    fill(this.header,
      iconButton('back', 'Back to the conversation', () => ui.closeThread(), 'only-narrow'),
      h('div', { class: 'conv-title' }, h('h1', {}, icon('thread'), ' Thread'), h('p', { class: 'conv-topic clip' }, c ? title(c) : '', parent && parent.thread ? ' · ' + plural(parent.thread.reply_count, 'reply', 'replies') : '')),
      h('div', { class: 'conv-tools' }, iconButton('close', 'Close the thread', () => ui.closeThread(), 'not-narrow')));
    fill(this.tools,
      member && h('button', { type: 'button', class: 'btn small' + (following ? ' primary' : ''), 'aria-pressed': String(!!following), onclick: () => set(following ? 'none' : 'following') }, following ? 'Following' : 'Follow'),
      member && h('button', { type: 'button', class: 'btn small', 'aria-pressed': String(!!ignored), onclick: () => set(ignored ? 'none' : 'ignored') }, ignored ? 'Ignored' : 'Ignore'),
      mayMove ? button('Move to a channel', () => moveThread(parent), 'small') : null);
    this.tools.hidden = !member;
  }

  drawFoot() {
    const c = conversation(this.cid);
    if (!c || !this.loaded) return;
    const moved = this.moved();
    let locked = null;
    if (moved) {
      // The old thread takes no more replies: the link stands where the
      // composer was.
      const to = moved.system.conversation_id;
      const target = conversation(to);
      locked = h('span', {}, 'This thread moved to its own channel and takes no more replies here. ',
        target ? h('button', { type: 'button', class: 'link-btn', onclick: () => ui.openConversation(to) }, 'Open ' + title(target)) : h('span', { class: 'dim' }, 'You are not in that channel.'));
    } else if (!c.membership) locked = 'Join the channel to reply.';
    else if (!canPost(c)) locked = c.archived_at ? 'This channel is archived.' : 'You can read here, and not post.';
    else if (c.membership.timed_out_until && new Date(c.membership.timed_out_until) > new Date()) locked = 'You are timed out here and cannot post.';
    if (!this.composer) {
      this.composer = new Composer({
        conversationId: this.cid, parentId: this.pid, label: 'Reply in thread',
        onSent: () => { setTimeout(() => { this.scroller.scrollTop = this.scroller.scrollHeight; }, 0); },
        onEditLast: () => { const last = [...this.replies()].reverse().find((x) => x.author_id === store.me.id && mayEdit(x)); if (last) this.composer.startEdit(last); },
      });
      fill(this.foot, this.composer.el);
    }
    this.composer.setLocked(locked);
  }

  atBottom() { const sc = this.scroller; return sc.scrollHeight - sc.scrollTop - sc.clientHeight < 48; }

  draw(first) {
    if (!this.loaded) return;
    const parent = this.parent();
    const rows = [];
    const sig = (m, extra) => `${m._v}|${drawEpoch()}|${store.bookmarks.has(m.id)}|${store.blocked.has(m.author_id)}|${extra}|${m.quote ? ((this.r.byId.get(m.quote.message_id) || findMessage(m.quote.message_id) || {})._v) : ''}`;
    if (parent) rows.push({ key: 'm-' + parent.id, sig: sig(parent, 'p'), make: () => { const el = renderMessage(parent, this.env, { inThread: true }); el.classList.add('thread-parent'); return el; } });
    const replies = this.replies();
    rows.push({ key: 'count', sig: String(replies.length) + this.hasOlder, make: () => h('div', { class: 'thread-count', role: 'separator' },
      h('span', {}, replies.length ? plural(replies.length, 'reply', 'replies') : 'No replies yet'),
      this.hasOlder ? button('Earlier replies', () => this.older(), 'small') : null) });
    let prev = null;
    for (const m of replies) {
      const system = !!m.system;
      const grouped = !!(prev && !system && !prev.system && prev.author_id === m.author_id && !m.quote && new Date(m.created_at) - new Date(prev.created_at) < 5 * 60 * 1000);
      rows.push({ key: 'm-' + m.id, sig: sig(m, grouped), make: () => renderMessage(m, this.envFor(), { inThread: true, grouped }) });
      prev = m;
    }
    if (this.cache.size === 0) fill(this.list);
    reconcile(this.list, this.cache, rows);
    if (first) this.scroller.scrollTop = this.scroller.scrollHeight;
  }

  // Quotes inside the thread are found in the pane's own copy first.
  envFor() {
    if (!this.envLocal) {
      const base = this.env;
      this.envLocal = { ...base, quoted: (id) => this.r.byId.get(id) || base.quoted(id) };
    }
    return this.envLocal;
  }

  // Reading the thread moves the reader's position in it, on every device.
  read() {
    const c = conversation(this.cid);
    const parent = this.parent();
    if (!c || !c.membership || !parent || !parent.thread || document.visibilityState !== 'visible' || !document.hasFocus()) return;
    const st = store.threadStates.get(this.pid);
    const last = parent.thread.last_reply_seq;
    if (st ? st.read_seq >= last : false) return;
    if (this.reading === last) return;
    this.reading = last;
    put(`/messages/${this.pid}/thread/read`, { seq: last }).then((out) => {
      if (out.thread_state) store.threadStates.set(this.pid, out.thread_state);
      store.emit('threads', this.pid);
      store.emit('messages', this.cid);
    }).catch(() => { this.reading = null; });
  }
}
