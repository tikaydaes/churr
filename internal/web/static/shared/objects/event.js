// An event in a message (ruling R73): one range in the reader's zone and
// locale, as a control with "Add to calendar", which makes a calendar
// file on this device, and the time converter for its start.

import { h, icon, openMenu } from '../dom.js';
import { viewerZone } from '../instant/zone.js';
import { compact, dayWord, timeOfDay, full, zoneName } from '../instant/format.js';
import { openConverter } from '../instant/view.js';
import { ics } from './ics.js';
import { t } from './words.js';

const locale = () => (typeof navigator !== 'undefined' && navigator.language) || 'en';

// eventRange words an event: "Thu 24 Dec, 6:00 pm – 8:00 pm", the end's
// day only when it is another.
export function eventRange(e, { zone, locale: loc }) {
  const a = Date.parse(e.time), b = Date.parse(e.end);
  const o = { zone, locale: loc, precision: e.precision };
  const end = dayWord(a, o) === dayWord(b, o) ? timeOfDay(b, o) : compact(b, o);
  return `${compact(a, o)} – ${end}`;
}

// download saves text as a file.
function download(name, text, type) {
  const url = URL.createObjectURL(new Blob([text], { type }));
  const a = h('a', { href: url, download: name });
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

// eventControl draws an event entity. opts: { title, link, uid } for the
// calendar file (the message's first line and a link to it), remind.
export function eventControl(e, typed, opts = {}) {
  const a = Date.parse(e.time), b = Date.parse(e.end);
  if (Number.isNaN(a) || Number.isNaN(b)) return document.createTextNode(typed);
  const zone = viewerZone(), loc = locale();
  const label = t('eventLabel', { from: full(a, { zone, locale: loc, precision: e.precision }), to: full(b, { zone, locale: loc, precision: e.precision }), zone: zoneName(zone, a), typed });
  const el = h('button', { type: 'button', class: 'instant event', 'aria-label': label, 'aria-haspopup': 'menu', title: typed, dataset: { time: e.time, end: e.end } },
    icon('clock', null, 'small'), h('span', {}, eventRange(e, { zone, locale: loc })));
  el.addEventListener('click', (ev) => {
    ev.preventDefault(); ev.stopPropagation();
    openMenu(el, [
      { label: t('addToCalendar'), icon: 'download', onclick: () => download('event.ics', ics({ uid: opts.uid || `${e.time}-${e.end}@churr`, start: e.time, end: e.end, title: opts.title || 'Event', link: opts.link || '', now: new Date().toISOString() }), 'text/calendar') },
      { label: t('openConverter'), icon: 'clock', onclick: () => openConverter(e, typed, { remind: opts.remind }) },
    ], { label: t('eventMenu') });
  });
  return el;
}
