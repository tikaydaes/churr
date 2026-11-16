// Checks, without a browser, how the web chat draws chance results from the
// artwork sheets (docs/ART.md): which sheet each deck and die uses
// (against testdata/art/sheets.json), where a face sits in its sheet, the
// box a die's number goes in, and that a missing or broken sheet leaves the
// text faces as they are.
//
//   node e2e/chanceart.mjs

import { readFileSync } from 'node:fs';
import { SHEETS } from '../internal/web/static/shared/chance/sheets.js';
import { deckSheet, slotArt, sprite, textBox, polyFace, loadSheet, setSheetsForTest, paint, unpaint, dress, BASE } from '../internal/web/static/shared/message/chanceart.js';
import { slots } from '../internal/web/static/shared/message/chance.js';

let failures = 0, checks = 0;
function check(name, ok, detail) {
  checks++;
  if (!ok) { failures++; console.log(`FAIL chanceart: ${name}${detail ? '\n       ' + detail : ''}`); }
}
const table = JSON.parse(readFileSync(new URL('../testdata/art/sheets.json', import.meta.url), 'utf8'));

// The product ships no sheets until real artwork is packed.
check('no sheets are listed in the tree', Array.isArray(SHEETS) && SHEETS.length === 0, JSON.stringify(SHEETS));

// Every deck draws from the sheet the inventory gives it.
for (const [name, s] of Object.entries(table.sheets)) for (const d of s.decks) check(`deck ${d} draws from ${name}`, deckSheet(d) === name, deckSheet(d));
check('an unknown deck has no sheet', deckSheet('uno') === null);

// Slots and their sheets.
{
  const draw = { kind: 'draw', deck: 'italian', cards: ['cups-fante'] };
  const a = slotArt(draw, slots(draw)[0]);
  check('a card comes from its deck\'s sheet by its own code', a.sheet === 'latin' && a.face('cups-fante') === 'cups-fante' && !a.text);
  const roll = { kind: 'roll', dice: '1d6+1d20+2', terms: [{ sign: 1, count: 1, sides: 6, rolls: [5], value: 5 }, { sign: 1, count: 1, sides: 20, rolls: [17], value: 17 }, { sign: 1, value: 2 }], total: 24 };
  const s = slots(roll);
  const d6 = slotArt(roll, s[0]), d20 = slotArt(roll, s[1]);
  check('a six-sided die shows a cube face', d6.sheet === 'dice-cube' && d6.face('5') === 'd6-5' && !d6.text);
  check('other dice show a shape with the number as text', d20.sheet === 'dice-poly' && d20.text && d20.sides === 20);
  check('a plain number has no picture', slotArt(roll, s[2]) === null);
  check('coins, picks and shuffles have no picture', slotArt({ kind: 'flip' }, slots({ kind: 'flip', outcome: 'heads' })[0]) === null);
  check('every die of the inventory\'s cube faces', [1, 2, 3, 4, 5, 6].every((n) => table.sheets['dice-cube'].faces.includes(d6.face(String(n)))));
}

// A placeholder index in the shape pack.py writes.
const cards = { sheet: 'french', file: 'french.svg', cell: [176, 246], faces: ['spades-ace', 'hearts-queen', 'back'] };
const poly = { sheet: 'dice-poly', file: 'dice-poly.svg', cell: [128, 128], faces: ['d4', 'd20', 'dn'], text: { d20: { x: 46, y: 56, w: 36, h: 22 }, dn: { x: 36, y: 50, w: 56, h: 30 } }, sides: { 4: 'd4', 20: 'd20' }, other: 'dn', text_colour: '#111111' };

{
  const s = sprite(cards, 'hearts-queen');
  check('a face is drawn at half its cell', s.width === 88 && s.height === 123, JSON.stringify(s));
  check('from its symbol in the set\'s sprite', s.href === '/shared/chance/french.svg#hearts-queen' && s.viewBox === '0 0 176 246', s.href);
  check('a face the sheet lacks has no sprite', sprite(cards, 'joker-red') === null);
  check('at full scale', sprite(cards, 'back', 1).width === 176);
  check('a die of known sides takes its shape', polyFace(poly, 20) === 'd20' && polyFace(poly, '4') === 'd4');
  check('any other die takes the generic shape', polyFace(poly, 7) === 'dn' && polyFace(poly, 1000) === 'dn');
  const b = textBox(poly, 'd20', '17');
  check('the number box is padding around the anchor', b.padding === '28px 23px 25px 23px', b.padding);
  check('the number fits the box', parseFloat(b.fontSize) <= 22 / 0.72 / 2 + 0.01 && parseFloat(b.fontSize) > 5, b.fontSize);
  check('three figures set smaller than two', parseFloat(textBox(poly, 'dn', '100').fontSize) < parseFloat(textBox(poly, 'dn', '10').fontSize));
}

