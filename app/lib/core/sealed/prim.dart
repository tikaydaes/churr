// The five primitives of docs/SEALED.md section 2.2 (X25519, Ed25519,
// HKDF-SHA-256, AES-256-GCM, SHA-256), and the small byte helpers every
// other file uses.
//
// Package. The primitives come from `package:cryptography` 2.9.0, used
// only through its pure-Dart classes (DartX25519, DartEd25519, DartAesGcm,
// DartHmac, DartSha256), so the same code runs on Android, iOS, macOS,
// Windows and Linux with no plugin and no platform implementation. It was
// chosen over its fork `cryptography_plus` 3.0.0 (checked 2026-11-14):
//
// - The pure-Dart implementations of all five primitives are identical in
//   the two packages, line for line, apart from the package name in their
//   imports; the fork changed the web, Flutter-plugin and build parts.
// - `cryptography` is published by the verified publisher dint.dev, has
//   about 1.37 million downloads in 30 days against the fork's 39 thousand
//   (315 likes against 13), so far more code depends on it being right.
// - Against it: its repository's last commit is 2025-11-21 (2.9.0), with
//   38 open issues, while the fork (emz-hanauer/dart-cryptography) had
//   commits in 2026-09. If the original stays idle, switching is one
//   import in this file, since nothing else here touches the package.
//
// Both accept caller-supplied private keys (X25519 from 32 bytes, Ed25519
// from a 32-byte seed) and nonces, which the test vectors need. Ed25519 in
// DartEd25519 is RFC 8032 deterministic, so signatures made here equal the
// Go reference's. Ed25519 is only offered as `Future`s, so everything that
// signs or verifies is async; the rest is synchronous.
//
// HKDF is written here on the package's synchronous HMAC-SHA-256 (RFC 5869
// extract then expand, a dozen lines) because the package's DartHkdf is
// async only; the RFC 5869 vectors in primitives.json check it.
//
// An X25519 public key is computed as X25519(private, 9) through the same
// synchronous function as every other agreement, as section 2.2 says.

import 'dart:typed_data';

import 'package:cryptography/cryptography.dart'
    show
        KeyPairType,
        Mac,
        SecretBox,
        SecretKeyData,
        Signature,
        SimpleKeyPairData,
        SimplePublicKey;
import 'package:cryptography/dart.dart'
    show DartAesGcm, DartEd25519, DartHmac, DartSha256, DartX25519;

import 'errors.dart';

// ---------------------------------------------------------------- bytes

/// Lower-case hex, no prefix.
String hexOf(List<int> b) {
  const digits = '0123456789abcdef';
  final out = StringBuffer();
  for (final x in b) {
    out
      ..write(digits[(x >> 4) & 15])
      ..write(digits[x & 15]);
  }
  return out.toString();
}

/// Reads lower- or upper-case hex.
Uint8List unhex(String s) {
  if (s.length.isOdd) throw FormatException('odd-length hex', s);
  final out = Uint8List(s.length ~/ 2);
  for (var i = 0; i < out.length; i++) {
    out[i] = int.parse(s.substring(2 * i, 2 * i + 2), radix: 16);
  }
  return out;
}

/// Joins byte strings end to end.
Uint8List cat(List<List<int>> parts) {
  var n = 0;
  for (final p in parts) {
    n += p.length;
  }
  final out = Uint8List(n);
  var i = 0;
  for (final p in parts) {
    out.setRange(i, i + p.length, p);
    i += p.length;
  }
  return out;
}

/// Byte-for-byte equality.
bool bytesEqual(List<int>? a, List<int>? b) {
  if (a == null || b == null) return a == b;
  if (a.length != b.length) return false;
  var d = 0;
  for (var i = 0; i < a.length; i++) {
    d |= a[i] ^ b[i];
  }
  return d == 0;
}

