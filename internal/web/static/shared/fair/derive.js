// The derivation `derive: 1` (docs/FAIR.md): a draw's 32-byte output turned
// into whole numbers, picks, shuffles, deals, yes or no and dice, exactly as
// the server's internal/fair does it and the app's lib/core/fair/derive.dart,
// all three held to testdata/fair/derive.json (e2e/fair.mjs). No floating
// point: every number here is a whole number below 2^53, and the words are
// unsigned 32-bit. SHA-256 is the browser's, so the stream is async.

export const DERIVE = 1;

const SPAN = 4294967296; // 2^32
export const MAX_N = 10000;
export const MAX_COUNT = 1000;
export const MIN_INT = -2147483648;
export const MAX_INT = 2147483647;
export const MAX_DICE_ROWS = 100;
export const MAX_DICE_TERMS = 100;
export const MAX_DICE_COUNT = 1000;
export const MAX_DICE_TOTAL = 10000;
export const MAX_DIE_SIDES = 1000000;
export const MAX_EXPLODE = 100;

/** Block k of object i's stream: SHA-256(beta ‖ u32be(i) ‖ u32be(k)). */
export async function block(beta, i, k) {
  const b = new Uint8Array(beta.length + 8);
  b.set(beta, 0);
  const v = new DataView(b.buffer);
  v.setUint32(beta.length, i >>> 0);
  v.setUint32(beta.length + 4, k >>> 0);
  return new Uint8Array(await crypto.subtle.digest('SHA-256', b));
}

/** One chance object's stream: its blocks read as big-endian words, in order. */
export class Stream {
  constructor(beta, i) {
    this.beta = Uint8Array.from(beta);
    this.i = i >>> 0;
    this.k = 0;
    this.buf = null;
    this.at = 32;
    this.used = 0;
  }

  async word() {
    if (this.at === 32) {
      this.buf = new DataView((await block(this.beta, this.i, this.k)).buffer);
      this.k++;
      this.at = 0;
    }
    const w = this.buf.getUint32(this.at);
    this.at += 4;
    this.used++;
    return w;
  }

  /** A whole number from 0 to n-1 by rejection; n = 1 takes no word. */
  async below(n) {
    if (!Number.isInteger(n) || n < 1 || n > SPAN) throw new Error('fair: below out of range');
    if (n === 1) return 0;
    const limit = SPAN - (SPAN % n);
    for (;;) {
      const w = await this.word();
      if (w < limit) return w % n;
    }
  }
}

class PrimitiveError extends Error {}
const bad = (msg) => { throw new PrimitiveError('fair: not a primitive of derive 1: ' + msg); };
const whole = (x) => Number.isInteger(x);

function checkDice(rows) {
  if (!Array.isArray(rows) || rows.length < 1 || rows.length > MAX_DICE_ROWS) bad('rows');
  let total = 0;
  for (const r of rows) {
    const terms = r?.terms;
    if (!Array.isArray(terms) || terms.length < 1 || terms.length > MAX_DICE_TERMS) bad('terms');
    for (const t of terms) {
      const count = t.count ?? 0, sides = t.sides ?? 0, keep = t.keep ?? '', n = t.keep_n ?? 0, success = t.success ?? 0;
      if (t.sign !== 1 && t.sign !== -1) bad('sign');
      if (![count, sides, n, success].every(whole)) bad('whole numbers');
      if (count === 0) {
        if (keep !== '' || t.explode || success !== 0 || sides !== 0) bad('a plain number has no dice');
        continue;
      }
      if (count < 1 || count > MAX_DICE_COUNT || sides < 2 || sides > MAX_DIE_SIDES) bad('dice');
      total += count;
      if (total > MAX_DICE_TOTAL) bad('too many dice');
      if (keep === '') { if (n !== 0) bad('keep_n'); }
      else if (keep === 'kh' || keep === 'kl') { if (n < 1 || n > count) bad('keep'); }
      else if (keep === 'dh' || keep === 'dl') { if (n < 1 || n >= count) bad('drop'); }
      else bad('keep');
      if (success < 0 || success > sides) bad('success');
    }
  }
}

