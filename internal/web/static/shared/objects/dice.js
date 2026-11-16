// Inline dice in a message (ruling R73): `[[2d6+3]]` with the result the
// server drew when the message was posted, drawn in the sentence. The
// reels spin once when the message arrives live, as a /roll result does.
// Dice an edit wrote or changed show that nothing was rolled.

import { h, icon } from '../dom.js';
import { slots, spinLive, spokenResult } from '../message/chance.js';
import { t } from './words.js';

// diceControl draws a dice entity. live() is the message's spin clock
// (spinLive) when it arrived live, and null otherwise.
export function diceControl(e, typed, { live } = {}) {
  if (e.removed || e.total == null) {
    return h('span', { class: 'dice-inline removed', role: 'group', 'aria-label': t('diceRemovedLabel', { typed }) },
      typed, h('span', { class: 'dim' }, ' ' + t('diceRemoved')));
  }
  const c = { kind: 'roll', dice: e.value, terms: e.terms || [], total: e.total };
  const faces = slots(c).map((x) => h('span', { class: 'slot' + (x.dropped ? ' dropped' : '') + (x.still ? ' still' : '') },
    x.sign ? h('span', { class: 'slot-sign', 'aria-hidden': 'true' }, x.sign) : null, h('span', { class: 'slot-face' }, x.text)));
  const el = h('span', { class: 'dice-inline', role: 'group', tabIndex: 0, title: typed, 'aria-label': t('diceLabel', { typed, said: spokenResult(c) }) },
    icon('dice', null, 'small'), h('span', { class: 'dice-typed' }, typed), ' ', faces, h('span', { class: 'chance-total' }, ' = ', h('strong', {}, String(e.total))));
  const clock = live && live();
  if (clock) spinLive(el, c, clock, 'dice@' + e.at);
  return el;
}
