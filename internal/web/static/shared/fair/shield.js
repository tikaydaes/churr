// The shield (docs/DESIGN-FAIR.md section 13): the one mark of a draw's
// proof, drawn from this browser's OWN check of it and never from anything a
// card or a value says. A card names which outcomes of its message's draw it
// stands on (a `fair_mark`, which carries no verdict); a built-in chance kind
// (a roll, a coin, a pick, a shuffle, cards) shows one by default for every
// value of the message that took chance.
//
//   verified   a shield with a tick, in green (--green, a colour only the
//              verified shield uses): the proof, its output and every
//              outcome named check out
//   unknown    a shield with a question mark, in the neutral tone: "Cannot
//              be checked here", the drawing server's key history was never
//              given to this browser
//   failed     a warning triangle, in the warning tone: "Not verified", a
//              check was made and did not hold
//
// Each has its own shape and a spoken label, so colour is never the only
// sign. Pressed, it opens "Validate result" with the draw filled in, carried
// in the address's fragment so no server sees it. Nothing else may draw
// this mark: no kind, block or card has a way to name these icons, and the
// state comes only from verifyMessage below.

import { h, icon } from '../dom.js';
import { verifyMessage } from './verify.js';
import { validateLink } from './export.js';

export const WORDS = {
  checking: 'Checking the proof',
  verified: 'Verified',
  failed: 'Not verified',
  unknown: 'Cannot be checked here',
  says: {
    verified: 'The server\'s proof of this result checks out here: the proof, its output and every outcome shown. Nobody, that server included, could have made it come out otherwise. Press to see the proof.',
    failed: 'This result could not be checked, so it may not be what the server drew. It is shown as it arrived. Press to see why.',
    unknown: 'This result was drawn by a server whose key history this browser has not been given, so its proof cannot be checked here. Press to check it yourself.',
  },
};

const ICONS = { checking: 'shield', verified: 'shield-check', unknown: 'shield-unknown', failed: 'warn' };

/** The check of a message's draw, made once: a promise kept on the message. */
export function checkOnce(m, ctx) {
  if (!m._fairCheck) m._fairCheck = verifyMessage(m, ctx).catch(() => ({ state: 'failed', why: 'The proof could not be read.', block: m.fair }));
  return m._fairCheck;
}

/**
 * What a shield standing on some outcomes of a message says, from the
 * message's own check: { state, why }. objects: the object indices named;
 * none is every value that took chance.
 */
export function judge(res, objects) {
  if (!res || res.state === 'none') return { state: 'failed', why: 'It stands on a draw this message does not carry.' };
  if (res.state !== 'verified') return { state: res.state, why: res.why || '' };
  const named = Array.isArray(objects) && objects.length ? objects : null;
  const of = res.objects || [];
  if (named) {
    for (const i of named) {
      const o = of.find((x) => x.i === i);
      if (!o) return { state: 'failed', why: `It names outcome ${i} of the draw, which nothing in this message took.` };
      if (o.removed) return { state: 'failed', why: `It names outcome ${i} of the draw, which an edit took away.` };
      if (!o.ok) return { state: 'failed', why: 'An outcome it stands on is not the one the proof gives.' };
      if (!o.known) return { state: 'unknown', why: 'An outcome it stands on is of a kind this browser cannot work out, so it is proved but not checked here.' };
    }
  } else if (of.length === 0) {
    return { state: 'failed', why: 'Nothing in this message took chance from its draw.' };
  } else if (of.some((o) => !o.known)) {
    return { state: 'unknown', why: 'An outcome here is of a kind this browser cannot work out, so it is proved but not checked here.' };
  }
  return { state: 'verified', why: WORDS.says.verified };
}

/** Draws a shield in a state: its shape, its tone and its spoken label. */
function paint(el, state, why) {
  el.className = 'fair-shield ' + state;
  el.dataset.state = state;
  const word = WORDS[state];
  el.replaceChildren(icon(ICONS[state], null, 'small'), h('span', { class: 'fair-shield-word' }, word));
  el.setAttribute('aria-label', state === 'checking' ? word : `${word}. ${why || WORDS.says[state]}`);
  el.title = why || WORDS.says[state] || word;
}

/**
 * The shield for outcomes of a message's draw, or null when there is no
 * draw to stand on and nothing names one. objects: what a card's fair_mark
 * names, or undefined for a built-in kind's (every value that took chance).
 * ctx: the chat's check context (verify.js). open(url) opens the Validate
 * result page; the default is a new tab.
 */
export function shieldFor(m, ctx, objects, { open } = {}) {
  if (!m || !ctx || (!m.fair && objects === undefined)) return null;
  const el = h('button', { type: 'button', class: 'fair-shield checking' });
  paint(el, 'checking');
  el.addEventListener('click', async (ev) => {
    ev.stopPropagation();
    const url = await validateLink(m, objects);
    if (open) open(url); else if (typeof window !== 'undefined') window.open(url, '_blank', 'noopener');
  });
  const done = checkOnce(m, ctx).then((res) => {
    const j = judge(res, objects);
    paint(el, j.state, j.why);
    return j;
  });
  el._judged = done;
  return el;
}
