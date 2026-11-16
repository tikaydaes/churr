// Building the page by hand: an element helper, icons, dialogs, menus,
// toasts and the few formatters every page shares.
//
// Nothing here ever parses a string as HTML. Values from the server become
// text nodes, so nothing a person typed can turn into markup.

import { draw as drawIcon } from './icons.js';

// h builds an element. Attributes that start with "on" are listeners;
// "class" is the class name; "style" is an object set through the style
// interface (the content security policy forbids inline style text);
// "dataset" is an object of data attributes. An attribute whose value is
// null, undefined or false is left out, and true gives the bare attribute.
// Children may be nodes, strings, numbers or (nested) lists of them; null,
// undefined and false are skipped.
export function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k === 'style') Object.assign(el.style, v);
    else if (k === 'dataset') Object.assign(el.dataset, v);
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (k.startsWith('aria-') || k === 'role' || k === 'for' || k === 'list' || k === 'form') el.setAttribute(k, v === true ? 'true' : v);
    else if (k in el) el[k] = v;
    else el.setAttribute(k, v === true ? '' : v);
  }
  append(el, kids);
  return el;
}

function append(el, kids) {
  for (const kid of kids.flat(Infinity)) {
    if (kid != null && kid !== false) el.append(kid);
  }
}

// fill replaces an element's children, skipping absent ones as h does.
export function fill(el, ...kids) {
  el.replaceChildren(...kids.flat(Infinity).filter((k) => k != null && k !== false));
  return el;
}

// icon draws one of the icons of /shared/icons.js. It is decoration unless
// given a label, in which case it is read out as an image with that name.
export function icon(name, label, cls) {
  return drawIcon(name, label, cls);
}

// iconButton is a button that shows only an icon. label is what it is
// called, for a screen reader and as a tooltip.
export function iconButton(name, label, onclick, cls) {
  return h('button', { type: 'button', class: 'icon-btn' + (cls ? ' ' + cls : ''), 'aria-label': label, title: label, onclick }, icon(name));
}

export function button(label, onclick, cls) {
  return h('button', { type: 'button', class: 'btn' + (cls ? ' ' + cls : ''), onclick }, label);
}

export function field(label, input, hint) {
  return h('label', { class: 'field' }, h('span', {}, label), input, hint && h('small', {}, hint));
}

export function check(label, input, hint) {
  return h('label', { class: 'check' }, input, h('span', {}, label, hint && h('small', {}, hint)));
}

// --- toasts ---

function toastHost() {
  let host = document.getElementById('toasts');
  if (!host) {
    host = h('div', { id: 'toasts', 'aria-live': 'polite' });
    document.body.append(host);
  }
  return host;
}

export function toast(message, isError) {
  const el = h('div', { class: 'toast' + (isError ? ' error' : ''), role: isError ? 'alert' : 'status' }, message);
  // A toast inside an open dialog's top layer would be hidden behind it,
  // so it goes into the dialog instead.
  const open = [...document.querySelectorAll('dialog[open]')].pop();
  (open || toastHost()).append(el);
  if (open) el.classList.add('in-dialog');
  setTimeout(() => el.remove(), isError ? 7000 : 3200);
}

// act runs an action and reports its failure instead of losing it. A
// passkey prompt the person closed is not an error worth shouting about.
export async function act(fn) {
  try {
    return await fn();
  } catch (err) {
    if (err && (err.name === 'NotAllowedError' || err.name === 'AbortError')) toast('Cancelled.', true);
    else if (err && err.name === 'InvalidStateError') toast('This device already holds a passkey for this account.', true);
    else if (err && err.silent) { /* already shown */ }
    else toast((err && err.message) || 'Something went wrong.', true);
    return undefined;
  }
}

// busy disables a button while its action runs, so a second press does
// nothing.
export async function busy(btn, fn) {
  if (btn.disabled) return undefined;
  btn.disabled = true;
  try { return await act(fn); } finally { btn.disabled = false; }
}

export async function copyText(text, what) {
  try {
    await navigator.clipboard.writeText(text);
    toast((what || 'Copied') + ' to the clipboard.');
  } catch {
    toast('The browser would not copy that. Select it and copy by hand.', true);
  }
}

