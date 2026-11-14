// The platform's own AES-256-GCM for sealed file contents, behind the
// FileChunkCipher interface of lib/core/sealed/file_cipher.dart. The app
// calls [installSealedFileCipher] once at start-up (and
// [installSealedFileCipherInIsolate] in any background isolate that
// encrypts files); the file streams then use whichever implementation
// passed its self-check, falling back to pure Dart.
//
// Plugin: `cryptography_flutter` 2.3.4 (dint.dev, the publisher of the
// `cryptography` package the sealed library already uses), checked
// 2026-11-14 against the alternatives on pub.dev:
//
// - What it accelerates. AES-GCM goes to CryptoKit (AES.GCM.seal/open) on
//   iOS 13+ and macOS 10.15+, and to javax.crypto "AES/GCM/NoPadding" on
//   Android (Conscrypt, i.e. BoringSSL with the CPU's AES instructions).
//   Windows, Linux and the web have no platform implementation and fall
//   back to pure Dart.
// - Caller-supplied nonce and associated data are passed straight through
//   to CryptoKit and javax.crypto; the result is the ciphertext and the
//   16-byte tag separately, which are joined here into the standard
//   ciphertext || tag layout. The self-check confirms the bytes.
// - Its own silent fallbacks are switched off: by default FlutterAesGcm
//   handles inputs under 2,000 bytes in Dart and falls back to another
//   isolate when the channel fails. Here every length goes to the channel
//   and the "fallback" throws, so a broken platform path shows up as a
//   failed self-check (and the app falls back openly) rather than as a
//   quietly slow one.
// - Background isolates: the plugin talks over a MethodChannel, which a
//   background isolate can use once BackgroundIsolateBinaryMessenger is
//   initialised; the plugin's Dart half also has to be registered there
//   (DartPluginRegistrant), since it only marks itself present when
//   registered. [installSealedFileCipherInIsolate] does both. The work
//   itself always runs on the platform's main thread, a 64 KiB chunk at a
//   time.
// - Alternatives. `cryptography_flutter_plus` 3.2.0 (a fork, active in
//   2026-09, 18 thousand downloads in 30 days against 89 thousand) has the
//   same Swift and Kotlin code line for line; it adds Swift Package Manager
//   and AGP 9 built-in-Kotlin support, neither needed here (this app sets
//   android.builtInKotlin=false, as other plugins require), its Windows and
//   Linux entries are Dart-only, and it depends on `cryptography_plus`, a
//   second copy of the crypto library beside `cryptography` (see
//   lib/core/sealed/prim.dart). Switching is the import below and the
//   pubspec line. `webcrypto` 0.6.2 (Google, maintained) is fast on every
//   platform including Windows and Linux, but it is BoringSSL compiled
//   into the app through build hooks (a C toolchain on every build
//   machine), not the operating system's implementation, which is what
//   was asked for. `pointycastle` is pure Dart.
// - Against it: its repository's last release is 2025-11-21, the same
//   idle state as `cryptography` itself.

import 'dart:isolate' show Isolate;
import 'dart:typed_data';
import 'dart:ui' show DartPluginRegistrant;

import 'package:cryptography/cryptography.dart'
    show
        AesGcm,
        Mac,
        SecretBox,
        SecretBoxAuthenticationError,
        SecretKey,
        SecretKeyData;
import 'package:cryptography_flutter/cryptography_flutter.dart'
    show CryptographyChannelPolicy, FlutterAesGcm;
import 'package:flutter/foundation.dart' show debugPrint;
import 'package:flutter/services.dart'
    show BackgroundIsolateBinaryMessenger, RootIsolateToken;

import '../core/sealed/sealed.dart';

/// AES-256-GCM through `cryptography_flutter`'s FlutterAesGcm, with its
/// in-Dart shortcuts and fallbacks switched off.
class PlatformFileChunkCipher implements FileChunkCipher {
  final FlutterAesGcm _gcm = FlutterAesGcm(
    secretKeyLength: 32,
    channelPolicy: const CryptographyChannelPolicy(
      minLength: 0,
      maxLength: null,
    ),
    fallback: _NoFallback(),
  );

