// Holds the web chat's ECVRF verifier, internal/web/static/shared/fair/,
// to the library's vectors (testdata/ecvrf/vectors.json, the same file the
// Go and Dart copies run): every positive vector verifies to its beta and
// gives it by proofToHash, every negative vector is refused, and malformed
// keys and proofs are refused without throwing. It also runs the client's
// pin of a server's VRF key history (shared/fair/pin.js). It needs no
// server and no browser: `node e2e/fair.mjs`.

import { readFileSync } from 'node:fs';
import { isPublicKey, proofToHash, verify } from '../internal/web/static/shared/fair/ecvrf.js';
import { checkServerPin, comparePin } from '../internal/web/static/shared/fair/pin.js';

const file = new URL('../testdata/ecvrf/vectors.json', import.meta.url);
const vf = JSON.parse(readFileSync(file, 'utf8'));
const hex = (s) => Uint8Array.from(s.match(/../g) ?? [], (b) => parseInt(b, 16));
const toHex = (b) => Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');

let failed = 0, passed = 0;
const check = (ok, what) => {
  if (ok) passed++;
  else { failed++; console.log('FAIL fair: ' + what); }
};

if (vf.suite !== 'ECVRF-SECP256K1-SHA256-TAI' || !vf.vectors.length || !vf.negative_vectors.length) {
  console.log('FAIL fair: the vectors file is not the one expected');
  process.exit(1);
}

for (const v of vf.vectors) {
  const r = await verify(hex(v.pk), hex(v.pi), hex(v.alpha));
  check(r.valid && toHex(r.beta) === v.beta, `verify: ${v.label}`);
  check(toHex(await proofToHash(hex(v.pi))) === v.beta, `proofToHash: ${v.label}`);
  check(isPublicKey(hex(v.pk)), `isPublicKey: ${v.label}`);
}

for (const v of vf.negative_vectors) {
  const r = await verify(hex(v.pk), hex(v.pi), hex(v.alpha));
  check(r.valid === v.expected_verify && r.valid === false && r.beta === null, `refuse: ${v.description}`);
}

// Malformed input, as the Dart tests try it.
{
  const v = vf.vectors[0];
  const pk = hex(v.pk), pi = hex(v.pi), alpha = hex(v.alpha);
  const flipped = pk.slice(); flipped[0] ^= 1;
  const xp = hex('02fffffffffffffffffffffffffffffffffffffffffffffffffffffffefffffc2f');
  // The other parity is a point of the curve (the negation), but not the
  // key the proof was made with.
  check(!(await verify(flipped, pi, alpha)).valid, 'refuse a public key: wrong parity prefix');
  for (const [what, k] of [['x >= p', xp], ['empty key', new Uint8Array(0)],
    ['truncated key', pk.subarray(0, 32)], ['uncompressed prefix', Uint8Array.from([4, ...pk.subarray(1)])]]) {
    check(!(await verify(k, pi, alpha)).valid, `refuse a public key: ${what}`);
    check(!isPublicKey(k), `isPublicKey refuses: ${what}`);
  }
  let threw = 0;
  for (const p of [pi.subarray(0, 80), Uint8Array.from([...pi, 0]), new Uint8Array(0), new Uint8Array(81)]) {
    try { await proofToHash(p); } catch { threw++; }
  }
  check(threw === 4, 'proofToHash refuses wrong lengths and an undecodable Gamma');
  check(!(await verify(pk, pi, 'not bytes')).valid, 'verify refuses an alpha that is not bytes');
}

// The pin: the first sight is kept; the same history, or one that only adds
// entries after the pinned ones, is fine; anything else is a change.
{
  const a = { key: 'AA', began: '2026-11-01T00:00:00Z' };
  const b = { key: 'BB', began: '2026-11-10T00:00:00Z' };
  const c = { key: 'CC', began: '2026-11-10T00:00:00Z' };
  check(comparePin(null, [a]).state === 'new', 'pin: first sight is new');
  check(comparePin([a], [a]).state === 'same', 'pin: the same history');
  const grown = comparePin([a], [a, b]);
  check(grown.state === 'grown' && grown.added.length === 1, 'pin: an appended key is grown');
  check(comparePin([a, b], [a, c]).state === 'changed', 'pin: a replaced key is a change');
  check(comparePin([a, b], [a]).state === 'changed', 'pin: a shortened history is a change');
  check(comparePin([a], []).state === 'changed', 'pin: an empty history is a change');

  // Through storage, as the chat does on every load.
  const mem = new Map();
  const storage = { getItem: (k) => (mem.has(k) ? mem.get(k) : null), setItem: (k, v) => mem.set(k, v) };
  const server = (keys) => ({ name: 'a.example', vrf_keys: keys });
  check(checkServerPin(server([a]), storage).state === 'new', 'stored pin: first sight');
  check(checkServerPin(server([a]), storage).state === 'same', 'stored pin: the same on the next load');
  check(checkServerPin(server([c]), storage).state === 'changed', 'stored pin: a swapped key is a change');
  check(checkServerPin(server([a]), storage).state === 'same', 'stored pin: a change is not taken');
  check(checkServerPin(server([a, b]), storage).state === 'grown', 'stored pin: an added key');
  check(checkServerPin(server([a, b]), storage).state === 'same', 'stored pin: the added key is kept');
  const broken = { getItem: () => { throw new Error('blocked'); }, setItem: () => { throw new Error('blocked'); } };
  check(checkServerPin(server([a]), broken).state === 'new', 'stored pin: storage that throws is a first sight');
}

if (failed) {
  console.log(`fair: ${failed} failed, ${passed} passed`);
  process.exit(1);
}
console.log(`fair: ${passed} checks passed (${vf.vectors.length} vectors, ${vf.negative_vectors.length} negative)`);
