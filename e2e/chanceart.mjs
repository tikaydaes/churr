// Checks, without a browser, how the web chat draws chance results from the
// artwork sheets (docs/ART.md): which sheet each deck and the six-sided die
// uses (against testdata/art/sheets.json), where a face sits in its sheet,
// that a missing or broken sheet leaves the text faces as they are, and
// the dice and coins drawn on the fly (diceshape.js).
//
//   node e2e/chanceart.mjs

import { readFileSync } from 'node:fs';
import { SHEETS } from '../internal/web/static/shared/chance/sheets.js';
import { deckSheet, slotArt, sprite, loadSheet, setSheetsForTest, paint, unpaint, dress, paintFace, backArt, tileBackDrawing, BASE } from '../internal/web/static/shared/message/chanceart.js';
import { slots, spokenResult } from '../internal/web/static/shared/message/chance.js';
import { NGON_MAX_SIDES, DIE_BOX, dieOutline, dieDrawing, dieSvg, fitText, coinFace } from '../internal/web/static/shared/message/diceshape.js';

let failures = 0, checks = 0;
function check(name, ok, detail) {
  checks++;
  if (!ok) { failures++; console.log(`FAIL chanceart: ${name}${detail ? '\n       ' + detail : ''}`); }
}
const table = JSON.parse(readFileSync(new URL('../testdata/art/sheets.json', import.meta.url), 'utf8'));

// Every set listed in the tree has its sprite and index, and the index
// names the faces the inventory gives it.
for (const name of SHEETS) {
  const dir = new URL('../internal/web/static/shared/chance/', import.meta.url);
  let idx = null, svg = '';
  try { idx = JSON.parse(readFileSync(new URL(name + '.json', dir), 'utf8')); svg = readFileSync(new URL(name + '.svg', dir), 'utf8'); } catch {}
  check(`listed set ${name} has its index and sprite`, idx && svg.startsWith('<svg'));
  if (idx) check(`${name}: the index has the inventory's faces`, JSON.stringify(idx.faces) === JSON.stringify(table.sheets[name].faces));
  if (idx) check(`${name}: every face has its symbol`, idx.faces.every((f) => svg.includes(`<symbol id="${f}"`)));
}

// Every deck draws from the sheet the inventory gives it.
for (const [name, s] of Object.entries(table.sheets)) for (const d of s.decks) check(`deck ${d} draws from ${name}`, deckSheet(d) === name, deckSheet(d));
check('an unknown deck has no sheet', deckSheet('uno') === null);

// Slots and their sheets.
{
  const draw = { kind: 'draw', deck: 'italian', cards: ['cups-fante'] };
  const a = slotArt(draw, slots(draw)[0]);
  check('a card comes from its deck\'s sheet by its own code', a.sheet === 'latin' && a.face('cups-fante') === 'cups-fante');
  const roll = { kind: 'roll', dice: '1d6+1d20+2', rows: [{ dice: '1d6+1d20+2', terms: [{ sign: 1, count: 1, sides: 6, rolls: [5], value: 5 }, { sign: 1, count: 1, sides: 20, rolls: [17], value: 17 }, { sign: 1, value: 2 }], total: 24 }] };
  const s = slots(roll);
  const d6 = slotArt(roll, s[0]), d20 = slotArt(roll, s[1]);
  check('a six-sided die shows a cube face', d6.sheet === 'dice-cube' && d6.face('5') === 'd6-5' && !d6.die);
  check('other dice are drawn on the fly', d20.die === 20 && !d20.sheet);
  check('a plain number has no picture', slotArt(roll, s[2]) === null);
  check('a flip is a coin', slotArt({ kind: 'flip' }, slots({ kind: 'flip', outcome: 'heads' })[0]).die === 2);
  check('picks and shuffles have no picture', slotArt({ kind: 'pick' }, slots({ kind: 'pick', options: ['a', 'b'], picked: 1 })[0]) === null);
  check('every die of the inventory\'s cube faces', [1, 2, 3, 4, 5, 6].every((n) => table.sheets['dice-cube'].faces.includes(d6.face(String(n)))));
}

// A placeholder index in the shape pack.py writes.
const cards = { sheet: 'french', file: 'french.svg', cell: [176, 246], faces: ['spades-ace', 'hearts-queen', 'back'] };

