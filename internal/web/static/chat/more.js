// The smaller screens of the web chat: browse channels, the notifications
// list, bookmarks, scheduled messages and reminders with saved replies,
// and the dialogs for a new chat, a new channel, the quick switcher and
// the list of keyboard shortcuts.

import { knockOn } from './cmddialogs.js';
import { STRINGS } from '/shared/commands.js';
import { get, post, patch, del, all, put } from '/shared/api.js';
import { h, icon, iconButton, button, fill, toast, plural, when, dateTime, openDialog, confirmDialog, debounce, fold, busy, avatar } from '/shared/dom.js';
import { formDialog } from '/shared/form.js';
import { pickAccounts, detailLine, channelIcon } from '/shared/pick.js';
import { renderMessage, firstLine } from '/shared/message/message.js';
import { store, conversation, seeConversation, needAccounts, nameOf, title, pathTitle, mayJoin, isPublic } from './store.js';
import { makeEnv, look, visibleChannels } from './env.js';
import { ui } from './ui.js';
import { openModes } from './modes.js';
import { offerSubChannels } from './conversation.js';
import { commandsChanged } from './composer.js';
import { isBuiltin, isProtected, STRINGS as COMMAND_STRINGS } from '/shared/commands.js';
import { selfServe, listed, toggle } from './groups.js';
import { joinCodeAnswer } from '/shared/hub.js';

function frame(name, iconName, tools, body) {
  return h('section', { class: 'view' },
    h('header', { class: 'conv-head' }, iconButton('back', 'Back to the list', () => ui.go('/chat/'), 'only-compact'),
      h('div', { class: 'conv-title' }, h('h1', {}, icon(iconName), ' ' + name)), h('div', { class: 'conv-tools' }, tools)),
    h('div', { class: 'view-body' }, body));
}

const bareEnv = () => makeEnv({ reply: () => {}, edit: () => {}, redraw: () => {} });

// --- browse channels ---

// askWithCode is "Ask to join a channel on a hub": nobody outside a
// private channel on a hub sees it, so a moderator or manager gives the
// person its join code, and they ask with it here (askToJoinHubChannel).
function askWithCode() {
  const code = h('input', { name: 'join-code', placeholder: 'Join code', 'aria-label': 'Join code', autocomplete: 'off', spellcheck: false, autocapitalize: 'none', class: 'mono', required: true });
  const said = h('p', { class: 'small', role: 'status' });
  const ask = h('button', { type: 'submit', class: 'btn small primary' }, 'Ask to join');
  const form = h('form', { class: 'stack tight ask-code', onsubmit: (e) => {
    e.preventDefault();
    const id = code.value.trim();
    if (!id) return;
    busy(ask, async () => {
      let err = null;
      try { await post(`/streams/${encodeURIComponent(id)}/channel/requests`); } catch (x) { err = x; }
      const a = joinCodeAnswer(err);
      said.className = 'small ' + (a.ok ? 'note' : 'problem');
      said.textContent = a.says;
      if (a.ok) code.value = '';
    });
  } },
  h('p', { class: 'dim small' }, 'A private channel on a hub is not listed anywhere. Ask one of its moderators or managers for its join code and enter it here.'),
  h('div', { class: 'row' }, code, ask), said);
  return [h('h2', { class: 'results-head' }, 'Ask to join a channel on a hub'), form];
}

// Every public channel, and the private ones the person is in or may join
// (ruling R103: a group they are in has access, or it was offered to them
// as a default channel), as a tree with the detail line under each. Same-named channels are listed
// together, those the person is in first.
export class BrowseView {
  constructor() {
    this.q = h('input', { type: 'search', placeholder: 'Find a channel', 'aria-label': 'Find a channel', autocomplete: 'off' });
    this.list = h('ul', { class: 'rows browse' });
    this.groups = h('ul', { class: 'rows browse', 'data-list': 'groups' });
    this.q.addEventListener('input', debounce(() => this.load(), 200));
    this.el = frame('Browse channels', 'hash', store.me.role !== 'guest' ? button('New channel', () => ui.newChannel(), 'small primary') : null,
      [this.q, this.list, h('h2', { class: 'results-head' }, 'Groups'), this.groups, store.me.role !== 'guest' ? askWithCode() : null]);
    this.off = store.on('conversations', () => this.draw());
    this.load();
  }

  destroy() { this.off(); }

