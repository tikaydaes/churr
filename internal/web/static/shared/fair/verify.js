// Checking a message's draw in the browser (docs/FAIR.md section 9), with
// nothing asked of any server: the message and the pinned VRF key history
// hold all it needs. A message's `fair` block checks out when its layout and
// rule are known; alpha is the one its conversation's chain gives for its n
// and prev; pk is the key its owner had when the message was made, by the
// history this browser pinned for its server; the proof verifies under pk
// and gives beta; and every value that took chance from the draw is what
// derive 1 gives for it (a roll: every die, every dropped die, every total).
// A check that fails says why; it never hides the roll.

import { verify } from './ecvrf.js';
import { DERIVE, Stream, sameRoll } from './derive.js';

const LABEL = 'churr-fair-1';

/** Bytes from base64url (padding allowed), or null. */
export function fromB64(s) {
  if (typeof s !== 'string') return null;
  let t = s.replace(/=+$/, '').replace(/-/g, '+').replace(/_/g, '/');
  while (t.length % 4) t += '=';
  try { return Uint8Array.from(atob(t), (c) => c.charCodeAt(0)); } catch { return null; }
}

const same = (a, b) => !!a && !!b && a.length === b.length && a.every((x, i) => x === b[i]);
const utf8 = (s) => new TextEncoder().encode(s);

/** alpha for draw n of a chain (docs/FAIR.md section 6). */
export function alphaOf(owner, chain, n, prev) {
  const o = utf8(owner), c = utf8(chain);
  const out = new Uint8Array(LABEL.length + 2 + o.length + 2 + c.length + 8 + prev.length);
  const v = new DataView(out.buffer);
  let at = 0;
  out.set(utf8(LABEL), at); at += LABEL.length;
  v.setUint16(at, o.length); at += 2; out.set(o, at); at += o.length;
  v.setUint16(at, c.length); at += 2; out.set(c, at); at += c.length;
  v.setUint32(at, Math.floor(n / 4294967296)); v.setUint32(at + 4, n >>> 0); at += 8;
  out.set(prev, at);
  return out;
}

/** The chain an alpha names, or null. */
export function chainOfAlpha(alpha) {
  const label = utf8(LABEL);
  if (!alpha || alpha.length < label.length + 4 || !same(alpha.subarray(0, label.length), label)) return null;
  const v = new DataView(alpha.buffer, alpha.byteOffset, alpha.byteLength);
  let at = label.length;
  at += 2 + v.getUint16(at);
  if (at + 2 > alpha.length) return null;
  const m = v.getUint16(at); at += 2;
  if (alpha.length !== at + m + 8 + 32) return null;
  return new TextDecoder().decode(alpha.subarray(at, at + m));
}

/** The key of a history in force at a moment: the last to have begun by then. */
export function keyAt(history, at) {
  const t = Date.parse(at);
  let out = null;
  for (const k of history || []) if (Date.parse(k.began) <= t) out = k.key;
  return out;
}

/**
 * Checks a message's draw. ctx: { server (this server's name), keys (the
 * pinned VRF key history: [{ key, began }]), chains (the names the
 * conversation's chain may have: its id, and its stream's when shared) }.
 * Gives { state: 'verified' | 'failed' | 'none', why, block, objects }, where
 * objects lists each value that took chance: { where, i, ok }.
 */
export async function verifyMessage(m, ctx) {
  const block = m && m.fair;
  if (!block) return { state: 'none' };
  const fail = (why) => ({ state: 'failed', why, block });
  if (block.v !== 1) return fail('This proof is in a layout this app does not know.');
  if (block.derive !== DERIVE) return fail('This roll was worked out by a rule this app does not know.');
  const pk = fromB64(block.pk), alpha = fromB64(block.alpha), beta = fromB64(block.beta);
  const proof = fromB64(block.proof), prev = fromB64(block.prev);
  if (!pk || !alpha || !beta || !proof || !prev || prev.length !== 32 || !(block.n >= 1)) return fail('The proof is not whole.');
  const chain = chainOfAlpha(alpha);
  if (!chain || !(ctx.chains || []).includes(chain)) return fail('The proof is for another conversation.');
  if (!same(alpha, alphaOf(block.owner, chain, block.n, prev))) return fail('The proof is not for this place in the conversation.');
  if (block.owner !== ctx.server) return fail(`The key of ${block.owner}, which made this roll, is not known here.`);
  const key = fromB64(keyAt(ctx.keys, m.created_at));
  if (!key || !same(key, pk)) return fail('The roll was not proved with the key this server had at the time.');
  const r = await verify(pk, proof, alpha);
  if (!r.valid || !same(r.beta, beta)) return fail('The proof does not check out.');
  const objects = [];
  const check = async (where, v) => {
    if (!v || !v.draw) return;
    let ok = true;
    if (v.kind === 'dice_roll' && v.data && Array.isArray(v.data.rows)) ok = await sameRoll(new Stream(beta, v.draw.i), v.data.rows);
    objects.push({ where, i: v.draw.i, ok });
  };
  await check('view', m.view);
  for (const e of m.entities || []) if (e.type === 'object') await check('object@' + e.at, e.object);
  if (objects.some((o) => !o.ok)) return { ...fail('A die shown is not the one the proof gives.'), objects };
  return { state: 'verified', block, objects };
}
