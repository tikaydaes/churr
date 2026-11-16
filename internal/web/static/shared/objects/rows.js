// The composer's row for each reading of an exact token (ruling R73;
// docs/CLIENTS.md, "Objects in the composer"), for the kinds after
// references and instants. One case per kind.

import { h, icon } from '../dom.js';
import { t } from './words.js';
import { convert, viewerSystem } from './measure.js';
import { eventRange } from './event.js';
import { viewerZone } from '../instant/zone.js';

// objectRowLabel words the row offering row.entity, typed as typed.
export function objectRowLabel(row, typed) {
  const e = row.entity || {};
  switch (row.kind) {
    case 'message_link': return h('span', {}, icon('link', null, 'small'), ' ', t('messageLinkRow'));
    case 'measure': {
      const alt = convert(e.value, e.unit, viewerSystem());
      return h('span', {}, icon('grid', null, 'small'), ' ', t(alt ? 'measureRowAlt' : 'measureRow', { typed, alt }));
    }
    case 'colour': {
      const swatch = h('span', { class: 'swatch', 'aria-hidden': 'true' });
      swatch.style.background = e.value;
      return h('span', {}, swatch, ' ', t('colourRow', { value: e.value }));
    }
    case 'event': return h('span', {}, icon('clock', null, 'small'), ' ', t('eventRow', { range: eventRange(e, { zone: viewerZone(), locale: (typeof navigator !== 'undefined' && navigator.language) || 'en' }) }));
    default: return h('span', {}, typed);
  }
}
