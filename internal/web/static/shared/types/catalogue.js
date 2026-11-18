// The catalogue in the web chat (docs/DESIGN-MODULES.md sections 3 and
// 4): the kinds of value this client knows, its catalogue level, and the
// day-one rules for what it does not know. A value is an envelope, `{kind,
// level, data, alt, by, source, removed}`, on a whole message as its `view`
// or on a span of the text as an `object` entity. The text is the message;
// the value is a view of it.
//
// This file has no DOM in it, so Node can run it as it is (e2e/types.mjs).
// What each kind looks like (its chip, tooltip, dialog and card) is in
// draw.js, beside the kind's own files under shared/types/<kind>/.

import { diceRollData } from './dice-roll/data.js';

// LEVEL is the catalogue level this client reads in full (day-one rule 3).
// A value whose level is above it is drawn by its alt, or as its text.
export const LEVEL = 1;

// KINDS is every kind this client knows: the lowest level that reads it,
// its data's check (which gives what is drawn, or null), and whether it
// moves when it arrives live.
export const KINDS = {
  dice_roll: { level: 1, data: diceRollData, spins: true },
};

// How deep a value's renditions are followed: an alt of an alt is the
// most a kind needs; past that the text stands.
const MAX_ALTS = 3;

// read decides how a value is drawn: the three-step fallback (day-one
// rule 1). It gives
//
//   { step: 'kind', kind, data, removed, alt }   the kind draws it: data is
//       the checked copy (or null when removed); alt says it is the value's
//       generic rendition that is drawn
//   { step: 'text', newer }                      the text stands; newer says
//       a kind or level this client does not read was the reason, which is
//       the built-in state "this needs a newer app" (rule 4)
//
// Data that fails its check falls back a step and is no error to the reader
// (section 4.2). level is the client's own (LEVEL), given for tests.
export function read(v, level = LEVEL, depth = 0) {
  if (!v || typeof v !== 'object' || Array.isArray(v) || typeof v.kind !== 'string') return { step: 'text', newer: false };
  const def = Object.hasOwn(KINDS, v.kind) ? KINDS[v.kind] : null;
  let newer = false;
  if (def && Number.isSafeInteger(v.level) && v.level <= level) {
    if (v.removed === true) return { step: 'kind', kind: v.kind, data: null, removed: true, alt: depth > 0 };
    const data = def.data(v.data);
    if (data) return { step: 'kind', kind: v.kind, data, removed: false, alt: depth > 0 };
  } else {
    newer = true;
  }
  if (v.alt && depth < MAX_ALTS) {
    const r = read(v.alt, level, depth + 1);
    if (r.step === 'kind') return r;
    newer = newer || r.newer;
  }
  return { step: 'text', newer };
}

// spins says whether a message has a value that moves once when it
// arrives live: a roll, as its view or inline.
export function spins(m) {
  const moves = (v) => { const r = read(v); return r.step === 'kind' && !r.removed && KINDS[r.kind].spins; };
  return !!(m && ((m.view && moves(m.view)) || (m.entities || []).some((e) => e.type === 'object' && moves(e.object))));
}

// keepValues carries a message's values through an edit this client
// makes itself (a sealed chat's, where no server reads the text): day-one
// rule 5, values it does not understand kept untouched. Each value over a
// span of the old text is kept, the same object, wherever the same
// characters still stand among the new text, left to right, clear of the
// entities the new text has of its own; one whose characters are gone
// goes with them. A message's view is not the client's to change: an edit
// never touches it.
export function keepValues(oldText, oldEntities, newText, newEntities) {
  const ours = [...(newEntities || [])];
  const clear = (at, len) => ours.every((e) => e.at + e.len <= at || e.at >= at + len);
  const kept = [];
  let from = 0;
  for (const e of oldEntities || []) {
    if (e.type !== 'object' || !e.object) continue;
    const typed = String(oldText || '').slice(e.at, e.at + e.len);
    if (!typed) continue;
    let at = String(newText || '').indexOf(typed, from);
    while (at >= 0 && !clear(at, typed.length)) at = newText.indexOf(typed, at + 1);
    if (at < 0) continue;
    kept.push({ ...e, at, len: typed.length });
    from = at + typed.length;
  }
  return [...ours, ...kept].sort((a, b) => a.at - b.at);
}
