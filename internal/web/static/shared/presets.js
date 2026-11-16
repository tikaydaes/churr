// The form presets people post from the composer (docs/FEATURES.md 7.5):
// /poll, /rsvp, /signup and /approve, read from the command text with the
// fixed grammar every client shares, and checked before anything is sent.
// The app reads by the same rules (app/lib/core/forms/presets.dart); both
// are held to testdata/commands/presets.json (e2e/commands.mjs). Nothing
// here touches the page, so it runs under Node.
//
// The time grammar of FEATURES.md 9.2 (the "once" part) is here too, for
// --until, the RSVP's When and /remind.

// A preset the client refused: the code the shared vectors name and the
// sentence to show.
export class PresetError extends Error {
  constructor(code, message) {
    super(message);
    this.code = code;
  }
}
const refuse = (code, message) => { throw new PresetError(code, message); };

// splitParts splits command text on `|`, trimming each part; `\|` is a
// literal bar.
export function splitParts(text) {
  const parts = [];
  let b = '';
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if (ch === '\\' && text[i + 1] === '|') { b += '|'; i++; } else if (ch === '|') { parts.push(b.trim()); b = ''; } else b += ch;
  }
  parts.push(b.trim());
  return parts;
}

// The flags of 7.5, and which commands take each.
const FLAGS_ON = {
  anonymous: ['poll'],
  multiple: ['poll', 'signup'],
  add: ['poll'],
  results: ['poll'],
  until: ['poll', 'signup', 'approve'],
};

// takeFlags takes the flags off the end of the last part: the part without
// them, and the flags by name (a flag with no value maps to '').
function takeFlags(command, last) {
  const m = /(^|\s)--/.exec(last);
  if (!m) return [last, {}];
  const head = last.slice(0, m.index).trim();
  const flags = {};
  let current = null;
  for (const t of last.slice(m.index).trim().split(/\s+/)) {
    if (t.startsWith('--')) {
      const name = t.slice(2);
      if (!Object.prototype.hasOwnProperty.call(FLAGS_ON, name) || !FLAGS_ON[name].includes(command)) refuse('unknown_flag', `/${command} does not know the flag --${name}.`);
      current = name;
      flags[name] = '';
    } else if (current === 'results' || current === 'until') {
      flags[current] = flags[current] ? `${flags[current]} ${t}` : t;
    } else refuse('stray_word', `"${t}" is not part of any flag.`);
  }
  return [head, flags];
}

export const WEEKDAYS = ['monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday', 'sunday'];

// weekday gives 1 (Monday) to 7 (Sunday) for a weekday's name or its first
// three letters, or null.
export function weekday(w) {
  for (let i = 0; i < 7; i++) if (w === WEEKDAYS[i] || w === WEEKDAYS[i].slice(0, 3)) return i + 1;
  return null;
}

// clockTime reads `HH:MM`, `H am`, `H pm`, `H:MM am`, `9am` (the words
// after `at`) as [hour, minute], or null.
export function clockTime(words) {
  let s = words.join(' ').toLowerCase().replaceAll(' ', '');
  let pm = false, am = false;
  if (s.endsWith('pm')) { pm = true; s = s.slice(0, -2); } else if (s.endsWith('am')) { am = true; s = s.slice(0, -2); }
  const m = /^(\d{1,2})(?::(\d{2}))?$/.exec(s);
  if (!m) return null;
  let h = Number(m[1]);
  const min = m[2] === undefined ? 0 : Number(m[2]);
  if (min > 59) return null;
  if (am || pm) {
    if (h < 1 || h > 12) return null;
    if (h === 12) h = 0;
    if (pm) h += 12;
  } else if (h > 23) return null;
  return [h, min];
}

const SPANS = { m: 60, min: 60, mins: 60, minute: 60, minutes: 60, h: 3600, hour: 3600, hours: 3600, d: 86400, day: 86400, days: 86400, w: 604800, week: 604800, weeks: 604800 };

// duration reads the words after `in` (`20m`, `2h 30m`, `3 days`) as
// milliseconds, or null.
export function duration(words) {
  const rest = words.join(' ').replace(/(\d+)\s*([a-z]+)/g, '$1$2 ');
  const spans = [...rest.matchAll(/(\d+)([a-z]+)/g)];
  if (!spans.length || rest.replace(/(\d+)([a-z]+)/g, '').trim()) return null;
  let secs = 0;
  for (const s of spans) {
    if (!Object.prototype.hasOwnProperty.call(SPANS, s[2])) return null;
    secs += Number(s[1]) * SPANS[s[2]];
  }
  return secs > 0 ? secs * 1000 : null;
}

