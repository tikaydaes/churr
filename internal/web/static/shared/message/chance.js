// How a chance result is drawn (docs/MESSAGE-FORMAT.md section 1.2, rulings
// R67, R68 and R87): /roll, /flip, /pick, /shuffle, /draw, /mahjongg and
// /tarot. The server drew it; this file only shows it, as a row of slots,
// one per die, coin, pick, shuffled place or card.
//
// The one animation plays when a result arrives live, never from history
// and never on a press. Dice, coins, picks and places spin like
// slot-machine reels through values of their own kind and stop on the
// server's value, left to right, in 1.2 seconds; a roll's total and its
// dropped dice show only once every reel has stopped. Cards and tiles lie
// face down and are turned over one after another (revealPlan); a tarot
// reading's words for each card appear as it is turned, and the summary
// after the last. The animation decides nothing: its last frame is the
// static drawing, so nothing moves when it ends. With reduced motion the
// result simply appears, face up, words and all.
//
// A card's name is not listed under it: it pops up over the card on
// hover, on keyboard focus and on a tap, and each card carries it as its
// label for screen readers.

import { h, icon } from '../dom.js';
import { dress, paint, paintFace } from './chanceart.js';
import { DECKS, SUITS, RANKS, EXTRAS, GLYPHS, SPREADS, POSITIONS, MEANINGS, SUMMARY } from './cardnames.js';

