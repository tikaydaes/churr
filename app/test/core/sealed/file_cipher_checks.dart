// File-encryption checks run against any FileChunkCipher: by
// file_cipher_test.dart under `flutter test` for the pure-Dart one, and by
// platform/harness_main.dart inside the release macOS app for the platform
// one, where the plugin really runs. Plain Dart, no flutter_test, so the
// harness can use it too; a failure throws a [CheckFailed].

import 'dart:math';
import 'dart:typed_data';

import 'package:churr/core/sealed/sealed.dart';

class CheckFailed implements Exception {
  final String message;
  CheckFailed(this.message);
  @override
  String toString() => 'CheckFailed: $message';
}

void _same(List<int> got, List<int> want, String what) {
  if (bytesEqual(got, want)) return;
  var i = 0;
  while (i < got.length && i < want.length && got[i] == want[i]) {
    i++;
  }
  throw CheckFailed(
    '$what: first differing byte at $i (got ${got.length} bytes, want ${want.length})',
  );
}

Uint8List _pattern(int n) =>
    Uint8List.fromList([for (var i = 0; i < n; i++) i % 251]);

Stream<List<int>> _pieces(List<int> b, int size) async* {
  for (var i = 0; i < b.length; i += size) {
    yield Uint8List.sublistView(
      b is Uint8List ? b : Uint8List.fromList(b),
      i,
      i + size > b.length ? b.length : i + size,
    );
  }
}

Future<Uint8List> _collect(Stream<List<int>> s) async {
  final out = BytesBuilder(copy: false);
  await for (final c in s) {
    out.add(c);
  }
  return out.takeBytes();
}

Future<Uint8List> encryptWith(
  FileChunkCipher c,
  List<int> key,
  List<int> plain, {
  int piece = 7777,
}) => _collect(encryptFileStream(key, _pieces(plain, piece), cipher: c));

Future<Uint8List> decryptWith(
  FileChunkCipher c,
  List<int> key,
  List<int> blob, {
  int piece = 10007,
}) => _collect(decryptFileStream(key, _pieces(blob, piece), cipher: c));

Future<void> _refused(
  FileChunkCipher c,
  List<int> key,
  Uint8List blob,
  String why,
) async {
  for (final piece in [4096, blob.length + 1]) {
    try {
      await decryptWith(c, key, blob, piece: piece);
    } on FileError {
      continue;
    }
    throw CheckFailed(
      '${c.name}: decrypted what must be refused: $why (pieces of $piece)',
    );
  }
}

/// Every file and refusal of testdata/sealed/file.json ([v], decoded)
/// through [c]. Returns the number of cases checked.
Future<int> checkFileVectors(FileChunkCipher c, Map<String, dynamic> v) async {
  var n = 0;
  final key = unhex(v['key'] as String);
  for (final f in v['files'] as List) {
    final size = f['size'] as int;
    final what = '${c.name}: file of $size bytes';
    final plain = _pattern(size);
    final blob = await encryptWith(c, key, plain);
    if (blob.length != f['blob_size']) throw CheckFailed('$what: blob size ${blob.length}');
    if (f['blob'] != null) _same(blob, unhex(f['blob']), '$what: blob');
    _same(
      blob.sublist(0, min(32, blob.length)),
      unhex(f['blob_first_bytes']),
      '$what: first bytes',
    );
    _same(
      blob.sublist(max(0, blob.length - 32)),
      unhex(f['blob_last_bytes']),
      '$what: last bytes',
    );
    _same(sha256(blob), unhex(f['blob_sha256']), '$what: blob sha256');
    _same(await decryptWith(c, key, blob), plain, '$what: decrypted');
    n++;
  }
  final big = await encryptWith(c, key, _pattern(200000));
  final blobs = {
    'chunks reordered': cat([
      big.sublist(sealedChunkSize, 2 * sealedChunkSize),
      big.sublist(0, sealedChunkSize),
      big.sublist(2 * sealedChunkSize),
    ]),
    'the file cut short': big.sublist(0, 3 * sealedChunkSize),
    'the file cut short to one chunk': (await encryptWith(
      c,
      key,
      _pattern(65537),
    )).sublist(0, sealedChunkSize),
    'a missing last-chunk mark': await encryptChunkWith(
      c,
      key,
      0,
      false,
      _pattern(1),
    ),
    'a changed byte': (await encryptWith(c, key, _pattern(1)))..[0] ^= 1,
    'shorter than a tag': Uint8List(15),
  };
  for (final x in v['refusals'] as List) {
    final why = x['why'] as String;
    final blob = blobs[why];
    if (blob == null) throw CheckFailed('no blob made for refusal "$why"');
    _same(
      sha256(blob),
      unhex(x['blob_sha256']),
      '${c.name}: refusal "$why" blob sha256',
    );
    await _refused(c, key, blob, why);
    n++;
  }
  return n;
}

