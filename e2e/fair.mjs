// Holds the web chat's ECVRF verifier, internal/web/static/shared/fair/,
// to the library's vectors (testdata/ecvrf/vectors.json, the same file the
// Go and Dart copies run): every positive vector verifies to its beta and
// gives it by proofToHash, every negative vector is refused, and malformed
// keys and proofs are refused without throwing. It also runs the client's
// pins of servers' VRF key histories (shared/fair/pin.js). It needs no
// server and no browser: `node e2e/fair.mjs`.

import { readFileSync } from 'node:fs';
import { isPublicKey, proofToHash, verify } from '../internal/web/static/shared/fair/ecvrf.js';
import { Histories, comparePin, pinHistory } from '../internal/web/static/shared/fair/pin.js';
import { DERIVE, Stream, block, draw, isPrimitive, sameRoll } from '../internal/web/static/shared/fair/derive.js';
import { verifyMessage } from '../internal/web/static/shared/fair/verify.js';

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
  const pin = (keys, st = storage) => pinHistory('a.example', keys, st);
  check(pin([a]).state === 'new', 'stored pin: first sight');
  check(pin([a]).state === 'same', 'stored pin: the same on the next load');
  const swapped = pin([c]);
  check(swapped.state === 'changed' && swapped.pinned.length === 1 && swapped.pinned[0].key === 'AA', 'stored pin: a swapped key is a change, checked against the pin');
  check(pin([a]).state === 'same', 'stored pin: a change is not taken');
  check(pin([a, b]).state === 'grown', 'stored pin: an added key');
  check(pin([a, b]).state === 'same', 'stored pin: the added key is kept');
  const broken = { getItem: () => { throw new Error('blocked'); }, setItem: () => { throw new Error('blocked'); } };
  check(pin([a], broken).state === 'new', 'stored pin: storage that throws is a first sight');

  // One pin per server name, from whichever source shows it: getServer for
  // this server, a shared conversation's vrf_keys for the rest.
  const hs = new Histories(storage);
  check(hs.of('b.example') === null, 'histories: a server nobody has shown is not known');
  hs.showShared({ shared: { vrf_keys: [{ server: 'b.example', keys: [b] }, { server: 'a.example', keys: [a, b] }] } });
  const hb = hs.of('b.example');
  check(hb && !hb.changed && hb.keys.length === 1 && hb.keys[0].key === 'BB', 'histories: a conversation pins another server\'s history');
  check(hs.of('a.example') && !hs.of('a.example').changed, 'histories: the same pin as getServer\'s for this server');
  hs.showShared({ shared: { vrf_keys: [{ server: 'b.example', keys: [c] }] } });
  check(hs.of('b.example').changed && hs.of('b.example').keys[0].key === 'BB', 'histories: another history shown for a server is a change, and the pin stands');
  hs.showShared({ shared: { vrf_keys: [{ server: 'b.example', keys: [b] }] } });
  check(hs.of('b.example').changed, 'histories: a change seen stays for the load');
  check(new Histories(storage).show('b.example', [b]).state === 'same', 'histories: the pin is kept across loads');
}

// The derivation (docs/FAIR.md): every vector of testdata/fair/derive.json,
// the stream's blocks, rejection, each primitive in order from one object's
// stream, and the primitives refused.
const df = JSON.parse(readFileSync(new URL('../testdata/fair/derive.json', import.meta.url), 'utf8'));
const deepEqual = (a, b) => {
  if (Array.isArray(a) || Array.isArray(b)) {
    return Array.isArray(a) && Array.isArray(b) && a.length === b.length && a.every((x, j) => deepEqual(x, b[j]));
  }
  if (a && b && typeof a === 'object' && typeof b === 'object') {
    const ka = Object.keys(a).sort(), kb = Object.keys(b).sort();
    return deepEqual(ka, kb) && ka.every((k) => deepEqual(a[k], b[k]));
  }
  return a === b;
};
check(df.derive === DERIVE && df.cases.length > 0 && df.invalid.length > 0, 'derive: the vectors file is derive 1\'s');
for (const v of df.streams) {
  check(toHex(await block(hex(v.beta), v.i, v.k)) === v.block, `derive: block ${v.i}/${v.k}`);
}
for (const v of df.below) {
  const s = new Stream(hex(v.beta), v.i);
  const got = [];
  for (const n of v.ns) got.push(await s.below(n));
  check(deepEqual(got, v.values) && s.used === v.words, `derive: below ${v.ns.join(',')}`);
}
for (const v of df.cases) {
  const s = new Stream(hex(v.beta), v.i);
  const got = [];
  for (const p of v.draws) got.push(await draw(s, p));
  check(deepEqual(got, v.results) && s.used === v.words, `derive: ${v.label}`);
  for (const [j, p] of v.draws.entries()) {
    if (p.op !== 'dice') continue;
    // A verifier re-derives the stored rows; a die changed is caught.
    const before = new Stream(hex(v.beta), v.i);
    for (const q of v.draws.slice(0, j)) await draw(before, q);
    check(await sameRoll(before, v.results[j].rows), `derive: re-derives ${v.label}`);
    const forged = structuredClone(v.results[j].rows);
    const t = forged[0].terms.find((x) => x.rolls);
    t.rolls[0] = t.rolls[0] === 1 ? 2 : 1;
    const again = new Stream(hex(v.beta), v.i);
    for (const q of v.draws.slice(0, j)) await draw(again, q);
    check(!(await sameRoll(again, forged)), `derive: a changed die is caught in ${v.label}`);
  }
}
for (const v of df.invalid) check(!isPrimitive(v.draw), `derive: refuses ${v.label}`);

