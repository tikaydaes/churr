// The part of the sealed-chat scheme the unlock page needs
// (docs/SEALED.md section 13.3), as a thin layer over the web chat's
// library in shared/sealed, which is checked against every test vector
// (e2e/sealed-vectors.mjs). The names and shapes here are the ones this
// page and internal/web's unlock test (testdata/unlock_vectors.mjs) use.
//
// This file touches no page and makes no request.

import * as S from '../shared/sealed/index.js';

export const { concat, equal, fromHex, toHex, toB64, sha256, hkdf, x25519, x25519Public, verify, prfInput, labelled } = S;
export const uuidBytes = S.idBytes;
export const WAY_PASSKEY = S.WAY_PASSKEY;
export const WAY_PHRASE = S.WAY_PHRASE;
export const WAY_DEVICE = S.WAY_DEVICE;

export function fromB64(s) {
  const b = S.fromB64(s);
  if (!b) throw new Error('That is not base64url.');
  return b;
}

const key = (seedOrKey) => (seedOrKey instanceof Uint8Array ? S.SigningKey.fromSeed(seedOrKey) : seedOrKey);
const way = (id) => id || S.ZERO_ID;

export async function sign(seed, label, record) { return S.sign(await key(seed), label, record); }

// seal encrypts to a recipient's X25519 public key; oneTime is for tests.
export function seal(recipient, label, context, plaintext, oneTime) {
  return S.seal(recipient, label, context, plaintext, oneTime || S.randomBytes(32));
}
export const open = (priv, label, context, sealed) => S.openSealed(priv, label, context, sealed);
export const unlock = (k, label, context, locked) => S.unlockLocked(k, label, context, locked);

export const unlockKeyFromPRF = (prfOutput, accountID, credentialID) => S.unlockKeyFromPrf(prfOutput, accountID, credentialID);
export async function unlockPublicFromPRF(prfOutput, accountID, credentialID) {
  return S.x25519Public(await S.unlockKeyFromPrf(prfOutput, accountID, credentialID));
}

export const openLockedCopy = (unlockPrivate, accountID, keyVersion, kind, id, stored) => S.openLockedCopy(unlockPrivate, accountID, keyVersion, kind, way(id), stored);
export const verifyLockedCopy = (accountKey, accountID, keyVersion, kind, id, stored) => S.verifyLockedCopy(accountKey, accountID, keyVersion, kind, way(id), stored);
export async function makeLockedCopy(unlockPublic, accountID, keyVersion, kind, id, backupKey, accountSeed, oneTime) {
  return S.makeLockedCopy(unlockPublic, accountID, keyVersion, kind, way(id), backupKey, await key(accountSeed), oneTime || S.randomBytes(32));
}

// openAccountRecord opens the account record and returns the account key
// pair in it.
export async function openAccountRecord(backupKey, accountID, keyVersion, revision, stored) {
  const a = await S.openAccountRecord(backupKey, accountID, keyVersion, revision, stored);
  return { accountKey: a.accountKey, accountSeed: a.accountKeyPrivate };
}

// readDeviceList checks that a device list was signed by its own account
// key and returns that key, the key version, and the devices in it.
export async function readDeviceList(list, sig) {
  const l = S.decodeList(list);
  if (!(await S.verify(l.accountKey, S.labelDeviceList, list, sig))) throw new Error('The server sent a device list that does not check out.');
  return { account: S.idBytes(l.accountId), version: l.version, keyVersion: l.keyVersion, accountKey: l.accountKey, devices: l.devices.map((d) => ({ id: d.deviceId.replace(/-/g, ''), boxKey: d.boxKey })) };
}

// unlockForDevice is the whole of what the unlock page does once a passkey
// has been touched (section 13.3, "Unlocking an app"):
//
//   1. derive the passkey's unlock key from its PRF output;
//   2. open the locked copy tagged with that passkey;
//   3. open the account record with the backup key it holds, and check the
//      copy's signature under the account key from that record;
//   4. check that this account key is the one in the account's device list;
//   5. seal the backup key to the app's device box key and sign the copy.
//
// It returns the 144 bytes to store as a locked copy of kind `device`. The
// PRF output, the unlock key and the backup key never leave this function.
//
// v: { accountID, prfOutput, credentialID, passkeyID, copy, keyVersion,
// revision, accountRecord, list, listSig, deviceID, deviceBoxKey }, with
// bytes as Uint8Array. oneTime is for tests only.
export async function unlockForDevice(v, oneTime) {
  const unlockPrivate = await S.unlockKeyFromPrf(v.prfOutput, v.accountID, v.credentialID);
  const backupKey = await openLockedCopy(unlockPrivate, v.accountID, v.keyVersion, WAY_PASSKEY, v.passkeyID, v.copy);
  const { accountKey, accountSeed } = await openAccountRecord(backupKey, v.accountID, v.keyVersion, v.revision, v.accountRecord);
  if (!(await verifyLockedCopy(accountKey, v.accountID, v.keyVersion, WAY_PASSKEY, v.passkeyID, v.copy))) {
    throw new Error('The locked copy for this passkey is not signed by the account key.');
  }
  const list = await readDeviceList(v.list, v.listSig);
  if (!equal(list.account, uuidBytes(v.accountID)) || !equal(list.accountKey, accountKey) || list.keyVersion !== v.keyVersion) {
    throw new Error("The backup key is not the one in this account's device list.");
  }
  return makeLockedCopy(v.deviceBoxKey, v.accountID, v.keyVersion, WAY_DEVICE, v.deviceID, backupKey, accountSeed, oneTime);
}