const words = (text) => String(text).trim().toLowerCase().split(/\s+/).filter(Boolean);

// dayOf reads one day word: `today`, `tomorrow`, a weekday (its next
// occurrence after today) or `YYYY-MM-DD`, as a local midnight; null if it
// is none of them.
export function dayOf(w, now) {
  const y = now.getFullYear(), mo = now.getMonth(), d = now.getDate();
  if (w === 'today') return new Date(y, mo, d);
  if (w === 'tomorrow') return new Date(y, mo, d + 1);
  const wd = weekday(w);
  if (wd !== null) {
    const today = (now.getDay() + 6) % 7 + 1;
    let add = (((wd - today) % 7) + 7) % 7;
    if (add === 0) add = 7;
    return new Date(y, mo, d + add);
  }
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(w);
  if (!m) return null;
  const day = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
  if (day.getMonth() !== Number(m[2]) - 1 || day.getDate() !== Number(m[3])) return null;
  return day;
}

// parseMoment reads a single moment in the "once" part of the time grammar
// of FEATURES.md 9.2: `in 2h 30m`, `tomorrow at 9`, `friday`,
// `2026-12-01 at 14:30`, `at 17:00`. Wall clock in this browser's zone. A
// missing time means 09:00; `at` alone means the next time the clock shows
// it; a weekday means its next occurrence after today. Null when the text
// does not parse.
export function parseMoment(text, now) {
  const ws = words(text);
  if (!ws.length) return null;
  if (ws[0] === 'in') {
    const ms = duration(ws.slice(1));
    return ms === null ? null : new Date(now.getTime() + ms);
  }
  const at = ws.indexOf('at');
  const dayWords = at < 0 ? ws : ws.slice(0, at);
  let hm = [9, 0];
  if (at >= 0) {
    hm = clockTime(ws.slice(at + 1));
    if (!hm) return null;
  }
  if (!dayWords.length) {
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate(), hm[0], hm[1]);
    return today > now ? today : new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1, hm[0], hm[1]);
  }
  if (dayWords.length !== 1) return null;
  const day = dayOf(dayWords[0], now);
  return day && new Date(day.getFullYear(), day.getMonth(), day.getDate(), hm[0], hm[1]);
}

function future(what, text, now) {
  const t = parseMoment(text, now);
  if (!t) refuse('bad_time', `Could not read "${text}" as a time.`);
  if (!(t > now)) refuse('in_past', `${what} is in the past.`);
  return t;
}

function options(labels, places = false) {
  return labels.map((l, i) => {
    let label = l, max;
    if (places) {
      const m = /^(.*?)\s*\((\d+)\)$/.exec(label);
      if (m) {
        label = m[1].trim();
        max = Number(m[2]);
        if (max < 1) refuse('no_places', `"${l}" needs at least one place.`);
      }
    }
    if (!label) refuse('empty_option', `Option ${i + 1} is empty.`);
    return max === undefined ? { id: `o${i + 1}`, label } : { id: `o${i + 1}`, label, max };
  });
}

const RESULTS = ['live', 'after_answer', 'after_close'];
const labelsOf = (xs) => xs.map((x) => x.trim()).filter(Boolean);
const until = (flags, now) => (flags.until === undefined ? null : future('The closing time', flags.until, now));
const closes = (t) => (t ? { closes_at: t.toISOString() } : {});

// Each command's parts, with the flags taken off the last.
function partsOf(command, args) {
  const parts = splitParts(args);
  const [last, flags] = takeFlags(command, parts[parts.length - 1]);
  parts[parts.length - 1] = last;
  return [parts, flags];
}

