// Recurring check-in forms (docs/FEATURES.md section 11): what a channel
// manager sets up, as the web chat holds it, and the scheduled item the
// server stores for it. A check-in is a `scheduled` row of kind `form`
// whose content is a message with a `checkin` form, action `thread`, one
// answer per person, posted on a repeating schedule (FEATURES.md 9.3).
//
// The setup the screens edit:
//   { title, text, questions: [{ label, long, required }], kind, days,
//     interval, monthBy, day, week, weekday, at, zone }
// kind is daily (on days), weekly (on days, every interval weeks) or
// monthly (monthBy day: on day of the month; monthBy weekday: on the week-th
// weekday, week 1 to 4 or -1 for the last, weekday 1 Monday to 7). days is
// seven 0s and 1s, Monday first; at is "HH:MM" in zone.

import { wallIn } from './instant/zone.js';

export const MAX_QUESTIONS = 20;
const DAY_NAMES = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'];
export const KINDS = [['daily', 'Daily'], ['weekly', 'Every few weeks'], ['monthly', 'Monthly']];
export const WEEKS = [[1, 'first'], [2, 'second'], [3, 'third'], [4, 'fourth'], [-1, 'last']];
const weekName = (n) => (WEEKS.find((w) => w[0] === n) || [0, ''])[1];

// blank is a new check-in: weekdays at nine, in the given zone.
export function blank(zone) {
  return { title: 'Check-in', text: '', questions: [{ label: 'What did you do yesterday?', long: false, required: false }, { label: 'What will you do today?', long: false, required: false }, { label: 'Anything in your way?', long: false, required: false }], kind: 'daily', days: '1111100', interval: 1, monthBy: 'day', day: 1, week: 1, weekday: 1, at: '09:00', zone: zone || 'UTC' };
}

// problem says what is wrong with a setup, or null.
export function problem(v) {
  if (!v.title || !v.title.trim()) return 'Give it a title.';
  const qs = (v.questions || []).filter((q) => q.label && q.label.trim());
  if (!qs.length) return 'Ask at least one question.';
  if (qs.length > MAX_QUESTIONS) return `At most ${MAX_QUESTIONS} questions.`;
  if (v.kind !== 'monthly' && (!/^[01]{7}$/.test(v.days || '') || !v.days.includes('1'))) return 'Choose at least one day.';
  if (v.kind === 'weekly' && !(Number.isInteger(v.interval) && v.interval >= 1 && v.interval <= 52)) return 'Repeat every 1 to 52 weeks.';
  if (v.kind === 'monthly' && v.monthBy === 'day' && !(Number.isInteger(v.day) && v.day >= 1 && v.day <= 31)) return 'Choose a day of the month from 1 to 31.';
  if (v.kind === 'monthly' && v.monthBy === 'weekday' && (!WEEKS.some((w) => w[0] === v.week) || !(v.weekday >= 1 && v.weekday <= 7))) return 'Choose which weekday of the month.';
  if (!/^([01][0-9]|2[0-3]):[0-5][0-9]$/.test(v.at || '')) return 'Give the time as HH:MM.';
  if (!v.zone) return 'Choose a time zone.';
  return null;
}

// content is the message each run posts.
export function content(v) {
  const qs = v.questions.filter((q) => q.label && q.label.trim());
  return {
    ...(v.text && v.text.trim() ? { text: v.text.trim() } : {}),
    form: {
      preset: 'checkin', title: v.title.trim(), action: { type: 'thread' }, one_answer: true,
      fields: qs.map((q, i) => ({ id: 'q' + (i + 1), type: q.long ? 'longtext' : 'text', label: q.label.trim(), ...(q.required ? { required: true } : {}) })),
    },
  };
}

// repeat is the rule of FEATURES.md 9.3 for a setup, in its kind.
export function repeat(v) {
  const when = { at: v.at, time_zone: v.zone };
  if (v.kind === 'weekly') return { every: 'week', days: v.days, ...(v.interval > 1 ? { interval: v.interval } : {}), ...when };
  if (v.kind === 'monthly') return { every: 'month', ...(v.monthBy === 'weekday' ? { week: v.week, weekday: v.weekday } : { day: v.day }), ...when };
  return { every: 'day', days: v.days, ...when };
}

// createBody is the request that sets one up in a channel.
export function createBody(conversationId, v) {
  return { conversation_id: conversationId, kind: 'form', content: content(v), repeat: repeat(v) };
}

// fromItem reads a stored check-in back into a setup, in the kind it was
// set up with. A weekly or monthly rule that names no day (one made with
// /remind) is read with the weekday or day of its next run.
export function fromItem(s) {
  const f = (s.content && s.content.form) || {};
  const r = s.repeat || {};
  const zone = r.time_zone || 'UTC';
  let next = null;
  try { next = s.next_run_at ? wallIn(Date.parse(s.next_run_at), zone) : null; } catch { next = null; }
  const kind = r.every === 'week' ? 'weekly' : r.every === 'month' ? 'monthly' : 'daily';
  let days = /^[01]{7}$/.test(r.days || '') ? r.days : '1111111';
  if (kind === 'weekly' && !r.days) days = next ? '0000000'.slice(0, next.wd) + '1' + '0000000'.slice(next.wd + 1) : '1000000';
  return {
    title: f.title || '',
    text: (s.content && s.content.text) || '',
    questions: (f.fields || []).map((x) => ({ label: x.label || '', long: x.type === 'longtext', required: !!x.required })),
    kind,
    days,
    interval: r.interval || 1,
    monthBy: r.week ? 'weekday' : 'day',
    day: r.day || (next ? next.d : 1),
    week: r.week || 1,
    weekday: r.weekday || 1,
    at: r.at || '09:00',
    zone,
  };
}

// repeatWords says when a schedule runs: "Weekdays at 09:30 (Europe/Dublin)".
export function repeatWords(r) {
  if (!r) return 'Once';
  const when = ` at ${r.at} (${r.time_zone})`;
  const named = (d) => [...d].map((x, i) => (x === '1' ? DAY_NAMES[i] : null)).filter(Boolean).join(', ');
  if (r.every === 'week' && r.days) return (r.interval > 1 ? `Every ${r.interval} weeks` : 'Every week') + ' on ' + named(r.days) + when;
  if (r.every === 'week') return 'Every week' + when;
  if (r.every === 'month' && r.week) return `Monthly on the ${weekName(r.week)} ${DAY_NAMES[r.weekday - 1]}` + when;
  if (r.every === 'month' && r.day) return `Monthly on day ${r.day}` + when;
  if (r.every === 'month') return 'Every month' + when;
  const d = r.days || '1111111';
  if (d === '1111111') return 'Every day' + when;
  if (d === '1111100') return 'Weekdays' + when;
  if (d === '0000011') return 'Weekends' + when;
  return named(d) + when;
}

export { DAY_NAMES };
