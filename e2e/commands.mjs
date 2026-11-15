// Checks, without a browser, the web chat's built-in commands (ruling R67)
// against the shared files in testdata/commands: the table, the grammar
// (parse.json), the decks (decks.json), and that the chance reels land on
// the server's result for every slot.
//
//   node e2e/commands.mjs

import { readFileSync } from 'node:fs';
import { COMMANDS, parse, findDeck } from '../internal/web/static/shared/commands.js';
import { DECKS, SUITS, RANKS, EXTRAS } from '../internal/web/static/shared/message/cardnames.js';
import { slots, cardLabel, summaryKeys } from '../internal/web/static/shared/message/chance.js';
import { SPREADS, MEANINGS } from '../internal/web/static/shared/message/cardnames.js';

const read = (p) => JSON.parse(readFileSync(new URL('../testdata/commands/' + p, import.meta.url), 'utf8'));
let failures = 0, checks = 0;
function check(name, ok, detail) {
  checks++;
  if (!ok) { failures++; console.log(`FAIL commands: ${name}${detail ? '\n       ' + detail : ''}`); }
}
const same = (a, b) => JSON.stringify(sorted(a)) === JSON.stringify(sorted(b));
function sorted(v) {
  if (Array.isArray(v)) return v.map(sorted);
  if (v && typeof v === 'object') return Object.fromEntries(Object.keys(v).sort().map((k) => [k, sorted(v[k])]));
  return v;
}

// The table.
const table = read('commands.json');
check('the table has every command, in order', same(Object.keys(COMMANDS), table.commands.map((c) => c.name)), Object.keys(COMMANDS).join(' '));
for (const c of table.commands) {
  const mine = COMMANDS[c.name] || [];
  check(`/${c.name}: args and sealed`, mine[0] === c.args && mine[1] === c.sealed, JSON.stringify(mine));
}

// The grammar.
const vectors = read('parse.json');
for (const c of vectors.cases) {
  const got = parse(c.text);
  check(`parse ${JSON.stringify(c.text)}`, same(got, c.want), `got ${JSON.stringify(got)}, want ${JSON.stringify(c.want)}`);
  if (got && got.error) check(`${got.error} is a known error`, vectors.errors.includes(got.error));
}

// The decks.
const decks = read('decks.json');
check('the same decks', same(Object.keys(DECKS), decks.decks.map((d) => d.name)));
for (const d of decks.decks) {
  const mine = DECKS[d.name];
  check(`deck ${d.name}: cards, aliases, orientation`, mine && same(mine.cards, d.cards) && same(mine.aliases, d.aliases) && mine.orientation === d.orientation && mine.naming === d.naming);
  for (const a of [d.name, ...d.aliases]) check(`deck ${a} is found`, findDeck(a.toUpperCase()) === d.name);
  for (const card of new Set(d.cards)) {
    const l = cardLabel(d.name, card);
    check(`${d.name} ${card} has words`, l && l.name && l.short && !/undefined/.test(l.name + l.short), JSON.stringify(l));
    check(`${d.name} ${card} reads in English as the server writes it`, l.name === decks.english[d.name][card], `${l.name} / ${decks.english[d.name][card]}`);
    check(`${d.name} ${card}: its character`, (l.glyph || undefined) === decks.glyphs[card]);
  }
}
check('every suit, rank and extra card has words', Object.keys(decks.suits).every((s) => SUITS[s]) && Object.keys(decks.ranks).every((r) => RANKS[r]) && Object.keys(decks.extras).every((e) => EXTRAS[e]));

// The reels land on the server's result, slot by slot.
const dice = read('dice.json');
for (const f of dice.fallback) {
  const c = f.chance;
  const s = slots(c);
  let want;
  switch (c.kind) {
    case 'roll': want = c.terms.flatMap((t) => (t.rolls && t.rolls.length ? t.rolls.map(String) : [String(t.value)])); break;
    case 'flip': want = [c.outcome]; break;
    case 'pick': want = [c.options[c.picked]]; break;
    case 'shuffle': want = c.order.map((i) => c.options[i]); break;
    case 'draw': case 'tarot': want = c.cards; break;
  }
  check(`${c.kind}: each slot holds the server's value`, same(s.map((x) => x.key), want), JSON.stringify(s.map((x) => x.key)));
  check(`${c.kind}: each reel spins and lands on the slot's face`, s.every((x) => x.reel.length >= 2 && x.reel[x.reel.length - 1] === x.text));
  if (c.kind === 'draw' || c.kind === 'tarot') check(`${c.kind}: each face is the card's`, s.every((x, i) => x.text === cardLabel(c.deck, c.cards[i]).short));
}

// The tarot.
const tarot = read('tarot.json');
check('the same spreads', same(Object.fromEntries(Object.entries(SPREADS).map(([k, v]) => [k, v.positions])), Object.fromEntries(tarot.spreads.map((s) => [s.name, s.positions]))));
check('the same meanings', same(MEANINGS, tarot.meanings));
for (const c of tarot.summary_cases) check(`the summary of ${c.cards.join(' ')}`, same(summaryKeys(c.cards, c.reversed), c.keys), JSON.stringify(summaryKeys(c.cards, c.reversed)));

console.log(`${failures ? 'FAIL' : 'ok  '} commands: ${checks - failures} of ${checks} checks`);
if (failures) process.exitCode = 1;
