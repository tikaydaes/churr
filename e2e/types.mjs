// The catalogue in the web chat (docs/DESIGN-MODULES.md sections 3.2, 4
// and 9.1), without a server or a browser:
//
//   1. the chance kinds' checks, against testdata/types/<kind>.json, the
//      cases the server's check passes too;
//   2. the three-step fallback: the kind, its generic rendition, the text,
//      and the state "this needs a newer app";
//   3. what moves when it arrives live;
//   4. values kept untouched through an edit the client makes itself;
//   5. a roll drawn: as a message's card and as inline dice, through the
//      catalogue, with the text set as text; and what cannot be drawn left
//      as its text;
//   6. the shield of a draw's proof: drawn from this client's own check,
//      by built-in chance kinds and by a card's fair_mark, never green on a
//      card's say-so.
//
// It runs on a small stand-in for the page's elements.
//
//   node e2e/types.mjs

import { readFileSync } from 'node:fs';

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
  // Nothing may set markup: a test fails if anything tries.
  set innerHTML(v) { throw new Error('markup set from a string: ' + v); }
  setAttribute(k, v) { this.attrs[k] = String(v); }
  getAttribute(k) { return k in this.attrs ? this.attrs[k] : null; }
  removeAttribute(k) { delete this.attrs[k]; }
  addEventListener(type, fn) { (this.listeners[type] = this.listeners[type] || []).push(fn); }
  dispatch(type, ev = {}) {
    const e = { type, target: this, preventDefault() {}, stopPropagation() {}, ...ev };
    for (let n = this; n; n = n.parentNode) for (const fn of (n.listeners && n.listeners[type]) || []) fn(e);
  }
  showModal() { this.open = true; }
  close() { this.open = false; }
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
const made = [];
globalThis.document = {
  createElement: (t) => { const e = new El(t); made.push(e.tagName); return e; },
  createElementNS: (_, t) => new El(t),
  body: new El('body'),
};
// Reduced motion is not asked for; a clock that never moves keeps a live
// spin on its first frame.
performance.now = () => 0;
globalThis.setTimeout = () => 0;
globalThis.clearTimeout = () => {};

const S = '../internal/web/static/shared/';
const { LEVEL, KINDS, read, spins, keepValues } = await import(S + 'types/catalogue.js');
const { diceRollData } = await import(S + 'types/dice-roll/data.js');
const { drawObject, drawView, newerNote } = await import(S + 'types/draw.js');
const { setSheetsForTest } = await import(S + 'message/chanceart.js');
setSheetsForTest([], {});

let checks = 0, failures = 0;
// sorted is a value with its keys in order, so that two are compared by
// what they hold.
const sorted = (v) => (Array.isArray(v) ? v.map(sorted) : v && typeof v === 'object' ? Object.fromEntries(Object.keys(v).sort().map((k) => [k, sorted(v[k])])) : v);
const check = (what, got, want) => {
  checks++;
  const g = JSON.stringify(sorted(got)), w = JSON.stringify(sorted(want));
  if (g !== w) { failures++; console.log(`FAIL types: ${what}\n       want ${w}\n       got  ${g}`); }
};

// 1. The kind's check, as the server's.
const cases = JSON.parse(readFileSync(new URL('../testdata/types/dice_roll.json', import.meta.url), 'utf8'));
check('the file is the kind\'s', [cases.kind, cases.level], ['dice_roll', KINDS.dice_roll.level]);
for (const c of cases.valid) {
  const got = diceRollData(c.data);
  check('valid: ' + c.name, got === null ? null : (c.draws ? got : true), c.draws || true);
  if (!c.draws && got) check('drawn as it came: ' + c.name, got.rows.map((r) => r.total), c.data.rows.map((r) => r.total));
}
for (const c of cases.invalid) check('invalid: ' + c.name, diceRollData(c.data), null);
// The other chance kinds (docs/FAIR.md section 5a), each against its own
// file, and each drawn as a message's card with the text set as text.
for (const kind of ['coin_flip', 'pick', 'shuffle', 'card_draw', 'tarot_reading']) {
  const f = JSON.parse(readFileSync(new URL(`../testdata/types/${kind}.json`, import.meta.url), 'utf8'));
  check(`${kind}: the file is the kind's`, [f.kind, f.level, !!KINDS[kind] && KINDS[kind].chance], [kind, KINDS[kind] && KINDS[kind].level, true]);
  for (const c of f.valid) check(`${kind} valid: ${c.name}`, KINDS[kind].data(c.data) !== null, true);
  for (const c of f.invalid) check(`${kind} invalid: ${c.name}`, KINDS[kind].data(c.data), null);
  const m = { text: 'words', view: { kind, level: 1, data: f.valid[0].data, by: 'x' } };
  const { card } = drawView(m, {});
  check(`${kind}: drawn as a card`, !!card && card.className === 'chance', true);
}
const one = cases.valid[0].data;
const frozen = JSON.stringify(one);
diceRollData(one);
check('the check changes nothing it is given', JSON.stringify(one), frozen);

