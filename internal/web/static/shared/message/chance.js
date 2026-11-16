// How a chance result is drawn (docs/MESSAGE-FORMAT.md section 1.2, rulings
// R67 and R68): /roll, /flip, /pick, /shuffle, /draw, /mahjongg and /tarot.
// The server drew it; this file only shows it, as a row of slots, one per
// die, coin, pick, shuffled place or card.
//
// The one animation: when a result arrives live, each slot spins like a
// slot-machine reel through values of its own kind and stops on the
// server's value, left to right, in about 1.2 seconds. The animation decides
// nothing. It changes only the text of each slot's face, and its last frame
// is the static drawing, so nothing moves when it ends. With reduced motion
// the result simply appears. More than ten slots get one short flurry.

import { h, icon } from '../dom.js';
import { DECKS, SUITS, RANKS, EXTRAS, GLYPHS, SPREADS, POSITIONS, MEANINGS, SUMMARY, FOR_FUN } from './cardnames.js';

const WORDS = {
  heads: 'Heads', tails: 'Tails', drawn: 'drawn by the server', dropped: 'dropped',
  upright: 'upright', reversed: 'reversed', total: 'total',
};

// cardLabel gives a card's words: its full name, a short form for its slot,
// and its Unicode character where it has one.
export function cardLabel(deck, code) {
  const d = DECKS[deck] || DECKS.standard;
  const glyph = GLYPHS[code] || '';
  if (EXTRAS[code]) return { name: EXTRAS[code].name, short: EXTRAS[code].short, glyph };
  const [suit, rank] = code.split(/-(.*)/s);
  const s = SUITS[suit] || { name: suit, symbol: '' };
  const r = RANKS[rank] || { name: rank, short: rank };
  const name = d.naming === 'suit_rank' ? `${s.name} ${r.name}` : d.naming === 'suit_number' ? `${s.name} ${r.short}` : `${r.name} of ${s.name}`;
  const short = s.symbol ? r.short + s.symbol : `${r.short} ${s.name.split(' ')[0].slice(0, 4)}`;
  return { name, short, glyph };
}

// isRedCard tells whether a standard-deck card is printed in red: hearts,
// diamonds and the red joker. Colour only repeats the suit, which the
// symbol and the card's name already say.
export function isRedCard(code) {
  return /^(hearts|diamonds)-/.test(code) || code === 'joker-red';
}

const cap = (s) => s.charAt(0).toUpperCase() + s.slice(1);
const pickFrom = (list) => list[Math.floor(Math.random() * list.length)];

// A reel: a few values of the slot's kind, ending on the server's.
function reel(pool, text, n = 14) {
  const out = [];
  for (let i = 0; i < n - 1; i++) out.push(pickFrom(pool));
  out.push(text);
  return out;
}

// summaryKeys is a tarot reading's overall energy, by the rule of
// tools/decks-gen/gen.py (testdata/commands/tarot.json, summary_cases).
export function summaryKeys(cards, reversed) {
  const n = cards.length;
  const major = cards.filter((c) => c.startsWith('major-')).length;
  const out = [major * 100 > 60 * n ? 'major_high' : major * 100 < 30 * n ? 'major_low' : 'major_balanced'];
  const r = (reversed || []).filter(Boolean).length;
  if (r * 100 > 60 * n) out.push('reversed_many');
  else if (r * 100 < 20 * n) out.push('reversed_few');
  for (const s of ['cups', 'pentacles', 'wands', 'swords']) {
    const k = cards.filter((c) => c.startsWith(s + '-')).length;
    if (k >= 2 && k * 4 > n) out.push('suit_' + s);
  }
  return out;
}

