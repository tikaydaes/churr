// The web chat's memory: everything it has loaded, kept in step by the
// live connection (docs/CLIENTS.md sections 7 and 17).
//
// The web chat keeps no copy of messages outside the page. What is here is
// what has been fetched since the page opened; reloading fetches again.
// The one thing kept in the browser is the default for "show in sidebar"
// and the "show hidden" toggle (section 22), in local storage under the
// account's identifier.
//
// Screens read from this object and listen for changes:
//
//   store.on('conversations', fn)     the list, a membership, a badge
//   store.on('messages', fn)          fn(conversationId): its messages changed
//   store.on('accounts', fn)          a profile, presence, status
//   store.on('me', fn)                the reader's own account
//   store.on('threads', fn)           a thread state changed
//   store.on('connection', fn)        the live connection's state
//   store.on('typing', fn)            fn(conversationId)
//   store.on('notifications', fn)     the notifications list
//   store.on('interaction', fn)       fn(frame): a bot answered this person
//   store.on('lists', fn)             sections, groups, emoji
//   store.on('themes', fn)            the theme library changed
//   store.on('hub', fn)               fn(conversationId): a hub's record of a channel changed

import { get, all, put } from '/shared/api.js';
import { Live } from '/shared/live.js';
import { sortByOrder, seqRange } from './order.js';
import { rememberServerDefaults } from '/shared/themes.js';
import { isModerator } from '/shared/hub.js';

const listeners = new Map();

export const store = {
  me: null,
  server: null,
  connection: 'offline',
  conversations: new Map(), // id → Conversation the reader is in
  others: new Map(),        // id → a public channel seen without joining, or a parent
  rooms: new Map(),         // id → { list, byId, synced, hasOlder, loaded, loading, readAtOpen }
  accounts: new Map(),
  groups: [],
  sections: [],
  emoji: [],                // custom emoji
  standard: { groups: [], byCode: new Map() },
  threadStates: new Map(),  // parent id → ThreadState
  typing: new Map(),        // conversation id → Map(account id + parent → { accountId, parentId, until })
  total: { badge: 0, mentions: 0, unread_conversations: 0, unread_threads: 0 },
  // `presence.counts`: how many people are online on the server (null if
  // the server does not say) and in each open conversation that is large.
  online: { server: null, conversations: {} },
  notifications: [],        // { key, conversationId, parentId, message, why, at, cleared }
  bookmarks: new Set(),
  blocked: new Set(),
  current: null,            // the conversation on screen
  hubChannels: new Map(),   // conversation id → { channel, moderator }: the hub's record of a channel it owns
  prefs: { showDefault: 'always', showHidden: false, collapsed: {} },

  on(name, fn) {
    if (!listeners.has(name)) listeners.set(name, new Set());
    listeners.get(name).add(fn);
    return () => listeners.get(name).delete(fn);
  },
  emit(name, arg) {
    for (const fn of listeners.get(name) || []) {
      try { fn(arg); } catch (err) { console.error(err); }
    }
  },
};

// --- what is kept in the browser ---

const prefsKey = () => 'churr.' + store.me.id;

export function loadPrefs() {
  try {
    const saved = JSON.parse(localStorage.getItem(prefsKey()) || '{}');
    if (['always', 'activity', 'notification'].includes(saved.showDefault)) store.prefs.showDefault = saved.showDefault;
    store.prefs.showHidden = saved.showHidden === true;
    store.prefs.collapsed = saved.collapsed && typeof saved.collapsed === 'object' ? saved.collapsed : {};
    store.prefs.notify = saved.notify !== false;
    store.prefs.recent = Array.isArray(saved.recent) ? saved.recent.slice(0, 24) : [];
  } catch { /* nothing kept, or storage is closed to this page */ }
}

export function savePrefs() {
  try { localStorage.setItem(prefsKey(), JSON.stringify(store.prefs)); } catch { /* the choice lasts for this page only */ }
  store.emit('conversations');
}

// Signing out removes what the browser kept.
export function forgetPrefs() {
  try { localStorage.removeItem(prefsKey()); } catch { /* nothing kept */ }
}

// --- accounts ---

// account returns what is known of an account, and asks for it if nothing
// is: the screens redraw when 'accounts' fires.
let wanted = new Set();
let wantTimer = null;
export function account(id) {
  if (!id) return undefined;
  const a = store.accounts.get(id);
  if (!a && !wanted.has(id)) {
    wanted.add(id);
    clearTimeout(wantTimer);
    wantTimer = setTimeout(fetchWanted, 30);
  }
  return a;
}

async function fetchWanted() {
  const ids = [...wanted].filter((id) => !store.accounts.has(id));
  for (let i = 0; i < ids.length; i += 50) {
    try {
      const { accounts } = await get('/accounts', { ids: ids.slice(i, i + 50).join(','), include_removed: true, limit: 200 });
      for (const a of accounts) store.accounts.set(a.id, a);
      // One that could not be found is remembered as unknown, so it is not
      // asked for on every redraw.
      for (const id of ids.slice(i, i + 50)) if (!store.accounts.has(id)) store.accounts.set(id, { id, username: 'unknown', display_name: 'Someone', presence: 'offline', unknown: true });
    } catch { /* asked again the next time the name is needed */ wanted = new Set([...wanted].filter((id) => !ids.includes(id))); }
  }
  store.emit('accounts');
}

export function needAccounts(ids) { for (const id of ids || []) account(id); }

