// Where the tiles of a call go, and which video the client asks the server
// for at what size (docs/CALLS.md section 7, docs/CLIENTS.md section 9.14).
//
// Pure: no DOM, no store. The call view measures its area and hands the
// numbers in; the tests run this file under Node.

// The gap between tiles, in CSS pixels.
export const GAP = 8;
// A video tile's shape.
export const ASPECT = 16 / 9;
// The most video tracks one client may subscribe to (CALLS.md section 12).
export const MAX_SUBSCRIPTIONS = 16;
// Below this width the call view is laid out for a phone.
export const NARROW = 600;

// grid finds the number of columns that makes n tiles of one shape as large
// as they can be inside width × height. Ties go to fewer columns.
export function grid(n, width, height, aspect = ASPECT, gap = GAP) {
  if (n <= 0 || width <= 0 || height <= 0) return { cols: 0, rows: 0, w: 0, h: 0 };
  let best = null;
  for (let cols = 1; cols <= n; cols++) {
    const rows = Math.ceil(n / cols);
    const cellW = (width - gap * (cols - 1)) / cols;
    const cellH = (height - gap * (rows - 1)) / rows;
    if (cellW <= 0 || cellH <= 0) continue;
    // The tile keeps its shape inside its cell.
    const w = Math.min(cellW, cellH * aspect);
    const h = w / aspect;
    if (!best || w > best.w + 0.5) best = { cols, rows, w, h };
  }
  if (!best) return { cols: 1, rows: n, w: 0, h: 0 };
  return { cols: best.cols, rows: best.rows, w: Math.floor(best.w), h: Math.floor(best.h) };
}

// place lays out n tiles of the size grid chose, centred in the area, the
// last row centred too. Returns [{ x, y, w, h }].
function place(n, x0, y0, width, height, g, gap = GAP) {
  const out = [];
  const totalH = g.rows * g.h + (g.rows - 1) * gap;
  const top = y0 + Math.max(0, Math.floor((height - totalH) / 2));
  for (let i = 0; i < n; i++) {
    const row = Math.floor(i / g.cols);
    const inRow = row === g.rows - 1 ? n - row * g.cols : g.cols;
    const col = i - row * g.cols;
    const rowW = inRow * g.w + (inRow - 1) * gap;
    const left = x0 + Math.max(0, Math.floor((width - rowW) / 2));
    out.push({ x: left + col * (g.w + gap), y: top + row * (g.h + gap), w: g.w, h: g.h });
  }
  return out;
}

// order puts the people of a call in the order their tiles are given
// space: the pinned one, then the current speakers loudest first, then the
// rest in the order they joined. The reader is never among them; their
// own picture is a preview of its own.
export function order(people, { speakers = [], pinned = null } = {}) {
  const rank = (p) => {
    if (p.account_id === pinned) return -1000;
    const s = speakers.indexOf(p.account_id);
    return s >= 0 ? -100 + s : 0;
  };
  return people
    .map((p, i) => ({ p, i }))
    .sort((a, b) => rank(a.p) - rank(b.p) || a.i - b.i)
    .map((x) => x.p);
}