  async load() {
    try {
      this.items = await all('/channels', 'conversations', { q: this.q.value.trim() }, 600);
      // Browse lists a private channel to its members and to those who may
      // join it without an invitation, and lists to a guest only channels
      // they are in or may join: such a channel is joined, never knocked on.
      for (const c of this.items) if (!c.membership && (c.private || store.me.role === 'guest')) c.joinable = true;
      for (const c of this.items) if (!store.conversations.has(c.id)) store.others.set(c.id, c);
      this.draw();
    } catch (err) { fill(this.list, h('li', {}, err.message)); }
    try { store.groups = (await get('/groups')).groups; } catch { /* the list from the start stays */ }
    this.drawGroups();
  }

  // The server's groups, with Join or Leave on an open one. Joining offers
  // the person every channel the group is in (ruling R103).
  drawGroups() {
    const groups = listed(store.groups);
    fill(this.groups, groups.length ? groups.map((g) => {
      const act = button(g.is_member ? 'Leave' : 'Join', () => busy(act, async () => {
        try {
          const now = await toggle(g, { post, del });
          store.groups = store.groups.map((x) => (x.id === g.id ? now : x));
          toast(g.is_member ? `You left @${g.handle}.` : `You joined @${g.handle}. The channels it is in are offered to you in the sidebar.`);
          this.drawGroups();
        } catch (err) { toast(err.message, true); }
      }), g.is_member ? 'small' : 'small primary');
      act.dataset.group = g.handle;
      return h('li', {}, icon('people'),
        h('div', { class: 'what' }, h('strong', { class: 'mono' }, '@' + g.handle),
          h('small', {}, [g.name, plural(g.member_count || 0, 'member'), g.is_member ? 'you are in it' : '', g.open ? '' : 'admins choose who is in it'].filter(Boolean).join(' · ')),
          g.description ? h('small', {}, g.description) : null),
        h('div', { class: 'does' }, selfServe(g, store.me.role) ? act : null));
    }) : h('li', { class: 'dim' }, 'This server has no groups yet.'));
  }

  draw() {
    if (!this.items) return;
    const items = this.items.map((c) => conversation(c.id) || c);
    const ids = new Set(items.map((c) => c.id));
    const sort = (xs) => xs.sort((a, b) => title(a).localeCompare(title(b), undefined, { sensitivity: 'base' }) || (!!b.membership - !!a.membership) || String(a.created_at).localeCompare(String(b.created_at)));
    const tops = sort(items.filter((c) => !c.parent_id || !ids.has(c.parent_id)));
    const rows = [];
    const row = (c, child) => {
      const join = button('Join', () => busy(join, async () => {
        const out = await post(`/conversations/${c.id}/join`);
        store.conversations.set(c.id, out.conversation); store.others.delete(c.id); store.emit('conversations');
        // Joining takes up an offer of it too: the server says so with
        // channel_offer.removed, and the sidebar need not wait for it.
        if (store.offers.some((o) => o.conversation_id === c.id)) { store.offers = store.offers.filter((o) => o.conversation_id !== c.id); store.emit('offers'); }
        toast('Joined ' + title(c) + '.');
        offerSubChannels(out.conversation, false);
      }), 'small primary');
      return h('li', { class: child ? 'tree-child' : '' }, channelIcon(c),
        h('div', { class: 'what' }, h('strong', {}, h('bdi', {}, title(c)), c.archived_at ? [' ', h('span', { class: 'tag grey' }, 'archived')] : null),
          h('small', {}, detailLine(c, look)), c.topic ? h('small', {}, c.topic) : null),
        // A knock channel is asked to join, never read from outside (ruling
        // R79), unless the person may join it without asking (R103). Only a
        // public channel is read before joining.
        c.access === 'knock' && !c.membership && !mayJoin(c)
          ? h('div', { class: 'does' }, button(STRINGS.askToJoin, () => knockOn(c), 'small primary'))
          : h('div', { class: 'does' }, c.membership || isPublic(c) ? button(c.membership ? 'Open' : 'Read', () => ui.openConversation(c.id), 'small') : null, mayJoin(c) ? join : null));
    };
    for (const c of tops) { rows.push(row(c, false)); for (const k of sort(items.filter((x) => x.parent_id === c.id))) rows.push(row(k, true)); }
    fill(this.list, rows.length ? rows : h('li', { class: 'dim' }, 'No channel matches.'));
  }
}

// --- the notifications list ---

