// The mark under a message whose chance was drawn (docs/FAIR.md): "Checking"
// while the browser checks the proof, then "Verified" or, plainly, "Not
// verified" with why. Pressed, it shows the proof itself, so anyone can
// check it again elsewhere. The roll stays shown whatever the check says.
// The check runs once per message and needs no server.

import { h, icon, openDialog } from '../dom.js';
import { verifyMessage } from './verify.js';

const WORDS = {
  checking: 'Checking the roll…',
  verified: 'Verified',
  failed: 'Not verified',
  title: 'The proof of this roll',
  says: {
    verified: 'Every die shown here is the one the server\'s proof gives. The proof was made with the key this server published, for this place in the conversation, and nobody, the server included, could have made it come out otherwise.',
    failed: 'This roll could not be checked, so it may not be what the server drew. It is shown as it arrived.',
  },
};

// The block's fields, in the order the proof is read.
const FIELDS = [['owner', 'Drawn by'], ['n', 'Draw number'], ['derive', 'Rule'], ['pk', 'Key'], ['alpha', 'Proved over (alpha)'],
  ['proof', 'Proof'], ['beta', 'Output (beta)'], ['prev', 'Previous proof (hash)']];

/** The check of a message's draw, made once: a promise kept on the message. */
export function checkOnce(m, ctx) {
  if (!m._fairCheck) m._fairCheck = verifyMessage(m, ctx).catch(() => ({ state: 'failed', why: 'The proof could not be read.', block: m.fair }));
  return m._fairCheck;
}

/** The proof shown in full. */
export function proofDialog(res) {
  const b = res.block || {};
  return openDialog(WORDS.title, () => [
    h('p', { class: 'fair-says' + (res.state === 'verified' ? '' : ' warn') }, icon(res.state === 'verified' ? 'check' : 'warn', null, 'small'), ' ',
      h('strong', {}, res.state === 'verified' ? WORDS.verified : WORDS.failed), ' — ', res.state === 'verified' ? WORDS.says.verified : res.why || WORDS.says.failed),
    h('dl', { class: 'fair-block' }, FIELDS.flatMap(([k, label]) => [h('dt', {}, label), h('dd', { class: k === 'owner' || k === 'n' || k === 'derive' ? '' : 'mono' }, String(b[k] ?? ''))])),
    h('p', { class: 'dim small' }, 'docs/FAIR.md says how to check it yourself.'),
  ]);
}

/** The mark for a message with a `fair` block, or null. */
export function fairMark(m, ctx) {
  if (!m || !m.fair || !ctx) return null;
  const label = h('span', {}, WORDS.checking);
  const el = h('button', { type: 'button', class: 'fair-mark checking', 'aria-live': 'polite' }, icon('shield', null, 'small'), ' ', label);
  let result = null;
  el.addEventListener('click', (ev) => { ev.stopPropagation(); if (result) proofDialog(result); });
  checkOnce(m, ctx).then((res) => {
    result = res;
    const ok = res.state === 'verified';
    el.className = 'fair-mark ' + (ok ? 'verified' : 'failed');
    el.replaceChildren(icon(ok ? 'check' : 'warn', null, 'small'), ' ', ok ? WORDS.verified : WORDS.failed);
    el.title = ok ? WORDS.says.verified : res.why || WORDS.says.failed;
    el.setAttribute('aria-haspopup', 'dialog');
  });
  return el;
}
