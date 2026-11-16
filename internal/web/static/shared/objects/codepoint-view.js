// Numbers in another base and code points on screen (MESSAGE-FORMAT.md
// section 3.2): the controls drawn in a message, and the converters they
// open, which work like the time converter of an instant.

import { h, copyText, openDialog } from '../dom.js';
import { numberRows, codePointFacts, codePointName } from './codepoint.js';
import { separators } from './measure.js';
import { t } from './words.js';

const locale = () => (typeof navigator !== 'undefined' && navigator.language) || 'en';

// The server's lookupCodePoints, for one code point.
async function serverLookup(value) {
  const { get } = await import('../api.js');
  const r = await get('/unicode?code_points=' + encodeURIComponent(value));
  return (r.code_points || [])[0] || null;
}

// A row of facts: what it is, its value large, and a press copies it.
function factRow(what, shown, copy, opts = {}) {
  return h('div', { role: 'listitem' }, h('button', {
    type: 'button', class: 'measure-row' + (opts.mine ? ' mine' : ''), 'aria-label': t('copyFact', { what, value: copy }),
    onclick: () => copyText(copy, t('copiedRow', { value: copy })),
  }, h('span', { class: 'measure-value' + (opts.mono ? ' mono' : '') }, shown), h('span', { class: 'measure-name' }, what)));
}

const typedLine = (typed) => h('p', { class: 'instant-typed' }, h('span', { class: 'dim' }, t('typedAs') + ' '), h('q', {}, typed));

// numberControl draws a number in another base as typed, as a button
// that opens the number converter.
export function numberControl(e, typed, ctx = {}) {
  let r;
  try { r = numberRows(e.value); } catch { return document.createTextNode(typed); }
  const el = h('button', { type: 'button', class: 'measure numeric', 'aria-label': t('numberLabel', { typed, decimal: separators(r.decimal, locale()) }) }, typed);
  el.addEventListener('click', (ev) => { ev.preventDefault(); ev.stopPropagation(); openNumberConverter(e, typed, ctx); });
  return el;
}

export function openNumberConverter(e, typed, ctx = {}) {
  const r = numberRows(e.value);
  return openDialog(t('numberTitle'), (close) => {
    const rows = [
      factRow(t('numberDecimal'), separators(r.decimal, locale()), r.copy.decimal, { mine: e.base === 10 }),
      factRow(t('numberHex'), r.hex, r.copy.hex, { mono: true, mine: e.base === 16 }),
      factRow(t('numberOctal'), r.octal, r.copy.octal, { mono: true, mine: e.base === 8 }),
      factRow(t('numberBinary'), r.binary, r.copy.binary, { mono: true, mine: e.base === 2 }),
    ];
    const body = [typedLine(typed), h('div', { class: 'measure-rows', role: 'list' }, rows)];
    if (r.codepoint) {
      const f = codePointFacts(r.codepoint);
      body.push(h('p', {}, h('button', { type: 'button', class: 'link-btn', onclick: () => { close(); openCodePoint({ value: r.codepoint }, r.codepoint, ctx); } },
        `${t('numberChar')}: ${f.shown.text} (${r.codepoint})`)));
    }
    return { body };
  });
}

// What is drawn beside the typed text: the character, or for what cannot
// be seen a dotted box with its short name, or a dotted circle and the
// mark.
function shownSpan(f) {
  return h('span', { class: 'cp-shown cp-' + f.shown.kind, 'aria-hidden': 'true' }, f.shown.text);
}

// codePointControl draws a code point as typed and the character beside
// it in muted text, as a button that opens what it is.
export function codePointControl(e, typed, ctx = {}) {
  let f;
  try { f = codePointFacts(e.value); } catch { return document.createTextNode(typed); }
  const el = h('button', { type: 'button', class: 'measure codepoint', 'aria-label': t('codePointLabel', { typed, shown: f.shown.kind === 'char' ? f.char : f.shown.text }) },
    typed, h('span', { class: 'measure-alt' }, ' '), shownSpan(f));
  el.addEventListener('click', (ev) => { ev.preventDefault(); ev.stopPropagation(); openCodePoint(e, typed, ctx); });
  return el;
}

// openCodePoint shows the character large, its encodings and escapes,
// and its name and category, which it asks the server for now (so the
// server learns which code point was looked at), once a session.
export function openCodePoint(e, typed, ctx = {}) {
  const f = codePointFacts(e.value);
  return openDialog(t('codePointTitle'), () => {
    const name = h('span', { class: 'measure-value' }, t('cpNameLoading'));
    const cat = h('span', { class: 'measure-value' }, '…');
    const nameRow = h('div', { role: 'listitem', class: 'measure-row static', 'aria-live': 'polite' }, name, h('span', { class: 'measure-name' }, t('cpName')));
    const catRow = h('div', { role: 'listitem', class: 'measure-row static' }, cat, h('span', { class: 'measure-name' }, t('cpCategory')));
    codePointName(f.value, ctx.lookupCodePoint || serverLookup).then((r) => {
      if (!r) { name.textContent = t('cpNameFailed'); cat.textContent = ''; catRow.hidden = true; return; }
      name.textContent = r.name || t('cpNameNone');
      cat.textContent = `${r.category_name} (${r.category})`;
    });
    const rows = [
      nameRow, catRow,
      factRow(t('cpCodePoint'), f.value, f.value, { mono: true, mine: true }),
      factRow(t('cpDecimal'), f.decimal, f.decimal, { mono: true }),
      factRow(t('cpUtf8'), f.utf8, f.utf8, { mono: true }),
      factRow(t('cpUtf16'), f.utf16, f.utf16, { mono: true }),
      factRow(t('cpHtml'), f.html, f.html, { mono: true }),
      factRow(t('cpJson'), f.json, f.json, { mono: true }),
      factRow(t('cpJs'), f.js, f.js, { mono: true }),
      factRow(t('cpPython'), f.python, f.python, { mono: true }),
    ];
    return {
      body: [
        typedLine(typed),
        h('button', { type: 'button', class: 'cp-big', 'aria-label': t('copyFact', { what: t('codePointTitle'), value: f.char }), onclick: () => copyText(f.char, t('copiedRow', { value: f.value })) }, shownSpan(f)),
        h('div', { class: 'measure-rows', role: 'list' }, rows),
      ],
    };
  });
}
