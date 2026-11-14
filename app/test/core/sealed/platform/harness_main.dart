// Runs the file-cipher checks of file_cipher_checks.dart and a 100 MB
// timing against the platform AES-256-GCM inside the real app, where the
// plugin is registered. Not a test (no _test suffix, so `flutter test`
// skips it): build it as the app's entry point in release mode and run the
// binary, which prints its results and exits without drawing anything.
// The app is sandboxed and cannot read testdata/, so file.json comes in as
// a compile-time define:
//
//   flutter build macos --release -t test/core/sealed/platform/harness_main.dart \
//     --dart-define=FILE_VECTORS=$(base64 -i ../testdata/sealed/file.json)
//   build/macos/Build/Products/Release/churr.app/Contents/MacOS/churr
//
// (`open -g -j -W --stdout out.txt build/.../churr.app` keeps the window
// hidden and the focus where it was.)

import 'dart:convert';
import 'dart:io';
import 'dart:isolate';
import 'dart:typed_data';

import 'package:churr/core/sealed/sealed.dart';
import 'package:churr/platform/sealed_file_cipher.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

import '../file_cipher_checks.dart';

void say(String s) => stdout.writeln(s);

Stream<List<int>> source(Uint8List data, int piece) async* {
  for (var i = 0; i < data.length; i += piece) {
    yield Uint8List.sublistView(
      data,
      i,
      i + piece > data.length ? data.length : i + piece,
    );
  }
}

Future<void> time(FileChunkCipher c, int runs) async {
  const size = 100 * 1024 * 1024;
  final plain = Uint8List(size);
  for (var i = 0; i < size; i++) {
    plain[i] = i % 251;
  }
  final key = sha256([1, 2, 3]);
  for (var run = 1; run <= runs; run++) {
    final sw = Stopwatch()..start();
    final blob = BytesBuilder(copy: false);
    await for (final x in encryptFileStream(
      key,
      source(plain, 1 << 20),
      cipher: c,
    )) {
      blob.add(x);
    }
    final enc = sw.elapsedMilliseconds;
    final sealed = blob.takeBytes();
    sw.reset();
    var n = 0;
    await for (final x in decryptFileStream(
      key,
      source(sealed, 1 << 20),
      cipher: c,
    )) {
      n += x.length;
    }
    final dec = sw.elapsedMilliseconds;
    if (n != size) throw StateError('size');
    final mb = size / (1024 * 1024);
    say(
      'TIME ${c.name} run $run: encrypt ${(enc / 1000).toStringAsFixed(2)} s '
      '(${(mb / (enc / 1000)).toStringAsFixed(0)} MB/s), decrypt ${(dec / 1000).toStringAsFixed(2)} s '
      '(${(mb / (dec / 1000)).toStringAsFixed(0)} MB/s), blob ${sealed.length} bytes',
    );
  }
}

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  var failed = false;
  try {
    const encoded = String.fromEnvironment('FILE_VECTORS');
    if (encoded.isEmpty) throw StateError('build with --dart-define=FILE_VECTORS=<base64 of file.json>',);
    final vectors =
        jsonDecode(utf8.decode(base64.decode(encoded))) as Map<String, dynamic>;

    final platform = PlatformFileChunkCipher();
    say('platform cipher claims support: ${platform.claimsSupport}');
    say('self-check: ${await fileChunkCipherSelfCheck(platform, log: say)}');
    final installed = await installSealedFileCipher(log: say);
    if (installed != platform && installed.name != platform.name) throw StateError('platform cipher not installed');

    for (final c in [dartFileChunkCipher, platform]) {
      say(
        'VECTORS ${c.name}: ${await checkFileVectors(c, vectors)} cases passed',
      );
      say('TAMPER ${c.name}: ${await checkTamper(c)} refusals passed');
    }
    var same = 0;
    for (var seed = 1; seed <= 3; seed++) {
      same += await checkSameBytes(dartFileChunkCipher, platform, seed: seed);
    }
    say(
      'SAME BYTES: $same random files of sizes $awkwardSizes identical and cross-decrypted',
    );

    final token = RootIsolateToken.instance!;
    final fromIsolate = await Isolate.run(() async {
      final c = await installSealedFileCipherInIsolate(token, log: (_) {});
      final key = sha256('isolate'.codeUnits);
      final plain = Uint8List.fromList(List.generate(200000, (i) => i * 3));
      final blob = await encryptFileStream(
        key,
        Stream.value(plain),
      ).expand((x) => x).toList();
      return '${c.name}, 200000-byte file ${bytesEqual(blob, encryptFile(key, plain)) ? 'matches' : 'DIFFERS FROM'} pure Dart';
    }, debugName: 'background');
    say('BACKGROUND ISOLATE: $fromIsolate');

    await time(platform, 3);
  } catch (e, st) {
    failed = true;
    say('FAILED: $e\n$st');
  }
  await stdout.flush();
  exit(failed ? 1 : 0);
}
