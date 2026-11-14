// Runs static/shared/palette.js, the web pages' theme arithmetic, against
// the table internal/web/palette_test.go works out with the server's Go
// package (testdata/palette_cases.json). Started by that test, and by
// e2e/palette.mjs:
//
//   node palette_vectors.mjs <palette.js> <palette_cases.json>
//
// It prints "ok <n>" with the number of checks made, or throws at the
// first disagreement.
//
// Everything must be identical: each derived colour, whether the kind
// fits, which pairing is refused first and the words and cut ratio of the
// refusal, and every luminance and ratio bit for bit (Object.is on the
// two doubles). The arithmetic is written so that it is: one table of
// linear light, written out, and an order of operations no engine or
// compiler may fuse (docs/STYLE.md "Themes", Arithmetic).

import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';

const p = await import(pathToFileURL(process.argv[2]).href);
const table = JSON.parse(readFileSync(process.argv[3], 'utf8'));
let checks = 0, floats = 0;

function same(got, want, what) {
  checks++;
  if (got !== want) throw new Error(`${what}: JavaScript says ${JSON.stringify(got)}, Go says ${JSON.stringify(want)}`);
}

// exact requires the same double, bit for bit.
function exact(got, want, what) {
  checks++; floats++;
  if (typeof want !== 'number' || !Object.is(got, want)) throw new Error(`${what}: JavaScript says ${got}, Go says ${want}; they must be the same double`);
}

// The linear light table itself, entry for entry.
for (let v = 0; v < 256; v++) exact(p.lin(v), table.linear[v], `linear light of ${v}`);

// Luminance of each channel value alone, in each channel: covers every
// input of the power function the arithmetic uses.
for (let v = 0; v < 256; v++) {
  exact(p.luminance([v, 0, 0]), table.channel[v][0], `luminance of red ${v}`);
  exact(p.luminance([0, v, 0]), table.channel[v][1], `luminance of green ${v}`);
  exact(p.luminance([0, 0, v]), table.channel[v][2], `luminance of blue ${v}`);
}

for (const m of table.mix) same(p.hex(p.mix(p.parseHex(m.A), p.parseHex(m.B), m.K)), m.Want, `mix(${m.A}, ${m.B}, ${m.K})`);

for (const kind of ['dark', 'light']) {
  const js = p.pairings(kind).map(([fore, back, need]) => `${fore} on ${back} ${need}`);
  const go = table.pairings[kind].map((x) => `${x.fore} on ${x.back} ${x.need}`);
  same(js.join(', '), go.join(', '), `the pairings of a ${kind} theme, in order`);
}

for (const [i, c] of table.cases.entries()) {
  const what = `case ${i} (${c.name})`;
  const got = p.parse(c.colours);
  if (got.message) throw new Error(`${what}: JavaScript refuses the colours: ${got.message}`);
  const col = got.colours;
  for (const k of p.KEYS) exact(p.luminance(col[k]), c.luminance[k], `${what}: luminance of ${k}`);
  same(p.kindFits(c.kind, col), c.kind_fits, `${what}: kind fits`);
  const d = p.derive(col);
  for (const [k, v] of Object.entries(c.derived)) same(p.hex(d[k]), v, `${what}: ${k}`);
  const f = p.check(c.kind, col);
  same(f ? f.message : '', c.refusal, `${what}: refusal`);
  if (c.ratios) {
    const all = p.measure(c.kind, col);
    same(all.length, c.ratios.length, `${what}: number of pairings`);
    all.forEach((m, j) => exact(m.ratio, c.ratios[j], `${what}: ${m.fore} on ${m.back}`));
  }
}

console.log(`ok ${checks} checks, ${table.cases.length} themes; all ${floats} luminances and ratios bit for bit the same as Go's`);
