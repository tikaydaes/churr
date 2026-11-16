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
export const OBJECT_KINDS = ['message_link', 'reference', 'measure', 'colour', 'phone', 'email', 'geo'];

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

// Message links: the server's origin, or churr:// and its host, then
// /chat/c/{conversation} and /m/{message}, /t/{thread} or
// /t/{thread}/m/{reply}, with nothing after.
const ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const validID = (s) => ID.test(s);

function messagePath(p) {
  const c = '/chat/c/';
  if (!p.startsWith(c) || !validID(p.slice(c.length, c.length + 36))) return null;
  const conv = p.slice(c.length, c.length + 36);
  const rest = p.slice(c.length + 36);
  const part = (s, mark) => (s.startsWith(mark) && validID(s.slice(mark.length, mark.length + 36)) ? s.slice(mark.length, mark.length + 36) : null);
  const n = c.length + 36;
  let id = part(rest, '/m/');
  if (id) return { conv, msg: id, n: n + 39 };
  id = part(rest, '/t/');
  if (id) {
    const reply = part(rest.slice(39), '/m/');
    return reply ? { conv, msg: reply, n: n + 78 } : { conv, msg: id, n: n + 39 };
  }
  return null;
}

function linkStart(s, origin) {
  if (!origin) return 0;
  if (s.slice(0, origin.length).toLowerCase() === origin) return origin.length;
  const k = origin.indexOf('://');
  if (k > 0) {
    const app = 'churr://' + origin.slice(k + 3);
    if (s.slice(0, app.length).toLowerCase() === app) return app.length;
  }
  return 0;
}

// messageLink reads a whole address as a link to a message of the server
// at origin: { conv, msg } or null.
export function messageLink(link, origin) {
  const o = String(origin || '').toLowerCase();
  const at = linkStart(String(link || ''), o);
  if (!at) return null;
  const p = messagePath(link.slice(at));
  return p && at + p.n === link.length ? { conv: p.conv, msg: p.msg } : null;
}

function churrLink(src, i, b, origin) {
  if (!origin || origin.indexOf('://') < 0 || !src.startsWith('churr://', i) || i + 8 > b) return null;
  const s = asciiRun(src, i, b, origin.length + 200);
  const at = linkStart(s, origin);
  if (!at) return null;
  const p = messagePath(s.slice(at));
  if (!p || !rightEdge(src, i + at + p.n)) return null;
  return { type: 'message_link', at: i, len: at + p.n, conversation_id: p.conv, url: s.slice(0, at + p.n), message_id: p.msg };
}

// objectAt reads the object that starts at i, of the one kind given or of
// the first kind that reads.
function objectAt(src, i, b, ctx, only) {
  const tryKind = (k) => !only || only === k;
  if (tryKind('message_link')) {
    const e = churrLink(src, i, b, ctx.origin);
    if (e) return e;
  }
  if (tryKind('reference')) {
    const e = reference(src, i, b, ctx.patterns);
    if (e) return e;
  }
  if (tryKind('measure')) {
    const e = measure(src, i, b);
    if (e) return e;
  }
  if (tryKind('colour')) {
    const e = colour(src, i, b);
    if (e) return e;
  }
  if (tryKind('phone')) {
    const e = phone(src, i, b);
    if (e) return e;
  }
  if (tryKind('email')) {
    const e = email(src, i, b);
    if (e) return e;
  }
  if (tryKind('geo')) {
    const e = geo(src, i, b);
    if (e) return e;
  }
  return null;
}

// Map coordinates: `lat, lon` with at least three decimals each, or
// `geo:lat,lon` with any; latitude at most 90, longitude 180.
function degrees(src, j, b, whole, min, max) {
  let k = j;
  if (src[k] === '-') k++;
  const d = k;
  while (k < b && isDigit(src.charCodeAt(k))) k++;
  if (k === d || k - d > whole || k + 1 >= b || src[k] !== '.') return null;
  k++;
  const f = k;
  while (k < b && isDigit(src.charCodeAt(k))) k++;
  if (k - f < min || k - f > 8) return null;
  const v = src.slice(j, k);
  return Math.abs(Number(v)) <= max ? { v, k } : null;
}