// Each message that notified or would have notified the person, newest
// first, with why it is here. It is built in the page from the `notify`
// mark on events, and filled in from the conversations that have a badge;
// no server keeps such a list (docs/CLIENTS.md sections 8.3 and 9.24).
export class NotificationsView {
  constructor() {
    this.kind = 'all';
    this.env = bareEnv();
    this.list = h('div', { class: 'results' });
    const filter = h('select', { 'aria-label': 'Which notifications', onchange: () => { this.kind = filter.value; this.draw(); } },
      h('option', { value: 'all' }, 'All kinds'), h('option', { value: 'mentions' }, 'Mentions only'), h('option', { value: 'threads' }, 'Threads only'));
    this.el = frame('Notifications', 'bell', [filter, button('Mark all read', () => this.readAll(), 'small')], this.list);
    this.off = [store.on('notifications', () => this.draw()), store.on('accounts', () => this.draw())];
    this.draw();
    this.fill();
  }

  destroy() { for (const off of this.off) off(); }

  // What happened before this page opened: for each conversation with a
  // badge, the messages after its read position.
  async fill() {
    const have = new Set(store.notifications.map((n) => n.key));
    for (const c of store.conversations.values()) {
      const m = c.membership;
      if (!m || !m.badge_count || c.kind === 'sealed') continue;
      try {
        const page = await get(`/conversations/${c.id}/messages`, { after: m.read_seq, limit: 50 });
        for (const msg of page.messages) {
          if (have.has(msg.id) || msg.author_id === store.me.id || msg.system) continue;
          const mentions = (msg.entities || []).some((e) => (e.type === 'account' && e.account_id === store.me.id) || e.type === 'broadcast' || (e.type === 'group' && store.groups.some((g) => g.id === e.group_id && g.is_member)));
          if (m.mode === 'any' ? !msg.parent_id || mentions : mentions) {
            store.notifications.push({ key: msg.id, conversationId: c.id, parentId: msg.parent_id || null, message: msg, at: msg.created_at, cleared: false,
              why: mentions ? 'mentioned you' : 'new message' });
          }
        }
      } catch { /* that conversation's items are left out */ }
    }
    store.notifications.sort((a, b) => b.at.localeCompare(a.at));
    needAccounts(store.notifications.map((n) => n.message.author_id));
    this.draw();
    store.emit('notifications');
  }

  async readAll() {
    const convs = new Set(), threads = new Map();
    for (const n of this.shown().filter((x) => !x.cleared)) { if (n.parentId) threads.set(n.parentId, n); else convs.add(n.conversationId); }
    try {
      for (const id of convs) { const c = store.conversations.get(id); if (c) { const out = await put(`/conversations/${id}/read`, { seq: c.last_seq }); c.membership = out.membership; } }
      for (const [pid, n] of threads) await put(`/messages/${pid}/thread/read`, { seq: n.message.seq });
      for (const n of this.shown()) n.cleared = true;
      store.emit('conversations'); store.emit('notifications');
    } catch (err) { toast(err.message, true); }
  }

  shown() {
    return store.notifications.filter((n) => this.kind === 'all' || (this.kind === 'mentions' ? n.why.startsWith('mentioned') : !!n.parentId));
  }

  draw() {
    const items = this.shown();
    const unread = items.filter((n) => !n.cleared), earlier = items.filter((n) => n.cleared);
    const one = (n) => {
      const c = conversation(n.conversationId);
      const open = () => ui.openConversation(n.conversationId, n.parentId ? { threadId: n.parentId } : { messageId: n.message.id });
      return h('article', { class: 'result' + (n.cleared ? '' : ' unread') },
        h('button', { type: 'button', class: 'result-where', onclick: open }, icon(n.parentId ? 'thread' : 'bell', null, 'small'),
          h('span', {}, c ? pathTitle(c) : 'a conversation'), h('span', { class: 'tag line' }, n.why), h('span', { class: 'dim' }, ' · ' + when(n.at)), h('span', { class: 'go' }, 'Open')),
        h('p', {}, h('strong', {}, nameOf(n.message.author_id) + ': '), n.message.sealed ? 'A sealed message' : firstLine(n.message, this.env, 240)),
        h('p', { class: 'row' },
          !n.cleared ? button('Mark read', async () => {
            try {
              if (n.parentId) await put(`/messages/${n.parentId}/thread/read`, { seq: n.message.seq });
              else if (c) { const out = await put(`/conversations/${n.conversationId}/read`, { seq: Math.max(c.membership.read_seq, n.message.seq) }); c.membership = out.membership; store.emit('conversations'); }
              n.cleared = true; this.draw();
            } catch (err) { toast(err.message, true); }
          }, 'small') : null,
          c && c.membership ? button('Settings for this conversation', () => openModes(c.id), 'small quiet') : null));
    };
    fill(this.list,
      unread.length ? unread.map(one) : h('p', { class: 'dim' }, 'Nothing new. Mentions, replies in threads you follow and messages in conversations set to “any activity” appear here, hidden conversations included.'),
      earlier.length ? [h('h2', { class: 'results-head' }, 'Earlier'), earlier.map(one)] : null);
  }
}