// 2. The three-step fallback.
const roll = { kind: 'dice_roll', level: 1, by: 'dice', data: one };
check('this client\'s level', LEVEL, 1);
check('a kind it knows, at its level: drawn by the kind', read(roll), { step: 'kind', kind: 'dice_roll', data: diceRollData(one), removed: false, alt: false });
check('a level above its own: the text, and a newer app would draw it', read({ ...roll, level: 2 }), { step: 'text', newer: true });
check('a kind it does not know: the same', read({ kind: 'tarot_spread', level: 4, data: { cards: [1] } }), { step: 'text', newer: true });
check('a kind it does not know, with a rendition it does: the rendition', read({ kind: 'tarot_spread', level: 4, data: {}, alt: roll }).alt, true);
check('a level above its own, with a rendition: the rendition', read({ ...roll, level: 9, alt: roll }).step, 'kind');
check('a rendition it cannot draw either: the text', read({ kind: 'later', level: 1, alt: { kind: 'later_still', level: 1 } }), { step: 'text', newer: true });
check('data that does not hold together: the text, no error, no newer app', read({ ...roll, data: { dice: '2d6', rows: [] } }), { step: 'text', newer: false });
check('data still in bytes (a server that did not know the kind): the text', read({ ...roll, data: 'GgUyZDYrMw' }), { step: 'text', newer: false });
check('dice nothing was rolled for: the kind, removed', read({ kind: 'dice_roll', level: 1, by: 'dice', removed: true }), { step: 'kind', kind: 'dice_roll', data: null, removed: true, alt: false });
check('no level: not trusted, the text', read({ kind: 'dice_roll', data: one }).step, 'text');
check('not a value at all', [read(null), read('x'), read([]), read({ level: 1 })].map((r) => r.step), ['text', 'text', 'text', 'text']);
let deep = roll;
for (let i = 0; i < 6; i++) deep = { kind: 'later' + i, level: 1, alt: deep };
check('renditions are followed only so far', read(deep).step, 'text');
check('a kind named like an object\'s own field is no kind', read({ kind: 'constructor', level: 1, data: {} }), { step: 'text', newer: true });

// 3. What moves when it arrives live.
check('a roll as a message\'s view spins', spins({ view: roll }), true);
check('inline dice spin', spins({ entities: [{ type: 'object', at: 0, len: 9, object: roll }] }), true);
check('dice not rolled do not', spins({ entities: [{ type: 'object', at: 0, len: 9, object: { ...roll, data: undefined, removed: true } }] }), false);
check('a kind this client does not know does not', spins({ view: { kind: 'later', level: 1 } }), false);

// 4. Values kept through an edit the client makes itself (rule 5).
const later = { kind: 'parcel', level: 7, by: 'parcels', data: { id: 'X1', extra: [1, { deep: true }] }, source: 'parcels/parcel' };
const oldText = 'track X1 and X2 [[1d6]]';
const oldEnts = [{ type: 'object', at: 6, len: 2, object: later }, { type: 'object', at: 16, len: 7, object: roll }, { type: 'task', at: 0, len: 1, item_id: 't1' }];
let kept = keepValues(oldText, oldEnts, 'please track X1 and X2 [[1d6]]', [{ type: 'instant', at: 0, len: 6 }]);
check('kept where its text now stands', kept.map((e) => [e.type, e.at, e.len]), [['instant', 0, 6], ['object', 13, 2], ['object', 23, 7]]);
check('kept untouched: the same value', kept[1].object === later && kept[2].object === roll, true);
check('byte for byte', JSON.stringify(kept[1].object), JSON.stringify(later));
kept = keepValues(oldText, oldEnts, 'track X3 [[1d6]]', []);
check('a value whose text is gone goes with it', kept.map((e) => e.at), [9]);
kept = keepValues(oldText, oldEnts, 'X1', [{ type: 'reference', at: 0, len: 2 }]);
check('the new text\'s own entities come first', kept.map((e) => e.type), ['reference']);
check('nothing to keep', keepValues('', null, 'x', []), []);