/** Throws unless p is a primitive of derive 1 within its limits. */
export function check(p) {
  const min = p.min ?? 0, max = p.max ?? 0, count = p.count ?? 0, n = p.n ?? 0, k = p.k ?? 0;
  switch (p.op) {
    case 'int': case 'ints':
      if (!whole(min) || !whole(max) || min < MIN_INT || max > MAX_INT || min > max) bad('min and max');
      if (p.op === 'ints' && (!whole(count) || count < 1 || count > MAX_COUNT)) bad('count');
      return;
    case 'pick': {
      if (!whole(n) || n < 1 || n > MAX_N) bad('n');
      if (!whole(k) || k < 1 || (!p.replace && k > n) || (p.replace && k > MAX_COUNT)) bad('k');
      if (p.weights != null) {
        if (!Array.isArray(p.weights) || p.weights.length !== n) bad('weights');
        let total = 0, positive = 0;
        for (const w of p.weights) {
          if (!whole(w) || w < 0 || w > SPAN) bad('a weight');
          total += w;
          if (w > 0) positive++;
        }
        if (total < 1 || total > SPAN) bad('the weights in all');
        if (!p.replace && k > positive) bad('k above the weighted');
      }
      return;
    }
    case 'shuffle':
      if (!whole(n) || n < 1 || n > MAX_N) bad('n');
      return;
    case 'deal': {
      if (!whole(n) || n < 1 || n > MAX_N || !Array.isArray(p.hands) || p.hands.length < 1 || p.hands.length > MAX_N) bad('deal');
      let total = 0;
      for (const h of p.hands) {
        if (!whole(h) || h < 1) bad('a hand');
        total += h;
        if (total > n) bad('the hands');
      }
      return;
    }
    case 'yesno': {
      const num = p.num ?? 0, den = p.den ?? 0;
      if (!whole(num) || !whole(den) || den < 1 || den > SPAN || num < 0 || num > den) bad('num/den');
      return;
    }
    case 'dice':
      checkDice(p.rows);
      return;
    default:
      bad('op');
  }
}

/** Whether p is a primitive of derive 1. */
export function isPrimitive(p) {
  try { check(p); return true; } catch (e) { if (e instanceof PrimitiveError) return false; throw e; }
}

const range = (n) => Array.from({ length: n }, (_, j) => j);

async function pickPlain(s, n, k) {
  const a = range(n);
  const out = [];
  for (let t = 0; t < k; t++) {
    const j = n - 1 - t;
    const r = await s.below(j + 1);
    [a[j], a[r]] = [a[r], a[j]];
    out.push(a[j]);
  }
  return out;
}

async function pick(s, p) {
  if (p.weights == null) {
    if (!p.replace) return pickPlain(s, p.n, p.k);
    const out = [];
    for (let t = 0; t < p.k; t++) out.push(await s.below(p.n));
    return out;
  }
  const w = p.weights.slice();
  const out = [];
  for (let t = 0; t < p.k; t++) {
    const total = w.reduce((x, y) => x + y, 0);
    let r = await s.below(total);
    let j = 0;
    for (; j < w.length; j++) {
      if (r < w[j]) break;
      r -= w[j];
    }
    out.push(j);
    if (!p.replace) w[j] = 0;
  }
  return out;
}

/** The rolls of a term that do not count, by position, in increasing order. */
export function dropped(keep, n, rolls) {
  if (!keep) return [];
  const drop = keep[0] === 'k' ? rolls.length - n : n;
  if (drop <= 0) return [];
  const lowest = keep === 'dl' || keep === 'kh';
  const before = (a, b) => (rolls[a] !== rolls[b] ? (lowest ? rolls[a] < rolls[b] : rolls[a] > rolls[b]) : a > b);
  const pos = range(rolls.length);
  for (let i = 1; i < pos.length; i++) {
    for (let j = i; j > 0 && before(pos[j], pos[j - 1]); j--) [pos[j], pos[j - 1]] = [pos[j - 1], pos[j]];
  }
  return pos.slice(0, drop).sort((a, b) => a - b);
}

