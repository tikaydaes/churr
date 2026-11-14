// Checks the web pages' theme arithmetic (internal/web/static/shared/
// palette.js), with no server and no browser:
//
// - against the table of cases the server's Go package worked out
//   (internal/web/testdata/palette_cases.json, made and checked against
//   Go by internal/web/palette_test.go);
// - that its copy of the built-in themes is the token file's;
// - "fix", the editor's smallest change to a failing colour;
// - the .churrtheme file, out and back in.
//
//   node e2e/palette.mjs

import { readFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const here = (p) => fileURLToPath(new URL(p, import.meta.url));
const MODULE = here('../internal/web/static/shared/palette.js');
const p = await import(new URL('../internal/web/static/shared/palette.js', import.meta.url).href);

let failures = 0;
function check(name, fn) {
  try { const said = fn(); console.log(`ok   palette: ${name}${said ? ' (' + said + ')' : ''}`); } catch (e) { failures++; console.log(`FAIL palette: ${name} - ${e.message}`); }
}
const assert = (c, m) => { if (!c) throw new Error(m); };

check('agrees with the server’s Go package on every case of the table', () => {
  const out = execFileSync(process.execPath, [here('../internal/web/testdata/palette_vectors.mjs'), MODULE, here('../internal/web/testdata/palette_cases.json')], { encoding: 'utf8' });
  assert(out.startsWith('ok '), out);
  return out.trim().slice(3);
});

check('the built-in themes are the token file’s', () => {
  const css = readFileSync(here('../internal/web/static/shared/tokens.css'), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
  const block = (sel) => {
    const at = css.indexOf(sel + ' {');
    const body = css.slice(at + sel.length + 2, css.indexOf('}', at));
    return Object.fromEntries([...body.matchAll(/--([a-z0-9-]+)\s*:\s*(#[0-9a-f]{6})\s*;/g)].map((m) => [m[1], m[2]]));
  };
  const sets = { dark: block(':root'), light: block(':root[data-theme="light"]') };
  const hd = { dark: block(':root[data-palette="hd"]'), light: block(':root[data-palette="hd"][data-theme="light"]') };
  let n = 0;
  for (const kind of ['dark', 'light']) {
    for (const [k, v] of Object.entries(p.BUILTIN[kind])) { assert(sets[kind][k] === v, `${kind} --${k}: palette.js ${v}, tokens.css ${sets[kind][k]}`); n++; }
    // --halo is a web-only helper, taken from the token file by kind.
    for (const [k, v] of Object.entries(sets[kind])) if (k !== 'halo') assert(p.BUILTIN[kind][k] === v, `${kind} --${k} is in tokens.css and not in palette.js`);
    for (const [k, v] of Object.entries(hd[kind])) { assert(p.BUILTIN_HD[kind][k] === v, `${kind} high distinction --${k}`); n++; }
  }
  return n + ' values';
});

check('the built-in themes pass as themes', () => {
  for (const kind of ['dark', 'light']) for (const hd of [false, true]) {
    const t = p.builtinTheme(kind, hd);
    assert(p.problem(t) === null, `${t.name}: ${p.problem(t)}`);
  }
});

check('measured cuts, never rounds', () => {
  assert(p.measured(4.4999) === '4.49', p.measured(4.4999));
  assert(p.measured(21) === '21.00', p.measured(21));
  assert(p.measured(3.0) === '3.00', p.measured(3));
  assert(p.measured(4.0699999) === '4.06', p.measured(4.0699999));
});

const dark = p.builtinTheme('dark').colours;
const light = p.builtinTheme('light').colours;

check('fix: a failing colour gets the smallest change that passes', () => {
  const cases = [
    ['dark', { ...dark, dim: '#6a6890' }, 'dim'],
    ['dark', { ...dark, pink: '#c0207f' }, 'pink'],
    ['dark', { ...dark, lav: '#7060c0' }, 'lav'],
    // Lavender is text: 4.5:1 on the panel too, not only on raised.
    ['dark', { ...dark, raise: '#0a0c1e', lav: '#7a6aff' }, 'lav'],
    ['dark', { ...dark, yel: '#5a5a20' }, 'yel'],
    ['light', { ...light, ink: '#8a8aa0' }, 'ink'],
    ['light', { ...light, cyan: '#33bbaa' }, 'cyan'],
    ['dark', { ...dark, raise: '#343a75' }, 'raise'],
  ];
  const said = [];
  for (const [kind, colours, key] of cases) {
    assert(p.problem({ kind, colours }), `${key} was meant to fail`);
    const next = p.fix(kind, colours, key);
    assert(next, `no fix for ${key}`);
    const fixed = { ...colours, [key]: next };
    assert(p.problem({ kind, colours: fixed }) === null, `${key} fixed to ${next} still fails: ${p.problem({ kind, colours: fixed })}`);
    // Smallest: one step less towards either end does not pass.
    const from = p.parseHex(colours[key]);
    let steps = 0;
    for (const end of [[0, 0, 0], [255, 255, 255]]) for (let k = 0; k <= 100; k++) if (p.hex(p.mix(from, end, k)) === next) steps = k;
    for (const end of [[0, 0, 0], [255, 255, 255]]) {
      for (let k = 0; k < steps; k++) assert(p.problem({ kind, colours: { ...colours, [key]: p.hex(p.mix(from, end, k)) } }), `${key}: ${k}% already passed, yet ${steps}% was taken`);
    }
    said.push(`${key} ${colours[key]}→${next}`);
  }
  return said.join(', ');
});

check('fix leaves a passing colour as it is, and says when nothing helps', () => {
  assert(p.failingKeys('dark', p.parse(dark).colours).size === 0, 'the guide dark has failing keys');
  // White text on a white page cannot be fixed by the text colour alone
  // when the ground is mid-grey and every role colour is too.
  const grey = Object.fromEntries(p.KEYS.map((k) => [k, '#808080']));
  const out = p.fix('dark', grey, 'dim');
  assert(out === null || typeof out === 'string', 'fix answered something odd');
});

check('the failing colour of each pairing', () => {
  assert(p.sourceOf('faint', 'bg') === 'dim', 'faint');
  assert(p.sourceOf('edge', 'raise') === 'dim', 'edge');
  assert(p.sourceOf('yel-ink', 'bg') === 'yel', 'yel-ink');
  assert(p.sourceOf('on-pink', 'pink') === 'pink', 'on-pink');
  assert(p.sourceOf('lav', 'raise') === 'lav', 'lav');
});

check('tokens: every token a page uses, from a theme; none from a failing theme', () => {
  const t = p.tokens({ kind: 'dark', colours: dark });
  for (const k of ['bg', 'panel', 'raise', 'line', 'grid', 'ink', 'dim', 'faint', 'edge', 'lav', 'cyan', 'pink', 'yel', 'on-lav', 'on-cyan', 'on-pink', 'on-yel', 'yel-ink', 'on', 'paper']) assert(/^#[0-9a-f]{6}$/.test(t[k]), `${k} is ${t[k]}`);
  assert(t.faint === '#8381a8' && t.edge === '#6d6c90', `derived faint ${t.faint}, edge ${t.edge}`);
  const more = p.tokens({ kind: 'dark', colours: dark }, true);
  assert(more.line === more.edge && more.faint === more.dim, 'increase contrast is not applied');
  assert(p.tokens({ kind: 'dark', colours: { ...dark, dim: '#6a6890' } }) === null, 'a failing theme got tokens');
  assert(p.tokens({ kind: 'light', colours: dark }) === null, 'a dark theme passed as light');
});

check('a .churrtheme file goes out and comes back the same', () => {
  const theme = { name: 'Night Market', kind: 'dark', colours: { ...dark, cyan: '#5DF0E4' } };
  const text = p.toFile(theme);
  const back = p.fromFile(text);
  assert(back.theme && back.theme.name === 'Night Market' && back.theme.colours.cyan === '#5df0e4', JSON.stringify(back));
  assert(p.fileName(theme) === 'night-market.churrtheme', p.fileName(theme));
  assert(p.fromFile('{').error, 'broken JSON was taken');
  assert(p.fromFile(JSON.stringify({ name: 'x', kind: 'dark', colours: { ...dark, dim: '#6a6890' } })).error === 'dim on bg is 3.57:1; text needs at least 4.5:1.', 'a failing file was not refused with the server’s words');
  assert(p.fromFile(JSON.stringify({ name: 'x', kind: 'dark', colours: { ...dark, accent: '#ffffff' } })).error.includes('"accent" is not a theme colour'), 'an extra key was taken');
});

if (failures) process.exitCode = 1;
