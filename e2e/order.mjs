// Checks, without a browser, where the web chat shows messages
// (docs/EVENTS.md section 8, "Where a message is shown"): sorting by
// `order` as plain text, paging by `seq`, where the "New" line goes when
// some unread messages arrived late, and what the store does with
// `message.created` for a late message, `message.sent` and
// `message.placed`. Shared conversations cannot easily be made in the
// browser run, so these are fed to the store directly.

import { register } from 'node:module';

// The chat's modules import each other by the paths the server serves
// them at ('/shared/api.js'); these are mapped onto the source tree.
const STATIC = new URL('../internal/web/static/', import.meta.url).href;
register('data:text/javascript,' + encodeURIComponent(`
  export async function resolve(spec, ctx, next) {
    if (spec.startsWith('/shared/') || spec.startsWith('/chat/')) return next(${JSON.stringify(STATIC)} + spec.slice(1), ctx);
    return next(spec, ctx);
  }`));

// store.js starts a live connection object at load; it only listens.
globalThis.window = { addEventListener() {} };
globalThis.document = { addEventListener() {}, visibilityState: 'hidden' };

const { compareOrder, sortByOrder, seqRange, layout } = await import('../internal/web/static/chat/order.js');
const { store, room, applyToRoom, apply } = await import('../internal/web/static/chat/store.js');

let failures = 0, checks = 0;
function check(name, ok, detail) {
  checks++;
  if (!ok) { failures++; console.log(`FAIL order: ${name}${detail ? '\n       ' + detail : ''}`); }
}
const ids = (list) => list.map((m) => m.id).join(' ');
const at = (n) => new Date(Date.UTC(2026, 10, 14, 9, n)).toISOString();
const msg = (id, seq, order, more = {}) => ({ id, conversation_id: 'c1', seq, order, author_id: 'sam', created_at: at(seq), text: id, ...more });

// --- sorting ---

{
  // Plain text, character by character: not by number, not by locale.
  const list = [msg('b', 2, 'B'), msg('a', 1, 'a'), msg('c', 3, '10'), msg('d', 4, '9'), msg('e', 5, 'Z')];
  check('order is compared as plain text', ids(sortByOrder(list)) === 'c d b e a', ids(list));

  // In a shared conversation a late message has a high seq and an early order.
  const shared = [msg('m1', 1, '0001'), msg('m2', 2, '0003'), msg('m3', 3, '0004'), msg('late', 4, '0002', { late: true })];
  check('a late message is shown in its place by order, not at the end', ids(sortByOrder(shared)) === 'm1 late m2 m3', ids(shared));
  const range = seqRange(shared);
  check('paging takes the lowest and highest seq, wherever they are shown', range.min === 1 && range.max === 4, JSON.stringify(range));

  const posting = { id: 'pending-x', _pending: true, created_at: at(0), author_id: 'kaity' };
  const withLocal = sortByOrder([posting, ...shared]);
  check('a copy still being posted goes after every message', withLocal[withLocal.length - 1] === posting, ids(withLocal));
  check('a copy still being posted does not count for paging', seqRange([posting]) === null && seqRange([posting, ...shared]).min === 1);
  check('equal orders compare equal', compareOrder(msg('x', 1, 'k'), msg('y', 2, 'k')) === 0);
}

// --- the "New" line ---

const base = { flat: false, threadStates: new Map(), meId: 'kaity', member: true };
const newAt = (items) => { const it = items.find((x) => x.newBefore); return it ? (it.m || it.marker).id : null; };
const lateOnes = (items) => items.filter((x) => x.late).map((x) => x.m.id).join(' ');

{
  // Read up to seq 3. A late message (seq 5) belongs between m1 and m2;
  // m4 is new in the ordinary way.
  const list = sortByOrder([msg('m1', 1, '0001'), msg('m2', 2, '0003'), msg('m3', 3, '0004'), msg('m4', 4, '0005'), msg('late', 5, '0002', { late: true })]);
  const items = layout({ ...base, list, readAt: 3, lastSeq: 5 });
  check('drawn in order', items.map((x) => x.m.id).join(' ') === 'm1 late m2 m3 m4', items.map((x) => x.m.id).join(' '));
  check('"New" goes before the first unread message that is not late', newAt(items) === 'm4', String(newAt(items)));
  check('a late unread message is tagged by itself', lateOnes(items) === 'late', lateOnes(items));

  // Only late messages are unread: no "New" line at all, each one tagged.
  const onlyLate = sortByOrder([msg('m1', 1, '0001'), msg('m2', 2, '0004'), msg('l1', 3, '0002', { late: true }), msg('l2', 4, '0003', { late: true })]);
  const items2 = layout({ ...base, list: onlyLate, readAt: 2, lastSeq: 4 });
  check('with only late messages unread there is no "New" line', newAt(items2) === null, String(newAt(items2)));
  check('every late unread message is tagged', lateOnes(items2) === 'l1 l2', lateOnes(items2));

  // A late message that was already read carries no tag.
  const items3 = layout({ ...base, list: onlyLate, readAt: 4, lastSeq: 4 });
  check('a late message already read is not tagged', lateOnes(items3) === '' && newAt(items3) === null, lateOnes(items3));

  // A late message below the line is still tagged, and the line stays put.
  const below = sortByOrder([msg('m1', 1, '0001'), msg('m2', 2, '0002'), msg('m3', 3, '0005'), msg('l', 4, '0006', { late: true })]);
  const items4 = layout({ ...base, list: below, readAt: 2, lastSeq: 4 });
  check('a late message below the "New" line is tagged and does not move it', newAt(items4) === 'm3' && lateOnes(items4) === 'l', `${newAt(items4)} / ${lateOnes(items4)}`);

  // The reader's own messages are not unread.
  const own = sortByOrder([msg('m1', 1, '0001'), msg('mine', 2, '0002', { author_id: 'kaity' }), msg('m3', 3, '0003')]);
  check('the reader\'s own message does not take the "New" line', newAt(layout({ ...base, list: own, readAt: 1, lastSeq: 3 })) === 'm3');

  // Without unread messages nothing is marked; a non-member has no line.
  check('nothing unread: no line', newAt(layout({ ...base, list: own, readAt: 3, lastSeq: 3 })) === null);
  check('not a member: no line', newAt(layout({ ...base, member: false, list: list, readAt: 0, lastSeq: 5 })) === null);
}