  @override
  String get name => 'platform (cryptography_flutter)';

  /// Whether the plugin claims a platform implementation here: it is
  /// registered in this isolate and the platform is Android, iOS or macOS.
  bool get claimsSupport => _gcm.isSupportedPlatform;

  @override
  Future<Uint8List> seal(
    Uint8List key,
    Uint8List nonce,
    Uint8List plaintext,
    Uint8List ad,
  ) async {
    if (key.length != 32 || nonce.length != 12) throw ArgumentError('AES-256-GCM: 32-byte key, 12-byte nonce');
    final box = await _gcm.encrypt(
      plaintext,
      secretKey: SecretKeyData(key),
      nonce: nonce,
      aad: ad,
    );
    final tag = box.mac.bytes;
    if (box.cipherText.length != plaintext.length || tag.length != 16) {
      throw StateError(
        'platform AES-256-GCM returned ${box.cipherText.length} + ${tag.length} bytes '
        'for ${plaintext.length}',
      );
    }
    return cat([box.cipherText, tag]);
  }

  @override
  Future<Uint8List> open(
    Uint8List key,
    Uint8List nonce,
    Uint8List sealed,
    Uint8List ad,
  ) async {
    if (key.length != 32 || nonce.length != 12) throw ArgumentError('AES-256-GCM: 32-byte key, 12-byte nonce');
    if (sealed.length < 16) throw const OpenError();
    final n = sealed.length - 16;
    final box = SecretBox(
      Uint8List.sublistView(sealed, 0, n),
      nonce: nonce,
      mac: Mac(Uint8List.sublistView(sealed, n)),
    );
    try {
      final plain = await _gcm.decrypt(
        box,
        secretKey: SecretKeyData(key),
        aad: ad,
      );
      return plain is Uint8List ? plain : Uint8List.fromList(plain);
    } on SecretBoxAuthenticationError {
      throw const OpenError();
    }
  }
}

/// Stands in for FlutterAesGcm's fallback so that it never quietly does
/// the work in Dart.
class _NoFallback extends AesGcm {
  _NoFallback() : super.constructor();

  @override
  int get secretKeyLength => 32;

  @override
  int get nonceLength => AesGcm.defaultNonceLength;

  @override
  Future<SecretBox> encrypt(
    List<int> clearText, {
    required SecretKey secretKey,
    List<int>? nonce,
    List<int> aad = const <int>[],
    Uint8List? possibleBuffer,
  }) => throw UnsupportedError('no platform AES-256-GCM here');

  @override
  Future<List<int>> decrypt(
    SecretBox secretBox, {
    required SecretKey secretKey,
    List<int> aad = const <int>[],
    Uint8List? possibleBuffer,
  }) => throw UnsupportedError('no platform AES-256-GCM here');
}

/// Chooses the AES-256-GCM for sealed file contents in this isolate: the
/// platform's if the plugin claims one here and it passes the self-check,
/// pure Dart otherwise. Sets [fileChunkCipher] and returns it.
Future<FileChunkCipher> installSealedFileCipher({
  void Function(String)? log,
}) async {
  log ??= debugPrint;
  final platform = PlatformFileChunkCipher();
  final chosen = await selectFileChunkCipher(
    platform.claimsSupport ? platform : null,
    log: log,
  );
  fileChunkCipher = chosen;
  log('sealed files: ${chosen.name} AES-256-GCM');
  return chosen;
}

/// [installSealedFileCipher] for a background isolate: [token] is
/// `RootIsolateToken.instance` taken in the root isolate and passed in.
Future<FileChunkCipher> installSealedFileCipherInIsolate(
  RootIsolateToken token, {
  void Function(String)? log,
}) async {
  BackgroundIsolateBinaryMessenger.ensureInitialized(token);
  DartPluginRegistrant.ensureInitialized();
  return installSealedFileCipher(
    log: log ?? (m) => debugPrint('[${Isolate.current.debugName}] $m'),
  );
}