/// Orders byte strings as the record encoding orders set keys: byte by
/// byte, ascending, a prefix first.
int compareBytes(List<int> a, List<int> b) {
  final n = a.length < b.length ? a.length : b.length;
  for (var i = 0; i < n; i++) {
    if (a[i] != b[i]) return a[i] < b[i] ? -1 : 1;
  }
  return a.length - b.length;
}

Uint8List u32be(int v) =>
    Uint8List(4)..buffer.asByteData().setUint32(0, v & 0xffffffff);

Uint8List u64be(int v) {
  final b = Uint8List(8);
  for (var i = 7; i >= 0; i--) {
    b[i] = v & 0xff;
    v >>= 8;
  }
  return b;
}

int readU64(List<int> b, int at) {
  var v = 0;
  for (var i = 0; i < 8; i++) {
    v = (v << 8) | b[at + i];
  }
  return v;
}

int readU32(List<int> b, int at) =>
    (b[at] << 24) | (b[at + 1] << 16) | (b[at + 2] << 8) | b[at + 3];

/// An identifier as its 16 bytes.
class Uuid implements Comparable<Uuid> {
  final Uint8List bytes;
  Uuid(List<int> b) : bytes = Uint8List.fromList(b) {
    if (b.length != 16) throw ArgumentError('an identifier is 16 bytes');
  }
  static final Uuid zero = Uuid(Uint8List(16));

  /// Reads the lower-case hyphenated form (upper case is taken too).
  factory Uuid.parse(String s) {
    if (s.length != 36 ||
        s[8] != '-' ||
        s[13] != '-' ||
        s[18] != '-' ||
        s[23] != '-') {
      throw FormatException('not an identifier', s);
    }
    return Uuid(unhex(s.replaceAll('-', '')));
  }

  static Uuid? tryParse(String? s) {
    if (s == null) return null;
    try {
      return Uuid.parse(s);
    } on FormatException {
      return null;
    }
  }

  bool get isZero => bytes.every((b) => b == 0);

  @override
  String toString() {
    final h = hexOf(bytes);
    return '${h.substring(0, 8)}-${h.substring(8, 12)}-${h.substring(12, 16)}-'
        '${h.substring(16, 20)}-${h.substring(20)}';
  }

  @override
  bool operator ==(Object other) =>
      other is Uuid && bytesEqual(bytes, other.bytes);

  @override
  int get hashCode => Object.hashAll(bytes);

  @override
  int compareTo(Uuid other) => compareBytes(bytes, other.bytes);
}

// ---------------------------------------------------------------- SHA-256, HKDF

const _sha256 = DartSha256();
final _hmac = DartHmac.sha256();

Uint8List sha256(List<int> data) =>
    Uint8List.fromList(_sha256.hashSync(data).bytes);

Uint8List _hmacSha256(List<int> key, List<int> data) {
  final mac = _hmac.calculateMacSync(
    data,
    secretKeyData: SecretKeyData(key),
    nonce: const <int>[],
  );
  return Uint8List.fromList(mac.bytes);
}

/// HKDF-SHA-256, extract then expand, giving [n] bytes (RFC 5869).
Uint8List hkdf(List<int> ikm, List<int> salt, List<int> info, int n) {
  if (n > 255 * 32) throw ArgumentError('hkdf: output too long');
  final prk = _hmacSha256(salt.isEmpty ? Uint8List(32) : salt, ikm);
  final out = BytesBuilder(copy: false);
  var t = Uint8List(0);
  for (var i = 1; out.length < n; i++) {
    t = _hmacSha256(
      prk,
      cat([
        t,
        info,
        [i],
      ]),
    );
    out.add(t);
  }
  return Uint8List.sublistView(out.takeBytes(), 0, n);
}

// ---------------------------------------------------------------- X25519

const _x25519 = DartX25519();
final Uint8List _basePoint = Uint8List(32)..[0] = 9;

