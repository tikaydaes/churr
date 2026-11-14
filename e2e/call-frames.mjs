// The web chat's call frame encryption (internal/web/static/chat/call/
// frames-worker.js) against testdata/call-frames/vectors.json, the file
// the Go reference writes and the app is checked against too
// (docs/CALL-FRAMES.md). Node stands in for the browser: both have the
// same Web Crypto. The worker is a classic script, so its source is read
// and run here with its functions handed back; the part of it that is a
// worker does not start outside one.
//
// Also here: the keys from an epoch secret through the web chat's sealed
// library to the frame, and the key rings across an epoch change.
//
//   node e2e/call-frames.mjs     prints "CALL-FRAMES: n/m passed"

import { readFileSync } from 'node:fs';

const source = readFileSync(new URL('../internal/web/static/chat/call/frames-worker.js', import.meta.url), 'utf8');
const W = new Function(source + '\nreturn { frameKey, headerLength, sealFrame, openFrame, frameKeyIndex, KeyRings, KEEP_OLD_KEYS, KEY_RING };')();
const S = await import('../internal/web/static/shared/sealed/index.js');
const v = JSON.parse(readFileSync(new URL('../testdata/call-frames/vectors.json', import.meta.url), 'utf8'));

const hx = (s) => Uint8Array.from(s.match(/../g) || [], (b) => parseInt(b, 16));
const hex = (b) => [...b].map((x) => x.toString(16).padStart(2, '0')).join('');
let passed = 0;
const failures = [];
async function check(name, fn) {
  try { await fn(); passed++; } catch (e) { failures.push(`${name}: ${e.message}`); }
}
function same(got, want, what) {
  const g = got == null ? String(got) : hex(got);
  if (g !== want) throw new Error(`${what}: got ${g}, want ${want}`);
}
// The key is not extractable in the worker, as it should be; the AES key
// in the file is checked by sealing with one made from it.
const rawAES = (h) => crypto.subtle.importKey('raw', hx(h), 'AES-GCM', false, ['encrypt', 'decrypt']);

for (const e of v.epochs) {
  await check(`epoch ${e.key_epoch}: from the epoch secret to every device's call sender key`, async () => {
    const got = await S.deriveCallKeys(hx(e.epoch_secret), v.conversation_id, e.key_epoch, v.call_id, e.devices.map((d) => d.device_id));
    if (got.keyIndex !== e.key_index) throw new Error(`key index ${got.keyIndex}, want ${e.key_index}`);
    for (const d of e.devices) same(got.keys.get(d.device_id), d.call_sender_key, d.device_id);
  });
}

for (const c of v.frames) {
  const plain = hx(c.plain), frame = hx(c.frame);
  await check(`${c.name}: header length`, async () => {
    if (W.headerLength(c.kind, plain) !== c.header_length) throw new Error(`of the frame: ${W.headerLength(c.kind, plain)}, want ${c.header_length}`);
    if (W.headerLength(c.kind, frame) !== c.header_length) throw new Error(`of the encrypted frame: ${W.headerLength(c.kind, frame)}`);
    if (W.frameKeyIndex(c.kind, frame) !== c.key_index) throw new Error(`key index read as ${W.frameKeyIndex(c.kind, frame)}`);
  });
  await check(`${c.name}: seal`, async () => {
    same(await W.sealFrame(await W.frameKey(hx(c.call_sender_key)), c.key_index, hx(c.iv), c.kind, plain), c.frame, 'frame');
  });
  await check(`${c.name}: the derived key is the file's AES key`, async () => {
    same(await W.sealFrame(await rawAES(c.aes_key), c.key_index, hx(c.iv), c.kind, plain), c.frame, 'frame under the AES key');
  });
  await check(`${c.name}: open`, async () => {
    const key = await W.frameKey(hx(c.call_sender_key));
    same(await W.openFrame((i) => (i === c.key_index ? key : null), c.kind, frame), c.plain, 'plain');
  });
}

