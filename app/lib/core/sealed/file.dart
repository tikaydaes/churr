// Files (docs/SEALED.md section 9): a file is cut into chunks of 65,536
// bytes and each is encrypted under a key used for that one file. Both
// directions work as streams, so a large file is never held in memory.
//
// The streams take their AES-256-GCM from a [FileChunkCipher]
// (file_cipher.dart), the platform's own where the app installed one; the
// chunk and whole-buffer functions below them are pure Dart and
// synchronous.

import 'dart:async';
import 'dart:typed_data';

import 'errors.dart';
import 'file_cipher.dart';
import 'prim.dart';
import 'sign.dart';

/// The plaintext length of every chunk but the last.
const chunkSize = 65536;

/// A full chunk with its tag.
const sealedChunkSize = chunkSize + 16;

final Uint8List _fileAd = Uint8List.fromList(labelFile.codeUnits);

/// The nonce of chunk [i]: 3 zero bytes, i as a u64, then 1 for the last
/// chunk and 0 otherwise.
Uint8List chunkNonce(int i, bool last) => cat([
  const [0, 0, 0],
  u64be(i),
  [last ? 1 : 0],
]);

Uint8List encryptChunk(List<int> key, int i, bool last, List<int> plain) =>
    gcmSeal(key, chunkNonce(i, last), plain, _fileAd);

Uint8List decryptChunk(List<int> key, int i, bool last, List<int> sealed) {
  try {
    return gcmOpen(key, chunkNonce(i, last), sealed, _fileAd);
  } on OpenError {
    throw const FileError();
  }
}

/// [encryptChunk] through [cipher].
Future<Uint8List> encryptChunkWith(
  FileChunkCipher cipher,
  Uint8List key,
  int i,
  bool last,
  Uint8List plain,
) => cipher.seal(key, chunkNonce(i, last), plain, _fileAd);

/// [decryptChunk] through [cipher].
Future<Uint8List> decryptChunkWith(
  FileChunkCipher cipher,
  Uint8List key,
  int i,
  bool last,
  Uint8List sealed,
) async {
  try {
    return await cipher.open(key, chunkNonce(i, last), sealed, _fileAd);
  } on OpenError {
    throw const FileError();
  }
}

/// Holds bytes until a whole piece of [size] is known not to be the last.
class _Pieces {
  final int size;
  Uint8List _buf;
  int _len = 0;
  _Pieces(this.size) : _buf = Uint8List(size * 2);

  void add(List<int> data) {
    if (_len + data.length > _buf.length) {
      final grown = Uint8List((_len + data.length) * 2);
      grown.setRange(0, _len, _buf);
      _buf = grown;
    }
    _buf.setRange(_len, _len + data.length, data);
    _len += data.length;
  }

  /// Pieces that are certainly not the last: only while more than one
  /// piece's worth is held.
  Iterable<Uint8List> takeNotLast() sync* {
    var start = 0;
    while (_len - start > size) {
      yield Uint8List.fromList(
        Uint8List.sublistView(_buf, start, start + size),
      );
      start += size;
    }
    if (start > 0) {
      _buf.setRange(0, _len - start, _buf, start);
      _len -= start;
    }
  }

  Uint8List rest() => Uint8List.fromList(Uint8List.sublistView(_buf, 0, _len));
}

/// Encrypts a file read as a stream; the output stream is the blob to
/// upload, one sealed chunk per event. An empty file is one empty chunk; a
/// file whose length is a multiple of the chunk size ends with a full one.
/// The AES-256-GCM is [cipher], or [fileChunkCipher] when none is given.
Stream<Uint8List> encryptFileStream(
  List<int> key,
  Stream<List<int>> plaintext, {
  FileChunkCipher? cipher,
}) async* {
  if (key.length != 32) throw ArgumentError('a file key is 32 bytes');
  final c = cipher ?? fileChunkCipher;
  final k = Uint8List.fromList(key);
  final pieces = _Pieces(chunkSize);
  var i = 0;
  await for (final data in plaintext) {
    pieces.add(data);
    for (final p in pieces.takeNotLast()) {
      yield await encryptChunkWith(c, k, i++, false, p);
    }
  }
  yield await encryptChunkWith(c, k, i, true, pieces.rest());
}

/// Decrypts a blob read as a stream, one checked chunk per event. A chunk
/// changed, moved or missing, or the blob cut short, ends the stream with
/// a [FileError]; the file must be used only once the stream has ended
/// without one, since only then has the last chunk checked out. The
/// AES-256-GCM is [cipher], or [fileChunkCipher] when none is given.
Stream<Uint8List> decryptFileStream(
  List<int> key,
  Stream<List<int>> blob, {
  FileChunkCipher? cipher,
}) async* {
  if (key.length != 32) throw ArgumentError('a file key is 32 bytes');
  final c = cipher ?? fileChunkCipher;
  final k = Uint8List.fromList(key);
  final pieces = _Pieces(sealedChunkSize);
  var i = 0;
  await for (final data in blob) {
    pieces.add(data);
    for (final p in pieces.takeNotLast()) {
      yield await decryptChunkWith(c, k, i++, false, p);
    }
  }
  final last = pieces.rest();
  // An empty blob, or a trailing piece shorter than a tag, is no chunk.
  if (last.length < 16) throw const FileError();
  yield await decryptChunkWith(c, k, i, true, last);
}

/// The whole-buffer form of [encryptFileStream].
Uint8List encryptFile(List<int> key, List<int> plaintext) {
  var chunks = (plaintext.length + chunkSize - 1) ~/ chunkSize;
  if (chunks == 0) chunks = 1;
  final out = BytesBuilder(copy: false);
  for (var i = 0; i < chunks; i++) {
    final end = (i + 1) * chunkSize < plaintext.length
        ? (i + 1) * chunkSize
        : plaintext.length;
    out.add(
      encryptChunk(
        key,
        i,
        i == chunks - 1,
        plaintext.sublist(i * chunkSize, end),
      ),
    );
  }
  return out.takeBytes();
}

/// The whole-buffer form of [decryptFileStream].
Uint8List decryptFile(List<int> key, List<int> blob) {
  if (blob.length < 16) throw const FileError();
  final rest = blob.length % sealedChunkSize;
  if (rest != 0 && rest < 16) throw const FileError();
  final chunks = (blob.length + sealedChunkSize - 1) ~/ sealedChunkSize;
  final out = BytesBuilder(copy: false);
  for (var i = 0; i < chunks; i++) {
    final end = (i + 1) * sealedChunkSize < blob.length
        ? (i + 1) * sealedChunkSize
        : blob.length;
    out.add(
      decryptChunk(
        key,
        i,
        i == chunks - 1,
        blob.sublist(i * sealedChunkSize, end),
      ),
    );
  }
  return out.takeBytes();
}
