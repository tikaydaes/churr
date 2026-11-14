// Which colour theme this browser uses (ruling R61), kept with the other
// look preferences in localStorage["churr-look"] and applied by
// /shared/look.js:
//
//   theme     "" follows the system, "dark" or "light"
//   palette   "hd": the high-distinction palette of the built-in themes
//   dark      the dark theme chosen: absent for "this server's default"
//             (the built-in dark when the server has none), "builtin",
//             or a copy of a theme { id, name, kind, colours }
//   light     the same for light
//   server    the server's defaults as last seen { dark, light }
//   paint     the tokens each kind works out to, for look.js; absent for
//             a kind drawn with the built-in theme
//
// Following the system picks between the chosen dark and the chosen light
// theme. A theme that fails the contrast check is never applied.

import { tokens, problem } from './palette.js';

const KEY = 'churr-look';
export const KINDS = ['dark', 'light'];

export function readLook() {
  try { const v = JSON.parse(localStorage.getItem(KEY) || '{}'); return v && typeof v === 'object' ? v : {}; } catch { return {}; }
}

// copyOf is the part of a theme a choice keeps.
export function copyOf(t) {
  return { id: t.id || undefined, name: t.name, kind: t.kind, colours: { ...t.colours }, author_id: t.author_id || t.owner_id || undefined };
}

// themeFor says which theme a kind is drawn with, given a look record:
// { source: 'builtin' | 'server' | 'mine', theme? }.
export function themeFor(v, kind) {
  const choice = v[kind];
  if (choice && typeof choice === 'object' && !problem(choice)) return { source: 'mine', theme: choice };
  if (choice === 'builtin') return { source: 'builtin' };
  const def = v.server && v.server[kind];
  if (def && !problem(def)) return { source: 'server', theme: def };
  return { source: 'builtin' };
}

// paintOf works out look.js's token sets from a look record.
export function paintOf(v) {
  const paint = {};
  for (const kind of KINDS) {
    const t = themeFor(v, kind).theme;
    const set = t && tokens(t);
    if (set) paint[kind] = set;
  }
  return paint;
}

function write(v) {
  for (const k of Object.keys(v)) if (v[k] === '' || v[k] == null) delete v[k];
  v.paint = paintOf(v);
  if (!Object.keys(v.paint).length) delete v.paint;
  try { localStorage.setItem(KEY, JSON.stringify(v)); } catch { /* the choice lasts for this page only */ }
  if (window.churrLook) window.churrLook.apply();
  for (const fn of listeners) { try { fn(v); } catch (err) { console.error(err); } }
}

const listeners = new Set();
export function onLookChange(fn) { listeners.add(fn); return () => listeners.delete(fn); }

// saveLook merges a change into the record and applies it.
export function saveLook(change) {
  write({ ...readLook(), ...change });
}

// choose sets the theme of a kind: 'builtin', null for the server's
// default, or a theme. A theme that fails the check is refused with the
// reason.
export function choose(kind, choice) {
  if (choice && typeof choice === 'object') {
    const p = problem(choice);
    if (p) throw new Error(p);
    if (choice.kind !== kind) throw new Error(`That is a ${choice.kind} theme.`);
    choice = copyOf(choice);
  }
  saveLook({ [kind]: choice || undefined });
}

// useTheme makes a theme the one for its kind, and shows that kind now if
// the browser was set to the other one.
export function useTheme(t) {
  const v = readLook();
  choose(t.kind, t);
  if (v.theme && v.theme !== t.kind) saveLook({ theme: t.kind });
  stopTrying();
}

// useServerDefaults goes back to the server's defaults for both kinds.
export function useServerDefaults() {
  const v = readLook();
  delete v.dark; delete v.light;
  write(v);
}

// rememberServerDefaults keeps the server's defaults (`default_themes` in
// the server's description) for look.js, and repaints if they changed.
export function rememberServerDefaults(defaults) {
  const v = readLook();
  const next = {};
  for (const kind of KINDS) if (defaults && defaults[kind]) next[kind] = copyOf(defaults[kind]);
  if (JSON.stringify(next) === JSON.stringify(v.server || {})) return;
  v.server = Object.keys(next).length ? next : undefined;
  write(v);
}

// refreshChosen keeps a chosen copy in step with a saved theme that
// changed, and drops one whose theme was deleted (theme null, id given).
export function refreshChosen(t, deletedId) {
  const v = readLook();
  let changed = false;
  for (const kind of KINDS) {
    const c = v[kind];
    if (!c || typeof c !== 'object' || !c.id) continue;
    if (deletedId && c.id === deletedId) { delete v[kind]; changed = true; }
    else if (t && c.id === t.id) {
      if (t.kind === kind && !problem(t)) v[kind] = copyOf(t); else delete v[kind];
      changed = true;
    }
  }
  if (changed) write(v);
}

// --- trying a theme without keeping it ---

let bar = null;

// tryTheme shows a theme on this page until stopped or the page is left,
// with a bar saying so. actions are extra buttons for the bar: nodes.
export function tryTheme(t, actions = []) {
  const set = tokens(t);
  if (!set) throw new Error(problem(t) || 'This theme cannot be applied.');
  if (window.churrLook) window.churrLook.tryTokens(t.kind, set, t.name);
  if (bar) bar.remove();
  bar = document.createElement('div');
  bar.className = 'trying-bar';
  bar.setAttribute('role', 'status');
  const text = document.createElement('span');
  text.className = 'grow';
  text.append('Trying ', Object.assign(document.createElement('strong'), { textContent: t.name }), '. Nothing is kept until you choose to.');
  const use = Object.assign(document.createElement('button'), { type: 'button', className: 'btn small primary', textContent: 'Use it here' });
  use.addEventListener('click', () => { useTheme(t); });
  const stop = Object.assign(document.createElement('button'), { type: 'button', className: 'btn small', textContent: 'Stop trying' });
  stop.addEventListener('click', () => stopTrying());
  bar.append(text, ...actions, use, stop);
  document.body.append(bar);
  return bar;
}

export function stopTrying() {
  if (window.churrLook) window.churrLook.tryTokens(null);
  if (bar) { bar.remove(); bar = null; }
}