// Elements, enough of them for paint: a face, and the picture's <svg><use>.
function fakeEl() {
  const attrs = {};
  return { attrs, setAttribute: (k, v) => { attrs[k] = v; }, parentNode: null, remove() { if (this.parentNode) this.parentNode.kids = this.parentNode.kids.filter((k) => k !== this); this.parentNode = null; } };
}
function fakeSvg() { const svg = fakeEl(); svg.firstChild = fakeEl(); return svg; }
function fakeFace(text) {
  const cls = new Set(['slot-face']);
  const f = {
    kids: [], style: { minWidth: '3ch' }, dataset: { minWidth: '3ch' },
    classList: { add: (...c) => c.forEach((x) => cls.add(x)), remove: (...c) => c.forEach((x) => cls.delete(x)), contains: (c) => cls.has(c), toggle: (c, on) => (on ? cls.add(c) : cls.delete(c)) },
    has: (c) => cls.has(c),
    get firstChild() { return this.kids[0] || null; },
    prepend(el) { el.parentNode = this; this.kids.unshift(el); },
    set textContent(t) { this.kids.forEach((k) => { if (typeof k === 'object') k.parentNode = null; }); this.kids = [t]; },
    get textContent() { return this.kids.filter((k) => typeof k === 'string').join(''); },
  };
  f.textContent = text;
  return f;
}
setSheetsForTest(SHEETS, { svg: fakeSvg });
{
  const f = fakeFace('Q♥');
  const art = { sheet: { index: cards, url: BASE + 'french.svg' }, face: (k) => k, text: false };
  check('paint draws a card', paint(f, art, 'hearts-queen', 'Q♥') && f.has('art') && f.style.width === '88px' && f.style.height === '123px');
  check('as an svg using the symbol', f.firstChild === f._svg && f._svg.firstChild.attrs.href === '/shared/chance/french.svg#hearts-queen' && f._svg.attrs.viewBox === '0 0 176 246');
  check('the text stays in the element', f.textContent === 'Q♥');
  f.textContent = '7♠';
  paint(f, art, 'spades-ace', '7♠');
  check('a new frame puts the picture back and moves it', f.firstChild === f._svg && f._svg.firstChild.attrs.href.endsWith('#spades-ace') && f.textContent === '7♠');
  check('a frame the sheet lacks falls back to text', !paint(f, art, 'joker-white', 'JK') && !f.has('art') && f.style.minWidth === '3ch' && f._svg.parentNode === null);
  const d = fakeFace('17');
  const dart = { sheet: { index: poly, url: BASE + 'dice-poly.svg' }, face: () => null, text: true, sides: 20 };
  check('paint draws a die shape with its number', paint(d, dart, '17', '17') && d.has('art-num') && d._svg.firstChild.attrs.href.endsWith('#d20') && d.style.padding === '28px 23px 25px 23px');
  unpaint(d);
  check('unpaint restores the text face', !d.has('art') && !d.has('art-num') && d.style.padding === '' && d.firstChild === '17');
}

// Loading: only listed sheets are fetched, each once; failures fall back.
{
  let fetched = [];
  setSheetsForTest([], { index: async (u) => { fetched.push(u); return cards; }, sprite: async () => {} });
  check('an unlisted sheet is never fetched', (await loadSheet('french')) === null && fetched.length === 0);
  setSheetsForTest(['french'], { index: async (u) => { fetched.push(u); return cards; }, sprite: async (u) => { fetched.push(u); } });
  const a = await loadSheet('french'), b = await loadSheet('french');
  check('a listed sheet loads once', a && a === b && a.url === '/shared/chance/french.svg' && JSON.stringify(fetched) === '["/shared/chance/french.json","/shared/chance/french.svg"]', JSON.stringify(fetched));
  setSheetsForTest(['french'], { index: async () => { throw new Error('404'); }, sprite: async () => {} });
  check('a missing index falls back', (await loadSheet('french')) === null);
  setSheetsForTest(['french'], { index: async () => cards, sprite: async () => { throw new Error('broken'); } });
  check('a sprite that fails to load falls back', (await loadSheet('french')) === null);

  // dress: faces get their pictures when the sheet is here, and keep their
  // text when it is not.
  const c = { kind: 'draw', deck: 'standard', cards: ['spades-ace', 'hearts-queen'] };
  const list = slots(c);
  setSheetsForTest([], {});
  let faces = list.map((x) => fakeFace(x.text));
  await dress(c, faces, list);
  check('with no sheets the faces stay text', faces.every((f) => !f.has('art') && !f._art));
  setSheetsForTest(['french'], { index: async () => cards, sprite: async () => {} });
  faces = list.map((x) => fakeFace(x.text));
  await dress(c, faces, list);
  check('with the sheet each face is its card', faces.every((f) => f.has('art')) && faces[1]._svg.firstChild.attrs.href.endsWith('#hearts-queen'));
  check('each face keeps what it needs to spin', faces.every((f) => f._art && f._art.sheet.index === cards));
  setSheetsForTest(SHEETS, {});
}

console.log(`${failures ? 'FAIL' : 'ok  '} chanceart: ${checks - failures} of ${checks} checks`);
if (failures) process.exitCode = 1;
