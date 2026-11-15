// Writes the four SVG charts in this folder from data.json.
//   node docs/perf/make-charts.mjs
// No dependencies. The SVGs are plain text and carry their own background
// panel and a light and a dark set of colours, so they can be read on any page.

import { readFileSync, writeFileSync } from 'node:fs';

const here = new URL('.', import.meta.url);
const data = JSON.parse(readFileSync(new URL('data.json', here), 'utf8'));

const W = 860;
const PAD = 28;
const SANS = "Archivo, 'Helvetica Neue', Helvetica, Arial, sans-serif";
const MONO = "'Martian Mono', ui-monospace, SFMono-Regular, Menlo, Consolas, monospace";

// Churr's tokens (docs/STYLE.md). The third series colour is the orange of the
// high-distinction palette, which stays apart from cyan and lavender for
// people with red-green colour blindness; pink does not.
const STYLE = `
:root{--bg:#f5f2ff;--ink:#15173a;--dim:#55527f;--faint:#6b6894;--grid:#dcd5f7;--line:#c9c1ee;
--cyan:#006e66;--lav:#5b3fd6;--org:#a8480a;--cyan2:#5aa8a1;--lav2:#9d8fe6}
@media (prefers-color-scheme: dark){:root{--bg:#0d1026;--ink:#efeaff;--dim:#a09dc9;--faint:#8b88b8;--grid:#1d2250;--line:#2c3170;
--cyan:#5df0e4;--lav:#b9a0ff;--org:#ff9a4d;--cyan2:#2c8580;--lav2:#6a5aa8}}
text{font-family:${SANS};fill:var(--ink)}
.bg{fill:var(--bg)}
.t{font-size:19px;font-weight:800}
.s{font-size:12.5px;fill:var(--dim)}
.h{font-size:11px;font-weight:700;letter-spacing:.09em;fill:var(--faint)}
.g{font-size:13px;font-weight:700;paint-order:stroke;stroke:var(--bg);stroke-width:5px}
.gm{font-weight:400;fill:var(--dim)}
.r{font-size:12.5px;fill:var(--dim)}
.v{font-family:${MONO};font-size:11px}
.vd{font-family:${MONO};font-size:11px;fill:var(--dim)}
.k{font-family:${MONO};font-size:10px;fill:var(--faint)}
.n{font-size:11.5px;fill:var(--faint)}
.grid{stroke:var(--grid);stroke-width:1}
.axis{stroke:var(--line);stroke-width:1}
.rng{stroke:var(--line);stroke-width:2}
.cyan{fill:var(--cyan)}.lav{fill:var(--lav)}.org{fill:var(--org)}
.cyan2{fill:var(--cyan2)}.lav2{fill:var(--lav2)}
.hollow{fill:var(--bg);stroke:var(--org);stroke-width:2}
.peak{fill:var(--bg);stroke:var(--ink);stroke-width:1.5}
.ring{stroke:var(--bg);stroke-width:2}
`;

const esc = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
const n = (x) => Math.round(x * 10) / 10;
const text = (cls, x, y, s, extra = '') => `<text class="${cls}" x="${n(x)}" y="${n(y)}"${extra}>${esc(s)}</text>`;
const rect = (cls, x, y, w, h) => `<rect class="${cls}" x="${n(x)}" y="${n(y)}" width="${n(Math.max(w, 1))}" height="${n(h)}"/>`;
const line = (cls, x1, y1, x2, y2) => `<line class="${cls}" x1="${n(x1)}" y1="${n(y1)}" x2="${n(x2)}" y2="${n(y2)}"/>`;
const diamond = (cls, cx, cy, r) =>
  `<polygon class="${cls}" points="${n(cx)},${n(cy - r)} ${n(cx + r)},${n(cy)} ${n(cx)},${n(cy + r)} ${n(cx - r)},${n(cy)}"/>`;
const thousands = (x) => x.toLocaleString('en-GB');

