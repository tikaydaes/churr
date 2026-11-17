// Checks, without a browser, how the web chat plays a chance result that
// arrives live (ruling R87): a roll's total and dropped dice only once the
// reels have stopped; cards and tiles face down, then turned over one
// after another; a tarot reading's words with each card and the summary
// after the last; no list of names, each card's name popping up on hover,
// focus or a tap, and kept as its label; nothing replayed on a press;
// with reduced motion, everything at once. It runs on a small stand-in
// for the page's elements and a clock of its own.
//
//   node e2e/chancereveal.mjs

// --- a stand-in document ---
class Node_ {
  constructor() { this.parentNode = null; }
  remove() { if (this.parentNode) this.parentNode.kids = this.parentNode.kids.filter((k) => k !== this); this.parentNode = null; }
}
class Text_ extends Node_ { constructor(t) { super(); this.data = String(t); } get textContent() { return this.data; } }
class El extends Node_ {
  constructor(tag) {
    super();
    this.tagName = tag.toUpperCase();
    this.kids = [];
    this.attrs = {};
    this.style = {};
    this.dataset = {};
    this.listeners = {};
    this.hidden = false;
    const cls = this._cls = new Set();
    this.classList = {
      add: (...c) => c.forEach((x) => cls.add(x)), remove: (...c) => c.forEach((x) => cls.delete(x)),
      contains: (c) => cls.has(c), toggle: (c, on) => { const v = on === undefined ? !cls.has(c) : on; if (v) cls.add(c); else cls.delete(c); return v; },
    };
  }
  get className() { return [...this._cls].join(' '); }
  set className(v) { this._cls.clear(); String(v).split(/\s+/).filter(Boolean).forEach((c) => this._cls.add(c)); }
  setAttribute(k, v) { this.attrs[k] = String(v); }
  getAttribute(k) { return k in this.attrs ? this.attrs[k] : null; }
  removeAttribute(k) { delete this.attrs[k]; }
  addEventListener(type, fn) { (this.listeners[type] = this.listeners[type] || []).push(fn); }
  dispatch(type, ev = {}) {
    const e = { type, target: this, preventDefault() {}, ...ev };
    for (let n = this; n; n = n.parentNode) for (const fn of (n.listeners && n.listeners[type]) || []) fn(e);
  }
  get firstChild() { return this.kids[0] || null; }
  append(...ns) { for (const n of ns) { const k = typeof n === 'object' ? n : new Text_(n); k.remove(); k.parentNode = this; this.kids.push(k); } }
  prepend(n) { n.remove(); n.parentNode = this; this.kids.unshift(n); }
  replaceChildren(...ns) { this.kids.forEach((k) => { k.parentNode = null; }); this.kids = []; this.append(...ns); }
  get textContent() { return this.kids.map((k) => k.textContent).join(''); }
  set textContent(t) { this.replaceChildren(String(t)); }
  matches(sel) { return sel.split(',').some((one) => simple(this, one.trim())); }
  closest(sel) { for (let n = this; n instanceof El; n = n.parentNode) if (n.matches(sel)) return n; return null; }
  querySelectorAll(sel) { const out = []; const walk = (n) => { for (const k of n.kids) if (k instanceof El) { if (k.matches(sel)) out.push(k); walk(k); } }; walk(this); return out; }
  querySelector(sel) { return this.querySelectorAll(sel)[0] || null; }
}
// A selector of a tag and classes, such as "span.slot.card".
function simple(el, sel) {
  const [tag, ...classes] = sel.split('.');
  return (!tag || el.tagName === tag.toUpperCase()) && classes.every((c) => el._cls.has(c));
}
globalThis.document = { createElement: (t) => new El(t), createElementNS: (_, t) => new El(t) };

// --- a clock of our own ---
let now = 0;
let timers = [];
performance.now = () => now;
globalThis.setTimeout = (fn, ms) => { const t = { fn, at: now + (ms || 0) }; timers.push(t); return t; };
globalThis.clearTimeout = (t) => { timers = timers.filter((x) => x !== t); };
globalThis.requestAnimationFrame = (fn) => setTimeout(fn, 16);
// advance moves the clock to ms after it is now, running what falls due.
function advance(ms) {
  const end = now + ms;
  for (;;) {
    timers.sort((a, b) => a.at - b.at);
    const t = timers[0];
    if (!t || t.at > end) break;
    timers.shift();
    now = t.at;
    t.fn();
  }
  now = end;
}