// --- bookmarks ---

export class BookmarksView {
  constructor() {
    this.env = bareEnv();
    this.list = h('div', { class: 'results' });
    this.el = frame('Bookmarks', 'bookmark', null, this.list);
    this.off = [store.on('bookmarks', () => this.load(true)), store.on('accounts', () => this.draw())];
    this.load();
  }

  destroy() { for (const off of this.off) off(); }

  async load(quiet) {
    if (!quiet) fill(this.list, h('p', { class: 'loading' }, 'Loading'));
    try {
      this.items = await all('/bookmarks', 'bookmarks');
      for (const b of this.items) { store.bookmarks.add(b.message_id); if (!conversation(b.message.conversation_id)) seeConversation(b.message.conversation_id); }
      needAccounts(this.items.map((b) => b.message.author_id));
      this.draw();
    } catch (err) { fill(this.list, h('p', { class: 'problem' }, err.message)); }
  }

  draw() {
    if (!this.items) return;
    fill(this.list, this.items.length ? this.items.map((b) => {
      const m = b.message, c = conversation(m.conversation_id);
      const open = () => ui.openConversation(m.conversation_id, m.parent_id ? { threadId: m.parent_id } : { messageId: m.id });
      return h('article', { class: 'result' },
        h('button', { type: 'button', class: 'result-where', onclick: open }, icon('bookmark', null, 'small'), h('span', {}, c ? pathTitle(c) : 'a conversation'),
          h('span', { class: 'dim' }, ' · bookmarked ' + when(b.created_at)), h('span', { class: 'go' }, 'Open')),
        renderMessage(m, this.env, { bare: true }),
        h('p', { class: 'row' }, button('Remove bookmark', async () => {
          try { await del(`/messages/${m.id}/bookmark`); store.bookmarks.delete(m.id); this.items = this.items.filter((x) => x !== b); this.draw(); } catch (err) { toast(err.message, true); }
        }, 'small')));
    }) : h('p', { class: 'dim' }, 'No bookmarks. Bookmark a message from its menu to find it here.'));
  }
}

// --- scheduled messages, reminders and saved replies ---

export class ScheduledView {
  constructor() {
    this.list = h('div', { class: 'results' });
    this.replies = h('div', { class: 'results' });
    this.el = frame('Scheduled', 'clock', null, [this.list, h('h2', { class: 'results-head' }, 'Your saved replies'), this.replies]);
    this.off = store.on('scheduled', () => this.load(true));
    this.load();
    this.loadReplies();
  }

  destroy() { this.off(); }

