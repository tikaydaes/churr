// Checks, without a browser, the web chat's built-in commands (ruling R67)
// against the shared files in testdata/commands: the table, the grammar
// (parse.json), the form presets and the time grammar (presets.json), /remind
// (remind.json), the decks (decks.json), and that the chance reels land on
// the server's result for every slot.
//
//   node e2e/commands.mjs

import { readFileSync } from 'node:fs';
import { COMMANDS, HIDDEN_ALIASES, REPLACEABLE, GROUP_OF, GROUPS, isProtected, parse, literal, findDeck, hiddenBuiltins, MUTE_MAX_SECONDS, STRINGS } from '../internal/web/static/shared/commands.js';
import { presetForm, parseMoment, PresetError } from '../internal/web/static/shared/presets.js';
import { fromWall, utc } from '../internal/web/static/shared/instant/zone.js';
import { parseRemind, RemindError } from '../internal/web/static/shared/remind.js';
import { DECKS, SUITS, RANKS, EXTRAS } from '../internal/web/static/shared/message/cardnames.js';
import { slots, cardLabel, summaryKeys, isRedCard } from '../internal/web/static/shared/message/chance.js';
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
  check(`/${c.name}: args, sealed and admin`, mine[0] === c.args && mine[1] === c.sealed && !!mine[2] === c.admin, JSON.stringify(mine));
  check(`/${c.name}: its group in /help`, GROUP_OF[c.name] === c.group && GROUPS.includes(c.group), GROUP_OF[c.name]);
}
check('the hidden aliases', same(HIDDEN_ALIASES, table.aliases), JSON.stringify(HIDDEN_ALIASES));
check('the replaceable ones', same([...REPLACEABLE].sort(), table.commands.filter((c) => c.replaceable).map((c) => c.name).sort()), [...REPLACEABLE].join(' '));
for (const [a, to] of Object.entries(table.aliases)) check(`alias /${a} is protected as /${to} is`, isProtected(a) === isProtected(to));

// The grammar.
const vectors = read('parse.json');
for (const c of vectors.cases) {
  const got = parse(c.text, c.hidden ? new Set(c.hidden) : undefined);
  check(`parse ${JSON.stringify(c.text)}`, same(got, c.want), `got ${JSON.stringify(got)}, want ${JSON.stringify(c.want)}`);
  if (got && got.error) check(`${got.error} is a known error`, vectors.errors.includes(got.error));
}

check('the longest /mute is the shared one', MUTE_MAX_SECONDS === vectors.mute_max_seconds, String(MUTE_MAX_SECONDS));
for (const e of vectors.errors) check(`${e} is worded`, typeof STRINGS.errors[e] === 'function');

// The form presets and the single moments of the time grammar
// (presets.json), read at its now in its zone: a time is wall clock in the
// zone unless it ends in Z.
const presets = read('presets.json');
const local = (s, zone) => {
  if (s.endsWith('Z')) return new Date(s);
  const [d, t] = s.split('T'); const [y, mo, da] = d.split('-').map(Number); const [h, mi] = t.split(':').map(Number);
  return new Date(fromWall(utc(y, mo, da, h, mi), zone));
};
const presetNow = local(presets.now, presets.zone);
for (const c of presets.cases) {
  let got;
  try {
    got = presetForm(c.command, c.args, presetNow, (h) => presets.groups[h] || null, presets.zone);
  } catch (err) {
    if (!(err instanceof PresetError)) throw err;
    check(`/${c.command} ${c.args}: refused`, c.error === err.code && presets.errors.includes(err.code), `got ${err.code} (${err.message}), want ${c.error || 'a form'}`);
    continue;
  }
  const want = c.want && { ...c.want };
  if (want && want.closes_at) want.closes_at = local(want.closes_at, presets.zone).toISOString();
  check(`/${c.command} ${c.args}`, !c.error && same(got, want), `got ${JSON.stringify(got)}, want ${JSON.stringify(want || c.error)}`);
}
for (const m of presets.moments) {
  const zone = m.zone || presets.zone;
  const got = parseMoment(m.text, local(m.now || presets.now, zone), zone);
  check(`the moment ${JSON.stringify(m.text)}`, m.want === null ? got === null : got !== null && got.getTime() === local(m.want, zone).getTime(), `got ${got && got.toISOString()}`);
}

