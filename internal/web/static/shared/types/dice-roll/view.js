// How a value of kind dice_roll looks (docs/DESIGN-MODULES.md section
// 9.1): a roll the server made, drawn as /roll's result always was.
//
//   card     a message whose view is a roll: a row of slots per row of
//            dice, then its total (shared/message/chance.js, whose reels
//            and artwork the kind uses until they move with it)
//   chip     inline dice, `[[2d6+3]]`: the typed text, each die and the
//            total in the sentence, the height of the line
//   tooltip  the roll in words, on the chip
//   dialog   the chip pressed: the whole roll, row by row, as the card
//
// Both spin once when the message arrives live; with reduced motion the
// result simply appears. The dice are drawn as /roll's are (chanceart.js:
// a d6 from the cube sprite, any other die or a coin on the fly); without
// the sprite a d6 keeps its number. Dice an edit wrote or changed say that
// nothing was rolled. Everything shown is text the server's data holds,
// set as text.

import { h, icon, openDialog } from '../../dom.js';
import { renderChance, slots, spinLive, spokenResult } from '../../message/chance.js';
import { dress } from '../../message/chanceart.js';
import { t } from '../words.js';

// asChance is a roll in the shape the reels draw.
const asChance = (d) => ({ kind: 'roll', dice: d.dice, rows: d.rows });

// tooltip is the roll in words: what a screen reader hears, and what the
// chip says on hover.
export const tooltip = (d) => spokenResult(asChance(d));

// card draws a message's roll. live is the message's spin clock when it
// arrived live, and null otherwise.
export function card(d, { live } = {}) {
  const c = asChance(d);
  const el = renderChance(c);
  if (live) spinLive(el, c, live, 'view');
  return el;
}

// dialog shows the whole roll of inline dice.
export function dialog(d, typed) {
  return openDialog(t('diceTitle'), () => [h('p', { class: 'instant-typed' }, h('q', {}, typed)), renderChance(asChance(d))]);
}

// chip draws inline dice in the sentence. removed: nothing was rolled for
// them. live and key: the message's spin clock and this chip's place in it.
export function chip(d, typed, { removed, live, key } = {}) {
  if (removed) {
    return h('span', { class: 'dice-inline removed', role: 'group', 'aria-label': t('diceRemovedLabel', { typed }) },
      typed, h('span', { class: 'dim' }, ' ' + t('diceRemoved')));
  }
  const c = asChance(d);
  const list = slots(c);
  const faces = list.map((x) => h('span', { class: 'slot' + (x.dropped ? ' dropped' : '') + (x.still ? ' still' : '') },
    x.sign ? h('span', { class: 'slot-sign', 'aria-hidden': 'true' }, x.sign) : null, h('span', { class: 'slot-face' }, x.text)));
  const totals = d.rows.map((row) => h('span', { class: 'chance-total' }, ' = ', h('strong', {}, String(row.total))));
  const said = tooltip(d);
  const el = h('span', { class: 'dice-inline', role: 'button', tabIndex: 0, title: said, 'aria-haspopup': 'dialog', 'aria-label': t('diceOpen', { typed, said }) },
    icon('dice', null, 'small'), h('span', { class: 'dice-typed' }, typed), ' ', faces, totals);
  const open = (ev) => { ev.preventDefault(); ev.stopPropagation(); dialog(d, typed); };
  el.addEventListener('click', open);
  el.addEventListener('keydown', (ev) => { if (ev.key === 'Enter' || ev.key === ' ') open(ev); });
  dress(c, [...el.querySelectorAll('.slot-face')], list);
  if (live) spinLive(el, c, live, key || 'dice');
  return el;
}
