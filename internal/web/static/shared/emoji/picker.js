// The emoji picker: search, the ones used lately, the server's custom set
// and the standard emoji in Unicode's groups. It gives back either a
// character or `:name:` for a custom emoji.
//
// The standard list is shared/emoji/emoji.json, made from Unicode's own
// data file (e2e/make-emoji.py); nothing is fetched from anywhere else.

import { h, fill, place, debounce } from '../dom.js';

let closeOpen = null;

// openPicker shows the picker beside an element. src:
//   groups     the standard groups: [{ name, emoji: [[char, code], …] }]
//   aliases    Map code → char, for searching by the names people type
//   custom     the server's emoji: [{ name, url }]
//   recent     list of recently used values
//   onAdd()    optional: "Add emoji" is shown, and closes the picker
//              before calling it
//   onPick(value)
export function openPicker(anchor, src) {
  if (closeOpen) closeOpen();
  const search = h('input', { type: 'search', placeholder: 'Search emoji', 'aria-label': 'Search emoji', autocomplete: 'off' });
  const grid = h('div', { class: 'picker-grid' });
  const add = src.onAdd ? h('button', { type: 'button', class: 'btn small picker-add', onclick: () => { close(false); src.onAdd(); } }, 'Add emoji') : null;
  const box = h('div', { class: 'picker', role: 'dialog', 'aria-label': 'Choose an emoji' }, search, grid, add);
  const close = (refocus) => {
    box.remove();
    document.removeEventListener('mousedown', outside, true);
    closeOpen = null;
    if (refocus && anchor.focus) anchor.focus();
  };
  const outside = (e) => { if (!box.contains(e.target)) close(false); };
  const pick = (value) => { close(true); src.onPick(value); };
  const cell = (value, label, img) => h('button', { type: 'button', class: 'picker-cell', title: label, 'aria-label': label, onclick: () => pick(value) },
    img ? h('img', { class: 'emoji', src: img, alt: '' }) : value);
  const section = (name, cells) => (cells.length ? [h('div', { class: 'picker-head' }, name), h('div', { class: 'picker-cells' }, cells)] : []);
  const customCell = (e) => cell(`:${e.name}:`, `:${e.name}:`, e.url);
  const codeOf = new Map();
  for (const g of src.groups) for (const [ch, code] of g.emoji) codeOf.set(ch, code);

  const draw = () => {
    const q = search.value.trim().toLowerCase().replace(/^:|:$/g, '').replace(/\s+/g, '_');
    if (q) {
      const found = [];
      const seen = new Set();
      for (const [code, ch] of src.aliases || []) if (code.includes(q) && !seen.has(ch)) { seen.add(ch); found.push(cell(ch, ':' + code + ':')); }
      for (const g of src.groups) for (const [ch, code] of g.emoji) { if (found.length > 150) break; if (code.includes(q) && !seen.has(ch)) { seen.add(ch); found.push(cell(ch, ':' + code + ':')); } }
      const custom = (src.custom || []).filter((e) => e.name.includes(q)).map(customCell);
      fill(grid, section('Custom', custom), section('Found', found), !custom.length && !found.length ? h('p', { class: 'dim small' }, 'No emoji by that name.') : null);
      return;
    }
    const recent = (src.recent || []).map((v) => {
      const m = /^:([^:]+):$/.exec(v);
      if (!m) return cell(v, ':' + (codeOf.get(v) || 'emoji') + ':');
      const e = (src.custom || []).find((x) => x.name === m[1]);
      return e ? customCell(e) : null;
    }).filter(Boolean);
    fill(grid, section('Used lately', recent), section('Custom', (src.custom || []).filter((e) => !e.server_name).map(customCell)),
      src.groups.map((g) => section(g.name, g.emoji.map(([ch, code]) => cell(ch, ':' + code + ':')))));
  };
  search.addEventListener('input', debounce(draw, 120));
  box.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); close(true); }
    if (e.key === 'Enter' && e.target === search) { const first = grid.querySelector('.picker-cell'); if (first) { e.preventDefault(); first.click(); } }
  });
  draw();
  const host = (anchor.closest && anchor.closest('dialog')) || document.body;
  host.append(box);
  place(box, anchor, { above: true });
  document.addEventListener('mousedown', outside, true);
  closeOpen = () => close(false);
  search.focus();
}
