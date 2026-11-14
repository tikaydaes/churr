// Runs static/unlock/sealed.js, the unlock page's cryptography, against
// the test vectors in testdata/sealed. Started by unlock_test.go with the
// path of sealed.js and of the vectors directory:
//
//   node unlock_vectors.mjs <sealed.js> <testdata/sealed>
//
// It prints "ok <n>" with the number of checks made, or throws.

import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
import { join } from 'node:path';

const s = await import(pathToFileURL(process.argv[2]).href);
const dir = process.argv[3];
const load = (name) => JSON.parse(readFileSync(join(dir, name), 'utf8'));
const hex = s.fromHex;
let checks = 0;

function same(got, want, what) {
  checks++;
  if (s.toHex(got) !== want) throw new Error(`${what}: got ${s.toHex(got)}, want ${want}`);
}

async function refused(fn, what) {
  checks++;
  let opened = false;
  try { await fn(); opened = true; } catch { /* refused, as it must be */ }
  if (opened) throw new Error(`${what}: accepted, and it must be refused`);
}

// The primitives the page uses.
const prim = load('primitives.json');
for (const v of prim.x25519) {
  if (v.refused) await refused(() => s.x25519(hex(v.private), hex(v.public)), 'an all-zero X25519 result');
  else same(await s.x25519(hex(v.private), hex(v.public)), v.shared, 'x25519');
}
for (const v of prim.x25519_public) same(await s.x25519Public(hex(v.private)), v.public, 'x25519 public');
for (const v of prim.hkdf_sha256) same(await s.hkdf(hex(v.ikm), hex(v.salt), hex(v.info), v.length), v.okm, 'hkdf');
for (const v of prim.sha256) same(await s.sha256(hex(v.message)), v.digest, 'sha256');

// Seal, both ways, and its refusals.
const seal = load('seal.json');
for (const v of seal.vectors) {
  same(await s.seal(hex(v.recipient_public), v.label, hex(v.context), hex(v.plaintext), hex(v.one_time_private)), v.sealed, 'seal');
  same(await s.open(hex(v.recipient_private), v.label, hex(v.context), hex(v.sealed)), v.plaintext, 'open');
}
for (const v of seal.refusals) await refused(() => s.open(hex(v.recipient_private), v.label, hex(v.context), hex(v.sealed)), 'seal: ' + v.why);

// Lock: the page only ever unlocks.
const lock = load('lock.json');
for (const v of lock.vectors) same(await s.unlock(hex(v.key), v.label, hex(v.context), hex(v.locked)), v.plaintext, 'unlock');
for (const v of lock.refusals) await refused(() => s.unlock(hex(v.key), v.label, hex(v.context), hex(v.locked)), 'lock: ' + v.why);

// Signatures and their labels.
const sigs = load('signatures.json');
for (const v of sigs.signatures) {
  checks++;
  if (!(await s.verify(hex(v.public), v.label, hex(v.record), hex(v.signature)))) throw new Error('a signature does not verify under ' + v.label);
  for (const other of v.refused_under) {
    checks++;
    if (await s.verify(hex(v.public), other, hex(v.record), hex(v.signature))) throw new Error(`a signature under ${v.label} verified under ${other}`);
  }
  same(await s.sign(hex(sigs.seed), v.label, hex(v.record)), v.signature, 'sign');
}

// The unlock key from a PRF output.
const keys = load('unlock_keys.json');
same(await s.prfInput(), keys.prf_input.bytes, 'the PRF input');
same(await s.unlockKeyFromPRF(hex(keys.passkey.prf_output), keys.account_id, hex(keys.passkey.credential_id)), keys.passkey.unlock_private, 'the unlock private key');
same(await s.unlockPublicFromPRF(hex(keys.passkey.prf_output), keys.account_id, hex(keys.passkey.credential_id)), keys.passkey.unlock_public, 'the unlock public key');