export function nameOf(id) {
  const a = account(id);
  return a ? (a.display_name || a.username) : '…';
}

// --- conversations ---

// conversation returns any conversation the reader can see: one they are
// in, or a public one that was looked up.
export function conversation(id) {
  return store.conversations.get(id) || store.others.get(id);
}

// seeConversation looks a conversation up by id if it is not known, for a
// parent the reader is not in or a channel link. A channel the reader may
// not see answers not_found, and is remembered as unseen.
const seeing = new Map();
// refreshBroad asks again whether the reader may use broad mentions in a
// conversation it holds (ruling R64).
function refreshBroad(id) {
  get('/conversations/' + id).then((r) => {
    const c = store.conversations.get(id);
    if (!c || r.conversation.broad_mentions_allowed === undefined) return;
    c.broad_mentions_allowed = r.conversation.broad_mentions_allowed;
    store.emit('conversations');
  }).catch(() => {});
}

export function seeConversation(id) {
  if (conversation(id)) return Promise.resolve(conversation(id));
  if (!seeing.has(id)) {
    seeing.set(id, get('/conversations/' + id).then((r) => {
      if (!store.conversations.has(id)) store.others.set(id, r.conversation);
      store.emit('conversations');
      // A channel link that was drawn "private channel" while this was
      // being looked up can now show the channel's name.
      store.emit('seen', id);
      return r.conversation;
    }).catch(() => null));
  }
  return seeing.get(id);
}

// refreshConversation fetches one conversation the reader is in again and
// keeps the reader's own membership if the answer has none.
export async function refreshConversation(id) {
  try {
    const { conversation: c } = await get('/conversations/' + id);
    const old = store.conversations.get(id);
    if (!c.membership && !(old && old.membership)) return;
    store.conversations.set(id, { ...c, membership: c.membership || old.membership });
    if (!c.name) needAccounts(c.member_ids);
    store.emit('conversations');
  } catch { /* the list is fetched again on the next reconnect */ }
}

// --- channels a hub owns ---

// hubOf returns what is known of the hub's record of a shared channel:
// { channel, moderator } once fetched, { none: true } when the channel is
// not one a hub owns, undefined while it is asked for. It asks once, and
// the screens redraw on 'hub' and 'messages' when the answer comes.
export function hubOf(c) {
  if (!c || !c.shared || !c.name) return null;
  const known = store.hubChannels.get(c.id);
  if (known && (known.channel || known.none)) return known;
  if (!known) loadHub(c);
  return undefined;
}

export async function loadHub(c) {
  store.hubChannels.set(c.id, { loading: true });
  try {
    const out = await get(`/streams/${c.shared.stream_id}/channel`);
    store.hubChannels.set(c.id, { channel: out.channel, moderator: out.moderator });
  } catch (err) {
    // A channel that is not on a hub answers not_found; anything else is
    // asked again next time.
    if (err.code === 'not_found' || err.status === 404) store.hubChannels.set(c.id, { none: true });
    else store.hubChannels.delete(c.id);
  }
  store.emit('hub', c.id);
  store.emit('messages', c.id);
  return store.hubChannels.get(c.id);
}

export const isAdHoc = (c) => c.kind === 'channel' && !c.name;
export const isSealed = (c) => c.kind === 'sealed';

// title is what a conversation is called: its name, or for an ad-hoc
// channel or a sealed chat the people in it other than the reader.
export function title(c) {
  if (!c) return 'unknown channel';
  if (c.name) return c.name;
  const others = (c.member_ids || []).filter((id) => id !== store.me.id);
  if (!others.length) return (c.member_ids || []).length ? 'Notes to self' : 'Chat';
  return others.slice(0, 4).map(nameOf).join(', ') + (others.length > 4 ? ` and ${others.length - 4} more` : '');
}

// pathTitle is `parent › name` for a sub-channel whose parent the reader
// can see.
export function pathTitle(c) {
  const parent = c && c.parent_id && conversation(c.parent_id);
  return parent ? `${title(parent)} › ${title(c)}` : title(c);
}

export function isMuted(c) {
  const m = c.membership;
  return !!(m && m.muted_until && new Date(m.muted_until) > new Date());
}

// The effective "show in sidebar" of a membership: its own, or the default
// kept in this browser.
export function showOf(c) {
  return (c.membership && c.membership.show) || store.prefs.showDefault;
}

// Whether a conversation has a row (docs/CLIENTS.md section 9.2). The one
// being viewed always has; otherwise its `show` decides.
export function hasRow(c) {
  if (!c.membership) return false;
  if (store.current === c.id) return true;
  const m = c.membership;
  switch (showOf(c)) {
    case 'activity': return m.unread_count > 0;
    case 'notification': return m.badge_count > 0;
    case 'hidden': return store.prefs.showHidden;
    default: return true;
  }
}

