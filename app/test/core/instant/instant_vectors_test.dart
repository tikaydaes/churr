// Instants (ruling R69) in the app: the shared resolution and phrase
// vectors (testdata/instants), and the composer's helpers.

import 'dart:convert';
import 'dart:io';

import 'package:churr/core/instant/compose.dart';
import 'package:churr/core/instant/phrases.dart';
import 'package:churr/core/instant/zone.dart';
import 'package:churr/core/markdown/parser.dart';
import 'package:flutter_test/flutter_test.dart';

Map<String, dynamic> _data(String name) =>
    jsonDecode(File('../testdata/instants/$name').readAsStringSync())
        as Map<String, dynamic>;

void main() {
  group('resolve.json', () {
    final v = _data('resolve.json');
    final cases = v['cases'] as List;
    test('holds the cases', () => expect(cases.length, greaterThan(30)));
    for (final c in cases.cast<Map<String, dynamic>>()) {
      test(c['name'], () {
        final got = resolveInstant(
          c['local'] as String,
          c['zone'] as String,
          c['author_zone'] as String,
        );
        expect(got?.toJson(), c['want']);
      });
    }
  });

  group('phrases.json', () {
    final v = _data('phrases.json');
    final cases = v['cases'] as List;
    test('holds the cases', () => expect(cases.length, greaterThan(40)));
    for (final c in cases.cast<Map<String, dynamic>>()) {
      test(c['name'], () {
        final got = findPhrases(
          c['text'] as String,
          now: DateTime.parse(c['now'] as String),
          zone: c['zone'] as String,
          lang: v['lang'] as String,
        );
        expect([for (final p in got) p.toJson()], c['want']);
      });
    }
  });

  group('the composer', () {
    test('sealed entities: in the author\'s zone, not in a checklist', () {
      expect(
        instantEntities(
          'at 2026-12-24 18:00\n- [ ] 2026-12-24 19:00Z',
          'Australia/Brisbane',
        ),
        [
          {
            'type': 'instant',
            'at': 3,
            'len': 16,
            'time': '2026-12-24T08:00:00.000Z',
            'precision': 'minute',
            'zone': 'Australia/Brisbane',
          },
        ],
      );
      expect(instantEntities('at 2026-12-24 18:00', null), isEmpty);
    });
    test('where a hint fits', () {
      expect(hintFits('see you tomorrow at 5pm', 8, 23), isTrue);
      expect(hintFits('`tomorrow at 5pm`', 1, 16), isFalse);
      expect(hintFits('**tomorrow** at 5pm', 0, 19), isFalse);
      expect(hintFits('2026-12-24 18:00Z', 0, 17), isFalse);
    });
    test('a hint follows its words, and an edit drops it', () {
      const h = InstantHint(
        start: 8,
        end: 23,
        typed: 'tomorrow at 5pm',
        time: 't',
        precision: 'minute',
        zone: 'UTC',
      );
      expect(
        [
          for (final x in trackHints([h], 'well, see you tomorrow at 5pm'))
            (x.start, x.end),
        ],
        [(14, 29)],
      );
      expect(trackHints([h], 'see you tomorrow at 6pm'), isEmpty);
    });
    test(
      'keep as text escapes the date and the grammar no longer reads it',
      () {
        final plain = instantAsPlain('Party 2026-12-24 18:00 Europe/Berlin', 6);
        expect(plain, r'Party 2026\-12-24 18:00 Europe/Berlin');
        expect(
          jsonEncode(parseMarkdown(plain).toTree()).contains('"instant"'),
          isFalse,
        );
      },
    );
  });
}
