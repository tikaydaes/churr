// Checking a message's draw in the browser (docs/FAIR.md section 9), with
// nothing asked of any server: the message and the VRF key history pinned
// for the server that drew it hold all it needs. A message's `fair` block
// checks out when its layout and rule are known; alpha is exactly the input
// spelt from its second `t` and the message's own client_id, so a proof
// cannot be moved to another message; the second is near the message's
// time; pk is the key its owner had when the message was made, by the
// history this browser pinned for that server; the proof verifies under pk
// and gives beta; and every value that took chance from the draw is what
// derive 1 gives for it (a roll: every die, every dropped die, every total).
// A check that fails says why; a draw by a server whose history this browser
// was never given says it cannot be checked here, which is not a failure. It
// never hides the roll.

import { verify } from './ecvrf.js';
import { DERIVE } from './derive.js';
import { rederive } from './kinds.js';
import { read, KINDS, LEVEL } from '../types/catalogue.js';

// How far a draw's second may be from its message's time: the server's
// window, and the looser one for a message drawn by another server, whose
// clock may be off from this one's (internal/fair, Window and LinkWindow).
export const WINDOW = 10, LINK_WINDOW = 120;

/** Bytes from base64url (padding allowed), or null. */
export function fromB64(s) {
  if (typeof s !== 'string') return null;
  let t = s.replace(/=+$/, '').replace(/-/g, '+').replace(/_/g, '/');
  while (t.length % 4) t += '=';
  try { return Uint8Array.from(atob(t), (c) => c.charCodeAt(0)); } catch { return null; }
}

const same = (a, b) => !!a && !!b && a.length === b.length && a.every((x, i) => x === b[i]);
const utf8 = (s) => new TextEncoder().encode(s);

/** Whether a nonce is a UUID in its one spelling: lowercase, hyphenated. */
export const canonical = (s) => typeof s === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(s);

/** The input of a draw (docs/FAIR.md section 6): "<t>|<nonce>". */
export const alphaOf = (t, nonce) => `${t}|${nonce}`;

/** The key of a history in force at a moment: the last to have begun by then. */
export function keyAt(history, at) {
  const t = Date.parse(at);
  let out = null;
  for (const k of history || []) if (Date.parse(k.began) <= t) out = k.key;
  return out;
}

/**
 * Checks a message's draw. ctx: { keysOf(name) (the VRF key history pinned
 * for a server, { keys: [{ key, began }], changed }, or null when no source
 * has given one: getServer for this server, a shared conversation's
 * `shared.vrf_keys` for the others), server (this server's name, whose
 * draws are held to the narrower window) }.
 * Gives { state: 'verified' | 'failed' | 'unknown' | 'none', why, block,
 * objects }, where objects lists each value that took chance: { where, i,
 * ok }. 'unknown' is a draw this client holds no key history for, which it
 * cannot check here; 'failed' is a check that was made and did not hold.
 */
export async function verifyMessage(m, ctx) {
  const block = m && m.fair;
  if (!block) return { state: 'none' };
  const fail = (why) => ({ state: 'failed', why, block });
  if (block.v !== 1) return fail('This proof is in a layout this app does not know.');
  if (block.derive !== DERIVE) return fail('This roll was worked out by a rule this app does not know.');
  const pk = fromB64(block.pk), beta = fromB64(block.beta), proof = fromB64(block.proof);
  if (!pk || !beta || !proof || typeof block.alpha !== 'string' || !Number.isSafeInteger(block.t) || block.t < 1) return fail('The proof is not whole.');
  if (!canonical(m.client_id) || block.alpha !== alphaOf(block.t, m.client_id)) return fail('The proof is not this message\'s.');
  const window = block.owner === ctx.server ? WINDOW : LINK_WINDOW;
  if (!(Math.abs(block.t - Date.parse(m.created_at) / 1000) <= window)) return fail('The proof is for another time than the message\'s.');
  const history = ctx.keysOf ? ctx.keysOf(block.owner) : null;
  if (!history) {
    return { state: 'unknown', block, why: `It was drawn by ${block.owner}, and this conversation does not carry that server's key history, so it cannot be checked here.` };
  }
  if (history.changed) return fail(`${block.owner} now shows a different key history than the one this app pinned for it, so its proofs are not trusted.`);
  const key = fromB64(keyAt(history.keys, m.created_at));
  if (!key || !same(key, pk)) return fail(`The roll was not proved with the key ${block.owner} had at the time.`);
  const r = await verify(pk, proof, utf8(block.alpha));
  if (!r.valid || !same(r.beta, beta)) return fail('The proof does not check out.');
  const objects = [];
  const check = async (where, v) => {
    if (!v || !v.draw) return;
    // A kind with a composition of derive 1 is re-derived in full, from its
    // checked data; one this client does not know is only proved, not
    // re-derived, and says so.
    const r = read(v);
    let ok = true, known = false;
    if (r.step === 'kind' && !r.removed && !r.alt && Number.isSafeInteger(v.draw.i) && v.draw.i >= 0) {
      const again = await rederive(v.kind, r.data, beta, v.draw.i);
      if (again !== null) { ok = again; known = true; }
    } else if (!v.removed && Object.hasOwn(KINDS, v.kind) && v.level <= LEVEL && r.step !== 'kind') {
      // A kind this client checks, whose data does not hold together: it
      // took the draw, and what it shows is not what the draw gives.
      ok = false; known = true;
    }
    objects.push({ where, i: v.draw.i, ok, known, kind: v.kind, data: r.step === 'kind' ? r.data : null });
  };
  await check('view', m.view);
  for (const e of m.entities || []) if (e.type === 'object') await check('object@' + e.at, e.object);
  if (objects.some((o) => !o.ok)) return { ...fail('An outcome shown is not the one the proof gives.'), objects };
  return { state: 'verified', block, objects };
}
