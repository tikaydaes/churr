// The objects recognised inside runs of plain text (ruling R73,
// docs/MESSAGE-FORMAT.md section 3.2): the same reading as the server's
// internal/markdown/objects.go, for the two places a client needs it: a
// sealed chat, where the sending device works out the entities, and the
// composer, which offers each object as it is typed. The shared vectors in
// testdata/markdown say what it must find. No DOM.
//
// Objects are not nodes of the tree. Each lies inside one text node, so
// none is found in code, a link's text or address, a bare link, an escape,
// across formatting marks or in a checklist item.

import { parse } from './parse.js';

// The kinds, in the order they are tried at a position.
export const OBJECT_KINDS = ['reference'];

// The limits the server sets (internal/model/reference.go).
export const MAX_REF_VALUE = 64;
export const MAX_OBJECT_HINTS = 50;
const REF_DIGITS = '[0-9]{1,12}';

const isWhite = (c) => (c >= 0x09 && c <= 0x0d) || c === 0x20 || c === 0x85 || c === 0xa0 || c === 0x1680
  || (c >= 0x2000 && c <= 0x200a) || c === 0x2028 || c === 0x2029 || c === 0x202f || c === 0x205f || c === 0x3000;
const WORD = /[\p{L}\p{N}]/u;
const isWord = (c) => WORD.test(String.fromCodePoint(c));
const LEFT = new Set([...'([{"\'~*_|>,;:=“‘«—–'].map((s) => s.codePointAt(0)));
const RIGHT = new Set([...')]}"\'*_~|<”’»…—–!?'].map((s) => s.codePointAt(0)));
const SOFT = new Set([...'.,;:'].map((s) => s.codePointAt(0)));

function before(src, i) {
  if (i <= 0) return -1;
  const c = src.charCodeAt(i - 1);
  if (c >= 0xdc00 && c <= 0xdfff && i >= 2) {
    const hi = src.charCodeAt(i - 2);
    if (hi >= 0xd800 && hi <= 0xdbff) return src.codePointAt(i - 2);
  }
  return c;
}
const at = (src, i) => (i >= src.length ? -1 : src.codePointAt(i));

// leftEdge says whether an object may start at i.
export function leftEdge(src, i) {
  const c = before(src, i);
  return c < 0 || isWhite(c) || LEFT.has(c);
}

// rightEdge says whether an object may end at j.
export function rightEdge(src, j) {
  const c = at(src, j);
  if (c < 0 || isWhite(c) || RIGHT.has(c)) return true;
  if (SOFT.has(c)) {
    const n = at(src, j + 1);
    return n < 0 || !isWord(n);
  }
  return false;
}

// orderPatterns: a channel's before the server's, then the longer prefix,
// then the older.
export function orderPatterns(list) {
  return [...(list || [])].sort((x, y) => {
    const cx = !!x.conversation_id, cy = !!y.conversation_id;
    if (cx !== cy) return cx ? -1 : 1;
    if (x.prefix.length !== y.prefix.length) return y.prefix.length - x.prefix.length;
    if (x.created_at !== y.created_at) return x.created_at < y.created_at ? -1 : 1;
    return x.id < y.id ? -1 : x.id > y.id ? 1 : 0;
  });
}

const compiled = new Map();
function expression(expr) {
  if (!compiled.has(expr)) {
    let re = null;
    // The server checked the expression when it was saved; its form
    // (no groups, no alternatives, no dot) reads the same here.
    try { re = new RegExp('^(?:' + expr + ')'); } catch { re = null; }
    compiled.set(expr, re);
  }
  return compiled.get(expr);
}

// refURL is the address a value resolves to.
export const refURL = (p, value) => p.url_template.split('{value}').join(value);

function asciiRun(src, i, b, max) {
  const end = Math.min(b, i + max);
  let k = i;
  while (k < end && src.charCodeAt(k) < 0x80) k++;
  return src.slice(i, k);
}