// /remind (remind.json), read at its now in its zone.
const reminds = read('remind.json');
for (const c of reminds.cases) {
  const zone = c.zone || reminds.zone;
  let got;
  try {
    got = parseRemind(c.args, local(c.now || reminds.now, zone), zone);
  } catch (err) {
    if (!(err instanceof RemindError)) throw err;
    check(`/remind ${c.args}: refused`, c.error === err.code && reminds.errors.includes(err.code), `got ${err.code} (${err.message}), want ${c.error || 'a reminder'}`);
    continue;
  }
  const want = c.want && { ...c.want };
  if (want && want.at) want.at = local(want.at, zone).getTime();
  if (got.at) got.at = got.at.getTime();
  check(`/remind ${c.args}`, !c.error && same(got, want), `got ${JSON.stringify(got)}, want ${JSON.stringify(want || c.error)}`);
}

// The one rule of hidden built-ins (ruling R72).
for (const [i, c] of vectors.hidden_cases.entries()) {
  const got = [...hiddenBuiltins(c.listed, c.admin)].sort();
  check(`hidden case ${i}`, same(got, c.hidden), `got ${JSON.stringify(got)}, want ${JSON.stringify(c.hidden)}`);
}

// The literal slash (ruling R74).
for (const c of vectors.literal_cases) {
  const got = literal(c.text);
  check(`literal ${JSON.stringify(c.text)}`, got === c.literal, `got ${JSON.stringify(got)}, want ${JSON.stringify(c.literal)}`);
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

// The reels (and the cards turned over) land on the server's result, slot by slot.
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
  if (c.kind === 'draw' || c.kind === 'tarot') check(`${c.kind}: cards are turned over, not spun: no reel`, s.every((x) => x.card && x.reel.length === 1 && x.reel[0] === x.text && !x.reelKeys));
  else check(`${c.kind}: each reel spins and lands on the slot's face`, s.every((x) => x.reel.length >= 2 && x.reel[x.reel.length - 1] === x.text));
  if (c.kind === 'draw' || c.kind === 'tarot') check(`${c.kind}: each face is the card's`, s.every((x, i) => x.text === cardLabel(c.deck, c.cards[i]).short));
}

// Red suits: hearts, diamonds and the red joker are red; spades, clubs and
// the other jokers are ink.
// Tarot and mahjong tiles are never red.
{
  const drawn = ['hearts-ace', 'diamonds-10', 'spades-king', 'clubs-2', 'joker-red', 'joker-black', 'joker-white'];
  const s = slots({ kind: 'draw', deck: 'jokers3', cards: drawn });
  check('red suits are red, black suits are not', JSON.stringify(s.map((x) => !!x.red)) === JSON.stringify([true, true, false, false, true, false, false]), JSON.stringify(s.map((x) => x.red)));
  check('isRedCard', isRedCard('hearts-queen') && isRedCard('diamonds-ace') && !isRedCard('spades-ace') && !isRedCard('clubs-ace') && !isRedCard('cups-ace'));
  const t = slots({ kind: 'tarot', deck: 'tarot', spread: 'three', cards: ['cups-ace', 'major-0', 'wands-2'], reversed: [false, false, false] });
  check('tarot is never red', t.every((x) => !x.red && !(x.reelRed || []).some(Boolean)));
  const m = slots({ kind: 'draw', deck: 'mahjong', cards: ['dragon-red', 'characters-1'] });
  check('mahjong is never red', m.every((x) => !x.red && !(x.reelRed || []).some(Boolean)));
}

// The tarot.
const tarot = read('tarot.json');
check('the same spreads', same(Object.fromEntries(Object.entries(SPREADS).map(([k, v]) => [k, v.positions])), Object.fromEntries(tarot.spreads.map((s) => [s.name, s.positions]))));
check('the same meanings', same(MEANINGS, tarot.meanings));
for (const c of tarot.summary_cases) check(`the summary of ${c.cards.join(' ')}`, same(summaryKeys(c.cards, c.reversed), c.keys), JSON.stringify(summaryKeys(c.cards, c.reversed)));

console.log(`${failures ? 'FAIL' : 'ok  '} commands: ${checks - failures} of ${checks} checks`);
if (failures) process.exitCode = 1;
