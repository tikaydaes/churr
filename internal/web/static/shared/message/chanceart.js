// The chance artwork (docs/ART.md): cards, tiles and six-sided dice drawn
// from one SVG sprite per set, in place of the text faces of a chance
// result. Each face is <svg><use href="/shared/chance/<set>.svg#<face>">
// </svg>; the browser fetches the sprite once. A set is used only if this
// server has it (shared/chance/sheets.js, written by tools/art-gen/pack.py)
// and its index and sprite load; otherwise the slot keeps its text face,
// which is also what a screen reader reads. Every other die, and a coin,
// is drawn on the fly (types/dice-roll/diceshape.js) and needs no sprite.
//
// Cards and tiles that arrive live lie face down before they are turned
// over (chance.js): a card shows the card back of the french sprite, at
// its own deck's size, and a tile the tile back, drawn here by rule.

import { SHEETS } from '../chance/sheets.js';
import { dieDrawing, CARD } from '../types/dice-roll/diceshape.js';

export const BASE = '/shared/chance/';
// A face is shown at half its cell (the cell is in sprite units).
export const SCALE = 0.5;

// The sheet each deck draws from (testdata/art/sheets.json).
const DECK_SHEET = {
  standard: 'french', jokers: 'french', jokers3: 'french', piquet: 'french', durak: 'french', euchre: 'french',
  pinochle: 'french', canasta: 'french', shoe2: 'french', shoe4: 'french', shoe6: 'french', shoe8: 'french',
  tarot: 'tarot', spanish: 'latin', italian: 'latin', german: 'german', austrian: 'german', swiss: 'swiss',
  hanafuda: 'hanafuda', mahjong: 'mahjong',
};
export const deckSheet = (deck) => DECK_SHEET[deck] || null;

// slotArt says where a slot's pictures come from: { sheet, face(key) }
// for a sprite, where face turns a frame's key (a card, or a die's value)
// into a face of the sheet; { die: sides } for a die or coin drawn on the
// fly, whose frame's key is the value it shows; or null.
export function slotArt(c, x) {
  if (!x || x.still) return null;
  if (c.kind === 'draw' || c.kind === 'tarot') {
    const sheet = deckSheet(c.deck);
    return sheet ? { sheet, face: (k) => k } : null;
  }
  if (c.kind === 'roll' && x.sides) {
    if (x.sides === 6) return { sheet: 'dice-cube', face: (k) => 'd6-' + k };
    return { die: x.sides };
  }
  if (c.kind === 'flip') return { die: 2 };
  return null;
}

// sprite is how to draw one face of a sheet: the box in CSS pixels and
// the address of its symbol, or null when the sheet lacks the face.
export function sprite(index, face, scale = SCALE) {
  if (!index || !index.faces || !index.faces.includes(face)) return null;
  const [cw, ch] = index.cell;
  const r = (v) => Math.round(v * scale * 100) / 100;
  return { width: r(cw), height: r(ch), href: `${BASE}${index.file}#${face}`, viewBox: `0 0 ${cw} ${ch}` };
}

// The sheets, each a promise of { index, url } or null, kept for the page's
// life. io holds the fetching and the making of elements, replaceable by
// tests.
const cache = new Map();
const SVG = 'http://www.w3.org/2000/svg';
const io = {
  async index(url) { const r = await fetch(url, { credentials: 'same-origin' }); if (!r.ok) throw new Error(r.status); return r.json(); },
  // The sprite is fetched once here, so a missing or broken file falls back
  // to text; the <use> elements then find it in the browser's cache.
  async sprite(url) { const r = await fetch(url, { credentials: 'same-origin' }); if (!r.ok) throw new Error(r.status); await r.text(); },
  svg() {
    const el = document.createElementNS(SVG, 'svg');
    el.setAttribute('class', 'art-svg');
    el.setAttribute('aria-hidden', 'true');
    el.setAttribute('focusable', 'false');
    el.append(document.createElementNS(SVG, 'use'));
    return el;
  },
  // A die drawn on the fly, from dieDrawing, or a tile's back
  // (tileBackDrawing).
  die(d) {
    const el = document.createElementNS(SVG, 'svg');
    el.setAttribute('class', 'art-svg');
    el.setAttribute('aria-hidden', 'true');
    el.setAttribute('focusable', 'false');
    el.setAttribute('viewBox', `0 0 ${d.w || d.box} ${d.h || d.box}`);
    for (const [tag, attrs, text] of d.parts) {
      const p = document.createElementNS(SVG, tag);
      for (const [k, v] of Object.entries(attrs)) p.setAttribute(k, String(v));
      if (text != null) p.textContent = text;
      el.append(p);
    }
    return el;
  },
};
const listed = { names: SHEETS };

