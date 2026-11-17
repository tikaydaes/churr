// The key backup (docs/SEALED.md section 13): the account record, backup
// records, locked copies, the unlock keys they are sealed to, the recovery
// phrase, and the hand-overs to an app.

import { compare, concat, equal, idBytes, u32, u64, utf8, ZERO_ID } from './bytes.js';
import { Reader, Writer } from './enc.js';
import { OpenError, PhraseError, RecordError, SealedError } from './errors.js';
import { hkdf, sha256, SigningKey } from './prim.js';
import { lock, openSealed, seal, unlockLocked } from './seal.js';
import { labelled, labelLockAccountRecord, labelLockBackupRecord, labelLockedCopy, labelLockPairing, labelSealBackupKey, labelSealedUnlock, labelSealHandover, labelSealPairing, labelUnlockAccessKey, labelUnlockPasskey, labelUnlockPhrase, sign, verify } from './sign.js';
import { WORDS } from './bip39words.js';

export const WAY_PASSKEY = 1;
export const WAY_PHRASE = 2;
export const WAY_DEVICE = 3;
export const WAY_ACCESS_KEY = 4;
export const WAY_NAMES = { 1: 'passkey', 2: 'phrase', 3: 'device', 4: 'access_key' };

const waySortKey = (w) => concat(new Uint8Array([w.kind]), idBytes(w.id));
export const sortWays = (ways) => ways.sort((a, b) => compare(waySortKey(a), waySortKey(b)));

// --- the account record (13.1) ---

// encodeAccountRecord: a = { format, accountId, keyVersion, revision,
// accountKey, accountKeyPrivate, ways: [{ kind, id, unlockPublic }], at }.
export function encodeAccountRecord(a) {
  if (a.accountKey.length !== 32 || a.accountKeyPrivate.length !== 32) throw new Error('an account key and its seed are 32 bytes each');
  const w = new Writer().u8(a.format ?? 1).uuid(a.accountId).u32(a.keyVersion).u64(a.revision).raw(a.accountKey).raw(a.accountKeyPrivate).u32(a.ways.length);
  a.ways.forEach((way, i) => {
    if (way.unlockPublic.length !== 32) throw new Error('an unlock public key is 32 bytes');
    if (i > 0 && compare(waySortKey(a.ways[i - 1]), waySortKey(way)) >= 0) throw RecordError.order();
    w.u8(way.kind).uuid(way.id).raw(way.unlockPublic);
  });
  w.time(a.at);
  return w.bytes();
}

export function decodeAccountRecord(b) {
  const r = new Reader(b);
  const a = { format: r.choice([1]), accountId: r.uuid(), keyVersion: r.u32(), revision: r.u64(), accountKey: r.raw(32), accountKeyPrivate: r.raw(32), ways: [] };
  const n = r.count(1 + 16 + 32);
  for (let i = 0; i < n && !r.err; i++) {
    const w = { kind: r.choice([WAY_PASSKEY, WAY_PHRASE, WAY_ACCESS_KEY]), id: r.uuid(), unlockPublic: r.raw(32) };
    if (i > 0 && compare(waySortKey(a.ways[i - 1]), waySortKey(w)) >= 0) r.fail(RecordError.order());
    a.ways.push(w);
  }
  a.at = r.u64();
  r.done();
  return a;
}

// account_id || key_version || revision.
export const accountRecordContext = (account, keyVersion, revision) => concat(idBytes(account), u32(keyVersion), u64(revision));

export function lockAccountRecord(backupKey, a, salt) {
  return lock(backupKey, labelLockAccountRecord, accountRecordContext(a.accountId, a.keyVersion, a.revision), encodeAccountRecord(a), salt);
}

// openAccountRecord opens a stored account record filed under this key
// version and revision. The record must say the same, and its private
// half must be the seed of its public half.
export async function openAccountRecord(backupKey, account, keyVersion, revision, stored) {
  const content = await unlockLocked(backupKey, labelLockAccountRecord, accountRecordContext(account, keyVersion, revision), stored);
  const a = decodeAccountRecord(content);
  if (a.accountId !== account || a.keyVersion !== keyVersion || BigInt(a.revision) !== BigInt(revision)) {
    throw new SealedError('the account record is not the one it was filed as');
  }
  const k = await SigningKey.fromSeed(a.accountKeyPrivate);
  if (!equal(k.public, a.accountKey)) throw new SealedError("the account record's two key halves do not belong together");
  return a;
}

