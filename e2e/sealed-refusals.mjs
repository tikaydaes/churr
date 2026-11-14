// Refusals the web chat's sealed-chat library must make beyond the vector
// files: the cases of the Dart library's negative tests
// (app/test/core/sealed/negative_test.dart) and of the Go reference's, run
// on the same Web Crypto as the browser. Also the 64-bit edges, and the
// lock that keeps several tabs from spending one counter (tabs.js, run
// against an in-process stand-in for the Web Locks API here and in
// Chromium by web-sealed.mjs).
//
//   node e2e/sealed-refusals.mjs

import { fileURLToPath } from 'node:url';
import * as S from '../internal/web/static/shared/sealed/index.js';
import { exclusive } from '../internal/web/static/shared/sealed/tabs.js';

const utf8 = (s) => new TextEncoder().encode(s);
const dec = (b) => new TextDecoder().decode(b);

// Any number of deterministic values, whatever they are for.
class StreamRandom {
  constructor(seed) { this.seed = seed; this.i = 0; }
  take(what, n) {
    let out = new Uint8Array(0);
    while (out.length < n) out = S.concat(out, sha256Sync(`${this.seed}/${this.i++}/${what}`));
    return out.slice(0, n);
  }
}
// A synchronous SHA-256 for StreamRandom, from Node's own crypto module:
// only the test's stand-in generator uses it.
import { createHash } from 'node:crypto';
const sha256Sync = (s) => new Uint8Array(createHash('sha256').update(s).digest());
const fixed = (s, n) => new StreamRandom(s).take('x', n);

const annId = '0193a000-0000-7000-8000-00000000000a';
const bobId = '0193a000-0000-7000-8000-00000000000b';
const convId = '0193a0e0-0000-7000-8000-0000000000c0';
const DAY = 24 * 60 * 60 * 1000;

class World {
  static async make() {
    const w = new World();
    w.now = 1800000000000;
    w.lists = new Map();
    w.seq = 0;
    w.ann = await S.Device.create({ account: annId, server: 'chat.example.org', platform: 1, random: new StreamRandom('ann'), now: () => w.now, id: '0193aa00-0000-7000-8000-0000000000a1' });
    w.bob = await S.Device.create({ account: bobId, server: 'chat.example.org', platform: 2, random: new StreamRandom('bob'), now: () => w.now, id: '0193aa00-0000-7000-8000-0000000000b1' });
    for (const d of [w.ann, w.bob]) { const b = await d.createBackup(); await d.adopt(b); w.lists.set(d.account, [b.list]); }
    await w.ann.learnLists(bobId, w.lists.get(bobId));
    await w.bob.learnLists(annId, w.lists.get(annId));
    const [stored, k] = await w.annChat.makeEpoch();
    await w.post(stored, k);
    return w;
  }
  get annChat() { return this.ann.chat(convId, bobId); }
  get bobChat() { return this.bob.chat(convId, annId); }
  async post(stored, keyEpoch) { this.seq++; for (const c of [this.annChat, this.bobChat]) await c.acceptEpoch(keyEpoch, this.seq, stored); }
  async send(c, t) {
    const o = await c.seal(S.messageContent(t));
    this.seq++;
    return { seq: this.seq, author: S.decodeHeader(o.ciphertext).accountId, keyEpoch: o.keyEpoch, quiet: o.quiet, ciphertext: o.ciphertext };
  }
}

const tests = [];
const test = (name, fn) => tests.push([name, fn]);
function expect(ok, why) { if (!ok) throw new Error(why); }
async function refusedAs(fn, type, why = '') {
  let err = null;
  try { await fn(); } catch (e) { err = e; }
  if (!err) throw new Error('accepted what must be refused ' + why);
  if (type && !(err instanceof type)) throw new Error(`refused with ${err.constructor.name} (${err.message}), want ${type.name} ${why}`);
}

// --- primitives and constructions ---

test('an all-zero X25519 result is refused', async () => {
  await refusedAs(() => S.x25519(fixed('a', 32), new Uint8Array(32)), S.ZeroSharedError);
});