// 5. Drawing.
const inline = (object, at = 0) => ({ type: 'object', at, len: 9, object });
let el = drawObject(inline(roll), '[[2d6+3]]', {});
check('inline dice are a chip', [el.tagName, el.classList.contains('dice-inline')], ['SPAN', true]);
check('each die and the total', [el.querySelectorAll('.slot-face').map((f) => f.textContent), el.querySelector('.chance-total').textContent], [['4', '1', '3'], ' = 8']);
check('the tooltip is the roll in words', el.getAttribute('title'), 'Rolled 2d6+3: 4, 1, 3; total 8');
el.dispatch('click');
const dlg = document.body.querySelector('dialog');
check('pressed, the dialog with the roll', !!(dlg && dlg.open && dlg.querySelector('.chance') && dlg.textContent.includes('[[2d6+3]]')), true);
el = drawObject(inline({ kind: 'dice_roll', level: 1, by: 'dice', removed: true }), '[[1d6]]', {});
check('dice not rolled say so', el.textContent, '[[1d6]] (not rolled: written in an edit)');
el = drawObject(inline({ kind: 'parcel', level: 7, data: {} }), 'X1', {});
check('a kind this client does not know: its typed text, marked as needing a newer app', [el.tagName, el.textContent, el.classList.contains('object-newer'), !!el.getAttribute('title')], ['SPAN', 'X1', true, true]);
check('data that does not hold together: its typed text, nothing more', drawObject(inline({ ...roll, data: { dice: 'x' } }), '[[1d6]]', {}), '[[1d6]]');
el = drawObject(inline({ kind: 'parcel', level: 7, data: {}, alt: roll }), 'X1', {});
check('a rendition this client knows is drawn', el.classList && el.classList.contains('dice-inline'), true);
let clock = {};
drawObject(inline(roll, 4), '[[2d6+3]]', { live: () => clock });
check('arrived live, it spins once on the message\'s clock', Object.keys(clock), ['object@4']);

const msg = { text: 'Rolled 2d6+3: [4, 1] + 3 = 8', view: roll };
let v = drawView(msg, {});
check('a roll message is the kind\'s card', [v.card && v.card.classList.contains('chance'), v.card && v.card.dataset.kind], [true, 'roll']);
check('its faces and total', [v.card.querySelectorAll('.slot-face').map((f) => f.textContent), v.card.querySelector('.chance-total').textContent], [['4', '1', '3'], '= 8']);
clock = {};
drawView(msg, { live: clock });
check('arrived live, the card spins once', Object.keys(clock), ['view']);
check('a view this client cannot draw: the text stands, with the note', drawView({ text: 'x', view: { kind: 'later', level: 1 } }), { card: null, newer: true });
check('a view that does not hold together: the text stands, no note', drawView({ text: 'x', view: { ...roll, data: {} } }), { card: null, newer: false });
check('no view', drawView({ text: 'x' }), { card: null, newer: false });
check('the note', newerNote().textContent.trim(), 'Shown as text: this needs a newer version of Churr.');

// Text is drawn as text: markup in the data stays characters.
made.length = 0;
const marked = { ...roll, data: { dice: '<img src=x onerror=alert(1)>', rows: [{ dice: '<b>2d6</b>', terms: one.rows[0].terms, total: 8 }] } };
v = drawView({ text: 'x', view: marked }, {});
check('markup in the data is shown as its characters', v.card.querySelector('.chance-title').textContent, 'Rolled <img src=x onerror=alert(1)>');
check('and makes no element of its own', made.filter((t) => t === 'IMG' || t === 'B'), []);