// --- backup records (13.2) ---

export function encodeBackupRecord(b) {
  if (b.secret.length !== 32) throw new Error('a history secret is 32 bytes');
  return new Writer().u8(b.format ?? 1).uuid(b.conversationId).u64(b.keyEpoch).raw(b.secret).time(b.at).bytes();
}

export function decodeBackupRecord(bytes) {
  const r = new Reader(bytes);
  const b = { format: r.choice([1]), conversationId: r.uuid(), keyEpoch: r.u64(), secret: r.raw(32), at: r.u64() };
  r.done();
  return b;
}

// account_id || conversation_id || key_epoch || key_version.
export const backupRecordContext = (account, conv, keyEpoch, keyVersion) => concat(idBytes(account), idBytes(conv), u64(keyEpoch), u32(keyVersion));

// The stored form in key_backups.record: 97 bytes.
export function lockBackupRecord(backupKey, account, keyVersion, b, salt) {
  return lock(backupKey, labelLockBackupRecord, backupRecordContext(account, b.conversationId, b.keyEpoch, keyVersion), encodeBackupRecord(b), salt);
}

// openBackupRecord opens a stored record for the row it was filed in. The
// caller then checks the secret against the epoch record's `check`.
export async function openBackupRecord(backupKey, account, conv, keyEpoch, keyVersion, stored) {
  const content = await unlockLocked(backupKey, labelLockBackupRecord, backupRecordContext(account, conv, keyEpoch, keyVersion), stored);
  const b = decodeBackupRecord(content);
  if (b.conversationId !== conv || BigInt(b.keyEpoch) !== BigInt(keyEpoch)) throw new SealedError('the backup record is not the one it was filed as');
  return b;
}

// --- locked copies (13.3) ---

export const LOCKED_COPY_SIZE = 80 + 64;

// kind (u8) || id (uuid: the passkey, 16 zero bytes, or the device).
export const wayTag = (kind, id) => concat(new Uint8Array([kind]), idBytes(id || ZERO_ID));

// account_id || key_version || way tag.
export const lockedCopyContext = (account, keyVersion, kind, id) => concat(idBytes(account), u32(keyVersion), wayTag(kind, id));

// makeLockedCopy seals the backup key to an unlock public key and signs
// the result with the account key: 144 bytes.
export async function makeLockedCopy(unlockPublic, account, keyVersion, kind, id, backupKey, accountKey, oneTime) {
  const ctx = lockedCopyContext(account, keyVersion, kind, id);
  const sealed = await seal(unlockPublic, labelSealBackupKey, ctx, backupKey, oneTime);
  return concat(sealed, await sign(accountKey, labelLockedCopy, concat(ctx, sealed)));
}

// openLockedCopy opens a copy with an unlock private key (bytes or a
// BoxKey). It is not trusted yet: the caller opens the account record with
// the key and calls verifyLockedCopy.
export async function openLockedCopy(unlockPrivate, account, keyVersion, kind, id, stored) {
  if (!stored || stored.length !== LOCKED_COPY_SIZE) throw new OpenError();
  const key = await openSealed(unlockPrivate, labelSealBackupKey, lockedCopyContext(account, keyVersion, kind, id), stored.subarray(0, 80));
  if (key.length !== 32) throw new OpenError();
  return key;
}

export async function verifyLockedCopy(accountKey, account, keyVersion, kind, id, stored) {
  if (!stored || stored.length !== LOCKED_COPY_SIZE) return false;
  return verify(accountKey, labelLockedCopy, concat(lockedCopyContext(account, keyVersion, kind, id), stored.subarray(0, 80)), stored.subarray(80));
}

// --- unlock keys (13.3) ---

// The first evaluation input of the WebAuthn PRF extension.
export const prfInput = () => sha256(utf8(labelSealedUnlock));