test('a seal with any byte changed, another label, context or key, or a zero one-time key does not open', async () => {
  const priv = fixed('recipient', 32);
  const sealed = await S.seal(await S.x25519Public(priv), 'churr seal test v1', utf8('context'), utf8('thirty-two bytes of plaintext!!!'), fixed('one-time', 32));
  expect(sealed.length === 80, 'sealing 32 bytes gives 80');
  expect(dec(await S.openSealed(priv, 'churr seal test v1', utf8('context'), sealed)) === 'thirty-two bytes of plaintext!!!', 'opens');
  await refusedAs(() => S.openSealed(priv, 'churr seal other v1', utf8('context'), sealed), S.OpenError, 'another label');
  await refusedAs(() => S.openSealed(priv, 'churr seal test v1', utf8('contexu'), sealed), S.OpenError, 'another context');
  await refusedAs(() => S.openSealed(fixed('someone else', 32), 'churr seal test v1', utf8('context'), sealed), S.OpenError, 'another key');
  await refusedAs(() => S.openSealed(priv, 'churr seal test v1', utf8('context'), S.concat(new Uint8Array(32), sealed.subarray(32))), S.OpenError, 'a zero one-time key');
  for (let i = 0; i < sealed.length; i++) {
    const changed = new Uint8Array(sealed); changed[i] ^= 1;
    await refusedAs(() => S.openSealed(priv, 'churr seal test v1', utf8('context'), changed), S.OpenError, 'byte ' + i);
  }
});

test('a lock with any byte changed or another label does not open', async () => {
  const key = fixed('lock key', 32);
  const locked = await S.lock(key, 'churr lock test v1', utf8('ctx'), utf8('hello'), fixed('salt', 16));
  expect(dec(await S.unlockLocked(key, 'churr lock test v1', utf8('ctx'), locked)) === 'hello', 'opens');
  await refusedAs(() => S.unlockLocked(key, 'churr lock test v2', utf8('ctx'), locked), S.OpenError);
  for (let i = 0; i < locked.length; i++) {
    const changed = new Uint8Array(locked); changed[i] ^= 1;
    await refusedAs(() => S.unlockLocked(key, 'churr lock test v1', utf8('ctx'), changed), S.OpenError, 'byte ' + i);
  }
});

test('a signature under one label is refused under every other, and a wrong signature is refused', async () => {
  const key = await S.SigningKey.fromSeed(fixed('label test', 32));
  const record = utf8('a record');
  for (const made of S.signatureLabels) {
    const sig = await S.sign(key, made, record);
    for (const checked of S.signatureLabels) expect((await S.verify(key.public, checked, record, sig)) === (made === checked), `${made} checked under ${checked}`);
    const bad = new Uint8Array(sig); bad[10] ^= 1;
    expect(!(await S.verify(key.public, made, record, bad)), 'a changed signature');
    expect(!(await S.verify(key.public, made, utf8('a recore'), sig)), 'a changed record');
  }
});

test('a file cut short, reordered, without its last-chunk mark or changed is refused, whole and as a stream', async () => {
  const key = fixed('file key', 32);
  const pattern = (n) => Uint8Array.from({ length: n }, (_, i) => i % 251);
  const SC = S.SEALED_CHUNK_SIZE;
  const asStream = (b) => new ReadableStream({ start(c) { c.enqueue(b); c.close(); } });
  const both = async (blob, why) => {
    await refusedAs(() => S.decryptFile(key, blob), S.FileError, why);
    await refusedAs(() => S.collect(asStream(blob).pipeThrough(S.decryptStream(key)), true), S.FileError, why + ' (stream)');
  };
  for (const n of [0, 1, 65535, 65536, 65537, 200000]) {
    const plain = pattern(n);
    const blob = await S.encryptFile(key, plain);
    expect(S.equal(await S.decryptFile(key, blob), plain), 'round trip ' + n);
    if (blob.length > SC) {
      await both(blob.slice(0, SC), `cut short at a chunk boundary (${n})`);
      await both(blob.slice(0, blob.length - 1), `last byte missing (${n})`);
      if (blob.length >= 2 * SC) await both(S.concat(blob.subarray(SC, 2 * SC), blob.subarray(0, SC), blob.subarray(2 * SC)), `reordered (${n})`);
    }
    const changed = new Uint8Array(blob); changed[blob.length >> 1] ^= 1;
    await both(changed, `changed (${n})`);
  }
  await both(await S.encryptChunk(key, 0, false, utf8('x')), 'no last-chunk mark');
  await both(new Uint8Array(0), 'empty');
  await both((await S.encryptFile(key, pattern(65536 + 3))).slice(0, SC + 10), 'a tail shorter than a tag');
});

