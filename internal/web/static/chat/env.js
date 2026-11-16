// What the message renderer needs from the chat: names, permissions, and
// the actions of docs/CLIENTS.md section 9.6. One env is made for each
// place messages are drawn (the conversation, the thread pane), because
// replying and editing happen in that place's own composer.

import { get, post, put, del } from '/shared/api.js';
import { h, toast, openMenu, confirmDialog, promptDialog, openDialog, button, copyText, dateTime, fill } from '/shared/dom.js';
import { renderMarkdown, plainText } from '/shared/md/render.js';
import { eventTitle } from '/shared/objects/ics.js';
import { openPicker } from '/shared/emoji/picker.js';
import { pickAccounts, pickChannel, detailLine } from '/shared/pick.js';
import { toggleTask, taskHistory } from '/shared/message/checklist.js';
import { formDialog } from '/shared/form.js';
import { store, account, nameOf, conversation, seeConversation, findMessage, putMessage, actsAsManager, canPost, isSealed, isAdHoc, room, savePrefs, title, hubOf } from './store.js';
import { deleteChoice, deleteWords, isModerator } from '/shared/hub.js';
import { ui, linkTo } from './ui.js';
import { openProfile } from './profile.js';
import { systemLine } from './call/index.js';

// --- lookups shared by every env ---

export function customEmoji(name) {
  return store.emoji.find((e) => (e.server_name ? `${e.name}@${e.server_name}` : e.name) === name);
}

// look is what the detail line and the pickers need to describe a channel.
export const look = {
  conversation,
  section: (id) => store.sections.find((s) => s.id === id),
  account,
};

export function visibleChannels() {
  return [...store.conversations.values(), ...store.others.values()].filter((c) => c.kind === 'channel' && c.name);
}

// A message the page has not loaded is fetched once, for a quote or a
// reply header. undefined means "not known yet", null "cannot be read".
const fetched = new Map();
function quoted(id) {
  if (!id) return null;
  const m = findMessage(id);
  if (m) return m;
  if (fetched.has(id)) return fetched.get(id);
  fetched.set(id, undefined);
  get('/messages/' + id).then((r) => { fetched.set(id, r.message); account(r.message.author_id); })
    .catch(() => fetched.set(id, null))
    .finally(() => store.emit('messages', store.current));
  return undefined;
}

// A message a message link names (ruling R73), fetched as this reader may
// see it and fetched again after a minute, so an edit or a deletion shows.
// undefined means "not known yet", null "cannot be seen".
const linked = new Map();
function linkedMessage(id, host) {
  const m = findMessage(id);
  if (m) return m;
  const had = linked.get(id);
  if (had && (had.loading || Date.now() - had.at < 60000)) { had.hosts.add(host); return had.m; }
  const entry = { at: Date.now(), m: had ? had.m : undefined, loading: true, hosts: new Set([host]) };
  linked.set(id, entry);
  get('/messages/' + id).then((r) => { entry.m = r.message; account(r.message.author_id); if (!conversation(r.message.conversation_id)) seeConversation(r.message.conversation_id); })
    .catch(() => { entry.m = null; })
    .finally(() => {
      // Each message showing the link is drawn again.
      entry.loading = false; entry.at = Date.now();
      for (const x of entry.hosts) { x._v = (x._v || 0) + 1; store.emit('messages', x.conversation_id); }
      entry.hosts.clear();
    });
  return entry.m;
}

// messageLinkView draws a message link as a compact quote of the message:
// who, when, where and its first lines, or "a message you can't see".
// Selecting it goes to the message.
function messageLinkView(e, typed, from) {
  const t = linkedMessage(e.message_id, from);
  const c = t ? conversation(t.conversation_id) : null;
  const where = c && c.kind === 'channel' ? '#' + title(c) : c ? title(c) : '';
  let head, body, said;
  if (t === undefined) { body = h('span', { class: 'dim' }, 'Loading the linked message…'); said = 'Loading'; }
  else if (!t || t.sealed) { body = h('span', { class: 'dim' }, 'A message you can’t see'); said = 'A message you can’t see'; }
  else {
    const who = nameOf(t.author_id);
    head = h('span', { class: 'msg-link-head' }, who, h('span', { class: 'dim' }, ' · ', dateTime(t.created_at), where ? ' · ' + where : ''));
    const text = t.deleted_at ? 'This message was deleted.' : firstLines(t);
    body = h('span', { class: t.deleted_at ? 'msg-link-text dim' : 'msg-link-text' }, text);
    said = `${who}, ${dateTime(t.created_at)}${where ? ', in ' + where : ''}: ${text}`;
  }
  const el = h('a', { class: 'msg-link', href: linkTo(e.conversation_id, e.message_id), 'aria-label': `Link to a message (${typed}). ${said}` }, head, body);
  el.addEventListener('click', (ev) => {
    if (ev.ctrlKey || ev.metaKey || ev.shiftKey) return;
    ev.preventDefault();
    if (t) ui.openConversation(t.conversation_id, t.parent_id ? { threadId: t.parent_id } : { messageId: t.id });
    else ui.openConversation(e.conversation_id);
  });
  return el;
}