// Locked copies: open, verify, make.
const copies = load('locked_copy.json');
const zeroID = '00000000-0000-0000-0000-000000000000';
for (const c of copies.copies) {
  const id = c.id === zeroID ? null : c.id;
  same(await s.openLockedCopy(hex(c.unlock_private), copies.account_id, copies.key_version, c.kind, id, hex(c.stored)), copies.backup_key, 'a locked copy: ' + c.way);
  checks++;
  if (!(await s.verifyLockedCopy(hex(copies.account_key), copies.account_id, copies.key_version, c.kind, id, hex(c.stored)))) throw new Error('a locked copy does not verify: ' + c.way);
  same(await s.makeLockedCopy(hex(c.unlock_public), copies.account_id, copies.key_version, c.kind, id, hex(copies.backup_key), hex(copies.account_key_seed), hex(c.one_time_private)),
    c.stored, 'making a locked copy: ' + c.way);
}
for (const v of copies.refusals) {
  await refused(() => s.openLockedCopy(hex(v.unlock_private), copies.account_id, v.key_version, v.kind, v.id, hex(v.stored)), 'locked copy: ' + v.why);
}
const forged = copies.opens_but_is_not_signed_by_the_account_key;
await s.openLockedCopy(hex(forged.unlock_private), copies.account_id, forged.key_version, forged.kind, null, hex(forged.stored));
checks++;
if (await s.verifyLockedCopy(hex(forged.account_key), copies.account_id, forged.key_version, forged.kind, null, hex(forged.stored))) {
  throw new Error('a copy the account key did not sign verified');
}

// The account record.
const rec = load('account_record.json');
const opened = await s.openAccountRecord(hex(rec.backup_key), rec.fields.account_id, rec.fields.key_version, rec.fields.revision, hex(rec.stored));
same(opened.accountKey, rec.fields.account_key, 'the account key');
same(opened.accountSeed, rec.fields.account_key_private, 'the account key seed');
for (const v of rec.refusals) {
  await refused(() => s.openAccountRecord(hex(v.backup_key), v.account_id, v.key_version, v.revision, hex(v.stored)), 'account record: ' + v.why);
}

// The device list: version 2 of the chain holds the account key of the
// account record above.
const lists = load('device_list.json');
const two = lists.chain[1];
const list = await s.readDeviceList(hex(two.list), hex(two.sig));
same(list.accountKey, two.fields.account_key, 'the list account key');
checks++;
if (list.keyVersion !== two.fields.key_version || list.version !== two.fields.version || list.devices.length !== two.fields.devices.length) {
  throw new Error('the device list was misread');
}
await refused(() => s.readDeviceList(hex(two.list), hex(lists.chain[0].sig)), 'a list with another list\'s signature');

// The whole of what the page does: a passkey touch to a device copy.
const deviceCopy = copies.copies.find((c) => c.way === 'device');
const passkeyCopy = copies.copies.find((c) => c.way === 'passkey');
const device = list.devices.find((d) => d.id === deviceCopy.id.replace(/-/g, ''));
const input = {
  accountID: copies.account_id, prfOutput: hex(keys.passkey.prf_output), credentialID: hex(keys.passkey.credential_id),
  passkeyID: passkeyCopy.id, copy: hex(passkeyCopy.stored), keyVersion: rec.fields.key_version, revision: rec.fields.revision,
  accountRecord: hex(rec.stored), list: hex(two.list), listSig: hex(two.sig), deviceID: deviceCopy.id, deviceBoxKey: device.boxKey,
};
same(await s.unlockForDevice(input, hex(deviceCopy.one_time_private)), deviceCopy.stored, 'the copy the page makes for the app');
// With fresh randomness it still opens on the device.
const fresh = await s.unlockForDevice(input);
same(await s.openLockedCopy(hex(deviceCopy.unlock_private), copies.account_id, copies.key_version, s.WAY_DEVICE, deviceCopy.id, fresh), copies.backup_key, 'the fresh copy');
// Another passkey's PRF output opens nothing; a list under another account
// key is refused.
await refused(() => s.unlockForDevice({ ...input, prfOutput: new Uint8Array(32) }), 'another PRF output');
const three = lists.chain[2];
await refused(() => s.unlockForDevice({ ...input, list: hex(three.list), listSig: hex(three.sig) }), 'a device list with another account key');
await refused(() => s.unlockForDevice({ ...input, copy: hex(forged.stored), passkeyID: null }), 'a copy the server made itself');

// The hand-over to an app at sign-in is a seal.
const ho = load('handover.json');
same(await s.seal(hex(ho.app_box_key), 'churr seal handover v1', hex(ho.sign_in.context), hex(ho.backup_key), hex(ho.sign_in.one_time_private)), ho.sign_in.handover, 'the hand-over');

// Base64url, as the API carries bytes.
checks++;
if (s.toB64(hex('fbff00')) !== '-_8A' || s.toHex(s.fromB64('-_8A')) !== 'fbff00') throw new Error('base64url');

console.log('ok ' + checks);
