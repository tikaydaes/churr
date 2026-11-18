// Holds the web chat's ECVRF verifier, internal/web/static/shared/fair/,
// to the library's vectors (testdata/ecvrf/vectors.json, the same file the
// Go and Dart copies run): every positive vector verifies to its beta and
// gives it by proofToHash, every negative vector is refused, and malformed
// keys and proofs are refused without throwing. It needs no server and no
// browser: `node e2e/fair.mjs`.

import { readFileSync } from 'node:fs';
import { isPublicKey, proofToHash, verify } from '../internal/web/static/shared/fair/ecvrf.js';

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

if (failed) {
  console.log(`fair: ${failed} failed, ${passed} passed`);
  process.exit(1);
}
console.log(`fair: ${passed} checks passed (${vf.vectors.length} vectors, ${vf.negative_vectors.length} negative)`);
