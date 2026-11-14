// Device lists (docs/SEALED.md section 4): the signed list of an
// account's devices, its chain from version to version, and the checks a
// client makes before it trusts one.

import { compareIds, concat, equal, idBytes, isZero, toHex, big } from './bytes.js';
import { Reader, Writer } from './enc.js';
import { ListError, RecordError, SealedError } from './errors.js';
import { hashOf, labelDeviceList, labelDeviceListHash, labelDeviceListRollover, labelDeviceProof, sign, verify } from './sign.js';

export const PLATFORMS = { 1: 'android', 2: 'ios', 3: 'macos', 4: 'windows', 5: 'linux', 6: 'web' };
export const PLATFORM_WEB = 6;
const ALL_PLATFORMS = [1, 2, 3, 4, 5, 6];

export function platformNumber(name) {
  for (const [n, w] of Object.entries(PLATFORMS)) if (w === name) return Number(n);
  return 0;
}

// What a person is told a device is. A browser is always called one.
export function platformLabel(n) {
  return { 1: 'Android app', 2: 'iPhone app', 3: 'Mac app', 4: 'Windows app', 5: 'Linux app', 6: 'a browser' }[n] || 'a device';
}

export const MAX_DEVICES = 50;
const DEVICE_SIZE = 16 + 32 + 32 + 1 + 8 + 64;

// encodeList writes the list record of section 4.3. l: { format, accountId,
// server, version, prev, reset, keyVersion, accountKey, devices, at }, the
// devices { deviceId, signKey, boxKey, platform, addedAt, proof } sorted
// by deviceId.
export function encodeList(l) {
  if (l.accountKey.length !== 32) throw new Error('an account key is 32 bytes');
  if (l.devices.length > MAX_DEVICES) throw RecordError.count();
  const w = new Writer().u8(l.format ?? 1).uuid(l.accountId).text(l.server).u64(l.version)
    .raw(l.prev || new Uint8Array(32)).u8(l.reset || 0).u32(l.keyVersion).raw(l.accountKey).u32(l.devices.length);
  l.devices.forEach((d, i) => {
    if (d.signKey.length !== 32 || d.boxKey.length !== 32 || d.proof.length !== 64) throw new Error('a device needs a 32-byte sign key, a 32-byte box key and a 64-byte proof');
    if (i > 0 && compareIds(l.devices[i - 1].deviceId, d.deviceId) >= 0) throw RecordError.order();
    w.uuid(d.deviceId).raw(d.signKey).raw(d.boxKey).u8(d.platform).time(d.addedAt).raw(d.proof);
  });
  w.time(l.at);
  return w.bytes();
}

// decodeList reads a list record, refusing anything that is not its one
// valid form.
export function decodeList(b) {
  const r = new Reader(b);
  const l = { format: r.choice([1]), accountId: r.uuid(), server: r.text(), version: r.u64(), prev: r.raw(32), reset: r.choice([0, 1]), keyVersion: r.u32(), accountKey: r.raw(32), devices: [] };
  const n = r.count(DEVICE_SIZE);
  if (n > MAX_DEVICES) r.fail(RecordError.count());
  for (let i = 0; i < n && !r.err; i++) {
    const d = { deviceId: r.uuid(), signKey: r.raw(32), boxKey: r.raw(32), platform: r.choice(ALL_PLATFORMS), addedAt: r.u64(), proof: r.raw(64) };
    if (i > 0 && compareIds(l.devices[i - 1].deviceId, d.deviceId) >= 0) r.fail(RecordError.order());
    l.devices.push(d);
  }
  l.at = r.u64();
  r.done();
  return l;
}

export const listDevice = (l, id) => l.devices.find((d) => d.deviceId === id) || null;

// What a device proof signs: account_id || device_id || sign_key || box_key.
export function proofBytes(account, device, signKey, boxKey) {
  return concat(idBytes(account), idBytes(device), signKey, boxKey);
}

// A device's own signature that it holds its signing key and belongs to
// this account.
export function deviceProof(signKey, account, device, boxKey) {
  return sign(signKey, labelDeviceProof, proofBytes(account, device, signKey.public, boxKey));
}

export const listHash = (list) => hashOf(labelDeviceListHash, list);

