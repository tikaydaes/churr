// The data of the chance kinds besides a roll (docs/FAIR.md section 5a; the
// schemas CoinFlip, Pick, Shuffle, CardDraw and TarotReading of
// docs/openapi.yaml): a coin flipped (/flip), one option picked (/pick), the
// options shuffled (/shuffle), cards drawn (/draw, /mahjongg) and a tarot
// reading (/tarot). The checks are the server's (internal/model,
// kinds_chance.go), and testdata/types/<kind>.json holds both to the same
// answers. Each check gives a copy of what is drawn, or null, which makes the
// value fall back a step. No DOM.

import { DECKS, SPREADS } from '../../message/cardnames.js';

// The limits of a pick's or shuffle's options (model.MaxChanceOption).
export const MIN_OPTIONS = 2, MAX_OPTIONS = 20, MAX_OPTION = 200;

const whole = (v) => Number.isSafeInteger(v);
const isObject = (d) => !!d && typeof d === 'object' && !Array.isArray(d);

function options(list) {
  if (!Array.isArray(list) || list.length < MIN_OPTIONS || list.length > MAX_OPTIONS) return null;
  for (const o of list) if (typeof o !== 'string' || o === '' || [...o].length > MAX_OPTION || /[\r\n]/.test(o)) return null;
  return [...list];
}

// cardsFrom: whether cards could have been drawn from a deck's cards without
// putting any back, each at most as often as the deck holds it.
function cardsFrom(deck, cards) {
  if (!Array.isArray(cards) || cards.length === 0 || cards.length > deck.length) return false;
  const left = new Map();
  for (const x of deck) left.set(x, (left.get(x) || 0) + 1);
  for (const x of cards) {
    if (typeof x !== 'string' || !left.get(x)) return false;
    left.set(x, left.get(x) - 1);
  }
  return true;
}

const bools = (v, n) => Array.isArray(v) && v.length === n && v.every((b) => typeof b === 'boolean');

export function coinFlipData(d) {
  if (!isObject(d) || (d.outcome !== 'heads' && d.outcome !== 'tails')) return null;
  return { outcome: d.outcome };
}

export function pickData(d) {
  const opts = isObject(d) && options(d.options);
  if (!opts || !whole(d.picked) || d.picked < 0 || d.picked >= opts.length) return null;
  return { options: opts, picked: d.picked };
}

export function shuffleData(d) {
  const opts = isObject(d) && options(d.options);
  if (!opts || !Array.isArray(d.order) || d.order.length !== opts.length) return null;
  if (!d.order.every((i) => whole(i) && i >= 0 && i < opts.length) || new Set(d.order).size !== opts.length) return null;
  return { options: opts, order: [...d.order] };
}

export function cardDrawData(d) {
  if (!isObject(d) || typeof d.deck !== 'string' || !Object.hasOwn(DECKS, d.deck)) return null;
  const deck = DECKS[d.deck];
  if (!cardsFrom(deck.cards, d.cards)) return null;
  if (deck.orientation) {
    if (!bools(d.reversed, d.cards.length)) return null;
    return { deck: d.deck, cards: [...d.cards], reversed: [...d.reversed] };
  }
  if (d.reversed !== undefined && d.reversed !== null) return null;
  return { deck: d.deck, cards: [...d.cards] };
}

export function tarotReadingData(d) {
  if (!isObject(d) || typeof d.spread !== 'string' || !Object.hasOwn(SPREADS, d.spread)) return null;
  const n = SPREADS[d.spread].positions.length;
  if (!Array.isArray(d.cards) || d.cards.length !== n || !bools(d.reversed, n) || !cardsFrom(DECKS.tarot.cards, d.cards)) return null;
  return { spread: d.spread, cards: [...d.cards], reversed: [...d.reversed] };
}

// asChance is a value of one of these kinds in the shape the reels draw
// (shared/message/chance.js), which drew the same results before they were
// kinds.
export function asChance(kind, d) {
  switch (kind) {
    case 'coin_flip': return { kind: 'flip', outcome: d.outcome };
    case 'pick': return { kind: 'pick', options: d.options, picked: d.picked };
    case 'shuffle': return { kind: 'shuffle', options: d.options, order: d.order };
    case 'card_draw': return { kind: 'draw', deck: d.deck, cards: d.cards, reversed: d.reversed };
    case 'tarot_reading': return { kind: 'tarot', deck: 'tarot', spread: d.spread, cards: d.cards, reversed: d.reversed };
    case 'dice_roll': return { kind: 'roll', dice: d.dice, rows: d.rows };
  }
  return null;
}