function reference(src, i, b, patterns) {
  for (const p of patterns) {
    const n = p.prefix.length;
    if (!n || i + n >= b || src.slice(i, i + n) !== p.prefix) continue;
    const re = expression(p.expression || REF_DIGITS);
    if (!re) continue;
    const m = re.exec(asciiRun(src, i + n, b, MAX_REF_VALUE));
    const value = m ? m[0] : '';
    if (!value || !rightEdge(src, i + n + value.length)) continue;
    return { type: 'reference', at: i, len: n + value.length, url: refURL(p, value), pattern_id: p.id, value };
  }
  return null;
}

// objectAt reads the object that starts at i, of the one kind given or of
// the first kind that reads.
function objectAt(src, i, b, ctx, only) {
  const tryKind = (k) => !only || only === k;
  if (tryKind('reference')) {
    const e = reference(src, i, b, ctx.patterns);
    if (e) return e;
  }
  return null;
}

const hinted = (hints, e) => hints.some((h) => h.kind === e.type && h.start === e.at && h.end === e.at + e.len);

// objectsInRun finds the objects of one run of plain text, [a, b).
function objectsInRun(src, a, b, ctx, out) {
  const hints = ctx.hints;
  for (let i = a; i < b;) {
    let only = '', skip = 0;
    for (const h of hints) {
      if (h.kind === 'plain' && i >= h.start && i < h.end) skip = h.end;
      else if (h.kind !== 'plain' && h.start === i) only = h.kind;
    }
    if (skip > i) { i = skip; continue; }
    if (leftEdge(src, i)) {
      let e = null;
      if (only) { e = objectAt(src, i, b, ctx, only); if (e && !hinted(hints, e)) e = null; }
      if (!e) e = objectAt(src, i, b, ctx, '');
      if (e && hints.some((h) => h.kind === 'plain' && e.at < h.end && h.start < e.at + e.len)) e = null;
      if (e) { out.push(e); i = e.at + e.len; continue; }
    }
    i++;
  }
}

// findObjects returns the object entities of a text, in order of `at`.
// ctx: { patterns: the reference patterns that apply, hints: the author's
// object hints }. tree, if given, is parse(text).
export function findObjects(text, ctx = {}, tree = null) {
  const src = String(text || '');
  const c = { patterns: orderPatterns(ctx.patterns), hints: (ctx.hints || []).slice(0, MAX_OBJECT_HINTS) };
  const out = [];
  for (const [a, b] of textRuns(tree || parse(src))) objectsInRun(src, a, b, c, out);
  return out;
}

// textRuns lists the runs of plain text [a, b) objects may be found in.
export function textRuns(tree) {
  const out = [];
  const inl = (nodes) => {
    for (const n of nodes || []) {
      if (n.t === 'text') out.push([n.at, n.at + n.len]);
      else if (n.t !== 'link' && n.t !== 'nolink' && n.c) inl(n.c);
    }
  };
  const blk = (nodes) => {
    for (const n of nodes || []) {
      if (n.t === 'p' || n.t === 'h') inl(n.c);
      else if (n.t === 'quote') blk(n.b);
      else if (n.t === 'list') for (const it of n.items || []) { if (it.checked === undefined) inl(it.c); blk(it.b); }
      else if (n.t === 'table') for (const cell of [...(n.head || []), ...(n.rows || []).flat()]) inl(cell.c);
    }
  };
  blk(tree);
  return out;
}

// readingsAt lists every reading of the text at i, one per kind, in the
// order the kinds are tried: the composer offers each as a row.
export function readingsAt(text, i, ctx = {}, tree = null) {
  const src = String(text || '');
  const run = textRuns(tree || parse(src)).find(([a, b]) => i >= a && i < b);
  if (!run || !leftEdge(src, i)) return [];
  const c = { patterns: orderPatterns(ctx.patterns), hints: [] };
  return OBJECT_KINDS.map((k) => objectAt(src, i, run[1], c, k)).filter(Boolean);
}

// plainCovers says whether a plain hint reaches into [a, b): the author
// asked for that text to stay as typed, which holds for a typed instant
// too.
export const plainCovers = (hints, a, b) => (hints || []).some((h) => h.kind === 'plain' && a < h.end && h.start < b);
