// The colour arithmetic of themes (ruling R61, docs/STYLE.md "Themes"):
// the nine base colours a theme stores, every token derived from them,
// and the contrast check that decides whether a theme may be saved,
// shared or applied.
//
// It is the same arithmetic as the server's internal/themes/palette,
// written out step for step, so a theme the server accepts passes here
// and draws the same colours. internal/web/palette_test.go runs this file
// under Node against a table of cases the Go package worked out
// (testdata/palette_cases.json); e2e/run.sh runs the same check. Change
// one and change both.
//
// Pure: no DOM, no storage. The pages and the Node tests import it alike.

// The base keys, in the order they are listed and checked.
export const KEYS = ['bg', 'panel', 'raise', 'ink', 'dim', 'lav', 'cyan', 'pink', 'yel'];
export const TEXT = 4.5; // text against what it sits on
export const EDGE = 3;   // control edges, icons, presence shapes, the focus ring

// What each base colour is for, in words, for the editor.
export const KEY_NAMES = {
  bg: 'Ground', panel: 'Panel', raise: 'Raised', ink: 'Text', dim: 'Secondary text',
  lav: 'Lavender', cyan: 'Cyan', pink: 'Pink', yel: 'Yellow',
};

const HEX = /^#[0-9a-fA-F]{6}$/;

// parseHex reads "#rrggbb" (either case) into [r, g, b], or null.
export function parseHex(s) {
  if (typeof s !== 'string' || !HEX.test(s)) return null;
  return [1, 3, 5].map((i) => parseInt(s.slice(i, i + 2), 16));
}

// hex writes [r, g, b] as lowercase "#rrggbb".
export function hex(c) {
  return '#' + c.map((v) => v.toString(16).padStart(2, '0')).join('');
}