export function myLevel(c) { return c && c.membership ? c.membership.level : null; }
export function isAdmin() { return store.me.role === 'owner' || store.me.role === 'admin'; }
// "Acting as manager": a manager of the channel, or an admin in a public
// channel or a private one they belong to.
export function actsAsManager(c) {
  if (!c || isSealed(c)) return false;
  if (myLevel(c) === 'manager') return true;
  return isAdmin() && !isAdHoc(c) && (!c.private || !!c.membership);
}
// In an announcement channel only its managers and the members they allow
// post; the others may reply in threads if the channel says so
// (docs/FEATURES.md section 1.9). reply asks about a reply in a thread.
export function mayAnnounce(c) {
  return actsAsManager(c) || !!(c.membership && c.membership.may_post);
}
export function canPost(c, reply) {
  const lv = myLevel(c);
  if (!lv || lv === 'reader' || c.archived_at || c.read_only) return false;
  return !c.announcement || mayAnnounce(c) || (!!reply && !!c.announcement_replies);
}
// What to say to someone who can read a conversation and not post in it.
export function whyNoPost(c) {
  if (c.archived_at) return 'This channel is archived.';
  if (c.announcement && myLevel(c) && myLevel(c) !== 'reader') return 'Only this channel’s managers and the people they allow post here.';
  return 'You can read here, and not post.';
}
// A large conversation shows nobody typing and no presence of single
// members: an online count instead, where the server sends one.
export function isLarge(c) { return !!(c && c.large); }
export function onlineIn(id) {
  const n = store.online.conversations[id];
  return typeof n === 'number' ? n : null;
}

// --- loading ---

export async function loadEverything() {
  const [convs, sections, groups, emoji, total, standard] = await Promise.all([
    all('/conversations', 'conversations'),
    get('/sections').then((r) => r.sections),
    get('/groups').then((r) => r.groups),
    get('/emoji').then((r) => r.emoji).catch(() => []),
    get('/status').catch(() => store.total),
    store.standard.groups.length ? Promise.resolve(null) : fetch('/shared/emoji/emoji.json').then((r) => r.json()).catch(() => null),
  ]);
  store.conversations = new Map(convs.map((c) => [c.id, c]));
  for (const c of convs) store.others.delete(c.id);
  store.sections = sections.sort((a, b) => a.position - b.position);
  store.groups = groups;
  store.emoji = emoji;
  store.total = total;
  if (standard) {
    store.standard.groups = standard.groups;
    for (const g of standard.groups) for (const [ch, code] of g.emoji) if (code.length <= 32 && !store.standard.byCode.has(code)) store.standard.byCode.set(code, ch);
    for (const [code, ch] of Object.entries(standard.aliases || {})) store.standard.byCode.set(code, ch);
  }
  store.blocked = new Set(store.me.blocked_ids || []);
  // The people of the small chats are needed to name them.
  for (const c of convs) if (!c.name) needAccounts(c.member_ids);
  // A sub-channel whose parent the reader is not in still shows the
  // parent's name, if the reader can see the parent.
  for (const c of convs) if (c.parent_id && !store.conversations.has(c.parent_id)) seeConversation(c.parent_id);
  store.emit('conversations');
  store.emit('lists');
}

export async function refreshMe() {
  store.me = (await get('/me')).account;
  store.accounts.set(store.me.id, { ...store.accounts.get(store.me.id), ...store.me });
  store.blocked = new Set(store.me.blocked_ids || []);
  store.emit('me');
}

// --- rooms: the messages of one conversation ---

function room(id) {
  if (!store.rooms.has(id)) store.rooms.set(id, { list: [], byId: new Map(), synced: 0, hasOlder: true, loaded: false, loading: null, pending: [], readAtOpen: 0 });
  return store.rooms.get(id);
}
export { room };

// A conversation is shown sorted by `order`, not by `seq` (order.js).
function sortRoom(r) { sortByOrder(r.list); }

function upsert(r, m) {
  const old = r.byId.get(m.id);
  if (old) {
    // What only this page knows about a message survives the server's copy.
    m._v = (old._v || 0) + 1;
    if (m.thread === undefined && old.thread && !m.parent_id) m.thread = old.thread;
    Object.assign(old, m);
    for (const k of Object.keys(old)) if (!(k in m) && !k.startsWith('_')) delete old[k];
    return old;
  }
  m._v = 0;
  r.byId.set(m.id, m);
  r.list.push(m);
  return m;
}

function noteMessages(list, states) {
  for (const m of list) {
    account(m.author_id);
    if (m.deleted_by_id) account(m.deleted_by_id);
    if (m.thread) needAccounts(m.thread.participant_ids);
    for (const e of m.entities || []) if (e.type === 'account') account(e.account_id);
    for (const t of m.tasks || []) if (t.checked_by_id) account(t.checked_by_id);
  }
  for (const s of states || []) store.threadStates.set(s.parent_id, s);
}

// openRoom loads the newest page of a conversation, or the page around one
// message. Events that arrive while it loads are applied afterwards.
export function openRoom(id, around) {
  const r = room(id);
  if (r.loaded && !around) return Promise.resolve(r);
  if (r.loading && !around) return r.loading;
  const c = conversation(id);
  const seqBefore = c ? c.last_seq : 0;
  r.loading = get(`/conversations/${id}/messages`, { limit: 60, around }).then((page) => {
    r.list = []; r.byId = new Map();
    for (const m of page.messages) upsert(r, m);
    sortRoom(r);
    noteMessages(page.messages, page.thread_states);
    r.hasOlder = around ? true : page.has_more;
    // After a jump there may be newer messages than the page holds.
    const range = seqRange(page.messages);
    r.hasNewer = !!around && !!range && !!c && range.max < c.last_seq;
    // What the page holds ends at the newest seq it knew of before it
    // asked, or the newest message it was given. Not at whatever
    // `last_seq` has become meanwhile: in a conversation delivered lazily
    // that grows from `conversation.changed` alone, and the events it
    // speaks of are still to come (syncOpen asks for them from here).
    // Events that arrived while this loaded are applied below.
    r.synced = Math.max(seqBefore, range ? range.max : 0);
    r.readAtOpen = c && c.membership ? c.membership.read_seq : r.synced;
    r.loaded = true;
    r.loading = null;
    const held = r.pending; r.pending = [];
    for (const e of held) applyToRoom(r, e);
    store.emit('messages', id);
    syncOpen();
    return r;
  }).catch((err) => { r.loading = null; throw err; });
  return r.loading;
}

