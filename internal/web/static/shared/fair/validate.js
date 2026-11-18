// "Validate result" (docs/DESIGN-FAIR.md section 12; the document is
// docs/FAIR.md section 11): a draw's alpha, beta, proof and public key, and
// the primitives asked of it with the outcome claimed, checked here with no
// server asked, each part on its own and each with its reason:
//
//   proof     the proof is valid for this key and alpha
//   beta      beta is that proof's output
//   outcome   each primitive, re-derived from beta, gives the claimed result
//             (one line per primitive)
//   alpha     alpha is well formed, "<unix seconds>|<nonce>", its time shown
//   key       whose key it is: the server named, by its published history,
//             or "key not attributed"
//
// The web chat's shield, the public page /validate and the app's view all
// read and write the one JSON document defined here. No DOM.

import { proofToHash, verify, PROOF_LENGTH, PUBLIC_KEY_LENGTH, BETA_LENGTH } from './ecvrf.js';
import { DERIVE, Stream, draw, isPrimitive } from './derive.js';

/** The document's layout, and where anyone reads how to check it without Churr. */
export const FORMAT = 1;
export const SUITE = 'ECVRF-SECP256K1-SHA256-TAI';
export const SPEC = 'https://github.com/tikaydaes/churr/blob/main/docs/FAIR.md';
export const LIBRARY = 'https://github.com/atikayda/ecvrf';