// LINEAR is each channel value's linear light, the inner step of WCAG 2
// relative luminance (v / 255 / 12.92 up to v = 10, otherwise
// ((v / 255 + 0.055) / 1.055) ^ 2.4), worked out exactly and rounded once
// to the nearest double. It is the server's table (internal/themes/
// palette, which also holds its generator), literal for literal: written
// out so that no Math.pow takes part, since engines differ in its last
// bit.
const LINEAR = [
  0, 0.0003035269835488375, 0.000607053967097675, 0.0009105809506465125,
  0.00121410793419535, 0.0015176349177441874, 0.001821161901293025, 0.0021246888848418626,
  0.0024282158683907, 0.0027317428519395373, 0.003035269835488375, 0.0033465357638991586,
  0.003676507324047435, 0.004024717018496304, 0.0043914420374102925, 0.0047769534806937275,
  0.005181516702338385, 0.005605391624202721, 0.006048833022857052, 0.006512090792594472,
  0.006995410187265385, 0.00749903204322617, 0.008023192985384993, 0.008568125618069302,
  0.009134058702220785, 0.009721217320237844, 0.010329823029626936, 0.01096009400648824,
  0.011612245179743881, 0.012286488356915867, 0.012983032342173007, 0.013702083047289683,
  0.014443843596092541, 0.015208514422912706, 0.015996293365509628, 0.016807375752887377,
  0.017641954488384078, 0.01850022012837969, 0.019382360956935723, 0.02028856305665239,
  0.021219010376003555, 0.022173884793387375, 0.023153366178110403, 0.02415763244850475,
  0.025186859627361623, 0.02624122189484989, 0.02732089163907489, 0.028426039504420787,
  0.029556834437808797, 0.03071344373299362, 0.03189603307301152, 0.03310476657088505,
  0.03433980680868216, 0.03560131487502032, 0.03688945040110002, 0.03820437159534648,
  0.03954623527673283, 0.04091519690685317, 0.042311410620809654, 0.04373502925697345,
  0.04518620438567554, 0.04666508633688008, 0.048171824226889405, 0.04970656598412722,
  0.051269458374043224, 0.05286064702318025, 0.054480276442442355, 0.05612849004960008,
  0.05780543019106721, 0.059511238162981185, 0.061246054231617594, 0.06301001765316766,
  0.06480326669290576, 0.06662593864377288, 0.06847816984440015, 0.07036009569659588,
  0.07227185068231748, 0.07421356838014961, 0.07618538148130781, 0.07818742180518633,
  0.08021982031446831, 0.0822827071298148, 0.08437621154414877, 0.08650046203654974,
  0.08865558628577294, 0.09084171118340768, 0.09305896284668742, 0.09530746663096466,
  0.09758734714186242, 0.09989872824711389, 0.10224173308810128, 0.10461648409110416,
  0.10702310297826759, 0.10946171077829933, 0.11193242783690557, 0.11443537382697372,
  0.11697066775851081, 0.1195384279883456, 0.12213877222960184, 0.12477181756095046,
  0.12743768043564743, 0.13013647669036427, 0.13286832155381792, 0.13563332965520564,
  0.13843161503245183, 0.14126329114027164, 0.14412847085805772, 0.14702726649759498,
  0.14995978981060853, 0.15292615199615014, 0.15592646370782734, 0.15896083506088035,
  0.162029375639111, 0.1651321945016676, 0.1682694001896907, 0.17144110073282254,
  0.17464740365558498, 0.17788841598362912, 0.18116424424986013, 0.1844749945004409,
  0.18782077230067779, 0.19120168274079136, 0.1946178304415757, 0.1980693195599488,
  0.20155625379439707, 0.2050787363903169, 0.20863687014525567, 0.2122307574140551,
  0.21586050011389915, 0.21952619972926918, 0.22322795731680842, 0.22696587351009834,
  0.23074004852434896, 0.23455058216100508, 0.23839757381227095, 0.24228112246555472,
  0.2462013267078354, 0.2501582847299533, 0.2541520943308267, 0.25818285292159576,
  0.262250657529696, 0.2663556048028623, 0.27049779101306576, 0.27467731206038454,
  0.27889426347681034, 0.28314874042999194, 0.2874408377269174, 0.29177064981753587,
  0.29613827079832095, 0.3005437944157764, 0.3049873140698861, 0.30946892281750843,
  0.3139887133757175, 0.31854677812509175, 0.3231432091129507, 0.32777809805654207,
  0.3324515363461792, 0.33716361504833026, 0.34191442490866075, 0.3467040563550295,
  0.3515325995004392, 0.3564001441459434, 0.36130677978350945, 0.3662525955988394,
  0.37123768047414896, 0.3762621229909062, 0.38132601143253, 0.3864294337870489,
  0.3915724777497231, 0.3967552307256268, 0.4019777798321956, 0.40724021190173665,
  0.4125426134839036, 0.41788507084813725, 0.4232676699860715, 0.42869049661390657,
  0.4341536361747488, 0.43965717384091874, 0.44520119451622775, 0.45078578283822335,
  0.4564110231804045, 0.46207699965440685, 0.4677837961121588, 0.4735314961480093,
  0.47932018310082664, 0.4851499400560704, 0.49102084984783545, 0.49693299506087035,
  0.5028864580325684, 0.5088813208549335, 0.5149176653765213, 0.5209955732043541,
  0.527115125705813, 0.533276404010505, 0.539479489012107, 0.5457244613701866,
  0.5520114015119999, 0.5583403896342677, 0.5647115057049289, 0.5711248294648729,
  0.5775804404296505, 0.584078417891164, 0.5906188409193368, 0.5972017883637631,
  0.6038273388553375, 0.6104955708078647, 0.6172065624196509, 0.6239603916750759,
  0.6307571363461467, 0.6375968739940324, 0.644479681970582, 0.6514056374198239,
  0.6583748172794482, 0.6653872982822719, 0.6724431569576873, 0.6795424696330937,
  0.6866853124353132, 0.6938717612919898, 0.7011018919329731, 0.7083757798916867,
  0.7156935005064805, 0.7230551289219689, 0.7304607400903533, 0.7379104087727307,
  0.7454042095403872, 0.7529422167760778, 0.7605245046752922, 0.7681511472475069,
  0.7758222183174234, 0.7835377915261932, 0.79129794033263, 0.7991027380144087,
  0.8069522576692514, 0.8148465722161011, 0.8227857543962833, 0.8307698767746545,
  0.8387990117407399, 0.8468732315098577, 0.8549926081242336, 0.863157213454102,
  0.8713671191987971, 0.8796223968878317, 0.8879231178819664, 0.8962693533742666,
  0.9046611743911491, 0.9130986517934189, 0.9215818562772945, 0.9301108583754234,
  0.9386857284578878, 0.9473065367331996, 0.955973353249286, 0.9646862478944651,
  0.9734452903984123, 0.982250550333117, 0.9911020971138297, 1,
];