const WORDS = {
  heads: 'Heads', tails: 'Tails', dropped: 'dropped',
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

// A coin's reel turns it over and over: the two faces by turns, ending on
// the one it landed on. pair is [heads, tails] in whatever form the slot
// keeps (its words, or a d2's 1 and 2).
export function coinReel(pair, landed, n = 14) {
  const end = pair.indexOf(landed) < 0 ? 0 : pair.indexOf(landed);
  return Array.from({ length: n }, (_, i) => pair[(end + n - 1 - i) % 2]);
}

// coinWords is how a d2's value reads: "heads (1)", "tails (2)".
const coinWords = (v) => `${v === '2' ? 'tails' : 'heads'} (${v})`;

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
        // A two-sided die is a coin: heads is 1, tails 2.
        if (t.sides === 2) {
          return (t.rolls || []).map((v, i) => {
            const keys = coinReel(['1', '2'], String(v));
            return { key: String(v), text: coinWords(String(v)), reel: keys.map(coinWords), reelKeys: keys, sides: 2, dropped: dropped.has(i), sign: i === 0 ? sign : '', caption: i === 0 ? 'd2' : '' };
          });
        }
        return (t.rolls || []).map((v, i) => ({ key: String(v), text: String(v), reel: reel(faces, String(v)), sides: t.sides, dropped: dropped.has(i), sign: i === 0 ? sign : '', caption: i === 0 ? `d${t.sides}` : '' }));
      });
    case 'flip': {
      const keys = coinReel(['heads', 'tails'], c.outcome);
      return [{ key: c.outcome, text: WORDS[c.outcome] || c.outcome, reel: keys.map((k) => WORDS[k]), reelKeys: keys }];
    }
    case 'pick': {
      const v = (c.options || [])[c.picked];
      return [{ key: v, text: v, reel: reel(c.options || [v], v) }];
    }
    case 'shuffle':
      return (c.order || []).map((i, at) => ({ key: c.options[i], text: c.options[i], reel: reel(c.options, c.options[i]), caption: String(at + 1) }));
    case 'draw':
    case 'tarot': {
      const suited = c.kind === 'draw' && c.deck !== 'mahjong';
      const positions = c.kind === 'tarot' && SPREADS[c.spread] ? SPREADS[c.spread].positions : [];
      return (c.cards || []).map((x, i) => {
        const l = cardLabel(c.deck, x);
        const rev = !!(c.reversed && c.reversed[i]);
        // A card is turned over, not spun: it has no reel.
        return { key: x, text: l.short, reel: [l.short], card: true, red: suited && isRedCard(x), caption: l.name, glyph: l.glyph, reversed: c.reversed ? rev : undefined, position: positions[i] };
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

// cardName is what a card's popup says and its label: its name, and
// "reversed" when it is.
export const cardName = (x) => cap(x.caption) + (x.reversed ? ' · ' + WORDS.reversed : '');

// renderChance draws a result as it stands: the static drawing, which is
// also the animation's last frame. Pressing it does nothing, except that
// a card or tile pressed shows its name.
export function renderChance(c) {
  const list = slots(c);
  const box = (x, i) => {
    const width = Math.max(...x.reel.map((t) => [...t].length), 1);
    const face = h('span', { class: 'slot-face' + (x.red ? ' red' : ''), style: { minWidth: width + 'ch' }, dataset: { minWidth: width + 'ch' } }, x.text);
    if (x.card) {
      // The name pops up (cardTip); the card is labelled with it.
      return h('span', { class: 'slot card' + (x.reversed ? ' reversed' : ''), tabIndex: 0, role: 'img', 'aria-label': cardName(x), dataset: { slot: String(i) } },
        face, h('span', { class: 'chance-tip', 'aria-hidden': 'true' }, cardName(x)));
    }
    return h('span', { class: 'slot' + (x.dropped ? ' dropped' : '') + (x.still ? ' still' : ''), title: x.caption || null },
      x.sign ? h('span', { class: 'slot-sign', 'aria-hidden': 'true' }, x.sign) : null,
      face,
      x.dropped ? h('span', { class: 'slot-note' }, WORDS.dropped) : null);
  };
  const el = h('div', { class: 'chance', role: 'group', 'aria-label': spokenResult(c), dataset: { kind: c.kind } });
  el.append(h('div', { class: 'chance-head' }, icon('dice', null, 'small'), h('span', { class: 'chance-title' }, headline(c))));
  if (c.kind === 'tarot') {
    // The words of each card (.chance-say, by its slot) and the summary
    // (.chance-after) are held back while the cards are turned over.
    const rows = h('ol', { class: 'chance-tarot' });
    list.forEach((x, i) => {
      const m = MEANINGS[x.key] || {};
      const say = { class: 'chance-say', dataset: { slot: String(i) } };
      rows.append(h('li', {},
        h('span', { ...say, class: 'chance-pos chance-say' }, POSITIONS[x.position] || ''),
        box(x, i),
        h('span', { ...say, class: 'chance-card chance-say' }, h('strong', {}, cap(x.caption)), ' ', h('span', { class: 'tag line' }, x.reversed ? WORDS.reversed : WORDS.upright)),
        h('span', { ...say, class: 'chance-meaning chance-say' }, (x.reversed ? m.reversed : m.upright) || '')));
    });
    el.append(rows);
    el.append(h('p', { class: 'chance-summary chance-after' }, summaryKeys(c.cards || [], c.reversed).map((k) => SUMMARY[k]).join(' ')));
    return dressed(tips(el), c, list);
  }
  el.append(h('div', { class: 'chance-slots' }, list.map(box),
    c.kind === 'roll' ? h('span', { class: 'chance-total' }, '= ', h('strong', {}, String(c.total))) : null));
  if (c.kind === 'shuffle') {
    el.append(h('ol', { class: 'chance-names' }, list.map((x) => h('li', {}, cap(x.text)))));
  }
  return dressed(c.kind === 'draw' ? tips(el) : el, c, list);
}

// tips makes each card's name pop up: over the card while the mouse is on
// it or it has the keyboard's focus, and on a tap or click until the next
// one, Escape, or a press elsewhere. The popup is the card's
// .chance-tip, shown with the class "open" (and in the top layer where the
// browser has popovers, so the panel's cut corner does not clip it).
function tips(el) {
  const show = (slot, on) => {
    const tip = slot.querySelector('.chance-tip');
    if (!tip || tip.classList.contains('open') === on) return;
    tip.classList.toggle('open', on);
    slot.classList.toggle('tip-open', on);
    if (tip.showPopover) {
      try {
        if (on) {
          tip.showPopover();
          const r = slot.getBoundingClientRect(), t = tip.getBoundingClientRect();
          const left = Math.max(4, Math.min(window.innerWidth - t.width - 4, r.left + r.width / 2 - t.width / 2));
          const top = r.top - t.height - 6 < 4 ? r.bottom + 6 : r.top - t.height - 6;
          Object.assign(tip.style, { left: left + 'px', top: top + 'px' });
        } else tip.hidePopover();
      } catch { /* not in the document */ }
    }
  };
  const all = () => [...el.querySelectorAll('.slot.card')];
  for (const slot of all()) {
    const tip = slot.querySelector('.chance-tip');
    if (tip && typeof tip.showPopover === 'function') tip.setAttribute('popover', 'manual');
    slot.addEventListener('pointerenter', (e) => { if (e.pointerType === 'mouse') show(slot, true); });
    slot.addEventListener('pointerleave', (e) => { if (e.pointerType === 'mouse' && !slot._pinned) show(slot, false); });
    slot.addEventListener('focus', () => show(slot, true));
    slot.addEventListener('blur', () => { slot._pinned = false; show(slot, false); });
    slot.addEventListener('keydown', (e) => { if (e.key === 'Escape') { slot._pinned = false; show(slot, false); } });
  }
  el.addEventListener('click', (e) => {
    const slot = e.target && e.target.closest ? e.target.closest('.slot.card') : null;
    for (const s of all()) if (s !== slot) { s._pinned = false; show(s, false); }
    if (!slot) return;
    slot._pinned = !slot._pinned;
    show(slot, slot._pinned);
  });
  return el;
}

// dressed puts the artwork on a drawing's slots once its sheet is here;
// until then, and without one, the slots show their text.
function dressed(el, c, list) {
  dress(c, [...el.querySelectorAll('.slot-face')], list);
  return el;
}

const reducedMotion = () => typeof window !== 'undefined' && window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;

// How long the reels turn.
export const SPIN_MS = 1200;

// How cards and tiles are turned over (revealPlan): the face-down pause
// before the first, the turn itself, the time from one card to the next
// (less for many cards), and in a tarot reading the pause after the last
// card's words before the summary.
export const REVEAL = { lead: 200, flip: 280, step: 260, steps: 1800, tarotStep: 700, tarotSteps: 3000, summary: 350, end: 150 };

// revealPlan says when each card of a drawn result starts to turn over
// (flips, from the animation's start, in ms), when its words appear
// (says: as it lands face up), when the summary appears (after) and when
// it is all over (total). /draw and /mahjongg turn 3 cards in 1.2 s, 5 in
// 1.7 s and any number in at most about 2.4 s; a three-card tarot reading takes
// 2.4 s and a Celtic cross 3.7 s.
export function revealPlan(c) {
  const n = (c.cards || []).length;
  const tarot = c.kind === 'tarot';
  const step = n <= 1 ? 0 : tarot ? Math.min(REVEAL.tarotStep, REVEAL.tarotSteps / n) : Math.min(REVEAL.step, REVEAL.steps / n);
  const flips = Array.from({ length: n }, (_, i) => Math.round(REVEAL.lead + i * step));
  const says = flips.map((t) => t + REVEAL.flip);
  const last = n ? says[n - 1] : REVEAL.lead;
  const after = tarot ? last + REVEAL.summary : last;
  return { flips, says, after, total: after + REVEAL.end };
}

// duration is how long a result's animation lasts.
export function duration(c) {
  return c.kind === 'draw' || c.kind === 'tarot' ? revealPlan(c).total : SPIN_MS;
}

// settle shows a result as it ends: face up, every word, the total.
function settle(el) {
  el.classList.remove('spinning');
  for (const s of el.querySelectorAll('.slot.card.down')) s.classList.remove('down', 'flipping');
  for (const f of el.querySelectorAll('.slot-face')) if (f._down) { f._down = false; paintFace(f); }
  for (const w of el.querySelectorAll('.chance-say, .chance-after')) w.classList.remove('held');
  for (const w of el.querySelectorAll('.chance-total, .slot-note')) w.hidden = false;
  for (const s of el.querySelectorAll('.slot')) if (s._dropped) { s.classList.add('dropped'); s._dropped = false; }
}

// reveal turns a drawn result's cards over once, one after another, by
// revealPlan; each card turns over halfway through its flip, a reversed
// one landing upside down.
function reveal(el, c, from) {
  const plan = revealPlan(c);
  const start = from == null ? performance.now() : from;
  if (reducedMotion() || performance.now() - start >= plan.total) { settle(el); return Promise.resolve(); }
  const cards = [...el.querySelectorAll('.slot.card')];
  const say = [...el.querySelectorAll('.chance-say')];
  const after = [...el.querySelectorAll('.chance-after')];
  clearTimeout(el._spinTimer);
  el.classList.add('spinning');
  return new Promise((resolve) => {
    const tick = () => {
      const t = performance.now() - start;
      if (t >= plan.total) { settle(el); resolve(); return; }
      cards.forEach((slot, i) => {
        const at = plan.flips[i] == null ? 0 : plan.flips[i];
        const down = t < at + REVEAL.flip / 2;
        slot.classList.toggle('flipping', t >= at && t < at + REVEAL.flip);
        slot.classList.toggle('down', down);
        const f = slot.querySelector('.slot-face');
        if (f && !!f._down !== down) { f._down = down; paintFace(f); }
      });
      for (const w of say) w.classList.toggle('held', t < (plan.says[Number(w.dataset.slot)] || 0));
      for (const w of after) w.classList.toggle('held', t < plan.after);
      el._spinTimer = setTimeout(tick, 30);
    };
    tick();
  });
}

// spin plays the animation of a drawn result once: the reels, or the
// cards turned over. It returns a promise that settles when every slot
// shows the server's value again. from is when it began, for a drawing
// that carries on an animation already playing.
export function spin(el, c, { total = SPIN_MS, from } = {}) {
  if (c.kind === 'draw' || c.kind === 'tarot') return reveal(el, c, from);
  const list = slots(c);
  const faces = [...el.querySelectorAll('.slot-face')];
  // landed says the frame is the slot's last, the server's value.
  const show = (f, x, text, red, key, landed) => {
    f.textContent = text;
    f.classList.toggle('red', !!red);
    if (f._art) paint(f, f._art, key, text, landed);
  };
  const land = () => { faces.forEach((f, i) => { if (list[i]) show(f, list[i], list[i].text, list[i].red, list[i].key, true); }); settle(el); };
  const start = from == null ? performance.now() : from;
  if (reducedMotion() || faces.length !== list.length || !faces.length || performance.now() - start >= total) { land(); return Promise.resolve(); }
  clearTimeout(el._spinTimer);
  el.classList.add('spinning');
  // The total and the dropped dice wait for the last reel to stop.
  for (const w of el.querySelectorAll('.chance-total, .slot-note')) w.hidden = true;
  for (const s of el.querySelectorAll('.slot.dropped')) { s._dropped = true; s.classList.remove('dropped'); }
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
        if (t < stop) { moving = true; show(faces[i], x, x.reel[at], x.reelRed && x.reelRed[at], (x.reelKeys || x.reel)[at], false); } else show(faces[i], x, x.text, x.red, x.key, true);
      });
      if (moving) el._spinTimer = setTimeout(tick, 60);
      else { land(); resolve(); }
    };
    tick();
  });
}

// spinLive plays the one animation of a result that arrived live. It
// belongs to the message, not to one drawing of it: clock (an object kept
// on the message) records under key when it began, and a message drawn
// again while it plays carries on from where it was, as when the
// sender's own copy comes back from the server just after the live event.
// Once it is over, no drawing plays it again. Its first frame is set at
// once, so the total or a card's face is never seen before it starts.
export function spinLive(el, c, clock, key) {
  const now = performance.now();
  if (clock[key] == null) clock[key] = now;
  const from = clock[key];
  if (now - from >= duration(c)) return;
  spin(el, c, { from });
}