// --- dialogs ---

// openDialog shows a modal dialog and resolves when it closes, with the
// value given to close(). The browser's <dialog> traps focus and returns
// it to where it was. build receives close and returns
// { body, foot } or a list of nodes for the body.
export function openDialog(title, build, opts = {}) {
  return new Promise((resolve) => {
    let result;
    const dlg = h('dialog', { class: opts.wide ? 'wide' : '', 'aria-label': title });
    const close = (value) => { result = value; dlg.close(); };
    const built = build(close, dlg);
    const body = Array.isArray(built) || built instanceof Node ? built : built.body;
    const foot = built && built.foot;
    dlg.append(
      h('div', { class: 'dialog-head' }, h('h2', {}, title), iconButton('close', 'Close', () => close(undefined))),
      h('div', { class: 'dialog-body' }, body),
      ...(foot ? [h('div', { class: 'dialog-foot' }, foot)] : []),
    );
    dlg.addEventListener('close', () => {
      // Something said inside the dialog just before it closed is still
      // worth reading: it moves out to the page.
      for (const t of dlg.querySelectorAll(':scope > .toast')) { t.classList.remove('in-dialog'); toastHost().append(t); }
      dlg.remove();
      resolve(result);
    });
    // A press on the backdrop closes, as Escape does.
    dlg.addEventListener('mousedown', (e) => { if (e.target === dlg) close(undefined); });
    document.body.append(dlg);
    dlg.showModal();
    const first = dlg.querySelector('[autofocus], .dialog-body input, .dialog-body textarea, .dialog-body select, .dialog-body button, .dialog-foot .btn.primary');
    if (first) first.focus();
  });
}

// confirmDialog asks a yes-or-no question. With opts.type the person must
// type that word to confirm, for the things that cannot be undone.
export function confirmDialog(title, message, opts = {}) {
  return openDialog(title, (close) => {
    const yes = h('button', { type: 'submit', class: 'btn ' + (opts.danger ? 'danger' : 'primary') }, opts.yes || 'Confirm');
    let typed;
    if (opts.type) {
      typed = h('input', { autocomplete: 'off', spellcheck: false, autocapitalize: 'none', oninput: () => { yes.disabled = typed.value.trim() !== opts.type; } });
      yes.disabled = true;
    }
    const form = h('form', { class: 'stack', id: 'confirm-form', onsubmit: (e) => { e.preventDefault(); if (!yes.disabled) close(true); } },
      [].concat(message).map((m) => (typeof m === 'string' ? h('p', {}, m) : m)),
      typed && field(`Type “${opts.type}” to confirm`, typed));
    yes.setAttribute('form', 'confirm-form');
    return { body: form, foot: [button(opts.no || 'Cancel', () => close(false)), yes] };
  }).then((v) => v === true);
}

// promptDialog asks for one line of text.
export function promptDialog(title, label, opts = {}) {
  return openDialog(title, (close) => {
    const input = h(opts.multiline ? 'textarea' : 'input', { value: opts.value || '', required: opts.required !== false, maxLength: opts.maxLength || 4000, placeholder: opts.placeholder || '', autocomplete: 'off' });
    const form = h('form', { class: 'stack', id: 'prompt-form', onsubmit: (e) => { e.preventDefault(); close(input.value); } },
      opts.message && h('p', {}, opts.message), field(label, input, opts.hint));
    const ok = h('button', { type: 'submit', class: 'btn primary' }, opts.yes || 'Save');
    ok.setAttribute('form', 'prompt-form');
    return { body: form, foot: [button('Cancel', () => close(undefined)), ok] };
  });
}

// --- menus ---

let closeOpenMenu = null;