  async load(quiet) {
    if (!quiet) fill(this.list, h('p', { class: 'loading' }, 'Loading'));
    let items;
    try { items = await all('/scheduled', 'scheduled'); } catch (err) { fill(this.list, h('p', { class: 'problem' }, err.message)); return; }
    const act = async (fn, said) => { try { await fn(); if (said) toast(said); this.load(true); } catch (err) { if (!err.silent) toast(err.message, true); } };
    await needAccounts(items.filter((s) => s.kind === 'watch').map((s) => s.watch_account_id));
    fill(this.list, items.length ? items.map((s) => {
      // A watch (ruling R76): who it waits for, and until when.
      if (s.kind === 'watch') {
        return h('article', { class: 'result', 'data-kind': 'watch' },
          h('div', { class: 'result-where static' }, icon('bell', null, 'small'), h('span', {}, COMMAND_STRINGS.watchKind(nameOf(s.watch_account_id))),
            h('span', { class: 'tag line' }, s.next_run_at ? COMMAND_STRINGS.watchUntil(dateTime(s.next_run_at)) : 'done')),
          s.next_run_at ? h('p', { class: 'row' },
            button(COMMAND_STRINGS.stopWatching, () => act(() => del('/scheduled/' + s.id), COMMAND_STRINGS.unwatched(nameOf(s.watch_account_id))), 'small danger')) : null);
      }
      const c = conversation(s.conversation_id);
      const what = s.kind === 'reminder' ? 'Reminder' : s.kind === 'form' ? 'Recurring form' : 'Message';
      return h('article', { class: 'result' },
        h('div', { class: 'result-where static' }, icon(s.kind === 'reminder' ? 'bell' : 'clock', null, 'small'), h('span', {}, `${what} for ${c ? pathTitle(c) : 'a conversation'}`),
          h('span', { class: 'tag ' + (s.failed_at ? 'red' : 'line') }, s.failed_at ? 'failed' : s.next_run_at ? dateTime(s.next_run_at) : 'done')),
        h('p', {}, (s.content && (s.content.text || (s.content.form && s.content.form.title))) || 'No text'),
        s.failure ? h('p', { class: 'note danger' }, s.failure) : null,
        h('p', { class: 'row' },
          s.kind !== 'form' ? button('Edit', async () => {
            const v = await formDialog('Edit', [
              { name: 'text', label: 'Text', type: 'textarea', value: s.content.text || '', required: true },
              { name: 'at', label: 'When', type: 'datetime', value: s.next_run_at, required: true },
            ], { submit: (x) => patch('/scheduled/' + s.id, { content: { ...s.content, text: x.text }, run_at: x.at }) });
            if (v) this.load(true);
          }, 'small') : null,
          s.kind === 'message' ? button('Send now', () => act(() => post(`/scheduled/${s.id}/send`), 'Sent.'), 'small') : null,
          button('Cancel it', async () => { if (await confirmDialog('Cancel this?', 'It will not be sent.', { yes: 'Cancel it', no: 'Keep it', danger: true })) act(() => del('/scheduled/' + s.id), 'Cancelled.'); }, 'small danger')));
    }) : h('p', { class: 'dim' }, 'Nothing is waiting. “Send later” is the clock beside the send button; “Remind me about this” is in a message’s menu.'));
  }

  async loadReplies() {
    let items;
    try { items = (await get('/saved-replies')).saved_replies.filter((r) => !r.community); } catch (err) { fill(this.replies, h('p', { class: 'problem' }, err.message)); return; }
    const fields = (r) => [
      { name: 'name', label: 'Command', required: true, code: true, value: r && r.name, maxLength: 32, hint: 'Typed as /name. Only a fun or game built-in (/shrug, /roll …) may be replaced; the others’ names are refused.' },
      { name: 'text', label: 'What it posts', type: 'textarea', required: true, value: r && r.text, rows: 5 }];
    // A built-in of the same name is replaced for this person (ruling R72).
    const saved = (out) => { if (!out) return; if (out.saved_reply && isBuiltin(out.saved_reply.name)) toast(COMMAND_STRINGS.replacesBuiltinForYou(out.saved_reply.name)); commandsChanged(); this.loadReplies(); };
    fill(this.replies,
      h('p', { class: 'row' }, button('New saved reply', async () => saved(await formDialog('New saved reply', fields(null), { yes: 'Create', submit: (v) => post('/saved-replies', v) })), 'small primary')),
      items.length ? items.map((r) => h('article', { class: 'result' }, h('div', { class: 'result-where static' }, h('code', {}, '/' + r.name)), isProtected(r.name) ? h('p', { class: 'dim' }, COMMAND_STRINGS.protectedName(r.name)) : null, h('p', { class: 'clip' }, r.text),
        h('p', { class: 'row' },
          button('Edit', async () => saved(await formDialog('Edit /' + r.name, fields(r), { submit: (v) => patch('/saved-replies/' + r.id, v) })), 'small'),
          button('Delete', async () => { try { await del('/saved-replies/' + r.id); this.loadReplies(); } catch (err) { toast(err.message, true); } }, 'small danger'))))
        : h('p', { class: 'dim' }, 'A saved reply is text you post by typing /name.'));
  }
}

// --- dialogs ---

