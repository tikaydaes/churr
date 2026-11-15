// A page for judging the chance reels by eye (ruling R67), served only by
// a server started with -dev, at /dev/chance. Each button makes a result in
// this browser, in the shape the server stores, and plays it as a message
// arriving live would. Nothing here is drawn by the server: in a
// conversation, every result is.

import { h, button, fill } from '/shared/dom.js';
import { renderChance, spin } from '/shared/message/chance.js';
import { DECKS, SPREADS } from '/shared/message/cardnames.js';

const rnd = (n) => Math.floor(Math.random() * n);
function draw(deck, n) {
  const cards = [...DECKS[deck].cards];
  for (let i = 0; i < n; i++) { const j = i + rnd(cards.length - i); [cards[i], cards[j]] = [cards[j], cards[i]]; }
  return cards.slice(0, n);
}
function roll(count, sides, keep) {
  const rolls = Array.from({ length: count }, () => rnd(sides) + 1);
  const order = rolls.map((v, i) => [v, i]).sort((a, b) => b[0] - a[0] || a[1] - b[1]);
  const dropped = keep ? order.slice(keep).map((x) => x[1]).sort((a, b) => a - b) : [];
  const value = rolls.reduce((s, v, i) => s + (dropped.includes(i) ? 0 : v), 0);
  return { sign: 1, count, sides, keep: keep ? 'kh' : undefined, keep_n: keep || undefined, rolls, dropped, value };
}
const opts = ['tea', 'coffee', 'cocoa', 'water', 'juice'];
const MAKERS = {
  '/roll 2d6+3': () => { const t = roll(2, 6); return { kind: 'roll', dice: '2d6+3', terms: [t, { sign: 1, value: 3 }], total: t.value + 3 }; },
  '/roll 4d6kh3': () => { const t = roll(4, 6, 3); return { kind: 'roll', dice: '4d6kh3', terms: [t], total: t.value }; },
  '/roll 20d6 (a flurry)': () => { const t = roll(20, 6); return { kind: 'roll', dice: '20d6', terms: [t], total: t.value }; },
  '/flip': () => ({ kind: 'flip', outcome: rnd(2) ? 'tails' : 'heads' }),
  '/pick tea | coffee | cocoa | water | juice': () => ({ kind: 'pick', options: opts, picked: rnd(opts.length) }),
  '/shuffle tea | coffee | cocoa | water | juice': () => { const order = [0, 1, 2, 3, 4]; for (let i = 4; i > 0; i--) { const j = rnd(i + 1); [order[i], order[j]] = [order[j], order[i]]; } return { kind: 'shuffle', options: opts, order }; },
  '/draw 5': () => ({ kind: 'draw', deck: 'standard', cards: draw('standard', 5) }),
  '/draw 3 hanafuda': () => ({ kind: 'draw', deck: 'hanafuda', cards: draw('hanafuda', 3) }),
  '/mahjongg 14': () => ({ kind: 'draw', deck: 'mahjong', cards: draw('mahjong', 14) }),
  '/tarot': () => ({ kind: 'tarot', deck: 'tarot', spread: 'three', cards: draw('tarot', 3), reversed: [0, 0, 0].map(() => !!rnd(2)) }),
  '/tarot celtic': () => ({ kind: 'tarot', deck: 'tarot', spread: 'celtic', cards: draw('tarot', SPREADS.celtic.positions.length), reversed: SPREADS.celtic.positions.map(() => !!rnd(2)) }),
};

const root = document.getElementById('demo');
root.append(h('h1', {}, 'Chance reels'), h('p', { class: 'dim' }, 'For judging the animation only: these results are made in your browser. In a conversation every result is drawn by the server. Press a result to spin it again; with reduced motion on, it simply appears.'));
for (const [label, make] of Object.entries(MAKERS)) {
  const slot = h('div', {});
  const play = () => { const c = make(); const el = renderChance(c); el.addEventListener('click', () => spin(el, c)); fill(slot, el); spin(el, c); };
  root.append(h('section', { class: 'stack tight' }, h('div', { class: 'row' }, button('Roll', play, 'primary'), h('code', {}, label)), slot));
}