export async function loadOlder(id) {
  const r = room(id);
  // Pages are cut by `seq`: the next older page is before the lowest seq
  // held, wherever that message is shown.
  const range = seqRange(r.list);
  if (!r.loaded || !r.hasOlder || r.loadingOlder || !range) return false;
  r.loadingOlder = true;
  try {
    const page = await get(`/conversations/${id}/messages`, { limit: 60, before: range.min });
    for (const m of page.messages) upsert(r, m);
    sortRoom(r);
    noteMessages(page.messages, page.thread_states);
    r.hasOlder = page.has_more;
    store.emit('messages', id);
    return page.messages.length > 0;
  } finally { r.loadingOlder = false; }
}

export async function loadNewer(id) {
  const r = room(id);
  if (!r.loaded || !r.hasNewer || r.loadingNewer) return;
  r.loadingNewer = true;
  try {
    const range = seqRange(r.list);
    const page = await get(`/conversations/${id}/messages`, { limit: 60, after: range ? range.max : 0 });
    for (const m of page.messages) upsert(r, m);
    sortRoom(r);
    noteMessages(page.messages, page.thread_states);
    r.hasNewer = page.has_more;
    if (!r.hasNewer) {
      // The newest page: what the page holds ends at its newest message,
      // and the server is asked for whatever followed (syncOpen).
      const end = seqRange(r.list);
      r.synced = Math.max(r.synced, end ? end.max : 0);
      syncOpen();
    }
    store.emit('messages', id);
  } finally { r.loadingNewer = false; }
}

export function dropRoom(id) { store.rooms.delete(id); }

// message finds a loaded message by id, in any room.
export function findMessage(id) {
  for (const r of store.rooms.values()) { const m = r.byId.get(id); if (m) return m; }
  return undefined;
}

// putMessage records a message the page got from a request (posting,
// editing, a thread page), so the screen shows it without waiting for the
// event that follows.
export function putMessage(m) {
  const r = room(m.conversation_id);
  if (!r.loaded) return m;
  const was = r.byId.has(m.id);
  const kept = upsert(r, m);
  if (!was) { sortRoom(r); bumpParent(r, kept); }
  noteMessages([kept]);
  store.emit('messages', m.conversation_id);
  return kept;
}

// A reply the page has not seen before updates its parent's summary, as
// docs/EVENTS.md section 8 asks of the client.
function bumpParent(r, m) {
  if (!m.parent_id) return;
  const parent = r.byId.get(m.parent_id);
  if (!parent) return;
  const t = parent.thread || { reply_count: 0, last_reply_seq: 0, last_reply_at: m.created_at, participant_ids: [] };
  if (m.seq <= t.last_reply_seq) return; // already counted
  parent.thread = { reply_count: t.reply_count + 1, last_reply_seq: m.seq, last_reply_at: m.created_at,
    participant_ids: t.participant_ids.includes(m.author_id) ? t.participant_ids : [...t.participant_ids, m.author_id] };
  parent._v = (parent._v || 0) + 1;
}

// --- applying events ---

