// Drawing the catalogue's values (docs/DESIGN-MODULES.md sections 3.2 and
// 4.1): a message's view and the objects in its text, each through its
// kind's own drawing, or through the three-step fallback when this client
// cannot (catalogue.js, read): the kind, else its generic rendition, else
// the text. The text is always there to fall back to, and it is set as
// text: nothing here parses a string as HTML.

import { h, icon } from '../dom.js';
import { read } from './catalogue.js';
import * as diceRoll from './dice-roll/view.js';
import { viewOf } from './chance/view.js';
import { t } from './words.js';

// VIEWS is how each kind this client knows looks: its card (a whole
// message), chip (in a sentence), tooltip and dialog.
const VIEWS = {
  dice_roll: diceRoll,
  ...Object.fromEntries(['coin_flip', 'pick', 'shuffle', 'card_draw', 'tarot_reading'].map((k) => [k, viewOf(k)])),
};

// drawObject draws an `object` entity: the value its typed characters
// stand for. live() is the message's spin clock when it arrived live, and
// null otherwise. A value this client cannot draw is its typed text; one
// that needs a newer app says so on hover.
export function drawObject(e, typed, { live } = {}) {
  const r = read(e.object);
  const view = r.step === 'kind' && VIEWS[r.kind];
  if (view && view.chip) {
    const clock = live ? live() : null;
    return view.chip(r.data, typed, { removed: r.removed, live: clock, key: 'object@' + e.at });
  }
  if (r.newer) return h('span', { class: 'object-newer', title: t('newerTip') }, typed);
  return typed;
}

// drawView draws a message's view as its kind's card. It gives { card }
// when the kind draws it, and otherwise { card: null, newer }, where the
// message's text is what stands, and newer says to add the note that a
// newer app shows more (newerNote).
export function drawView(m, { live } = {}) {
  if (!m || !m.view) return { card: null, newer: false };
  const r = read(m.view);
  const view = r.step === 'kind' && !r.removed && VIEWS[r.kind];
  if (view && view.card) return { card: view.card(r.data, { live: live || null }) };
  return { card: null, newer: r.newer };
}

// newerNote is the built-in state "this needs a newer app" of a card
// (day-one rule 4), under the text that stands for it.
export function newerNote() {
  return h('p', { class: 'needs-newer dim small' }, icon('info', null, 'small'), ' ', t('newerNote'));
}
