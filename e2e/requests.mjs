// Checks, without a browser, what the web chat's store does with chat
// requests and chats that are not open (rulings R77, R78, R79; docs/EVENTS.md
// section 9): chat_request.received and .removed keep the list of requests,
// a notification is asked for only when the event says so, a chat that
// waits, was declined or was closed takes no posts, and which chats can be
// closed.

import { register } from 'node:module';

const STATIC = new URL('../internal/web/static/', import.meta.url).href;
register('data:text/javascript,' + encodeURIComponent(`
  export async function resolve(spec, ctx, next) {
    if (spec.startsWith('/shared/') || spec.startsWith('/chat/')) return next(${JSON.stringify(STATIC)} + spec.slice(1), ctx);
    return next(spec, ctx);
  }`));

globalThis.window = { addEventListener() {} };
globalThis.document = { addEventListener() {}, visibilityState: 'hidden' };

const { store, apply, canPost, whyNoPost, isDirectChat, title, mayJoin, offerOf, invitationOf, isPublic } = await import('../internal/web/static/chat/store.js');

let failures = 0, checks = 0;
function check(name, ok, detail) {
  checks++;
  if (!ok) { failures++; console.log(`FAIL requests: ${name}${detail ? '\n       ' + detail : ''}`); }
}

store.me = { id: 'me', role: 'member' };
const at = '2026-11-16T09:00:00.000Z';
const req = (id, who) => ({ id, kind: 'direct', account: { id: who, username: who, display_name: who.toUpperCase() }, created_at: at });
let notified = [], redrawn = 0;
store.on('request.notify', (r) => notified.push(r.id));
store.on('requests', () => { redrawn++; });

apply({ type: 'chat_request.received', at, request: req('r1', 'ana'), notify: true });
check('a request is listed', store.requests.length === 1 && store.requests[0].id === 'r1');
check('the person asking is among the accounts', store.accounts.get('ana') && store.accounts.get('ana').display_name === 'ANA');
check('it notifies when the event says so', notified.join() === 'r1');
check('the list is redrawn', redrawn === 1, `redrawn ${redrawn}`);

apply({ type: 'chat_request.received', at, request: req('r2', 'ben'), notify: false });
check('one that came back after an unblock does not notify', notified.join() === 'r1' && store.requests.length === 2);
apply({ type: 'chat_request.received', at, request: req('r2', 'ben'), notify: false });
check('the same request twice is listed once', store.requests.length === 2);

apply({ type: 'chat_request.removed', at, id: 'r1' });
check('a request leaves the list', store.requests.length === 1 && store.requests[0].id === 'r2');
apply({ type: 'chat_request.removed', at, id: 'nope' });
check('removing one not held is harmless', store.requests.length === 1);

// Chats that are not open take no posts.
const conv = (extra) => ({ id: 'c', kind: 'channel', private: true, member_ids: ['me', 'ana'], membership: { level: 'member' }, ...extra });
check('an open chat takes posts', canPost(conv({})));
check('a waiting chat takes none', !canPost(conv({ chat_state: 'waiting' })));
check('a declined chat takes none', !canPost(conv({ chat_state: 'declined' })));
check('a closed chat takes none', !canPost(conv({ chat_state: 'closed' })));
check('a closed chat says so', whyNoPost(conv({ chat_state: 'closed' })) === 'This chat is closed.');

// Which chats close.
check('a direct chat closes', isDirectChat(conv({})));
check('a sealed chat closes', isDirectChat({ kind: 'sealed', member_ids: ['me', 'ana'] }));
check('a group does not', !isDirectChat(conv({ member_ids: ['me', 'ana', 'ben'] })));
check('a named channel does not', !isDirectChat(conv({ name: 'den' })));
check('one’s own channel does not', !isDirectChat(conv({ member_ids: ['me'] })));

