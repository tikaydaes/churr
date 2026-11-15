// Phrases that name a time ("this Friday at 5pm", "tomorrow 9:30", "in 2
// hours"), which the composer offers to make into an instant (ruling R69).
// Nothing is ever converted without the author accepting; the words stay
// the message's text and the instant travels as a hint beside it.
//
// The grammar is the apps' too (app/lib/core/instant/phrases.dart): both
// run testdata/instants/phrases.json, with a fixed "now" and zone. The
// words come from a table per language, so another language is a new
// table, not new code. No DOM here.
//
//   phrase   = day [","] [ "at" ] time          tomorrow at 5pm, Fri 9:30
//            | time [ "on" ] day                5pm tomorrow, 5pm on Friday
//            | "in" amount unit                 in 2 hours, in an hour
//   day      = today | tomorrow | tonight
//            | [ this | next | on ] weekday
//            | [ on ] D month [ YYYY ] | [ on ] month D [ "," ] [ YYYY ]
//   time     = H [ ":" MM ] am|pm   |   H ":" MM (24-hour)   |   noon
//   amount   = 1 to 999 | a | an | half an
//   unit     = minutes | hours (and their short forms)
//
// Rules, each with cases in the vectors:
// - An hour alone with no minutes and no am/pm ("at 5") is too vague and
//   is not offered, except after "tonight", where it is in the evening
//   (as is "tonight 9:30"). H:MM without am/pm is otherwise 24-hour.
// - A day without a time ("Friday", "tomorrow", "3 March") is not offered:
//   a date alone is not an instant. Nor is "in 2 days".
// - A weekday is the next one from today; today itself only if the time
//   is still to come, else a week on. "this" is the same. "next" moves it a
//   week on when it falls within the current week (Monday to Sunday).
// - A date without a year is the next one to come, this year or the next.
// - The wall time is read in the author's zone. One the clocks skip is not
//   offered; one they show twice is the first.
// - Midnight is not recognised: whether "Friday at midnight" starts or ends
//   Friday is anyone's guess.

import { wallIn, utc, fromWall, iso } from './zone.js';

// The words of each language. Keys are lower case; the matching ignores
// case.
export const WORDS = {
  en: {
    today: ['today'], tomorrow: ['tomorrow'], tonight: ['tonight'],
    this: ['this'], next: ['next'], on: ['on'], at: ['at'], in: ['in'],
    weekdays: [['monday', 'mon'], ['tuesday', 'tue', 'tues'], ['wednesday', 'wed'], ['thursday', 'thu', 'thur', 'thurs'], ['friday', 'fri'], ['saturday', 'sat'], ['sunday', 'sun']],
    months: [['january', 'jan'], ['february', 'feb'], ['march', 'mar'], ['april', 'apr'], ['may'], ['june', 'jun'], ['july', 'jul'], ['august', 'aug'], ['september', 'sep', 'sept'], ['october', 'oct'], ['november', 'nov'], ['december', 'dec']],
    am: ['am', 'a.m.'], pm: ['pm', 'p.m.'], noon: ['noon', 'midday'],
    ordinal: ['st', 'nd', 'rd', 'th'],
    one: ['a', 'an'], half: ['half an', 'half a'],
    minutes: ['minutes', 'minute', 'mins', 'min'], hours: ['hours', 'hour', 'hrs', 'hr', 'h'],
  },
};

const esc = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&').replace(/ /g, '\\s+');
const alt = (list) => '(?:' + [...list].sort((a, b) => b.length - a.length).map(esc).join('|') + ')';

// The compiled grammar of one language.
function grammar(lang) {
  const w = WORDS[lang] || WORDS.en;
  const wk = w.weekdays.flat(), mo = w.months.flat();
  const ord = alt(w.ordinal);
  const S = '\\s+';
  const g = {
    w,
    day: new RegExp(`(${alt(w.today)})|(${alt(w.tomorrow)})|(${alt(w.tonight)})|(?:(${alt(w.this)}|${alt(w.next)})${S}|${alt(w.on)}${S})?(${alt(wk)})(?![\\p{L}\\p{N}])|(?:${alt(w.on)}${S})?(?:(\\d{1,2})(?:${ord})?${S}(${alt(mo)})(?:${S}(\\d{4}))?|(${alt(mo)})${S}(\\d{1,2})(?:${ord})?(?:,?${S}(\\d{4}))?)`, 'iuy'),
    joinDT: new RegExp(`,?${S}(?:${alt(w.at)}${S})?`, 'iuy'),
    joinTD: new RegExp(`${S}(?:${alt(w.on)}${S})?`, 'iuy'),
    time: new RegExp(`(\\d{1,2})(?::(\\d{2}))?(?:\\s?(${alt(w.am)}|${alt(w.pm)}))?|(${alt(w.noon)})`, 'iuy'),
    in: new RegExp(`${alt(w.in)}${S}(\\d{1,3}|${alt(w.half)}|${alt(w.one)})${S}(${alt(w.minutes)}|${alt(w.hours)})`, 'iuy'),
  };
  g.is = (list, s) => list.includes(s.toLowerCase().replace(/\s+/g, ' '));
  return g;
}

const grammars = new Map();
const grammarOf = (lang) => { if (!grammars.has(lang)) grammars.set(lang, grammar(lang)); return grammars.get(lang); };

const isWordChar = (ch) => !!ch && /[\p{L}\p{N}]/u.test(ch);

function at(re, text, i) {
  re.lastIndex = i;
  return re.exec(text);
}

