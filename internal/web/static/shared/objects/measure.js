// What a reader sees beside a measurement (ruling R73): the original, and
// in muted text the equivalent in the reader's system, metric or imperial,
// or nothing when the unit is already of that system or the reader turned
// it off. testdata/objects/conversions.json holds the cases both clients
// pass. No DOM.

import { UNITS } from './units.js';

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
  if (!u || !u.alt || !isFinite(v) || u.sys === system || (system !== 'metric' && system !== 'imperial')) return null;
  if (unit === '°C') return Math.round(v * 9 / 5 + 32) + '°F';
  if (unit === '°F') return Math.round((v - 32) * 5 / 9) + '°C';
  return number(v * u.size / UNITS[u.alt].size) + ' ' + u.alt;
}

// The converter (testdata/objects/units.json): a value in any unit of its
// category, and how it is written.

const toC = (v, u) => (u === '°C' ? v : u === '°F' ? (v - 32) * 5 / 9 : v - 273.15);
const fromC = (c, u) => (u === '°C' ? c : u === '°F' ? c * 9 / 5 + 32 : c + 273.15);

// to converts v from one unit to another of the same category.
export function to(v, from, unit) {
  if (UNITS[from].cat === 'temperature') return fromC(toC(v, from), unit);
  return v * UNITS[from].size / UNITS[unit].size;
}

const SUPER = { '-': '⁻', 0: '⁰', 1: '¹', 2: '²', 3: '³', 4: '⁴', 5: '⁵', 6: '⁶', 7: '⁷', 8: '⁸', 9: '⁹' };

// written is a converted value: four significant digits, halves away
// from zero, the whole part grouped with "," and the point "." (separators
// swaps them for a locale's), and a power of ten from 10^15 up and below
// 10^-6: "1.23×10¹⁵".
export function written(v) {
  if (v === 0 || !isFinite(v)) return '0';
  const sign = v < 0 ? '-' : '';
  const [m, ex] = Math.abs(v).toExponential(3).split('e');
  const e = Number(ex);
  const digits = m.replace('.', '');
  if (e >= 15 || e < -6) {
    const rest = digits.slice(1).replace(/0+$/, '');
    return sign + digits[0] + (rest ? '.' + rest : '') + '×10' + String(e).replace(/./g, (c) => SUPER[c]);
  }
  const p = e + 1;
  let whole, frac;
  if (p <= 0) { whole = '0'; frac = '0'.repeat(-p) + digits; } else if (p >= 4) { whole = digits + '0'.repeat(p - 4); frac = ''; } else { whole = digits.slice(0, p); frac = digits.slice(p); }
  frac = frac.replace(/0+$/, '');
  whole = whole.replace(/\B(?=(\d{3})+$)/g, ',');
  return sign + whole + (frac ? '.' + frac : '');
}

// separators puts a locale's grouping and decimal marks into a written
// value.
export function separators(s, locale) {
  let group = ',', point = '.';
  try {
    for (const p of new Intl.NumberFormat(locale).formatToParts(12345.6)) { if (p.type === 'group') group = p.value; if (p.type === 'decimal') point = p.value; }
  } catch { /* the written marks */ }
  return s.replace(/[,.]/g, (c) => (c === ',' ? group : point));
}

// joined is a number and a unit's symbol: "3.107 mi", "22.22°C".
export const joined = (n, unit) => n + (unit === '°C' || unit === '°F' ? '' : ' ') + UNITS[unit].sym;

// rows are the units the converter lists for a measurement, for a reader
// of system: every other unit of the category, the reader's system first,
// then the other system, then the rest. Each is { unit, text }, the text
// before the locale's marks.
export function rows(value, unit, system) {
  const u = UNITS[unit];
  if (!u) return [];
  const v = Number(value);
  const order = system === 'imperial' ? ['imperial', 'metric', 'other'] : ['metric', 'imperial', 'other'];
  const out = [];
  for (const s of order) {
    for (const [code, x] of Object.entries(UNITS)) {
      if (x.cat === u.cat && x.sys === s && code !== unit) out.push({ unit: code, text: joined(written(to(v, unit, code)), code) });
    }
  }
  return out;
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