/// X25519 of RFC 7748. A result of 32 zero bytes is refused with
/// [ZeroSharedError].
Uint8List x25519(List<int> private, List<int> public) {
  if (private.length != 32 || public.length != 32) {
    throw ArgumentError('X25519 keys are 32 bytes');
  }
  final pair = SimpleKeyPairData(
    DartX25519.modifiedPrivateKeyBytes(private),
    publicKey: SimplePublicKey(Uint8List(32), type: KeyPairType.x25519),
    type: KeyPairType.x25519,
  );
  final secret = _x25519.sharedSecretSync(
    keyPairData: pair,
    remotePublicKey: SimplePublicKey(
      Uint8List.fromList(public),
      type: KeyPairType.x25519,
    ),
  ) as SecretKeyData;
  final shared = Uint8List.fromList(secret.bytes);
  if (shared.every((b) => b == 0)) throw const ZeroSharedError();
  return shared;
}

/// The public key of an X25519 private key: X25519(private, 9).
Uint8List x25519Public(List<int> private) => x25519(private, _basePoint);

// ---------------------------------------------------------------- AES-256-GCM

final _gcm = DartAesGcm.with256bits();

/// AES-256-GCM with a 12-byte nonce; the 16-byte tag is appended.
Uint8List gcmSeal(
  List<int> key,
  List<int> nonce,
  List<int> plaintext,
  List<int> ad,
) {
  if (key.length != 32 || nonce.length != 12) throw ArgumentError('AES-256-GCM: 32-byte key, 12-byte nonce');
  final box = _gcm.encryptSync(
    plaintext,
    secretKeyData: SecretKeyData(key),
    nonce: nonce,
    aad: ad,
  );
  return cat([box.cipherText, box.mac.bytes]);
}

/// The inverse of [gcmSeal]. Any failure is an [OpenError].
Uint8List gcmOpen(
  List<int> key,
  List<int> nonce,
  List<int> ciphertext,
  List<int> ad,
) {
  if (key.length != 32 || nonce.length != 12) throw ArgumentError('AES-256-GCM: 32-byte key, 12-byte nonce');
  if (ciphertext.length < 16) throw const OpenError();
  final n = ciphertext.length - 16;
  try {
    final plain = _gcm.decryptSync(
      SecretBox(
        Uint8List.sublistView(Uint8List.fromList(ciphertext), 0, n),
        nonce: nonce,
        mac: Mac(ciphertext.sublist(n)),
      ),
      secretKeyData: SecretKeyData(key),
      aad: ad,
    );
    return Uint8List.fromList(plain);
  } catch (_) {
    throw const OpenError();
  }
}

// ---------------------------------------------------------------- Ed25519

final _ed25519 = DartEd25519();

/// An Ed25519 private key: its 32-byte seed and the public key.
class SigningKey {
  final Uint8List seed;
  final Uint8List public;
  SigningKey._(this.seed, this.public);

  static Future<SigningKey> fromSeed(List<int> seed) async {
    if (seed.length != 32) throw ArgumentError('an Ed25519 seed is 32 bytes');
    final pair = await _ed25519.newKeyPairFromSeed(seed);
    final pub = await pair.extractPublicKey();
    return SigningKey._(
      Uint8List.fromList(seed),
      Uint8List.fromList(pub.bytes),
    );
  }

  /// Ed25519 over exactly [message].
  Future<Uint8List> signRaw(List<int> message) async {
    final pair = SimpleKeyPairData(
      seed,
      publicKey: SimplePublicKey(public, type: KeyPairType.ed25519),
      type: KeyPairType.ed25519,
    );
    final sig = await _ed25519.sign(message, keyPair: pair);
    return Uint8List.fromList(sig.bytes);
  }
}

/// Verifies an Ed25519 signature over exactly [message]. Never throws.
Future<bool> verifyRaw(
  List<int> public,
  List<int> message,
  List<int> sig,
) async {
  if (public.length != 32 || sig.length != 64) return false;
  try {
    return await _ed25519.verify(
      message,
      signature: Signature(
        sig,
        publicKey: SimplePublicKey(public, type: KeyPairType.ed25519),
      ),
    );
  } catch (_) {
    return false;
  }
}
