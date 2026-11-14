// The FileChunkCipher interface (lib/core/sealed/file_cipher.dart): the
// pure-Dart implementation against every file vector, the tamper and
// truncation refusals, and the self-check that makes the app fall back to
// pure Dart when a platform implementation is missing or wrong. The
// platform implementation itself only runs inside the app; the same checks
// run against it in platform/harness_main.dart (see there).

import 'dart:async';
import 'dart:typed_data';

import 'package:churr/core/sealed/sealed.dart';
import 'package:flutter_test/flutter_test.dart';

import 'file_cipher_checks.dart';
import 'vectors_helper.dart';

/// The pure-Dart cipher with one fault, for the self-check.
class Broken implements FileChunkCipher {
  final String fault;
  Broken(this.fault);

  @override
  String get name => 'broken ($fault)';

  @override
  Future<Uint8List> seal(
    Uint8List key,
    Uint8List nonce,
    Uint8List plaintext,
    Uint8List ad,
  ) async {
    switch (fault) {
      case 'throws':
        throw UnsupportedError('no platform here');
      case 'hangs':
        return Completer<Uint8List>().future;
      case 'wrong bytes':
        final out = gcmSeal(key, nonce, plaintext, ad);
        return out..[out.length - 1] ^= 1;
      case 'ignores the nonce':
        return gcmSeal(key, Uint8List(12), plaintext, ad);
      case 'ignores the associated data':
        return gcmSeal(key, nonce, plaintext, const []);
      case 'tag first':
        final out = gcmSeal(key, nonce, plaintext, ad);
        return cat([
          out.sublist(out.length - 16),
          out.sublist(0, out.length - 16),
        ]);
      case 'large inputs':
        if (plaintext.length > 4096) throw StateError('too large for the channel');
    }
    return gcmSeal(key, nonce, plaintext, ad);
  }

  @override
  Future<Uint8List> open(
    Uint8List key,
    Uint8List nonce,
    Uint8List sealed,
    Uint8List ad,
  ) async {
    if (fault == 'skips the tag check') {
      try {
        return gcmOpen(key, nonce, sealed, ad);
      } on OpenError {
        return Uint8List(sealed.length - 16);
      }
    }
    if (fault == 'wrong error') {
      try {
        return gcmOpen(key, nonce, sealed, ad);
      } on OpenError {
        throw StateError('not an OpenError');
      }
    }
    return gcmOpen(key, nonce, sealed, ad);
  }
}

void main() {
  final vectors = loadVector('file.json');

  group('pure Dart', () {
    test('every file vector', () async {
      expect(
        await checkFileVectors(dartFileChunkCipher, vectors),
        (vectors['files'] as List).length +
            (vectors['refusals'] as List).length,
      );
    });

    test('the streams through the interface give the bytes of the whole-buffer functions, awkward sizes', () async {
      final key = sha256('same bytes'.codeUnits);
      for (final size in awkwardSizes) {
        final plain = Uint8List.fromList([
          for (var i = 0; i < size; i++) (i * 7 + size) & 255,
        ]);
        final blob = await encryptWith(dartFileChunkCipher, key, plain);
        sameBytes(blob, encryptFile(key, plain), 'blob of $size bytes');
        sameBytes(
          await decryptWith(dartFileChunkCipher, key, blob),
          plain,
          'decrypted $size bytes',
        );
      }
      expect(
        await checkSameBytes(dartFileChunkCipher, const DartFileChunkCipher()),
        awkwardSizes.length,
      );
    });

    test('tampered and truncated files are refused', () async {
      expect(await checkTamper(dartFileChunkCipher), 20);
    });

    test('passes its self-check', () async {
      expect(await fileChunkCipherSelfCheck(dartFileChunkCipher), isTrue);
    });
  });

  group('self-check and fallback', () {
    for (final fault in [
      'throws',
      'wrong bytes',
      'ignores the nonce',
      'ignores the associated data',
      'tag first',
      'large inputs',
      'skips the tag check',
      'wrong error',
    ]) {
      test(
        'an implementation that $fault fails and pure Dart is chosen',
        () async {
          final logs = <String>[];
          final broken = Broken(fault);
          expect(
            await fileChunkCipherSelfCheck(broken, log: logs.add),
            isFalse,
          );
          expect(logs.single, contains('broken ($fault)'));
          expect(
            await selectFileChunkCipher(broken, log: (_) {}),
            same(dartFileChunkCipher),
          );
        },
      );
    }

    test(
      'an implementation that never answers times out and pure Dart is chosen',
      () async {
        final logs = <String>[];
        expect(
          await fileChunkCipherSelfCheck(
            Broken('hangs'),
            timeout: const Duration(milliseconds: 50),
            log: logs.add,
          ),
          isFalse,
        );
        expect(logs.single, contains('TimeoutException'));
      },
    );

    test('a correct implementation is kept; none given is pure Dart', () async {
      final good = Broken('nothing');
      expect(await selectFileChunkCipher(good), same(good));
      expect(await selectFileChunkCipher(null), same(dartFileChunkCipher));
    });

    test('the streams use the installed cipher when none is passed', () async {
      final key = sha256('installed'.codeUnits);
      final plain = Uint8List.fromList(List.filled(1000, 7));
      expect(fileChunkCipher, same(dartFileChunkCipher));
      try {
        fileChunkCipher = Broken('wrong bytes');
        final blob = await encryptFileStream(
          key,
          Stream.value(plain),
        ).expand((c) => c).toList();
        expect(bytesEqual(blob, encryptFile(key, plain)), isFalse);
        await expectLater(
          decryptFileStream(
            key,
            Stream.value(encryptFile(key, plain)),
          ).toList(),
          completes,
        );
        fileChunkCipher = Broken('throws');
        await expectLater(
          encryptFileStream(key, Stream.value(plain)).toList(),
          throwsUnsupportedError,
        );
      } finally {
        fileChunkCipher = dartFileChunkCipher;
      }
    });
  });
}
