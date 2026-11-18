// The web chat's wording for the catalogue's values, one table per
// language like the rest of its strings.

const EN = {
  newerTip: 'This needs a newer version of Churr to be shown as more than text',
  newerNote: 'Shown as text: this needs a newer version of Churr.',
  diceTitle: 'Dice',
  diceOpen: 'Dice {typed}. {said}. Opens the roll',
  diceRemoved: '(not rolled: written in an edit)',
  diceRemovedLabel: 'Dice {typed}, not rolled: written in an edit',
};

const TABLES = { en: EN };

// t words a key, filling {names} from vars.
export function t(key, vars = {}) {
  const lang = (typeof navigator !== 'undefined' && navigator.language || 'en').slice(0, 2);
  const table = TABLES[lang] || EN;
  return String(table[key] ?? EN[key] ?? key).replace(/\{(\w+)\}/g, (m, k) => (vars[k] ?? m));
}