// New chat: pick people. It opens the existing chat with exactly them, or
// makes one.
export async function newChat() {
  const chosen = await pickAccounts('New chat', { multiple: true, exclude: new Set([store.me.id]), yes: 'Open the chat',
    note: 'A chat is a private channel with no name. If one with exactly these people exists it is opened. Anyone added later sees the whole history.' });
  if (!chosen) return;
  try {
    const out = await post('/chats', { account_ids: chosen.map((a) => a.id) });
    store.conversations.set(out.conversation.id, { ...store.conversations.get(out.conversation.id), ...out.conversation });
    needAccounts(out.conversation.member_ids);
    store.emit('conversations');
    ui.openConversation(out.conversation.id);
  } catch (err) { toast(err.message, true); }
}

// New channel. Names need not be unique: channels that already have the
// name are listed, and the person may carry on.
export async function newChannel(parentId) {
  const tops = [...store.conversations.values()].filter((c) => c.kind === 'channel' && c.name && !c.parent_id && !c.archived_at);
  const twins = h('div', {});
  const out = await formDialog('New channel', [
    { name: 'name', label: 'Name', required: true, maxLength: 64, hint: '1 to 64 characters of any script: “Design reviews” is fine.' },
    { node: twins },
    { name: 'topic', label: 'Topic', maxLength: 250 },
    { name: 'private', label: 'Private', type: 'checkbox', hint: 'Only people who are added can see it. A private channel can never be made public.' },
    { name: 'announcement', label: 'Announcement channel', type: 'checkbox', hint: 'Only its managers and the people they allow post. Everyone else reads and reacts, and is not shown who else is in it. This can be changed later.' },
    { name: 'parent_id', label: 'Under', type: 'select', value: parentId || '', options: [['', 'No parent: a top-level channel'], ...tops.map((c) => [c.id, c.name])], hint: 'A sub-channel is listed under its parent and is otherwise a channel in its own right: nothing is inherited.' },
    { name: 'section_id', label: 'Section', type: 'select', value: '', options: [['', 'None'], ...store.sections.map((s) => [s.id, s.name])], when: (v) => !v.parent_id },
  ], { yes: 'Create', submit: (v) => {
    const same = visibleChannels().filter((c) => c.name.toLowerCase() === v.name.toLowerCase());
    if (same.length && twins.dataset.shown !== v.name.toLowerCase()) {
      twins.dataset.shown = v.name.toLowerCase();
      fill(twins, h('div', { class: 'note' }, h('p', {}, `${plural(same.length, 'channel')} already ${same.length === 1 ? 'has' : 'have'} this name. That is allowed: press Create again to go on.`),
        h('ul', {}, same.map((c) => h('li', { class: 'small' }, detailLine(c, look) || 'a channel')))));
      throw Object.assign(new Error('Check the name, then press Create again.'), {});
    }
    const body = { name: v.name, private: v.private, announcement: v.announcement || undefined, topic: v.topic || undefined, parent_id: v.parent_id || undefined, section_id: v.parent_id ? undefined : v.section_id || undefined };
    // While new channels wait for approval (channels.creation_approval)
    // the server answers approval_required: the person may ask instead.
    return post('/channels', body).catch((err) => { if (err.code === 'approval_required') return { ask: body }; throw err; });
  } });
  if (out && out.ask) { await askForChannel(out.ask); return; }
  if (out && out.conversation) {
    store.conversations.set(out.conversation.id, out.conversation);
    store.emit('conversations');
    ui.openConversation(out.conversation.id);
  }
}

// askForChannel offers to ask for a channel that waits for approval
// (requestChannel), with an optional note for whoever decides.
async function askForChannel(body) {
  const sent = await formDialog('Ask for approval', [
    { name: 'note', label: 'A note for whoever decides (optional)', type: 'textarea', rows: 3, maxLength: 500, placeholder: 'What the channel is for' },
  ], { yes: 'Ask for approval', intro: `New channels here are approved first. Ask for #${body.name}, and you are told when it is decided; once approved it is made, with you as its first manager.`,
    submit: (v) => post('/approvals/channels', { ...body, note: (v.note || '').trim() || undefined }) });
  if (sent) toast(`Asked for #${body.name}. You will be told when it is decided.`);
}