// openMenu shows a small menu beside an element. items are
// { label, onclick, icon, danger, checked, href } or { sep: true } or
// { head: 'text' }; absent entries are skipped. Arrow keys move, Escape
// closes and focus goes back to the element the menu belongs to.
export function openMenu(anchor, items, opts = {}) {
  if (closeOpenMenu) closeOpenMenu();
  const list = h('ul', { class: 'menu', role: 'menu', 'aria-label': opts.label || 'Menu' });
  const close = (refocus) => {
    list.remove();
    document.removeEventListener('mousedown', outside, true);
    window.removeEventListener('resize', onResize);
    closeOpenMenu = null;
    if (anchor && anchor.setAttribute) anchor.setAttribute('aria-expanded', 'false');
    if (refocus && anchor && anchor.focus) anchor.focus();
  };
  const outside = (e) => { if (!list.contains(e.target)) close(false); };
  const onResize = () => close(false);
  for (const it of items.filter(Boolean)) {
    if (it.sep) { list.append(h('li', { class: 'sep', role: 'separator' })); continue; }
    if (it.head) { list.append(h('li', { class: 'head', role: 'presentation' }, it.head)); continue; }
    const role = it.checked != null ? 'menuitemradio' : 'menuitem';
    const inner = [it.icon && icon(it.icon), h('span', { class: 'grow' }, it.label, it.hint && h('small', { class: 'dim' }, ' ' + it.hint))];
    const el = it.href
      ? h('a', { href: it.href, role, target: it.target, rel: it.target ? 'noopener noreferrer' : null, onclick: () => close(false) }, inner)
      : h('button', { type: 'button', role, class: it.danger ? 'danger' : '', 'aria-checked': it.checked != null ? String(!!it.checked) : null,
        onclick: () => { close(true); it.onclick && it.onclick(); } }, inner);
    list.append(h('li', { role: 'none' }, el));
  }
  list.addEventListener('keydown', (e) => {
    const els = [...list.querySelectorAll('button, a')];
    const i = els.indexOf(document.activeElement);
    if (e.key === 'ArrowDown') { e.preventDefault(); els[(i + 1) % els.length].focus(); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); els[(i - 1 + els.length) % els.length].focus(); }
    else if (e.key === 'Home') { e.preventDefault(); els[0].focus(); }
    else if (e.key === 'End') { e.preventDefault(); els[els.length - 1].focus(); }
    else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); close(true); }
    else if (e.key === 'Tab') close(false);
  });
  // A menu opened from inside a dialog must live in the dialog, or the
  // dialog's top layer covers it.
  const host = (anchor && anchor.closest && anchor.closest('dialog')) || document.body;
  host.append(list);
  place(list, anchor, opts);
  if (anchor && anchor.setAttribute) { anchor.setAttribute('aria-haspopup', 'menu'); anchor.setAttribute('aria-expanded', 'true'); }
  document.addEventListener('mousedown', outside, true);
  window.addEventListener('resize', onResize);
  closeOpenMenu = () => close(false);
  const first = list.querySelector('button, a');
  if (first) first.focus();
  return close;
}

// place puts a floating element beside its anchor (an element, or a point
// { x, y }), kept inside the window.
export function place(el, anchor, opts = {}) {
  const r = anchor.getBoundingClientRect ? anchor.getBoundingClientRect() : { left: anchor.x, right: anchor.x, top: anchor.y, bottom: anchor.y };
  const w = el.offsetWidth, ht = el.offsetHeight;
  let x = opts.alignRight ? r.right - w : r.left;
  let y = opts.above ? r.top - ht - 4 : r.bottom + 4;
  if (y + ht > window.innerHeight - 8) y = Math.max(8, r.top - ht - 4);
  if (y < 8) y = 8;
  x = Math.max(8, Math.min(x, window.innerWidth - w - 8));
  el.style.left = x + 'px';
  el.style.top = y + 'px';
}

// --- formatting ---

const pad = (n) => String(n).padStart(2, '0');

export function clock(iso) {
  const d = new Date(iso);
  return pad(d.getHours()) + ':' + pad(d.getMinutes());
}

export function sameDay(a, b) {
  const x = new Date(a), y = new Date(b);
  return x.getFullYear() === y.getFullYear() && x.getMonth() === y.getMonth() && x.getDate() === y.getDate();
}

