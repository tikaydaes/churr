// Chat requests, invitations, channel offers and closed chats (rulings
// R77, R78, R79, R103; docs/FEATURES.md section 20, docs/CLIENTS.md
// section 9.28).
//
// A new direct or sealed chat with someone who does not approve chats
// automatically is a request: the person asking sees the chat waiting and
// cannot write in it; the person asked sees who is asking under Requests,
// and accepts, declines, dismisses or blocks. Either person can close a
// direct or sealed chat; both keep it to read, and "Ask to chat again"
// starts a new one.

import { get, post } from '/shared/api.js';
import { h, icon, iconButton, button, fill, toast, when, avatar, busy, confirmDialog, plural } from '/shared/dom.js';
import { channelIcon } from '/shared/pick.js';
import { store, nameOf, isSealed, isDirectChat, markRead, needAccounts, account, isPublic, offerOf, invitationOf } from './store.js';
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
//
// Nobody ends up in a conversation they did not accept (ruling R103):
// being included in a group chat and being invited to a named channel are
// requests too, answered the same four ways. A public channel can be read
// before answering; a private or knock channel, and a group chat, cannot.

// who is the name of whoever is asking.
const who = (r) => { const a = { ...r.account, ...account(r.account.id) }; return a.display_name || a.username; };

// requestText says what a request asks, after the name of who asks.
export function requestText(r) {
  if (r.kind === 'group') {
    const people = r.people || [];
    const shown = people.slice(0, 6).join(', ') + (people.length > 6 ? ` and ${people.length - 6} more` : '');
    return people.length ? `added you to a group chat with ${shown}` : 'added you to a group chat';
  }
  if (r.kind === 'channel') return `invited you to #${(r.channel && r.channel.name) || 'a channel'}`;
  return `asks for ${r.kind === 'sealed' ? 'a sealed chat' : 'a direct chat'}`;
}

// preview opens a public channel to read before answering, as Browse's
// Read does; the page looks the channel up as it opens it. Nothing of a
// private or knock channel shows before joining.
export function preview(channel) {
  if (!channel || !isPublic(channel)) return;
  ui.openConversation(channel.id);
}

export async function answerRequest(r, how) {
  try {
    const out = await post(`/me/chat-requests/${r.id}/${how}`);
    store.requests = store.requests.filter((x) => x.id !== r.id);
    store.emit('requests');
    if (how === 'accept' && out && out.conversation) { keep(out.conversation); store.others.delete(out.conversation.id); ui.openConversation(out.conversation.id); }
    if (how === 'block') { store.blocked.add(r.account.id); store.emit('me'); }
    return true;
  } catch (err) { toast(err.message, true); return false; }
}

// answerOffer accepts a channel offered to the reader, which opens it, or
// declines it, which takes it off the sidebar. A declined one can still be
// joined from Browse while the access lasts.
export async function answerOffer(o, accept) {
  const id = o.conversation_id;
  try {
    const out = await post(`/me/channel-offers/${id}/${accept ? 'accept' : 'decline'}`);
    store.offers = store.offers.filter((x) => x.conversation_id !== id);
    store.emit('offers');
    if (accept && out && out.conversation) {
      keep(out.conversation);
      store.others.delete(id);
      ui.openConversation(id);
    } else if (!accept) {
      toast(`Declined. You can still join #${(o.channel && o.channel.name) || 'it'} from Browse channels.`);
    }
    return true;
  } catch (err) { toast(err.message, true); return false; }
}

