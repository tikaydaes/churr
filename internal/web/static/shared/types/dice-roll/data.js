// The data of kind dice_roll (docs/DESIGN-MODULES.md sections 4.2 and 9.1;
// the schema DiceRoll of docs/openapi.yaml): dice the server rolled, as
// `{dice, rows}`, each row `{dice, terms, total}`, each term `{sign, count,
// sides, keep, keep_n, rolls, dropped, value}` for dice or `{sign, value}`
// for a number. The checks are the server's (internal/model/chance.go,
// DiceRoll.Valid) and testdata/types/dice_roll.json holds both to the same
// answers. No DOM.

// The limits a roll is held to (model.MaxDiceRows and the rest).
export const MAX_ROWS = 8, MAX_TERMS = 10, MAX_DICE = 100, MAX_SIDES = 1000;

const KEEPS = ['kh', 'kl', 'dh', 'dl'];
const whole = (v) => Number.isSafeInteger(v);
const wholes = (v) => Array.isArray(v) && v.every(whole);
const text = (v) => typeof v === 'string';
const named = (v) => typeof v === 'string' && v !== '';

// diceRollData checks a value's data and gives the roll as this client
// draws it, or null when it does not hold together, which makes the value
// fall back a step (to its alt, then its text). Fields this client does
// not know are left out of what it draws; a `keep` it does not know is
// taken as its default, none (day-one rule 2). The data itself is never
// changed: what is returned is a copy.
export function diceRollData(d) {
  if (!d || typeof d !== 'object' || Array.isArray(d) || !named(d.dice) || !Array.isArray(d.rows)) return null;
  if (d.rows.length === 0 || d.rows.length > MAX_ROWS) return null;
  let dice = 0;
  const rows = [];
  for (const row of d.rows) {
    if (!row || typeof row !== 'object' || !named(row.dice) || !whole(row.total) || !Array.isArray(row.terms)) return null;
    if (row.terms.length === 0 || row.terms.length > MAX_TERMS) return null;
    let total = 0;
    const terms = [];
    for (const t of row.terms) {
      if (!t || typeof t !== 'object' || (t.sign !== 1 && t.sign !== -1) || !whole(t.value)) return null;
      for (const k of ['count', 'sides', 'keep_n']) if (t[k] !== undefined && !whole(t[k])) return null;
      for (const k of ['rolls', 'dropped']) if (t[k] !== undefined && !wholes(t[k])) return null;
      if (t.keep !== undefined && !text(t.keep)) return null;
      const term = { sign: t.sign, value: t.value };
      if (t.count > 0) {
        const rolls = t.rolls || [];
        if (rolls.length !== t.count || !(t.sides >= 2 && t.sides <= MAX_SIDES)) return null;
        if (rolls.some((r) => r < 1 || r > t.sides)) return null;
        const dropped = t.dropped || [];
        if (dropped.some((i) => i < 0 || i >= rolls.length) || new Set(dropped).size !== dropped.length) return null;
        dice += t.count;
        Object.assign(term, { count: t.count, sides: t.sides, rolls: [...rolls], dropped: [...dropped] });
        if (KEEPS.includes(t.keep)) Object.assign(term, { keep: t.keep, keep_n: t.keep_n });
      }
      total += t.sign * t.value;
      terms.push(term);
    }
    if (total !== row.total) return null;
    rows.push({ dice: row.dice, terms, total: row.total });
  }
  if (dice > MAX_DICE) return null;
  return { dice: d.dice, rows };
}
