// The web chat's entry: start-up, moving between screens, and the layout
// of the panes (docs/CLIENTS.md sections 10, 17 and 22).
//
// It is a basic client for when an app is not to hand, and works only
// while its page is open. It is one server's page: the rail layout only,
// with no rail to draw.
//
// Addresses, all under /chat/, moved between with the History interface:
//   /chat/                      the list (and an empty main pane)
//   /chat/c/<id>                a conversation
//   /chat/c/<id>/m/<message>    a conversation, opened at one message
//   /chat/c/<id>/t/<parent>     a conversation with a thread open
//   /chat/threads  /chat/notifications  /chat/bookmarks  /chat/search
//   /chat/scheduled  /chat/browse  /chat/requests  /chat/approvals
//   /chat/notices
//   /chat/themes  /chat/themes/library

import { api, get, onSignedOut, goToSignIn, goToLock } from '/shared/api.js';
import { forgetDrafts, browserStorage } from '/shared/drafts.js';
import { h, fill, toast, button, icon, iconButton, dateTime } from '/shared/dom.js';
import { renderInline, renderMarkdown } from '/shared/md/render.js';
import { STRINGS as CMD } from '/shared/commands.js';
import { installFreshCheck } from '/shared/fresh.js';
import { store, live, loadEverything, loadPrefs, forgetPrefs, conversation, seeConversation, markRead, title, syncOpen, nameOf } from './store.js';
import { ui } from './ui.js';
import { Sidebar, rowOrder } from './list.js';
import { ConversationView, privateResponses } from './conversation.js';
import { ThreadView } from './thread.js';
import { SearchView } from './search.js';
import { ThreadsView } from './threads.js';
import { ThemesView, LibraryView } from './themes.js';
import { rememberServerDefaults } from '/shared/themes.js';
import { BrowseView, NotificationsView, BookmarksView, ScheduledView, newChat, newChannel, switcher, shortcuts } from './more.js';
import { watchInteractions } from './composer.js';
import { startNotifications } from './notify.js';
import { openDnd } from './profile.js';
import { signOutSealed } from './sealed/index.js';
import { RequestsView, notifyRequest } from './requests.js';
import { ApprovalsView, watchApprovals } from './approvals.js';
import { NoticesView } from './notices.js';
import { lockNow, startAutoLock, isLockKey } from './lock.js';
import { notifyKnock, setAsking } from './cmddialogs.js';
import { install as keepComposerFocus, opened } from './keepfocus.js';

const app = document.getElementById('app');
const main = document.getElementById('main');
const side = document.getElementById('sidebar');
const threadPane = document.getElementById('thread');
const announcer = document.getElementById('announcer');

let mainView = null, threadView = null, shown = { path: null };

const VIEWS = { requests: RequestsView, approvals: ApprovalsView, notices: NoticesView, threads: ThreadsView, notifications: NotificationsView, bookmarks: BookmarksView, search: SearchView, scheduled: ScheduledView, browse: BrowseView, themes: ThemesView, library: LibraryView };

function parse(pathname) {
  const p = pathname.replace(/^\/chat\/?/, '').split('/').filter(Boolean);
  if (p[0] === 'c' && p[1]) return { kind: 'conversation', id: p[1], messageId: p[2] === 'm' ? p[3] : null, threadId: p[2] === 't' ? p[3] : null };
  if (p[0] === 'themes') return { kind: 'view', name: p[1] === 'library' ? 'library' : 'themes' };
  if (VIEWS[p[0]] && p[0] !== 'library') return { kind: 'view', name: p[0] };
  return { kind: 'home' };
}