const { renderChance, spinLive, spin, revealPlan, duration, SPIN_MS, REVEAL } = await import('../internal/web/static/shared/message/chance.js');
const { setSheetsForTest } = await import('../internal/web/static/shared/message/chanceart.js');
const { diceControl } = await import('../internal/web/static/shared/objects/dice.js');
setSheetsForTest([], {});

let failures = 0, checks = 0;
function check(name, ok, detail) {
  checks++;
  if (!ok) { failures++; console.log(`FAIL chancereveal: ${name}${detail ? '\n       ' + detail : ''}`); }
}
const live = (c) => { const el = renderChance(c); spinLive(el, c, {}, 'chance'); return el; };
const shown = (el) => !el.hidden;

// 1. A roll's total and its dropped dice wait for the reels.
{
  const roll = { kind: 'roll', dice: '4d6kh3+2', terms: [{ sign: 1, count: 4, sides: 6, rolls: [5, 2, 6, 3], dropped: [1], value: 14 }, { sign: 1, value: 2 }], total: 16 };
  const el = live(roll);
  const total = el.querySelector('.chance-total');
  check('a live roll is rolling from its first frame', el.classList.contains('spinning'));
  check('the total is hidden while the dice roll', !shown(total));
  check('the dropped note is hidden while the dice roll', el.querySelectorAll('.slot-note').every((n) => !shown(n)));
  check('no die is struck out while the dice roll', el.querySelectorAll('.slot.dropped').length === 0);
  // The fourth die (of five slots, the last a plain 2) stops at 1035 ms.
  advance(990);
  check('still hidden just before the last die stops', !shown(total) && el.classList.contains('spinning'));
  advance(SPIN_MS - 990);
  check('once the dice stop: the total', shown(total) && total.textContent === '= 16' && !el.classList.contains('spinning'));
  check('once the dice stop: the dropped die, struck out and noted', el.querySelectorAll('.slot.dropped').length === 1 && el.querySelectorAll('.slot-note').every(shown));
  check('the faces landed on the server\'s dice', el.querySelectorAll('.slot-face').map((f) => f.textContent).join() === '5,2,6,3,2');
  // Drawn from history, it is all there at once.
  const old = renderChance(roll);
  check('from history the total is there and nothing rolls', shown(old.querySelector('.chance-total')) && !old.classList.contains('spinning'));
  // Pressed, it does nothing.
  advance(5000);
  el.dispatch('click');
  check('a press does not roll it again', !el.classList.contains('spinning') && timers.length === 0);
  check('the panel is not a button and says nothing of spinning again', el.getAttribute('title') === null && el.tabIndex === undefined);
}

// Inline dice: the same.
{
  const e = { type: 'dice', at: 9, value: '2d6+3', terms: [{ sign: 1, count: 2, sides: 6, rolls: [4, 1], value: 5 }, { sign: 1, value: 3 }], total: 8 };
  const clock = {};
  const el = diceControl(e, '[[2d6+3]]', { live: () => clock });
  const total = el.querySelector('.chance-total');
  check('inline dice hide their total while they roll', el.classList.contains('spinning') && !shown(total));
  advance(SPIN_MS + 100);
  check('inline dice show their total once stopped', shown(total) && total.textContent.includes('8') && !el.classList.contains('spinning'));
  el.dispatch('click');
  check('inline dice pressed do not roll again', !el.classList.contains('spinning'));
}

