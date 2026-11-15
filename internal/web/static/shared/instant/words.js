// The web chat's wording for instants (ruling R69), kept in one table per
// language like the rest of its strings, so a translation is a new table.
// Dates and times themselves are never worded here: they come from the
// browser's Intl in the reader's locale.

const EN = {
  converterTitle: 'Time converter',
  yourTime: 'Your time',
  asWritten: 'As written',
  utc: 'UTC',
  typedAs: 'Typed as',
  addZone: 'Add a time zone',
  searchZones: 'Search time zones and cities',
  noZones: 'No time zone matches.',
  removeZone: 'Remove {zone}',
  copyISO: 'Copy as ISO 8601',
  copied: 'Time copied',
  remindMe: 'Remind me',
  reminderSet: 'Reminder set for {when}.',
  chipLabel: '{when}, {zone}. Written as “{typed}”. Opens the time converter.',
  yourTimeShort: 'your time',
  composerSays: '{typed} will show as {when} {yourTime}',
  composerPhrase: '“{typed}” will show as {when} {yourTime}',
  keepAsText: 'Keep as text',
  keepAsTextLabel: 'Send “{typed}” as plain text',
  dropTime: 'Not a time',
  dropTimeLabel: 'Send “{typed}” as plain words',
  makeTime: 'Make this a time',
  makeTimeLine: 'Make this a time: {when} ({yourTime})',
  instantsHere: 'Times in this message',
};

const TABLES = { en: EN };

// t words a key, filling {names} from vars.
export function t(key, vars = {}) {
  const lang = (typeof navigator !== 'undefined' && navigator.language || 'en').slice(0, 2);
  const table = TABLES[lang] || EN;
  return String(table[key] ?? EN[key] ?? key).replace(/\{(\w+)\}/g, (_, k) => (vars[k] ?? ''));
}