// firstLines is the start of a message's text as plain text: three lines
// at most.
function firstLines(m) {
  const text = m.text ? plainText(m.text, m.entities, {}) : (m.files || []).length ? m.files[0].name : '';
  const lines = text.split('\n').filter((l) => l.trim()).slice(0, 3).join('\n');
  return lines.length > 280 ? lines.slice(0, 279) + '…' : lines;
}

function mdContext(m) {
  const c = conversation(m.conversation_id);
  const member = !!(c && c.membership);
  const timedOut = member && c.membership.timed_out_until && new Date(c.membership.timed_out_until) > new Date();
  return {
    meId: store.me.id,
    myGroupIds: new Set(store.groups.filter((g) => g.is_member).map((g) => g.id)),
    account,
    group: (id) => store.groups.find((g) => g.id === id),
    // A channel the reader cannot see has no name here, and is looked up
    // once in case it is a public one they have simply not opened.
    conversation: (id) => { const x = conversation(id); if (!x) seeConversation(id); return x && x.kind === 'channel' ? { ...x, name: title(x) } : x; },
    emoji: (id) => store.emoji.find((e) => e.id === id),
    standardEmoji: (name) => store.standard.byCode.get(name),
    tasks: m.tasks,
    canTick: member && c.membership.level !== 'reader' && !timedOut && !m.deleted_at,
    onTask: m.id && !m._pending ? async (itemId, checked, box) => {
      const task = await toggleTask(m, itemId, checked, box);
      if (task) { m.tasks = (m.tasks || []).map((t) => (t.id === task.id ? task : t)); m._v = (m._v || 0) + 1; store.emit('messages', m.conversation_id); }
    } : null,
    onTaskHistory: m.id && !m._pending ? (itemId) => taskHistory(m, itemId, nameOf) : null,
    onAccount: (id, el) => openProfile(id, el),
    onConversation: (id, el) => channelCard(id, el),
    // "Remind me" in an instant's converter (ruling R69): a reminder about
    // this message at that moment.
    remindAt: m.id && !m._pending ? (at) => remindAtInstant(m, at) : null,
    messageLink: (e, typed) => messageLinkView(e, typed, m),
    // An event's calendar file: the message's first line, and a link to it.
    eventFile: (e) => ({ title: eventTitle(m.text ? plainText(m.text, m.entities, {}) : ''), link: m.id && !m.sealed ? linkTo(m.conversation_id, m.id, m.parent_id) : '', uid: `${m.id || 'draft'}-${e.at}@${location.host}` }),
  };
}

// The small card of a channel link: the detail line, the topic, and Open
// or Join. It is how a reader tells which of several same-named channels
// is meant (docs/CLIENTS.md section 9.26).
function channelCard(id, anchor) {
  const c = conversation(id);
  if (!c) return;
  openMenu(anchor, [
    { head: detailLine(c, look) || 'A channel' },
    c.topic && { head: c.topic },
    { label: c.membership ? 'Open' : 'Open to read', icon: 'out', onclick: () => ui.openConversation(id) },
    !c.membership && !c.private && { label: 'Join', icon: 'plus', onclick: async () => {
      try { const out = await post(`/conversations/${id}/join`); store.conversations.set(id, out.conversation); store.others.delete(id); store.emit('conversations'); ui.openConversation(id); } catch (err) { toast(err.message, true); }
    } },
    { label: 'Copy link', icon: 'copy', onclick: () => copyText(linkTo(id), 'Link copied') },
  ], { label: 'Channel ' + (c.name || '') });
}

function perms(m) {
  const c = conversation(m.conversation_id);
  const member = !!(c && c.membership);
  const timedOut = !!(member && c.membership.timed_out_until && new Date(c.membership.timed_out_until) > new Date());
  const manager = actsAsManager(c);
  return { member, timedOut, manager, canPost: member && canPost(c, true) && !timedOut, canRemovePreview: member && (m.author_id === store.me.id || manager) };
}