function home() {
  return h('section', { class: 'view empty-view' },
    h('img', { class: 'empty-sticker', src: '/icons/sticker.png', alt: '' }),
    h('h1', {}, 'Pick a ', h('em', {}, 'conversation')),
    h('p', { class: 'dim' }, 'Or press Ctrl K to jump to one. This page works only while it is open; the apps keep you notified when it is not.'),
    // The welcome text is Churr Markdown (docs/FEATURES.md section 13), drawn
    // here as /motd draws it.
    store.server.welcome && store.server.welcome.trim() ? h('div', { class: 'note welcome' }, renderMarkdown(store.server.welcome, [])) : null);
}

// route draws what the address names. Only what changed is rebuilt: moving
// between threads of one conversation keeps the conversation as it is.
async function route(moveFocus) {
  const r = parse(location.pathname);
  const convKey = r.kind === 'conversation' ? 'c:' + r.id : r.kind === 'view' ? 'view:' + r.name + location.search : 'home';
  const before = store.current;
  store.current = r.kind === 'conversation' ? r.id : null;
  // The server is told what is on screen (lazy delivery).
  if (before !== store.current) syncOpen();

  if (shown.convKey !== convKey) {
    if (mainView && mainView.destroy) mainView.destroy();
    mainView = null;
    if (r.kind === 'conversation') {
      if (!conversation(r.id)) await seeConversation(r.id);
      mainView = new ConversationView(r.id, { messageId: r.messageId });
      fill(main, mainView.el);
    } else if (r.kind === 'view') {
      mainView = new VIEWS[r.name]();
      fill(main, mainView.el);
    } else fill(main, home());
    shown.convKey = convKey;
    shown.jump = r.messageId || null;
    // A conversation opened on a desktop puts focus in its composer
    // (keepfocus.js); otherwise the new screen takes it.
    if (!(r.kind === 'conversation' && opened({ force: true })) && moveFocus) main.focus({ preventScroll: true });
  } else if (r.kind === 'conversation' && r.messageId && shown.jump !== r.messageId) {
    // The same conversation, at another message.
    shown.jump = r.messageId;
    mainView.jumpTo(r.messageId);
  } else if (r.kind === 'conversation' && !r.messageId) shown.jump = null;

  const threadKey = r.kind === 'conversation' && r.threadId ? r.id + '|' + r.threadId : null;
  if (shown.threadKey !== threadKey) {
    if (threadView) threadView.destroy();
    threadView = null;
    if (threadKey) {
      threadView = new ThreadView(r.id, r.threadId, ui.pendingThread || {});
      ui.pendingThread = null;
      fill(threadPane, threadView.el);
      threadPane.hidden = false;
    } else { fill(threadPane); threadPane.hidden = true; }
    shown.threadKey = threadKey;
  }

  // Which pane a narrow screen shows: the list, then the conversation,
  // then the thread, each over the last.
  app.dataset.pane = threadKey ? 'thread' : r.kind === 'home' ? 'list' : 'main';
  app.classList.toggle('with-thread', !!threadKey);
  // A row that was only there because its conversation was open goes once
  // the person has moved on.
  if (before !== store.current) store.emit('conversations');
  store.emit('route');
}

function go(path, replace) {
  if (location.pathname + location.search === path) return;
  if (replace) history.replaceState(null, '', path); else history.pushState(null, '', path);
  route(true);
}

Object.assign(ui, {
  go,
  openConversation(id, opts = {}) {
    go('/chat/c/' + id + (opts.threadId ? '/t/' + opts.threadId : opts.messageId ? '/m/' + opts.messageId : ''));
  },
  openThread(conversationId, parentId, opts) {
    ui.pendingThread = opts && opts.quote ? opts : null;
    const path = `/chat/c/${conversationId}/t/${parentId}`;
    if (location.pathname === path) { if (threadView && opts && opts.quote && threadView.composer) { threadView.composer.setQuote(opts.quote, opts.quoteOf); threadView.composer.focus(); } return; }
    history.pushState(null, '', path);
    route(false);
  },
  closeThread() {
    const r = parse(location.pathname);
    if (r.kind === 'conversation' && r.threadId) { history.pushState(null, '', '/chat/c/' + r.id); route(false); if (mainView && mainView.composer && window.matchMedia('(min-width: 600px)').matches) mainView.composer.focus(); }
  },
  refresh: () => route(false),
  announce(text) { announcer.textContent = ''; setTimeout(() => { announcer.textContent = text; }, 30); },
  newChat, newChannel: () => newChannel(), switcher, shortcuts,
  lock: (opts) => lockNow(opts),
  async signOut() {
    forgetPrefs();
    forgetDrafts(browserStorage);
    live.stop();
    // This browser leaves its sealed-chat device list and deletes its keys
    // before the session ends (docs/SEALED.md section 14).
    await signOutSealed();
    try { await api('POST', '/logout', {}); } catch { /* the session may already be gone */ }
    location.assign('/login');
  },
});