export function dayName(iso) {
  const d = new Date(iso), now = new Date();
  if (sameDay(d, now)) return 'Today';
  if (sameDay(d, new Date(now.getTime() - 864e5))) return 'Yesterday';
  return d.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long', year: d.getFullYear() === now.getFullYear() ? undefined : 'numeric' });
}

export function dateTime(iso) {
  if (!iso) return 'Never';
  return new Date(iso).toLocaleString(undefined, { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' });
}

export function day(iso) {
  if (!iso) return 'Never';
  return new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
}

export function monthYear(iso) {
  return new Date(iso).toLocaleDateString(undefined, { month: 'long', year: 'numeric' });
}

// when is a short time for a list: the clock today, otherwise the date.
export function when(iso) {
  if (!iso) return '';
  return sameDay(iso, new Date()) ? clock(iso) : new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short' });
}

export function ago(iso) {
  if (!iso) return 'never';
  const s = Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 1000));
  if (s < 60) return 'just now';
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
  if (s < 86400 * 30) return `${Math.floor(s / 86400)} d ago`;
  return day(iso);
}

export function until(iso) {
  if (!iso) return '';
  const s = Math.floor((new Date(iso).getTime() - Date.now()) / 1000);
  if (s <= 0) return 'now';
  if (s < 3600) return `${Math.ceil(s / 60)} min`;
  if (s < 86400) return sameDay(iso, new Date()) ? clock(iso) : `${Math.ceil(s / 3600)} h`;
  return day(iso);
}

export function duration(seconds) {
  if (seconds == null) return '';
  if (seconds % 86400 === 0 && seconds >= 86400) { const d = seconds / 86400; return d === 1 ? '1 day' : `${d} days`; }
  if (seconds % 3600 === 0 && seconds >= 3600) { const x = seconds / 3600; return x === 1 ? '1 hour' : `${x} hours`; }
  if (seconds % 60 === 0 && seconds >= 60) { const m = seconds / 60; return m === 1 ? '1 minute' : `${m} minutes`; }
  return seconds === 1 ? '1 second' : `${seconds} seconds`;
}

export function bytes(n) {
  if (n == null) return '';
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(n < 10240 ? 1 : 0)} KB`;
  if (n < 1024 ** 3) return `${(n / 1024 / 1024).toFixed(n < 10485760 ? 1 : 0)} MB`;
  return `${(n / 1024 ** 3).toFixed(1)} GB`;
}

export function plural(n, one, many) {
  return `${n} ${n === 1 ? one : (many || one + 's')}`;
}

// localInput turns a time into the value a datetime-local field takes, and
// fromLocalInput turns such a value back into an RFC 3339 time.
export function localInput(date) {
  const d = new Date(date);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
export function fromLocalInput(value) {
  return value ? new Date(value).toISOString() : undefined;
}

// initials and colour for an avatar that has no picture.
export function initials(name) {
  const parts = String(name || '?').trim().split(/\s+/);
  const a = [...(parts[0] || '?')][0] || '?';
  const b = parts.length > 1 ? [...parts[parts.length - 1]][0] : '';
  return (a + b).toUpperCase();
}
export function hue(id) {
  let n = 0;
  for (const c of String(id || '')) n = (n * 31 + c.charCodeAt(0)) >>> 0;
  return n % 4;
}

// avatar draws an account's picture, or its initials on one of the four
// house colours.
export function avatar(account, cls) {
  const name = (account && (account.display_name || account.username)) || '?';
  if (account && account.avatar_url) {
    return h('img', { class: 'avatar ' + (cls || ''), src: account.avatar_url, alt: '', loading: 'lazy' });
  }
  return h('span', { class: `avatar c${hue(account && account.id)} ${cls || ''}`, 'aria-hidden': 'true' }, initials(name));
}

// fold makes text comparable without regard to case or accents, which is
// how names are matched everywhere (docs/CLIENTS.md section 9.26).
export function fold(s) {
  return String(s || '').normalize('NFD').replace(/\p{M}/gu, '').toLowerCase();
}

export function debounce(fn, ms) {
  let t;
  return (...args) => { clearTimeout(t); t = setTimeout(() => fn(...args), ms); };
}
