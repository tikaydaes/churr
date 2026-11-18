// The built-in chance kinds under derive 1 (docs/FAIR.md section 5a), as the
// server's internal/fair (kinds.go) and the app's lib/core/fair/kinds.dart
// write them, all three held to testdata/fair/kinds.json (e2e/fair.mjs):
// each kind a fixed composition of the primitives, read from what its data
// says was asked, so the outcome its data shows is compared with what the
// primitives give from the value's share of the draw. No DOM.

import { DECKS, SPREADS } from '../message/cardnames.js';
import { Stream, draw, sameRoll } from './derive.js';

const coin = () => [{ op: 'yesno', num: 1, den: 2 }];
const cards = (size, count, oriented) => {
  const out = [{ op: 'pick', n: size, k: count }];
  if (oriented) out.push({ op: 'ints', min: 0, max: 1, count });
  return out;
};

/**
 * The primitives a value of a built-in chance kind asks of its stream, in
 * order, from its data (checked already), or null for a kind with no
 * composition. A roll's is its rows as one dice primitive.
 */
export function kindAsks(kind, d) {
  switch (kind) {
    case 'coin_flip': return coin();
    case 'pick': return [{ op: 'pick', n: d.options.length, k: 1 }];
    case 'shuffle': return [{ op: 'shuffle', n: d.options.length }];
    case 'card_draw': return cards(DECKS[d.deck].cards.length, d.cards.length, !!DECKS[d.deck].orientation);
    case 'tarot_reading': return cards(DECKS.tarot.cards.length, d.cards.length, true);
    case 'dice_roll': return [{ op: 'dice', rows: d.rows.map((r) => ({ dice: r.dice, terms: r.terms.map((t) => (t.count ? { sign: t.sign, count: t.count, sides: t.sides, keep: t.keep, keep_n: t.keep_n } : { sign: t.sign, value: t.value })) })) }];
  }
  return null;
}

/**
 * The outcome a value's data claims, as the results of its primitives (for
 * the Validate result view and its export, docs/FAIR.md section 10). A card
 * is claimed by what it is, so where a deck holds a card more than once any
 * of its positions is the same claim: the positions the draw gives are taken
 * when they hold the cards shown, and otherwise the first free position of
 * each card, which then does not re-derive.
 */
export async function kindClaims(kind, d, beta, i) {
  switch (kind) {
    case 'coin_flip': return [{ yes: d.outcome === 'heads' }];
    case 'pick': return [{ picked: [d.picked] }];
    case 'shuffle': return [{ order: d.order }];
    case 'dice_roll': return [{ rows: d.rows }];
    case 'card_draw': case 'tarot_reading': {
      const deck = (kind === 'tarot_reading' ? DECKS.tarot : DECKS[d.deck]).cards;
      const asks = kindAsks(kind, d);
      let picked = null;
      try { picked = (await draw(new Stream(beta, i), asks[0])).picked; } catch { /* below */ }
      if (!picked || !sameCards(deck, picked, d.cards)) {
        const used = new Set();
        picked = d.cards.map((c) => { const p = deck.findIndex((x, j) => x === c && !used.has(j)); used.add(p); return p; });
      }
      const out = [{ picked }];
      if (asks.length > 1) out.push({ values: (d.reversed || []).map((r) => (r ? 1 : 0)) });
      return out;
    }
  }
  return null;
}

/** Whether the deck's card at each position picked is the card shown. */
const sameCards = (deck, picked, shown) => picked.length === shown.length && picked.every((p, i) => deck[p] === shown[i]);

/**
 * Whether a value of a built-in chance kind is what derive 1 gives from
 * object i of a draw whose output is beta: true, false, or null for a kind
 * with no composition. The data is the checked copy (shared/types).
 */
export async function rederive(kind, d, beta, i) {
  if (kind === 'dice_roll') return sameRoll(new Stream(beta, i), d.rows);
  const asks = kindAsks(kind, d);
  if (!asks) return null;
  const s = new Stream(beta, i);
  const rs = [];
  try { for (const p of asks) rs.push(await draw(s, p)); } catch { return false; }
  switch (kind) {
    case 'coin_flip': return rs[0].yes === (d.outcome === 'heads');
    case 'pick': return rs[0].picked[0] === d.picked;
    case 'shuffle': return JSON.stringify(rs[0].order) === JSON.stringify(d.order);
    case 'card_draw': case 'tarot_reading': {
      const deck = kind === 'tarot_reading' ? DECKS.tarot : DECKS[d.deck];
      if (!sameCards(deck.cards, rs[0].picked, d.cards)) return false;
      if (rs.length < 2) return !d.reversed;
      return JSON.stringify(rs[1].values.map((v) => v === 1)) === JSON.stringify(d.reversed);
    }
  }
  return null;
}

/** The kinds with a composition. */
export const KINDS = ['dice_roll', 'coin_flip', 'pick', 'shuffle', 'card_draw', 'tarot_reading'];

/** The spread a tarot reading names, for its words. */
export const spreadOf = (d) => SPREADS[d.spread];
