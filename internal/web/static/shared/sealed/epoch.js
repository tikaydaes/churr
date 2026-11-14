// Key epochs (docs/SEALED.md section 5): the record that announces one,
// what is derived from its secret, and the checks a device makes before
// it accepts one. Also the call keys of section 10, which come from the
// epoch secret.

import { big, compareIds, concat, equal, idBytes, isZero, u64, utf8 } from './bytes.js';
import { decodeList, listHash } from './devicelist.js';
import { MAX_RECORD, Reader, Writer } from './enc.js';
import { EpochError, OpenError, RecordError } from './errors.js';
import { hkdf } from './prim.js';
import { openSealed, seal } from './seal.js';
import { hashOf, labelCall, labelCallSender, labelContent, labelEpoch, labelEpochCheck, labelEpochHash, labelHistory, labelled, labelSealEpoch, labelSender, sign, verify } from './sign.js';

export const SIGNATURE_SIZE = 64;
const ACCOUNT_REF_SIZE = 16 + 8 + 32;
const RECIPIENT_SIZE = 16 + 16 + 80;

// encodeEpoch writes the record of section 5.1, without its signature.
// e: { format, conversationId, keyEpoch, prev, makerAccountId,
// makerDeviceId, accounts: [{ accountId, listVersion, listHash }], check,
// recipients: [{ accountId, deviceId, sealed }], at }.
export function encodeEpoch(e) {
  if (e.accounts.length !== 2) throw RecordError.count();
  const w = new Writer().u8(e.format ?? 1).uuid(e.conversationId).u64(e.keyEpoch).raw(e.prev || new Uint8Array(32))
    .uuid(e.makerAccountId).uuid(e.makerDeviceId).u32(e.accounts.length);
  e.accounts.forEach((a, i) => {
    if (i > 0 && compareIds(e.accounts[i - 1].accountId, a.accountId) >= 0) throw RecordError.order();
    w.uuid(a.accountId).u64(a.listVersion).raw(a.listHash || new Uint8Array(32));
  });
  w.raw(e.check || new Uint8Array(32)).u32(e.recipients.length);
  e.recipients.forEach((r, i) => {
    if (r.sealed.length !== 80) throw new Error('a sealed epoch secret is 80 bytes');
    if (i > 0 && compareIds(e.recipients[i - 1].deviceId, r.deviceId) >= 0) throw RecordError.order();
    w.uuid(r.accountId).uuid(r.deviceId).raw(r.sealed);
  });
  w.time(e.at);
  return w.bytes();
}

export function decodeEpoch(b) {
  const r = new Reader(b);
  const e = { format: r.choice([1]), conversationId: r.uuid(), keyEpoch: r.u64(), prev: r.raw(32), makerAccountId: r.uuid(), makerDeviceId: r.uuid(), accounts: [], recipients: [] };
  let n = r.count(ACCOUNT_REF_SIZE);
  if (n !== 2) r.fail(RecordError.count());
  for (let i = 0; i < n && !r.err; i++) {
    const a = { accountId: r.uuid(), listVersion: r.u64(), listHash: r.raw(32) };
    if (i > 0 && compareIds(e.accounts[i - 1].accountId, a.accountId) >= 0) r.fail(RecordError.order());
    e.accounts.push(a);
  }
  e.check = r.raw(32);
  n = r.count(RECIPIENT_SIZE);
  for (let i = 0; i < n && !r.err; i++) {
    const rc = { accountId: r.uuid(), deviceId: r.uuid(), sealed: r.raw(80) };
    if (i > 0 && compareIds(e.recipients[i - 1].deviceId, rc.deviceId) >= 0) r.fail(RecordError.order());
    e.recipients.push(rc);
  }
  e.at = r.u64();
  r.done();
  return e;
}

export const epochRecipient = (e, device) => e.recipients.find((r) => r.deviceId === device) || null;
export const epochHasAccount = (e, account) => e.recipients.some((r) => r.accountId === account);
export const epochRef = (e, account) => e.accounts.find((a) => a.accountId === account) || null;

