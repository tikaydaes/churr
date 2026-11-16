// What the composer does with objects while the author types (ruling R73;
// docs/CLIENTS.md, "Objects in the composer"). No DOM, so e2e/objects.mjs
// can drive it without a browser.
//
// Class A is an exact token: a reference, a typed date and time, and the
// kinds after them. When one is complete at the caret a list opens with
// each reading of it and then "Plain text", the first pre-selected.
// Finishing the word (space, punctuation, Enter to send) takes the selected
// row; Escape takes plain text; Backspace straight after an accepted object
// makes it plain text again.
//
// Class B is a loose phrase ("tomorrow at 5pm"): offered, never taken by
// itself; Tab or Enter takes the selected row and Enter then does not send.
// Escape, or typing on, dismisses it; space does not take it.
//
// What the author chose goes to the server as object hints: `plain` for a
// range kept as typed, or a kind for a range read as other than its first
// reading. Taking the first reading sends nothing: the server reads it so
// anyway.

import { parse } from '../md/parse.js';
import { findObjects, readingsAt, leftEdge, rightEdge } from '../md/objects.js';
import { instantNodes, resolve as resolveInstant } from '../instant/zone.js';

// PLAIN is the kind of the last row, and of a hint keeping text as typed.
export const PLAIN = 'plain';

// candidate is the class A token that ends at the caret and that the
// author has not yet decided about: { start, end, typed, rows }, where each
// row is { kind, entity } and the last is { kind: 'plain' }. Null when
// there is none. ctx: { patterns, zone, decided, origin }.
export function candidate(text, caret, ctx = {}) {
  const src = String(text || '');
  const tree = parse(src);
  const decided = ctx.decided || [];
  const hints = hintsOf(decided);
  const found = [];
  for (const n of instantNodes(src)) {
    const r = resolveInstant(n.local, n.zone, ctx.zone);
    if (r) found.push({ type: 'instant', at: n.at, len: n.len, ...r });
  }
  found.push(...findObjects(src, { patterns: ctx.patterns, hints, origin: ctx.origin }, tree));
  const e = found.find((x) => x.at + x.len === caret && x.len > 0);
  if (!e) return null;
  if (decided.some((d) => d.start === e.at && d.end === e.at + e.len)) return null;
  const rows = [{ kind: e.type, entity: e }];
  if (e.type !== 'instant') {
    for (const r of readingsAt(src, e.at, { patterns: ctx.patterns, origin: ctx.origin }, tree)) {
      if (r.type !== e.type && r.len === e.len) rows.push({ kind: r.type, entity: r });
    }
  }
  rows.push({ kind: PLAIN });
  return { start: e.at, end: e.at + e.len, typed: src.slice(e.at, e.at + e.len), rows };
}

// choose records the author's choice of a row: { start, end, typed, kind,
// first }, where first says it was the first row.
export function choose(cand, index) {
  const row = cand.rows[index];
  return { start: cand.start, end: cand.end, typed: cand.typed, kind: row.kind, first: index === 0 };
}

// track moves decisions to where their text is now: the same place, or
// the nearest place the same text is found, standing alone as a token.
// One whose text was edited is dropped, so the token may be offered again.
export function track(decided, text) {
  const out = [];
  const alone = (i, n) => text.slice(i, i + n.length) === n && leftEdge(text, i) && rightEdge(text, i + n.length);
  for (const d of decided) {
    let best = -1;
    if (alone(d.start, d.typed)) best = d.start;
    else {
      for (let i = text.indexOf(d.typed); i >= 0; i = text.indexOf(d.typed, i + 1)) {
        if (!alone(i, d.typed) || out.some((o) => o.start === i)) continue;
        if (best < 0 || Math.abs(i - d.start) < Math.abs(best - d.start)) best = i;
      }
    }
    if (best >= 0) out.push({ ...d, start: best, end: best + d.typed.length });
  }
  return out;
}

// hintsOf is the object hints decisions send: plain text, and readings
// other than the first.
export function hintsOf(decided) {
  return decided.filter((d) => d.kind === PLAIN || !d.first).map(({ start, end, kind }) => ({ start, end, kind }));
}

// undo makes the decision over [start, end) plain text.
export function undo(decided, start, end) {
  return decided.map((d) => (d.start === start && d.end === end ? { ...d, kind: PLAIN, first: false } : d));
}

const WORD = /[\p{L}\p{N}]/u;

// keyAction says what a key does while the list is open, or straight
// after an object was accepted:
//
//   cls          'A' or 'B'
//   key          KeyboardEvent.key
//   afterAccept  the last thing done was accepting an object
//
// It returns one of: 'down', 'up' (move, wrapping), 'commit' (take the
// selected row and let the key through), 'commit-send' (take it, then
// send), 'accept' (take it and swallow the key), 'plain' (take plain text),
// 'dismiss' (close, leave the text alone), 'undo' (make the accepted
// object plain text), or '' (nothing to do with objects).
export function keyAction(cls, key, { open = true, afterAccept = false, shift = false } = {}) {
  if (!open) return afterAccept && key === 'Backspace' ? 'undo' : '';
  if (key === 'ArrowDown') return 'down';
  if (key === 'ArrowUp') return 'up';
  if (key === 'Escape') return cls === 'A' ? 'plain' : 'dismiss';
  if (cls === 'A') {
    if (key === 'Enter') return shift ? 'commit' : 'commit-send';
    if (key === 'Tab') return 'accept';
    if (key === ' ' || (key.length === 1 && !WORD.test(key))) return 'commit';
    return '';
  }
  // Class B: never by itself.
  if (key === 'Tab' || key === 'Enter') return 'accept';
  return '';
}