// Whether the author may still edit: the server's edit window, if it has
// one, counts from when the message was posted.
function mayEdit(m) {
  if (m.author_id !== store.me.id || m.deleted_at || m.system || m.sealed) return false;
  const w = store.server.limits.edit_window_seconds;
  return w == null || (Date.now() - new Date(m.created_at).getTime()) / 1000 < w;
}

export function recordRecentEmoji(value) {
  const list = [value, ...(store.prefs.recent || []).filter((v) => v !== value)].slice(0, 24);
  store.prefs.recent = list;
  savePrefs();
}

export function pickEmoji(anchor, onPick) {
  openPicker(anchor, {
    groups: store.standard.groups, aliases: store.standard.byCode, custom: store.emoji, recent: store.prefs.recent || [],
    onPick: (v) => { recordRecentEmoji(v); onPick(v); },
  });
}

async function toggleReaction(m, emoji, on) {
  try {
    if (on) await put(`/messages/${m.id}/reactions/${encodeURIComponent(emoji)}`);
    else await del(`/messages/${m.id}/reactions/${encodeURIComponent(emoji)}`);
    // The event that follows carries the new counts. A public channel
    // read without joining gets no events, so the list is asked for.
    const c = conversation(m.conversation_id);
    if (!c || !store.conversations.has(c.id)) { m.reactions = (await get(`/messages/${m.id}/reactions`)).reactions; m._v++; store.emit('messages', m.conversation_id); }
  } catch (err) { if (!err.silent) toast(err.message, true); }
}

// --- the history view ---

// A small word diff: what of `from` is not in `to` is marked removed, and
// what of `to` is not in `from` is marked added.
function diffWords(from, to) {
  const a = from.split(/(\s+)/), b = to.split(/(\s+)/);
  if (a.length * b.length > 400000) return [{ t: 'del', s: from }, { t: 'ins', s: to }];
  const n = a.length, m = b.length;
  const lcs = Array.from({ length: n + 1 }, () => new Uint16Array(m + 1));
  for (let i = n - 1; i >= 0; i--) for (let j = m - 1; j >= 0; j--) lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
  const out = [];
  const push = (t, s) => { const last = out[out.length - 1]; if (last && last.t === t) last.s += s; else out.push({ t, s }); };
  let i = 0, j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) { push('same', a[i]); i++; j++; } else if (lcs[i + 1][j] >= lcs[i][j + 1]) push('del', a[i++]); else push('ins', b[j++]);
  }
  while (i < n) push('del', a[i++]);
  while (j < m) push('ins', b[j++]);
  return out;
}

// A message's earlier versions, newest first, each with when it was
// replaced and by whom, and the differences from the version after it
// marked (docs/CLIENTS.md section 9.7).
export function historyDialog(m) {
  return openDialog('History of this message', (close) => {
    const body = h('div', { class: 'stack' }, h('p', { class: 'loading' }, 'Loading'));
    get(`/messages/${m.id}/history`).then(({ versions }) => {
      if (!versions.length) { fill(body, h('p', { class: 'dim' }, 'There is nothing to show: the history was purged, or is hidden while an admin decides.')); return; }
      const sorted = [...versions].sort((x, y) => y.version - x.version);
      fill(body, sorted.map((v, i) => {
        const after = i === 0 ? (m.deleted_at ? null : m.text || '') : sorted[i - 1].text;
        const diff = after == null ? null : diffWords(v.text || '', after);
        return h('section', { class: 'history-version' },
          h('div', { class: 'label' }, `Version ${v.version} · ${v.reason === 'delete' ? 'deleted' : 'replaced'} ${dateTime(v.replaced_at)} by ${nameOf(v.replaced_by_id)}`),
          renderMarkdown(v.text, v.entities, mdContext({ ...m, tasks: [], id: null })),
          (v.files || []).length ? h('p', { class: 'dim small' }, 'Files: ' + v.files.map((f) => f.name).join(', ')) : null,
          diff && diff.some((d) => d.t !== 'same') ? h('p', { class: 'history-diff', 'aria-label': 'What changed in the next version' },
            diff.map((d) => (d.t === 'same' ? d.s : h(d.t === 'del' ? 'del' : 'ins', {}, d.s)))) : null);
      }));
    }).catch((err) => fill(body, h('p', { class: err.code === 'forbidden' || err.code === 'not_found' ? 'note' : 'problem' },
      err.code === 'forbidden' ? 'This history is hidden while an admin decides on a request to erase it.' : err.message)));
    return { body, foot: [button('Close', () => close())] };
  }, { wide: true });
}

