// "Validate result" on the device (docs/FAIR.md section 11), as
// e2e/validate.mjs holds the web chat's: the document of the message of
// testdata/fair/message.json, through a link's fragment and back, and its
// separate verdicts on good data and on each kind of bad data.

import 'dart:convert';
import 'dart:io';

import 'package:churr/core/fair/validate.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  final mv = jsonDecode(
    File('../testdata/fair/message.json').readAsStringSync(),
  ) as Map<String, dynamic>;
  final FairKeys keys = (
    name: mv['server'] as String,
    history: (mv['keys'] as List).cast<Map>(),
    why: '',
  );
  final doc = exportFairMessage(mv['message'] as Map);
  FairVerdict one(List<FairVerdict> vs, String id) =>
      vs.firstWhere((v) => v.id == id);
  Iterable<FairVerdict> outcomes(List<FairVerdict> vs) =>
      vs.where((v) => v.id == 'outcome');
  Map<String, Object?> changed(void Function(Map<String, Object?> d) f) {
    final d = jsonDecode(jsonEncode(doc)) as Map<String, Object?>;
    f(d);
    return d;
  }

  test('the document, and through the fragment and back', () {
    expect(doc['churr_fair'], 1);
    expect(doc['spec'], fairSpec);
    expect(doc['library'], fairLibrary);
    expect((doc['objects'] as List).length, 2);
    expect(jsonEncode(fairFromFragment(fairFragment(doc))), jsonEncode(doc));
    expect(readFairDocument('{nope').error, isNotNull);
    expect(
      exportFairMessage(mv['message'] as Map, [1])['objects'],
      hasLength(1),
    );
  });

  test('good data: every verdict passes, each with its reason', () {
    final vs = validateFair(doc, keys: keys);
    expect([for (final v in vs) v.ok], everyElement(isTrue));
    expect(outcomes(vs), hasLength(2));
    expect(one(vs, 'alpha').time, isNotNull);
  });

  test('a tampered proof fails the proof and beta, not the outcomes', () {
    final d = changed((d) {
      final p = (d['proof'] as String).split('');
      p[20] = p[20] == 'A' ? 'B' : 'A';
      d['proof'] = p.join();
    });
    final vs = validateFair(d, keys: keys);
    expect(one(vs, 'proof').ok, isFalse);
    expect(one(vs, 'beta').ok, isFalse);
    expect(outcomes(vs).every((v) => v.ok == true), isTrue);
  });

  test('a wrong beta: the proof stands, beta and the outcomes do not', () {
    final vs = validateFair(changed((d) => d['beta'] = 'A' * 43), keys: keys);
    expect(one(vs, 'proof').ok, isTrue);
    expect(one(vs, 'beta').ok, isFalse);
    expect(outcomes(vs).any((v) => v.ok == false), isTrue);
  });

  test('a wrong outcome fails its own line only', () {
    final vs = validateFair(
      changed((d) {
        final row =
            (((d['objects'] as List)[1] as Map)['results']
                    as List)[0]['rows'][0]
                as Map;
        row['total'] = (row['total'] as int) + 1;
      }),
      keys: keys,
    );
    expect([for (final v in outcomes(vs)) v.ok], [true, false]);
  });

  test('a malformed alpha is said so', () {
    final nonce = readAlpha(doc['alpha'] as String)!.nonce;
    for (final bad in [
      '0$nonce',
      '01790000000|$nonce',
      '1790000000|${nonce.toUpperCase()}',
      '',
    ]) {
      final vs = validateFair(changed((d) => d['alpha'] = bad), keys: keys);
      expect(one(vs, 'alpha').ok, isFalse, reason: bad);
      expect(one(vs, 'proof').ok, isFalse, reason: bad);
    }
  });

  test(
    'whose key: not attributed, not the server\'s, or not in force then',
    () {
      expect(one(validateFair(doc), 'key').ok, isNull);
      expect(
        one(
          validateFair(
            doc,
            keys: (
              name: 'gone.example',
              history: null,
              why: 'gone.example could not be reached from here.',
            ),
          ),
          'key',
        ).ok,
        isNull,
      );
      expect(
        one(
          validateFair(
            doc,
            keys: (
              name: 'b.example',
              history: [(mv['keys'] as List).first as Map],
              why: '',
            ),
          ),
          'key',
        ).ok,
        isFalse,
      );
      final later = [
        (mv['keys'] as List).first as Map,
        {
          'key': ((mv['keys'] as List)[1] as Map)['key'],
          'began': '2099-01-01T00:00:00Z',
        },
      ];
      expect(
        one(
          validateFair(
            doc,
            keys: (name: mv['server'] as String, history: later, why: ''),
          ),
          'key',
        ).ok,
        isFalse,
      );
    },
  );
}
