// Recurring check-in forms (docs/FEATURES.md section 11): what a channel
// manager sets up, as the web chat holds it, and the scheduled item the
// server stores for it. A check-in is a `scheduled` row of kind `form`
// whose content is a message with a `checkin` form, action `thread`, one
// answer per person, posted on a repeating schedule (FEATURES.md 9.3).
//
// The setup the screens edit:
//   { title, text, questions: [{ label, long, required }], days, at, zone }
// days is seven 0s and 1s, Monday first; at is "HH:MM" in zone.

export const MAX_QUESTIONS = 20;
const DAY_NAMES = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'];

// blank is a new check-in: weekdays at nine, in the given zone.
export function blank(zone) {
  return { title: 'Check-in', text: '', questions: [{ label: 'What did you do yesterday?', long: false, required: false }, { label: 'What will you do today?', long: false, required: false }, { label: 'Anything in your way?', long: false, required: false }], days: '1111100', at: '09:00', zone: zone || 'UTC' };
}

// problem says what is wrong with a setup, or null.
export function problem(v) {
  if (!v.title || !v.title.trim()) return 'Give it a title.';
  const qs = (v.questions || []).filter((q) => q.label && q.label.trim());
  if (!qs.length) return 'Ask at least one question.';
  if (qs.length > MAX_QUESTIONS) return `At most ${MAX_QUESTIONS} questions.`;
  if (!/^[01]{7}$/.test(v.days || '') || !v.days.includes('1')) return 'Choose at least one day.';
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

export const repeat = (v) => ({ every: 'day', days: v.days, at: v.at, time_zone: v.zone });

// createBody is the request that sets one up in a channel.
export function createBody(conversationId, v) {
  return { conversation_id: conversationId, kind: 'form', content: content(v), repeat: repeat(v) };
}

// fromItem reads a stored check-in back into a setup. A schedule that
// runs every week or month is read as the days it is not (every day);
// saving it makes it a daily one on the days chosen.
export function fromItem(s) {
  const f = (s.content && s.content.form) || {};
  const r = s.repeat || {};
  return {
    title: f.title || '',
    text: (s.content && s.content.text) || '',
    questions: (f.fields || []).map((x) => ({ label: x.label || '', long: x.type === 'longtext', required: !!x.required })),
    days: r.every === 'day' && /^[01]{7}$/.test(r.days || '') ? r.days : '1111111',
    at: r.at || '09:00',
    zone: r.time_zone || 'UTC',
  };
}

// repeatWords says when a schedule runs: "Weekdays at 09:30 (Europe/Dublin)".
export function repeatWords(r) {
  if (!r) return 'Once';
  const when = ` at ${r.at} (${r.time_zone})`;
  if (r.every === 'week') return 'Every week' + when;
  if (r.every === 'month') return 'Every month' + when;
  const d = r.days || '1111111';
  if (d === '1111111') return 'Every day' + when;
  if (d === '1111100') return 'Weekdays' + when;
  if (d === '0000011') return 'Weekends' + when;
  return [...d].map((x, i) => (x === '1' ? DAY_NAMES[i] : null)).filter(Boolean).join(', ') + when;
}

export { DAY_NAMES };