// --- making an env ---

// makeEnv builds the env for one place messages are drawn. host:
//   reply(m, quote)   start a reply to m in this place's way
//   edit(m)           put m into this place's composer to edit
//   redraw()          draw the list again
//   inThread          true in the thread pane
export function makeEnv(host) {
  const on = {
    profile: (id, el) => openProfile(id, el),
    openConversation: (id) => ui.openConversation(id),
    openThread: (m) => ui.openThread(m.conversation_id, m.id),
    jump: (m) => ui.openConversation(m.conversation_id, m.parent_id && !host.flat ? { threadId: m.parent_id } : { messageId: m.id }),
    redraw: () => host.redraw(),
    history: historyDialog,
    purge: askPurge,
    reply: (m) => host.reply(m, selectionQuote(m)),
    react: (m, anchor) => pickEmoji(anchor, (emoji) => toggleReaction(m, emoji, true)),
    toggleReaction,
    formChanged: (m, form) => { m.form = form; m._v = (m._v || 0) + 1; store.emit('messages', m.conversation_id); },
    interaction: (id) => store.emit('waiting', { interactionId: id }),
    removePreview: async (m, index) => {
      try { putMessage((await del(`/messages/${m.id}/previews/${index}`)).message); } catch (err) { toast(err.message, true); }
    },
    retry: (m) => host.retry && host.retry(m),
    discard: (m) => { const r = room(m.conversation_id); r.list.splice(r.list.indexOf(m), 1); r.byId.delete(m.id); store.emit('messages', m.conversation_id); },
    more: (m, anchor) => messageMenu(m, anchor, host),
  };
  return {
    me: store.me, account, nameOf, conversation, custom: customEmoji, quoted, perms, md: mdContext,
    moderator: (m) => isHubModerator(m.author_id, m.conversation_id),
    blocked: (id) => store.blocked.has(id),
    bookmarked: (id) => store.bookmarks.has(id),
    threadState: (id) => store.threadStates.get(id),
    pickAccounts, pickChannel: (t) => pickChannel(t, visibleChannels(), look),
    systemLine,
    on,
  };
}

// With text selected inside a message, Reply quotes the selection. The
// quote is a range of the message's own text, so the selection is looked
// for there; if it is not found as typed, the whole message is quoted.
function selectionQuote(m) {
  const sel = window.getSelection();
  const text = sel && !sel.isCollapsed ? String(sel).trim() : '';
  if (text && m.text) {
    const el = document.querySelector(`.msg[data-id="${m.id}"] .md`);
    if (el && el.contains(sel.anchorNode)) {
      const at = m.text.indexOf(text);
      if (at >= 0) return { message_id: m.id, start: at, end: at + text.length };
      return { message_id: m.id };
    }
  }
  return null;
}

const REMIND = [['In 20 minutes', 20 * 60], ['In an hour', 3600], ['In 3 hours', 3 * 3600], ['Tomorrow', 24 * 3600], ['In a week', 7 * 24 * 3600]];

async function remindAbout(m, seconds) {
  let at = seconds ? new Date(Date.now() + seconds * 1000).toISOString() : null;
  if (!at) {
    const v = await formDialog('Remind me about this', [{ name: 'at', label: 'When', type: 'datetime', required: true, value: new Date(Date.now() + 3600e3) }], { yes: 'Set the reminder' });
    if (!v || !v.at) return;
    at = v.at;
  }
  try {
    // A reminder to yourself arrives in your own one-person channel.
    const mine = (await post('/chats', { account_ids: [] })).conversation;
    if (!store.conversations.has(mine.id)) { store.conversations.set(mine.id, mine); store.emit('conversations'); }
    await post('/scheduled', { conversation_id: mine.id, kind: 'reminder', content: { text: 'Reminder' }, about_message_id: m.id, run_at: at });
    toast('Reminder set for ' + dateTime(at) + '.');
  } catch (err) { if (!err.silent) toast(err.message, true); }
}

// remindAtInstant sets a reminder about m at the moment at, as "Remind me
// about this" does; the caller says what happened.
async function remindAtInstant(m, at) {
  const mine = (await post('/chats', { account_ids: [] })).conversation;
  if (!store.conversations.has(mine.id)) { store.conversations.set(mine.id, mine); store.emit('conversations'); }
  await post('/scheduled', { conversation_id: mine.id, kind: 'reminder', content: { text: 'Reminder' }, about_message_id: m.id, run_at: at });
}

