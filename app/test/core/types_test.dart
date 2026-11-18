// The catalogue in the app (docs/DESIGN-MODULES.md sections 3.2, 4 and
// 9.1), as the web chat's e2e/types.mjs holds its own:
//
//   1. the chance kinds' checks, against testdata/types/<kind>.json, the
//      cases the server's and the web chat's checks pass too;
//   2. the three-step fallback: the kind, its generic rendition, the text,
//      and the state "this needs a newer app";
//   3. what moves when it arrives live;
//   4. values kept untouched through an edit the client makes itself, and
//      by a sealed chat's receiving device.
//
// How a value looks is test/ui/types_view_test.dart.

import 'dart:convert';
import 'dart:io';

import 'package:churr/core/commands/chance_live.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/core/sealed_client/content_view.dart';
import 'package:churr/core/types/catalogue.dart';
import 'package:churr/core/types/dice_roll.dart';
import 'package:churr/core/wire/schema.g.dart';
import 'package:flutter_test/flutter_test.dart';

final _cases = jsonDecode(
  File('../testdata/types/dice_roll.json').readAsStringSync(),
) as Map<String, dynamic>;

final _one =
    ((_cases['valid'] as List).first as Map)['data'] as Map<String, dynamic>;

final _roll = <String, dynamic>{
  'kind': 'dice_roll',
  'level': 1,
  'by': 'dice',
  'data': _one,
};

Message _message(Map<String, dynamic> extra) => Message.fromJson({
  'id': 'm1',
  'conversation_id': 'c1',
  'text': 'Rolled 2d6+3: [4, 1] + 3 = 8',
  'created_at': DateTime.now().toUtc().toIso8601String(),
  ...extra,
});