test('a phrase with a bad checksum, too few words or an unknown word is refused; no two words share four letters', async () => {
  for (const bad of ['abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon', 'abandon abandon abandon',
    'abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon churr']) {
    await refusedAs(() => S.readPhrase(bad), S.PhraseError, bad);
  }
  const { WORDS } = await import('../internal/web/static/shared/sealed/bip39words.js');
  const seen = new Set();
  for (const w of WORDS) { const k = w.length > 4 ? w.slice(0, 4) : w; expect(!seen.has(k), w); seen.add(k); }
});

test('approval codes: a different request gives a different code; look-alike letters are read as digits', async () => {
  const code = await S.approvalCode(utf8('request'));
  expect(code.length === 23, 'length');
  expect(S.normalizeApprovalCode(code.toLowerCase().replace(/-/g, ' ')) === code, 'typed in lower case with spaces');
  expect(S.normalizeApprovalCode('OIL00-00000-00000-00000') === '01100-00000-00000-00000', 'look-alikes');
  expect((await S.approvalCode(utf8('requesu'))) !== code, 'another request');
});

test('records: bytes left over, cut short, out of order, too long, a choice out of range', async () => {
  const b = S.encodeList({ accountId: annId, server: 's', version: 1, keyVersion: 1, accountKey: new Uint8Array(32), devices: [], at: 0 });
  await refusedAs(() => S.decodeList(S.concat(b, new Uint8Array(1))), S.RecordError);
  await refusedAs(() => S.decodeList(b.slice(0, b.length - 1)), S.RecordError);
  await refusedAs(() => S.decodeList(new Uint8Array(S.MAX_RECORD + 1)), S.RecordError);
  await refusedAs(() => S.decodeBackupRecord(S.concat(new Uint8Array([2]), new Uint8Array(64))), S.RecordError);
});

test('64-bit values: the edges of u64 round-trip, a number beyond 2^53 comes back as a BigInt, and out-of-range values are refused', async () => {
  for (const v of [0n, 1n, 2n ** 32n - 1n, 2n ** 32n, 2n ** 53n - 1n, 2n ** 53n, 2n ** 53n + 1n, 2n ** 63n, 2n ** 64n - 1n]) {
    const back = S.readU64(S.u64(v));
    expect(S.big(back) === v, 'round trip ' + v);
    expect(typeof back === (v <= 2n ** 53n - 1n ? 'number' : 'bigint'), 'kind of ' + v);
  }
  expect(S.toHex(S.u64(Number.MAX_SAFE_INTEGER)) === '001fffffffffffff', 'a number');
  await refusedAs(() => S.u64(2n ** 64n), RangeError);
  await refusedAs(() => S.u64(-1), RangeError);
  await refusedAs(() => S.u32(2 ** 32), RangeError);
  // A header with the largest counter and key epoch reads back exactly.
  const h = { conversationId: convId, keyEpoch: 2n ** 64n - 1n, accountId: annId, deviceId: annId, counter: 2n ** 64n - 2n, reference: convId };
  const got = S.decodeHeader(S.encodeHeader(h));
  expect(got.keyEpoch === 2n ** 64n - 1n && got.counter === 2n ** 64n - 2n, 'header u64s');
  expect(S.callKeyIndex(2n ** 64n - 1n) === 15 && S.callKeyIndex(17) === 1, 'key index');
});

// --- the state machine ---

test('a message is drawn; a replayed counter at a later sequence number is dropped', async () => {
  const w = await World.make();
  const row = await w.send(w.annChat, 'hello');
  expect(!(await w.bobChat.receive(row)).dropped, 'drawn');
  expect(w.bobChat.view().length === 1 && w.bobChat.view()[0].text === 'hello', 'shown');
  w.seq++;
  expect((await w.bobChat.receive({ ...row, seq: w.seq })).dropped, 'replay dropped');
  expect(w.bobChat.view().length === 1, 'drawn once');
  expect((await w.bobChat.receive(row)).already, 'the same row changes nothing');
});

test('a replay seen first is replaced by the original, and the message is drawn once', async () => {
  const w = await World.make();
  const row = await w.send(w.annChat, 'hello');
  await w.bobChat.receive({ ...row, seq: row.seq + 5 });
  expect(!(await w.bobChat.receive(row)).dropped, 'the original is not dropped');
  const view = w.bobChat.view();
  expect(view.length === 1 && view[0].seq === row.seq, 'drawn once, at its own place');
});

