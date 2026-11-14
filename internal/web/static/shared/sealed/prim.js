// The five primitives of docs/SEALED.md section 2.2 on the browser's own
// Web Crypto: X25519, Ed25519, HKDF-SHA-256, AES-256-GCM, SHA-256, and
// nothing else. Node has the same Web Crypto, so the tests run this file
// as it is.
//
// Raw private keys. Web Crypto imports a raw public key, but a private key
// only inside a PKCS#8 wrapper; the wrappers of a 32-byte X25519 private
// key and of a 32-byte Ed25519 seed are fixed 16-byte prefixes (RFC 8410).
// A public key is read back through a JWK export of a private key imported
// as extractable for that one moment, which gives X25519(private, 9) for
// X25519 and the RFC 8032 public key for Ed25519.
//
// Keys that cannot be read. A device's own two key pairs are held as
// CryptoKeys made non-extractable: BoxKey and SigningKey below hold one
// and its public half. A test makes them from given bytes; the browser
// makes them with generateKey. Either way the bytes are not readable once
// made. Keys that must travel (the account key, which lives in the
// account record) keep their seed beside the CryptoKey.

import { concat, fromB64, isZero } from './bytes.js';
import { ZeroSharedError, OpenError } from './errors.js';

const subtle = globalThis.crypto.subtle;

const PKCS8_X25519 = new Uint8Array([0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x6e, 0x04, 0x22, 0x04, 0x20]);
const PKCS8_ED25519 = new Uint8Array([0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x04, 0x22, 0x04, 0x20]);

// --- SHA-256 and HKDF ---

export async function sha256(bytes) {
  return new Uint8Array(await subtle.digest('SHA-256', bytes));
}

// HKDF-SHA-256, extract then expand, n bytes (RFC 5869).
export async function hkdf(ikm, salt, info, n) {
  const key = await subtle.importKey('raw', ikm, 'HKDF', false, ['deriveBits']);
  return new Uint8Array(await subtle.deriveBits({ name: 'HKDF', hash: 'SHA-256', salt, info }, key, n * 8));
}

// --- X25519 ---

// A device box key: an X25519 private key that cannot be read, and its
// public half.
export class BoxKey {
  constructor(key, pub) { this.key = key; this.public = pub; }

  // fromBytes makes one from 32 private bytes (tests, and unlock keys).
  static async fromBytes(priv, extractable = false) {
    if (priv.length !== 32) throw new Error('an X25519 private key is 32 bytes');
    const pub = await x25519Public(priv);
    const key = await subtle.importKey('pkcs8', concat(PKCS8_X25519, priv), { name: 'X25519' }, extractable, ['deriveBits']);
    return new BoxKey(key, pub);
  }

  // generate makes a fresh one whose private half is never readable.
  static async generate() {
    const pair = await subtle.generateKey({ name: 'X25519' }, false, ['deriveBits']);
    return new BoxKey(pair.privateKey, new Uint8Array(await subtle.exportKey('raw', pair.publicKey)));
  }
}

async function x25519Key(priv) {
  if (priv instanceof BoxKey) return priv.key;
  if (priv instanceof Uint8Array) {
    if (priv.length !== 32) throw new Error('an X25519 private key is 32 bytes');
    return subtle.importKey('pkcs8', concat(PKCS8_X25519, priv), { name: 'X25519' }, false, ['deriveBits']);
  }
  return priv; // a CryptoKey
}

// The public key of a private key: X25519(private, 9).
export async function x25519Public(priv) {
  if (priv instanceof BoxKey) return priv.public;
  const key = await subtle.importKey('pkcs8', concat(PKCS8_X25519, priv), { name: 'X25519' }, true, ['deriveBits']);
  return fromB64((await subtle.exportKey('jwk', key)).x);
}

// X25519 of RFC 7748. A result of 32 zero bytes is refused.
export async function x25519(priv, pub) {
  if (pub.length !== 32) throw new Error('an X25519 public key is 32 bytes');
  const theirs = await subtle.importKey('raw', pub, { name: 'X25519' }, true, []);
  let shared;
  try {
    shared = new Uint8Array(await subtle.deriveBits({ name: 'X25519', public: theirs }, await x25519Key(priv), 256));
  } catch {
    // Some engines refuse the all-zero result themselves.
    throw new ZeroSharedError();
  }
  if (isZero(shared)) throw new ZeroSharedError();
  return shared;
}

// --- AES-256-GCM ---

async function gcmKey(key, usage) {
  if (key.length !== 32) throw new Error('an AES-256-GCM key is 32 bytes');
  return subtle.importKey('raw', key, 'AES-GCM', false, [usage]);
}

// AES-256-GCM with a 12-byte nonce; the 16-byte tag is appended.
export async function gcmSeal(key, nonce, plaintext, ad) {
  if (nonce.length !== 12) throw new Error('an AES-GCM nonce is 12 bytes');
  return new Uint8Array(await subtle.encrypt({ name: 'AES-GCM', iv: nonce, additionalData: ad, tagLength: 128 }, await gcmKey(key, 'encrypt'), plaintext));
}

// The inverse of gcmSeal. Any failure is an OpenError.
export async function gcmOpen(key, nonce, ciphertext, ad) {
  if (nonce.length !== 12) throw new Error('an AES-GCM nonce is 12 bytes');
  if (ciphertext.length < 16) throw new OpenError();
  try {
    return new Uint8Array(await subtle.decrypt({ name: 'AES-GCM', iv: nonce, additionalData: ad, tagLength: 128 }, await gcmKey(key, 'decrypt'), ciphertext));
  } catch {
    throw new OpenError();
  }
}

// --- Ed25519 ---

// An Ed25519 private key and its public half. `seed` is kept only for a
// key that has to travel (the account key).
export class SigningKey {
  constructor(key, pub, seed = null) { this.key = key; this.public = pub; this.seed = seed; }

  // fromSeed makes one from a 32-byte seed. keepSeed keeps the bytes
  // beside it; a device's own key does not.
  static async fromSeed(seed, keepSeed = true) {
    if (seed.length !== 32) throw new Error('an Ed25519 seed is 32 bytes');
    const open = await subtle.importKey('pkcs8', concat(PKCS8_ED25519, seed), { name: 'Ed25519' }, true, ['sign']);
    const pub = fromB64((await subtle.exportKey('jwk', open)).x);
    const key = await subtle.importKey('pkcs8', concat(PKCS8_ED25519, seed), { name: 'Ed25519' }, false, ['sign']);
    return new SigningKey(key, pub, keepSeed ? new Uint8Array(seed) : null);
  }

  static async generate() {
    const pair = await subtle.generateKey({ name: 'Ed25519' }, false, ['sign', 'verify']);
    return new SigningKey(pair.privateKey, new Uint8Array(await subtle.exportKey('raw', pair.publicKey)));
  }

  // Ed25519 over exactly message.
  async signRaw(message) {
    return new Uint8Array(await subtle.sign({ name: 'Ed25519' }, this.key, message));
  }
}

// Verifies an Ed25519 signature over exactly message. Never throws.
export async function verifyRaw(pub, message, sig) {
  if (!pub || !sig || pub.length !== 32 || sig.length !== 64) return false;
  try {
    const key = await subtle.importKey('raw', pub, { name: 'Ed25519' }, false, ['verify']);
    return await subtle.verify({ name: 'Ed25519' }, key, sig, message);
  } catch {
    return false;
  }
}
