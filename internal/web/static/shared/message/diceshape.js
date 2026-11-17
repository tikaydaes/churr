// Dice other than the six-sided one, and coins, drawn on the fly
// (docs/ART.md): a die of N sides is a regular N-sided polygon, one corner
// up, with the number it shows inside; past NGON_MAX_SIDES sides it is a
// circle. A two-sided die is a coin, as is the result of /flip: heads for
// 1, tails for 2. Nothing here touches the page: dieDrawing describes the
// picture, dieSvg writes it as markup (for tests and the contact sheet),
// and chanceart.js builds it as elements. The app draws the same
// (lib/ui/message/dice_shape.dart), with the same constants.

// A die of more sides than this is drawn as a circle.
export const NGON_MAX_SIDES = 30;

// The drawing's box, in CSS pixels: the size of a cube face.
export const DIE_BOX = 64;
// The circle the polygon's corners sit on, the edge's width, and the
// room kept between the number and the edge.
export const DIE_RADIUS = 29;
export const DIE_EDGE = 2;
const NUMBER_MARGIN = 2;

// Card colours, the same on every theme (tools/art-gen/spec.py).
export const CARD = {
  paper: '#F5F2FF', ink: '#0D1026', black: '#111111',
  cyan: '#5DF0E4', yellow: '#FFD95E', pink: '#FF5CCF', lavender: '#B9A0FF',
};

// The number's type: Archivo 900 at 72% width, where a figure is about
// 0.5 em wide and 0.72 em tall.
const FIGURE_W = 0.5, CAP_H = 0.72;

// A coin's face, in the box's units: the inner ring, the letter's size
// and baseline, and its marks: a cut-corner crown over the H (by its
// base), small diamonds either side of the T (by their centres' distance
// from the middle). Letter and marks sit together in the middle.
export const COIN = {
  ring: 24, size: 28,
  heads: { baseline: 46.1, crown: 23.9 },
  tails: { baseline: 42.1, diamonds: 13 },
};

const r2 = (v) => Math.round(v * 100) / 100;

// dieFill is a die's colour: paper for every die and the coin, as the
// six-sided cube is (ruling R87).
export function dieFill() {
  return CARD.paper;
}

// dieOutline is the die's edge: { circle: true, cx, cy, r } or
// { points: [[x, y], ...] }, with inner, the radius of the circle that fits
// inside it, and its centre cx, cy. A polygon is set in the middle of the
// box by its own height.
export function dieOutline(sides) {
  const c = DIE_BOX / 2, R = DIE_RADIUS;
  if (sides === 2 || sides > NGON_MAX_SIDES || sides < 3) return { circle: true, cx: c, cy: c, r: R, inner: R };
  const pts = [];
  for (let i = 0; i < sides; i++) {
    const a = -Math.PI / 2 + (2 * Math.PI * i) / sides;
    pts.push([c + R * Math.cos(a), c + R * Math.sin(a)]);
  }
  const ys = pts.map((p) => p[1]);
  const dy = c - (Math.min(...ys) + Math.max(...ys)) / 2;
  return { points: pts.map(([x, y]) => [r2(x), r2(y + dy)]), cx: c, cy: r2(c + dy), inner: R * Math.cos(Math.PI / sides) };
}

// fitText is the font size at which text of `chars` characters, each
// `w` em wide and CAP_H em tall, fits the largest box of its shape inside
// a circle of radius rho.
export function fitText(chars, rho, w = FIGURE_W) {
  const a = (w * Math.max(1, chars)) / CAP_H;
  const h = (2 * rho) / Math.sqrt(1 + a * a);
  return r2(h / CAP_H);
}

// coinFace is the face a coin shows: 'heads' or 'tails', from a d2's 1
// or 2 or a flip's outcome.
export function coinFace(v) {
  const s = String(v);
  return s === '2' || s === 'tails' ? 'tails' : 'heads';
}

// coinMarks is the corners of each of a coin face's marks.
export function coinMarks(face) {
  const x = DIE_BOX / 2;
  if (face === 'heads') {
    const y = COIN.heads.crown, w = 7, h = 5;
    return [[[x - w, y], [x - w, y - h], [x - w / 2, y - h / 2], [x, y - h - 1], [x + w / 2, y - h / 2], [x + w, y - h], [x + w, y]]];
  }
  const y = DIE_BOX / 2, d = 2.5;
  return [-1, 1].map((side) => {
    const c = x + side * COIN.tails.diamonds;
    return [[c, y - d], [c + d, y], [c, y + d], [c - d, y]];
  });
}

// dieDrawing describes the picture of a die of `sides` showing `value`
// (a /flip result is a die of 2 showing 'heads' or 'tails'): the box, and
// its parts as [tag, attributes, text] in drawing order.
export function dieDrawing(sides, value) {
  const o = dieOutline(sides);
  const edge = { fill: dieFill(sides), stroke: CARD.ink, 'stroke-width': DIE_EDGE, 'stroke-linejoin': 'miter' };
  const parts = [];
  if (o.circle) parts.push(['circle', { cx: o.cx, cy: o.cy, r: o.r, ...edge }]);
  else parts.push(['polygon', { points: o.points.map((p) => p.join(',')).join(' '), ...edge }]);
  const text = (content, size, y) => ['text', {
    class: 'die-num', x: o.cx, y: r2(y), 'font-size': size, 'text-anchor': 'middle',
    fill: CARD.black, 'font-family': 'Archivo', 'font-weight': 900, 'font-stretch': '72%',
  }, content];
  if (sides === 2) {
    // A coin: an inner ring, and a large letter with a small mark by rule,
    // a cut-corner crown over the H, a diamond under the T.
    parts.push(['circle', { cx: o.cx, cy: o.cy, r: COIN.ring, fill: 'none', stroke: CARD.ink, 'stroke-width': 1.5 }]);
    const face = coinFace(value);
    const pts = (list) => list.map((p) => p.map(r2).join(',')).join(' ');
    for (const m of coinMarks(face)) parts.push(['polygon', { class: 'coin-mark', points: pts(m), fill: CARD.ink }]);
    parts.push(text(face === 'heads' ? 'H' : 'T', COIN.size, COIN[face].baseline));
  } else {
    const s = String(value);
    const rho = o.inner - DIE_EDGE / 2 - NUMBER_MARGIN;
    const size = fitText([...s].length, rho);
    parts.push(text(s, size, o.cy + (CAP_H * size) / 2));
  }
  return { box: DIE_BOX, parts };
}

const esc = (s) => String(s).replace(/[&<>"]/g, (ch) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[ch]);

// dieSvg writes a drawing as SVG markup.
export function dieSvg(sides, value) {
  const d = dieDrawing(sides, value);
  const body = d.parts.map(([tag, attrs, content]) => {
    const a = Object.entries(attrs).map(([k, v]) => ` ${k}="${esc(v)}"`).join('');
    return content == null ? `<${tag}${a}/>` : `<${tag}${a}>${esc(content)}</${tag}>`;
  }).join('');
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${d.box} ${d.box}" width="${d.box}" height="${d.box}">${body}</svg>`;
}