// 6. The shield (shared/fair/shield.js, DESIGN-FAIR.md section 13): drawn
// from this client's own check, never from what a card says. Checking at
// first, then a shield with a tick for a proof that checks out, a shield
// with a question mark for a draw whose server's key history this client
// was never given, a warning triangle for one that fails; each its own
// shape and spoken label. A card's fair_mark stands on outcomes it names,
// and cannot make the shield green: on a message with no draw, or naming an
// outcome the draw does not have, it fails. A value of kind fair_mark is no
// kind this client draws. Pressed, it opens Validate result with the draw in
// the fragment.
{
  const { shieldFor } = await import(S + 'fair/shield.js');
  const { renderCards } = await import(S + 'message/cards.js');
  const { fromFragment } = await import(S + 'fair/validate.js');
  const mv = JSON.parse(readFileSync(new URL('../testdata/fair/message.json', import.meta.url), 'utf8'));
  const ctx = { keysOf: (name) => (name === mv.server ? { keys: mv.keys } : null), server: mv.server };
  const shape = (el) => el.querySelectorAll('path').map((p) => p.getAttribute('d'));
  const word = (el) => el.querySelector('span.fair-shield-word').textContent;
  const judged = async (el) => { await el._judged; return [el.dataset.state, word(el)]; };
  check('no shield for a message that drew nothing', shieldFor({ text: 'x' }, ctx), null);

  const good = structuredClone(mv.message);
  const ok = shieldFor(good, ctx);
  check('the shield checks first', [ok.dataset.state, word(ok)], ['checking', 'Checking the proof']);
  check('then is green: the proof and every outcome check out', await judged(ok), ['verified', 'Verified']);
  check('its label is spoken', ok.getAttribute('aria-label').startsWith('Verified. '), true);

  const bad = structuredClone(mv.message);
  bad.fair.t += 1;
  const no = shieldFor(bad, ctx);
  check('a check that fails is the warning mark', await judged(no), ['failed', 'Not verified']);
  check('which says why', no.title, 'The proof is not this message\'s.');
  const changed = structuredClone(mv.message);
  changed.view.data.rows[0].terms[0].rolls[0] = changed.view.data.rows[0].terms[0].rolls[0] === 1 ? 2 : 1;
  check('an outcome changed fails', (await judged(shieldFor(changed, ctx)))[0], 'failed');

  const far = structuredClone(mv.message);
  const unknown = shieldFor(far, { ...ctx, keysOf: () => null });
  check('a draw this client holds no key history for cannot be checked here', await judged(unknown), ['unknown', 'Cannot be checked here']);
  check('the three states are three shapes', new Set([ok, no, unknown].map((e) => JSON.stringify(shape(e)))).size, 3);

  // A card's fair_mark: named outcomes only, and never a verdict of its own.
  const named = (objects, m = structuredClone(mv.message)) => shieldFor(m, ctx, objects);
  check('a card naming the roll is green', (await judged(named([0])))[0], 'verified');
  check('a card naming an outcome the draw does not have fails', (await judged(named([7])))[0], 'failed');
  const plain = { id: 'p', text: 'A bot says it is fair', created_at: mv.message.created_at, cards: [{ title: 'Verified fair', fair_mark: { objects: [0], state: 'verified', verified: true } }] };
  const drawn = renderCards(plain.cards, { shield: (objects) => shieldFor(plain, ctx, objects) });
  const cardShield = drawn.querySelector('button.fair-shield');
  check('a card with a fair_mark draws the shield', !!cardShield, true);
  check('on a message with no draw it fails, whatever the card says', await judged(cardShield), ['failed', 'Not verified']);
  check('a card with no fair_mark draws none', renderCards([{ title: 'Shield' }], { shield: () => { throw new Error('asked for a shield'); } }).querySelector('button.fair-shield'), null);
  check('a value of kind fair_mark is not drawn as a shield', [read({ kind: 'fair_mark', level: 1, data: { objects: [0] } }).step,
    drawView({ text: 'x', view: { kind: 'fair_mark', level: 1, data: { objects: [0] } } }, {}).card], ['text', null]);

  // Pressed: Validate result, the draw in the fragment.
  let opened = null;
  const press = shieldFor(structuredClone(mv.message), ctx, undefined, { open: (u) => { opened = u; } });
  await press._judged;
  press.dispatch('click');
  for (let k = 0; k < 50 && !opened; k++) await new Promise((r) => setImmediate(r));
  const doc = opened && fromFragment(opened.slice(opened.indexOf('#')));
  check('pressed, it opens /validate with the draw in the fragment', [opened && opened.startsWith('/validate#'), doc && doc.alpha === mv.message.fair.alpha, doc && doc.objects.length], [true, true, 2]);
}

if (checks < 40) { failures++; console.log('FAIL types: too few checks ran'); }
console.log(`${failures ? 'FAIL' : 'ok  '} types: ${checks} checks, ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
