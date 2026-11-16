// What a reader sees beside a measurement (ruling R73): the original, and
// in muted text the equivalent in the reader's system, metric or imperial,
// or nothing when the unit is already of that system or the reader turned
// it off. testdata/objects/conversions.json holds the cases both clients
// pass. No DOM.

// Each unit's system, the unit it becomes in the other system, and its
// size in the base unit of its kind (metres, kilograms, litres, metres a
// second). Temperatures are worked out apart.
const UNITS = {
  mm: ['metric', 'in', 0.001], cm: ['metric', 'in', 0.01], m: ['metric', 'ft', 1], km: ['metric', 'mi', 1000],
  in: ['imperial', 'cm', 0.0254], ft: ['imperial', 'm', 0.3048], yd: ['imperial', 'm', 0.9144], mi: ['imperial', 'km', 1609.344],
  mg: ['metric', 'oz', 1e-6], g: ['metric', 'oz', 0.001], kg: ['metric', 'lb', 1],
  oz: ['imperial', 'g', 0.028349523125], lb: ['imperial', 'kg', 0.45359237],
  ml: ['metric', 'fl oz', 0.001], l: ['metric', 'gal', 1],
  gal: ['imperial', 'l', 3.785411784], 'fl oz': ['imperial', 'ml', 0.0295735295625],
  'km/h': ['metric', 'mph', 1 / 3.6], 'm/s': ['metric', 'mph', 1], mph: ['imperial', 'km/h', 0.44704],
  '°C': ['metric', '°F'], '°F': ['imperial', '°C'],
};

// SYSTEMS a reader may choose; '' is "from this device's region".
export const SYSTEMS = ['metric', 'imperial', 'off'];

// The regions that measure in imperial units.
const IMPERIAL = new Set(['US', 'LR', 'MM']);

// regionSystem is the system a locale's region uses: "en-US" is imperial.
export function regionSystem(locale) {
  const m = /[-_]([A-Za-z]{2})\b/.exec(String(locale || ''));
  return m && IMPERIAL.has(m[1].toUpperCase()) ? 'imperial' : 'metric';
}

// number writes a converted value: whole from 100 up, otherwise three
// significant digits, without trailing zeros.
export function number(v) {
  if (Math.abs(v) >= 100) return String(Math.round(v));
  let s = v.toPrecision(3);
  if (s.includes('e')) return String(Number(s));
  if (s.includes('.')) s = s.replace(/0+$/, '').replace(/\.$/, '');
  return s === '-0' ? '0' : s;
}

// convert is what a reader of `system` sees beside value and unit: "3.11
// mi", or null for nothing.
export function convert(value, unit, system) {
  const u = UNITS[unit];
  const v = Number(value);
  if (!u || !isFinite(v) || system === 'off' || u[0] === system || (system !== 'metric' && system !== 'imperial')) return null;
  const to = u[1];
  if (unit === '°C') return Math.round(v * 9 / 5 + 32) + '°F';
  if (unit === '°F') return Math.round((v - 32) * 5 / 9) + '°C';
  return number(v * u[2] / UNITS[to][2]) + ' ' + to;
}

const KEY = 'churr-units';

// chosen is this browser's choice: 'metric', 'imperial', 'off', or '' for
// the region's.
export function chosen() {
  try { const v = localStorage.getItem(KEY) || ''; return SYSTEMS.includes(v) ? v : ''; } catch { return ''; }
}

export function choose(v) {
  try { if (v) localStorage.setItem(KEY, v); else localStorage.removeItem(KEY); } catch { /* for this page only */ }
}

// viewerSystem is the system this reader sees measurements in.
export function viewerSystem() {
  return chosen() || regionSystem(typeof navigator !== 'undefined' ? navigator.language : '');
}
