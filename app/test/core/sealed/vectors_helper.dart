// Shared by the vector tests: reading testdata/sealed, comparing bytes with
// the first differing offset, and counting cases so the total passed out
// of the total can be reported.

import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:churr/core/sealed/sealed.dart';
import 'package:flutter_test/flutter_test.dart';

/// testdata/sealed, from the app directory (where `flutter test` runs) or
/// from the repository root.
Directory vectorsDir() {
  for (final p in [
    '../testdata/sealed',
    'testdata/sealed',
    '../../testdata/sealed',
  ]) {
    final d = Directory(p);
    if (d.existsSync()) return d;
  }
  throw StateError('testdata/sealed not found from ${Directory.current.path}');
}

Map<String, dynamic> loadVector(String name) =>
    jsonDecode(File('${vectorsDir().path}/$name').readAsStringSync())
        as Map<String, dynamic>;

Uint8List hx(Object? s) => unhex(s as String);
Uuid id(Object? s) => Uuid.parse(s as String);

/// A mismatch, with the first byte offset at which two byte strings differ.
class Mismatch implements Exception {
  final String message;
  Mismatch(this.message);
  @override
  String toString() => message;
}

void sameBytes(List<int> got, List<int> want, String what) {
  if (bytesEqual(got, want)) return;
  var i = 0;
  while (i < got.length && i < want.length && got[i] == want[i]) {
    i++;
  }
  throw Mismatch(
    '$what: first differing byte at offset $i (got ${got.length} bytes, want ${want.length}); '
    'got ${hexOf(got.sublist(i, (i + 8).clamp(0, got.length)))}…, '
    'want ${hexOf(want.sublist(i, (i + 8).clamp(0, want.length)))}…',
  );
}

void eq(Object? got, Object? want, String what) {
  if (got != want) throw Mismatch('$what: got $got, want $want');
}

void yes(bool ok, String what) {
  if (!ok) throw Mismatch(what);
}

/// Expects [f] to throw (any refusal is a refusal).
Future<void> refused(Future<Object?> Function() f, String what) async {
  try {
    await f();
  } catch (_) {
    return;
  }
  throw Mismatch('accepted what must be refused: $what');
}

/// Counts the cases of one vector file. Every case runs; failures are
/// collected and reported together at the end.
class Cases {
  final String file;
  int passed = 0;
  final List<String> failures = [];
  Cases(this.file);

  int get total => passed + failures.length;

  Future<void> check(String name, Future<void> Function() body) async {
    try {
      await body();
      passed++;
    } catch (e, st) {
      failures.add('$name: $e${e is Mismatch ? '' : '\n$st'}');
    }
  }

  void finish() {
    // ignore: avoid_print
    print('VECTORS $file: $passed/$total passed');
    if (failures.isNotEmpty) fail('${failures.length} of $total cases failed:\n${failures.join('\n')}',);
  }
}
