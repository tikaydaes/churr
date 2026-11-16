// Instants (ruling R69): turning a typed date and time into the moment it
// names, and that moment into a wall clock in any zone. The rules are the
// server's (internal/instant): every client runs the cases in
// testdata/instants/resolve.json. The zone database is the browser's own,
// through Intl.
//
// No DOM here, so Node runs it as it is (e2e/instants.mjs).

import { parse } from '../md/parse.js';

const DAY = 86400000;
const formatters = new Map();

// The browser's formatter that reads a moment's wall clock in a zone.
function wallFormatter(zone) {
  let f = formatters.get(zone);
  if (!f) {
    f = new Intl.DateTimeFormat('en-US', { timeZone: zone, hourCycle: 'h23', year: 'numeric', month: 'numeric', day: 'numeric', hour: 'numeric', minute: 'numeric', second: 'numeric', weekday: 'short', era: 'short' });
    formatters.set(zone, f);
  }
  return f;
}

const WEEKDAYS = { Mon: 0, Tue: 1, Wed: 2, Thu: 3, Fri: 4, Sat: 5, Sun: 6 };

// wallIn gives the wall clock in zone at the moment ms: { y, mo, d, h, mi,
// s, wd } with mo from 1 and wd 0 for Monday to 6 for Sunday.
export function wallIn(ms, zone) {
  const p = {};
  for (const part of wallFormatter(zone).formatToParts(new Date(ms))) p[part.type] = part.value;
  let y = +p.year;
  if (p.era && /^B/.test(p.era)) y = 1 - y;
  return { y, mo: +p.month, d: +p.day, h: +p.hour % 24, mi: +p.minute, s: +p.second, wd: WEEKDAYS[p.weekday] };
}

// utc is Date.UTC without its reading of years 0 to 99 as 1900 to 1999.
export function utc(y, mo, d, h = 0, mi = 0, s = 0, ms = 0) {
  const t = new Date(Date.UTC(2000, mo - 1, d, h, mi, s, ms));
  t.setUTCFullYear(y);
  return t.getTime();
}

// offsetAt is the zone's offset from UTC, in milliseconds, at the moment ms.
export function offsetAt(zone, ms) {
  const w = wallIn(ms, zone);
  const whole = Math.floor(ms / 1000) * 1000;
  return utc(w.y, w.mo, w.d, w.h, w.mi, w.s) - whole;
}

// isZoneName says whether z is an IANA name the zone database knows, held to
// the server's rule that every part starts with a capital letter, since the
// browser itself does not mind case.
export function isZoneName(z) {
  if (typeof z !== 'string' || !z || z === 'Local' || z.length > 64 || z.includes('..')) return false;
  if (!z.split('/').every((p) => /^[A-Z][A-Za-z0-9_+-]*$/.test(p))) return false;
  try { wallFormatter(z); return true; } catch { return false; }
}

// offsetOf reads a zone written as an offset ("Z", "+10:00", "+1000",
// "-05") in milliseconds east of UTC, or null.
export function offsetOf(z) {
  if (z === 'Z') return 0;
  const m = /^([+-])(\d{2})(?::?(\d{2}))?$/.exec(z || '');
  if (!m || (z.length === 6 && z[3] !== ':') || (z.length === 5 && z.includes(':'))) return null;
  const h = +m[2], mi = m[3] ? +m[3] : 0;
  if (h > 14 || mi > 59) return null;
  return (m[1] === '-' ? -1 : 1) * (h * 3600 + mi * 60) * 1000;
}

export const validZone = (z) => offsetOf(z) !== null || isZoneName(z);

// fromWall finds the moment at which zone's clocks show the wall time w (a
// UTC millisecond count of the wall fields). A time the clocks skip gives
// null; a time they show twice gives the first.
export function fromWall(w, zone) {
  let best = null;
  const seen = new Set();
  for (const probe of [w - DAY, w, w + DAY]) {
    const off = offsetAt(zone, probe);
    if (seen.has(off)) continue;
    seen.add(off);
    const u = w - off;
    if (offsetAt(zone, u) !== off) continue;
    if (best === null || u < best) best = u;
  }
  return best;
}

// iso writes a moment as the API does: UTC with milliseconds.
export function iso(ms) {
  const d = new Date(ms);
  const y = d.getUTCFullYear();
  const pad = (n, k = 2) => String(n).padStart(k, '0');
  return `${pad(y, 4)}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}T${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}.${pad(d.getUTCMilliseconds(), 3)}Z`;
}

// resolve gives the instant entity's fields for a typed local date and
// time ("YYYY-MM-DDTHH:MM[:SS[.f]]") in zone as written, or in authorZone
// when none was: { time, precision, zone }, or null when it stays text.
export function resolve(local, zone, authorZone) {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2})(?:\.(\d{1,9}))?)?$/.exec(local || '');
  if (!m) return null;
  const [y, mo, d, h, mi] = [+m[1], +m[2], +m[3], +m[4], +m[5]];
  const s = m[6] ? +m[6] : 0;
  const ms = m[7] ? +(m[7] + '00').slice(0, 3) : 0;
  const leap = (y % 4 === 0 && y % 100 !== 0) || y % 400 === 0;
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][mo - 1];
  if (y < 1 || !days || d < 1 || d > days || h > 23 || mi > 59 || s > 59) return null;
  const precision = m[6] ? 'second' : 'minute';
  let z = zone || '';
  if (!z) {
    if (!isZoneName(authorZone)) return null;
    z = authorZone;
  }
  const wall = utc(y, mo, d, h, mi, s, ms);
  const off = offsetOf(z);
  if (off !== null) return { time: iso(wall - off), precision, zone: z };
  if (!isZoneName(z)) return null;
  const u = fromWall(wall, z);
  return u === null ? null : { time: iso(u), precision, zone: z };
}

// viewerZone is the zone this browser is in.
export function viewerZone() {
  try { return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'; } catch { return 'UTC'; }
}

// allZones lists the zone names the browser knows, for the converter's
// search.
export function allZones() {
  try { return Intl.supportedValuesOf('timeZone'); } catch { return ['UTC']; }
}

// instantNodes lists the typed instants of a text that may become entities:
// every instant node of its tree outside checklist items, where no entity
// may stand (testdata/markdown/README.md).
export function instantNodes(text) {
  const out = [];
  const inl = (nodes) => {
    for (const n of nodes || []) {
      if (n.t === 'instant') out.push(n);
      else if (n.c && n.t !== 'link' && n.t !== 'nolink') inl(n.c);
    }
  };
  const blk = (nodes) => {
    for (const n of nodes || []) {
      if (n.t === 'p' || n.t === 'h') inl(n.c);
      else if (n.t === 'quote') blk(n.b);
      else if (n.t === 'list') for (const it of n.items || []) { if (it.checked === undefined) inl(it.c); blk(it.b); }
      else if (n.t === 'table') for (const c of [...(n.head || []), ...(n.rows || []).flat()]) inl(c.c);
    }
  };
  blk(parse(String(text || '')));
  return out;
}

// instantEntities resolves a text's typed instants the way the server
// would, for a sealed chat where the sending device works out the entities
// (docs/SEALED.md section 8.3).
export function instantEntities(text, authorZone, objectHints = []) {
  const out = [];
  for (const n of instantNodes(text)) {
    // One the author kept as plain text (an object hint, ruling R73).
    if (objectHints.some((h) => h.kind === 'plain' && n.at < h.end && h.start < n.at + n.len)) continue;
    const r = resolve(n.local, n.zone, authorZone);
    if (r) out.push({ type: 'instant', at: n.at, len: n.len, ...r });
  }
  return out;
}
