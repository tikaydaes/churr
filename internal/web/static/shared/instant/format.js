// Showing an instant (ruling R69): in the reader's zone and locale,
// compact in the text ("Fri 20 Nov, 2:30 pm", the year only when it is not
// this one, "Today" where true), in full for screen readers and the
// converter. Everything is worded by Intl in the given locale. No DOM.

import { offsetOf, wallIn, utc } from './zone.js';

const DAY = 86400000;
const cache = new Map();

function fmt(locale, opts) {
  const key = locale + JSON.stringify(opts);
  let f = cache.get(key);
  if (!f) { f = new Intl.DateTimeFormat(locale, opts); cache.set(key, f); }
  return f;
}

// inZone formats ms with opts in a zone that may be an offset ("+10:00"),
// which Intl is not sure to take: the clock is moved by hand and read as
// UTC.
function inZone(ms, zone, locale, opts) {
  const off = offsetOf(zone);
  if (off !== null) return fmt(locale, { ...opts, timeZone: 'UTC' }).format(new Date(ms + off));
  return fmt(locale, { ...opts, timeZone: zone }).format(new Date(ms));
}

function wall(ms, zone) {
  const off = offsetOf(zone);
  if (off === null) return wallIn(ms, zone);
  const d = new Date(ms + off);
  return { y: d.getUTCFullYear(), mo: d.getUTCMonth() + 1, d: d.getUTCDate() };
}

const capital = (s, locale) => s.charAt(0).toLocaleUpperCase(locale) + s.slice(1);

// The time of day, with seconds when they were typed.
export function timeOfDay(ms, { zone, locale, precision }) {
  return inZone(ms, zone, locale, { hour: 'numeric', minute: '2-digit', second: precision === 'second' ? '2-digit' : undefined });
}

// dayWord is "Today", "Tomorrow" or "Yesterday" where true, else the
// weekday, day and month, and the year when it is not this one.
export function dayWord(ms, { zone, locale, now = Date.now() }) {
  const a = wall(ms, zone), b = wall(now, zone);
  const diff = Math.round((utc(a.y, a.mo, a.d) - utc(b.y, b.mo, b.d)) / DAY);
  if (diff >= -1 && diff <= 1) {
    return capital(new Intl.RelativeTimeFormat(locale, { numeric: 'auto' }).format(diff, 'day'), locale);
  }
  return inZone(ms, zone, locale, { weekday: 'short', day: 'numeric', month: 'short', year: a.y === b.y ? undefined : 'numeric' });
}

// compact is how an instant is shown in a message.
export function compact(ms, opts) {
  return `${dayWord(ms, opts)}, ${timeOfDay(ms, opts)}`;
}

// full is the whole date, time and zone, for screen readers and the
// converter.
export function full(ms, { zone, locale, precision }) {
  const date = inZone(ms, zone, locale, { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });
  return `${date}, ${timeOfDay(ms, { zone, locale, precision })}`;
}

// zoneName is a zone as a person reads it: "Europe/Berlin (UTC+1)", or
// "UTC+10:00" for an offset.
export function zoneName(zone, ms) {
  if (zone === 'Z') return 'UTC';
  const off = offsetOf(zone);
  if (off !== null) return 'UTC' + zone;
  return `${zone.replace(/_/g, ' ')} (${offsetText(zone, ms)})`;
}

// offsetText is a zone's offset at a moment: "UTC+10", "UTC−3:30".
export function offsetText(zone, ms) {
  const w = wallIn(ms, zone);
  const off = Math.round((utc(w.y, w.mo, w.d, w.h, w.mi, w.s) - Math.floor(ms / 1000) * 1000) / 60000);
  if (off === 0) return 'UTC';
  const a = Math.abs(off);
  return `UTC${off < 0 ? '−' : '+'}${Math.floor(a / 60)}${a % 60 ? ':' + String(a % 60).padStart(2, '0') : ''}`;
}

// relative is how long until or since: "in 3 days", "2 hours ago".
export function relative(ms, { locale, now = Date.now() }) {
  const diff = ms - now;
  const a = Math.abs(diff);
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'always' });
  const steps = [[60e3, 1e3, 'second'], [3600e3, 60e3, 'minute'], [DAY, 3600e3, 'hour'], [60 * DAY, DAY, 'day'], [365 * DAY, 30 * DAY, 'month']];
  for (const [limit, unit, name] of steps) {
    if (a < limit) return rtf.format(Math.trunc(diff / unit), name);
  }
  return rtf.format(Math.trunc(diff / (365 * DAY)), 'year');
}