// A passkey's unlock private key from its PRF output.
export const unlockKeyFromPrf = (prfOutput, account, credentialId) => hkdf(prfOutput, new Uint8Array(32), labelled(labelUnlockPasskey, idBytes(account), credentialId), 32);

// The phrase's unlock private key from the 16 bytes the words stand for.
export const unlockKeyFromPhrase = (entropy, account) => hkdf(entropy, new Uint8Array(32), labelled(labelUnlockPhrase, idBytes(account)), 32);

// The access key's unlock private key from its wrap value (the derivation
// of /shared/accesskey.js accessKeyWrap, docs/AUTH.md section 5.6).
export const unlockKeyFromAccessKey = (wrap) => hkdf(wrap, new Uint8Array(32), labelled(labelUnlockAccessKey), 32);

// --- the recovery phrase ---

const INDEX = new Map(WORDS.map((w, i) => [w, i]));
const BY_FOUR = new Map(WORDS.filter((w) => w.length >= 4).map((w) => [w.slice(0, 4), INDEX.get(w)]));

function bits11(b, i) {
  let v = 0;
  for (let bit = i * 11; bit < (i + 1) * 11; bit++) v = (v << 1) | ((b[bit >> 3] >> (7 - (bit % 8))) & 1);
  return v;
}

// phraseOf writes 16 bytes as twelve words.
export async function phraseOf(entropy) {
  if (entropy.length !== 16) throw new Error('a phrase stands for 16 bytes');
  const bits = concat(entropy, new Uint8Array([(await sha256(entropy))[0]]));
  return Array.from({ length: 12 }, (_, i) => WORDS[bits11(bits, i)]).join(' ');
}

// wordIndex is the index of a typed word: the word itself, or for one of
// at least four letters the list word with the same first four. -1 if none.
export function wordIndex(w) {
  if (INDEX.has(w)) return INDEX.get(w);
  if (w.length < 4) return -1;
  return BY_FOUR.has(w.slice(0, 4)) ? BY_FOUR.get(w.slice(0, 4)) : -1;
}

// readPhrase turns a typed phrase back into its 16 bytes: lower-cased,
// split on white space, each word matched to the list, the checksum
// checked.
export async function readPhrase(typed) {
  const words = String(typed).toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length !== 12) throw new PhraseError();
  const bits = new Uint8Array(17);
  for (let i = 0; i < 12; i++) {
    const idx = wordIndex(words[i]);
    if (idx < 0) throw new PhraseError();
    for (let k = 0; k < 11; k++) {
      if ((idx >> (10 - k)) & 1) { const bit = i * 11 + k; bits[bit >> 3] |= 1 << (7 - (bit % 8)); }
    }
  }
  const entropy = bits.slice(0, 16);
  if (bits[16] >> 4 !== (await sha256(entropy))[0] >> 4) throw new PhraseError();
  return entropy;
}

// --- hand-overs to an app (13.3, 13.7) ---

const handoverContext = (account, codeChallenge) => concat(idBytes(account), utf8(codeChallenge));

// What the sign-in page makes for an app: the backup key sealed to the
// app's box key, bound to the account and the PKCE code challenge.
export const sealHandover = (appBoxKey, account, codeChallenge, backupKey, oneTime) => seal(appBoxKey, labelSealHandover, handoverContext(account, codeChallenge), backupKey, oneTime);

export async function openHandover(appBox, account, codeChallenge, handover) {
  const key = await openSealed(appBox, labelSealHandover, handoverContext(account, codeChallenge), handover);
  if (key.length !== 32) throw new OpenError();
  return key;
}

// The two-layer hand-over of QR pairing: 112 bytes.
export async function sealPairingHandover(appBoxKey, pairingSecret, account, backupKey, oneTime, salt) {
  const inner = await seal(appBoxKey, labelSealPairing, idBytes(account), backupKey, oneTime);
  return lock(pairingSecret, labelLockPairing, idBytes(account), inner, salt);
}

export async function openPairingHandover(appBox, pairingSecret, account, handover) {
  const inner = await unlockLocked(pairingSecret, labelLockPairing, idBytes(account), handover);
  const key = await openSealed(appBox, labelSealPairing, idBytes(account), inner);
  if (key.length !== 32) throw new OpenError();
  return key;
}