export function applyToRoom(r, e) {
  if (e.seq != null) {
    if (e.seq <= r.synced && e.type !== 'message.created') return; // already in what was fetched
    if (e.seq > r.synced) r.synced = e.seq;
  }
  const byId = (id) => r.byId.get(id);
  const touch = (m) => { if (m) m._v = (m._v || 0) + 1; };
  switch (e.type) {
    case 'message.created': {
      // The pending copy the page drew for its own message becomes the
      // real one.
      if (e.client_id) { const p = r.list.find((m) => m._pending && m.client_id === e.client_id); if (p) { r.list.splice(r.list.indexOf(p), 1); r.byId.delete(p.id); } }
      const was = r.byId.has(e.message.id);
      const m = upsert(r, e.message);
      if (!was) { sortRoom(r); bumpParent(r, m); }
      noteMessages([m]);
      break;
    }
    case 'message.sent': {
      // The link acknowledged a message of this server's own.
      const m = byId(e.message_id);
      if (m && m.unsent) { delete m.unsent; touch(m); }
      break;
    }
    case 'message.placed':
      // A message's `order` changed: it moves to its new place.
      if (byId(e.message.id)) { upsert(r, e.message); sortRoom(r); noteMessages([e.message]); }
      break;
    case 'message.edited': case 'message.purged': case 'message.history_restored': case 'preview.added': case 'preview.removed':
      if (byId(e.message.id)) { upsert(r, e.message); noteMessages([e.message]); }
      break;
    case 'message.deleted': {
      const old = byId(e.message.id);
      if (!old) break;
      const wasLive = !old.deleted_at;
      upsert(r, e.message);
      noteMessages([e.message]);
      if (wasLive && old.parent_id) { const p = byId(old.parent_id); if (p && p.thread) { p.thread = { ...p.thread, reply_count: Math.max(0, p.thread.reply_count - 1) }; touch(p); } }
      break;
    }
    case 'reaction.added': case 'reaction.removed': {
      const m = byId(e.message_id);
      if (!m) break;
      const list = (m.reactions || []).filter((x) => x.emoji !== e.reaction.emoji);
      const mine = (m.reactions || []).find((x) => x.emoji === e.reaction.emoji);
      // `me` is filled in for the account the frame is sent to; when it is
      // someone else's reaction that changed, the reader's own stays.
      const me = e.account_id === store.me.id ? e.type === 'reaction.added' : (e.reaction.me != null ? e.reaction.me : !!(mine && mine.me));
      if (e.reaction.count > 0) {
        const at = (m.reactions || []).findIndex((x) => x.emoji === e.reaction.emoji);
        const entry = { ...e.reaction, me };
        if (at >= 0) list.splice(at, 0, entry); else list.push(entry);
      }
      m.reactions = list;
      needAccounts(e.reaction.account_ids);
      touch(m);
      break;
    }
    case 'task.changed': {
      const m = byId(e.message_id);
      if (!m) break;
      m.tasks = (m.tasks || []).map((t) => (t.id === e.task.id ? e.task : t));
      if (!m.tasks.some((t) => t.id === e.task.id)) m.tasks.push(e.task);
      if (e.task.checked_by_id) account(e.task.checked_by_id);
      touch(m);
      break;
    }
    case 'form.answered': case 'form.answer_withdrawn': case 'form.option_added': case 'form.closed': {
      const m = byId(e.message_id);
      if (m) { m.form = e.form; touch(m); }
      break;
    }
    case 'pin.added': { const m = byId(e.pin.message_id); if (m) { m.pinned = true; touch(m); } break; }
    case 'pin.removed': { const m = byId(e.message_id); if (m) { m.pinned = false; touch(m); } break; }
    case 'thread.moved': {
      // The replies have gone to the new channel; the pointer stands in
      // their place.
      for (const m of [...r.list]) if (m.parent_id === e.parent.id) { r.list.splice(r.list.indexOf(m), 1); r.byId.delete(m.id); }
      upsert(r, e.parent);
      upsert(r, e.pointer);
      sortRoom(r);
      break;
    }
    default: break;
  }
}

// What a notification item says about why it is there.
function whyNotified(m) {
  const ents = m.entities || [];
  if (ents.some((e) => e.type === 'account' && e.account_id === store.me.id)) return 'mentioned you';
  const g = ents.find((e) => e.type === 'group' && store.groups.some((x) => x.id === e.group_id && x.is_member));
  if (g) return 'mentioned @' + (store.groups.find((x) => x.id === g.group_id) || {}).handle;
  if (ents.some((e) => e.type === 'broadcast')) return 'mentioned the channel';
  if (m.parent_id) return 'reply in a thread you follow';
  return 'new message';
}

function addNotification(m, cleared) {
  const key = m.id;
  if (store.notifications.some((n) => n.key === key)) return;
  store.notifications.unshift({ key, conversationId: m.conversation_id, parentId: m.parent_id || null, message: m, why: whyNotified(m), at: m.created_at, cleared: !!cleared });
  store.notifications.sort((a, b) => b.at.localeCompare(a.at));
  if (store.notifications.length > 300) store.notifications.length = 300;
  store.emit('notifications');
}

// Reading clears: an item is cleared when the read position of its
// conversation, or of its thread, has passed it.
function clearNotifications() {
  let changed = false;
  for (const n of store.notifications) {
    if (n.cleared) continue;
    const c = store.conversations.get(n.conversationId);
    const ts = n.parentId && store.threadStates.get(n.parentId);
    const read = n.parentId ? (ts ? ts.read_seq : 0) : (c && c.membership ? c.membership.read_seq : Infinity);
    if (!c || read >= n.message.seq) { n.cleared = true; changed = true; }
  }
  if (changed) store.emit('notifications');
}