// 3 and 5. /draw: face down, then turned over in order; no names listed.
{
  const draw = { kind: 'draw', deck: 'standard', cards: ['spades-ace', 'hearts-queen', 'clubs-7'] };
  const el = live(draw);
  const cards = el.querySelectorAll('.slot.card');
  const plan = revealPlan(draw);
  check('no list of names under the cards', el.querySelectorAll('.chance-names').length === 0 && el.querySelectorAll('ol').length === 0);
  check('each card is labelled with its name', cards.map((s) => s.getAttribute('aria-label')).join('|') === 'Ace of spades|Queen of hearts|Seven of clubs', cards.map((s) => s.getAttribute('aria-label')).join('|'));
  check('each card can take the keyboard\'s focus', cards.every((s) => (s.tabIndex === 0 || s.getAttribute('tabIndex') === '0') && s.getAttribute('role') === 'img'));
  check('live, every card starts face down', cards.every((s) => s.classList.contains('down')) && cards.every((s) => s.querySelector('.slot-face')._down));
  check('the plan turns them one after another', plan.flips.every((t, i) => i === 0 || t > plan.flips[i - 1]) && plan.flips[0] >= REVEAL.lead);
  const order = [];
  let last = cards.map(() => true);
  for (let t = 0; t <= plan.total + 50; t += 10) {
    advance(10);
    cards.forEach((s, i) => { const d = s.classList.contains('down'); if (last[i] && !d) order.push(i); last[i] = d; });
    if (t === plan.flips[1] + 20) check('mid-reveal: the first is face up, the last still down', !cards[0].classList.contains('down') && cards[2].classList.contains('down'));
    if (t === plan.flips[0] + 50) check('a card turning over is flipping', cards[0].classList.contains('flipping'));
  }
  check('they were turned over in order, each once', order.join() === '0,1,2', order.join());
  check('at the end all are face up and nothing plays', cards.every((s) => !s.classList.contains('down') && !s.classList.contains('flipping')) && !el.classList.contains('spinning'));
  check(`/draw of 3 is over in ${plan.total} ms`, plan.total < 1500);
  check('a big draw stays short', revealPlan({ kind: 'draw', cards: Array(14).fill('dots-1') }).total <= 2400 && revealPlan({ kind: 'draw', cards: Array(52).fill('spades-ace') }).total <= 2400);
  check('the duration of a draw is its plan', duration(draw) === plan.total);

  // 3 and 4. The name pops up on hover, on focus and on a tap; a press
  // never turns the cards again.
  const tip = (i) => cards[i].querySelector('.chance-tip');
  check('each card has its name to pop up, hidden from screen readers (the label says it)', tip(1).textContent === 'Queen of hearts' && tip(1).getAttribute('aria-hidden') === 'true' && !tip(1).classList.contains('open'));
  cards[1].dispatch('pointerenter', { pointerType: 'mouse' });
  check('hover shows the name', tip(1).classList.contains('open'));
  cards[1].dispatch('pointerleave', { pointerType: 'mouse' });
  check('leaving hides it', !tip(1).classList.contains('open'));
  cards[0].dispatch('pointerenter', { pointerType: 'touch' });
  check('a touch does not count as hover', !tip(0).classList.contains('open'));
  cards[0].dispatch('click');
  check('a tap shows the name', tip(0).classList.contains('open'));
  check('a tap does not turn the cards again', !el.classList.contains('spinning') && cards.every((s) => !s.classList.contains('down')) && timers.length === 0);
  cards[2].dispatch('click');
  check('a tap on another card moves the name to it', !tip(0).classList.contains('open') && tip(2).classList.contains('open'));
  cards[2].dispatch('click');
  check('a second tap hides it', !tip(2).classList.contains('open'));
  cards[1].dispatch('focus');
  check('keyboard focus shows the name', tip(1).classList.contains('open'));
  cards[1].dispatch('keydown', { key: 'Escape' });
  check('Escape hides it', !tip(1).classList.contains('open'));
  cards[0].dispatch('click');
  el.querySelector('.chance-head').dispatch('click');
  check('a press elsewhere on the result hides it', !tip(0).classList.contains('open'));

  // Mahjong tiles: the same, no names listed.
  const tiles = live({ kind: 'draw', deck: 'mahjong', cards: ['dots-1', 'wind-east'] });
  check('tiles start face down, labelled, no names listed', tiles.querySelectorAll('.slot.card.down').length === 2 && tiles.querySelectorAll('.chance-names').length === 0 && tiles.querySelectorAll('.slot.card')[1].getAttribute('aria-label') === 'East wind');
  advance(5000);
  // From history: face up, nothing held.
  const old = renderChance(draw);
  check('from history the cards are face up and still', old.querySelectorAll('.slot.card.down').length === 0 && !old.classList.contains('spinning'));
}

