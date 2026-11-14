// Seal and lock (docs/SEALED.md sections 2.5 and 2.6): the two ways
// anything is encrypted. Each gives AES-256-GCM a key used exactly once.
//
// The random value each needs (a one-time private key, a salt) is passed
// in; the callers draw it from a SealedRandom (random.js), so tests can
// give the vectors' values.

import { concat } from './bytes.js';
import { OpenError, ZeroSharedError } from './errors.js';
import { gcmOpen, gcmSeal, hkdf, x25519, x25519Public } from './prim.js';
import { labelled } from './sign.js';

export const SALT_SIZE = 16;

// seal encrypts plaintext to a recipient's X25519 public key. oneTime is
// the private half of a fresh key pair, used once. The result is the
// one-time public key and then the ciphertext with its tag.
export async function seal(recipient, label, context, plaintext, oneTime) {
  if (recipient.length !== 32 || oneTime.length !== 32) throw new Error('seal needs a 32-byte recipient key and a 32-byte one-time key');
  const shared = await x25519(oneTime, recipient);
  const e = await x25519Public(oneTime);
  const ad = labelled(label, context);
  const okm = await hkdf(shared, concat(e, recipient), ad, 44);
  return concat(e, await gcmSeal(okm.subarray(0, 32), okm.subarray(32), plaintext, ad));
}

// openSealed opens what seal made, with the recipient's private key (32
// bytes or a BoxKey). Any failure is an OpenError.
export async function openSealed(priv, label, context, sealed) {
  if (!sealed || sealed.length < 32 + 16) throw new OpenError();
  const e = sealed.subarray(0, 32);
  let shared;
  try { shared = await x25519(priv, e); } catch (err) {
    if (err instanceof ZeroSharedError) throw new OpenError();
    throw err;
  }
  const ad = labelled(label, context);
  const okm = await hkdf(shared, concat(e, await x25519Public(priv)), ad, 44);
  return gcmOpen(okm.subarray(0, 32), okm.subarray(32), sealed.subarray(32), ad);
}

// lock encrypts plaintext under 32 secret bytes with a 16-byte salt.
export async function lock(key, label, context, plaintext, salt) {
  if (key.length !== 32 || salt.length !== SALT_SIZE) throw new Error('lock needs a 32-byte key and a 16-byte salt');
  const ad = labelled(label, context);
  const okm = await hkdf(key, salt, ad, 44);
  return concat(salt, await gcmSeal(okm.subarray(0, 32), okm.subarray(32), plaintext, ad));
}

export async function unlockLocked(key, label, context, locked) {
  if (!key || key.length !== 32 || !locked || locked.length < SALT_SIZE + 16) throw new OpenError();
  const ad = labelled(label, context);
  const okm = await hkdf(key, locked.subarray(0, SALT_SIZE), ad, 44);
  return gcmOpen(okm.subarray(0, 32), okm.subarray(32), locked.subarray(SALT_SIZE), ad);
}
