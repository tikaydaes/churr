// What /help shows (rulings R67 and R74), worked out from the one source,
// testdata/commands/help.json (here as helptext.js, written by
// tools/help-gen/gen.py), the command table and the registries: the full
// entry of a command, and the section on the things people can type. No
// DOM, so e2e/help.mjs runs it under Node; the app's copy of this logic is
// app/lib/core/commands/help.dart, held to the same source.

import { HELP } from './helptext.js';
import { COMMANDS, HIDDEN_ALIASES, REPLACEABLE, aliasesOf, isBuiltin } from './commands.js';
import { DECKS, SPREADS, POSITIONS } from './message/cardnames.js';
import { MEASURE_UNITS } from './objects/units.js';

export { HELP };
export const LABELS = HELP.labels;

// words fills {name} and the like in a label.
export const words = (key, vars = {}) => String(LABELS[key] || '').replace(/\{(\w+)\}/g, (_, k) => (vars[k] ?? ''));

// canonical is the command a name stands for: itself, or the command it
// is another name of (/part is /leave, /kb is /ban).
export function canonical(name) {
  const n = String(name || '').toLowerCase().replace(/^\//, '');
  if (HIDDEN_ALIASES[n]) return HIDDEN_ALIASES[n];
  const e = HELP.commands[n];
  return e && e.same_as ? e.same_as : n;
}

// findListed finds what a typed word names among the commands listed here
// for this person (the server's list leaves out what is hidden for them):
// a command, or a built-in by another of its names. Null if none.
export function findListed(listed, word) {
  const n = String(word || '').trim().toLowerCase().replace(/^\//, '');
  if (!n || /\s/.test(n)) return null;
  const all = listed || [];
  const exact = all.find((c) => c.name === n && (c.kind !== 'builtin' || !HELP.commands[n] || !HELP.commands[n].same_as));
  if (exact) return exact;
  // Another name: only if the person may have the command it stands for.
  if (!isBuiltin(n) && !HELP.commands[n]) return null;
  const to = canonical(n);
  return all.find((c) => c.kind === 'builtin' && c.name === to) || null;
}

// entry is the full entry for a listed command: { name, kind, summary,
// usage, args, description, examples, aliases, where, who, notes,
// related, more }. Related commands are only those listed here, so a
// command hidden from this person is never named to them.
export function entry(c, listed) {
  const here = new Set((listed || []).map((x) => x.name));
  if (c.kind === 'module') {
    // A module's command: its help comes from the module, through the
    // server, with the names commands have here written in.
    const e = c.help || {};
    return {
      name: c.name, kind: c.kind, summary: c.description || words('noDescription'),
      usage: '/' + c.name + (c.hint ? ' ' + c.hint : ''), args: e.args || [], examples: e.examples || [],
      description: e.description || '', aliases: [], where: e.where || '', who: e.who || '', notes: [], related: [], more: null,
    };
  }
  if (c.kind !== 'builtin') {
    const notes = [isBuiltin(c.name) ? words('replacesBuiltin', { name: c.name }) : ''].filter(Boolean);
    return {
      name: c.name, kind: c.kind, summary: c.description || words('noDescription'),
      usage: '/' + c.name + (c.hint ? ' ' + c.hint : ''), args: [], examples: [],
      description: words(c.kind === 'bot' ? 'botCommand' : 'savedReply'),
      aliases: [], where: '', who: '', notes, related: [], more: null,
    };
  }
  const name = canonical(c.name);
  const e = HELP.commands[name] || {};
  const row = COMMANDS[name] || [];
  return {
    name, kind: 'builtin', summary: c.description || '',
    usage: '/' + name + (c.hint ? ' ' + c.hint : ''),
    args: e.args || [], description: e.description || '', examples: e.examples || [],
    aliases: aliasesOf(name).map((a) => '/' + a),
    where: [e.where, row[1] ? words('sealedYes') : words('sealedNo')].filter(Boolean).join(' '),
    who: [e.who, row[2] ? words('adminOnly') : ''].filter(Boolean).join(' '),
    notes: REPLACEABLE.has(name) ? [words('replaceable')] : [],
    related: (e.related || []).filter((r) => here.has(r)),
    more: more(e.more),
  };
}

// more is a list a command's entry draws from a registry: the decks of
// /draw, the spreads of /tarot, the set /mahjongg draws from. Each row is
// { name, aliases, count, about }.
export function more(kind) {
  switch (kind) {
    case 'decks':
      return { title: words('decks'), rows: Object.entries(DECKS).map(([k, d]) => ({
        name: k, aliases: d.aliases, about: HELP.decks[k] || '',
        count: words(k === 'mahjong' ? 'deckTiles' : 'deckCards', { n: d.cards.length }),
      })) };
    case 'spreads':
      return { title: words('spreads'), rows: Object.entries(SPREADS).map(([k, sp]) => ({
        name: k, aliases: sp.aliases, about: sp.title + ': ' + sp.positions.map((p) => POSITIONS[p] || p).join(', '),
        count: words('spreadCards', { n: sp.positions.length }),
      })) };
    case 'mahjong': {
      const d = DECKS.mahjong;
      return { title: words('decks'), rows: [{ name: 'mahjong', aliases: d.aliases, about: HELP.decks.mahjong || '', count: words('deckTiles', { n: d.cards.length }) }] };
    }
  }
  return null;
}

// matches says whether a row of "Things you can type" answers a search.
export function thingMatches(t, query) {
  const q = String(query || '').trim().toLowerCase();
  return !q || [t.title, t.type, t.becomes].some((s) => String(s || '').toLowerCase().includes(q));
}

// things is the section on what people can type, with what only this
// conversation knows filled in. ctx: { sealed, patterns: { server: [],
// here: [] } }. Each row is help.json's, plus { patterns } or { units }
// where it says live.
export function things(ctx = {}) {
  return HELP.things.map((t) => {
    if (t.live === 'patterns') return { ...t, patterns: patternLists(ctx) };
    if (t.live === 'units') return { ...t, units: unitTable() };
    return t;
  });
}

// patternLists is what the references row says about the patterns that
// apply here: { here, server, none, sealed }, each pattern as { name,
// prefix, example, host }.
export function patternLists(ctx = {}) {
  const show = (p) => {
    const value = patternExample(p.expression || '');
    return { name: p.name, prefix: p.prefix, example: p.prefix + value, host: hostOf(String(p.url_template || '').replace('{value}', value)) };
  };
  const server = ((ctx.patterns && ctx.patterns.server) || []).map(show);
  const here = ctx.sealed ? [] : ((ctx.patterns && ctx.patterns.here) || []).map(show);
  return { here, server, none: !here.length && !server.length, sealed: !!ctx.sealed };
}

function hostOf(url) {
  try { return new URL(url).host; } catch { return url; }
}

// unitTable sorts the measurement table's units into kinds: [{ title,
// units: [{ code, typed: […] }] }], each unit with every way of typing it.
export function unitTable() {
  return HELP.measure_kinds.map((k) => ({
    title: k.title,
    units: k.units.map((code) => ({ code, typed: MEASURE_UNITS.filter((u) => u[1] === code).map((u) => u[0]) })),
  }));
}

// patternExample writes a value a reference pattern's expression matches
// (docs/MESSAGE-FORMAT.md 3.2, the restricted form), for its example: each
// part once, or three times for + and *, or its count; digits counting
// from 1, letters from a, a class from its first member.
// (help.json, pattern_examples.)
export function patternExample(expression) {
  const src = String(expression || '');
  if (!src) return '123';
  let out = '';
  let i = 0;
  while (i < src.length) {
    let pool;
    if (src[i] === '\\') {
      const c = src[i + 1];
      pool = c === 'd' ? '1234567890' : c === 'w' ? 'abcdefghij' : c;
      i += 2;
    } else if (src[i] === '[') {
      const end = src.indexOf(']', i);
      pool = classMembers(src.slice(i + 1, end));
      i = end + 1;
    } else {
      pool = src[i];
      i += 1;
    }
    let n = 1;
    const r = /^(?:([?+*])|\{(\d+)(?:,(\d+))?\})/.exec(src.slice(i));
    if (r) {
      if (r[1]) n = r[1] === '?' ? 1 : 3;
      else n = Math.max(1, Number(r[2]));
      i += r[0].length;
    }
    for (let k = 0; k < n; k++) out += pool[k % pool.length];
  }
  return out;
}

// classMembers lists the characters a class stands for, in order: a-z
// for a range, \- for a hyphen itself.
function classMembers(body) {
  const toks = [];
  for (let i = 0; i < body.length; i++) {
    if (body[i] === '\\') { toks.push({ c: body[i + 1], esc: true }); i++; } else toks.push({ c: body[i], esc: false });
  }
  let out = '';
  for (let i = 0; i < toks.length; i++) {
    const dash = toks[i + 1];
    if (dash && dash.c === '-' && !dash.esc && i + 2 < toks.length) {
      for (let c = toks[i].c.charCodeAt(0); c <= toks[i + 2].c.charCodeAt(0); c++) out += String.fromCharCode(c);
      i += 2;
    } else out += toks[i].c;
  }
  return out;
}
