// Checks, without a browser, how the web chat plays a chance result that
// arrives live (ruling R87): a roll's total and dropped dice only once the
// reels have stopped; cards and tiles face down, then turned over one
// after another; a tarot reading's cards in its spread and the summary
// after the last, laid out in its spread, each card opening its details on
// a press; no list of names, each card's name popping up on hover, focus
// or a tap, and kept as its label; nothing replayed on a press;
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
const { drawObject } = await import('../internal/web/static/shared/types/draw.js');
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
  const roll = { kind: 'roll', dice: '4d6kh3+2', rows: [{ dice: '4d6kh3+2', terms: [{ sign: 1, count: 4, sides: 6, rolls: [5, 2, 6, 3], dropped: [1], value: 14 }, { sign: 1, value: 2 }], total: 16 }] };
  const el = live(roll);
  const total = el.querySelector('.chance-total');
  check('a live roll is rolling from its first frame', el.classList.contains('spinning'));
  check('the total is hidden while the dice roll', !shown(total));
  check('no die is struck out while the dice roll', el.querySelectorAll('.slot.dropped').length === 0);
  // The fourth die (of five slots, the last a plain 2) stops at 1035 ms.
  advance(990);
  check('still hidden just before the last die stops', !shown(total) && el.classList.contains('spinning'));
  advance(SPIN_MS - 990);
  check('once the dice stop: the total', shown(total) && total.textContent === '= 16' && !el.classList.contains('spinning'));
  check('once the dice stop: the dropped die, struck out, no word beside it, and said so', el.querySelectorAll('.slot.dropped').length === 1 && el.querySelectorAll('.slot-note').length === 0 && !el.textContent.includes('dropped') && el.getAttribute('aria-label').includes('2 (dropped)'));
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

// 1b. A roll of two rows: a line each, rolling together, each row's
// total once its own dice stop.
{
  const roll = { kind: 'roll', dice: '1d6, 4d6kh3', rows: [
    { dice: '1d6', terms: [{ sign: 1, count: 1, sides: 6, rolls: [4], value: 4 }], total: 4 },
    { dice: '4d6kh3', terms: [{ sign: 1, count: 4, sides: 6, keep: 'kh', keep_n: 3, rolls: [5, 2, 6, 3], dropped: [1], value: 14 }], total: 14 }] };
  const el = live(roll);
  const lines = el.querySelectorAll('.chance-slots');
  const totals = el.querySelectorAll('.chance-total');
  check('two rows: two lines, each with its expression, dice and total', lines.length === 2 && lines[0].querySelectorAll('.slot').length === 1 && lines[1].querySelectorAll('.slot').length === 4 && lines[0].querySelector('.chance-row-dice').textContent === '1d6' && totals.length === 2);
  check('both rows roll at first, no total shown', el.classList.contains('spinning') && totals.every((t) => !shown(t)));
  // The one-die row stops at 1.2 s too (a row of one stops last); the
  // four-dice row's first die stops at 540 ms. Both rows are rolling at 500.
  advance(560);
  check('a row\'s total waits for its own last die', totals.every((t) => !shown(t)));
  advance(SPIN_MS);
  check('once all stop: each row\'s total, and the dropped die greyed', shown(totals[0]) && totals[0].textContent === '= 4' && shown(totals[1]) && totals[1].textContent === '= 14' && el.querySelectorAll('.slot.dropped').length === 1);
  check('said row by row', el.getAttribute('aria-label') === 'Rolled 1d6, 4d6kh3: 1d6: 4; total 4. 4d6kh3: 5, 2 (dropped), 6, 3; total 14');
}
{
  // Rows of different lengths: each stops left to right within the same 1.2 s.
  const roll = { kind: 'roll', dice: '2d6, 6d6', rows: [
    { dice: '2d6', terms: [{ sign: 1, count: 2, sides: 6, rolls: [1, 2], value: 3 }], total: 3 },
    { dice: '6d6', terms: [{ sign: 1, count: 6, sides: 6, rolls: [1, 2, 3, 4, 5, 6], value: 21 }], total: 21 }] };
  const el = live(roll);
  const totals = el.querySelectorAll('.chance-total');
  advance(SPIN_MS - 100);
  check('rows roll together and the whole roll keeps to 1.2 s', el.classList.contains('spinning'));
  advance(200);
  check('each row\'s total once its own dice stop', shown(totals[0]) && shown(totals[1]) && !el.classList.contains('spinning'));
}

