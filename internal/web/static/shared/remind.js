// The /remind grammar (docs/FEATURES.md 9.2 and 9.3):
//
//   /remind [target] when | what
//
// read into who it is for, when it runs (one moment, or a repeat rule) and
// what it says. The app reads by the same rules
// (app/lib/core/commands/remind.dart); both are held to
// testdata/commands/remind.json (e2e/commands.mjs). Nothing here touches
// the page, so it runs under Node.

import { parseMoment, clockTime, weekday, WEEKDAYS } from './presets.js';

// A reminder the client refused: the code the shared vectors name and the
// sentence to show.
export class RemindError extends Error {
  constructor(code, message) {
    super(message);
    this.code = code;
  }
}
const refuse = (code, message) => { throw new RemindError(code, message); };

const PERSON = /^@([a-z0-9._-]+(?:@[a-z0-9.-]+)?)$/i;
const LINK = /^<#([0-9a-f-]{36})>$/i;

// repeatOf reads the words after `every`: a unit, then optionally `at` and
// a time. It gives { every, days?, at } or null.
function repeatOf(words) {
  const at = words.indexOf('at');
  const unit = (at < 0 ? words : words.slice(0, at)).join('');
  let hm = [9, 0];
  if (at >= 0) {
    hm = clockTime(words.slice(at + 1));
    if (!hm) return null;
  }
  const clock = String(hm[0]).padStart(2, '0') + ':' + String(hm[1]).padStart(2, '0');
  if (unit === 'day') return { every: 'day', days: '1111111', at: clock };
  if (unit === 'weekday') return { every: 'day', days: '1111100', at: clock };
  if (unit === 'week' || unit === 'month') return { every: unit, at: clock };
  const days = ['0', '0', '0', '0', '0', '0', '0'];
  const names = unit.split(',');
  for (const n of names) {
    const d = weekday(n);
    if (d === null) return null;
    days[d - 1] = '1';
  }
  return { every: 'day', days: days.join(''), at: clock };
}

// parseRemind reads what follows /remind. target is { kind: 'me' },
// { kind: 'person', person } or { kind: 'channel', id }; then either at, a
// Date (one run), or repeat, { every, days?, at } (the rule of 9.3 without
// its time zone, which the client adds). Times are wall clock in this
// browser's zone. It throws a RemindError saying what is wrong.
export function parseRemind(args, now) {
  const bar = args.indexOf('|');
  const head = (bar < 0 ? args : args.slice(0, bar)).trim();
  const what = bar < 0 ? '' : args.slice(bar + 1).trim();
  let words = head.split(/\s+/).filter(Boolean);
  let target = { kind: 'me' };
  if (words.length) {
    const first = words[0];
    const p = PERSON.exec(first), l = LINK.exec(first);
    if (first.toLowerCase() === 'me') { words = words.slice(1); } else if (p) { target = { kind: 'person', person: p[1].toLowerCase() }; words = words.slice(1); } else if (l) { target = { kind: 'channel', id: l[1].toLowerCase() }; words = words.slice(1); } else if (first.startsWith('#')) refuse('bad_channel', 'Name a channel by typing # and picking it.');
  }
  words = words.map((w) => w.toLowerCase());
  if (!words.length) refuse('needs_when', 'Say when, before the |: /remind tomorrow at 9 | Send the report.');
  if (!what) refuse('needs_what', 'Say what to be reminded of, after a |: /remind tomorrow at 9 | Send the report.');
  if (words[0] === 'every') {
    const repeat = repeatOf(words.slice(1));
    if (!repeat) refuse('bad_when', `Could not read "${words.join(' ')}" as a time. Write every day, every weekday, every week, every month or every monday,thursday, then at and a time if you like.`);
    return { target, what, repeat };
  }
  const at = parseMoment(words.join(' '), now);
  if (!at) refuse('bad_when', `Could not read "${words.join(' ')}" as a time. Write in 20m, tomorrow at 9, friday at 14:30, 2026-12-01 or at 17:00.`);
  if (!(at > now)) refuse('in_past', 'That time has already passed.');
  return { target, what, at };
}

// repeatWords says a repeat rule back in words, for what the client says
// once it is set: "every weekday at 09:30".
export function repeatWords(r) {
  let unit = r.every;
  if (r.every === 'day' && r.days === '1111100') unit = 'weekday';
  else if (r.every === 'day' && r.days && r.days !== '1111111') unit = [...r.days].map((c, i) => (c === '1' ? WEEKDAYS[i] : null)).filter(Boolean).join(', ');
  return `every ${unit} at ${r.at}`;
}