// slots lists what a result shows, one slot per die, coin, pick, place or
// card: key (what the server drew), text (what the slot shows), reel (what
// it spins through, ending on text), and caption, dropped, still, reversed.
export function slots(c) {
  switch (c.kind) {
    case 'roll':
      return (c.terms || []).flatMap((t, ti) => {
        const sign = t.sign < 0 ? '−' : ti > 0 ? '+' : '';
        if (!t.count) return [{ key: String(t.value), text: String(t.value), reel: [String(t.value), String(t.value)], still: true, sign }];
        const faces = Array.from({ length: Math.min(t.sides, 1000) }, (_, i) => String(i + 1));
        const dropped = new Set(t.dropped || []);
        return (t.rolls || []).map((v, i) => ({ key: String(v), text: String(v), reel: reel(faces, String(v)), dropped: dropped.has(i), sign: i === 0 ? sign : '', caption: i === 0 ? `d${t.sides}` : '' }));
      });
    case 'flip':
      return [{ key: c.outcome, text: WORDS[c.outcome] || c.outcome, reel: reel([WORDS.heads, WORDS.tails], WORDS[c.outcome] || c.outcome) }];
    case 'pick': {
      const v = (c.options || [])[c.picked];
      return [{ key: v, text: v, reel: reel(c.options || [v], v) }];
    }
    case 'shuffle':
      return (c.order || []).map((i, at) => ({ key: c.options[i], text: c.options[i], reel: reel(c.options, c.options[i]), caption: String(at + 1) }));
    case 'draw':
    case 'tarot': {
      const deckCards = (DECKS[c.deck] || DECKS.standard).cards;
      const suited = c.kind === 'draw' && c.deck !== 'mahjong';
      const positions = c.kind === 'tarot' && SPREADS[c.spread] ? SPREADS[c.spread].positions : [];
      return (c.cards || []).map((x, i) => {
        const l = cardLabel(c.deck, x);
        const rev = !!(c.reversed && c.reversed[i]);
        // The reel spins through cards, so each frame keeps its own colour.
        const keys = reel(deckCards, x);
        return { key: x, text: l.short, reel: keys.map((k) => cardLabel(c.deck, k).short), red: suited && isRedCard(x), reelRed: keys.map((k) => suited && isRedCard(k)), caption: l.name, glyph: l.glyph, reversed: c.reversed ? rev : undefined, position: positions[i] };
      });
    }
  }
  return [];
}

// headline is the result's first line.
function headline(c) {
  switch (c.kind) {
    case 'roll': return `Rolled ${c.dice}`;
    case 'flip': return 'Flipped a coin';
    case 'pick': return `Picked from ${(c.options || []).length}`;
    case 'shuffle': return `Shuffled ${(c.options || []).length}`;
    case 'draw': return c.deck === 'mahjong' ? `Drew ${(c.cards || []).length} mahjong ${(c.cards || []).length === 1 ? 'tile' : 'tiles'}` : `Drew ${(c.cards || []).length} from the ${c.deck} deck`;
    case 'tarot': return `Tarot: ${SPREADS[c.spread] ? SPREADS[c.spread].title : c.spread}`;
  }
  return 'A result';
}

// What a screen reader hears: the whole result in words.
export function spokenResult(c) {
  const s = slots(c);
  switch (c.kind) {
    case 'roll': return `${headline(c)}: ${s.map((x) => (x.sign === '−' ? 'minus ' : '') + x.text + (x.dropped ? ' (dropped)' : '')).join(', ')}; total ${c.total}`;
    case 'draw': case 'tarot': return `${headline(c)}: ${s.map((x) => (x.position ? POSITIONS[x.position] + ': ' : '') + x.caption + (x.reversed ? ', reversed' : x.reversed === false ? ', upright' : '')).join('; ')}`;
    default: return `${headline(c)}: ${s.map((x) => x.text).join(', ')}`;
  }
}