// whichDelete is the delete the reader is offered for a message, or null.
// In a channel a hub owns, a moderator deletes for everyone and a local
// manager who is not one deletes here only (docs/PERMISSIONS.md 14.1).
export function whichDelete(m) {
  const c = conversation(m.conversation_id);
  const p = perms(m);
  const hub = hubOf(c);
  return deleteChoice({ mine: m.author_id === store.me.id, manager: p.manager, hub: hub && hub.channel, moderator: !!(hub && hub.moderator) });
}

// isHubModerator says whether an account is a moderator of the channel a
// hub owns that a message is in, for the badge beside its name.
export function isHubModerator(accountId, conversationId) {
  const hub = hubOf(conversation(conversationId));
  return !!(hub && hub.channel && isModerator(hub.channel, account(accountId), store.server.name));
}

// Deleting and pinning are reached from the menu and from the keyboard.
export async function deleteMessage(m) {
  const c = conversation(m.conversation_id);
  const choice = whichDelete(m);
  if (m.deleted_at || !choice) return;
  const hub = hubOf(c);
  const heading = choice.label.replace('…', '') + '?';
  const yes = await confirmDialog(heading === 'Delete?' ? 'Delete this message?' : heading, c && isSealed(c) ? 'It is removed for both of you.'
    : deleteWords(choice, hub && hub.channel), { yes: choice.label.replace('…', ''), danger: true });
  if (!yes) return;
  try { putMessage((await del('/messages/' + m.id)).message); } catch (err) { if (!err.silent) toast(err.message, true); }
}

// Asking for a message's history to be purged says first what that means
// in this kind of channel.
export async function askPurge(m) {
  const c = conversation(m.conversation_id);
  const here = c && (c.private || isAdHoc(c));
  const yes = await confirmDialog('Purge this message’s history?', here
    ? 'In a private channel the earlier versions are erased at once. That cannot be undone.'
    : 'In a public channel the earlier versions are hidden from everyone at once, and an admin decides whether they are erased. You are not told the outcome.', { yes: here ? 'Erase' : 'Ask for it', danger: true });
  if (!yes) return;
  try {
    const out = await post(`/messages/${m.id}/purge`);
    putMessage(out.message);
    toast(out.state === 'purged' ? 'The history is erased.' : 'Asked. The history is hidden meanwhile.');
  } catch (err) { if (!err.silent) toast(err.message, true); }
}

export async function togglePin(m) {
  if (!perms(m).manager) return;
  try {
    if (m.pinned) await del(`/conversations/${m.conversation_id}/pins/${m.id}`);
    else { await put(`/conversations/${m.conversation_id}/pins/${m.id}`); toast('Pinned.'); }
  } catch (err) { if (!err.silent) toast(err.message, true); }
}