function keys(e) {
  const mod = e.metaKey || e.ctrlKey;
  const k = e.key.toLowerCase();
  const hit = (fn) => { e.preventDefault(); fn(); };
  if (mod && !e.shiftKey && k === 'k') return hit(() => switcher());
  // Mod+Shift+L locks Churr here (the lock).
  if (isLockKey(e)) return hit(() => lockNow({ ask: true }));
  if (mod && k === '/') return hit(() => shortcuts());
  if (mod && e.shiftKey) {
    if (k === 'f') return hit(() => go('/chat/search'));
    if (k === 't') return hit(() => go('/chat/threads'));
    if (k === 's') return hit(() => go('/chat/bookmarks'));
    if (k === 'i') return hit(() => go('/chat/notifications'));
    if (k === 'd') return hit(() => openDnd());
    if (k === 'a') return hit(() => { for (const c of store.conversations.values()) if (c.membership && c.membership.unread_count) markRead(c.id); });
  }
  if (e.altKey && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) {
    return hit(() => {
      let rows = rowOrder();
      if (e.shiftKey) rows = rows.filter((c) => c.membership.unread_count > 0 || c.id === store.current);
      if (!rows.length) return;
      const i = rows.findIndex((c) => c.id === store.current);
      const next = rows[(i + (e.key === 'ArrowDown' ? 1 : -1) + rows.length) % rows.length];
      if (next) ui.openConversation(next.id);
    });
  }
  // Escape closes the thread pane, then search; then marks the
  // conversation read. An open dialog or menu takes the key first.
  if (e.key === 'Escape' && !document.querySelector('dialog[open], .menu, .suggest, .picker')) {
    const r = parse(location.pathname);
    if (r.kind === 'conversation' && r.threadId) return hit(() => ui.closeThread());
    if (r.kind === 'view') return hit(() => history.back());
    if (r.kind === 'conversation') return hit(() => markRead(r.id));
  }
  return undefined;
}

function banner() {
  let el = null;
  store.on('connection', (s) => {
    // The session was locked from another tab of this browser.
    if (s === 'locked') { goToLock(); return; }
    if (s === 'signed-out') {
      if (!el) { el = h('div', { class: 'conn-banner', role: 'alert' }); document.body.append(el); }
      fill(el, icon('warn'), ' You have been signed out. ', button('Sign in again', () => goToSignIn(), 'small primary'));
    } else if (el && s === 'live') { el.remove(); el = null; }
  });
}