// renderChance draws a result as it stands: the static drawing, which is
// also the animation's last frame. onReplay, if given, runs when the result
// is pressed.
export function renderChance(c) {
  const list = slots(c);
  const box = (x) => {
    const width = Math.max(...x.reel.map((t) => [...t].length), 1);
    return h('span', { class: 'slot' + (x.dropped ? ' dropped' : '') + (x.still ? ' still' : '') + (x.reversed ? ' reversed' : ''), title: x.caption || null },
      x.sign ? h('span', { class: 'slot-sign', 'aria-hidden': 'true' }, x.sign) : null,
      h('span', { class: 'slot-face' + (x.red ? ' red' : ''), style: { minWidth: width + 'ch' } }, x.text),
      x.dropped ? h('span', { class: 'slot-note' }, WORDS.dropped) : null);
  };
  const el = h('div', { class: 'chance', role: 'group', 'aria-label': spokenResult(c), dataset: { kind: c.kind } });
  el.append(h('div', { class: 'chance-head' }, icon('dice', null, 'small'), h('span', { class: 'chance-title' }, headline(c)), h('span', { class: 'tag line' }, WORDS.drawn)));
  if (c.kind === 'tarot') {
    const rows = h('ol', { class: 'chance-tarot' });
    list.forEach((x) => {
      const m = MEANINGS[x.key] || {};
      rows.append(h('li', {},
        h('span', { class: 'chance-pos' }, POSITIONS[x.position] || ''),
        box(x),
        h('span', { class: 'chance-card' }, h('strong', {}, cap(x.caption)), ' ', h('span', { class: 'tag line' }, x.reversed ? WORDS.reversed : WORDS.upright)),
        h('span', { class: 'chance-meaning' }, (x.reversed ? m.reversed : m.upright) || '')));
    });
    el.append(rows);
    el.append(h('p', { class: 'chance-summary' }, summaryKeys(c.cards || [], c.reversed).map((k) => SUMMARY[k]).join(' ')));
    el.append(h('p', { class: 'dim small' }, FOR_FUN));
    return el;
  }
  el.append(h('div', { class: 'chance-slots' }, list.map(box),
    c.kind === 'roll' ? h('span', { class: 'chance-total' }, '= ', h('strong', {}, String(c.total))) : null));
  if (c.kind === 'draw' || c.kind === 'shuffle') {
    el.append(h('ol', { class: 'chance-names' }, list.map((x) => h('li', {}, x.glyph ? h('span', { class: 'glyph' + (x.red ? ' red' : ''), 'aria-hidden': 'true' }, x.glyph + ' ') : null, cap(c.kind === 'shuffle' ? x.text : x.caption || x.text), x.reversed ? h('span', { class: 'dim' }, ' · ' + WORDS.reversed) : null))));
  }
  return el;
}

const reducedMotion = () => typeof window !== 'undefined' && window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;

// How long the reels turn.
export const SPIN_MS = 1200;

// spin plays the reels of a drawn result once. It returns a promise that
// settles when every slot shows the server's value again. from is when
// the spin began, for a drawing that carries on a spin already turning.
export function spin(el, c, { total = SPIN_MS, from } = {}) {
  const list = slots(c);
  const faces = [...el.querySelectorAll('.slot-face')];
  const show = (f, x, text, red) => { f.textContent = text; f.classList.toggle('red', !!red); };
  const land = () => faces.forEach((f, i) => { if (list[i]) show(f, list[i], list[i].text, list[i].red); });
  const start = from == null ? performance.now() : from;
  if (reducedMotion() || faces.length !== list.length || !faces.length || performance.now() - start >= total) { land(); return Promise.resolve(); }
  clearTimeout(el._spinTimer);
  el.classList.add('spinning');
  const many = list.length > 10;
  return new Promise((resolve) => {
    const tick = () => {
      const t = performance.now() - start;
      let moving = false;
      list.forEach((x, i) => {
        if (x.still) return;
        // Stops are staggered left to right; past ten, one flurry.
        const stop = many ? total * 0.5 : total * (0.45 + 0.55 * (list.length === 1 ? 1 : i / (list.length - 1)));
        const at = Math.floor(t / 70) % (x.reel.length - 1);
        if (t < stop) { moving = true; show(faces[i], x, x.reel[at], x.reelRed && x.reelRed[at]); } else show(faces[i], x, x.text, x.red);
      });
      if (moving) el._spinTimer = setTimeout(tick, 60);
      else { land(); el.classList.remove('spinning'); resolve(); }
    };
    tick();
  });
}

// spinLive plays the one spin of a result that arrived live. The spin
// belongs to the message, not to one drawing of it: clock (an object kept
// on the message) records under key when it began, and a message drawn
// again while its reels turn carries on from where they were, as when the
// sender's own copy comes back from the server just after the live event.
// Once the spin is over, no drawing turns the reels again.
export function spinLive(el, c, clock, key) {
  const now = performance.now();
  if (clock[key] == null) clock[key] = now;
  const from = clock[key];
  if (now - from >= SPIN_MS) return;
  requestAnimationFrame(() => { if (el.isConnected) spin(el, c, { from }); });
}
