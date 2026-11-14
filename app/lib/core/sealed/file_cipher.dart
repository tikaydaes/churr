// AES-256-GCM for file chunks (docs/SEALED.md section 9) behind a small
// interface, so that the app can hand file contents to the platform's own
// AES-256-GCM where there is one. The pure-Dart AES-GCM of prim.dart
// manages about 14 MB a second; a 100 MB file takes about 7 seconds each
// way. Everything else in this library keeps using prim.dart directly: only
// file contents are large enough to matter.
//
// This file stays free of Flutter imports. The platform implementation
// lives in lib/platform/sealed_file_cipher.dart (it needs a Flutter
// plugin) and is installed by the app through [selectFileChunkCipher],
// which keeps it only if it reproduces known answers; otherwise the
// pure-Dart one is used. Both must produce identical bytes, and
// test/core/sealed checks both against every file vector.

import 'dart:async';
import 'dart:typed_data';

import 'errors.dart';
import 'prim.dart';
import 'sign.dart' show labelFile;

/// AES-256-GCM as file chunks use it: 32-byte key, 12-byte nonce,
/// associated data, and the 16-byte tag appended to the ciphertext.
abstract interface class FileChunkCipher {
  /// For logs: which implementation this is.
  String get name;

  /// Encrypts [plaintext]; the result is the ciphertext then the tag.
  Future<Uint8List> seal(
    Uint8List key,
    Uint8List nonce,
    Uint8List plaintext,
    Uint8List ad,
  );

  /// The inverse of [seal]. A wrong tag, or anything else that does not
  /// open, is an [OpenError]; an implementation that cannot run at all may
  /// throw something else.
  Future<Uint8List> open(
    Uint8List key,
    Uint8List nonce,
    Uint8List sealed,
    Uint8List ad,
  );
}

/// The pure-Dart AES-256-GCM of prim.dart. Runs everywhere.
class DartFileChunkCipher implements FileChunkCipher {
  const DartFileChunkCipher();

  @override
  String get name => 'pure Dart';

  @override
  Future<Uint8List> seal(
    Uint8List key,
    Uint8List nonce,
    Uint8List plaintext,
    Uint8List ad,
  ) async => gcmSeal(key, nonce, plaintext, ad);

  @override
  Future<Uint8List> open(
    Uint8List key,
    Uint8List nonce,
    Uint8List sealed,
    Uint8List ad,
  ) async => gcmOpen(key, nonce, sealed, ad);
}

const FileChunkCipher dartFileChunkCipher = DartFileChunkCipher();

/// The implementation the file streams of file.dart use when none is
/// passed. The pure-Dart one until the app installs another, normally the
/// result of [selectFileChunkCipher]. Each isolate has its own.
FileChunkCipher fileChunkCipher = dartFileChunkCipher;

// Known answers for the self-check, from testdata/sealed/file.json: under
// its key, the files of 0, 1 and 65,536 bytes (each one last chunk, the
// plaintext bytes i mod 251).
final Uint8List _checkKey = unhex(
  'cdf860813d99780315045ab0b05c3bb27d34da57167d666c2972178df6ab2da9',
);
final Uint8List _checkAd = Uint8List.fromList(labelFile.codeUnits);
final Uint8List _checkNonce = unhex('000000000000000000000001');
final Map<int, String> _checkBlobs = {
  0: 'd445a87ee041bf211af8d63102c2577c',
  1: '09beebd561e4e55a3678dcd821adb4efb8',
};
const _checkBlob65536Sha256 =
    'fac8dc8571551d7a7e5c60db7e52877a96a8ee87d88f984d202f3a7c712e92e3';

Uint8List _pattern(int n) =>
    Uint8List.fromList([for (var i = 0; i < n; i++) i % 251]);

/// Whether [cipher] reproduces the known answers: it seals the files of 0,
/// 1 and 65,536 bytes of testdata/sealed/file.json to the right bytes,
/// opens them again, and refuses each with one bit changed. Never throws;
/// a failure is described through [log].
Future<bool> fileChunkCipherSelfCheck(
  FileChunkCipher cipher, {
  Duration timeout = const Duration(seconds: 10),
  void Function(String)? log,
}) async {
  try {
    return await _selfCheck(cipher).timeout(timeout);
  } on _CheckFailed catch (e) {
    log?.call('${cipher.name} AES-256-GCM failed its self-check: ${e.what}');
    return false;
  } catch (e) {
    log?.call('${cipher.name} AES-256-GCM failed its self-check: $e');
    return false;
  }
}

class _CheckFailed implements Exception {
  final String what;
  _CheckFailed(this.what);
}

Future<bool> _selfCheck(FileChunkCipher c) async {
  for (final n in [0, 1, 65536]) {
    final plain = _pattern(n);
    final sealed = await c.seal(_checkKey, _checkNonce, plain, _checkAd);
    final want = _checkBlobs[n];
    final ok = want != null
        ? hexOf(sealed) == want
        : hexOf(sha256(sealed)) == _checkBlob65536Sha256;
    if (!ok || sealed.length != n + 16) throw _CheckFailed('sealing $n bytes gave the wrong bytes');
    final opened = await c.open(_checkKey, _checkNonce, sealed, _checkAd);
    if (!bytesEqual(opened, plain)) throw _CheckFailed('opening $n bytes gave the wrong bytes');
    for (final at in {0, sealed.length - 1}) {
      final bad = Uint8List.fromList(sealed)..[at] ^= 1;
      try {
        await c.open(_checkKey, _checkNonce, bad, _checkAd);
        throw _CheckFailed('opened $n bytes with byte $at changed');
      } on OpenError {
        // Refused, as it must be.
      }
    }
  }
  return true;
}

/// [candidate] if it is given and passes [fileChunkCipherSelfCheck],
/// otherwise the pure-Dart implementation.
Future<FileChunkCipher> selectFileChunkCipher(
  FileChunkCipher? candidate, {
  void Function(String)? log,
}) async {
  if (candidate != null && await fileChunkCipherSelfCheck(candidate, log: log)) return candidate;
  return dartFileChunkCipher;
}