/// Sizes around the chunk boundaries, and several chunks.
const awkwardSizes = [
  0,
  1,
  15,
  16,
  17,
  65535,
  65536,
  65537,
  3 * 65536,
  3 * 65536 + 5,
  5 * 65536 - 1,
];

/// [a] and [b] produce identical blobs for random keys and contents of
/// [awkwardSizes], and each decrypts the other's. Returns the cases.
Future<int> checkSameBytes(
  FileChunkCipher a,
  FileChunkCipher b, {
  int seed = 1,
}) async {
  final r = Random(seed);
  Uint8List rnd(int n) =>
      Uint8List.fromList([for (var i = 0; i < n; i++) r.nextInt(256)]);
  var n = 0;
  for (final size in awkwardSizes) {
    final key = rnd(32);
    final plain = rnd(size);
    final blobA = await encryptWith(
      a,
      key,
      plain,
      piece: 1 + r.nextInt(100000),
    );
    final blobB = await encryptWith(
      b,
      key,
      plain,
      piece: 1 + r.nextInt(100000),
    );
    _same(blobA, blobB, '${a.name} against ${b.name}, $size bytes');
    _same(
      await decryptWith(b, key, blobA),
      plain,
      '${b.name} decrypting ${a.name}, $size bytes',
    );
    _same(
      await decryptWith(a, key, blobB),
      plain,
      '${a.name} decrypting ${b.name}, $size bytes',
    );
    n++;
  }
  return n;
}

/// A file cut short, reordered, without its last-chunk mark or changed is
/// refused by [c] (as negative_test.dart does for the pure-Dart one).
Future<int> checkTamper(FileChunkCipher c) async {
  final key = sha256('tamper key'.codeUnits);
  var n = 0;
  Future<void> refused(Uint8List blob, String why) async {
    await _refused(c, key, blob, why);
    n++;
  }

  for (final size in [0, 1, 65535, 65536, 65537, 200000]) {
    final plain = _pattern(size);
    final blob = await encryptWith(c, key, plain);
    _same(
      await decryptWith(c, key, blob),
      plain,
      '${c.name}: round trip of $size bytes',
    );
    if (blob.length > sealedChunkSize) {
      await refused(
        blob.sublist(0, sealedChunkSize),
        'cut short at a chunk boundary ($size)',
      );
      await refused(
        blob.sublist(0, blob.length - 1),
        'last byte missing ($size)',
      );
      if (blob.length >= 2 * sealedChunkSize) {
        await refused(
          cat([
            blob.sublist(sealedChunkSize, 2 * sealedChunkSize),
            blob.sublist(0, sealedChunkSize),
            blob.sublist(2 * sealedChunkSize),
          ]),
          'reordered ($size)',
        );
      }
    }
    await refused(
      Uint8List.fromList(blob)..[blob.length ~/ 2] ^= 1,
      'changed in the middle ($size)',
    );
    await refused(
      Uint8List.fromList(blob)..[blob.length - 1] ^= 1,
      'tag changed ($size)',
    );
  }
  await refused(
    await encryptChunkWith(c, key, 0, false, Uint8List.fromList('x'.codeUnits)),
    'no last-chunk mark',
  );
  await refused(Uint8List(0), 'empty');
  await refused(
    (await encryptWith(
      c,
      key,
      _pattern(65536 + 3),
    )).sublist(0, sealedChunkSize + 10),
    'a tail shorter than a tag',
  );
  return n;
}
