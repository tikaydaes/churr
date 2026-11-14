// Byte helpers for sealed chats (docs/SEALED.md section 2.3): hex,
// base64url, joining and comparing, identifiers, and the big-endian whole
// numbers of the record encoding.
//
// 64-bit values. A `u64` can be up to 2^64 − 1 and a JavaScript number is
// exact only to 2^53 − 1. Every u64 read here comes back as a number when
// it fits and as a BigInt when it does not, and every function that writes
// or compares one takes either. Arithmetic on a value that may be large
// goes through big() so the two kinds never meet in one expression.

const enc = new TextEncoder();

export const utf8 = (s) => enc.encode(s);

export function concat(...parts) {
  let n = 0;
  for (const p of parts) n += p.length;
  const out = new Uint8Array(n);
  let at = 0;
  for (const p of parts) { out.set(p, at); at += p.length; }
  return out;
}

// equal compares in time that does not depend on where two strings differ.
export function equal(a, b) {
  if (a == null || b == null) return a == b;
  if (a.length !== b.length) return false;
  let d = 0;
  for (let i = 0; i < a.length; i++) d |= a[i] ^ b[i];
  return d === 0;
}

// compare orders byte strings as the record encoding orders set keys:
// byte by byte, ascending, a prefix first.
export function compare(a, b) {
  const n = Math.min(a.length, b.length);
  for (let i = 0; i < n; i++) if (a[i] !== b[i]) return a[i] < b[i] ? -1 : 1;
  return a.length - b.length;
}

export function isZero(b) {
  for (const x of b) if (x !== 0) return false;
  return true;
}

export function fromHex(s) {
  if (s.length % 2) throw new Error('odd-length hex');
  const out = new Uint8Array(s.length / 2);
  for (let i = 0; i < out.length; i++) {
    const v = parseInt(s.substr(i * 2, 2), 16);
    if (Number.isNaN(v)) throw new Error('not hex');
    out[i] = v;
  }
  return out;
}

export function toHex(b) {
  let s = '';
  for (const x of b) s += (x < 16 ? '0' : '') + x.toString(16);
  return s;
}

// In JSON, bytes are base64url without padding. A value that is not
// base64url gives null.
export function fromB64(s) {
  if (typeof s !== 'string' || s.includes('=') || /[^A-Za-z0-9_-]/.test(s)) return null;
  try {
    const bin = atob(s.replace(/-/g, '+').replace(/_/g, '/'));
    const out = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
  } catch { return null; }
}

export function toB64(b) {
  let s = '';
  for (let i = 0; i < b.length; i += 0x8000) s += String.fromCharCode.apply(null, b.subarray(i, i + 0x8000));
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

// --- identifiers ---

const UUID = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/;

export const ZERO_ID = '00000000-0000-0000-0000-000000000000';

export function isId(s) { return typeof s === 'string' && UUID.test(s); }

// An identifier as its 16 bytes.
export function idBytes(id) {
  if (!isId(id)) throw new Error('not an identifier: ' + id);
  return fromHex(id.replace(/-/g, ''));
}

// 16 bytes as an identifier, lower case and hyphenated.
export function idOf(b) {
  const x = toHex(b);
  return `${x.slice(0, 8)}-${x.slice(8, 12)}-${x.slice(12, 16)}-${x.slice(16, 20)}-${x.slice(20)}`;
}

export const compareIds = (a, b) => compare(idBytes(a), idBytes(b));

// --- whole numbers ---

const MAX = BigInt(Number.MAX_SAFE_INTEGER);
const U64 = (1n << 64n) - 1n;

// big makes a BigInt of a number or a BigInt.
export const big = (v) => (typeof v === 'bigint' ? v : BigInt(v));

// small gives a number when the value fits exactly, a BigInt otherwise.
export const small = (v) => (typeof v === 'bigint' && v <= MAX && v >= -MAX ? Number(v) : v);

export function u32(v) {
  if (!Number.isInteger(v) || v < 0 || v > 0xffffffff) throw new RangeError('not a u32: ' + v);
  return new Uint8Array([v >>> 24, (v >>> 16) & 255, (v >>> 8) & 255, v & 255]);
}

export function u64(v) {
  const x = big(v);
  if (x < 0n || x > U64) throw new RangeError('not a u64: ' + v);
  const out = new Uint8Array(8);
  new DataView(out.buffer).setBigUint64(0, x);
  return out;
}

export function readU32(b, at = 0) {
  return ((b[at] << 24) >>> 0) + (b[at + 1] << 16) + (b[at + 2] << 8) + b[at + 3];
}

export function readU64(b, at = 0) {
  return small(new DataView(b.buffer, b.byteOffset + at, 8).getBigUint64(0));
}

// sameNumber says whether two u64 values, of either kind, are equal.
export const sameNumber = (a, b) => big(a) === big(b);

// The random generator of section 2.2.
export function randomBytes(n) {
  return globalThis.crypto.getRandomValues(new Uint8Array(n));
}
