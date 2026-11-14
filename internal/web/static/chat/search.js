// Search (docs/CLIENTS.md section 9.8): a query box with filters for the
// conversation, the author, dates and "has a file"; results newest first,
// each with its conversation, author, time and the matching lines. A
// result opens in place: in its conversation, or in its thread if it is a
// reply. Sealed chats are never searched.

import { get } from '/shared/api.js';
import { h, icon, button, fill, toast, fromLocalInput, when, iconButton } from '/shared/dom.js';
import { pickAccounts, pickChannel } from '/shared/pick.js';
import { renderMessage } from '/shared/message/message.js';
import { store, conversation, seeConversation, needAccounts, title, pathTitle } from './store.js';
import { makeEnv, look, visibleChannels } from './env.js';
import { ui } from './ui.js';

export class SearchView {
  constructor() {
    const params = new URLSearchParams(location.search);
    this.in = params.get('in') ? conversation(params.get('in')) || { id: params.get('in') } : null;
    this.from = null;
    this.cursor = null;
    this.env = makeEnv({ reply: () => {}, edit: () => {}, redraw: () => {} });
    this.q = h('input', { type: 'search', placeholder: 'Search messages', 'aria-label': 'Search messages', value: params.get('q') || '', autocomplete: 'off' });
    this.since = h('input', { type: 'date', 'aria-label': 'Since' });
    this.until = h('input', { type: 'date', 'aria-label': 'Until' });
    this.hasFile = h('input', { type: 'checkbox', checked: params.has('has_file') });
    this.inBtn = button('Any conversation', async () => {
      const c = await pickChannel('Search in', [...visibleChannels(), ...[...store.conversations.values()].filter((x) => !x.name && x.kind === 'channel').map((x) => ({ ...x, name: title(x) }))], look, { none: 'Any conversation' });
      if (c === undefined) return;
      this.in = c; this.drawFilters(); this.run();
    }, 'small');
    this.fromBtn = button('Anyone', async () => {
      const got = await pickAccounts('Written by', { yes: 'Choose' });
      if (got) { this.from = got[0]; this.drawFilters(); this.run(); }
    }, 'small');
    this.results = h('div', { class: 'results', 'aria-live': 'polite' });
    this.more = h('div', { class: 'load-more', hidden: true }, button('More results', () => this.run(true), 'small'));
    const form = h('form', { class: 'search-form', role: 'search', onsubmit: (e) => { e.preventDefault(); this.run(); } },
      h('div', { class: 'row' }, h('div', { class: 'grow' }, this.q), h('button', { type: 'submit', class: 'btn primary' }, icon('search'), h('span', {}, 'Search'))),
      h('div', { class: 'row filters' },
        h('span', { class: 'label' }, 'In'), this.inBtn,
        h('span', { class: 'label' }, 'From'), this.fromBtn, this.clearFrom = button('×', () => { this.from = null; this.drawFilters(); this.run(); }, 'small quiet'),
        h('label', { class: 'row' }, h('span', { class: 'label' }, 'Since'), this.since),
        h('label', { class: 'row' }, h('span', { class: 'label' }, 'Until'), this.until),
        h('label', { class: 'check' }, this.hasFile, h('span', {}, 'Has a file'))));
    this.since.addEventListener('change', () => this.run());
    this.until.addEventListener('change', () => this.run());
    this.hasFile.addEventListener('change', () => this.run());
    this.el = h('section', { class: 'view' },
      h('header', { class: 'conv-head' }, iconButton('back', 'Back to the list', () => ui.go('/chat/'), 'only-compact'), h('div', { class: 'conv-title' }, h('h1', {}, icon('search'), ' Search'))),
      h('div', { class: 'view-body' }, form, h('p', { class: 'dim small' }, 'Search covers the channels and chats you can read, replies included. Sealed chats are never searched, and neither is message history.'), this.results, this.more));
    this.clearFrom.setAttribute('aria-label', 'Clear the author');
    this.drawFilters();
    if (this.q.value || this.hasFile.checked || this.in) this.run();
    this.off = store.on('accounts', () => { if (this.items) this.draw(); });
    setTimeout(() => this.q.focus(), 0);
  }

  destroy() { this.off(); }

  drawFilters() {
    this.inBtn.textContent = this.in ? (this.in.name ? pathTitle(this.in) : title(conversation(this.in.id)) || 'One conversation') : 'Any conversation';
    this.fromBtn.textContent = this.from ? '@' + this.from.username : 'Anyone';
    this.clearFrom.hidden = !this.from;
  }

  async run(more) {
    const q = this.q.value.trim();
    const filtered = this.hasFile.checked || this.in || this.from || this.since.value || this.until.value;
    if (!q && !filtered) { fill(this.results, h('p', { class: 'dim' }, 'Type something to search for, or choose a filter.')); this.more.hidden = true; return; }
    if (!more) { this.items = []; this.cursor = null; fill(this.results, h('p', { class: 'loading' }, 'Searching')); }
    try {
      const out = await get('/search', {
        q, in: this.in && this.in.id, from: this.from && this.from.id, has_file: this.hasFile.checked || undefined,
        since: this.since.value ? fromLocalInput(this.since.value + 'T00:00') : undefined, until: this.until.value ? fromLocalInput(this.until.value + 'T23:59') : undefined,
        limit: 30, cursor: more ? this.cursor : undefined,
      });
      this.items.push(...out.results);
      this.cursor = out.next_cursor || null;
      needAccounts(out.results.map((r) => r.message.author_id));
      for (const r of out.results) if (!conversation(r.message.conversation_id)) seeConversation(r.message.conversation_id);
      this.draw();
    } catch (err) {
      if (!more) fill(this.results, h('p', { class: 'problem' }, err.message)); else toast(err.message, true);
    }
  }

  draw() {
    this.more.hidden = !this.cursor;
    if (!this.items.length) { fill(this.results, h('p', { class: 'dim' }, 'Nothing found.')); return; }
    fill(this.results, h('p', { class: 'dim small' }, `${this.items.length}${this.cursor ? '+' : ''} found, newest first`), this.items.map((r) => {
      const m = r.message;
      const c = conversation(m.conversation_id);
      const open = () => ui.openConversation(m.conversation_id, m.parent_id ? { threadId: m.parent_id } : { messageId: m.id });
      return h('article', { class: 'result' },
        h('button', { type: 'button', class: 'result-where', onclick: open }, icon(m.parent_id ? 'thread' : 'hash', null, 'small'),
          h('span', {}, c ? pathTitle(c) : 'a conversation', m.parent_id ? ' · in a thread' : ''), h('span', { class: 'dim' }, ' · ' + when(m.created_at)), h('span', { class: 'go' }, 'Open')),
        renderMessage(m, this.env, { bare: true }),
        r.snippet ? h('p', { class: 'result-snippet dim small' }, r.snippet) : null);
    }));
  }
}

