// Instants on screen (ruling R69): the control drawn in a message for an
// `instant` entity, and the time converter it opens.

import { h, icon, iconButton, button, field, copyText, openDialog, toast } from '../dom.js';
import { viewerZone, allZones, validZone } from './zone.js';
import { compact, full, zoneName, relative } from './format.js';
import { t } from './words.js';

const ZONES_KEY = 'churr.instant.zones';
const locale = () => (typeof navigator !== 'undefined' && navigator.language) || 'en';

// The zones this browser's reader added to the converter. Kept in this
// browser only; a page that cannot store simply starts empty.
function savedZones() {
  try {
    const v = JSON.parse(localStorage.getItem(ZONES_KEY) || '[]');
    return Array.isArray(v) ? v.filter((z) => typeof z === 'string' && validZone(z)) : [];
  } catch { return []; }
}
function saveZones(list) {
  try { localStorage.setItem(ZONES_KEY, JSON.stringify(list)); } catch { /* nothing kept; the rows still work now */ }
}

// instantControl draws an instant entity: the moment in the reader's zone,
// compact, as a button that opens the converter. typed is the text it
// covers, as the author wrote it. opts.remind(iso), when given, sets a
// reminder at the instant.
export function instantControl(e, typed, opts = {}) {
  const ms = Date.parse(e.time);
  if (Number.isNaN(ms)) return document.createTextNode(typed);
  const zone = viewerZone();
  const shown = compact(ms, { zone, locale: locale(), precision: e.precision });
  const label = t('chipLabel', { when: full(ms, { zone, locale: locale(), precision: e.precision }), zone: zoneName(zone, ms), typed });
  const el = h('button', { type: 'button', class: 'instant', 'aria-label': label, title: typed, dataset: { time: e.time } },
    icon('clock', null, 'small'), h('span', {}, shown));
  el.addEventListener('click', (ev) => { ev.preventDefault(); ev.stopPropagation(); openConverter(e, typed, opts); });
  return el;
}

// openConverter shows the instant in the reader's zone, in the zone as
// written, in UTC and in the zones the reader added, with how long until or
// since, ticking live.
export function openConverter(e, typed, opts = {}) {
  const ms = Date.parse(e.time);
  const here = viewerZone();
  const loc = locale();
  return openDialog(t('converterTitle'), (close, dlg) => {
    const rel = h('p', { class: 'instant-rel', 'aria-live': 'off' });
    const rows = h('div', { class: 'instant-rows', role: 'list' });
    const row = (labelText, zone, removable) => h('div', { class: 'instant-row', role: 'listitem' },
      h('div', { class: 'grow' }, h('div', { class: 'instant-zone' }, labelText), h('div', { class: 'instant-when' }, full(ms, { zone, locale: loc, precision: e.precision }))),
      removable ? iconButton('close', t('removeZone', { zone: zone.replace(/_/g, ' ') }), () => { saveZones(savedZones().filter((z) => z !== zone)); draw(); }, 'small') : null);
    const draw = () => {
      const list = [row(`${t('yourTime')} · ${zoneName(here, ms)}`, here)];
      if (e.zone && e.zone !== here) list.push(row(`${t('asWritten')} · ${zoneName(e.zone, ms)}`, e.zone));
      if (here !== 'UTC' && e.zone !== 'Z' && e.zone !== 'UTC') list.push(row(t('utc'), 'UTC'));
      for (const z of savedZones()) if (z !== here && z !== e.zone) list.push(row(zoneName(z, ms), z, true));
      rows.replaceChildren(...list);
    };
    const tick = () => { rel.textContent = relative(ms, { locale: loc }); };
    draw(); tick();
    const timer = setInterval(() => { if (!dlg.isConnected) { clearInterval(timer); return; } tick(); }, 1000);

    // The zone search: names and cities, matched on any word.
    const results = h('ul', { class: 'instant-results', role: 'listbox', 'aria-label': t('addZone') });
    const search = h('input', { type: 'search', class: 'instant-search', placeholder: t('searchZones'), 'aria-label': t('searchZones'), autocomplete: 'off', spellcheck: false });
    const zones = allZones();
    const add = (z) => { const s = savedZones(); if (!s.includes(z)) saveZones([...s, z]); search.value = ''; results.replaceChildren(); draw(); search.focus(); };
    search.addEventListener('input', () => {
      const q = search.value.trim().toLowerCase().replace(/\s+/g, '_');
      if (!q) { results.replaceChildren(); return; }
      const found = zones.filter((z) => z.toLowerCase().includes(q)).slice(0, 8);
      results.replaceChildren(...(found.length ? found.map((z) => h('li', {}, h('button', { type: 'button', class: 'link-btn', role: 'option', onclick: () => add(z) }, zoneName(z, ms))))
        : [h('li', { class: 'dim' }, t('noZones'))]));
    });
    search.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter') { ev.preventDefault(); const first = results.querySelector('button'); if (first) first.click(); }
      if (ev.key === 'ArrowDown') { const first = results.querySelector('button'); if (first) { ev.preventDefault(); first.focus(); } }
    });

    const foot = [button(t('copyISO'), () => copyText(e.time, t('copied')))];
    if (opts.remind && ms > Date.now()) {
      foot.push(h('button', { type: 'button', class: 'btn primary', onclick: async () => {
        try { await opts.remind(e.time); toast(t('reminderSet', { when: compact(ms, { zone: here, locale: loc, precision: e.precision }) })); close(true); } catch (err) { if (!err.silent) toast(err.message, true); }
      } }, t('remindMe')));
    }
    return {
      body: [
        h('p', { class: 'instant-typed' }, h('span', { class: 'dim' }, t('typedAs') + ' '), h('q', {}, typed)),
        rows, rel,
        field(t('addZone'), search), results,
      ],
      foot,
    };
  });
}