{
  // Thread markers go at the place of the last reply, by its order.
  const parent = msg('p', 1, '0001', { thread: { reply_count: 1, last_reply_seq: 4, last_reply_at: at(4), participant_ids: ['sam'] } });
  const list = sortByOrder([parent, msg('m2', 2, '0003'), msg('m3', 3, '0005'), msg('r', 4, '0004', { parent_id: 'p' })]);
  const items = layout({ ...base, list, readAt: 3, lastSeq: 4 });
  const drawn = items.map((x) => (x.marker ? 'marker:' : '') + (x.m || x.marker).id).join(' ');
  check('a thread marker sits at its last reply\'s order', drawn === 'p m2 marker:p m3', drawn);
  check('an unread thread marker takes the "New" line', newAt(items) === 'p' && items.find((x) => x.newBefore).marker);

  // The reply is late: the marker is not where "New" goes.
  const lateReply = sortByOrder([parent, msg('m2', 2, '0003'), msg('m3', 3, '0005'), msg('r', 4, '0004', { parent_id: 'p', late: true })]);
  check('a marker for a late reply does not take the "New" line', newAt(layout({ ...base, list: lateReply, readAt: 3, lastSeq: 4 })) === null);

  // The reply is not loaded and is newer than what is: the marker goes last.
  const unloaded = sortByOrder([parent, msg('m2', 2, '0003'), msg('m3', 3, '0005')]);
  const drawn2 = layout({ ...base, list: unloaded, readAt: 1, lastSeq: 4 }).map((x) => (x.marker ? 'marker:' : '') + (x.m || x.marker).id).join(' ');
  check('a marker whose reply is not loaded goes last', drawn2 === 'p m2 m3 marker:p', drawn2);

  // In the flat view replies are messages, and there are no markers.
  const flat = layout({ ...base, flat: true, list, readAt: 3, lastSeq: 4 }).map((x) => (x.marker ? 'marker:' : '') + (x.m || x.marker).id).join(' ');
  check('the flat view shows replies by order, without markers', flat === 'p m2 r m3', flat);
}

// --- the store, fed events ---

{
  store.me = { id: 'kaity', prefs: {} };
  store.conversations.set('c1', { id: 'c1', kind: 'channel', name: 'shared', last_seq: 3, membership: { read_seq: 3, unread_count: 0 } });
  const r = room('c1');
  for (const m of [msg('m1', 1, '0001'), msg('m2', 2, '0003'), msg('mine', 3, '0005', { author_id: 'kaity', unsent: true })]) { r.byId.set(m.id, m); r.list.push(m); }
  r.synced = 3; r.loaded = true; r.hasOlder = false;
  let emitted = 0;
  store.on('messages', (cid) => { if (cid === 'c1') emitted++; });

  apply({ type: 'message.created', conversation_id: 'c1', seq: 4, message: msg('late', 4, '0002', { late: true }), notify: 'badge' });
  check('message.created for a late message inserts it at its place', ids(r.list) === 'm1 late m2 mine', ids(r.list));
  check('a late message counts as unread', store.conversations.get('c1').membership.unread_count === 1);

  const v = r.byId.get('mine')._v || 0;
  apply({ type: 'message.sent', at: at(5), conversation_id: 'c1', message_id: 'mine' });
  check('message.sent clears unsent and redraws the message', r.byId.get('mine').unsent === undefined && r.byId.get('mine')._v === v + 1 && emitted >= 2);

  r.byId.get('m2').unsent = true;
  apply({ type: 'message.placed', at: at(6), conversation_id: 'c1', seq: 5, message: msg('m2', 2, '0006', { unsent: true }) });
  check('message.placed replaces the message and sorts again', ids(r.list) === 'm1 late mine m2' && r.byId.get('m2').order === '0006', ids(r.list));
  check('message.placed keeps the one copy of the message', r.list.length === 4 && r.list.filter((m) => m.id === 'm2').length === 1);

  apply({ type: 'message.placed', at: at(7), conversation_id: 'c1', seq: 6, message: msg('gone', 9, '0000') });
  check('message.placed for a message not held adds nothing', !r.byId.has('gone') && r.list.length === 4);

  // A pane's own copy (the thread pane) takes the same events without a number.
  const pane = { list: [], byId: new Map(), synced: 0, pending: [] };
  const reply = msg('r1', 7, '0007', { parent_id: 'p', unsent: true });
  pane.list.push(reply); pane.byId.set('r1', reply);
  applyToRoom(pane, { type: 'message.sent', conversation_id: 'c1', message_id: 'r1' });
  check('message.sent applies to a thread pane\'s copy', !('unsent' in reply));

  // The events that were removed do nothing any more.
  applyToRoom(r, { type: 'message.queued', conversation_id: 'c1', seq: 8, message: msg('q', 8, '0008') });
  check('message.queued is no longer understood', !r.byId.has('q'));
}

console.log(`${failures ? 'FAIL' : 'ok  '} order: ${checks} checks, ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