{
  const s = sprite(cards, 'hearts-queen');
  check('a face is drawn at half its cell', s.width === 88 && s.height === 123, JSON.stringify(s));
  check('from its symbol in the set\'s sprite', s.href === '/shared/chance/french.svg#hearts-queen' && s.viewBox === '0 0 176 246', s.href);
  check('a face the sheet lacks has no sprite', sprite(cards, 'joker-red') === null);
  check('at full scale', sprite(cards, 'back', 1).width === 176);
}

// Dice drawn on the fly: a die of N sides is a regular N-gon, one corner up,
// from 3 to NGON_MAX_SIDES sides; above that a circle; two sides a coin.
{
  const parts = (n, v) => dieDrawing(n, v).parts;
  const shape = (n) => parts(n, 1)[0];
  check('the breakpoint is 30 sides', NGON_MAX_SIDES === 30);
  for (let n = 3; n <= NGON_MAX_SIDES; n++) {
    const [tag, a] = shape(n);
    const pts = tag === 'polygon' ? a.points.split(' ') : [];
    check(`a d${n} is a polygon of ${n} corners`, pts.length === n, `${tag} ${pts.length}`);
    const o = dieOutline(n);
    check(`a d${n} has a corner straight up`, Math.abs(o.points[0][0] - DIE_BOX / 2) < 0.01 && o.points.every((p) => p[1] >= o.points[0][1]));
    check(`a d${n} sits inside its box`, o.points.every(([x, y]) => x >= 1 && x <= DIE_BOX - 1 && y >= 1 && y <= DIE_BOX - 1));
  }
  for (const n of [31, 100, 1000]) check(`a d${n} is a circle`, shape(n)[0] === 'circle');
  check('every die is paper: one colour for all dice', [3, 4, 8, 10, 12, 13, 20, 30, 100, 1000].every((n) => shape(n)[1].fill === '#F5F2FF'));
  check('the edge is 2 px of ink with mitred corners', shape(20)[1].stroke === '#0D1026' && shape(20)[1]['stroke-width'] === 2 && shape(20)[1]['stroke-linejoin'] === 'miter');
  // The number fits the circle inside the shape, whatever its length.
  const fits = (n, v) => {
    const t = parts(n, v).find((p) => p[0] === 'text');
    const size = t[1]['font-size'];
    const w = 0.5 * size * String(v).length, hgt = 0.72 * size;
    const rho = dieOutline(n).inner - 1;
    return t[2] === String(v) && Math.hypot(w / 2, hgt / 2) <= rho + 0.01 && size > 6;
  };
  for (const [n, v] of [[3, 1], [4, 3], [20, 1], [20, 20], [30, 27], [100, 100], [1000, 1000], [1000, 7]]) check(`d${n} showing ${v}: the number fits`, fits(n, v));
  check('four figures set smaller than one', fitText(4, 20) < fitText(1, 20));
  check('the number is card black, Archivo 900 narrow', parts(20, 17)[1][1].fill === '#111111' && parts(20, 17)[1][1]['font-weight'] === 900 && parts(20, 17)[1][1].class === 'die-num');
  // Coins.
  check('a coin is a paper circle with an ink inner ring, its letter in black', shape(2)[0] === 'circle' && shape(2)[1].fill === '#F5F2FF' && parts(2, 1)[1][0] === 'circle' && parts(2, 1)[1][1].stroke === '#0D1026' && parts(2, 1).find((p) => p[0] === 'text')[1].fill === '#111111');
  check('1 is heads, 2 tails, and a flip names its face', coinFace(1) === 'heads' && coinFace('2') === 'tails' && coinFace('tails') === 'tails' && coinFace('heads') === 'heads');
  check('heads shows an H under a crown', parts(2, 1).some((p) => p[0] === 'text' && p[2] === 'H') && parts(2, 1).some((p) => p[1].class === 'coin-mark'));
  check('tails shows a T between two diamonds', parts(2, 2).some((p) => p[0] === 'text' && p[2] === 'T') && parts(2, 'tails').filter((p) => p[1].class === 'coin-mark').length === 2);
  check('the markup is one svg', dieSvg(20, 17).startsWith('<svg') && (dieSvg(20, 17).match(/<polygon/g) || []).length === 1 && dieSvg(20, 17).includes('>17</text>'));
  // A d2 reads as heads or tails with its number; the arithmetic is the same.
  const d2 = { kind: 'roll', dice: '2d2', rows: [{ dice: '2d2', terms: [{ sign: 1, count: 2, sides: 2, rolls: [1, 2], value: 3 }], total: 3 }] };
  const ds = slots(d2);
  check('a d2 says heads (1) and tails (2)', ds[0].text === 'heads (1)' && ds[1].text === 'tails (2)' && ds[0].key === '1' && ds[1].key === '2');
  check('the spoken result names the faces and the total', spokenResult(d2) === 'Rolled 2d2: heads (1), tails (2); total 3', spokenResult(d2));
  check('a coin\'s reel turns it over and over, ending on its face', ds[1].reelKeys.every((k, i, a) => i === 0 || k !== a[i - 1]) && ds[1].reelKeys.at(-1) === '2');
  const fl = slots({ kind: 'flip', outcome: 'tails' })[0];
  check('a flip\'s reel turns too, ending on its face', fl.reelKeys.every((k, i, a) => i === 0 || k !== a[i - 1]) && fl.reelKeys.at(-1) === 'tails' && fl.text === 'Tails');
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
setSheetsForTest(SHEETS, { svg: fakeSvg, die: (drawing) => Object.assign(fakeEl(), { drawing }) });
{
  const f = fakeFace('Q♥');
  const art = { sheet: { index: cards, url: BASE + 'french.svg' }, face: (k) => k, text: false };
  check('paint draws a card', paint(f, art, 'hearts-queen', 'Q♥') && f.has('art') && f.style.width === '88px' && f.style.height === '123px');
  check('as an svg using the symbol', f.firstChild === f._svg && f._svg.firstChild.attrs.href === '/shared/chance/french.svg#hearts-queen' && f._svg.attrs.viewBox === '0 0 176 246');
  check('the text stays in the element', f.textContent === 'Q♥');
  f.textContent = '7♠';
  paint(f, art, 'spades-ace', '7♠');
  check('a new frame puts the picture back and moves it', f.firstChild === f._svg && f._svg.firstChild.attrs.href.endsWith('#spades-ace') && f.textContent === '7♠');
  check('a frame the sheet lacks falls back to text', !paint(f, art, 'joker-white', 'JK') && !f.has('art') && f.style.minWidth === '3ch' && f.kids.every((k) => typeof k === 'string'));
  const d = fakeFace('17');
  check('paint draws a die on the fly', paint(d, { die: 20 }, '17', '17') && d.has('art-die') && d.firstChild === d._svg && d._svg.drawing.parts[0][0] === 'polygon' && d.style.width === '64px');
  d.textContent = '3';
  paint(d, { die: 20 }, '3', '3', false);
  check('each frame draws its own number', d.firstChild === d._svg && d._svg.drawing.parts[1][2] === '3' && d.textContent === '3');
  unpaint(d);
  check('unpaint restores the text face', !d.has('art') && !d.has('art-die') && d.firstChild === '3');
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
  // Dice drawn on the fly need no sheet at all.
  const r = { kind: 'roll', dice: '1d20+1d2', rows: [{ dice: '1d20+1d2', terms: [{ sign: 1, count: 1, sides: 20, rolls: [17], value: 17 }, { sign: 1, count: 1, sides: 2, rolls: [2], value: 2 }], total: 19 }] };
  setSheetsForTest([], {});
  const dl = slots(r);
  faces = dl.map((x) => fakeFace(x.text));
  await dress(r, faces, dl);
  check('a d20 and a d2 are drawn with no sheet', faces.every((f) => f.has('art-die')) && faces[0]._svg.drawing.parts[0][1].points.split(' ').length === 20 && faces[1]._svg.drawing.parts.some((p) => p[2] === 'T'));
  check('their words stay for screen readers', faces[1].textContent === 'tails (2)');
  const flip = { kind: 'flip', outcome: 'heads' };
  faces = [fakeFace('Heads')];
  await dress(flip, faces, slots(flip));
  check('a flip is drawn as the coin, heads up', faces[0].has('art-die') && faces[0]._svg.drawing.parts.some((p) => p[2] === 'H') && faces[0].textContent === 'Heads');

  // A reversed card is turned upside down once it has landed, in a tarot
  // reading and in any draw from a deck with orientation; an upright one
  // and a card still turning in the reel are not.
  const tarotIdx = JSON.parse(readFileSync(new URL('../internal/web/static/shared/chance/tarot.json', import.meta.url), 'utf8'));
  setSheetsForTest(['tarot'], { index: async () => tarotIdx, sprite: async () => {} });
  const reading = { kind: 'tarot', deck: 'tarot', spread: 'three', cards: ['major-0', 'cups-2', 'wands-ace'], reversed: [true, false, true] };
  const rl = slots(reading);
  faces = rl.map((x) => fakeFace(x.text));
  await dress(reading, faces, rl);
  check('a reversed card is drawn turned, an upright one is not', faces.map((f) => f.has('turned')).join() === 'true,false,true', faces.map((f) => f.has('turned')).join());
  check('a turned card keeps its text', faces[0].textContent === rl[0].text);
  paint(faces[0], faces[0]._art, 'cups-2', 'x', false);
  check('a card still turning in the reel is upright', !faces[0].has('turned') && faces[0].has('art'));
  paint(faces[0], faces[0]._art, 'major-0', rl[0].text);
  unpaint(faces[0]);
  check('unpaint takes the turn off with the picture', !faces[0].has('turned'));

  // Face down, a card shows the french sheet's back at its own size, never
  // turned; face up again, its picture, turned if reversed.
  const french = { sheet: 'french', file: 'french.svg', cell: [176, 246], faces: ['back', 'spades-ace'] };
  setSheetsForTest(['tarot', 'french'], { index: async (u) => (u.includes('french') ? french : tarotIdx), sprite: async () => {} });
  faces = rl.map((x) => fakeFace(x.text));
  faces[0]._down = true;
  await dress(reading, faces, rl);
  check('a card face down shows the card back of the french sheet', faces[0]._svg.firstChild.attrs.href === '/shared/chance/french.svg#back' && !faces[0].has('turned') && faces[0].textContent === rl[0].text);
  faces[0]._down = false;
  paintFace(faces[0]);
  check('turned face up it shows its picture, upside down when reversed', faces[0]._svg.firstChild.attrs.href.endsWith('#major-0') && faces[0].has('turned'));
  // A tile face down is its back, drawn by rule, with no sheet at all.
  setSheetsForTest([], {});
  const tiles = { kind: 'draw', deck: 'mahjong', cards: ['dots-1'] };
  faces = [fakeFace('1 Dots')];
  faces[0]._down = true;
  await dress(tiles, faces, slots(tiles));
  const tb = tileBackDrawing();
  check('a tile face down is the tile back drawn by rule', faces[0].has('art') && !!faces[0]._svg.drawing && faces[0]._svg.drawing.w === 144 && faces[0].style.width === '72px');
  check('the tile back: lavender thickness, ink edge, a lavender face of ink dots', tb.parts[0][1].fill === '#B9A0FF' && tb.parts[1][1].fill === '#0D1026' && tb.parts[2][1].fill === '#B9A0FF' && tb.parts.filter((p) => p[1].class === 'tile-dot').length > 40 && tb.parts.every((p) => ['#B9A0FF', '#0D1026'].includes(p[1].fill)));
  check('backs: tiles for mahjong, the card back for other decks, none for dice', backArt(tiles) === 'tile' && backArt(reading) === 'card' && backArt({ kind: 'draw', deck: 'standard' }) === 'card' && backArt({ kind: 'roll' }) === null);
  setSheetsForTest(['tarot'], { index: async () => tarotIdx, sprite: async () => {} });

  // A plain deck with no orientation never turns.
  setSheetsForTest(['french'], { index: async () => cards, sprite: async () => {} });
  const plain = { kind: 'draw', deck: 'standard', cards: ['spades-ace'] };
  faces = [fakeFace('A♠')];
  await dress(plain, faces, slots(plain));
  check('a card from a deck without orientation is never turned', faces[0].has('art') && !faces[0].has('turned'));
  setSheetsForTest(SHEETS, {});
}

console.log(`${failures ? 'FAIL' : 'ok  '} chanceart: ${checks - failures} of ${checks} checks`);
if (failures) process.exitCode = 1;