// pollForm reads `/poll Question? | Option | Option … [--anonymous]
// [--multiple] [--add] [--results live|after_answer|after_close]
// [--until when]`.
export function pollForm(args, now) {
  const [parts, flags] = partsOf('poll', args);
  if (flags.results !== undefined && !RESULTS.includes(flags.results)) refuse('bad_results', '--results takes live, after_answer or after_close.');
  const closesAt = until(flags, now);
  const question = parts[0].trim();
  const labels = labelsOf(parts.slice(1));
  if (!question) refuse('needs_question', 'A poll needs a question.');
  if (labels.length < 2) refuse('too_few_options', 'A poll needs at least two options.');
  if (labels.length > 50) refuse('too_many_options', 'A poll has at most 50 options.');
  return {
    preset: 'poll',
    title: question,
    fields: [{ id: 'choice', type: 'choice', label: question, required: true, ...('multiple' in flags ? { multiple: true } : {}), options: options(labels) }],
    action: { type: 'tally' },
    ...('anonymous' in flags ? { anonymous: true } : {}),
    results: flags.results || 'live',
    ...('add' in flags ? { allow_added_options: true } : {}),
    ...closes(closesAt),
  };
}

// rsvpForm reads `/rsvp What | When`; When is the event's time.
export function rsvpForm(args, now) {
  const [parts] = partsOf('rsvp', args);
  if (!parts[0]) refuse('needs_what', 'An RSVP needs a what.');
  if (parts.length < 2 || !parts[1]) refuse('needs_when', 'An RSVP needs a when.');
  if (parts.length > 2) refuse('too_many_parts', 'An RSVP takes a what and a when, nothing more.');
  const when = future('The time', parts[1], now);
  return {
    preset: 'rsvp',
    title: parts[0],
    fields: [{ id: 'choice', type: 'choice', label: parts[0], required: true, options: [{ id: 'going', label: 'Going' }, { id: 'maybe', label: 'Maybe' }, { id: 'cant', label: "Can't" }] }],
    action: { type: 'tally' },
    results: 'live',
    closes_at: when.toISOString(),
  };
}

// signupForm reads `/signup What | Slot | Slot (3) … [--multiple]
// [--until when]`. A slot ending in a number in brackets has that many
// places; without one there is no limit.
export function signupForm(args, now) {
  const [parts, flags] = partsOf('signup', args);
  const closesAt = until(flags, now);
  const what = parts[0].trim();
  const labels = labelsOf(parts.slice(1));
  if (!what) refuse('needs_what', 'A sign-up needs a what.');
  if (!labels.length) refuse('needs_slot', 'A sign-up needs at least one slot.');
  if (labels.length > 50) refuse('too_many_options', 'A sign-up has at most 50 slots.');
  return {
    preset: 'signup',
    title: what,
    fields: [{ id: 'choice', type: 'choice', label: what, required: true, ...('multiple' in flags ? { multiple: true } : {}), options: options(labels, true) }],
    action: { type: 'tally' },
    ...closes(closesAt),
  };
}

// approveForm reads `/approve What [| @group] [--until when]`. The second
// part names a group whose members alone may press; the server refuses it
// from anyone not acting as manager. groupIdOf finds a group's id by its
// handle.
export function approveForm(args, now, groupIdOf = () => null) {
  const [parts, flags] = partsOf('approve', args);
  if (parts.length > 2) refuse('too_many_parts', '/approve takes a what and at most one group.');
  let group = null;
  if (parts.length === 2 && parts[1]) {
    const handle = parts[1].startsWith('@') ? parts[1].slice(1) : parts[1];
    group = groupIdOf(handle);
    if (!group) refuse('unknown_group', `No group @${handle} here.`);
  }
  const closesAt = until(flags, now);
  const what = parts[0].trim();
  if (!what) refuse('needs_what', 'Say what is to be approved.');
  return {
    preset: 'approve',
    title: what,
    fields: [],
    buttons: [{ id: 'approve', label: 'Approve', style: 'primary' }, { id: 'decline', label: 'Decline', style: 'danger' }],
    action: { type: 'tally' },
    ...(group ? { answerer_group_id: group } : {}),
    ...closes(closesAt),
  };
}

// presetForm reads a form command's text into the form to post; it throws
// a PresetError saying what is wrong.
export function presetForm(name, args, now, groupIdOf) {
  switch (name) {
    case 'poll': return pollForm(args, now);
    case 'rsvp': return rsvpForm(args, now);
    case 'signup': return signupForm(args, now);
    case 'approve': return approveForm(args, now, groupIdOf);
  }
  return refuse('bad_form', `/${name} is not a form command.`);
}
