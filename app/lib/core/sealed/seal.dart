// Seal and lock (docs/SEALED.md sections 2.5 and 2.6): the two ways
// anything is encrypted. Each gives AES-256-GCM a key used exactly once.
//
// Every function that needs random bytes is handed them; the caller draws
// them from a [SealedRandom] (random.dart), so tests can fix them.

import 'dart:typed_data';

import 'errors.dart';
import 'prim.dart';
import 'sign.dart';

/// Encrypts [plaintext] to a recipient's X25519 public key. [oneTime] is
/// the private half of a fresh key pair, used once. The result is the
/// one-time public key then the ciphertext with its tag.
Uint8List seal(
  List<int> recipient,
  String label,
  List<int> context,
  List<int> plaintext,
  List<int> oneTime,
) {
  if (recipient.length != 32 || oneTime.length != 32) {
    throw ArgumentError(
      'seal needs a 32-byte recipient key and a 32-byte one-time key',
    );
  }
  final shared = x25519(oneTime, recipient);
  final e = x25519Public(oneTime);
  final ad = labelled(label, [context]);
  final okm = hkdf(shared, cat([e, recipient]), ad, 44);
  return cat([e, gcmSeal(okm.sublist(0, 32), okm.sublist(32), plaintext, ad)]);
}

/// Opens what [seal] made for this private key, label and context. Any
/// failure is an [OpenError].
Uint8List openSealed(
  List<int> private,
  String label,
  List<int> context,
  List<int> sealed,
) {
  if (private.length != 32 || sealed.length < 32 + 16) throw const OpenError();
  final e = sealed.sublist(0, 32);
  final Uint8List shared;
  try {
    shared = x25519(private, e);
  } on ZeroSharedError {
    throw const OpenError();
  }
  final ad = labelled(label, [context]);
  final okm = hkdf(shared, cat([e, x25519Public(private)]), ad, 44);
  return gcmOpen(okm.sublist(0, 32), okm.sublist(32), sealed.sublist(32), ad);
}

/// The length of the random salt in front of a locked value.
const saltSize = 16;

/// Encrypts [plaintext] under 32 secret bytes with a 16-byte random salt.
Uint8List lock(
  List<int> key,
  String label,
  List<int> context,
  List<int> plaintext,
  List<int> salt,
) {
  if (key.length != 32 || salt.length != saltSize) {
    throw ArgumentError('lock needs a 32-byte key and a 16-byte salt');
  }
  final ad = labelled(label, [context]);
  final okm = hkdf(key, salt, ad, 44);
  return cat([
    salt,
    gcmSeal(okm.sublist(0, 32), okm.sublist(32), plaintext, ad),
  ]);
}

/// Opens what [lock] made under this key, label and context.
Uint8List unlockLocked(
  List<int> key,
  String label,
  List<int> context,
  List<int> locked,
) {
  if (key.length != 32 || locked.length < saltSize + 16) throw const OpenError();
  final ad = labelled(label, [context]);
  final okm = hkdf(key, locked.sublist(0, saltSize), ad, 44);
  return gcmOpen(
    okm.sublist(0, 32),
    okm.sublist(32),
    locked.sublist(saltSize),
    ad,
  );
}