// splitStoredEpoch splits the stored form into [record, signature].
export function splitStoredEpoch(stored) {
  if (stored.length > MAX_RECORD + SIGNATURE_SIZE) throw RecordError.tooLong();
  if (stored.length < SIGNATURE_SIZE) throw RecordError.short();
  const n = stored.length - SIGNATURE_SIZE;
  return [stored.subarray(0, n), stored.subarray(n)];
}

// The hash of an epoch's stored bytes, signature included.
export const epochHash = (stored) => hashOf(labelEpochHash, stored);

// conversation_id || key_epoch: the salt of every derivation of 5.2.
export const epochSalt = (conv, keyEpoch) => concat(idBytes(conv), u64(keyEpoch));

const derive = (key, conv, keyEpoch, label, id) => hkdf(key, epochSalt(conv, keyEpoch), id ? labelled(label, id) : utf8(label), 32);

export const historySecret = (epochSecret, conv, keyEpoch) => derive(epochSecret, conv, keyEpoch, labelHistory);
export const contentKey = (history, conv, keyEpoch) => derive(history, conv, keyEpoch, labelContent);
export const epochCheck = (history, conv, keyEpoch) => derive(history, conv, keyEpoch, labelEpochCheck);
export const senderKey = (content, conv, keyEpoch, device) => derive(content, conv, keyEpoch, labelSender, idBytes(device));
export const callSecret = (epochSecret, conv, keyEpoch, call) => derive(epochSecret, conv, keyEpoch, labelCall, idBytes(call));
export const callSenderKey = (secret, conv, keyEpoch, device) => derive(secret, conv, keyEpoch, labelCallSender, idBytes(device));

// The key index carried in each call frame: key_epoch mod 16.
export const callKeyIndex = (keyEpoch) => Number(big(keyEpoch) % 16n);

// conversation_id || key_epoch || device_id.
export const epochSealContext = (conv, keyEpoch, device) => concat(epochSalt(conv, keyEpoch), idBytes(device));

// makeEpoch builds and signs an epoch record (section 5.3) and returns its
// stored form. The device set is not chosen: it is every device in the two
// lists. lists: [{ accountId, list (record bytes) or null }]; oneTime(device)
// gives the one-time private key for that device's seal.
export async function makeEpoch({ conversationId, keyEpoch, prev, makerAccountId, makerDeviceId, makerSign, lists, secret, oneTime, at }) {
  if (secret.length !== 32) throw new Error('an epoch secret is 32 bytes');
  const accounts = [];
  const recipients = [];
  for (const input of lists) {
    let ref = { accountId: input.accountId, listVersion: 0, listHash: new Uint8Array(32) };
    if (input.list) {
      const l = decodeList(input.list);
      if (l.accountId !== input.accountId) throw new Error("a list is not that account's");
      ref = { accountId: input.accountId, listVersion: l.version, listHash: await listHash(input.list) };
      for (const d of l.devices) {
        const sealed = await seal(d.boxKey, labelSealEpoch, epochSealContext(conversationId, keyEpoch, d.deviceId), secret, oneTime(d.deviceId));
        recipients.push({ accountId: input.accountId, deviceId: d.deviceId, sealed });
      }
    }
    accounts.push(ref);
  }
  accounts.sort((a, b) => compareIds(a.accountId, b.accountId));
  // Array.prototype.sort is stable.
  recipients.sort((a, b) => compareIds(a.deviceId, b.deviceId));
  const check = await epochCheck(await historySecret(secret, conversationId, keyEpoch), conversationId, keyEpoch);
  const record = encodeEpoch({ conversationId, keyEpoch, prev, makerAccountId, makerDeviceId, accounts, check, recipients, at });
  return concat(record, await sign(makerSign, labelEpoch, record));
}

