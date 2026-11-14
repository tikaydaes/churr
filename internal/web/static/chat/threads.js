// The Threads view (docs/CLIENTS.md section 9.9): every thread the person
// follows that has unread replies, newest reply first, across all
// conversations. "Show read threads" lists followed threads with nothing
// new.

import { get, put } from '/shared/api.js';
import { h, icon, iconButton, button, fill, toast, plural, ago, check } from '/shared/dom.js';
import { firstLine } from '/shared/message/message.js';
import { store, conversation, seeConversation, needAccounts, nameOf, pathTitle } from './store.js';
import { makeEnv } from './env.js';
import { ui } from './ui.js';

export class ThreadsView {
  constructor() {
    this.all = false;
    this.env = makeEnv({ reply: () => {}, edit: () => {}, redraw: () => {} });
    this.list = h('div', { class: 'results' });
    const toggle = h('input', { type: 'checkbox', onchange: () => { this.all = toggle.checked; this.load(); } });
    this.el = h('section', { class: 'view' },
      h('header', { class: 'conv-head' }, iconButton('back', 'Back to the list', () => ui.go('/chat/'), 'only-compact'), h('div', { class: 'conv-title' }, h('h1', {}, icon('thread'), ' Threads')),
        h('div', { class: 'conv-tools' }, check('Show read threads', toggle))),
      h('div', { class: 'view-body' }, this.list));
    this.off = [store.on('threads', () => this.later()), store.on('accounts', () => this.draw()), store.on('message.created', (e) => { if (e.message.parent_id) this.later(); })];
    this.load();
  }

  destroy() { for (const off of this.off) off(); clearTimeout(this.timer); }
  later() { clearTimeout(this.timer); this.timer = setTimeout(() => this.load(true), 400); }

  async load(quiet) {
    if (!quiet) fill(this.list, h('p', { class: 'loading' }, 'Loading'));
    try {
      const out = await get('/threads', { all: this.all || undefined, limit: 100 });
      this.items = out.threads;
      for (const t of this.items) { store.threadStates.set(t.thread_state.parent_id, t.thread_state); if (!conversation(t.parent.conversation_id)) seeConversation(t.parent.conversation_id); }
      needAccounts(this.items.flatMap((t) => [t.parent.author_id, ...((t.parent.thread && t.parent.thread.participant_ids) || [])]));
      this.draw();
    } catch (err) { fill(this.list, h('p', { class: 'problem' }, err.message)); }
  }

  draw() {
    if (!this.items) return;
    const shown = this.all ? this.items : this.items.filter((t) => t.thread_state.unread_count > 0);
    const sorted = [...shown].sort((a, b) => ((b.parent.thread || {}).last_reply_seq || 0) - ((a.parent.thread || {}).last_reply_seq || 0)
      || String((b.parent.thread || {}).last_reply_at).localeCompare(String((a.parent.thread || {}).last_reply_at)));
    if (!sorted.length) { fill(this.list, h('p', { class: 'dim' }, this.all ? 'You follow no threads yet. Replying in a thread, or being mentioned in one, follows it.' : 'No thread you follow has anything new.')); return; }
    const act = async (fn) => { try { await fn(); this.load(true); } catch (err) { toast(err.message, true); } };
    fill(this.list, sorted.map((t) => {
      const p = t.parent, st = t.thread_state, c = conversation(p.conversation_id);
      const open = () => ui.openConversation(p.conversation_id, { threadId: p.id });
      return h('article', { class: 'result thread-item' + (st.unread_count ? ' unread' : '') },
        h('button', { type: 'button', class: 'result-where', onclick: open }, icon('thread', null, 'small'),
          h('span', {}, c ? pathTitle(c) : 'a conversation'), st.unread_count ? h('span', { class: 'badge' }, String(st.unread_count)) : null, h('span', { class: 'go' }, 'Open')),
        h('p', {}, h('strong', {}, nameOf(p.author_id) + ': '), firstLine(p, this.env, 220)),
        h('p', { class: 'dim small' }, p.thread ? `${plural(p.thread.reply_count, 'reply', 'replies')} · last ${ago(p.thread.last_reply_at)}` : 'No replies'),
        h('p', { class: 'row' },
          button('Reply', open, 'small primary'),
          st.unread_count && p.thread ? button('Mark read', () => act(async () => { const out = await put(`/messages/${p.id}/thread/read`, { seq: p.thread.last_reply_seq }); store.threadStates.set(p.id, out.thread_state); store.emit('threads', p.id); }), 'small') : null,
          button('Stop following', () => act(async () => { await put(`/messages/${p.id}/thread/state`, { state: 'none' }); store.threadStates.delete(p.id); store.emit('threads', p.id); }), 'small')));
    }));
  }
}