test('tampered ciphertext, a wrong signature and a changed quiet flag are refused and counted', async () => {
  const w = await World.make();
  const row = await w.send(w.annChat, 'hello');
  const ct = new Uint8Array(row.ciphertext); ct[S.HEADER_SIZE + S.SALT_SIZE + 1] ^= 1;
  await refusedAs(() => w.bobChat.receive({ ...row, ciphertext: ct }), S.MessageError);
  const sig = new Uint8Array(row.ciphertext); sig[sig.length - 1] ^= 1;
  await refusedAs(() => w.bobChat.receive({ ...row, ciphertext: sig }), S.MessageError);
  await refusedAs(() => w.bobChat.receive({ ...row, quiet: true }), S.MessageError);
  const flag = new Uint8Array(row.ciphertext); flag[1] = 1;
  await refusedAs(() => w.bobChat.receive({ ...row, ciphertext: flag, quiet: true }), S.MessageError);
  expect(w.bobChat.unchecked === 4, 'counted');
  expect(w.bobChat.view().length === 0, 'nothing drawn');
});

test('a message in a stale epoch, after a newer epoch began, is refused; a sender must make a new epoch for age', async () => {
  const w = await World.make();
  const old = await w.send(w.annChat, 'in epoch 1');
  const [stored, k] = await w.bobChat.makeEpoch();
  await w.post(stored, k);
  await refusedAs(() => w.bobChat.receive({ ...old, seq: w.seq + 1, keyEpoch: 1, quiet: false }), S.MessageError);
  w.now += 8 * DAY;
  expect(w.annChat.needsEpoch() === 'age', 'needs an epoch for age');
  await refusedAs(() => w.annChat.seal(S.messageContent('x')), S.NeedsEpochError);
});

test('an epoch record with a wrong chain hash, a wrong device set or a wrong signature is refused and kept in the chain', async () => {
  const w = await World.make();
  const resign = async (r, k) => { const raw = S.encodeEpoch(r); return S.concat(raw, await S.sign(k, S.labelEpoch, raw)); };
  const ruleOf = async (stored) => {
    const w2 = await World.make();
    try { await w2.bobChat.acceptEpoch(2, 5, stored); } catch (e) {
      if (!(e instanceof S.EpochError)) throw e;
      expect(!w2.bobChat.current.accepted, 'kept as refused');
      expect(w2.bobChat.needsEpoch() === 'refused', 'asks for a repair');
      expect(w2.bob.notices.at(-1).kind === S.NOTICE_OUT_OF_STEP, 'out of step notice');
      return e.rule;
    }
    return 0;
  };
  const [good] = await w.annChat.makeEpoch();
  const parse = () => S.decodeEpoch(S.splitStoredEpoch(good)[0]);
  const next = parse(); next.prev = new Uint8Array(32);
  expect((await ruleOf(await resign(next, w.ann.sign))) === 2, 'wrong chain hash');
  const dropped = parse(); dropped.recipients.pop();
  expect((await ruleOf(await resign(dropped, w.ann.sign))) === 5, 'wrong device set');
  const other = await S.SigningKey.fromSeed(fixed('intruder', 32));
  expect((await ruleOf(await resign(parse(), other))) === 6, 'wrong signature');
});

test('a second record for a key epoch and a record after a gap are refused', async () => {
  const w = await World.make();
  const [a] = await w.annChat.makeEpoch();
  const [b] = await w.bobChat.makeEpoch();
  await w.post(a, 2);
  await refusedAs(() => w.bobChat.acceptEpoch(2, 9, b), S.SecondRecordError);
  await refusedAs(() => w.bobChat.acceptEpoch(4, 10, b), S.ChainGapError);
});

test('a list that does not check out is refused, the pin kept and a notice shown', async () => {
  const w = await World.make();
  const k = await S.SigningKey.fromSeed(fixed('k', 32));
  const forged = await S.signList(S.encodeList({ accountId: bobId, server: 'chat.example.org', version: 2, prev: await S.listHash(w.lists.get(bobId)[0].list), keyVersion: 1, accountKey: k.public, devices: [], at: 0 }), k);
  await refusedAs(() => w.ann.learnLists(bobId, [forged]), S.ListError);
  expect(w.ann.pins.get(bobId).newest === 1, 'pin kept');
  expect(w.ann.notices.at(-1).kind === S.NOTICE_LIST_REFUSED, 'notice shown');
});