// lin is one channel's linear light, 0 to 255.
export function lin(v) {
  return LINEAR[v];
}

// luminance is WCAG 2 relative luminance in the written order of
// docs/STYLE.md: three products, then (r + g) + b. JavaScript rounds every
// operation to a double and never fuses a multiply with an add, so this is
// the server's answer bit for bit.
export function luminance(c) {
  const r = 0.2126 * LINEAR[c[0]];
  const g = 0.7152 * LINEAR[c[1]];
  const b = 0.0722 * LINEAR[c[2]];
  return (r + g) + b;
}

// ratio is the contrast of two colours, 1 to 21.
export function ratio(a, b) {
  let x = luminance(a), y = luminance(b);
  if (x < y) [x, y] = [y, x];
  return (x + 0.05) / (y + 0.05);
}

// measured writes a ratio cut, not rounded, to two decimals, so a pairing
// that fails never reads 4.50.
export function measured(r) {
  const n = Math.floor(r * 100);
  return Math.floor(n / 100) + '.' + String(n % 100).padStart(2, '0');
}

// mix lays b over a at k per cent, channel by channel, in whole numbers.
export function mix(a, b, k) {
  return [0, 1, 2].map((i) => Math.floor((a[i] * (100 - k) + b[i] * k + 50) / 100));
}

// smallest is mix(from, to, k) for the smallest k from 0 to 100 that
// reaches need against every surface, or to when none does.
function smallest(from, to, need, surfaces) {
  for (let k = 0; k <= 100; k++) {
    const c = mix(from, to, k);
    if (surfaces.every((s) => ratio(c, s) >= need)) return c;
  }
  return to;
}

// parse checks a theme's stored colours: exactly the nine keys, each six
// hex digits. It returns { colours } with [r, g, b] values, or
// { key, message } naming what is wrong, in the server's words.
export function parse(raw) {
  if (!raw || typeof raw !== 'object' || !Object.keys(raw).length) return { key: '', message: 'Give the theme’s colours.' };
  for (const k of Object.keys(raw)) {
    if (!KEYS.includes(k)) return { key: k, message: `"${k}" is not a theme colour. The colours are ${KEYS.join(', ')}.` };
  }
  const colours = {};
  for (const k of KEYS) {
    if (!(k in raw)) return { key: k, message: `The colour "${k}" is missing. The colours are ${KEYS.join(', ')}.` };
    const c = parseHex(raw[k]);
    if (!c) return { key: k, message: `The colour "${k}" must be six hex digits after #, like #0d1026.` };
    colours[k] = c;
  }
  return { colours };
}

// derive works out the colours a client derives (docs/STYLE.md):
// each fill's partner text colour, faint, edge and yellow as text.
export function derive(c) {
  const { bg, panel, raise, ink, dim } = c;
  const partner = (fill) => (ratio(ink, fill) >= ratio(bg, fill) ? ink : bg);
  return {
    'on-lav': partner(c.lav), 'on-cyan': partner(c.cyan), 'on-pink': partner(c.pink), 'on-yel': partner(c.yel),
    faint: smallest(bg, dim, TEXT, [bg, panel]),
    edge: smallest(bg, dim, EDGE, [bg, panel, raise]),
    'yel-ink': smallest(c.yel, ink, TEXT, [bg, panel, raise]),
  };
}

// pairings are what check measures, in order: [fore, back, need].
export function pairings(kind) {
  const out = [];
  const on = (fore, need, ...backs) => { for (const b of backs) out.push([fore, b, need]); };
  const all = ['bg', 'panel', 'raise'];
  on('ink', TEXT, ...all);
  on('dim', TEXT, ...all);
  on('faint', TEXT, 'bg', 'panel');
  on('cyan', TEXT, ...all);
  on('pink', TEXT, ...all);
  on('yel-ink', TEXT, ...all);
  on('lav', TEXT, ...all); // mentions, channel names, labels, code
  on('on-lav', TEXT, 'lav');
  on('on-cyan', TEXT, 'cyan');
  on('on-pink', TEXT, 'pink');
  on('on-yel', TEXT, 'yel');
  on('edge', EDGE, ...all);
  on('faint', EDGE, 'raise');
  if (kind === 'dark') on('yel', EDGE, ...all);
  return out;
}

