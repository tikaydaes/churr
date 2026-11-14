// Checks every pairing of text and control colours the pages use against
// the contrast the style guide requires (docs/STYLE.md, "Contrast"):
// 4.5:1 for text, 3:1 for the edges of controls, icons, state marks and the
// focus ring. It reads the one token file, works out each of the eight
// sets (dark or light, standard or high-distinction palette, normal or
// increased contrast) and fails on any pair below its requirement.
//
//   node e2e/contrast.mjs          print the failures, or one line if none
//   node e2e/contrast.mjs -v       print every pair

import { readFileSync } from 'node:fs';

const FILE = new URL('../internal/web/static/shared/tokens.css', import.meta.url);
const css = readFileSync(FILE, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
const verbose = process.argv.includes('-v');

// block returns the declarations of the rule whose selector is exactly sel
// (within the media query when one is given).
function block(sel, media) {
  let src = css;
  if (media) {
    const at = css.indexOf(media);
    if (at < 0) throw new Error(`tokens.css has no ${media}`);
    src = css.slice(at + media.length);
  }
  const at = src.indexOf(sel + ' {');
  if (at < 0) throw new Error(`tokens.css has no rule ${sel}${media ? ' in ' + media : ''}`);
  const body = src.slice(at + sel.length + 2, src.indexOf('}', at));
  const out = {};
  for (const m of body.matchAll(/(--[a-z0-9-]+)\s*:\s*([^;]+);/g)) out[m[1]] = m[2].trim();
  return out;
}

const dark = block(':root');
const darkHD = block(':root[data-palette="hd"]');
const light = block(':root[data-theme="light"]');
const lightHD = block(':root[data-palette="hd"][data-theme="light"]');
const more = block(':root[data-contrast="more"]');

// The copies the system settings use must say the same as the ones the
// per-browser override uses.
const same = (a, b, what) => {
  const ka = JSON.stringify(Object.entries(a).sort()), kb = JSON.stringify(Object.entries(b).sort());
  if (ka !== kb) { console.log(`FAIL contrast: ${what} differ`); process.exitCode = 1; }
};
same(light, block(':root:not([data-theme="dark"])', '@media (prefers-color-scheme: light)'), 'the two light theme blocks');
same(lightHD, block(':root[data-palette="hd"]:not([data-theme="dark"])', '@media (prefers-color-scheme: light)'), 'the two light high-distinction blocks');
same(more, block(':root:not([data-contrast="normal"])', '@media (prefers-contrast: more)'), 'the two increase-contrast blocks');

function resolve(set, v, depth = 0) {
  const m = /^var\((--[a-z0-9-]+)\)$/.exec(v);
  if (!m) return v;
  if (depth > 5 || !(m[1] in set)) throw new Error(`cannot resolve ${v}`);
  return resolve(set, set[m[1]], depth + 1);
}
function rgb(hex) {
  const m = /^#([0-9a-f]{6})$/i.exec(hex);
  if (!m) throw new Error(`not a colour: ${hex}`);
  return [0, 2, 4].map((i) => parseInt(m[1].slice(i, i + 2), 16));
}
const lum = (c) => {
  const f = c.map((x) => { x /= 255; return x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4; });
  return 0.2126 * f[0] + 0.7152 * f[1] + 0.0722 * f[2];
};
const ratio = (a, b) => { const x = lum(a), y = lum(b); return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05); };
// mix(a, b, t): b laid over a at opacity t, as the share bar of a poll is.
function colour(set, spec) {
  const m = /^mix\((--[a-z0-9-]+),(--[a-z0-9-]+),([\d.]+)\)$/.exec(spec);
  if (m) {
    const a = rgb(resolve(set, set[m[1]])), b = rgb(resolve(set, set[m[2]])), t = Number(m[3]);
    return a.map((x, i) => Math.round(x * (1 - t) + b[i] * t));
  }
  if (!(spec in set)) throw new Error(`no token ${spec}`);
  return rgb(resolve(set, set[spec]));
}