test('a reset after codes were compared removes the mark and holds sending until acknowledged', async () => {
  const w = await World.make();
  w.ann.markVerified(bobId);
  const bob2 = await S.Device.create({ account: bobId, server: 'chat.example.org', platform: 2, random: new StreamRandom('bob2'), now: () => w.now, id: '0193aa00-0000-7000-8000-0000000000b9' });
  await bob2.learnLists(bobId, w.lists.get(bobId));
  const reset = await bob2.resetBackup();
  await w.ann.learnLists(bobId, [reset.list]);
  expect(w.ann.notices.at(-1).kind === S.NOTICE_RESET, 'reset notice');
  expect(!w.ann.pins.get(bobId).verified, 'mark removed');
  await refusedAs(() => w.annChat.seal(S.messageContent('x')), S.HeldError);
  w.ann.acknowledge(bobId);
  await refusedAs(() => w.annChat.seal(S.messageContent('x')), S.NeedsEpochError);
});

test('unlocking refuses a copy for another way, an older account record, a copy not signed by the account key, and a wrong code', async () => {
  const w = await World.make();
  const newcomer = await S.Device.create({ account: annId, server: 'chat.example.org', platform: 6, random: new StreamRandom('new'), now: () => w.now, id: '0193aa00-0000-7000-8000-0000000000a7' });
  await newcomer.learnLists(annId, w.lists.get(annId));
  const [request, code] = await newcomer.newUnlockRequest();
  await refusedAs(() => w.ann.answerUnlockRequest(request, 'AAAAA-AAAAA-AAAAA-AAAAA'), S.CodeError);
  const answer = await w.ann.answerUnlockRequest(request, code.toLowerCase());
  const rec = w.ann.record;
  const sb = { keyVersion: rec.keyVersion, revision: rec.revision, accountRecord: await S.lockAccountRecord(w.ann.backupKey, rec, fixed('salt', 16)) };
  await refusedAs(() => newcomer.unlockWithCopy(newcomer.box, S.WAY_PHRASE, S.ZERO_ID, answer, sb), S.UnlockError, 'opened as a phrase copy');
  const forged = await S.makeLockedCopy(newcomer.boxPublic, annId, rec.keyVersion, S.WAY_DEVICE, newcomer.id, w.ann.backupKey, await S.SigningKey.fromSeed(fixed('server', 32)), fixed('ot', 32));
  await refusedAs(() => newcomer.unlockWithDeviceCopy(forged, sb), S.UnlockError, 'not signed by the account key');
  expect(!newcomer.unlocked, 'still locked');
  await newcomer.unlockWithDeviceCopy(answer, sb);
  expect(newcomer.unlocked, 'unlocked');
  newcomer.lockDevice();
  newcomer.record = { ...rec, revision: 5 };
  await refusedAs(() => newcomer.unlockWithDeviceCopy(answer, sb), S.UnlockError, 'an older account record');
});

test('a backup record for another epoch, or with a made-up secret, is refused', async () => {
  const w = await World.make();
  const recs = await w.annChat.takeNewSecrets();
  expect(recs.length === 1, 'one record');
  const r = recs[0];
  await refusedAs(() => w.annChat.readBackupRecord({ conversationId: convId, keyEpoch: 2, keyVersion: r.keyVersion, locked: r.locked }), S.BackupRecordError);
  const madeUp = await S.lockBackupRecord(w.ann.backupKey, annId, w.ann.keyVersion, { conversationId: convId, keyEpoch: 1, secret: fixed('made up', 32), at: 0 }, fixed('s', 16));
  await refusedAs(() => w.annChat.readBackupRecord({ conversationId: convId, keyEpoch: 1, keyVersion: r.keyVersion, locked: madeUp }), S.BackupRecordError);
  await w.annChat.readBackupRecord(r);
});

test('a locked device cannot make epochs, send copies or hand over', async () => {
  const w = await World.make();
  w.ann.lockDevice();
  await refusedAs(() => w.annChat.makeEpoch(), S.LockedError);
  await refusedAs(() => w.ann.deviceCopyFor(w.bob.id, w.bob.boxPublic), S.LockedError);
  await refusedAs(() => w.ann.handoverFor(w.bob.boxPublic, 'c'), S.LockedError);
});