function geo(src, i, b) {
  let j = i, min = 3;
  if (src.startsWith('geo:', i) && i + 4 <= b) { j = i + 4; min = 1; } else {
    let p = i;
    if (p > 0 && src[p - 1] === ' ') p--;
    if (p >= 2 && (src[p - 1] === '.' || src[p - 1] === ',') && isDigit(src.charCodeAt(p - 2))) return null;
  }
  const lat = degrees(src, j, b, 2, min, 90);
  if (!lat || src[lat.k] !== ',') return null;
  let k = lat.k + 1;
  if (min === 3 && src[k] === ' ') k++;
  const lon = degrees(src, k, b, 3, min, 180);
  if (!lon || !rightEdge(src, lon.k)) return null;
  if (src[lon.k] === ',') {
    let n = lon.k + 1;
    if (src[n] === ' ') n++;
    if (n < b && (isDigit(src.charCodeAt(n)) || src[n] === '-')) return null;
  }
  return { type: 'geo', at: i, len: lon.k - i, lat: lat.v, lon: lon.v };
}

// Telephone numbers: `+` and 7 to 15 digits, the first not 0, in groups
// one space, hyphen or dot may separate, a group optionally in brackets; a
// bracketed lone 0 is not dialled. The value is `+` and the digits.
function phone(src, i, b) {
  const dig = (k) => k < b && isDigit(src.charCodeAt(k));
  if (src[i] !== '+' || !dig(i + 1) || src[i + 1] === '0') return null;
  let digits = '+', j = i + 1, last = i + 1;
  while (j < b && j - i <= 32) {
    let k = j;
    if (k > i + 1 && (src[k] === ' ' || src[k] === '-' || src[k] === '.')) k++;
    const open = k < b && src[k] === '(' && k > i + 1;
    if (open) k++;
    const start = k;
    while (dig(k)) k++;
    if (k === start || (open && src[k] !== ')')) break;
    let group = src.slice(start, k);
    if (open) { k++; if (group === '0') group = ''; }
    digits += group;
    j = k; last = k;
  }
  const n = digits.length - 1;
  if (n < 7 || n > 15 || !rightEdge(src, last)) return null;
  return { type: 'phone', at: i, len: last - i, value: digits };
}

// Email addresses: an ASCII local part, `@`, and a domain of two or more
// labels, the last all letters and at least two of them.
const LOCAL = /[A-Za-z0-9._%+-]/;
const LABEL = /[A-Za-z0-9-]/;
const ALNUM = /[A-Za-z0-9]/;
function email(src, i, b) {
  let k = i;
  while (k < b && LOCAL.test(src[k])) k++;
  const local = src.slice(i, k);
  if (k === i || k - i > 64 || src[k] !== '@' || local[0] === '.' || local[local.length - 1] === '.' || local.includes('..')) return null;
  const d = k + 1;
  let end = d, labels = 0, lastStart = d;
  for (;;) {
    const s = end;
    while (end < b && LABEL.test(src[end])) end++;
    if (end === s || src[s] === '-' || src[end - 1] === '-') return null;
    labels++;
    lastStart = s;
    if (end + 1 < b && src[end] === '.' && ALNUM.test(src[end + 1])) { end++; continue; }
    break;
  }
  const tld = src.slice(lastStart, end);
  if (labels < 2 || tld.length < 2 || end - d > 253 || !/^[A-Za-z]+$/.test(tld) || !rightEdge(src, end)) return null;
  return { type: 'email', at: i, len: end - i, value: src.slice(i, end) };
}

// Colours: `#` and 3, 6 or 8 hex digits; the value is #rrggbb[aa] in
// lower case.
const HEX = /[0-9a-fA-F]/;
function colour(src, i, b) {
  if (src[i] !== '#') return null;
  let k = i + 1;
  while (k < b && k - i <= 9 && HEX.test(src[k])) k++;
  const n = k - i - 1;
  if ((n !== 3 && n !== 6 && n !== 8) || !rightEdge(src, k)) return null;
  let hex = src.slice(i + 1, k).toLowerCase();
  if (n === 3) hex = hex[0] + hex[0] + hex[1] + hex[1] + hex[2] + hex[2];
  return { type: 'colour', at: i, len: k - i, value: '#' + hex };
}