// A message's draw checked as the chat checks it (shared/fair/verify.js),
// from testdata/fair/message.json, which the Go and Dart checks read too:
// it passes as it is, and fails, saying so, for each thing changed.
{
  const mv = JSON.parse(readFileSync(new URL('../testdata/fair/message.json', import.meta.url), 'utf8'));
  const keysOf = (keys) => (name) => (name === mv.server ? { keys, changed: false } : null);
  const ctx = { keysOf: keysOf(mv.keys), chains: mv.chains };
  const ok = await verifyMessage(mv.message, ctx);
  check(ok.state === 'verified' && ok.objects.length === 2 && ok.objects.every((o) => o.ok), 'verify: the message checks out');
  check((await verifyMessage({ text: 'plain' }, ctx)).state === 'none', 'verify: a message with no draw has nothing to check');
  const changed = async (what, fn, c = ctx) => {
    const m = structuredClone(mv.message);
    fn(m);
    const r = await verifyMessage(m, c);
    check(r.state === 'failed' && typeof r.why === 'string' && r.why.length > 0, `verify: fails for ${what}`);
  };
  await changed('a die of the card', (m) => { const t = m.view.data.rows[0].terms[0]; t.rolls[0] = t.rolls[0] === 1 ? 2 : 1; });
  await changed('an inline total', (m) => { m.entities[0].object.data.rows[0].total += 1; });
  await changed('another object index', (m) => { m.view.draw.i = 5; });
  await changed('another beta', (m) => { m.fair.beta = m.fair.prev; });
  await changed('another n', (m) => { m.fair.n += 1; });
  await changed('a message older than its key', (m) => { m.created_at = '2026-02-01T00:00:00.000Z'; });
  await changed('a key not pinned', (m) => { m.fair.pk = mv.keys[0].key; });
  await changed('another owner', (m) => { m.fair.owner = 'b.example'; });
  await changed('a rule not known', (m) => { m.fair.derive = 2; });
  await changed('another conversation', () => {}, { ...ctx, chains: ['01a1ee00-0000-7000-8000-0000000000aa'] });
  await changed('another pin', () => {}, { ...ctx, keysOf: keysOf([mv.keys[0]]) });
  await changed('a history that changed', () => {}, { ...ctx, keysOf: () => ({ keys: mv.keys, changed: true }) });
  // A draw by a server whose history this client was never given cannot be
  // checked here: it says so, and is not a failure.
  const unknown = await verifyMessage(mv.message, { ...ctx, keysOf: () => null });
  check(unknown.state === 'unknown' && unknown.why.includes(mv.server), 'verify: a draw by a server with no history given cannot be checked here');
  // The same draw by another server, checked under that server's history
  // as a shared conversation gives it.
  const remote = await verifyMessage(mv.message, { chains: mv.chains, keysOf: (n) => (n === mv.server ? { keys: mv.keys } : null) });
  check(remote.state === 'verified', 'verify: a draw checks under the history given for its owner');
}

if (failed) {
  console.log(`fair: ${failed} failed, ${passed} passed`);
  process.exit(1);
}
console.log(`fair: ${passed} checks passed (${vf.vectors.length} vectors, ${vf.negative_vectors.length} negative, ${df.cases.length} derive cases)`);
