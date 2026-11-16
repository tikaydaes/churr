// Measurements on screen (ruling R73): the control drawn in a message for
// a `measure` entity, and the unit converter it opens, which works like
// the time converter of an instant.

import { h, copyText, openDialog } from '../dom.js';
import { convert, viewerSystem, rows, separators } from './measure.js';
import { UNITS } from './units.js';
import { t } from './words.js';

const locale = () => (typeof navigator !== 'undefined' && navigator.language) || 'en';

// measureControl draws a measurement: what was typed and, in muted text,
// the reader's system's equivalent, as a button that opens the converter.
export function measureControl(e, typed, system) {
  if (!UNITS[e.unit]) return document.createTextNode(typed);
  const sys = system || viewerSystem();
  const alt = convert(e.value, e.unit, sys);
  const el = h('button', { type: 'button', class: 'measure', 'aria-label': t('measureLabel', { typed, alt: alt ? ` (${alt})` : '' }) },
    typed, alt ? h('span', { class: 'measure-alt' }, ' (' + alt + ')') : null);
  el.addEventListener('click', (ev) => { ev.preventDefault(); ev.stopPropagation(); openUnitConverter(e, typed, sys); });
  return el;
}

// openUnitConverter lists the value in the other units of its kind, the
// reader's system first; each row copies its value.
export function openUnitConverter(e, typed, system) {
  const loc = locale();
  return openDialog(t('converterTitle'), () => {
    const list = rows(e.value, e.unit, system).map((r) => {
      const text = separators(r.text, loc);
      const u = UNITS[r.unit];
      return h('div', { role: 'listitem' }, h('button', {
        type: 'button', class: 'measure-row' + (u.sys === system ? ' mine' : ''), dataset: { unit: r.unit },
        'aria-label': t('copyRow', { value: text, name: u.name }), onclick: () => copyText(text, t('copiedRow', { value: text })),
      }, h('span', { class: 'measure-value' }, text), h('span', { class: 'measure-name' }, u.name)));
    });
    return { body: [
      h('p', { class: 'instant-typed' }, h('span', { class: 'dim' }, t('typedAs') + ' '), h('q', {}, typed)),
      h('div', { class: 'measure-rows', role: 'list' }, list),
    ] };
  });
}