// apply takes one event from the live connection, or from catch-up, and
// brings the store up to date. replayed says it happened while the page
// was away, so it never pops a notification up.
export function apply(e, within) {
  if (e.type === 'conversation.changed') { changed(e); return; }
  // The thread pane keeps its own copy of a thread and follows along.
  store.emit('event', e);
  const cid = e.conversation_id;
  const c = cid && store.conversations.get(cid);

  // Conversation events with a number. One that came inside
  // `conversation.changed` is not the next event of what the page holds:
  // it is not put into the room, which catches up when it is opened.
  if (cid && e.seq != null && !within) {
    if (c && e.seq > c.last_seq) c.last_seq = e.seq;
    const r = store.rooms.get(cid);
    if (r && r.loading) r.pending.push(e);
    else if (r && r.loaded) {
      // A jump in the numbering means something was missed: fetch the gap,
      // then carry on (docs/EVENTS.md section 2).
      if (e.seq > r.synced + 1 && !r.hasNewer && !r.filling) fillGap(cid, r, e);
      else if (r.filling) r.pending.push(e);
      else if (!r.hasNewer) applyToRoom(r, e);
      store.emit('messages', cid);
    }
  }

  switch (e.type) {
    case 'message.created': {
      const m = e.message;
      if (c && !m.quiet) { c.last_message_at = m.created_at; }
      // A message already counted (it came inside `conversation.changed`,
      // or a signal spoke for it) is not counted or announced again when
      // the conversation is opened and catches up.
      const again = c && m.seq <= (c._counted || 0);
      if (c && !again) c._counted = m.seq;
      // A message from someone else ends their typing line.
      const t = store.typing.get(cid);
      if (t) { for (const k of [...t.keys()]) if (k.startsWith(m.author_id)) t.delete(k); store.emit('typing', cid); }
      // The server sends `badge.changed` only for a message its mode
      // counts. A message that is merely unread comes with no numbers, so
      // the page counts it by the rule of docs/FEATURES.md section 2.1:
      // not a system notice, not quiet, not the reader's own (a reminder
      // excepted), not from someone they blocked; top-level, or in the
      // flat view a reply in a thread that is not ignored. The next
      // `badge.changed` or `read_position.moved` replaces the guess.
      if (!again && c && c.membership && !m.system && !m.quiet && !m.deleted_at && !store.blocked.has(m.author_id)
        && (m.author_id !== store.me.id || (m.via && m.via.kind === 'reminder')) && m.seq > c.membership.read_seq) {
        const flat = (c.membership.thread_view || (store.me.prefs && store.me.prefs.thread_view)) === 'flat';
        const st = m.parent_id && store.threadStates.get(m.parent_id);
        if (!m.parent_id || (flat && !(st && st.state === 'ignored'))) c.membership.unread_count = (c.membership.unread_count || 0) + 1;
      }
      if (!again && (e.notify === 'notify' || e.notify === 'badge') && m.author_id !== store.me.id) addNotification(m, false);
      if (!again) store.emit('message.created', e);
      store.emit('conversations');
      break;
    }
    case 'member.joined':
      if (e.conversation) {
        // The reader joined, here or on another device.
        store.conversations.set(cid, e.conversation);
        store.others.delete(cid);
        if (!e.conversation.name) needAccounts(e.conversation.member_ids);
        // A chat is named by who is in it. The conversation in this event
        // comes without its `member_ids`, so it is asked for once.
        if (!e.conversation.name && !e.conversation.member_ids) refreshConversation(cid);
      } else if (c) {
        c.member_count = (c.member_count || 0) + 1;
        if (c.member_ids && !c.member_ids.includes(e.member.account_id)) c.member_ids = [...c.member_ids, e.member.account_id];
      }
      store.emit('conversations'); store.emit('members', cid);
      break;
    case 'member.left':
      if (e.account_id === store.me.id) {
        store.conversations.delete(cid);
        dropRoom(cid);
        store.emit('left', cid);
      } else if (c) {
        c.member_count = Math.max(0, (c.member_count || 1) - 1);
        if (c.member_ids) c.member_ids = c.member_ids.filter((x) => x !== e.account_id);
      }
      store.emit('conversations'); store.emit('members', cid);
      break;
    case 'member.level_changed':
      if (c && e.member.account_id === store.me.id && c.membership) { c.membership.level = e.member.level; refreshBroad(cid); }
      store.emit('conversations'); store.emit('members', cid);
      break;
    case 'channel_group.added': case 'channel_group.changed': case 'channel_group.removed':
      if (e.conversation) store.conversations.set(cid, e.conversation);
      store.emit('conversations'); store.emit('members', cid);
      break;
    case 'conversation.updated': case 'conversation.archived': case 'conversation.unarchived':
      if (e.conversation && store.conversations.has(cid)) {
        // The frame carries the reader's own membership; if it does not,
        // the one already held stays.
        const membership = e.conversation.membership || (c && c.membership);
        const next = { ...e.conversation, membership, call: e.conversation.call || (c && c.call) };
        // Whether this reader may use broad mentions is not in a frame
        // sent to everyone: the answer held stays, and is asked for again
        // when the channel's own rule about them has changed.
        if (next.broad_mentions_allowed === undefined && c && c.broad_mentions_allowed !== undefined) {
          next.broad_mentions_allowed = c.broad_mentions_allowed;
          if (c.broad_mentions !== next.broad_mentions) refreshBroad(cid);
        }
        store.conversations.set(cid, next);
        if (e.conversation.parent_id) seeConversation(e.conversation.parent_id);
      }
      store.emit('conversations');
      break;
    case 'thread.moved': store.emit('thread.moved', e); break;
    case 'message.sent': {
      // It has no number, so it is not among the events above.
      const r = store.rooms.get(cid);
      if (r && r.loading) r.pending.push(e);
      else if (r && r.loaded) { applyToRoom(r, e); store.emit('messages', cid); }
      break;
    }
    case 'typing.started': {
      if (e.account_id === store.me.id) break;
      if (!store.typing.has(cid)) store.typing.set(cid, new Map());
      store.typing.get(cid).set(e.account_id + (e.parent_id || ''), { accountId: e.account_id, parentId: e.parent_id || null, until: Date.now() + 8000 });
      account(e.account_id);
      store.emit('typing', cid);
      setTimeout(() => store.emit('typing', cid), 8100);
      break;
    }
    case 'call.started': if (c) { c.call = e.call; store.emit('conversations'); } break;
    case 'call.ended': if (c) { delete c.call; store.emit('conversations'); } break;

    // Account events.
    case 'presence.changed': { const a = store.accounts.get(e.account_id); if (a) { a.presence = e.presence; a.in_call = e.in_call; store.emit('accounts'); } break; }
    case 'presence.counts':
      store.online = { server: typeof e.online_count === 'number' ? e.online_count : null, conversations: e.conversations || {} };
      store.emit('counts');
      break;
    case 'presence.batch': {
      // Several presence.changed objects in one frame, sent when a great
      // many people's presence changes at once (docs/EVENTS.md section 9).
      let changed = false;
      for (const p of Array.isArray(e.changes) ? e.changes : []) {
        const a = p && store.accounts.get(p.account_id);
        if (a) { a.presence = p.presence; a.in_call = p.in_call; changed = true; }
      }
      if (changed) store.emit('accounts');
      break;
    }
    case 'status.changed': { const a = store.accounts.get(e.account_id); if (a) { a.status = e.status; store.emit('accounts'); } if (e.account_id === store.me.id) { store.me.status = e.status; store.emit('me'); } break; }
    case 'availability.changed': { const a = store.accounts.get(e.account_id); if (a) { a.availability = e.availability; store.emit('accounts'); } if (e.account_id === store.me.id) { store.me.availability = e.availability; store.emit('me'); } break; }
    case 'dnd.changed': {
      const a = store.accounts.get(e.account_id);
      if (a) { a.dnd = e.dnd; store.emit('accounts'); }
      if (e.account_id === store.me.id) { store.me.dnd = e.dnd; if (e.dnd_state) store.me.dnd_state = e.dnd_state; store.emit('me'); }
      break;
    }
    case 'account.updated': case 'account.removed':
      store.accounts.set(e.account.id, { ...store.accounts.get(e.account.id), ...e.account });
      store.emit('accounts');
      break;
    case 'me.updated':
      store.me = { ...store.me, ...e.account };
      store.accounts.set(store.me.id, { ...store.accounts.get(store.me.id), ...e.account });
      store.blocked = new Set(store.me.blocked_ids || [...store.blocked]);
      store.emit('me');
      break;
    case 'group.created': case 'group.updated': case 'group.member_added': case 'group.member_removed':
      if (e.group) { store.groups = [...store.groups.filter((g) => g.id !== e.group.id), e.group]; store.emit('lists'); }
      break;
    case 'group.deleted': store.groups = store.groups.filter((g) => g.id !== (e.group && e.group.id)); store.emit('lists'); break;
    case 'block.added': store.blocked.add(e.account_id); store.emit('me'); store.emit('messages', store.current); break;
    case 'block.removed': store.blocked.delete(e.account_id); store.emit('me'); store.emit('messages', store.current); break;
    case 'read_position.moved': case 'badge.changed':
      if (c && c.membership) {
        if (e.read_seq != null) c.membership.read_seq = e.read_seq;
        c.membership.unread_count = e.unread_count; c.membership.mention_count = e.mention_count; c.membership.badge_count = e.badge_count;
      }
      if (e.total) store.total = { ...store.total, ...e.total };
      clearNotifications();
      store.emit('conversations');
      break;
    case 'membership.updated':
      if (c) { c.membership = e.membership; store.emit('conversations'); store.emit('membership', cid); }
      break;
    case 'thread_state.changed': {
      if (e.thread_state) store.threadStates.set(e.parent_id, e.thread_state); else store.threadStates.delete(e.parent_id);
      clearNotifications();
      store.emit('threads', e.parent_id);
      store.emit('messages', e.conversation_id);
      break;
    }
    case 'bookmark.added': store.bookmarks.add(e.message_id); store.emit('bookmarks'); break;
    case 'bookmark.removed': store.bookmarks.delete(e.message_id); store.emit('bookmarks'); break;
    case 'server.updated': store.server = e.server; rememberServerDefaults(e.server && e.server.default_themes); store.emit('lists'); break;
    case 'theme.published': case 'theme.unpublished': store.emit('themes', e); break;
    case 'stream.channel_changed': {
      // Every member is sent the record without who is invited or asking;
      // whether the reader is a moderator is read off its list.
      store.hubChannels.set(e.conversation_id, { channel: e.channel, moderator: isModerator(e.channel, store.me, store.server.name) });
      store.emit('hub', e.conversation_id);
      store.emit('messages', e.conversation_id);
      break;
    }
    case 'stream.move_changed': store.emit('move', e); break;
    case 'sections.changed': store.sections = (e.sections || []).sort((a, b) => a.position - b.position); store.emit('lists'); store.emit('conversations'); break;
    case 'emoji.added': case 'emoji.updated': store.emoji = [...store.emoji.filter((x) => x.id !== e.emoji.id), e.emoji]; store.emit('lists'); break;
    case 'emoji.removed': store.emoji = store.emoji.filter((x) => x.id !== e.emoji.id); store.emit('lists'); break;
    case 'interaction.responded': store.emit('interaction', e); break;
    case 'scheduled.created': case 'scheduled.updated': case 'scheduled.cancelled': case 'scheduled.fired': store.emit('scheduled', e); break;
    case 'session.created': store.emit('session', e); break;
    case 'timeout.started': case 'timeout.lifted': {
      const t = e.timeout;
      if (t && t.account_id === store.me.id && t.conversation_id) {
        const tc = store.conversations.get(t.conversation_id);
        if (tc && tc.membership) { if (e.type === 'timeout.started') tc.membership.timed_out_until = t.until; else delete tc.membership.timed_out_until; }
        store.emit('conversations');
      }
      break;
    }
    case 'webhook.decided': store.emit('webhook', e); break;
    default: break; // an event this client does not know is ignored
  }
}

