// The access key grammar (docs/AUTH.md section 5.6) against the vectors
// the server and the web pages share (testdata/accesskey).

import 'dart:convert';
import 'dart:io';

import 'package:churr/core/access_key.dart';
import 'package:churr/core/sealed/prim.dart' show sha256;
import 'package:flutter_test/flutter_test.dart';

String _hex(List<int> b) =>
    b.map((x) => x.toRadixString(16).padLeft(2, '0')).join();

List<int> _unhex(String s) => [
  for (var i = 0; i < s.length; i += 2)
    int.parse(s.substring(i, i + 2), radix: 16),
];

void main() {
  final v = jsonDecode(
    File('../testdata/accesskey/vectors.json').readAsStringSync(),
  ) as Map<String, dynamic>;

  test('the alphabet', () => expect(accessKeyAlphabet, v['alphabet']));

  group('keys', () {
    for (final k in (v['keys'] as List).cast<Map<String, dynamic>>()) {
      test(k['name'], () {
        final r = _unhex(k['random'] as String);
        expect(formatAccessKey(r), k['key']);
        expect(_hex(accessKeyAuth(r)), k['auth']);
        expect(_hex(sha256(accessKeyAuth(r))), k['auth_hash']);
        expect(_hex(accessKeyWrap(r)), k['wrap']);
        expect(_hex(parseAccessKey(k['key'] as String)), k['random']);
      });
    }
  });

  group('inputs', () {
    for (final c in (v['inputs'] as List).cast<Map<String, dynamic>>()) {
      test(c['name'], () {
        if (c['error'] == null) {
          expect(_hex(parseAccessKey(c['input'] as String)), c['random']);
          return;
        }
        try {
          parseAccessKey(c['input'] as String);
          fail('accepted, want ${c['error']}');
        } on AccessKeyException catch (e) {
          expect(e.kind, c['error']);
          expect(e.message, c['message']);
        }
      });
    }
  });
}
