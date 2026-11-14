// Small pieces the admin sections share.

import { get, patch } from '/shared/api.js';
import { h, button, busy, toast, duration } from '/shared/dom.js';

// people fetches accounts by id, once each, and remembers them for the
// life of the page. Lists in the console hold ids; this turns them into
// names.
const known = new Map();
export async function people(ids) {
  const want = [...new Set(ids.filter(Boolean))].filter((id) => !known.has(id));
  for (let i = 0; i < want.length; i += 50) {
    try {
      const { accounts } = await get('/accounts', { ids: want.slice(i, i + 50).join(','), include_removed: true, limit: 200 });
      for (const a of accounts) known.set(a.id, a);
    } catch { /* names fall back to "someone" */ }
  }
  return known;
}
export function name(id) {
  const a = known.get(id);
  return a ? (a.display_name || a.username) : id ? 'someone' : 'the server';
}
export function handle(id) {
  const a = known.get(id);
  return a ? '@' + a.username : '';
}

// more adds a "Load more" button under a list that pages by cursor.
// load(cursor) returns { items, next }; add(items) draws them.
export function pager(load, add) {
  let cursor;
  const btn = button('Load more', () => busy(btn, step));
  const wrap = h('div', { class: 'pager' }, btn);
  async function step() {
    const page = await load(cursor);
    add(page.items);
    cursor = page.next;
    wrap.hidden = !cursor;
  }
  return { node: wrap, first: step };
}

// --- settings ---
//
// A setting is drawn as a row: what it is, a line of explanation, and its
// control. A change is saved at once with one `updateSettings` call, takes
// effect with no restart and is written to the audit log by the server.

const UNITS = [[60, 'minutes'], [3600, 'hours'], [86400, 'days']];

// save sends one key and says so. It returns the settings the server now
// holds, or undefined if the change was refused.
export async function saveSetting(key, value) {
  try {
    const out = await patch('/admin/settings', { [key]: value });
    toast('Saved.');
    return out.settings;
  } catch (err) {
    if (!err.silent) toast(err.message, true);
    return undefined;
  }
}

function row(title, hint, control, extra) {
  return h('div', { class: 'setting' }, h('div', { class: 'what' }, h('strong', {}, title), hint && h('small', {}, hint), extra), h('div', { class: 'control' }, control));
}

// settingRow draws one setting from its description:
//   { key, label, hint, type, options, nullable, nullLabel, min, max, fallback }
// types: switch, select, text, textarea, number, duration, bytes, checks
export function settingRow(d, settings, onSaved) {
  const cur = settings[d.key] !== undefined ? settings[d.key] : d.fallback;
  const commit = async (value, revert) => {
    const out = await saveSetting(d.key, value);
    if (out) { Object.assign(settings, out); if (onSaved) onSaved(out); } else if (revert) revert();
  };
  const label = d.label;
  if (d.type === 'switch') {
    const input = h('input', { type: 'checkbox', class: 'switch', checked: !!cur, 'aria-label': label, onchange: () => commit(input.checked, () => { input.checked = !input.checked; }) });
    const state = h('span', { class: 'small dim' }, cur ? 'on' : 'off');
    input.addEventListener('change', () => { state.textContent = input.checked ? 'on' : 'off'; });
    return row(label, d.hint, [input, state]);
  }
  if (d.type === 'select') {
    const input = h('select', { 'aria-label': label, onchange: () => commit(input.value, () => { input.value = String(cur); }) },
      d.options.map(([v, text]) => h('option', { value: v, selected: String(v) === String(cur) }, text)));
    return row(label, d.hint, input);
  }
  if (d.type === 'checks') {
    const on = new Set(cur || []);
    const boxes = d.options.map(([v, text]) => {
      const input = h('input', { type: 'checkbox', checked: on.has(v), onchange: () => commit(boxes.filter((b) => b.input.checked).map((b) => b.v), () => { input.checked = !input.checked; }) });
      return { v, input, node: h('label', { class: 'check' }, input, h('span', {}, text)) };
    });
    return row(label, d.hint, h('div', {}, boxes.map((b) => b.node)));
  }
  if (d.type === 'duration') {
    // Stored in whole seconds; typed as a number and a unit. A nullable
    // one has a "no limit" tick, which stores null.
    const pick = (secs) => { for (const [n] of [...UNITS].reverse()) if (secs && secs % n === 0) return n; return d.unit || 60; };
    const unit0 = cur ? pick(cur) : (d.unit || 86400);
    const num = h('input', { type: 'number', min: 0, step: 1, value: cur ? String(cur / unit0) : '', 'aria-label': label });
    const unit = h('select', { 'aria-label': label + ' unit' }, [[1, 'seconds'], ...UNITS].map(([n, text]) => h('option', { value: n, selected: n === (cur && cur % 60 ? 1 : unit0) }, text)));
    if (cur && cur % 60) num.value = String(cur);
    const none = d.nullable && h('input', { type: 'checkbox', checked: cur == null, 'aria-label': d.nullLabel });
    const sync = () => { if (none) { num.disabled = none.checked; unit.disabled = none.checked; } };
    const send = () => {
      if (none && none.checked) return commit(null);
      const secs = Math.round(Number(num.value) * Number(unit.value));
      if (!Number.isFinite(secs) || secs < (d.min || 0)) { toast(`Give at least ${duration(d.min || 0)}.`, true); return undefined; }
      return commit(secs);
    };
    num.addEventListener('change', send); unit.addEventListener('change', send);
    if (none) none.addEventListener('change', () => { sync(); if (none.checked || num.value) send(); else num.focus(); });
    sync();
    return row(label, d.hint, [num, unit], none && h('label', { class: 'check' }, none, h('span', {}, d.nullLabel)));
  }
  if (d.type === 'bytes') {
    const num = h('input', { type: 'number', min: 1, step: 1, value: cur ? String(Math.round(cur / 1048576)) : '', 'aria-label': label + ' in megabytes',
      onchange: () => { const mb = Number(num.value); if (mb >= 1) commit(Math.round(mb * 1048576)); } });
    return row(label, d.hint, [num, h('span', { class: 'small dim' }, 'MB')]);
  }
  if (d.type === 'number') {
    const num = h('input', { type: 'number', min: d.min, max: d.max, step: 1, value: cur != null ? String(cur) : '', 'aria-label': label,
      onchange: () => { if (num.value !== '') commit(Number(num.value), () => { num.value = String(cur); }); } });
    return row(label, d.hint, num);
  }
  // text and textarea are saved with a button, not on every key.
  const input = d.type === 'textarea'
    ? h('textarea', { rows: 4, maxLength: d.maxLength || 4000, value: cur || '', 'aria-label': label })
    : h('input', { value: cur || '', maxLength: d.maxLength || 200, placeholder: d.placeholder || '', 'aria-label': label, spellcheck: d.type !== 'code' });
  const save = button('Save', () => busy(save, () => commit(input.value.trim())), 'small');
  return h('div', { class: 'setting' }, h('div', { class: 'what' }, h('strong', {}, label), d.hint && h('small', {}, d.hint)),
    h('div', { class: 'control' + (d.type === 'textarea' ? ' tall' : '') }, input, save));
}

export function settingsCard(title, intro, list, settings, onSaved) {
  return h('section', { class: 'console-card' }, title && h('h2', {}, title), intro && h('p', { class: 'dim' }, intro),
    h('div', {}, list.map((d) => settingRow(d, settings, onSaved))));
}

export const COLOURS = ['lavender', 'cyan', 'pink', 'yellow', 'green', 'red', 'grey'];
