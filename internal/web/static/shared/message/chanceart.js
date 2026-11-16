// The chance artwork (docs/ART.md): cards, tiles and dice drawn from one
// sprite sheet per set, in place of the text faces of a chance result.
// A sheet is fetched the first time something from its set is shown, and
// only if this server has it (shared/chance/sheets.js, written by
// tools/art-gen/pack.py). With no sheet, or one that fails to load, the
// slot keeps its text face, which is also what a screen reader reads.

import { SHEETS } from '../chance/sheets.js';

export const BASE = '/shared/chance/';
// Sheets are drawn at twice the pixels of the screen size.
export const SCALE = 0.5;

// The sheet each deck draws from (testdata/art/sheets.json).
const DECK_SHEET = {
  standard: 'french', jokers: 'french', jokers3: 'french', piquet: 'french', durak: 'french', euchre: 'french',
  pinochle: 'french', canasta: 'french', shoe2: 'french', shoe4: 'french', shoe6: 'french', shoe8: 'french',
  tarot: 'tarot', spanish: 'latin', italian: 'latin', german: 'german', austrian: 'german', swiss: 'swiss',
  hanafuda: 'hanafuda', mahjong: 'mahjong',
};
export const deckSheet = (deck) => DECK_SHEET[deck] || null;

// slotArt says where a slot's pictures come from: { sheet, face(key),
// text } or null. face turns a frame's key (a card, or a die's value) into
// a face of the sheet; text says the slot's own text is set over the
// picture (a polyhedral die's number).
export function slotArt(c, x) {
  if (!x || x.still) return null;
  if (c.kind === 'draw' || c.kind === 'tarot') {
    const sheet = deckSheet(c.deck);
    return sheet ? { sheet, face: (k) => k, text: false } : null;
  }
  if (c.kind === 'roll' && x.sides) {
    if (x.sides === 6) return { sheet: 'dice-cube', face: (k) => 'd6-' + k, text: false };
    return { sheet: 'dice-poly', face: () => null, text: true, sides: x.sides };
  }
  return null;
}

// polyFace is the shape of a die of this many sides in a dice-poly index.
export function polyFace(index, sides) {
  return (index.sides && index.sides[String(sides)]) || index.other || null;
}

// sprite is how to draw one face of a sheet at a scale: the box and the
// background's size and position, in CSS pixels, or null when the sheet
// lacks the face.
export function sprite(index, face, scale = SCALE) {
  const at = index && index.faces && index.faces[face];
  if (!at) return null;
  const [cw, ch] = index.cell;
  const [sw, sh] = index.size;
  const r = (v) => Math.round(v * scale * 100) / 100;
  return {
    width: r(cw), height: r(ch),
    size: `${r(sw)}px ${r(sh)}px`,
    position: `${r(-at[0] * cw)}px ${r(-at[1] * ch)}px`,
  };
}

// textBox is where a die's number goes over its shape, as padding of the
// face box, and the font size that fits the number in it.
export function textBox(index, face, text, scale = SCALE) {
  const b = index.text && index.text[face];
  if (!b) return null;
  const [cw, ch] = index.cell;
  const r = (v) => Math.round(v * scale * 100) / 100;
  const chars = Math.max(1, [...String(text)].length);
  // Archivo 900 at 72% width: a figure is about 0.5 em wide, caps 0.72 em tall.
  const size = Math.min(b.h / 0.72, b.w / (0.5 * chars));
  return {
    padding: `${r(b.y)}px ${r(cw - b.x - b.w)}px ${r(ch - b.y - b.h)}px ${r(b.x)}px`,
    fontSize: r(size) + 'px',
  };
}

// The sheets, each a promise of { index, url } or null, kept for the page's
// life. load and image are the fetching, replaceable by tests.
const cache = new Map();
const io = {
  async index(url) { const r = await fetch(url, { credentials: 'same-origin' }); if (!r.ok) throw new Error(r.status); return r.json(); },
  image(url) {
    return new Promise((resolve, reject) => { const im = new Image(); im.onload = () => resolve(); im.onerror = reject; im.src = url; });
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
        const url = BASE + (index.image || name + '.webp');
        await io.image(url);
        return { index, url };
      } catch { return null; }
    })());
  }
  return cache.get(name);
}

// paint draws one frame on a slot face: the picture of key, and, for a
// die shape, the text. It returns false when the sheet lacks the face, and
// the face then shows its text as before.
export function paint(f, art, key, text) {
  const { index, url } = art.sheet;
  const face = art.text ? polyFace(index, art.sides) : art.face(key);
  const s = face && sprite(index, face);
  if (!s) { unpaint(f); return false; }
  f.classList.add('art');
  f.classList.toggle('art-num', !!art.text);
  const st = f.style;
  st.minWidth = '';
  st.width = s.width + 'px';
  st.height = s.height + 'px';
  st.backgroundImage = `url("${url}")`;
  st.backgroundSize = s.size;
  st.backgroundPosition = s.position;
  if (art.text) {
    const b = textBox(index, face, text);
    if (b) { st.padding = b.padding; st.fontSize = b.fontSize; }
  } else {
    st.padding = '';
    st.fontSize = '';
  }
  return true;
}

export function unpaint(f) {
  if (!f.classList.contains('art')) return;
  f.classList.remove('art', 'art-num');
  for (const p of ['width', 'height', 'backgroundImage', 'backgroundSize', 'backgroundPosition', 'padding', 'fontSize']) f.style[p] = '';
  if (f.dataset.minWidth) f.style.minWidth = f.dataset.minWidth;
}

// dress puts pictures on the slot faces of a drawn result once their
// sheets have loaded. faces are the result's .slot-face elements, list its
// slots. Each face keeps what it needs to change picture while it spins.
export function dress(c, faces, list) {
  const wanted = new Map();
  list.forEach((x, i) => {
    const a = slotArt(c, x);
    if (a && faces[i]) wanted.set(i, a);
  });
  const sheets = [...new Set([...wanted.values()].map((a) => a.sheet))];
  return Promise.all(sheets.map((n) => loadSheet(n).then((s) => [n, s]))).then((got) => {
    const loaded = Object.fromEntries(got);
    for (const [i, a] of wanted) {
      const sheet = loaded[a.sheet];
      if (!sheet) continue;
      const f = faces[i];
      f._art = { ...a, sheet };
      paint(f, f._art, list[i].key, f.textContent);
    }
  });
}