// changed takes `conversation.changed`: something happened in a
// conversation that is delivered lazily and is not on screen
// (docs/EVENTS.md section 4.1). The list shows it; the room is left as it
// is and catches up when it is opened. With `event`, the frame carries one
// event the reader should have at once: a message that notifies them, or a
// change to the conversation itself.
function changed(e) {
  const c = store.conversations.get(e.conversation_id);
  if (e.event) { apply(e.event, true); }
  if (!c) return;
  if (e.last_seq > c.last_seq) c.last_seq = e.last_seq;
  if (!e.event && c.membership) {
    if (e.unread_count != null) {
      c.membership.unread_count = e.unread_count; c.membership.mention_count = e.mention_count; c.membership.badge_count = e.badge_count;
      if (e.total) store.total = { ...store.total, ...e.total };
    } else if (e.messages > 0 && e.last_seq > (c._counted || 0)) {
      // The server did not have the numbers at hand; `badge.changed`
      // follows with them. Until then, what came is unread.
      c.membership.unread_count = (c.membership.unread_count || 0) + e.messages;
    }
    if (e.last_seq > (c._counted || 0)) c._counted = e.last_seq;
    if (e.messages > 0) c.last_message_at = e.at || c.last_message_at;
  }
  store.emit('conversations');
}