const T = 4.5, G = 3; // text; graphics (edges, icons, marks, focus)
const surfaces = ['--bg', '--panel', '--raise'];
const on = (fg, bgs, need, what) => bgs.map((bg) => [what, fg, bg, need]);
const PAIRS = [
  ...on('--ink', surfaces, T, 'text'),
  ...on('--dim', surfaces, T, 'secondary text'),
  // faint is never used as text on the raised step (docs/STYLE.md)
  ...on('--faint', ['--bg', '--panel'], T, 'faint labels, section names, times, placeholders'),
  ...on('--cyan', surfaces, T, 'links, thread line, channel chips'),
  ...on('--pink', surfaces, T, 'problems, failed, danger in a menu, code keywords'),
  ...on('--yel-ink', surfaces, T, 'yellow as text: marker count, code strings, link warning'),
  ...on('--lav', ['--raise'], T, 'code function names'),
  ['text on cyan (primary button, toast, tag)', '--on-cyan', '--cyan', T],
  ['text on lavender (button, tag, mention of you, avatar)', '--on-lav', '--lav', T],
  ['text on pink (danger, hover, NEW, banner)', '--on-pink', '--pink', T],
  ['the unread count on yellow', '--on-yel', '--yel', T],
  ['poll share bar under text', '--ink', 'mix(--raise,--lav,0.28)', T],
  ['your poll choice under text', '--ink', 'mix(--raise,--cyan,0.28)', T],
  ...on('--edge', surfaces, G, 'control edges: inputs, switches, buttons, N more'),
  ...on('--cyan', surfaces, G, 'selected bar, online light, switch on'),
  ...on('--lav', ['--bg', '--panel'], G, 'lavender icons and edges'),
  ...on('--pink', ['--bg', '--panel'], G, 'do not disturb diamond, danger fill'),
  ...on('--yel-ink', ['--panel', '--raise'], G, 'away: the hollow yellow square'),
  ...on('--faint', ['--bg', '--panel', '--raise'], G, 'offline light, switch off'),
  ['the bar of a do-not-disturb shield', '--on-pink', '--pink', G],
];
// The focus ring: yellow on dark; on light, yellow with a dark halo.
// The yellow price tag: on dark the yellow stands out by itself; on light
// it holds by its dark edge.
const FOCUS_DARK = [...on('--yel', surfaces, G, 'the focus ring, the price tag')];
const FOCUS_LIGHT = [['the focus ring against its halo', '--yel', '--halo', G], ...on('--halo', surfaces, G, 'the halo of the focus ring'),
  ['the price tag against its edge', '--yel', '--ink', G], ...on('--ink', surfaces, G, 'the edge of the price tag')];

const sets = [];
for (const [theme, base, hd] of [['dark', dark, darkHD], ['light', { ...dark, ...light }, lightHD]]) {
  for (const palette of ['standard', 'high distinction']) {
    for (const contrast of ['normal', 'increased']) {
      let set = { ...base };
      if (palette !== 'standard') set = { ...set, ...hd };
      if (contrast === 'increased') set = { ...set, ...more };
      sets.push({ name: `${theme}, ${palette}, ${contrast} contrast`, theme, set });
    }
  }
}

let bad = 0, checked = 0;
for (const { name, theme, set } of sets) {
  for (const [what, fg, bg, need] of [...PAIRS, ...(theme === 'light' ? FOCUS_LIGHT : FOCUS_DARK)]) {
    const r = ratio(colour(set, fg), colour(set, bg));
    checked++;
    const ok = r >= need;
    if (!ok) bad++;
    if (!ok || verbose) console.log(`${ok ? 'ok  ' : 'FAIL'} ${name}: ${fg} on ${bg} ${r.toFixed(2)}:1, needs ${need}:1 (${what})`);
  }
}
console.log(bad ? `FAIL contrast: ${bad} of ${checked} pairs below their requirement` : `ok   contrast: all ${checked} pairs pass in ${sets.length} sets`);
if (bad) process.exitCode = 1;