/** A rolled term's value: the sum of its counted rolls, or how many of them reached success. */
export function score(t) {
  const skip = new Set(t.dropped ?? []);
  let v = 0;
  (t.rolls ?? []).forEach((r, j) => {
    if (skip.has(j)) return;
    if ((t.success ?? 0) > 0) { if (r >= t.success) v++; } else v += r;
  });
  return v;
}

async function rollRows(s, rows) {
  const out = [];
  for (const r of rows) {
    const row = { terms: [], total: 0 };
    if (r.dice) row.dice = r.dice;
    for (const src of r.terms) {
      const t = { ...src };
      delete t.rolls; delete t.dropped;
      const count = t.count ?? 0;
      if (count > 0) {
        const rolls = [];
        let extra = 0;
        for (let d = 0; d < count; d++) {
          let v = 1 + (await s.below(t.sides));
          rolls.push(v);
          while (t.explode && v === t.sides && extra < MAX_EXPLODE) {
            extra++;
            v = 1 + (await s.below(t.sides));
            rolls.push(v);
          }
        }
        t.rolls = rolls;
        const dr = dropped(t.keep ?? '', t.keep_n ?? 0, rolls);
        if (dr.length) t.dropped = dr;
        t.value = score(t);
      }
      row.terms.push(t);
      row.total += t.sign * (t.value ?? 0);
    }
    out.push(row);
  }
  return out;
}

/** A primitive's result, derived from the stream; throws for a primitive check refuses. */
export async function draw(s, p) {
  check(p);
  switch (p.op) {
    case 'int':
      return { value: (p.min ?? 0) + (await s.below((p.max ?? 0) - (p.min ?? 0) + 1)) };
    case 'ints': {
      const values = [];
      for (let j = 0; j < p.count; j++) values.push((p.min ?? 0) + (await s.below((p.max ?? 0) - (p.min ?? 0) + 1)));
      return { values };
    }
    case 'pick':
      return { picked: await pick(s, p) };
    case 'shuffle': {
      const a = range(p.n);
      for (let j = p.n - 1; j >= 1; j--) {
        const r = await s.below(j + 1);
        [a[j], a[r]] = [a[r], a[j]];
      }
      return { order: a };
    }
    case 'deal': {
      const taken = await pickPlain(s, p.n, p.hands.reduce((x, y) => x + y, 0));
      const dealt = [];
      let at = 0;
      for (const h of p.hands) { dealt.push(taken.slice(at, at + h)); at += h; }
      return { dealt };
    }
    case 'yesno':
      return { yes: (await s.below(p.den)) < (p.num ?? 0) };
    case 'dice':
      return { rows: await rollRows(s, p.rows) };
  }
  return bad('op');
}

/** Rolls rows of dice (dice_roll's rows, rolls left out) from a stream. */
export async function roll(s, rows) {
  return (await draw(s, { op: 'dice', rows })).rows;
}

/**
 * Whether rows rolled from a stream are the rows given: every roll, every
 * dropped die, every value and every total.
 */
export async function sameRoll(s, rows) {
  let again;
  try { again = await roll(s, rows); } catch { return false; }
  if (again.length !== rows.length) return false;
  for (let i = 0; i < rows.length; i++) {
    const a = again[i], b = rows[i];
    if (a.total !== b.total || a.terms.length !== b.terms.length) return false;
    for (let j = 0; j < a.terms.length; j++) {
      const x = a.terms[j], y = b.terms[j];
      const same = (p, q) => JSON.stringify(p ?? []) === JSON.stringify(q ?? []);
      if (x.value !== y.value || !same(x.rolls, y.rolls) || !same(x.dropped, y.dropped)) return false;
    }
  }
  return true;
}