// The menu of a message: the actions of docs/CLIENTS.md section 9.6 that
// apply to this message and this reader.
function messageMenu(m, anchor, host) {
  const c = conversation(m.conversation_id);
  const p = perms(m);
  const mine = m.author_id === store.me.id;
  const sealed = c && isSealed(c);
  const parentId = m.parent_id || (m.thread && m.thread.reply_count ? m.id : null);
  const state = parentId && store.threadStates.get(parentId);
  const run = async (fn, said) => { try { const out = await fn(); if (said) toast(said); return out; } catch (err) { if (!err.silent) toast(err.message, true); return undefined; } };
  const setThread = (value) => run(async () => {
    const out = await put(`/messages/${parentId}/thread/state`, { state: value });
    if (out.thread_state) store.threadStates.set(parentId, out.thread_state); else store.threadStates.delete(parentId);
    store.emit('threads', parentId); store.emit('messages', m.conversation_id);
  });
  const dndTargets = mine && !m.via ? (m.entities || []).filter((e) => e.type === 'account').map((e) => account(e.account_id)).filter((a) => a && a.dnd && a.id !== store.me.id) : [];

  openMenu(anchor, [
    p.member && !p.timedOut && { label: 'React', icon: 'emoji', onclick: () => pickEmoji(anchor.getBoundingClientRect ? anchor : document.querySelector(`.msg[data-id="${m.id}"]`), (e) => toggleReaction(m, e, true)) },
    p.canPost && { label: host.inThread ? 'Reply with a quote' : 'Reply in thread', icon: 'reply', onclick: () => host.reply(m, selectionQuote(m)) },
    { label: 'Copy text', icon: 'copy', onclick: () => copyText(plainText(m.text, m.entities, mdContext(m)), 'Text copied') },
    { label: 'Copy link', icon: 'link', onclick: () => copyText(linkTo(m.conversation_id, m.id, m.parent_id), 'Link copied') },
    !sealed && (store.bookmarks.has(m.id)
      ? { label: 'Remove bookmark', icon: 'bookmark', onclick: () => run(async () => { await del(`/messages/${m.id}/bookmark`); store.bookmarks.delete(m.id); store.emit('bookmarks'); store.emit('messages', m.conversation_id); }) }
      : { label: 'Bookmark', icon: 'bookmark', onclick: () => run(async () => { await put(`/messages/${m.id}/bookmark`); store.bookmarks.add(m.id); m._v++; store.emit('bookmarks'); store.emit('messages', m.conversation_id); }, 'Bookmarked.') }),
    !sealed && p.manager && { label: m.pinned ? 'Unpin' : 'Pin', icon: 'pin', onclick: () => togglePin(m) },
    !sealed && { label: 'Remind me about this…', icon: 'clock', onclick: () => openMenu(document.querySelector(`.msg[data-id="${m.id}"]`) || document.body,
      [...REMIND.map(([label, s]) => ({ label, onclick: () => remindAbout(m, s) })), { label: 'Pick a time…', onclick: () => remindAbout(m, 0) }], { label: 'When' }) },
    { sep: true },
    mayEdit(m) && p.canPost && { label: 'Edit', icon: 'edit', onclick: () => host.edit(m) },
    m.has_history && { label: 'History', icon: 'history', onclick: () => historyDialog(m) },
    parentId && p.member && !sealed && (!state || state.state !== 'following') && { label: 'Follow thread', icon: 'bell', onclick: () => setThread('following') },
    parentId && p.member && !sealed && state && state.state === 'following' && { label: 'Stop following', icon: 'bell-off', onclick: () => setThread('none') },
    parentId && p.member && !sealed && (!state || state.state !== 'ignored') && { label: 'Ignore thread', icon: 'eye-off', onclick: () => setThread('ignored') },
    p.member && m.seq != null && { label: 'Mark unread from here', icon: 'dot', onclick: () => run(async () => {
      const out = await put(`/conversations/${m.conversation_id}/read`, { seq: m.seq - 1 });
      if (c.membership) c.membership = out.membership;
      store.emit('conversations');
    }, 'Marked unread.') },
    dndTargets.length && { head: 'In do not disturb' },
    ...dndTargets.map((a) => ({ label: `Notify ${a.display_name || a.username} anyway`, icon: 'bell', onclick: () => run(async () => putMessage((await post(`/messages/${m.id}/notify-anyway`, { account_id: a.id })).message), 'Asked. You are never told whether they were reached.') })),
    { sep: true },
    !sealed && !mine && p.member !== undefined && { label: 'Report…', icon: 'flag', onclick: async () => {
      const reason = await promptDialog('Report this message', 'Why', { required: false, multiline: true, yes: 'Report', maxLength: 500,
        message: c && c.private ? 'The report goes to this channel’s managers.' : 'The report goes to this channel’s managers and to the admins.' });
      if (reason != null) run(() => post(`/messages/${m.id}/report`, { reason: reason.trim() || undefined }), 'Reported.');
    } },
    !sealed && mine && (m.has_history || m.deleted_at) && { label: 'Ask for purge…', icon: 'trash', onclick: () => askPurge(m) },
    !m.deleted_at && whichDelete(m) && { label: whichDelete(m).label, icon: 'trash', danger: true, onclick: () => deleteMessage(m) },
  ], { alignRight: !!anchor.getBoundingClientRect, label: 'Message actions' });
}

// moveThread asks for a name and moves a thread out to its own channel.
// It cannot be undone, and the dialog says so.
export async function moveThread(parent) {
  const out = await formDialog('Move this thread to a channel', [
    { name: 'name', label: 'Name of the new channel', required: true, maxLength: 64 },
  ], { yes: 'Move it', danger: true, intro: ['The replies move to a new channel, which starts with a copy of this message. A line here points to it, and this thread takes no more replies.', 'This cannot be undone.'],
    submit: (v) => post(`/messages/${parent.id}/move-to-channel`, { name: v.name }) });
  if (out && out.conversation) {
    store.conversations.set(out.conversation.id, out.conversation);
    store.emit('conversations');
    ui.openConversation(out.conversation.id);
  }
}

export { mdContext, perms, mayEdit, toggleReaction };