// token is a base or derived colour by its STYLE.md name.
export function token(c, d, name) {
  return d[name] || c[name];
}

export function kindFits(kind, c) {
  const g = luminance(c.bg), i = luminance(c.ink);
  if (kind === 'dark') return g < i;
  if (kind === 'light') return g > i;
  return false;
}

// message is a failing pairing in the server's words.
export function message(fore, back, need, r) {
  return `${fore} on ${back} is ${measured(r)}:1; ${need === EDGE ? 'a control edge' : 'text'} needs at least ${need}:1.`;
}

// measure returns every pairing with its ratio and whether it passes.
export function measure(kind, c) {
  const d = derive(c);
  return pairings(kind).map(([fore, back, need]) => {
    const r = ratio(token(c, d, fore), token(c, d, back));
    return { fore, back, need, ratio: r, ok: r >= need };
  });
}

// check returns the first pairing below its need, as
// { fore, back, need, ratio, message }, or null when the theme passes.
export function check(kind, c) {
  for (const p of measure(kind, c)) {
    if (!p.ok) return { ...p, message: message(p.fore, p.back, p.need, p.ratio) };
  }
  return null;
}

// problem says everything the server would refuse a theme for, first
// fault first, as the server says it; null when it may be saved.
export function problem(theme) {
  if (!theme || (theme.kind !== 'dark' && theme.kind !== 'light')) return 'kind is dark or light.';
  const got = parse(theme.colours);
  if (got.message) return got.message;
  if (!kindFits(theme.kind, got.colours)) {
    return theme.kind === 'dark' ? 'A dark theme’s ground (bg) must be darker than its ink.' : 'A light theme’s ground (bg) must be lighter than its ink.';
  }
  const f = check(theme.kind, got.colours);
  return f ? f.message : null;
}

// The base colour a pairing's failure is fixed by: the foreground's own
// colour, or the fill a partner text colour sits on.
const SOURCE = { faint: 'dim', edge: 'dim', 'yel-ink': 'yel', 'on-lav': 'lav', 'on-cyan': 'cyan', 'on-pink': 'pink', 'on-yel': 'yel' };
export function sourceOf(fore, back) {
  if (fore.startsWith('on-')) return SOURCE[fore];
  if (KEYS.includes(fore)) return fore;
  return SOURCE[fore] || back;
}

// failingKeys are the base colours with a failing pairing to their name.
export function failingKeys(kind, c) {
  const out = new Set();
  for (const p of measure(kind, c)) if (!p.ok) out.add(sourceOf(p.fore, p.back));
  return out;
}

// fix makes the smallest change to one base colour that passes: the
// colour is laid over black or over white, k per cent at a time, and the
// first k (in either direction; towards the side that adds contrast
// first on a tie) at which the whole theme passes wins. If no k makes
// the whole theme pass, the first k at which this colour's own pairings
// pass is taken. Returns the new hex, or null when nothing helps.
export function fix(kind, colours, key) {
  const got = parse(colours);
  if (got.message) return null;
  const c = got.colours;
  const from = c[key];
  const ends = [[0, 0, 0], [255, 255, 255]];
  // The side away from the ground first: lighter on dark, darker on light,
  // except for the ground steps themselves, which move away from the ink.
  const ground = ['bg', 'panel', 'raise'].includes(key);
  const lighter = (kind === 'dark') !== ground;
  const order = lighter ? [ends[1], ends[0]] : [ends[0], ends[1]];
  const passesAll = (x) => kindFits(kind, x) && !check(kind, x);
  const passesOwn = (x) => kindFits(kind, x) && !failingKeys(kind, x).has(key);
  for (const test of [passesAll, passesOwn]) {
    for (let k = 1; k <= 100; k++) {
      for (const end of order) {
        const next = { ...c, [key]: mix(from, end, k) };
        if (test(next)) return hex(next[key]);
      }
    }
  }
  return null;
}