// RequestsView is /chat/requests: who is asking to chat, who included the
// reader in a group chat or invited them to a channel, and the four
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
      const act = (label, how, cls) => button(label, (e) => busy(e.currentTarget, () => answerRequest(r, how)), cls);
      const ch = r.kind === 'channel' ? r.channel : null;
      const invite = r.kind === 'group' || r.kind === 'channel';
      return h('article', { class: 'result request', dataset: { request: r.id, kind: r.kind } },
        h('div', { class: 'row' }, avatar(a),
          h('div', { class: 'grow' },
            h('strong', {}, h('bdi', {}, a.display_name || a.username)), ' ', h('span', { class: 'dim' }, '@' + a.username),
            invite ? h('p', { class: 'small' }, h('bdi', {}, who(r)), ' ', requestText(r)) : null,
            ch && ch.topic ? h('p', { class: 'small' }, h('bdi', {}, ch.topic)) : null,
            ch ? h('p', { class: 'dim small' }, [ch.access === 'knock' ? 'A knock channel' : ch.private ? 'A private channel' : 'A public channel',
              plural(ch.member_count || 0, 'member'), r.level && r.level !== 'member' ? 'you would join as ' + r.level : ''].filter(Boolean).join(' · ')) : null,
            r.override ? h('p', { class: 'small' }, h('span', { class: 'tag pink' }, 'Override'), ' Sent from the admin console by an Owner or an Admin. When you join, every member is shown that you were added by override.') : null,
            h('p', { class: 'dim small' }, invite ? when(r.created_at) : `Asks for ${r.kind === 'sealed' ? 'a sealed chat' : 'a direct chat'} · ${when(r.created_at)}`))),
        h('p', { class: 'row' },
          act('Accept', 'accept', 'small primary'),
          act('Decline', 'decline', 'small'),
          act('Dismiss', 'dismiss', 'small'),
          act('Block', 'block', 'small danger'),
          ch && isPublic(ch) ? button('Preview', () => preview(ch), 'small') : null),
        h('p', { class: 'dim small' }, invite
          ? 'You are not in it until you accept. Decline tells them; dismiss and block do not.'
          : 'Decline tells them; dismiss and block do not.'));
    }) : h('p', { class: 'dim' }, 'No requests. When someone asks to start a chat with you, includes you in a group chat or invites you to a channel, it waits here for your answer.'));
  }
}

// offersList is the sidebar's list of channels offered to the reader, each
// with Accept and Decline; a public one opens to read when its name is
// pressed. Nothing when none is offered.
export function offersList() {
  if (!store.offers.length) return null;
  return h('section', { class: 'side-offers', 'aria-label': 'Channels offered to you' },
    h('h2', {}, h('span', { class: 'grow' }, 'Offered to you'), h('em', { 'aria-label': plural(store.offers.length, 'channel') }, String(store.offers.length))),
    h('ul', {}, store.offers.map((o) => {
      const ch = o.channel || { id: o.conversation_id, name: 'a channel' };
      const name = h('span', { class: 'row-name' }, h('bdi', {}, ch.name));
      const why = o.source === 'default' ? 'A default channel of this instance' : 'A group you are in was added to it';
      return h('li', { class: 'side-offer', dataset: { offer: o.conversation_id } },
        isPublic(ch)
          ? h('a', { class: 'offer-link', href: '/chat/c/' + ch.id, title: why + '. Open it to read before you answer.',
            onclick: (e) => { if (e.metaKey || e.ctrlKey || e.shiftKey) return; e.preventDefault(); preview(ch); } }, h('span', { class: 'g' }, channelIcon(ch, 'glyph')), name)
          : h('span', { class: 'offer-link', title: why }, h('span', { class: 'g' }, channelIcon(ch, 'glyph')), name),
        h('span', { class: 'offer-does' },
          button('Accept', (e) => busy(e.currentTarget, () => answerOffer(o, true)), 'small primary'),
          button('Decline', (e) => busy(e.currentTarget, () => answerOffer(o, false)), 'small')));
    })));
}

// invitationBar stands in for the composer of a channel the reader is
// reading before answering an invitation or an offer: who invited them,
// and Accept and Decline. Null when nothing waits for this channel.
export function invitationBar(c) {
  const r = invitationOf(c.id);
  const o = !r && offerOf(c.id);
  if (!r && !o) return null;
  const said = r ? `${who(r)} invited you to this channel.${r.override ? ' An override: when you join, every member is shown that you were added by override.' : ''}`
    : o.source === 'default' ? 'This channel is offered to you as a default channel of this instance.' : 'This channel is offered to you: a group you are in was added to it.';
  const yes = button('Accept', () => busy(yes, () => (r ? answerRequest(r, 'accept') : answerOffer(o, true))), 'primary');
  const no = button('Decline', () => busy(no, async () => { if (await (r ? answerRequest(r, 'decline') : answerOffer(o, false))) ui.go('/chat/', true); }));
  return h('div', { class: 'join-bar invitation' }, h('span', { class: 'grow' }, said), yes, no);
}

// notifyRequest shows a browser notification for a new request: who is
// asking and for what, nothing more.
export function notifyRequest(r) {
  if (!('Notification' in window) || Notification.permission !== 'granted' || store.prefs.notify === false) return;
  const name = r.account.display_name || r.account.username;
  const text = r.kind === 'group' || r.kind === 'channel' ? `${name} ${requestText(r)}` : `${name} wants to chat`;
  try {
    const n = new Notification(text, { tag: 'request:' + r.id, icon: '/icons/icon-192.png' });
    n.onclick = () => { window.focus(); ui.go('/chat/requests'); n.close(); };
  } catch { /* no notifications outside a service worker on some browsers */ }
}