function frame(file, { title, sub, desc, height, body, notes = [] }) {
  const subs = [].concat(sub);
  let out = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${W} ${height}" width="${W}" height="${height}" role="img" aria-labelledby="t d">\n`;
  out += `<title id="t">${esc(title)}</title>\n<desc id="d">${esc(desc)}</desc>\n<style>${STYLE}</style>\n`;
  // The panel, with Churr's cut corner at the top right.
  out += `<polygon class="bg" points="0,0 ${W - 18},0 ${W},18 ${W},${height} 0,${height}"/>\n`;
  out += text('t', PAD, 42, title) + '\n';
  subs.forEach((s, i) => { out += text('s', PAD, 63 + i * 17, s) + '\n'; });
  out += body.join('\n') + '\n';
  notes.forEach((s, i) => { out += text('n', PAD, height - 18 - (notes.length - 1 - i) * 16, s) + '\n'; });
  out += '</svg>\n';
  writeFileSync(new URL(file, here), out);
  console.log('wrote', file);
}

// A legend row: [kind, label] pairs. Returns the elements.
function legend(items, y, x = PAD) {
  const out = [];
  for (const [kind, label] of items) {
    out.push(mark(kind, x + 5, y - 4));
    out.push(text('r', x + 16, y, label));
    x += 16 + label.length * 6.6 + 22;
  }
  return out;
}
function mark(kind, cx, cy) {
  switch (kind) {
    case 'p50': return rect('cyan ring', cx - 5.5, cy - 5.5, 11, 11);
    case 'p95': return diamond('lav ring', cx, cy, 7.5);
    case 'p99': return rect('hollow', cx - 4, cy - 4, 8, 8);
    case 'peak': return diamond('peak', cx, cy, 5);
    default: return rect(kind, cx - 5, cy - 5, 10, 10);
  }
}

const machineOf = (g) =>
  `${g.vcpu} ${g.vcpu_note ? g.vcpu_note + ' ' : ''}vCPU, ${g.ram_gb} GB, ${g.database}`;
const groupHead = (g, y) =>
  `<text class="g" x="${PAD}" y="${y}">${thousands(g.members)} members<tspan class="gm">   ${esc(machineOf(g))}</tspan></text>`;
const charted = (r) => r.chart !== false;
const caseLabel = (r) => `${r.case} · ${r.posts_per_s} posts/s`;
const ms = (v) => (v >= 1000 ? `${(v / 1000).toFixed(v % 1000 === 0 ? 1 : 2)} s` : `${v} ms`);

// ---- 1. The ladder: what each run used -----------------------------------
{
  const body = [];
  const top = 140;
  const A = { x: 330, w: 150, max: 4.5 };
  const B = { x: 590, w: 150, max: 4.5 };
  body.push(...legend([
    ['cyan', 'a fifth of members online, 20 posts/s'],
    ['lav', 'everyone online'],
    ['peak', 'busiest'],
  ], 104));
  body.push(text('h', A.x, top - 8, 'CPUS USED BY CHURR'));
  body.push(text('h', B.x, top - 8, 'PEAK MEMORY'));
  let y = top;
  const marks = [];
  for (const g of data.ladder) {
    y += 14;
    marks.push(groupHead(g, y + 10));
    y += 20;
    for (const r of g.rows.filter(charted)) {
      const cls = r.kind === 'typical' ? 'cyan' : 'lav';
      const cy = y + 11;
      marks.push(text('r', PAD + 10, cy + 4, caseLabel(r)));
      const wa = (r.churr_cpu / A.max) * A.w;
      marks.push(rect(cls, A.x, cy - 6, wa, 12));
      const cpuText = (r.approx || r.churr_cpu_approx ? '~' : '') + (r.churr_cpu_text || r.churr_cpu);
      marks.push(text('v', A.x + wa + 6, cy + 4, cpuText));
      if (r.churr_cpu_busiest) {
        const px = A.x + (r.churr_cpu_busiest / A.max) * A.w;
        marks.push(mark('peak', px, cy));
        marks.push(text('vd', px + 10, cy + 4, String(r.churr_cpu_busiest)));
      }
      const gb = r.peak_mem_mb / 1000;
      const wb = (gb / B.max) * B.w;
      marks.push(rect(cls, B.x, cy - 6, wb, 12));
      const memText = (r.approx || r.peak_mem_approx ? '~' : '') + (r.peak_mem_text || `${r.peak_mem_mb} MB`);
      marks.push(text('v', B.x + wb + 6, cy + 4, memText));
      y += 24;
    }
  }
  const bottom = y + 6;
  for (const P of [A, B]) {
    for (let i = 0; i <= 4; i++) {
      const x = P.x + (i / P.max) * P.w;
      body.push(line(i === 0 ? 'axis' : 'grid', x, top, x, bottom));
      body.push(text('k', x, bottom + 14, i === 4 ? (P === A ? '4 CPUs' : '4 GB') : String(i), ' text-anchor="middle"'));
    }
  }
  body.push(...marks);
  frame('ladder-resources.svg', {
    title: 'CPU and memory by community size',
    sub: ['Each heading is a community size and the machine it ran on. Bars are what Churr itself used while',
      'messages flowed. PostgreSQL used 0.2 of a CPU or less in every run that had it. Each row is one run.'],
    desc: 'Horizontal bars for nine load-test runs on four machine sizes, 500 to 50,000 members: the processors Churr used and the peak memory. ' +
      data.ladder.flatMap((g) => g.rows.filter(charted).map((r) =>
        `${thousands(g.members)} members on ${machineOf(g)}, ${caseLabel(r)}: ${(r.approx || r.churr_cpu_approx ? 'about ' : '')}${r.churr_cpu} CPUs${r.churr_cpu_busiest ? ` (busiest ${r.churr_cpu_busiest})` : ''}, ${(r.approx || r.peak_mem_approx ? 'about ' : '')}${r.peak_mem_text || r.peak_mem_mb + ' MB'}.`)).join(' '),
    height: bottom + 62,
    body,
    notes: ['Synthetic text chat, measured 15 November 2026. ~ marks a figure recorded as approximate.'],
  });
}