// arrange is the whole layout of the call view.
//
//   people    the other participants: [{ account_id, camera, screen }],
//             camera and screen being track ids (or null) with
//             `cameraOn` saying whether the camera is unmuted
//   speakers  account ids, loudest first
//   pinned    an account id the reader pinned, or null
//   width, height  the area in CSS pixels
//   alone     the reader is the only one in the call
//
// It returns { mode, tiles, preview, overflow }:
//   mode      'grid', 'speaker' (someone is pinned) or 'screen'
//   tiles     [{ key, account_id, source: 'camera' | 'screen', main, x, y, w, h }]
//   preview   where the reader's own picture goes: { x, y, w, h }, or a
//             tile in the grid when they are alone (then `self` is true)
//   overflow  how many people did not get a tile of their own
export function arrange({ people = [], speakers = [], pinned = null, width, height, maxTiles = MAX_SUBSCRIPTIONS }) {
  const narrow = width < NARROW;
  const sharing = people.find((p) => p.screen);
  const sorted = order(people, { speakers, pinned });
  const tiles = [];
  let overflow = 0;
  const previewSize = (w, h) => {
    const pw = Math.round(Math.max(96, Math.min(narrow ? 120 : 240, w * 0.22)));
    return { w: pw, h: Math.round(pw / ASPECT) };
  };

  if (!people.length) {
    // Alone: the reader's own picture fills the area, as a tile.
    const g = grid(1, width, height);
    const [spot] = place(1, 0, 0, width, height, g);
    return { mode: 'grid', tiles: [], preview: { ...spot, self: true }, overflow: 0 };
  }

  if (sharing || (pinned && people.some((p) => p.account_id === pinned))) {
    // One main tile, with everyone else in a strip: beside it on a wide
    // screen, under it on a narrow one.
    const main = sharing || sorted[0];
    const mainSource = sharing ? 'screen' : 'camera';
    const rest = sorted.filter((p) => mainSource === 'screen' || p.account_id !== main.account_id);
    const room = Math.max(0, maxTiles - 1);
    const shown = rest.slice(0, room);
    overflow = rest.length - shown.length;
    let mainBox, strip;
    if (!shown.length) {
      mainBox = { x: 0, y: 0, w: width, h: height };
    } else if (narrow) {
      const stripH = Math.min(110, Math.floor(height * 0.24));
      mainBox = { x: 0, y: 0, w: width, h: height - stripH - GAP };
      strip = { x: 0, y: height - stripH, w: width, h: stripH, row: true };
    } else {
      const stripW = Math.min(280, Math.floor(width * 0.24));
      mainBox = { x: 0, y: 0, w: width - stripW - GAP, h: height };
      strip = { x: width - stripW, y: 0, w: stripW, h: height, row: false };
    }
    // The main tile keeps the shape of what it shows when that is known; a
    // screen fills the box it is given.
    const mg = mainSource === 'screen' ? { w: mainBox.w, h: mainBox.h } : fit(mainBox.w, mainBox.h);
    tiles.push({ key: main.account_id + ':' + mainSource, account_id: main.account_id, source: mainSource, main: true,
      x: mainBox.x + Math.floor((mainBox.w - mg.w) / 2), y: mainBox.y + Math.floor((mainBox.h - mg.h) / 2), w: mg.w, h: mg.h });
    if (strip) {
      // Strip tiles are all the same size, as many as fit along the strip.
      const along = strip.row ? strip.w : strip.h;
      const across = strip.row ? strip.h : strip.w;
      const tw = strip.row ? Math.floor(across * ASPECT) : across;
      const th = strip.row ? across : Math.floor(across / ASPECT);
      const step = (strip.row ? tw : th) + GAP;
      const fits = Math.max(1, Math.floor((along + GAP) / step));
      const visible = shown.slice(0, fits);
      overflow += shown.length - visible.length;
      visible.forEach((p, i) => {
        tiles.push({ key: p.account_id + ':camera', account_id: p.account_id, source: 'camera', main: false,
          x: strip.row ? strip.x + i * step : strip.x, y: strip.row ? strip.y : strip.y + i * step, w: tw, h: th });
      });
    }
    const pv = previewSize(width, height);
    return { mode: sharing ? 'screen' : 'speaker', tiles, preview: { x: mainBox.x + mainBox.w - pv.w - GAP, y: mainBox.y + mainBox.h - pv.h - GAP, ...pv }, overflow };
  }

  // A grid of everyone, the loudest first.
  const shown = sorted.slice(0, maxTiles);
  overflow = sorted.length - shown.length;
  const g = grid(shown.length, width, height);
  const spots = place(shown.length, 0, 0, width, height, g);
  shown.forEach((p, i) => tiles.push({ key: p.account_id + ':camera', account_id: p.account_id, source: 'camera', main: shown.length === 1, ...spots[i] }));
  const pv = previewSize(width, height);
  return { mode: 'grid', tiles, preview: { x: width - pv.w - GAP, y: height - pv.h - GAP, ...pv }, overflow };
}

// fit is the largest 16:9 box inside width × height.
function fit(width, height) {
  const w = Math.min(width, height * ASPECT);
  return { w: Math.floor(w), h: Math.floor(w / ASPECT) };
}

// subscriptions turns a layout into the list `call.subscribe` sends: every
// tile that is showing live video, at its size in physical pixels. A camera
// that is off is not asked for (its tile shows the person's avatar); the
// list is empty when nothing is on screen (the compact bar, a hidden page).
// Screens first, then the main tile, then the rest, so the cap of sixteen
// drops the smallest.
export function subscriptions(tiles, people, { dpr = 1, showing = true, max = MAX_SUBSCRIPTIONS } = {}) {
  if (!showing) return [];
  const byId = new Map(people.map((p) => [p.account_id, p]));
  const out = [];
  const seen = new Set();
  const ranked = [...tiles].sort((a, b) => (b.source === 'screen') - (a.source === 'screen') || (b.main - a.main) || (b.w * b.h - a.w * a.h));
  for (const t of ranked) {
    const p = byId.get(t.account_id);
    if (!p) continue;
    const id = t.source === 'screen' ? p.screen : (p.cameraOn ? p.camera : null);
    if (!id || seen.has(id)) continue;
    seen.add(id);
    out.push({ track_id: id, width: Math.max(1, Math.round(t.w * dpr)), height: Math.max(1, Math.round(t.h * dpr)) });
    if (out.length >= max) break;
  }
  return out;
}

// sameSubscriptions says whether two lists would tell the server the same.
// A change of a few pixels is not worth a message: sizes within 10% count
// as the same, since the server picks among three layers anyway.
export function sameSubscriptions(a, b) {
  if (!a || !b || a.length !== b.length) return false;
  const key = (l) => [...l].sort((x, y) => (x.track_id < y.track_id ? -1 : 1));
  const x = key(a), y = key(b);
  for (let i = 0; i < x.length; i++) {
    if (x[i].track_id !== y[i].track_id) return false;
    if (Math.abs(x[i].height - y[i].height) > Math.max(8, y[i].height * 0.1)) return false;
    if (Math.abs(x[i].width - y[i].width) > Math.max(8, y[i].width * 0.1)) return false;
  }
  return true;
}

// expectedLayer is the layer the server should pick for a tile of this
// height when every layer exists (CALLS.md section 7, step 1): the
// smallest whose height is at least 80% of the tile's. Used to label tiles
// and by the tests to check the sizes sent mean what they should.
export const LAYER_HEIGHTS = { q: 180, h: 360, f: 720 };
export function expectedLayer(tileHeight) {
  for (const l of ['q', 'h', 'f']) if (LAYER_HEIGHTS[l] >= 0.8 * tileHeight) return l;
  return 'f';
}
