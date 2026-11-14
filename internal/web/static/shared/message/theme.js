// Themes in messages (docs/STYLE.md "Themes", "Sharing"): a card the
// server made for a link to one of its themes (`card.theme`), and an
// attached .churrtheme file, are both drawn as a theme card with "Try"
// and "Add".

import { h, toast, fill } from '../dom.js';
import { post } from '../api.js';
import { themeCard } from '../themecard.js';
import { fromFile } from '../palette.js';
import { tryTheme } from '../themes.js';

// addButtonFor makes the "Add" action of a theme: a copy of a theme on
// this server by its id, or a new theme from a file's colours.
function adder(theme) {
  return async (e) => {
    const btn = e && e.currentTarget;
    if (btn) btn.disabled = true;
    try {
      if (theme.id) await post(`/themes/${theme.id}/add`);
      else await post('/me/themes', { name: theme.name, kind: theme.kind, colours: theme.colours });
      toast(`“${theme.name}” is in your themes. Choose it in the settings, or edit it.`);
      if (btn) btn.textContent = 'Added';
    } catch (err) {
      if (btn) btn.disabled = false;
      if (!err.silent) toast(err.message, true);
    }
  };
}

function tryer(theme) {
  return () => {
    try {
      const add = h('button', { type: 'button', class: 'btn small', onclick: adder(theme) }, 'Add to my themes');
      tryTheme(theme, [add]);
    } catch (err) { toast(err.message, true); }
  };
}

// cardTheme draws the theme card of a message card. env.nameOf names the
// author.
export function cardTheme(c, env = {}) {
  const t = c.theme;
  const author = t.author_id && env.nameOf ? env.nameOf(t.author_id) : null;
  return themeCard(t, { author, onTry: tryer(t), onAdd: adder(t), link: c.preview_url || (t.id ? '/themes/' + t.id : null) });
}

export const isThemeFile = (f) => /\.churrtheme$/i.test(f.name || '') && !!f.url;

// A message is drawn again on every change, so each file is read once.
const read = new Map();
function contents(f) {
  const key = f.id || f.url;
  if (!read.has(key)) {
    read.set(key, fetch(f.url, { credentials: 'same-origin' }).then((r) => {
      if (!r.ok) throw new Error(`The file could not be fetched (${r.status}).`);
      return r.text();
    }));
    read.get(key).catch(() => read.delete(key));
  }
  return read.get(key);
}

// fileTheme draws an attached .churrtheme file as a theme card, once its
// contents are fetched from this server. A file that is not a theme that
// passes is drawn as a line saying why.
export function fileTheme(f) {
  const el = h('div', { class: 'theme-file' }, h('p', { class: 'loading small' }, 'Reading ' + f.name));
  contents(f).then((text) => {
    const got = fromFile(text);
    if (got.error) { fill(el, h('p', { class: 'note warn small' }, `${f.name} is not a theme that can be used: ${got.error}`)); return; }
    fill(el, themeCard(got.theme, { note: 'From the file ' + f.name, onTry: tryer(got.theme), onAdd: adder(got.theme) }));
  }).catch((err) => fill(el, h('p', { class: 'note warn small' }, err.message)));
  return el;
}