test('a device saved and loaded keeps its counter, so the next message does not reuse it; the browser store keeps no message', async () => {
  const w = await World.make();
  const store = new S.MemoryStore({ keepsMessages: false });
  S.attachStore(w.ann, store);
  const row = await w.send(w.annChat, 'one');
  await w.bobChat.receive(row);
  S.attachStore(w.bob, new S.MemoryStore({ keepsMessages: false }));
  await S.saveDevice(w.bob);
  const written = JSON.stringify([...w.bob.store.values.values()].map((v) => v.data));
  expect(!written.includes('one') || !/"text"/.test(written), 'no message text in what the browser writes');
  expect(!/"messages"/.test(written), 'no drawn messages in what the browser writes');
  const loaded = await S.loadDevice(store, { random: new StreamRandom('ann-after'), now: () => w.now });
  const o = await loaded.chat(convId, bobId).seal(S.messageContent('two'));
  expect(S.big(S.decodeHeader(o.ciphertext).counter) === S.big(S.decodeHeader(row.ciphertext).counter) + 1n, 'counter carried on');
});

test('the private keys a device holds cannot be read', async () => {
  const w = await World.make();
  expect(w.ann.sign.key.extractable === false && w.ann.box.key.extractable === false, 'device keys are non-extractable');
  await refusedAs(() => globalThis.crypto.subtle.exportKey('pkcs8', w.ann.box.key));
  const g = await S.BoxKey.generate();
  const s = await S.SigningKey.generate();
  expect(!g.key.extractable && !s.key.extractable && g.public.length === 32 && s.public.length === 32, 'generated keys');
  // A generated pair works for sealing and signing.
  const sealed = await S.seal(g.public, 'churr seal test v1', utf8('c'), utf8('x'), fixed('o', 32));
  expect(dec(await S.openSealed(g, 'churr seal test v1', utf8('c'), sealed)) === 'x', 'generated box key opens');
  expect(await S.verify(s.public, S.labelEpoch, utf8('r'), await S.sign(s, S.labelEpoch, utf8('r'))), 'generated signing key signs');
});

test('several tabs: the lock lets one at a time spend counters, so no counter is used twice', async () => {
  // Two "tabs" share one store; each loads the device afresh under the
  // lock before it seals, as the page does.
  const w = await World.make();
  const store = new S.MemoryStore({ keepsMessages: false });
  S.attachStore(w.ann, store);
  await S.saveDevice(w.ann);
  const locks = fakeLocks();
  const counters = [];
  const tab = async (name, n) => {
    for (let i = 0; i < n; i++) {
      await exclusive('churr-sealed-test', async () => {
        const d = await S.loadDevice(store, { random: new StreamRandom(name + i), now: () => w.now });
        const o = await d.chat(convId, bobId).seal(S.messageContent(name + i));
        counters.push(o.counter);
      }, locks);
    }
  };
  await Promise.all([tab('a', 10), tab('b', 10), tab('c', 10)]);
  const sorted = [...counters].sort((x, y) => x - y);
  expect(sorted.length === 30 && sorted.every((c, i) => c === i + 1), 'every counter used exactly once: ' + sorted.join(','));
  // Without the lock the same code reuses counters.
  const counters2 = [];
  const store2 = new S.MemoryStore({ keepsMessages: false });
  S.attachStore(w.ann, store2);
  await S.saveDevice(w.ann);
  const loose = async (name) => {
    const d = await S.loadDevice(store2, { random: new StreamRandom(name), now: () => w.now });
    counters2.push((await d.chat(convId, bobId).seal(S.messageContent(name))).counter);
  };
  await Promise.all([loose('x'), loose('y')]);
  expect(counters2[0] === counters2[1], 'without the lock two tabs spend the same counter (this is what the lock is for)');
});

// fakeLocks is the part of the Web Locks API exclusive() uses, for Node.
function fakeLocks() {
  const queues = new Map();
  return {
    request(name, opts, fn) {
      const prev = queues.get(name) || Promise.resolve();
      const run = prev.then(() => fn({ name }));
      queues.set(name, run.catch(() => {}));
      return run;
    },
  };
}

export async function runRefusals(log = console.log) {
  let passed = 0;
  const failures = [];
  for (const [name, fn] of tests) {
    try { await fn(); passed++; } catch (e) { failures.push(`${name}: ${e.message}`); }
  }
  log(`REFUSALS: ${passed}/${tests.length} passed`);
  return { passed, total: tests.length, failures };
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  const r = await runRefusals();
  for (const f of r.failures) console.log('FAIL ' + f);
  if (r.failures.length) process.exitCode = 1;
}