const b64 = (bytes) => btoa(String.fromCharCode(...bytes)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');

/** Bytes from base64url (padding allowed) or hex, or null. */
export function bytesOf(s) {
  if (typeof s !== 'string') return null;
  const t = s.trim();
  if (t.length % 2 === 0 && /^[0-9a-fA-F]+$/.test(t) && t.length >= 64) return Uint8Array.from(t.match(/../g), (x) => parseInt(x, 16));
  if (!/^[A-Za-z0-9_\-+/]*=*$/.test(t)) return null;
  let u = t.replace(/=+$/, '').replace(/-/g, '+').replace(/_/g, '/');
  while (u.length % 4) u += '=';
  try { return Uint8Array.from(atob(u), (c) => c.charCodeAt(0)); } catch { return null; }
}

const ALPHA = /^(0|[1-9][0-9]{0,15})\|([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$/;

/** An alpha read: { t, nonce } when it is "<unix seconds>|<nonce>", or null. */
export function readAlpha(alpha) {
  const m = typeof alpha === 'string' ? ALPHA.exec(alpha) : null;
  if (!m || !Number.isSafeInteger(Number(m[1]))) return null;
  return { t: Number(m[1]), nonce: m[2] };
}

/** A result or primitive in one spelling: keys in order, and nothing that is empty, 0 or false (FAIR.md section 4: a missing number is 0). */
export function canonical(v) {
  if (Array.isArray(v)) return v.map(canonical);
  if (v && typeof v === 'object') {
    const out = {};
    for (const k of Object.keys(v).sort()) {
      const x = v[k];
      if (x === undefined || x === null || x === false || x === 0 || (Array.isArray(x) && x.length === 0)) continue;
      out[k] = canonical(x);
    }
    return out;
  }
  return v;
}
const same = (a, b) => JSON.stringify(canonical(a)) === JSON.stringify(canonical(b));

/** A dice result compared by what the dice show: each row's terms' rolls, dropped dice and values, and its total. */
function sameRows(got, want) {
  if (!Array.isArray(want) || got.length !== want.length) return false;
  return got.every((r, i) => {
    const w = want[i];
    if (!w || r.total !== w.total || !Array.isArray(w.terms) || r.terms.length !== w.terms.length) return false;
    return r.terms.every((t, j) => t.value === w.terms[j].value && same(t.rolls || [], w.terms[j].rolls || []) && same(t.dropped || [], w.terms[j].dropped || []));
  });
}

/**
 * The document, made from its fields: alpha, beta, proof and pk as text,
 * owner (the server named, or ''), and objects, each { i, kind?,
 * primitives, results }.
 */
export function makeDocument({ alpha, beta, proof, pk, owner, objects }) {
  const doc = { churr_fair: FORMAT, suite: SUITE, derive: DERIVE };
  if (owner) doc.owner = owner;
  Object.assign(doc, { pk, alpha, beta, proof, objects: objects || [], spec: SPEC, library: LIBRARY });
  return doc;
}

/** The document a message's block and its objects make: bytes as base64url. */
export function documentOf(block, objects) {
  const text = (v) => (v instanceof Uint8Array ? b64(v) : String(v ?? ''));
  return makeDocument({ alpha: block.alpha, beta: text(block.beta), proof: text(block.proof), pk: text(block.pk), owner: block.owner, objects });
}

/** Reads pasted text as the document: { doc } or { error }. */
export function readDocument(text) {
  let doc;
  try { doc = JSON.parse(text); } catch { return { error: 'This is not JSON.' }; }
  if (!doc || typeof doc !== 'object' || Array.isArray(doc)) return { error: 'This is not a draw\'s document: it is not a JSON object.' };
  if (doc.churr_fair !== undefined && doc.churr_fair !== FORMAT) return { error: `This document is in layout ${doc.churr_fair}, which this page does not know.` };
  for (const k of ['alpha', 'beta', 'proof', 'pk']) if (typeof doc[k] !== 'string') return { error: `The document has no ${k}.` };
  if (doc.objects !== undefined && !Array.isArray(doc.objects)) return { error: 'The document\'s objects are not a list.' };
  return { doc: { ...doc, objects: doc.objects || [] } };
}

/** The document in a link's fragment, and back: the serving server never sees it. */
export const fragmentOf = (doc) => '#' + b64(new TextEncoder().encode(JSON.stringify(doc)));
export function fromFragment(hash) {
  const raw = bytesOf(String(hash || '').replace(/^#/, ''));
  if (!raw || raw.length === 0) return null;
  try { return readDocument(new TextDecoder().decode(raw)).doc || null; } catch { return null; }
}

/** The key of a history in force at a second: the last entry to have begun by then. */
function inForce(history, t) {
  let out = null;
  for (const e of history || []) if (Date.parse(e.began) <= t * 1000) out = e;
  return out;
}

/**
 * Checks a document. keys: { name, history } when the viewer named a server
 * and its published VRF key history ([{ key, began }]) is to hand (from its
 * /.well-known/churr or the browser's pin), { name, history: null, why } when
 * it could not be had, or null. Gives the verdicts, each { id, ok, why } and
 * an outcome's also { i, j, op }; ok is true, false, or null where nothing
 * could be said (a key not attributed).
 */
export async function validate(doc, keys = null) {
  const out = [];
  const pk = bytesOf(doc.pk), proof = bytesOf(doc.proof), beta = bytesOf(doc.beta);
  const alphaText = typeof doc.alpha === 'string' ? doc.alpha : '';
  // 1. The proof under the key and alpha.
  let output = null;
  if (!pk || pk.length !== PUBLIC_KEY_LENGTH) out.push({ id: 'proof', ok: false, why: `The public key is not ${PUBLIC_KEY_LENGTH} bytes of base64url or hex.` });
  else if (!proof || proof.length !== PROOF_LENGTH) out.push({ id: 'proof', ok: false, why: `The proof is not ${PROOF_LENGTH} bytes of base64url or hex.` });
  else {
    const r = await verify(pk, proof, new TextEncoder().encode(alphaText));
    out.push(r.valid
      ? { id: 'proof', ok: true, why: 'The proof is valid for this public key and alpha (ECVRF-SECP256K1-SHA256-TAI).' }
      : { id: 'proof', ok: false, why: 'The proof is not valid for this public key and alpha: it was made with another key, over another alpha, or changed.' });
    if (r.valid) output = r.beta;
  }
  // 2. Beta from the proof.
  if (!beta || beta.length !== BETA_LENGTH) out.push({ id: 'beta', ok: false, why: `Beta is not ${BETA_LENGTH} bytes of base64url or hex.` });
  else {
    const hashed = output || (proof && proof.length === PROOF_LENGTH ? await proofToHash(proof).catch(() => null) : null);
    const ok = !!hashed && hashed.every((x, i) => x === beta[i]);
    out.push(ok ? { id: 'beta', ok: true, why: 'Beta is the output of this proof.' }
      : { id: 'beta', ok: false, why: hashed ? 'Beta is not the output of this proof.' : 'The proof cannot be read, so its output cannot be worked out.' });
  }
  // 3. Each primitive re-derived from beta, in order on its object's stream.
  if (doc.derive !== undefined && doc.derive !== DERIVE) {
    out.push({ id: 'outcome', i: null, j: null, ok: false, why: `The outcomes are by rule derive ${doc.derive}, which this page does not know.` });
  } else if (beta && beta.length === BETA_LENGTH) {
    for (const o of doc.objects || []) {
      const i = o && Number.isSafeInteger(o.i) && o.i >= 0 ? o.i : null;
      const prims = Array.isArray(o && o.primitives) ? o.primitives : [];
      const results = Array.isArray(o && o.results) ? o.results : [];
      if (i === null) { out.push({ id: 'outcome', i: o && o.i, j: null, ok: false, why: 'An object has no index i, so its stream is not known.' }); continue; }
      const s = new Stream(beta, i);
      let broken = false;
      for (let j = 0; j < prims.length; j++) {
        const p = prims[j];
        const line = { id: 'outcome', i, j, op: p && p.op };
        if (broken) { out.push({ ...line, ok: false, why: 'Not checked: an earlier primitive of this object could not be drawn, so the stream\'s place is lost.' }); continue; }
        if (!isPrimitive(p)) { broken = true; out.push({ ...line, ok: false, why: 'This is not a primitive of derive 1, or is outside its limits.' }); continue; }
        const got = await draw(s, p);
        const want = results[j];
        const ok = want !== undefined && (p.op === 'dice' ? sameRows(got.rows, want && want.rows) : same(got, want));
        out.push({ ...line, ok, got, why: ok ? 'Re-derived from beta, it gives the claimed result.' : want === undefined ? 'No result is claimed for it.' : `Re-derived from beta, it gives ${JSON.stringify(canonical(got))}, not the claimed ${JSON.stringify(canonical(want))}.` });
      }
      if (results.length > prims.length) out.push({ id: 'outcome', i, j: prims.length, ok: false, why: 'More results are claimed than primitives asked.' });
    }
    if (!(doc.objects || []).some((o) => o && Array.isArray(o.primitives) && o.primitives.length)) out.push({ id: 'outcome', i: null, j: null, ok: null, why: 'No primitive was given, so no outcome was checked.' });
  } else {
    out.push({ id: 'outcome', i: null, j: null, ok: false, why: 'Without a beta nothing can be re-derived.' });
  }
  // 4. The alpha's shape, and its time.
  const a = readAlpha(alphaText);
  out.push(a ? { id: 'alpha', ok: true, t: a.t, time: new Date(a.t * 1000).toISOString(), why: `Alpha is "<unix seconds>|<nonce>": the second ${new Date(a.t * 1000).toISOString()}, and the nonce ${a.nonce}.` }
    : { id: 'alpha', ok: false, why: 'Alpha is not "<unix seconds>|<nonce>": a second in decimal with no sign or leading zeros, a bar, and a UUID in lowercase, hyphenated.' });
  // 5. Whose key it is.
  if (!keys || !keys.name) out.push({ id: 'key', ok: null, why: 'Key not attributed: name a server to check the key against its published history.' });
  else if (!Array.isArray(keys.history)) out.push({ id: 'key', ok: null, why: `Key not attributed: ${keys.why || `the key history of ${keys.name} could not be had.`}` });
  else {
    const text = pk ? b64(pk) : '';
    const entry = keys.history.find((e) => e && e.key === text);
    const now = a ? inForce(keys.history, a.t) : null;
    if (!entry) out.push({ id: 'key', ok: false, why: `This key is not in the key history ${keys.name} publishes.` });
    else if (a && (!now || now.key !== text)) out.push({ id: 'key', ok: false, why: `This key is ${keys.name}'s, but was not its key in force at the second alpha names.` });
    else out.push({ id: 'key', ok: true, why: `This is ${keys.name}'s key${a ? ', in force at the second alpha names' : ''}, by the history it publishes (began ${entry.began}).` });
  }
  return out;
}

/** Fetches a server's published VRF key history from its /.well-known/churr: { name, history } or { name, history: null, why }. */
export async function fetchHistory(name, fetchFn = globalThis.fetch) {
  const host = String(name || '').trim().toLowerCase();
  if (!/^[a-z0-9.-]+(:[0-9]+)?$/.test(host)) return { name: host, history: null, why: 'that is not a server\'s name.' };
  try {
    const r = await fetchFn(`https://${host}/.well-known/churr`, { credentials: 'omit', cache: 'no-cache' });
    if (!r.ok) return { name: host, history: null, why: `${host} answered ${r.status}.` };
    const doc = await r.json();
    if (!Array.isArray(doc.vrf_keys)) return { name: host, history: null, why: `${host} publishes no VRF key history.` };
    return { name: doc.name || host, history: doc.vrf_keys };
  } catch {
    return { name: host, history: null, why: `${host} could not be reached from here.` };
  }
}