void main() {
  group('the other chance kinds\' checks, as the server\'s', () {
    for (final kind in [
      'coin_flip',
      'pick',
      'shuffle',
      'card_draw',
      'tarot_reading',
    ]) {
      final f = jsonDecode(
        File('../testdata/types/$kind.json').readAsStringSync(),
      ) as Map<String, dynamic>;
      test('$kind: the file is the kind\'s', () {
        expect(f['kind'], kind);
        expect(f['level'], catalogueKinds[kind]!.level);
        expect(catalogueKinds[kind]!.chance, isTrue);
      });
      for (final c in (f['valid'] as List).cast<Map>()) {
        test('$kind valid: ${c['name']}', () {
          expect(catalogueKinds[kind]!.data(c['data']), isNotNull);
        });
      }
      for (final c in (f['invalid'] as List).cast<Map>()) {
        test('$kind invalid: ${c['name']}', () {
          expect(catalogueKinds[kind]!.data(c['data']), isNull);
        });
      }
    }
  });

  group('the kind\'s check, as the server\'s', () {
    test('the file is the kind\'s, at the level the wire names', () {
      expect(_cases['kind'], 'dice_roll');
      expect(_cases['level'], catalogueKinds['dice_roll']!.level);
      expect(wireValueKinds.map((k) => (k.name, k.level)), [
        for (final e in catalogueKinds.entries) (e.key, e.value.level),
      ]);
    });
    for (final c in (_cases['valid'] as List).cast<Map>()) {
      test('valid: ${c['name']}', () {
        final got = diceRollData(c['data']);
        expect(got, isNotNull);
        if (c['draws'] != null) {
          expect(got, c['draws']);
        } else {
          expect(
            [for (final r in got!['rows'] as List) r['total']],
            [for (final r in (c['data'] as Map)['rows'] as List) r['total']],
          );
        }
      });
    }
    for (final c in (_cases['invalid'] as List).cast<Map>()) {
      test('invalid: ${c['name']}', () {
        expect(diceRollData(c['data']), isNull);
      });
    }
    test('the check changes nothing it is given', () {
      final frozen = jsonEncode(_one);
      diceRollData(_one);
      expect(jsonEncode(_one), frozen);
    });
  });

  group('the three-step fallback', () {
    String said(ValueReading r) => r.toString();
    test('this client\'s level', () => expect(catalogueLevel, 1));
    test('a kind it knows, at its level: drawn by the kind', () {
      final r = readValue(_roll);
      expect(said(r), 'kind(dice_roll, removed: false, alt: false)');
      expect(r.data, diceRollData(_one));
    });
    test('a level above its own: the text, and a newer app would draw it', () {
      expect(said(readValue({..._roll, 'level': 2})), 'text(newer: true)');
    });
    test('a kind it does not know: the same', () {
      expect(
        said(
          readValue({
            'kind': 'tarot_spread',
            'level': 4,
            'data': {
              'cards': [1],
            },
          }),
        ),
        'text(newer: true)',
      );
    });
    test(
      'a kind it does not know, with a rendition it does: the rendition',
      () {
        final r = readValue({
          'kind': 'tarot_spread',
          'level': 4,
          'data': {},
          'alt': _roll,
        });
        expect(said(r), 'kind(dice_roll, removed: false, alt: true)');
      },
    );
    test('a level above its own, with a rendition: the rendition', () {
      expect(readValue({..._roll, 'level': 9, 'alt': _roll}).drawn, isTrue);
    });
    test('a rendition it cannot draw either: the text', () {
      expect(
        said(
          readValue({
            'kind': 'later',
            'level': 1,
            'alt': {'kind': 'later_still', 'level': 1},
          }),
        ),
        'text(newer: true)',
      );
    });
    test('data that does not hold together: the text, no newer app', () {
      expect(
        said(
          readValue({
            ..._roll,
            'data': {'dice': '2d6', 'rows': []},
          }),
        ),
        'text(newer: false)',
      );
    });
    test('data still in bytes (a server that did not know the kind)', () {
      expect(
        said(readValue({..._roll, 'data': 'GgUyZDYrMw'})),
        'text(newer: false)',
      );
    });
    test('dice nothing was rolled for: the kind, removed', () {
      final r = readValue({
        'kind': 'dice_roll',
        'level': 1,
        'by': 'dice',
        'removed': true,
      });
      expect(said(r), 'kind(dice_roll, removed: true, alt: false)');
      expect(r.data, isNull);
    });
    test('no level: not trusted, the text', () {
      expect(readValue({'kind': 'dice_roll', 'data': _one}).drawn, isFalse);
    });
    test('not a value at all', () {
      for (final v in [
        null,
        'x',
        [],
        {'level': 1},
      ]) {
        expect(said(readValue(v)), 'text(newer: false)', reason: '$v');
      }
    });
    test('renditions are followed only so far', () {
      Map<String, dynamic> deep = _roll;
      for (var i = 0; i < 6; i++) {
        deep = {'kind': 'later$i', 'level': 1, 'alt': deep};
      }
      expect(readValue(deep).drawn, isFalse);
    });
    test('a kind named like an object\'s own field is no kind', () {
      expect(
        said(readValue({'kind': 'constructor', 'level': 1, 'data': {}})),
        'text(newer: true)',
      );
    });
  });

  group('what moves when it arrives live', () {
    Entity inline(Map<String, dynamic> object, [int at = 0]) => Entity.fromJson(
      {'type': 'object', 'at': at, 'len': 9, 'object': object},
    );
    test('a roll as a message\'s view spins', () {
      expect(spins(_message({'view': _roll})), isTrue);
    });
    test('inline dice spin', () {
      expect(objectSpins(inline(_roll)), isTrue);
    });
    test('dice not rolled do not', () {
      expect(objectSpins(inline({..._roll, 'removed': true})), isFalse);
    });
    test('a kind this client does not know does not', () {
      expect(
        spins(
          _message({
            'view': {'kind': 'later', 'level': 1},
          }),
        ),
        isFalse,
      );
    });
    test('arrived live, the view and each roll in the text are noted', () {
      final m = _message({
        'view': _roll,
        'entities': [
          {'type': 'object', 'at': 4, 'len': 9, 'object': _roll},
          {
            'type': 'object',
            'at': 20,
            'len': 2,
            'object': {'kind': 'parcel', 'level': 7},
          },
        ],
      });
      ChanceLive.arrivedMessage(m);
      expect(ChanceLive.spinAt('m1'), Duration.zero);
      expect(ChanceLive.spinAt(ChanceLive.objectId('m1', 4)), Duration.zero);
      expect(ChanceLive.spinAt(ChanceLive.objectId('m1', 20)), isNull);
    });
  });

  group('values kept through an edit the client makes itself', () {
    final later = {
      'kind': 'parcel',
      'level': 7,
      'by': 'parcels',
      'data': {
        'id': 'X1',
        'extra': [
          1,
          {'deep': true},
        ],
      },
      'source': 'parcels/parcel',
    };
    const oldText = 'track X1 and X2 [[1d6]]';
    final oldEnts = <Map<String, dynamic>>[
      {'type': 'object', 'at': 6, 'len': 2, 'object': later},
      {'type': 'object', 'at': 16, 'len': 7, 'object': _roll},
      {'type': 'task', 'at': 0, 'len': 1, 'item_id': 't1'},
    ];
    test('kept where its text now stands, the same value', () {
      final kept = keepValues(
        oldText,
        oldEnts,
        'please track X1 and X2 [[1d6]]',
        [
          {'type': 'instant', 'at': 0, 'len': 6},
        ],
      );
      expect(
        [for (final e in kept) (e['type'], e['at'], e['len'])],
        [('instant', 0, 6), ('object', 13, 2), ('object', 23, 7)],
      );
      expect(identical(kept[1]['object'], later), isTrue);
      expect(identical(kept[2]['object'], _roll), isTrue);
      expect(jsonEncode(kept[1]['object']), jsonEncode(later));
    });
    test('a value whose text is gone goes with it', () {
      final kept = keepValues(oldText, oldEnts, 'track X3 [[1d6]]', []);
      expect([for (final e in kept) e['at']], [9]);
    });
    test('the new text\'s own entities come first', () {
      final kept = keepValues(oldText, oldEnts, 'X1', [
        {'type': 'reference', 'at': 0, 'len': 2},
      ]);
      expect([for (final e in kept) e['type']], ['reference']);
    });
    test('nothing to keep', () {
      expect(keepValues('', const [], 'x', const []), isEmpty);
    });
    test('a sealed chat\'s receiving device keeps the sender\'s values', () {
      final got = workOutEntities(
        oldText,
        const {},
        sent: [
          ...oldEnts,
          {'type': 'object', 'at': 20, 'len': 9, 'object': later},
          {'type': 'object', 'at': 7, 'len': 3, 'object': later},
        ],
      );
      final values = [
        for (final e in got)
          if (e['type'] == 'object') e,
      ];
      // The one past the end of the text and the one over another go.
      expect([for (final e in values) e['at']], [6, 16]);
      expect(identical(values[0]['object'], later), isTrue);
    });
  });
}
