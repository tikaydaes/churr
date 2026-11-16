// Chat requests and closed chats (rulings R77, R78, R79; docs/FEATURES.md
// section 20, docs/CLIENTS.md section 9.28).
//
// A new direct or sealed chat with someone who does not approve chats
// automatically is a request: the person asking sees the chat waiting and
// cannot write in it; the person asked sees who is asking under Requests,
// and accepts, declines, dismisses or blocks. Either person can close a
// direct or sealed chat; both keep it to read, and "Ask to chat again"
// starts a new one.

import { get, post } from '/shared/api.js';
import { h, icon, iconButton, button, fill, toast, when, avatar, busy, confirmDialog } from '/shared/dom.js';
import { store, nameOf, isSealed, isDirectChat, markRead, needAccounts, account } from './store.js';
import { ui } from './ui.js';

export { isDirectChat };

function otherIn(c) {
  return (c.member_ids || []).find((x) => x !== store.me.id);
}

function keep(c) {
  store.conversations.set(c.id, { ...store.conversations.get(c.id), ...c });
  needAccounts(c.member_ids);
  store.emit('conversations');
}

// closeChat closes a direct or sealed chat, after asking. withRead marks it
// read first, as /close does.
export async function closeChat(c, withRead) {
  if (!isDirectChat(c)) return;
  if (c.chat_state === 'waiting') return withdrawRequest(c);
  const ok = await confirmDialog('Close this chat?', [
    `You and ${nameOf(otherIn(c))} both keep what was written here to read. Neither of you can write in it again.`,
    'Either of you can ask to chat again later.'], { yes: 'Close chat', danger: true });
  if (!ok) return;
  try {
    if (withRead) await markRead(c.id);
    keep((await post(`/conversations/${c.id}/close`)).conversation);
  } catch (err) { toast(err.message, true); }
}

// withdrawRequest takes back a request that waits.
export async function withdrawRequest(c) {
  const ok = await confirmDialog('Withdraw this request?', `You cannot ask ${nameOf(otherIn(c))} again for a day.`, { yes: 'Withdraw', danger: true });
  if (!ok) return;
  try {
    await post(`/conversations/${c.id}/withdraw`);
    store.conversations.delete(c.id);
    store.emit('conversations');
    if (store.current === c.id) ui.go('/chat/', true);
  } catch (err) { toast(err.message, true); }
}

// askAgain starts a new chat with the other person of a closed or declined
// one: a new request, unless they approve chats automatically.
export async function askAgain(c) {
  const other = otherIn(c);
  if (!other) return;
  if (isSealed(c)) {
    // A fresh sealed chat: a new identifier and epoch record 1, made here
    // (docs/SEALED.md section 6). Loaded when needed: the sealed code
    // draws chats that use this file.
    const { startSealedChat } = await import('./sealed/index.js');
    await startSealedChat(other);
    return;
  }
  try {
    const out = await post('/chats', { account_ids: [other] });
    keep(out.conversation);
    ui.openConversation(out.conversation.id);
  } catch (err) { toast(err.message, true); }
}

// chatStateBar is what stands in for the composer of a chat that waits,
// was declined or was closed, or null for an open one.
export function chatStateBar(c) {
  if (!c || !c.chat_state) return null;
  const who = nameOf(otherIn(c));
  let text, action = null;
  switch (c.chat_state) {
    case 'waiting':
      text = `Waiting for ${who} to accept your request. You can write once they do.`;
      action = button('Withdraw request', () => withdrawRequest(c), 'small');
      break;
    case 'declined':
      text = `${who} declined this chat.`;
      break;
    default:
      text = 'This chat is closed. You can both still read it.';
      action = button('Ask to chat again', (e) => busy(e.currentTarget, () => askAgain(c)), 'small primary');
  }
  return h('div', { class: 'chat-state ' + c.chat_state, role: 'status' },
    icon(c.chat_state === 'waiting' ? 'clock' : c.chat_state === 'closed' ? 'lock' : 'info', null, 'small'),
    h('span', { class: 'grow' }, text), action);
}

// The tag a sidebar row carries for a chat that is not open.
export function chatStateTag(c) {
  if (!c || !c.chat_state) return null;
  return h('span', { class: 'tag line', role: 'img', 'aria-label': c.chat_state }, c.chat_state);
}

// --- the person asked ---

async function answer(r, how) {
  try {
    const out = await post(`/me/chat-requests/${r.id}/${how}`);
    store.requests = store.requests.filter((x) => x.id !== r.id);
    store.emit('requests');
    if (how === 'accept' && out && out.conversation) { keep(out.conversation); ui.openConversation(out.conversation.id); }
    if (how === 'block') { store.blocked.add(r.account.id); store.emit('me'); }
  } catch (err) { toast(err.message, true); }
}

// RequestsView is /chat/requests: who is asking to chat, and the four
// answers.
export class RequestsView {
  constructor() {
    this.list = h('div', { class: 'results requests' });
    this.el = h('section', { class: 'view' },
      h('header', { class: 'conv-head' }, iconButton('back', 'Back to the list', () => ui.go('/chat/'), 'only-compact'),
        h('div', { class: 'conv-title' }, h('h1', {}, icon('person'), ' Requests')), h('div', { class: 'conv-tools' })),
      h('div', { class: 'view-body' }, this.list));
    this.off = [store.on('requests', () => this.draw()), store.on('accounts', () => this.draw())];
    this.draw();
    get('/me/chat-requests').then((r) => { store.requests = r.requests; store.emit('requests'); }).catch(() => {});
  }

  destroy() { for (const off of this.off) off(); }

  draw() {
    fill(this.list, store.requests.length ? store.requests.map((r) => {
      const a = { ...r.account, ...account(r.account.id) };
      const kind = r.kind === 'sealed' ? 'a sealed chat' : 'a direct chat';
      const act = (label, how, cls) => button(label, (e) => busy(e.currentTarget, () => answer(r, how)), cls);
      return h('article', { class: 'result request', dataset: { request: r.id } },
        h('div', { class: 'row' }, avatar(a),
          h('div', { class: 'grow' },
            h('strong', {}, h('bdi', {}, a.display_name || a.username)), ' ', h('span', { class: 'dim' }, '@' + a.username),
            h('p', { class: 'dim small' }, `Asks for ${kind} · ${when(r.created_at)}`))),
        h('p', { class: 'row' },
          act('Accept', 'accept', 'small primary'),
          act('Decline', 'decline', 'small'),
          act('Dismiss', 'dismiss', 'small'),
          act('Block', 'block', 'small danger')),
        h('p', { class: 'dim small' }, 'Decline tells them; dismiss and block do not.'));
    }) : h('p', { class: 'dim' }, 'No requests. When someone asks to start a chat with you, it waits here for your answer.'));
  }
}

// notifyRequest shows a browser notification for a new request: who is
// asking, nothing more.
export function notifyRequest(r) {
  if (!('Notification' in window) || Notification.permission !== 'granted' || store.prefs.notify === false) return;
  try {
    const n = new Notification(`${r.account.display_name || r.account.username} wants to chat`, { tag: 'request:' + r.id, icon: '/icons/icon-192.png' });
    n.onclick = () => { window.focus(); ui.go('/chat/requests'); n.close(); };
  } catch { /* no notifications outside a service worker on some browsers */ }
}