// conversation.updated brings the new state.
store.conversations.set('c', conv({ chat_state: 'waiting' }));
apply({ type: 'conversation.updated', at, conversation_id: 'c', seq: 3, conversation: { ...conv({}), membership: undefined } });
check('accepted: the state goes', !store.conversations.get('c').chat_state && canPost(store.conversations.get('c')));
apply({ type: 'conversation.updated', at, conversation_id: 'c', seq: 4, conversation: { ...conv({ chat_state: 'closed' }), membership: undefined } });
check('closed: the state arrives', store.conversations.get('c').chat_state === 'closed' && !canPost(store.conversations.get('c')));

// First contact (ruling R103): invitations, offers, the invited.
const offer = (id, extra) => ({ conversation_id: id, source: 'group', level: 'member', created_at: at, channel: { id, kind: 'channel', name: 'den-' + id, private: true, access: 'private', member_count: 3 }, ...extra });
let offered = 0;
store.on('offers', () => { offered++; });
apply({ type: 'channel_offer.added', at, offer: offer('o1') });
check('an offer is kept', store.offers.length === 1 && offerOf('o1') && offered === 1);
apply({ type: 'channel_offer.added', at, offer: offer('o1') });
check('the same offer twice is kept once', store.offers.length === 1);
apply({ type: 'channel_offer.added', at, offer: offer('o2', { source: 'default' }) });
check('a second offer is kept after the first', store.offers.map((o) => o.conversation_id).join() === 'o1,o2');
apply({ type: 'channel_offer.removed', at, conversation_id: 'o1' });
check('an offer goes with channel_offer.removed, named in the envelope', store.offers.map((o) => o.conversation_id).join() === 'o2' && !offerOf('o1'));
check('an offered channel is joined, whatever its access', mayJoin({ id: 'o2', name: 'den', private: true, access: 'knock' }));
check('a private channel Browse did not mark is not', !mayJoin({ id: 'x', name: 'x', private: true, access: 'private' }));
check('a private channel Browse marked is', mayJoin({ id: 'x', name: 'x', private: true, access: 'private', joinable: true }));
check('a knock channel not offered is asked to join', !mayJoin({ id: 'k', name: 'k', private: true, access: 'knock' }));
check('a public channel is joined', mayJoin({ id: 'p', name: 'p', private: false, access: 'public' }));
check('nothing is joined twice', !mayJoin({ id: 'p', name: 'p', access: 'public', membership: { level: 'member' } }));
check('only a public channel is read before joining', isPublic({ access: 'public' }) && !isPublic({ access: 'knock', private: true }) && !isPublic({ access: 'private', private: true }));

const invite = { id: 'r9', kind: 'channel', conversation_id: 'ch', account: { id: 'ana', username: 'ana' }, created_at: at, level: 'member', channel: { id: 'ch', name: 'den', access: 'public' } };
apply({ type: 'chat_request.received', at, request: invite, notify: true });
check('an invitation to a channel is found by the channel', invitationOf('ch') && invitationOf('ch').id === 'r9');
check('a direct request is no invitation', !invitationOf('c'));
apply({ type: 'chat_request.removed', at, id: 'r9' });
check('an accepted invitation goes', !invitationOf('ch'));

// A group chat with someone invited: named by them too, and not a direct chat.
store.accounts.set('ana', { id: 'ana', display_name: 'Ana' });
store.accounts.set('ben', { id: 'ben', display_name: 'Ben' });
const group = { id: 'g', kind: 'channel', private: true, member_ids: ['me', 'ana'], invited_ids: ['ben'], membership: { level: 'member' }, member_count: 2 };
check('a group chat names who was invited', title(group) === 'Ana, Ben', title(group));
check('a chat with two members and one invited is a group chat', !isDirectChat(group));
store.conversations.set('g', group);
apply({ type: 'member.joined', at, conversation_id: 'g', seq: 9, member: { account_id: 'ben', level: 'member' } });
const joined = store.conversations.get('g');
check('someone invited who joins is a member, no longer invited', joined.member_ids.includes('ben') && !joined.invited_ids.includes('ben'));
check('and the title stays the same', title(joined) === 'Ana, Ben', title(joined));

console.log(`${failures ? 'FAIL' : 'ok  '} requests: ${checks} checks, ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