// syncOpen tells the server which conversation is on screen and where what
// the page holds of it ends. Nothing is open until its room has loaded, or
// while the page shows an old part of it.
export function syncOpen() {
  const id = store.current;
  const r = id && store.rooms.get(id);
  const c = id && store.conversations.get(id);
  const positions = {};
  if (r && r.loaded && !r.hasNewer && c && c.membership) positions[id] = r.synced;
  live.open(positions);
}

async function fillGap(cid, r, first) {
  r.filling = true;
  r.pending.push(first);
  try {
    let after = r.synced, more = true;
    while (more) {
      const page = await get(`/conversations/${cid}/events`, { after, limit: 200 });
      if (page.reload) { dropRoom(cid); store.emit('reload', cid); return; }
      for (const ev of page.events) { applyToRoom(r, ev); after = Math.max(after, ev.seq || after); }
      more = page.has_more && page.events.length > 0;
    }
    // In a shared conversation some numbers never arrive; what was asked
    // for is all there is.
    r.synced = Math.max(r.synced, first.seq - 1);
  } catch { /* the next event will try again */ } finally {
    r.filling = false;
    const held = r.pending; r.pending = [];
    for (const ev of held.sort((a, b) => (a.seq || 0) - (b.seq || 0))) applyToRoom(r, ev);
    store.emit('messages', cid);
  }
}

// --- the live connection ---

export const live = new Live({
  // The web chat holds the conversations whose messages it has loaded.
  positions: () => {
    const out = {};
    for (const [id, r] of store.rooms) if (r.loaded && !r.hasNewer) out[id] = r.synced;
    return out;
  },
  open: () => {
    const id = store.current;
    const r = id && store.rooms.get(id);
    return r && r.loaded && !r.hasNewer ? { [id]: r.synced } : {};
  },
  onEvent: (e) => apply(e),
  onReload: (f) => {
    // Too far behind, or no longer a member: what is held is thrown away.
    dropRoom(f.conversation_id);
    if (f.reason !== 'too_far_behind') { store.conversations.delete(f.conversation_id); store.emit('left', f.conversation_id); }
    store.emit('reload', f.conversation_id);
    store.emit('conversations');
  },
  onReady: async () => {
    // Account state has no numbering and is not replayed, so what the page
    // shows of it is fetched again (docs/EVENTS.md section 2).
    try { await Promise.all([refreshMe(), loadEverything()]); } catch { /* the next reconnect tries again */ }
    store.emit('ready');
  },
  onState: (s) => { store.connection = s; store.emit('connection', s); },
});

// --- presence by interest ---

// The web chat shows presence in three places: beside each chat with one
// other person, in that chat's heading, and on a profile that is open. The
// server is told those accounts and sends presence for them alone
// (docs/EVENTS.md section 4); `shown` is the open profile's account.
let shown = null;
export function watchPresence() {
  if (!store.me) return;
  const ids = [store.me.id];
  if (shown) ids.push(shown);
  for (const c of store.conversations.values()) {
    if (!isAdHoc(c) && !isSealed(c)) continue;
    const others = (c.member_ids || []).filter((x) => x !== store.me.id);
    if (others.length === 1) ids.push(others[0]);
  }
  live.watch(ids);
}
export function showingProfile(id) { shown = id; watchPresence(); }
store.on('conversations', watchPresence);
store.on('ready', watchPresence);

// --- reading ---

// markRead moves the read position of a conversation to its newest
// message, on every device.
let readTimer = null;
export function markRead(id) {
  const c = store.conversations.get(id);
  if (!c || !c.membership) return;
  if (c.membership.read_seq >= c.last_seq && !c.membership.unread_count) return;
  clearTimeout(readTimer);
  readTimer = setTimeout(async () => {
    try {
      const out = await put(`/conversations/${id}/read`, { seq: c.last_seq });
      c.membership = out.membership;
      clearNotifications();
      store.emit('conversations');
    } catch { /* tried again the next time the bottom is on screen */ }
  }, 250);
}

export function typingIn(id, parentId) {
  const t = store.typing.get(id);
  if (!t) return [];
  const now = Date.now();
  return [...t.values()].filter((x) => x.until > now && (x.parentId || null) === (parentId || null)).map((x) => x.accountId);
}