// broadcastBanner draws the server broadcast (ruling R74): a floating
// notice over the page until it runs out or the person dismisses it, said
// once to a screen reader. Dismissed ones stay dismissed in this tab.
function broadcastBanner() {
  let el = null, timer = null;
  const dismissed = new Set(), said = new Set();
  const draw = () => {
    clearTimeout(timer);
    const b = store.broadcast;
    const left = b ? Date.parse(b.until) - Date.now() : 0;
    if (!b || dismissed.has(b.id) || !(left > 0)) { if (el) { el.remove(); el = null; } return; }
    if (!el) { el = h('div', { class: 'broadcast', role: 'region', 'aria-label': CMD.broadcastLabel }); document.body.append(el); }
    fill(el,
      h('div', { class: 'broadcast-text' },
        h('span', { class: 'tag' }, CMD.broadcastLabel), ' ',
        b.by_id ? h('span', { class: 'dim' }, CMD.broadcastFrom(nameOf(b.by_id))) : null,
        h('p', {}, ...renderInline(b.text, []))),
      iconButton('close', CMD.broadcastDismiss, () => { dismissed.add(b.id); draw(); }));
    if (!said.has(b.id)) { said.add(b.id); ui.announce(CMD.broadcastAnnounce(b.text)); }
    timer = setTimeout(draw, Math.min(left, 2 ** 31 - 1));
  };
  store.on('broadcast', draw);
  draw();
}

async function start() {
  installFreshCheck();
  onSignedOut(() => goToSignIn());
  try {
    [store.me, store.server] = await Promise.all([get('/me').then((r) => r.account), get('/server').then((r) => r.server)]);
  } catch (err) {
    if (err.status === 401) return; // on the way to the sign-in page
    fill(main, h('div', { class: 'view empty-view' }, h('h1', {}, 'The chat could not load'), h('p', { class: 'problem' }, err.message), h('p', {}, button('Try again', () => location.reload(), 'primary'))));
    return;
  }
  store.accounts.set(store.me.id, store.me);
  rememberServerDefaults(store.server.default_themes);
  loadPrefs();
  try { await loadEverything(); } catch (err) { toast(err.message, true); }
  get('/bookmarks', { limit: 200 }).then((r) => { for (const b of r.bookmarks) store.bookmarks.add(b.message_id); }).catch(() => {});

  fill(side, new Sidebar().el);
  banner();
  broadcastBanner();
  // The answer to a request to join a channel (ruling R74), told once.
  store.on('knock', (e) => {
    if (!e.knock) return;
    // To the channel's moderators and managers: someone asks, or was answered.
    if (e.type === 'knock.created') { notifyKnock(e); return; }
    if (e.knock.account_id !== store.me.id) { setAsking(e.knock.conversation_id, Math.max(0, (store.knocks.get(e.knock.conversation_id) || 0) - 1)); return; }
    const where = '#' + (e.conversation_name || '');
    toast(e.knock.outcome === 'accepted' ? CMD.knockAccepted(where) : CMD.knockDeclined(where), e.knock.outcome !== 'accepted');
  });
  watchInteractions((e) => {
    const cid = e.conversation_id;
    if (!cid) return;
    privateResponses.set(cid, [...(privateResponses.get(cid) || []), { id: e.interaction_id + Date.now(), botId: e.bot_id, conversationId: cid, response: e.response }]);
    store.emit('messages', cid);
  });
  startNotifications();
  store.on('request.notify', notifyRequest);
  watchApprovals();
  store.on('left', (cid) => { if (store.current === cid) { toast('You are no longer in ' + (title(conversation(cid)) || 'that conversation') + '.'); go('/chat/', true); } });
  // A channel ban (ruling R72): leave the channel quietly, then say why once.
  store.on('banned', (cid) => { if (store.current === cid) go('/chat/', true); });
  store.on('channelBan', (e) => {
    const where = e.conversation_name ? '#' + e.conversation_name : 'a chat';
    const until = e.ban.expires_at ? ` until ${dateTime(e.ban.expires_at)}` : '';
    toast(`You were banned from ${where}${until}${e.ban.reason ? ': “' + e.ban.reason + '”' : '.'}`, true);
  });
  store.on('session', (e) => { if (e.session && e.session.recovery) toast('A recovery session has just started on your account. If that was not you, tell an admin.', true); });
  window.addEventListener('popstate', () => route(false));
  window.addEventListener('keydown', keys);
  keepComposerFocus();
  startAutoLock();

  // The first sign-in: the server's welcome text, shown once per browser.
  await route(false);
  live.start();
}

start();