// The quick switcher: a box that finds conversations and people by name as
// it is typed. It finds hidden conversations too. Words typed are matched
// against the parent's name as well as the channel's.
export function switcher() {
  return openDialog('Jump to', (close) => {
    const input = h('input', { type: 'search', placeholder: 'A channel, a chat or a person', 'aria-label': 'Jump to', autocomplete: 'off' });
    const list = h('ul', { class: 'rows pick-list' });
    let people = [];
    const go = (fn) => { close(); fn(); };
    const draw = () => {
      const words = fold(input.value).split(/\s+/).filter(Boolean);
      const convs = [...store.conversations.values()].filter((c) => {
        const hay = fold(pathTitle(c));
        return words.every((w) => hay.includes(w));
      }).sort((a, b) => String(b.last_message_at || '').localeCompare(String(a.last_message_at || ''))).slice(0, 12);
      const known = [...store.accounts.values()].filter((a) => !a.unknown && a.id !== store.me.id && words.length && words.every((w) => fold(a.display_name).includes(w) || a.username.includes(w)));
      const persons = [...new Map([...known, ...people].map((a) => [a.id, a])).values()].slice(0, 6);
      fill(list,
        convs.map((c) => h('li', {}, h('button', { type: 'button', class: 'pick', onclick: () => go(() => ui.openConversation(c.id)) }, channelIcon(c),
          h('span', { class: 'what' }, h('strong', {}, h('bdi', {}, pathTitle(c))), h('small', {}, c.name ? detailLine(c, look) : 'chat'))))),
        persons.map((a) => h('li', {}, h('button', { type: 'button', class: 'pick', onclick: () => go(async () => {
          try { const out = await post('/chats', { account_ids: [a.id] }); store.conversations.set(out.conversation.id, { ...store.conversations.get(out.conversation.id), ...out.conversation }); store.emit('conversations'); ui.openConversation(out.conversation.id); } catch (err) { toast(err.message, true); }
        }) }, avatar(a), h('span', { class: 'what' }, h('strong', {}, a.display_name || a.username), h('small', {}, '@' + a.username + ' · message them'))))),
        !convs.length && !persons.length ? h('li', { class: 'dim' }, 'Nothing by that name.') : null);
    };
    // People not yet seen in this page are looked for once three
    // characters have been typed.
    const find = debounce(async () => {
      const q = input.value.trim();
      if (q.length < 3) { people = []; return; }
      try { people = (await get('/accounts', { q, limit: 6 })).accounts.filter((a) => a.id !== store.me.id); draw(); } catch { /* the local results stand */ }
    }, 200);
    input.addEventListener('input', () => { draw(); find(); });
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { const first = list.querySelector('button.pick'); if (first) { e.preventDefault(); first.click(); } }
      if (e.key === 'ArrowDown') { const first = list.querySelector('button.pick'); if (first) { e.preventDefault(); first.focus(); } }
    });
    draw();
    return { body: [input, list] };
  });
}

const KEYS = [
  ['Ctrl K', 'Jump to a conversation or a person'], ['Alt ↑ / Alt ↓', 'Previous, next conversation; on a focused sidebar row or heading, move it'], ['Alt Shift ↑ / ↓', 'Previous, next unread conversation'],
  ['Ctrl Shift F', 'Search everywhere'], ['Ctrl Shift T', 'Threads'], ['Ctrl Shift S', 'Bookmarks'], ['Ctrl Shift I', 'Notifications'], ['Ctrl Shift A', 'Mark everything read'],
  ['Ctrl Shift D', 'Do not disturb'], ['Escape', 'Close the thread, a dialog or search'], ['Enter', 'Send. Shift Enter makes a new line'], ['↑ in an empty box', 'Edit your last message'],
  ['Ctrl B, Ctrl I, Ctrl Shift X', 'Bold, italic, strikethrough'], ['Ctrl E, Ctrl Shift C', 'Inline code, code block'], ['Ctrl Shift K', 'Insert a link'], ['Ctrl U', 'Attach a file'],
  ['R, E, T, P, Delete', 'With a message focused: react, edit, reply in thread, pin, delete'], ['Ctrl Shift L', 'Lock Churr in this browser'], ['Ctrl /', 'Show this list'],
];

export function shortcuts() {
  return openDialog('Keyboard shortcuts', (close) => ({
    body: [h('p', { class: 'dim small' }, 'On a Mac, Ctrl is Command. Where the browser keeps a key for itself, it wins.'),
      h('dl', { class: 'keys' }, KEYS.map(([k, what]) => h('div', {}, h('dt', {}, k.split(', ').map((x, i) => [i ? ', ' : '', h('kbd', {}, x)])), h('dd', {}, what))))],
    foot: [button('Close', () => close(), 'primary')],
  }));
}

