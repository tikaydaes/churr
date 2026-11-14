// The appearance settings: which of light and dark, the colour theme of
// each, palette and contrast, for this browser only (docs/STYLE.md,
// "Themes and settings" and "Themes"). The choice is applied by
// /shared/look.js, which every page loads in its <head>, and kept by
// /shared/themes.js; this module draws the controls that change it, for
// the web chat's settings and the account page.

import { h, fill, toast } from './dom.js';
import { get } from './api.js';
import { readLook, saveLook, choose, useServerDefaults, onLookChange } from './themes.js';
import { swatches } from './themecard.js';

export { readLook, saveLook };

// choiceRow draws a row of buttons of which one is pressed.
function choiceRow(label, name, options, value, onPick) {
  const row = h('div', { class: 'choices', role: 'group', 'aria-label': label });
  const draw = (current) => {
    row.replaceChildren(...options.map(([v, text]) => h('button', {
      type: 'button', class: 'btn small', 'aria-pressed': String(v === current), dataset: { look: name + ':' + (v || 'system') },
      onclick: () => { onPick(v); draw(v); },
    }, text)));
  };
  draw(value);
  return row;
}

// themePicker lists the themes a kind can be drawn with: this server's
// default, the built-in one, and the person's own of that kind.
function themePicker(kind, mine, opts) {
  const label = kind === 'dark' ? 'Colours for dark' : 'Colours for light';
  const group = h('div', { class: 'theme-picks', role: 'group', 'aria-label': label });
  const draw = () => {
    const v = readLook();
    const chosen = v[kind];
    const def = v.server && v.server[kind];
    const pick = (text, pressed, onclick, colours) => h('button', { type: 'button', class: 'theme-pick', 'aria-pressed': String(pressed), onclick },
      colours ? swatches({ kind, colours }, 'small') : null, h('span', {}, text));
    const own = mine.filter((t) => t.kind === kind);
    fill(group,
      pick(def ? `This server’s default: ${def.name}` : 'This server’s default (none set: built-in)', chosen == null, () => choose(kind, null), def && def.colours),
      pick(kind === 'dark' ? 'Built-in dark' : 'Built-in light', chosen === 'builtin', () => choose(kind, 'builtin')),
      own.map((t) => pick(t.name, !!(chosen && typeof chosen === 'object' && chosen.id === t.id), () => { try { choose(kind, t); } catch (err) { toast(err.message, true); } }, t.colours)),
      chosen && typeof chosen === 'object' && !own.some((t) => t.id === chosen.id)
        ? pick(chosen.name + ' (kept in this browser)', true, () => {}, chosen.colours) : null);
  };
  draw();
  const off = onLookChange(draw);
  if (opts && opts.onGone) opts.onGone(off);
  return group;
}

// themeControls draws the theme choice: one picker per kind, "use this
// server's defaults", and the ways to make and find themes. The person's
// own themes are fetched; until they arrive only the built-in and server
// choices show.
export function themeControls(opts = {}) {
  const wrap = h('div', { class: 'stack tight' });
  const draw = (mine, problemText) => {
    fill(wrap,
      h('span', { class: 'label' }, 'Colour themes'),
      h('p', { class: 'dim small' }, 'When following the system, the chosen dark theme is used in dark and the chosen light theme in light. Kept in this browser only; your saved themes are kept on the server.'),
      problemText ? h('p', { class: 'note warn small' }, problemText) : null,
      h('div', { class: 'cols-2 theme-kinds' },
        h('div', { class: 'stack tight' }, h('span', { class: 'label' }, 'Dark'), themePicker('dark', mine, opts)),
        h('div', { class: 'stack tight' }, h('span', { class: 'label' }, 'Light'), themePicker('light', mine, opts))),
      h('p', { class: 'row' },
        h('button', { type: 'button', class: 'btn small', onclick: () => { useServerDefaults(); toast('Using this server’s defaults.'); } }, 'Use this server’s defaults'),
        h('a', { class: 'btn small', href: '/chat/themes' }, 'Make or edit a theme'),
        h('a', { class: 'btn small', href: '/chat/themes/library' }, 'Theme library')));
  };
  draw([]);
  get('/me/themes').then((r) => draw(r.themes || [])).catch((err) => draw([], err.code === 'forbidden' ? null : 'Your saved themes could not be loaded: ' + err.message));
  return wrap;
}

// appearanceControls returns the settings as nodes, each with its label
// and a line saying what it does.
export function appearanceControls(opts) {
  const v = readLook();
  const part = (title, hint, control) => h('div', { class: 'stack tight' }, h('span', { class: 'label' }, title), hint && h('p', { class: 'dim small' }, hint), control);
  return [
    part('Theme', 'Follows your system unless you choose here. Kept in this browser only.',
      choiceRow('Theme', 'theme', [['', 'System'], ['dark', 'Dark'], ['light', 'Light']], v.theme || '', (x) => saveLook({ theme: x }))),
    themeControls(opts),
    part('Palette', 'For the built-in themes. High distinction swaps pink for orange and lavender for blue, so the colours stay apart with red-green colour blindness.',
      choiceRow('Palette', 'palette', [['', 'Standard'], ['hd', 'High distinction']], v.palette || '', (x) => saveLook({ palette: x }))),
    part('Increase contrast', 'Draws every line in the stronger edge colour and puts a line between areas. “System” follows your system’s setting.',
      choiceRow('Increase contrast', 'contrast', [['', 'System'], ['more', 'On'], ['normal', 'Off']], v.contrast || '', (x) => saveLook({ contrast: x }))),
  ];
}