// 6. /tarot: each card's words with it, the summary after the last; a
// reversed card lands upside down.
{
  const tarot = { kind: 'tarot', deck: 'tarot', spread: 'three', cards: ['major-16', 'cups-2', 'wands-page'], reversed: [false, true, false] };
  const plan = revealPlan(tarot);
  const el = live(tarot);
  const says = (i) => el.querySelectorAll('.chance-say').filter((w) => w.dataset.slot === String(i));
  const after = el.querySelectorAll('.chance-after');
  const held = (ws) => ws.every((w) => w.classList.contains('held'));
  const open = (ws) => ws.every((w) => !w.classList.contains('held'));
  check('each card has its position, name and meaning to say', [0, 1, 2].every((i) => says(i).length === 3) && after.length === 1);
  check('at first all the words wait', [0, 1, 2].every((i) => held(says(i))) && held(after));
  advance(plan.says[0] + 40);
  check('card 1 face up: its words, and only its', open(says(0)) && held(says(1)) && held(says(2)) && held(after) && el.querySelectorAll('.slot.card.down').length === 2);
  advance(plan.says[1] - plan.says[0]);
  check('card 2 face up: its words too', open(says(1)) && held(says(2)) && held(after));
  advance(plan.says[2] - plan.says[1]);
  check('card 3 face up: its words, the summary still waiting', open(says(2)) && held(after));
  advance(plan.after - plan.says[2]);
  check('then the summary, which ends the reading', open(after) && after[0].classList.contains('chance-summary'));
  advance(plan.total);
  check('the reversed card is marked reversed and labelled so', el.querySelectorAll('.slot.card')[1].classList.contains('reversed') && el.querySelectorAll('.slot.card')[1].getAttribute('aria-label').endsWith('reversed'));
  check(`a three-card reading takes ${plan.total} ms, well under 4 s`, plan.total < 3000);
  check('a Celtic cross stays under 4 s', revealPlan({ kind: 'tarot', cards: Array(10).fill('major-0') }).total < 4000);
  check('the order: card, its words, the next card', plan.flips.every((t, i) => plan.says[i] > t && (i === 0 || t >= plan.says[i - 1])) && plan.after > plan.says[2]);
}

// 7. Reduced motion: no animation, everything at once.
{
  globalThis.window = { matchMedia: () => ({ matches: true }) };
  const tarot = { kind: 'tarot', deck: 'tarot', spread: 'three', cards: ['major-16', 'cups-2', 'wands-page'], reversed: [false, true, false] };
  const el = live(tarot);
  check('reduced motion: tarot face up, every word there, nothing playing', el.querySelectorAll('.slot.card.down').length === 0 && el.querySelectorAll('.held').length === 0 && !el.classList.contains('spinning') && timers.length === 0);
  const roll = live({ kind: 'roll', dice: '1d20', terms: [{ sign: 1, count: 1, sides: 20, rolls: [17], value: 17 }], total: 17 });
  check('reduced motion: the total at once', shown(roll.querySelector('.chance-total')) && !roll.classList.contains('spinning'));
  const draw = live({ kind: 'draw', deck: 'standard', cards: ['spades-ace'] });
  check('reduced motion: a draw face up at once', draw.querySelectorAll('.slot.card.down').length === 0);
  await spin(draw, { kind: 'draw', deck: 'standard', cards: ['spades-ace'] });
  delete globalThis.window;
}

// Played once: a message drawn again after its animation does not replay
// it; drawn again during it, it carries on.
{
  const c = { kind: 'draw', deck: 'standard', cards: ['spades-ace', 'hearts-2'] };
  const clock = {};
  const a = renderChance(c);
  spinLive(a, c, clock, 'chance');
  advance(revealPlan(c).flips[1] + REVEAL.flip);
  const b = renderChance(c);
  spinLive(b, c, clock, 'chance');
  check('drawn again mid-reveal it carries on, not from the start', b.querySelectorAll('.slot.card.down').length === 0 && b.classList.contains('spinning'));
  advance(5000);
  const d = renderChance(c);
  spinLive(d, c, clock, 'chance');
  check('drawn again after, it does not play', d.querySelectorAll('.slot.card.down').length === 0 && !d.classList.contains('spinning'));
}

console.log(`${failures ? 'FAIL' : 'ok  '} chancereveal: ${checks - failures} of ${checks} checks`);
if (failures) process.exitCode = 1;