// setSheetsForTest replaces the list of sheets and the fetching.
export function setSheetsForTest(names, fetchers) {
  listed.names = names;
  cache.clear();
  if (fetchers) Object.assign(io, fetchers);
}

// loadSheet fetches a sheet's index and picture once.
export function loadSheet(name) {
  if (!name || !listed.names.includes(name)) return Promise.resolve(null);
  if (!cache.has(name)) {
    cache.set(name, (async () => {
      try {
        const index = await io.index(BASE + name + '.json');
        const url = BASE + (index.file || name + '.svg');
        await io.sprite(url);
        return { index, url };
      } catch { return null; }
    })());
  }
  return cache.get(name);
}

// paint draws one frame on a slot face: the picture of key. A reversed card (art.reversed, from a deck
// drawn with orientation) is turned upside down once it has landed: in the
// static drawing and the reel's last frame, not while the reel turns. It
// returns false when the sheet lacks the face, and the face then shows its
// text as before.
export function paint(f, art, key, text, landed = true) {
  if (art.die) return paintDie(f, art.die, key);
  const { index } = art.sheet;
  const face = art.face(key);
  const s = face && sprite(index, face);
  if (!s) { unpaint(f); return false; }
  f.classList.add('art');
  f.classList.remove('art-die');
  f.classList.toggle('turned', !!(art.reversed && landed));
  const st = f.style;
  st.minWidth = '';
  st.width = s.width + 'px';
  st.height = s.height + 'px';
  useSvg(f);
  f._svg.setAttribute('viewBox', s.viewBox);
  f._svg.removeAttribute && f._svg.removeAttribute('preserveAspectRatio');
  f._svg.firstChild.setAttribute('href', s.href);
  // Setting the text (the reel does) takes the picture out; put it back.
  if (f.firstChild !== f._svg) f.prepend(f._svg);
  return true;
}

// useSvg gives a face the <svg><use> a sprite is drawn with, in place of
// a drawing made on the fly.
function useSvg(f) {
  if (f._svg && f._svgKind === 'use') return;
  if (f._svg && f._svg.parentNode === f) f._svg.remove();
  f._svg = io.svg();
  f._svgKind = 'use';
}

// paintDie draws a die of `sides` (2: a coin) showing `value`, made anew
// for each frame: only the number, or the coin's face, changes.
function paintDie(f, sides, value) {
  const d = dieDrawing(sides, value);
  f.classList.add('art', 'art-die');
  const st = f.style;
  st.minWidth = '';
  st.width = d.box + 'px';
  st.height = d.box + 'px';
  drawn(f, io.die(d));
  return true;
}

function drawn(f, el) {
  if (f._svg && f._svg.parentNode === f) f._svg.remove();
  f._svg = el;
  f._svgKind = 'drawn';
  f.prepend(el);
}

// The tile back, in the units of the mahjong sheet's cell: the tile's
// lavender thickness, its ink edge and, inside, the card back's lavender
// with a straight grid of round ink dots.
export const TILE = { w: 144, h: 192, scale: SCALE };
export function tileBackDrawing() {
  const parts = [
    ['path', { fill: CARD.lavender, d: 'M8 8L130 8L144 22L144 192L8 192Z' }],
    ['path', { fill: CARD.ink, d: 'M0 0L122 0L136 14L136 184L0 184Z' }],
    ['path', { class: 'tile-back', fill: CARD.lavender, d: 'M4 4L120.34 4L132 15.66L132 180L4 180Z' }],
  ];
  // Dots 14 apart, clear of the cut corner.
  for (let y = 14; y <= 172; y += 14) for (let x = 12; x <= 124; x += 14) {
    if (x > 112 && y < 28) continue;
    parts.push(['circle', { class: 'tile-dot', cx: x, cy: y, r: 3, fill: CARD.ink }]);
  }
  return { w: TILE.w, h: TILE.h, parts };
}