// A time of day as { h, mi, bare }, or null. bare: an hour alone.
function readTime(g, m) {
  if (m[4]) return { h: 12, mi: 0 };
  let h = +m[1];
  const mi = m[2] === undefined ? 0 : +m[2];
  if (mi > 59) return null;
  if (m[3]) {
    if (h < 1 || h > 12) return null;
    const pm = g.is(g.w.pm, m[3]);
    h = (h % 12) + (pm ? 12 : 0);
    return { h, mi, ampm: true };
  }
  if (m[2] === undefined) return h >= 1 && h <= 12 ? { h, mi: 0, bare: true } : null;
  return h <= 23 ? { h, mi } : null;
}

// The day a day-match names, as { y, mo, d } in the author's calendar,
// given the time it goes with and now's wall clock.
function readDay(g, m, time, now, zone) {
  const days = (y, mo, d, n) => { const t = new Date(utc(y, mo, d) + n * 86400000); return { y: t.getUTCFullYear(), mo: t.getUTCMonth() + 1, d: t.getUTCDate() }; };
  const later = (day) => utc(day.y, day.mo, day.d, time.h, time.mi) > utc(now.y, now.mo, now.d, now.h, now.mi);
  if (m[1]) return time.bare ? null : { y: now.y, mo: now.mo, d: now.d };
  if (m[2]) return time.bare ? null : days(now.y, now.mo, now.d, 1);
  if (m[3]) {
    // Tonight is the evening: an hour before noon without am or pm is
    // after it.
    if (!time.ampm && time.h < 12) time.h += 12;
    else if (time.bare) return null;
    return { y: now.y, mo: now.mo, d: now.d };
  }
  if (time.bare) return null;
  if (m[5]) {
    const target = g.w.weekdays.findIndex((names) => g.is(names, m[5]));
    let delta = (target - now.wd + 7) % 7;
    if (delta === 0 && !later({ y: now.y, mo: now.mo, d: now.d })) delta = 7;
    if (m[4] && g.is(g.w.next, m[4]) && now.wd + delta <= 6) delta += 7;
    return days(now.y, now.mo, now.d, delta);
  }
  const d = +(m[6] || m[10]);
  const monthWord = m[7] || m[9];
  const mo = g.w.months.findIndex((names) => g.is(names, monthWord)) + 1;
  const year = m[8] || m[11];
  const valid = (y) => { const leap = (y % 4 === 0 && y % 100 !== 0) || y % 400 === 0; return d >= 1 && d <= [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][mo - 1]; };
  if (year) return valid(+year) ? { y: +year, mo, d } : null;
  let day = { y: now.y, mo, d };
  if (!valid(day.y) || !later(day)) day = { y: now.y + 1, mo, d };
  return valid(day.y) ? day : null;
}

// find lists the phrases in text: { start, end, time, precision, zone }
// with start and end in UTF-16 code units, the instant in UTC as the API
// writes it, read in zone at the moment nowMs.
export function find(text, { now: nowMs = Date.now(), zone, lang = 'en' } = {}) {
  const g = grammarOf(lang);
  const out = [];
  if (!zone) return out;
  const now = wallIn(nowMs, zone);
  const bounded = (end) => !isWordChar(text[end]);
  for (let i = 0; i < text.length; i++) {
    if (isWordChar(text[i - 1]) || !isWordChar(text[i])) continue;
    const hit = phraseAt(g, text, i, now, nowMs, zone, bounded);
    if (hit) { out.push(hit); i = hit.end - 1; }
  }
  return out;
}

function phraseAt(g, text, i, now, nowMs, zone, bounded) {
  const done = (end, ms) => (ms === null ? null : { start: i, end, time: iso(ms), precision: 'minute', zone });
  const wall = (day, t) => fromWall(utc(day.y, day.mo, day.d, t.h, t.mi), zone);
  let m = at(g.in, text, i);
  if (m && bounded(g.in.lastIndex)) {
    const n = /^\d/.test(m[1]) ? +m[1] : g.is(g.w.half, m[1]) ? 0.5 : 1;
    const minutes = n * (g.is(g.w.hours, m[2]) ? 60 : 1);
    if (!Number.isInteger(minutes) || minutes < 1) return null;
    return done(g.in.lastIndex, Math.floor(nowMs / 60000) * 60000 + minutes * 60000);
  }
  // Day first, then the time.
  m = at(g.day, text, i);
  if (m) {
    const j = at(g.joinDT, text, g.day.lastIndex) ? g.joinDT.lastIndex : -1;
    const t = j > 0 ? at(g.time, text, j) : null;
    if (t && bounded(g.time.lastIndex)) {
      const time = readTime(g, t);
      const day = time && readDay(g, m, time, now, zone);
      return day ? done(g.time.lastIndex, wall(day, time)) : null;
    }
    return null;
  }
  // The time first, then the day.
  const t = at(g.time, text, i);
  if (t && t[0]) {
    const j = at(g.joinTD, text, g.time.lastIndex) ? g.joinTD.lastIndex : -1;
    const d = j > 0 ? at(g.day, text, j) : null;
    if (d && bounded(g.day.lastIndex)) {
      const time = readTime(g, t);
      const day = time && readDay(g, d, time, now, zone);
      return day ? done(g.day.lastIndex, wall(day, time)) : null;
    }
  }
  return null;
}

// endingAt is the phrase that ends exactly at position end of text, as the
// composer asks while the author types; null if there is none.
export function endingAt(text, end, opts) {
  return find(text.slice(0, end), opts).find((p) => p.end === end) || null;
}