// ---- Delivery percentiles on a log axis (used by charts 2 and 5) ----------
function percentileRows({ rows, top, x0, w, lo, hi, ticks, linear = false }) {
  const sx = (v) => x0 + (linear ? (v - lo) / (hi - lo) : Math.log10(v / lo) / Math.log10(hi / lo)) * w;
  const grid = [];
  const marks = [];
  let y = top;
  for (const r of rows) {
    if (r.head) { y += 14; marks.push(r.head(y + 10)); y += 20; continue; }
    const cy = y + 11;
    marks.push(text('r', PAD + (r.indent ?? 10), cy + 4, r.label));
    marks.push(line('rng', sx(r.p50), cy, sx(r.p99), cy));
    marks.push(mark('p50', sx(r.p50), cy));
    marks.push(mark('p99', sx(r.p99), cy));
    if (r.p95 != null) marks.push(mark('p95', sx(r.p95), cy));
    marks.push(text('v', x0 + w + 26, cy + 4, r.values));
    y += 26;
  }
  const bottom = y + 6;
  for (const t of ticks) {
    grid.push(line('grid', sx(t), top, sx(t), bottom));
    grid.push(text('k', sx(t), bottom + 14, ms(t), ' text-anchor="middle"'));
  }
  return { els: [...grid, ...marks], bottom };
}
const pLegend = (y) => legend([
  ['p50', 'half arrived within (p50)'],
  ['p95', '95 in 100 (p95)'],
  ['p99', '99 in 100 (p99)'],
], y);

// ---- 2. Delivery time across the ladder -----------------------------------
{
  const rows = [];
  for (const g of data.ladder) {
    rows.push({ head: (y) => groupHead(g, y) });
    for (const r of g.rows.filter(charted)) {
      rows.push({
        label: caseLabel(r), p50: r.p50_ms, p95: r.p95_ms, p99: r.p99_ms,
        values: r.p99_text ? `${r.p50_ms} / ${r.p95_ms} ms / ${r.p99_text}` : `${r.p50_ms} / ${r.p95_ms ?? '–'} / ${r.p99_ms} ms`,
      });
    }
  }
  const top = 140;
  const x0 = 340, w = 310;
  const { els, bottom } = percentileRows({ rows, top, x0, w, lo: 5, hi: 1500, ticks: [10, 100, 1000] });
  frame('ladder-delivery.svg', {
    title: 'Delivery time by community size',
    sub: ['From a member posting to each connected member receiving it. The scale is logarithmic: each step',
      'to the right is ten times longer. Each row is one run.'],
    desc: 'Delivery time at the 50th, 95th and 99th percentile for nine load-test runs, on a logarithmic scale from 5 milliseconds to 1.5 seconds. ' +
      data.ladder.flatMap((g) => g.rows.filter(charted).map((r) =>
        `${thousands(g.members)} members, ${caseLabel(r)}: ${ms(r.p50_ms)}, ${r.p95_ms == null ? 'not recorded' : ms(r.p95_ms)}, ${r.p99_text || ms(r.p99_ms)}.`)).join(' '),
    height: bottom + 78,
    body: [...pLegend(104), text('h', x0, top - 8, 'DELIVERY TIME'), text('h', x0 + w + 26, top - 8, 'P50 / P95 / P99'), ...els],
    notes: ['Synthetic text chat, measured 15 November 2026. The 1,000-member run recorded no p95. It and the 50,000-member runs at 20 posts/s',
      'and with all viewing used compressed protobuf; the others compressed JSON.'],
  });
}