// checkEpochRecord applies the seven rules of section 5.4 and returns
// { record, devices (Map device → list entry), secret (or null) }.
//
// previous is the stored record before this one, accepted or not; null for
// the oldest record the device was given. listOf(account, version) gives
// the record of a list version the device has checked, or null. box may
// be null for a device that only checks the chain.
export async function checkEpochRecord({ stored, conversationId, accounts, previous, listOf, deviceId, box }) {
  // 1. It parses, with nothing left over, and is this chat's.
  let record, sig, e;
  try {
    [record, sig] = splitStoredEpoch(stored);
    e = decodeEpoch(record);
  } catch (err) {
    if (err instanceof RecordError) throw new EpochError(1, err.message);
    throw err;
  }
  if (e.conversationId !== conversationId) throw new EpochError(1, 'the record is for another chat');
  // 2. It is the next number and names the hash of the one before.
  let before = null;
  if (previous) {
    try { before = decodeEpoch(splitStoredEpoch(previous)[0]); } catch { before = null; }
    if (before && big(e.keyEpoch) !== big(before.keyEpoch) + 1n) throw new EpochError(2, "the key epoch is not one above the previous record's");
    if (!equal(e.prev, await epochHash(previous))) throw new EpochError(2, 'prev is not the hash of the previous record');
  } else if (big(e.keyEpoch) === 0n) {
    throw new EpochError(2, "a chat's first key epoch is 1");
  } else if (big(e.keyEpoch) === 1n && !isZero(e.prev)) {
    throw new EpochError(2, 'epoch 1 has a previous hash');
  }
  // 3. It names exactly the chat's two accounts.
  const want = [...accounts].sort(compareIds);
  if (e.accounts[0].accountId !== want[0] || e.accounts[1].accountId !== want[1]) throw new EpochError(3, "the record does not name the chat's two accounts");
  // 4. The lists it names are checked, with those hashes, never older.
  const devices = new Map();
  const owner = new Map();
  for (const a of e.accounts) {
    if (before) {
      const was = epochRef(before, a.accountId);
      if (was && big(a.listVersion) < big(was.listVersion)) throw new EpochError(4, 'a device list went backwards');
    }
    if (big(a.listVersion) === 0n) {
      if (!isZero(a.listHash)) throw new EpochError(4, 'an account with no list has a list hash');
      continue;
    }
    const raw = listOf(a.accountId, a.listVersion);
    if (!raw) throw new EpochError(4, 'the device has not checked a device list the record names');
    if (!equal(await listHash(raw), a.listHash)) throw new EpochError(4, 'a list hash is not the hash of that version');
    let l;
    try { l = decodeList(raw); } catch { throw new EpochError(4, 'a device list does not parse'); }
    for (const d of l.devices) { devices.set(d.deviceId, d); owner.set(d.deviceId, a.accountId); }
  }
  // 5. The recipients are exactly those lists' devices, each under its
  // right account.
  if (e.recipients.length !== devices.size) throw new EpochError(5, 'the recipients are not exactly the devices of the two lists');
  for (const r of e.recipients) {
    if (owner.get(r.deviceId) !== r.accountId) throw new EpochError(5, 'the recipients are not exactly the devices of the two lists');
  }
  // 6. The maker is a recipient, and signed.
  const maker = devices.get(e.makerDeviceId);
  if (!maker || owner.get(e.makerDeviceId) !== e.makerAccountId) throw new EpochError(6, 'the maker is not one of the recipients');
  if (!(await verify(maker.signKey, labelEpoch, record, sig))) throw new EpochError(6, "the maker's signature does not verify");
  // 7. If the device is a recipient, its sealed value opens to the secret
  // the record announces.
  let secret = null;
  const mine = epochRecipient(e, deviceId);
  if (mine && box) {
    try {
      secret = await openSealed(box, labelSealEpoch, epochSealContext(e.conversationId, e.keyEpoch, deviceId), mine.sealed);
    } catch (err) {
      if (err instanceof OpenError) throw new EpochError(7, 'the sealed epoch secret does not open');
      throw err;
    }
    if (secret.length !== 32) throw new EpochError(7, 'the sealed epoch secret does not open');
    const check = await epochCheck(await historySecret(secret, e.conversationId, e.keyEpoch), e.conversationId, e.keyEpoch);
    if (!equal(check, e.check)) throw new EpochError(7, 'the secret is not the one the record announces');
  }
  return { record: e, devices, secret };
}

// checkHistorySecret says whether a history secret from a backup or from
// the other person is the one an epoch record announced.
export async function checkHistorySecret(history, e) {
  return !!history && history.length === 32 && equal(await epochCheck(history, e.conversationId, e.keyEpoch), e.check);
}