for (const r of v.refused) {
  await check(`refused: ${r.name}`, async () => {
    const key = await W.frameKey(hx(r.call_sender_key));
    const got = await W.openFrame((i) => (i === r.key_index_held ? key : null), r.kind, hx(r.frame));
    if (got !== null) throw new Error('opened to ' + hex(got));
  });
}

// An epoch change in the middle of a call, with the file's two epochs: the
// sender switches at once, the receiver opens both for ten seconds and
// then only the new one, and a device the new epoch leaves out is gone.
await check('re-keying: the new epoch at once, the old one for ten seconds', async () => {
  const [e1, e2] = v.epochs;
  let t = 1000;
  const rings = new W.KeyRings(() => t);
  const keysOf = async (e) => Object.fromEntries(await Promise.all(e.devices.map(async (d) => [d.device_id, await W.frameKey(hx(d.call_sender_key))])));
  const [a1, a2, b1] = e1.devices.map((d) => d.device_id);
  const oldFrame = v.frames.find((f) => f.key_epoch === e1.key_epoch && f.device_id === a1);
  const newFrame = v.frames.find((f) => f.key_epoch === e2.key_epoch && f.device_id === a1);
  const leftOut = v.frames.find((f) => f.key_epoch === e1.key_epoch && f.device_id === a2);
  const open = (f, device = f.device_id) => W.openFrame((i) => rings.get(device, i), f.kind, hx(f.frame));

  if (await open(oldFrame) !== null) throw new Error('a frame opened before any key was set');
  rings.set(e1.key_index, b1, await keysOf(e1));
  if (rings.send.index !== e1.key_index) throw new Error('send index ' + rings.send.index);
  same(await open(oldFrame), oldFrame.plain, 'epoch 1 frame under epoch 1');
  if (await open(newFrame) !== null) throw new Error('the next epoch\'s frame opened before its keys came');
  if (await open(oldFrame, a2) !== null) throw new Error('a frame opened with the key of another device of the same account');

  rings.set(e2.key_index, b1, await keysOf(e2));
  if (rings.send.index !== e2.key_index) throw new Error('the sender did not switch index at once');
  same(await W.sealFrame(rings.send.key, rings.send.index, hx('0badc0de00000000000000ff'), 'audio', hx('7801')),
    hex(await W.sealFrame(await rawAES(e2.devices.find((d) => d.device_id === b1).aes_key), e2.key_index, hx('0badc0de00000000000000ff'), 'audio', hx('7801'))), 'sending under the new key');
  same(await open(newFrame), newFrame.plain, 'epoch 2 frame');
  t += W.KEEP_OLD_KEYS - 1;
  same(await open(oldFrame), oldFrame.plain, 'epoch 1 frame within ten seconds');
  same(await open(leftOut), leftOut.plain, 'the removed device within ten seconds');
  t += 2;
  if (await open(oldFrame) !== null) throw new Error('the old epoch\'s key was kept past ten seconds');
  if (await open(leftOut) !== null) throw new Error('a device the epoch left out still decrypts after ten seconds');
  same(await open(newFrame), newFrame.plain, 'epoch 2 frame afterwards');

  // A device the epoch leaves out sends nothing.
  rings.set(e2.key_index, a2, await keysOf(e2));
  if (rings.send !== null) throw new Error('a device outside the epoch still has a key to send under');
});

await check('re-keying: sixteen epochs on, the index comes round and the new key replaces the old at once', async () => {
  let t = 0;
  const rings = new W.KeyRings(() => t);
  const k1 = await W.frameKey(new Uint8Array(32).fill(1)), k2 = await W.frameKey(new Uint8Array(32).fill(2));
  rings.set(3, 'd', { d: k1 });
  rings.set(3 + W.KEY_RING, 'd', { d: k2 });
  if (rings.get('d', 3) !== k2) throw new Error('the old key is still at the index');
  t += W.KEEP_OLD_KEYS + 1;
  if (rings.get('d', 3) !== k2) throw new Error('the new key was swept away with the old');
});

for (const f of failures) console.log('FAIL call-frames: ' + f);
console.log(`CALL-FRAMES: ${passed}/${passed + failures.length} passed`);
if (failures.length) process.exitCode = 1;