// What is stored and sent for one version: { list, sig, prevSig }.
export function encodeSignedList(s) {
  if (s.sig.length !== 64 || (s.prevSig && s.prevSig.length !== 64)) throw new Error('a signature is 64 bytes');
  const w = new Writer().blob(s.list).raw(s.sig);
  if (s.prevSig) w.u8(1).raw(s.prevSig); else w.u8(0);
  return w.bytes();
}

export function decodeSignedList(b) {
  const r = new Reader(b);
  const list = r.blob();
  const sig = r.raw(64);
  let prevSig = null;
  if (r.choice([0, 1]) === 1) prevSig = r.raw(64);
  r.done();
  return { list, sig, prevSig };
}

// signList signs a list record with its account key and, for a key
// change, with the previous version's account key too.
export async function signList(list, accountKey, previous) {
  const sig = await sign(accountKey, labelDeviceList, list);
  const prevSig = previous ? await sign(previous, labelDeviceListRollover, list) : null;
  return { list, sig, prevSig };
}

// The checks one version allows by itself: it parses, its own account key
// signed it, every proof verifies and no identifier or key repeats.
export async function checkListAlone(s) {
  let l;
  try { l = decodeList(s.list); } catch (e) {
    if (e instanceof RecordError) throw new ListError(0, e.message);
    throw e;
  }
  if (big(l.version) === 0n) throw new ListError(1, "a list's version starts at 1");
  const seen = new Set();
  for (const d of l.devices) {
    if (!(await verify(d.signKey, labelDeviceProof, proofBytes(l.accountId, d.deviceId, d.signKey, d.boxKey), d.proof))) {
      throw new ListError(2, "a device's proof does not verify");
    }
    for (const k of ['s' + toHex(d.signKey), 'b' + toHex(d.boxKey)]) {
      if (seen.has(k)) throw new ListError(2, 'two devices have the same key');
      seen.add(k);
    }
  }
  if (!(await verify(l.accountKey, labelDeviceList, s.list, s.sig))) throw new ListError(3, 'the list is not signed by its account key');
  return l;
}

// checkFirstList checks a list a client has no pin for: trust on first use.
export async function checkFirstList(s) {
  const l = await checkListAlone(s);
  if (big(l.version) === 1n) {
    if (!isZero(l.prev)) throw new ListError(1, 'version 1 has a previous hash');
    if (s.prevSig) throw new ListError(3, 'version 1 has a rollover signature');
  }
  return l;
}

export const STEP_ORDINARY = 'ordinary';
export const STEP_KEY_CHANGE = 'key_change';
export const STEP_RESET = 'reset';

// checkListStep checks the step from one accepted version (its record,
// exactly as signed) to the next, and says which kind it is: [list, step].
export async function checkListStep(prev, next) {
  let p;
  try { p = decodeList(prev); } catch { throw new SealedError('the previous list does not parse'); }
  const l = await checkListAlone(next);
  // 1. The chain.
  if (l.accountId !== p.accountId || l.server !== p.server) throw new ListError(1, 'the account or server changed');
  if (big(l.version) !== big(p.version) + 1n) throw new ListError(1, 'the version is not one higher');
  if (!equal(l.prev, await listHash(prev))) throw new ListError(1, 'prev is not the hash of the previous version');
  // 3. One of the three kinds of step.
  const sameKey = equal(l.accountKey, p.accountKey);
  if (l.reset === 1) {
    if (l.keyVersion !== p.keyVersion + 1) throw new ListError(3, 'a reset must raise the key version by one');
    if (next.prevSig) throw new ListError(3, 'a reset carries no rollover signature');
    return [l, STEP_RESET];
  }
  if (sameKey && l.keyVersion === p.keyVersion) {
    if (next.prevSig) throw new ListError(3, 'an ordinary change carries no rollover signature');
    return [l, STEP_ORDINARY];
  }
  if (!sameKey && l.keyVersion === p.keyVersion + 1) {
    if (!next.prevSig || !(await verify(p.accountKey, labelDeviceListRollover, next.list, next.prevSig))) {
      throw new ListError(3, 'the previous account key did not sign the key change');
    }
    return [l, STEP_KEY_CHANGE];
  }
  throw new ListError(3, 'the account key and key version do not fit any kind of step');
}
