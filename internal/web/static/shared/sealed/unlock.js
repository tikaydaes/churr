// Approval from another device (docs/SEALED.md section 13.7): the unlock
// request a new, locked device posts and the code that stops the server
// swapping its keys; and the comparison code of section 15.

import { concat, idBytes, utf8 } from './bytes.js';
import { proofBytes } from './devicelist.js';
import { Reader, Writer } from './enc.js';
import { ListError, RecordError } from './errors.js';
import { sha256 } from './prim.js';
import { hashOf, labelCompare, labelDeviceApproval, labelDeviceProof, verify } from './sign.js';

const ALL_PLATFORMS = [1, 2, 3, 4, 5, 6];

// encodeUnlockRequest: u = { format, accountId, deviceId, signKey, boxKey,
// platform, at, proof }. 170 bytes.
export function encodeUnlockRequest(u) {
  if (u.signKey.length !== 32 || u.boxKey.length !== 32 || u.proof.length !== 64) throw RecordError.short();
  return new Writer().u8(u.format ?? 1).uuid(u.accountId).uuid(u.deviceId).raw(u.signKey).raw(u.boxKey).u8(u.platform).time(u.at).raw(u.proof).bytes();
}

// decodeUnlockRequest reads the record without checking the proof.
export function decodeUnlockRequest(b) {
  const r = new Reader(b);
  const u = { format: r.choice([1]), accountId: r.uuid(), deviceId: r.uuid(), signKey: r.raw(32), boxKey: r.raw(32), platform: r.choice(ALL_PLATFORMS), at: r.u64(), proof: r.raw(64) };
  r.done();
  return u;
}

// checkUnlockRequest reads a request and checks its device proof.
export async function checkUnlockRequest(b) {
  const u = decodeUnlockRequest(b);
  if (!(await verify(u.signKey, labelDeviceProof, proofBytes(u.accountId, u.deviceId, u.signKey, u.boxKey), u.proof))) {
    throw new ListError(2, "the device's proof does not verify");
  }
  return u;
}

const CROCKFORD = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

// approvalCode: the first 100 bits of SHA-256("churr device approval v1"
// || 0x00 || request) as 20 Crockford base32 characters in four groups.
export async function approvalCode(request) {
  const sum = await hashOf(labelDeviceApproval, request);
  let out = '';
  for (let i = 0; i < 20; i++) {
    let v = 0;
    for (let bit = i * 5; bit < (i + 1) * 5; bit++) v = (v << 1) | ((sum[bit >> 3] >> (7 - (bit % 8))) & 1);
    if (i > 0 && i % 5 === 0) out += '-';
    out += CROCKFORD[v];
  }
  return out;
}

// normalizeApprovalCode puts a typed code in the form approvalCode gives:
// upper case, hyphens and spaces dropped and put back, O read as 0 and I
// and L as 1.
export function normalizeApprovalCode(typed) {
  const chars = [];
  for (const c of String(typed).toUpperCase()) {
    if (c === '-' || c === ' ') continue;
    chars.push(c === 'O' ? '0' : c === 'I' || c === 'L' ? '1' : c);
  }
  return chars.map((c, i) => (i > 0 && i % 5 === 0 ? '-' : '') + c).join('');
}

// How many times each half of the comparison code is hashed again.
export const COMPARE_ROUNDS = 5200;

// compareHalf: one account's 30 digits (section 15).
export async function compareHalf(accountKey, account, server) {
  let h = await hashOf(labelCompare, concat(accountKey, idBytes(account), utf8(server)));
  for (let i = 0; i < COMPARE_ROUNDS; i++) h = await sha256(concat(h, accountKey));
  let out = '';
  for (let g = 0; g < 6; g++) {
    let v = 0n;
    for (const b of h.subarray(g * 5, g * 5 + 5)) v = (v << 8n) | BigInt(b);
    out += String(v % 100000n).padStart(5, '0');
  }
  return out;
}

// compareCode: the 60 digits two people compare, the smaller half first,
// in twelve groups of five separated by spaces.
export function compareCode(a, b) {
  if (b < a) [a, b] = [b, a];
  return (a + b).match(/.{5}/g).join(' ');
}