// ---- 3. Bytes per frame, as the clients speak now ------------------------
{
  const fb = data.frame_bytes;
  const top = 126;
  const x0 = 220, w = 520, max = 360;
  const series = [
    ['web_json_compressed', 'lav', 'web chat (compressed JSON)'],
    ['apps_protobuf_compressed', 'cyan', 'apps (compressed protobuf)'],
  ];
  const body = [...legend(series.map(([, cls, label]) => [cls, label]), 96)];
  const marks = [];
  let y = top + 8;
  for (const f of fb.frames) {
    marks.push(text('g', PAD, y + 17, f.frame));
    series.forEach(([key, cls], i) => {
      const by = y + i * 13;
      const bw = (f[key] / max) * w;
      marks.push(rect(cls, x0, by, bw, 11));
      marks.push(text('v', x0 + bw + 6, by + 9.5, String(f[key])));
    });
    y += 2 * 13 + 16;
  }
  const bottom = y - 8;
  for (const t of [0, 100, 200, 300]) {
    const x = x0 + (t / max) * w;
    body.push(line(t === 0 ? 'axis' : 'grid', x, top, x, bottom));
    body.push(text('k', x, bottom + 14, t === 300 ? '300 bytes' : String(t), ' text-anchor="middle"'));
  }
  body.push(...marks);
  const avg = fb.run_average_per_delivered_frame;
  frame('frame-sizes.svg', {
    title: 'Bytes sent per frame',
    sub: 'The size of one frame sent to one connection, for five common kinds, as each client speaks.',
    desc: 'Bars of frame sizes in bytes for the web chat, which speaks compressed JSON, and the apps, which speak compressed protobuf. ' +
      fb.frames.map((f) => `${f.frame}: ${f.web_json_compressed} and ${f.apps_protobuf_compressed}.`).join(' '),
    height: bottom + 62,
    body,
    notes: [`Averaged over a 50,000-member run: ${avg.web_json_compressed} bytes per delivered frame as compressed JSON, ${avg.apps_protobuf_compressed} as compressed protobuf.`],
  });
}

// ---- 4. Three small servers through one hub -------------------------------
{
  const top = 140;
  const x0 = 340, w = 310;
  const rows = data.hub.rows.map((r) => ({
    label: `${r.posts_per_s} posts/s`, indent: 0, p50: r.p50_ms, p95: r.p95_ms, p99: r.p99_ms,
    values: `${r.p50_ms} / ${r.p95_ms} / ${r.p99_ms} ms`,
  }));
  const { els, bottom } = percentileRows({ rows, top: top + 4, x0, w, lo: 0, hi: 50, ticks: [0, 10, 20, 30, 40, 50], linear: true });
  frame('hub-delivery.svg', {
    title: 'Three servers sharing a channel through one hub',
    sub: ['300 members on each of three 1-vCPU servers, all in one shared channel. Delivery time to every member',
      'on every server. Every run delivered every message.'],
    desc: 'Delivery time at the 50th, 95th and 99th percentile for three runs through a hub, on a scale from 0 to 50 milliseconds. ' +
      data.hub.rows.map((r) => `${r.posts_per_s} posts a second: ${r.p50_ms}, ${r.p95_ms} and ${r.p99_ms} milliseconds.`).join(' '),
    height: bottom + 62,
    body: [...pLegend(104), text('h', x0, top - 8, 'DELIVERY TIME'), text('h', x0 + w + 26, top - 8, 'P50 / P95 / P99'), ...els],
    notes: ['Compressed protobuf on the links and the clients. One run at each rate. Median on the same server 12.5–14 ms, on another 28–30 ms.'],
  });
}
