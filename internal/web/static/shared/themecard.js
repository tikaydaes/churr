// A theme drawn small: its swatches, and the theme card (docs/STYLE.md
// "Themes", "Sharing") with its name, author, "Try" and "Add". The card is
// what a theme link in a message, an attached .churrtheme file, the
// library and the page at /themes/{id} all show.
//
// The swatches are the theme's own colours, set through the style
// interface: they are a picture of the theme, not the page's colours.

import { h } from './dom.js';
import { KEYS, KEY_NAMES, tokens } from './palette.js';

// swatches draws a theme's nine base colours as a strip, with a sample
// of its text on its ground so the strip reads as a theme, not a palette.
export function swatches(theme, size) {
  const t = tokens(theme) || {};
  const strip = h('span', { class: 'swatches' + (size ? ' ' + size : ''), role: 'img', 'aria-label': `${theme.kind === 'light' ? 'Light' : 'Dark'} theme colours` });
  const sample = h('span', { class: 'sw-sample', style: { background: t.bg || theme.colours.bg, color: t.ink || theme.colours.ink } }, 'Aa');
  strip.append(sample);
  for (const k of KEYS) {
    if (k === 'ink') continue;
    strip.append(h('span', { class: 'sw', title: KEY_NAMES[k], style: { background: theme.colours[k] } }));
  }
  return strip;
}

// themeCard draws a theme as a card. opts:
//   author     the author's name, or null
//   onTry      () => …   "Try" (applies without saving)
//   onAdd      () => …   "Add" (a copy to one's own themes); absent hides it
//   added      true: "Add" reads "Added" and is off
//   link       an address for the name to lead to
//   extra      more nodes for the row of buttons
//   note       a line under the name
export function themeCard(theme, opts = {}) {
  const tryBtn = opts.onTry ? h('button', { type: 'button', class: 'btn small primary', onclick: opts.onTry }, 'Try') : null;
  const addBtn = opts.onAdd ? h('button', { type: 'button', class: 'btn small', disabled: !!opts.added, onclick: opts.onAdd }, opts.added ? 'Added' : 'Add') : null;
  const name = opts.link ? h('a', { class: 'tc-name', href: opts.link }, theme.name) : h('strong', { class: 'tc-name' }, theme.name);
  return h('div', { class: 'theme-card', dataset: { theme: theme.id || '' } },
    swatches(theme),
    h('div', { class: 'tc-what' },
      h('div', { class: 'tc-title' }, name, h('span', { class: 'tag ' + (theme.kind === 'light' ? 'yellow' : 'lavender') }, theme.kind)),
      h('small', { class: 'dim' }, opts.author ? 'by ' + opts.author : 'Colour theme'),
      opts.note ? h('small', { class: 'dim' }, opts.note) : null),
    h('div', { class: 'tc-does' }, tryBtn, addBtn, opts.extra || null));
}