// backArt is what a slot shows face down: 'tile' for a mahjong tile,
// 'card' for a card of any other deck with artwork, or null.
export function backArt(c) {
  if (c.kind !== 'draw' && c.kind !== 'tarot') return null;
  const sheet = deckSheet(c.deck);
  return !sheet ? null : sheet === 'mahjong' ? 'tile' : 'card';
}

// paintBack draws a face down: the tile back, or the french sheet's card
// back stretched to the card's own size. Without the french sheet the
// face stays text, shown face down by the page's style (.slot.down).
function paintBack(f) {
  if (f._back === 'tile') {
    const d = tileBackDrawing();
    f.classList.add('art');
    f.classList.remove('art-die', 'turned');
    Object.assign(f.style, { minWidth: '', width: d.w * TILE.scale + 'px', height: d.h * TILE.scale + 'px' });
    drawn(f, io.die(d));
    return true;
  }
  const back = f._back && f._back.index && sprite(f._back.index, 'back');
  if (!back) { unpaint(f); return false; }
  const own = f._art && f._art.sheet && f._art.sheet.index ? sprite(f._art.sheet.index, f._art.sheet.index.faces[0]) : null;
  const size = own || back;
  f.classList.add('art');
  f.classList.remove('art-die', 'turned');
  Object.assign(f.style, { minWidth: '', width: size.width + 'px', height: size.height + 'px' });
  useSvg(f);
  f._svg.setAttribute('viewBox', back.viewBox);
  f._svg.setAttribute('preserveAspectRatio', 'none');
  f._svg.firstChild.setAttribute('href', back.href);
  if (f.firstChild !== f._svg) f.prepend(f._svg);
  return true;
}

// paintFace draws a card's face as it now lies: its back while face down
// (f._down), else its picture, landed, or its text.
export function paintFace(f) {
  if (f._down) return paintBack(f);
  if (f._art && f._art.sheet) return paint(f, f._art, f._key, f.textContent, true);
  unpaint(f);
  return false;
}

export function unpaint(f) {
  if (!f.classList.contains('art')) return;
  f.classList.remove('art', 'art-die', 'turned');
  for (const p of ['width', 'height']) f.style[p] = '';
  if (f._svg && f._svg.parentNode === f) f._svg.remove();
  f._svg = null;
  f._svgKind = null;
  if (f.dataset.minWidth) f.style.minWidth = f.dataset.minWidth;
}

// dress puts pictures on the slot faces of a drawn result once their
// sheets have loaded. faces are the result's .slot-face elements, list its
// slots. Each face keeps what it needs to change picture while it spins.
export function dress(c, faces, list) {
  const wanted = new Map();
  const back = backArt(c);
  list.forEach((x, i) => {
    const a = slotArt(c, x);
    if (!a || !faces[i]) return;
    faces[i]._key = x.key;
    if (back === 'tile') { faces[i]._back = 'tile'; if (faces[i]._down) paintBack(faces[i]); }
    // A die drawn on the fly needs nothing fetched.
    if (a.die) {
      faces[i]._art = a;
      paint(faces[i], a, list[i].key, faces[i].textContent);
    } else wanted.set(i, a);
  });
  const sheets = [...new Set([...wanted.values()].map((a) => a.sheet).concat(back === 'card' && wanted.size ? ['french'] : []))];
  return Promise.all(sheets.map((n) => loadSheet(n).then((s) => [n, s]))).then((got) => {
    const loaded = Object.fromEntries(got);
    for (const [i, a] of wanted) {
      const sheet = loaded[a.sheet];
      const f = faces[i];
      if (back === 'card' && loaded.french) f._back = loaded.french;
      if (sheet) f._art = { ...a, sheet, reversed: !!list[i].reversed };
      // A face down card shows its back; one face up its picture.
      if (f._down) paintBack(f);
      else if (sheet) paint(f, f._art, list[i].key, f.textContent);
    }
  });
}
