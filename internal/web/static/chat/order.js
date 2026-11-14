// Where messages are shown (docs/EVENTS.md section 8, "Where a message is
// shown"). A conversation and a thread are shown sorted by `order`,
// compared as plain text; `seq` is only the arrival number, which read
// positions, unread counts and paging go by. This module has no imports,
// so it can be checked under Node (e2e/order.mjs).

// The copy the composer draws for a message it is still posting exists in
// this page only: it has neither `seq` nor `order` yet, and goes after
// every message the server has, in the order it was typed.
const local = (m) => !!m._pending;

export function compareOrder(a, b) {
  const la = local(a), lb = local(b);
  if (la || lb) return la && lb ? (a.created_at < b.created_at ? -1 : a.created_at > b.created_at ? 1 : 0) : la ? 1 : -1;
  return a.order < b.order ? -1 : a.order > b.order ? 1 : 0;
}

export function sortByOrder(list) { return list.sort(compareOrder); }

// seqRange is the lowest and highest `seq` among the server's messages in
// a list, whatever their place on screen: paging goes by these. null when
// there is none.
export function seqRange(list) {
  let min = Infinity, max = -Infinity;
  for (const m of list) {
    if (local(m)) continue;
    if (m.seq < min) min = m.seq;
    if (m.seq > max) max = m.seq;
  }
  return min === Infinity ? null : { min, max };
}

// layout says what the conversation view draws, in order: messages and
// thread markers, which item the "New" line goes before, and which unread
// messages carry the "arrived late" tag.
//
//   list          the room's messages
//   flat          the flat view (replies shown among the messages)
//   threadStates  parent id → ThreadState
//   readAt        the read position when the conversation was opened
//   meId          the reader
//   member        whether the reader is a member (only members have a
//                 read position)
//   lastSeq       the conversation's newest seq
//
// It returns items { m } or { marker, unread }, with `newBefore` on the
// one the "New" line goes before and `late` on each late unread message.
//
// The "New" line goes before the first unread item, by `order`, that did
// not arrive late: a late message belongs further up, among what was
// already read, and drawing the line above it would call everything
// after it new. Each late unread message is marked by itself instead.
export function layout({ list, flat, threadStates, readAt, meId, member, lastSeq }) {
  const items = [];
  for (const m of list) {
    if (m.parent_id) {
      if (!flat) continue;
      // Replies in ignored threads are left out of the flat view.
      const st = threadStates.get(m.parent_id);
      if (st && st.state === 'ignored') continue;
    }
    items.push({ m, rank: local(m) ? 3 : 1 });
  }
  if (!flat) {
    // The marker: one line at the place of a thread's latest reply, while
    // it has replies the reader has not seen. It is drawn from the
    // parent's last-reply number and the reader's position in the thread;
    // nothing is stored for it.
    const range = seqRange(list);
    for (const m of list) {
      const t = m.thread;
      if (m.parent_id || !t || !t.reply_count) continue;
      const st = threadStates.get(m.id);
      if (st && st.state === 'ignored') continue;
      const seen = st ? st.read_seq : Math.max(readAt, m.seq || 0);
      if (!(t.last_reply_seq > seen && t.last_reply_seq > (m.seq || 0))) continue;
      // The thread summary carries no `order`, so the marker takes the
      // place of the last reply if it is loaded. If it is not, the reply
      // is either older than everything loaded (the marker goes first) or
      // newer (it goes last).
      const reply = list.find((x) => x.parent_id === m.id && x.seq === t.last_reply_seq);
      items.push({ marker: m, unread: st ? st.unread_count : 0, reply, rank: reply ? 1 : range && t.last_reply_seq < range.min ? 0 : 2 });
    }
  }
  items.sort((a, b) => a.rank - b.rank || (a.rank === 1 ? compareOrder(a.reply || a.m, b.reply || b.m) : 0) || (a.marker ? 1 : 0) - (b.marker ? 1 : 0));

  const counting = member && readAt < (lastSeq || 0);
  let newDrawn = false;
  for (const it of items) {
    const seq = it.marker ? it.marker.thread.last_reply_seq : it.m.seq;
    const unread = counting && seq > readAt && (!!it.marker || it.m.author_id !== meId);
    if (!unread) continue;
    const late = !!(it.marker ? it.reply && it.reply.late : it.m.late);
    if (late) { if (it.m) it.late = true; continue; }
    if (!newDrawn) { it.newBefore = true; newDrawn = true; }
  }
  for (const it of items) { delete it.rank; delete it.reply; }
  return items;
}
