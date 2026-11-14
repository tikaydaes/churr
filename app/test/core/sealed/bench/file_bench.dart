// Times encrypting and decrypting a 100 MB file in 64 KB chunks with the
// pure-Dart AES-256-GCM. Not a test: compile and run it in release mode,
//
//   dart compile exe test/core/sealed/bench/file_bench.dart -o /tmp/file_bench
//   /tmp/file_bench

import 'dart:typed_data';

import 'package:churr/core/sealed/file.dart';
import 'package:churr/core/sealed/prim.dart';

Stream<List<int>> source(Uint8List data, int piece) async* {
  for (var i = 0; i < data.length; i += piece) {
    yield Uint8List.sublistView(
      data,
      i,
      i + piece > data.length ? data.length : i + piece,
    );
  }
}

Future<void> main() async {
  const size = 100 * 1024 * 1024;
  final plain = Uint8List(size);
  for (var i = 0; i < size; i++) {
    plain[i] = i % 251;
  }
  final key = sha256([1, 2, 3]);
  // Warm up.
  decryptFile(key, encryptFile(key, plain.sublist(0, 4 * chunkSize)));

  for (var run = 1; run <= 3; run++) {
    final sw = Stopwatch()..start();
    final blob = BytesBuilder(copy: false);
    await for (final c in encryptFileStream(key, source(plain, 1 << 20))) {
      blob.add(c);
    }
    final enc = sw.elapsedMilliseconds;
    final sealed = blob.takeBytes();
    sw.reset();
    var n = 0;
    await for (final c in decryptFileStream(key, source(sealed, 1 << 20))) {
      n += c.length;
    }
    final dec = sw.elapsedMilliseconds;
    if (n != size) throw StateError('size');
    final mb = size / (1024 * 1024);
    // ignore: avoid_print
    print(
      'run $run: encrypt ${(enc / 1000).toStringAsFixed(2)} s (${(mb / (enc / 1000)).toStringAsFixed(1)} MB/s), '
      'decrypt ${(dec / 1000).toStringAsFixed(2)} s (${(mb / (dec / 1000)).toStringAsFixed(1)} MB/s), blob ${sealed.length} bytes',
    );
  }
}