// Measurements: a number, at most one space, and a unit of the fixed
// table written exactly as listed; only a temperature may be negative.
// [typed, code, a space may come before it, a temperature].
export const MEASURE_UNITS = [
  ['km/h', 'km/h', 1, 0], ['kph', 'km/h', 1, 0], ['mph', 'mph', 1, 0], ['m/s', 'm/s', 1, 0],
  ['mm', 'mm', 1, 0], ['cm', 'cm', 1, 0], ['km', 'km', 1, 0], ['mi', 'mi', 1, 0],
  ['ft', 'ft', 1, 0], ['yd', 'yd', 1, 0], ['in', 'in', 0, 0], ['m', 'm', 1, 0],
  ['mg', 'mg', 1, 0], ['kg', 'kg', 1, 0], ['lbs', 'lb', 1, 0], ['lb', 'lb', 1, 0],
  ['oz', 'oz', 1, 0], ['g', 'g', 1, 0],
  ['°C', '°C', 1, 1], ['°F', '°F', 1, 1], ['\u2103', '°C', 1, 1], ['\u2109', '°F', 1, 1],
  ['ml', 'ml', 1, 0], ['mL', 'ml', 1, 0], ['gal', 'gal', 1, 0], ['l', 'l', 1, 0], ['L', 'l', 1, 0],
];
const isDigit = (c) => c >= 48 && c <= 57;

function measure(src, i, b) {
  let j = i;
  const neg = j < b && (src[j] === '-' || src[j] === '\u2212');
  if (neg) j++;
  const p = src.charCodeAt(i - 1);
  if (i >= 2 && (p === 46 || p === 44) && isDigit(src.charCodeAt(i - 2))) return null;
  let k = j;
  while (k < b && isDigit(src.charCodeAt(k))) k++;
  if (k === j || k - j > 9) return null;
  if (k + 1 < b && src[k] === '.' && isDigit(src.charCodeAt(k + 1))) {
    let f = k + 1;
    while (f < b && isDigit(src.charCodeAt(f))) f++;
    if (f - k - 1 > 6) return null;
    k = f;
  }
  if (k < b && (isDigit(src.charCodeAt(k)) || ((src[k] === '.' || src[k] === ',') && k + 1 < b && isDigit(src.charCodeAt(k + 1))))) return null;
  const value = (neg ? '-' : '') + src.slice(j, k);
  let u = k;
  const spaced = u < b && (src[u] === ' ' || src[u] === '\u00a0' || src[u] === '\u202f');
  if (spaced) u++;
  for (const [typed, code, sp, temp] of MEASURE_UNITS) {
    if ((spaced && !sp) || (neg && !temp)) continue;
    if (u + typed.length > b || !src.startsWith(typed, u) || !rightEdge(src, u + typed.length)) continue;
    return { type: 'measure', at: i, len: u + typed.length - i, value, unit: code };
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

// context is what the scanner reads with.
const context = (ctx, hints) => ({ patterns: orderPatterns(ctx.patterns), hints, origin: String(ctx.origin || '').toLowerCase() });

// findObjects returns the object entities of a text, in order of `at`.
// ctx: { patterns: the reference patterns that apply, hints: the author's
// object hints, origin: the server's own origin, for links to its
// messages }. tree, if given, is parse(text).
export function findObjects(text, ctx = {}, tree = null) {
  const src = String(text || '');
  const t = tree || parse(src);
  const c = context(ctx, (ctx.hints || []).slice(0, MAX_OBJECT_HINTS));
  const out = [];
  for (const [a, b] of textRuns(t)) objectsInRun(src, a, b, c, out);
  // A bare link to one of the server's messages is a message link, unless
  // the author kept it plain.
  for (const n of bareLinks(t)) {
    const m = messageLink(n.url, c.origin);
    if (m && !plainCovers(c.hints, n.at, n.at + n.len)) out.push({ type: 'message_link', at: n.at, len: n.len, conversation_id: m.conv, url: n.url, message_id: m.msg });
  }
  return out.sort((x, y) => x.at - y.at);
}

// bareLinks lists the bare links of a tree, where the server finds them.
function bareLinks(tree) {
  const out = [];
  const inl = (nodes) => {
    for (const n of nodes || []) {
      if (n.t === 'url') out.push(n);
      else if (n.t !== 'link' && n.t !== 'nolink' && n.t !== 'code' && n.c) inl(n.c);
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
  const c = context(ctx, []);
  return OBJECT_KINDS.map((k) => objectAt(src, i, run[1], c, k)).filter(Boolean);
}

// plainCovers says whether a plain hint reaches into [a, b): the author
// asked for that text to stay as typed, which holds for a typed instant
// too.
export const plainCovers = (hints, a, b) => (hints || []).some((h) => h.kind === 'plain' && a < h.end && h.start < b);