// Inline dice, an object entity of kind dice_roll: the same.
const inline = (at, dice, terms, total) => ({ type: 'object', at, len: 9, object: { kind: 'dice_roll', level: 1, by: 'dice', data: { dice, rows: [{ dice, terms, total }] } } });
{
  const e = inline(9, '2d6+3', [{ sign: 1, count: 2, sides: 6, rolls: [4, 1], value: 5 }, { sign: 1, value: 3 }], 8);
  const clock = {};
  const el = drawObject(e, '[[2d6+3]]', { live: () => clock });
  const total = el.querySelector('.chance-total');
  check('inline dice hide their total while they roll', el.classList.contains('spinning') && !shown(total));
  advance(SPIN_MS + 100);
  check('inline dice show their total once stopped', shown(total) && total.textContent.includes('8') && !el.classList.contains('spinning'));
  // Pressed, they open the roll in a dialog, and do not roll again.
  const body = new El('body');
  globalThis.document.body = body;
  El.prototype.showModal = function () { this.open = true; };
  El.prototype.close = function () {};
  el.dispatch('click', { stopPropagation() {} });
  check('inline dice pressed do not roll again', !el.classList.contains('spinning'));
  const dlg = body.querySelector('dialog');
  check('inline dice pressed open the roll', dlg && dlg.open && dlg.querySelector('.chance') && dlg.querySelectorAll('.slot').length === 3 && dlg.textContent.includes('[[2d6+3]]'));
  delete globalThis.document.body;
}
{
  // Inline dice are drawn as /roll's: a d20 on the fly; a d6 without its
  // sprite (none here) keeps its number; the label is unchanged.
  const e = inline(0, '1d20+1d6', [{ sign: 1, count: 1, sides: 20, rolls: [17], value: 17 }, { sign: 1, count: 1, sides: 6, rolls: [3], value: 3 }], 20);
  const el = drawObject(e, '[[d20+d6]]', {});
  const faces = el.querySelectorAll('.slot-face');
  check('an inline d20 is drawn as a die', faces[0].classList.contains('art') && faces[0].classList.contains('art-die'));
  check('an inline d6 without its sprite keeps its number', !faces[1].classList.contains('art') && faces[1].textContent === '3');
  check('the inline label is as before: the expression, each die and the total', el.getAttribute('aria-label') === 'Dice [[d20+d6]]. Rolled 1d20+1d6: 17, 3; total 20. Opens the roll', el.getAttribute('aria-label'));
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

// 6. /tarot: the cards lie in the spread's layout and turn over in its
// order, the one just landed marked, the summary after the last; no words
// listed; a press opens a card's position, name and meaning; a reversed
// card lands upside down.
{
  const tarot = { kind: 'tarot', deck: 'tarot', spread: 'three', cards: ['major-16', 'cups-2', 'wands-page'], reversed: [false, true, false] };
  const plan = revealPlan(tarot);
  const el = live(tarot);
  const cards = el.querySelectorAll('.slot.card');
  const after = el.querySelectorAll('.chance-after');
  const held = (ws) => ws.every((w) => w.classList.contains('held'));
  const open = (ws) => ws.every((w) => !w.classList.contains('held'));
  const just = () => cards.map((s, i) => (s.classList.contains('just') ? i : -1)).filter((i) => i >= 0);
  check('the reading is a spread of three, no list of words', el.querySelectorAll('.chance-spread').length === 1 && cards.length === 3 && el.querySelectorAll('.chance-say').length === 0 && el.querySelectorAll('ol').length === 0 && after.length === 1);
  check('three cards lie in a row, left to right', cards.every((s) => parseFloat(s.style.top) === 0) && parseFloat(cards[0].style.left) < parseFloat(cards[1].style.left) && parseFloat(cards[1].style.left) < parseFloat(cards[2].style.left));
  check('each card is a button labelled with its position, name and orientation', cards.every((s) => s.getAttribute('role') === 'button') && cards[0].getAttribute('aria-label') === 'Past: The Tower, upright' && cards[1].getAttribute('aria-label') === 'Present: Two of cups, reversed');
  check('at first all face down, nothing marked, the summary waits', el.querySelectorAll('.slot.card.down').length === 3 && just().length === 0 && held(after));
  advance(plan.says[0] + 40);
  check('card 1 face up and marked', el.querySelectorAll('.slot.card.down').length === 2 && just().join() === '0' && held(after));
  advance(plan.says[1] - plan.says[0]);
  check('card 2 face up, the mark moved to it', el.querySelectorAll('.slot.card.down').length === 1 && just().join() === '1');
  advance(plan.says[2] - plan.says[1]);
  check('card 3 face up, the summary still waiting', just().join() === '2' && held(after));
  advance(plan.after - plan.says[2]);
  check('then the summary, which ends the reading', open(after) && after[0].classList.contains('chance-summary'));
  advance(plan.total);
  check('when it is over no card is marked', just().length === 0);
  check('the reversed card is marked reversed', cards[1].classList.contains('reversed'));
  check(`a three-card reading takes ${plan.total} ms, well under 4 s`, plan.total < 3000);
  check('a Celtic cross stays under 4 s', revealPlan({ kind: 'tarot', cards: Array(10).fill('major-0') }).total < 4000);
  check('the order: each card lands before the next turns', plan.flips.every((t, i) => plan.says[i] > t && (i === 0 || t >= plan.says[i - 1])) && plan.after > plan.says[2]);

  const detail = (i) => cards[i].querySelector('.chance-detail');
  const name = (i) => cards[i].querySelector('.chance-name');
  cards[0].dispatch('pointerenter', { pointerType: 'mouse' });
  check('hover shows only the name', name(0).classList.contains('open') && !detail(0).classList.contains('open'));
  cards[0].dispatch('pointerleave', { pointerType: 'mouse' });
  cards[0].dispatch('click');
  check('a press opens the details: position, name, orientation, meaning', detail(0).classList.contains('open') && cards[0].getAttribute('aria-expanded') === 'true' && detail(0).textContent.startsWith('Past') && detail(0).textContent.includes('The Tower') && detail(0).textContent.includes('upright') && detail(0).querySelector('.chance-meaning').textContent.length > 10);
  cards[1].dispatch('keydown', { key: 'Enter' });
  check('Enter on another card opens its details, closing the first', detail(1).classList.contains('open') && !detail(0).classList.contains('open') && cards[0].getAttribute('aria-expanded') === 'false' && detail(1).textContent.includes('reversed'));
  cards[1].dispatch('keydown', { key: 'Escape' });
  check('Escape closes them', !detail(1).classList.contains('open'));
  cards[2].dispatch('keydown', { key: ' ' });
  check('Space opens them too', detail(2).classList.contains('open'));
  el.querySelector('.chance-head').dispatch('click');
  check('a press elsewhere on the result closes them', !detail(2).classList.contains('open'));
  let focused = null;
  cards.forEach((s) => { s.focus = () => { focused = s; }; });
  cards[0].dispatch('keydown', { key: 'ArrowRight' });
  check('an arrow moves to the next card in reading order', focused === cards[1]);
  cards[0].dispatch('keydown', { key: 'ArrowLeft' });
  check('and back round from the first to the last', focused === cards[2]);
}

// 6b. The spreads' layouts: a Celtic cross with the challenge across the
// present, the staff to the right from the bottom up; a horseshoe.
{
  const c = { kind: 'tarot', deck: 'tarot', spread: 'celtic', cards: Array.from({ length: 10 }, (_, i) => 'major-' + i), reversed: [false, true, false, false, false, false, false, false, false, false] };
  const cards = renderChance(c).querySelectorAll('.slot.card');
  const at = (i) => [parseFloat(cards[i].style.left), parseFloat(cards[i].style.top)];
  check('a Celtic cross lays ten cards', cards.length === 10);
  check('the challenge lies across the present, turned', at(0).join() === at(1).join() && cards[1].classList.contains('cross') && !cards[0].classList.contains('cross'));
  check('the reversed crossing card is both turned and reversed', cards[1].classList.contains('cross') && cards[1].classList.contains('reversed'));
  check('past left, future right, above over, below under', at(2)[0] < at(0)[0] && at(3)[0] > at(0)[0] && at(4)[1] < at(0)[1] && at(5)[1] > at(0)[1] && at(2)[1] === at(0)[1]);
  check('the staff: one column to the right, advice at the bottom to outcome at the top', [6, 7, 8, 9].every((i) => at(i)[0] === at(6)[0] && at(i)[0] > at(3)[0]) && at(6)[1] > at(7)[1] && at(7)[1] > at(8)[1] && at(8)[1] > at(9)[1]);
  check('each card fits the table', cards.every((s) => parseFloat(s.style.left) >= 0 && parseFloat(s.style.left) + parseFloat(s.style.width) <= 100.001 && parseFloat(s.style.top) + parseFloat(s.style.height) <= 100.001));
  check('labels name the position', cards[1].getAttribute('aria-label').startsWith('Challenge: ') && cards[9].getAttribute('aria-label').startsWith('Outcome: '));
  const shoe = renderChance({ kind: 'tarot', deck: 'tarot', spread: 'horseshoe', cards: Array.from({ length: 7 }, (_, i) => 'major-' + i), reversed: Array(7).fill(false) }).querySelectorAll('.slot.card');
  const top = (i) => parseFloat(shoe[i].style.top);
  check('a horseshoe: seven cards in a U, lowest in the middle', shoe.length === 7 && top(3) > top(2) && top(2) > top(1) && top(1) > top(0) && top(3) > top(4) && top(4) > top(5) && top(5) > top(6));
}

// 7. Reduced motion: no animation, everything at once.
{
  globalThis.window = { matchMedia: () => ({ matches: true }) };
  const tarot = { kind: 'tarot', deck: 'tarot', spread: 'three', cards: ['major-16', 'cups-2', 'wands-page'], reversed: [false, true, false] };
  const el = live(tarot);
  check('reduced motion: tarot face up in place, the summary there, nothing playing', el.querySelectorAll('.slot.card.down').length === 0 && el.querySelectorAll('.slot.card.just').length === 0 && el.querySelectorAll('.held').length === 0 && !el.classList.contains('spinning') && timers.length === 0);
  const roll = live({ kind: 'roll', dice: '1d20', rows: [{ dice: '1d20', terms: [{ sign: 1, count: 1, sides: 20, rolls: [17], value: 17 }], total: 17 }] });
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
