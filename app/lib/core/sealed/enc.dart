// The record encoding of docs/SEALED.md section 2.3: a fixed binary layout
// with exactly one valid form. A record is its fields in order with nothing
// between them and nothing after the last.

import 'dart:convert';
import 'dart:typed_data';

import 'errors.dart';
import 'prim.dart';

/// The longest record: 64 KB.
const maxRecord = 64 * 1024;

/// Builds a record.
class Writer {
  final BytesBuilder _b = BytesBuilder();
  void u8(int v) => _b.addByte(v & 0xff);
  void u32(int v) => _b.add(u32be(v));
  void u64(int v) => _b.add(u64be(v));

  /// The `bytes(n)` type: exactly these bytes.
  void raw(List<int> b) => _b.add(b);
  void uuid(Uuid u) => _b.add(u.bytes);
  void time(int ms) => u64(ms);
  void blob(List<int> b) {
    u32(b.length);
    _b.add(b);
  }

  void text(String s) => blob(utf8.encode(s));

  /// The record. One over 64 KB is refused.
  Uint8List bytes() {
    if (_b.length > maxRecord) throw RecordError.tooLong;
    return _b.toBytes();
  }
}

/// Takes a record apart. The first failure sticks: later reads return zero
/// values, and [done] reports it.
class Reader {
  final Uint8List _b;
  int _at = 0;
  RecordError? _err;

  Reader(List<int> b) : _b = b is Uint8List ? b : Uint8List.fromList(b) {
    if (_b.length > maxRecord) _err = RecordError.tooLong;
  }

  int get _left => _b.length - _at;

  Uint8List? _take(int n) {
    if (_err != null) return null;
    if (n < 0 || _left < n) {
      _err = RecordError.short;
      return null;
    }
    final out = Uint8List.sublistView(_b, _at, _at + n);
    _at += n;
    return out;
  }

  int u8() => _take(1)?[0] ?? 0;

  /// A u8 that must be one of [allowed].
  int choice(List<int> allowed) {
    final v = u8();
    if (_err != null) return 0;
    if (allowed.contains(v)) return v;
    _err = RecordError.choice;
    return 0;
  }

  int u32() {
    final b = _take(4);
    return b == null ? 0 : readU32(b, 0);
  }

  int u64() {
    final b = _take(8);
    return b == null ? 0 : readU64(b, 0);
  }

  /// Exactly [n] bytes, as a copy.
  Uint8List raw(int n) {
    final b = _take(n);
    return b == null ? Uint8List(n) : Uint8List.fromList(b);
  }

  Uuid uuid() => Uuid(raw(16));

  Uint8List blob() {
    final n = u32();
    if (_err != null) return Uint8List(0);
    if (n > _left) {
      _err = RecordError.short;
      return Uint8List(0);
    }
    return raw(n);
  }

  /// A blob that must hold valid UTF-8. It is never normalised.
  String text() {
    final b = blob();
    if (_err != null) return '';
    try {
      return const Utf8Decoder(allowMalformed: false).convert(b);
    } on FormatException {
      _err = RecordError.utf8;
      return '';
    }
  }

  /// The u32 count in front of a list. A count that could not fit in what
  /// is left, given the smallest size of one item, is refused at once.
  int count(int minItem) {
    final n = u32();
    if (_err != null) return 0;
    if (n * minItem > _left) {
      _err = RecordError.short;
      return 0;
    }
    return n;
  }

  /// Records a refusal the caller found itself, such as a list out of order.
  void fail(RecordError e) => _err ??= e;

  RecordError? get err => _err;

  /// Ends the record: bytes left over are refused. Throws the first failure.
  void done() {
    if (_err != null) throw _err!;
    if (_left != 0) throw RecordError.trailing;
  }
}