// --- the built-in themes ---
//
// The guide's own sets (shared/tokens.css), which are not derived. A
// theme takes from the built-in theme of its kind everything it does not
// name. Keep in step with tokens.css; e2e/palette.mjs checks they agree.

export const BUILTIN = {
  dark: {
    bg: '#0d1026', panel: '#161a3d', raise: '#1d2250', line: '#2c3170', grid: '#1d2250',
    ink: '#efeaff', dim: '#a09dc9', faint: '#8b88b8', edge: '#6368bd',
    lav: '#b9a0ff', cyan: '#5df0e4', pink: '#ff5ccf', yel: '#ffd95e',
    'on-lav': '#0d1026', 'on-cyan': '#0d1026', 'on-pink': '#0d1026', 'on-yel': '#15173a', 'yel-ink': '#ffd95e',
    on: '#0d1026', paper: '#f5f2ff',
  },
  light: {
    bg: '#f5f2ff', panel: '#ffffff', raise: '#ebe6ff', line: '#c9c1ee', grid: '#dcd5f7',
    ink: '#15173a', dim: '#55527f', faint: '#6b6894', edge: '#7e73c2',
    lav: '#5b3fd6', cyan: '#006e66', pink: '#b3127f', yel: '#ffd95e',
    'on-lav': '#ffffff', 'on-cyan': '#ffffff', 'on-pink': '#ffffff', 'on-yel': '#15173a', 'yel-ink': '#7a5600',
    on: '#ffffff', paper: '#15173a',
  },
};
export const BUILTIN_HD = {
  dark: { lav: '#7fb2ff', pink: '#ff9a4d', yel: '#ffe066', 'yel-ink': '#ffe066' },
  light: { lav: '#1f56c4', cyan: '#00626b', pink: '#a8480a', yel: '#ffe066' },
};

// builtinTheme is a built-in as a theme object, for starting a copy.
export function builtinTheme(kind, hd) {
  const set = { ...BUILTIN[kind], ...(hd ? BUILTIN_HD[kind] : {}) };
  const colours = {};
  for (const k of KEYS) colours[k] = set[k];
  return { name: (kind === 'dark' ? 'Dark' : 'Light') + (hd ? ', high distinction' : ''), kind, colours, builtin: true };
}

// tokens works out every colour token of a theme, as CSS custom property
// values by name without the dashes: the nine base colours, the derived
// ones, and what the built-in theme of its kind gives the rest. contrast
// says increase contrast is on: lines take the edge colour, faint text
// becomes dim. A theme that does not pass gets null: it is never applied.
export function tokens(theme, contrast) {
  if (problem(theme)) return null;
  const c = parse(theme.colours).colours;
  const d = derive(c);
  const out = { ...BUILTIN[theme.kind] };
  for (const k of KEYS) out[k] = hex(c[k]);
  for (const [k, v] of Object.entries(d)) out[k] = hex(v);
  out.on = out['on-lav'];
  if (contrast) { out.line = out.edge; out.faint = out.dim; }
  return out;
}

// --- the .churrtheme file ---

// toFile is a theme as a .churrtheme file's text.
export function toFile(theme) {
  const colours = {};
  for (const k of KEYS) colours[k] = String(theme.colours[k]).toLowerCase();
  return JSON.stringify({ name: theme.name, kind: theme.kind, colours }, null, 2) + '\n';
}

// fromFile reads a .churrtheme file's text. It returns { theme } or
// { error } in words; a theme that fails the check is an error too.
export function fromFile(text) {
  let v;
  try { v = JSON.parse(text); } catch { return { error: 'This is not a theme file: it is not JSON.' }; }
  if (!v || typeof v !== 'object' || Array.isArray(v)) return { error: 'This is not a theme file.' };
  const name = typeof v.name === 'string' ? v.name.trim() : '';
  if (!name || name.length > 64) return { error: 'A theme’s name is 1 to 64 characters.' };
  const theme = { name, kind: v.kind, colours: v.colours };
  const p = problem(theme);
  if (p) return { error: p };
  const colours = {};
  for (const k of KEYS) colours[k] = v.colours[k].toLowerCase();
  return { theme: { name, kind: v.kind, colours } };
}

// fileName is a safe file name for a theme.
export function fileName(theme) {
  const base = String(theme.name || 'theme').toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 40) || 'theme';
  return base + '.churrtheme';
}
