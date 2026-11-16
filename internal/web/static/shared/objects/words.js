// The web chat's wording for objects in messages (ruling R73), one table
// per language like the rest of its strings.

const EN = {
  title: 'What is “{typed}”?',
  referenceRow: 'Reference {typed}, opens {host}',
  instantRow: 'Time: {when} ({yourTime})',
  plainRow: 'Plain text: “{typed}”',
  messageLinkRow: 'Link to a message, shown as a preview',
  saidObject: '{typed} accepted. Backspace makes it plain text.',
  saidPlain: '{typed} kept as plain text.',
  patternsTitle: 'Reference patterns',
  patternsAbout: 'Text such as PROJ-123 or #4521 becomes a link to where the thing it names is kept. A change applies to messages posted after it.',
  patternsNone: 'No reference patterns yet.',
  patternsServer: 'Server-wide patterns',
  patternsChannel: 'This channel’s patterns',
  patternsChannelAbout: 'They apply in this channel, ahead of the server’s with the same prefix.',
  patternName: 'Name',
  patternPrefix: 'Prefix',
  patternPrefixHelp: 'Such as PROJ- or #. The value follows it at once.',
  patternExpression: 'Value (optional)',
  patternExpressionHelp: 'Empty for 1 to 12 digits. Otherwise letters, digits, _ and -, classes such as [A-Z0-9], \\d and \\w, each with ?, +, * or {n,m}.',
  patternURL: 'Address',
  patternURLHelp: 'https://… with {value} where the matched value goes.',
  patternAdd: 'Add a pattern',
  patternEdit: 'Change pattern',
  patternSave: 'Save',
  patternRemove: 'Remove {name}',
  patternRemoveAsk: 'Remove the pattern “{name}”? Messages already sent keep their links.',
  patternExample: 'Example: {example}',
};

const TABLES = { en: EN };

// t words a key, filling {names} from vars.
export function t(key, vars = {}) {
  const lang = (typeof navigator !== 'undefined' && navigator.language || 'en').slice(0, 2);
  const table = TABLES[lang] || EN;
  return String(table[key] ?? EN[key] ?? key).replace(/\{(\w+)\}/g, (m, k) => (vars[k] ?? m));
}
