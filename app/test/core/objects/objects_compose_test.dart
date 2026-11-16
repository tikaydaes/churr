// Objects in the app's composer (ruling R73), without widgets: which token
// is offered at the caret, its rows, decisions followed as the text
// changes, the hints sent, what each key does for class A and class B, and
// a sealed chat's entities. The grammar itself is in the shared corpus
// (markdown_corpus_test.dart).

import 'package:churr/core/instant/zone.dart';
import 'package:churr/core/markdown/objects.dart';
import 'package:churr/core/objects/compose.dart';
import 'package:churr/core/sealed_client/content_view.dart';
import 'package:flutter_test/flutter_test.dart';

final patterns = <Map<String, dynamic>>[
  {
    'id': 'p1',
    'name': 'Tracker',
    'prefix': 'PROJ-',
    'url_template': 'https://t.example/{value}',
    'created_at': '2026-11-16T00:00:00.000Z',
  },
];

ObjectCandidate? at(
  String text,
  int caret, [
  List<ObjectDecision> d = const [],
]) => objectCandidate(
  text,
  caret,
  patterns: patterns,
  zone: 'Australia/Brisbane',
  decided: d,
);

void main() {
  group('each kind after references, offered as typed', () {
    ObjectCandidate? kind(String text, {String origin = ''}) =>
        objectCandidate(text, text.length, origin: origin, zone: 'UTC');

    test('a pasted message link', () {
      const link =
          'https://chat.example/chat/c/0193a000-0000-7000-8000-0000000000a1/m/0193a000-0000-7000-8000-0000000000b1';
      final c = kind('see $link', origin: 'https://chat.example')!;
      expect(c.rows.map((r) => r.kind), ['message_link', objectPlain]);
      expect(
        c.rows.first.entity!['message_id'],
        '0193a000-0000-7000-8000-0000000000b1',
      );
      // No origin (a sealed chat): no message link.
      expect(kind('see $link'), isNull);
    });

    test('a measurement', () {
      final c = kind('a 5 km')!;
      expect(c.rows.map((r) => r.kind), ['measure', objectPlain]);
      expect(c.rows.first.entity!['unit'], 'km');
      expect(kind('5 k'), isNull);
    });

    test('a colour, and a reference before it where a pattern reads', () {
      final c = kind('logo #fff')!;
      expect(c.rows.map((r) => r.kind), ['colour', objectPlain]);
      expect(c.rows.first.entity!['value'], '#ffffff');
      final both = objectCandidate(
        '#123456',
        7,
        patterns: [
          {
            'id': 'p2',
            'prefix': '#',
            'url_template': 'https://g.example/{value}',
            'created_at': '2026-11-16T00:00:00.000Z',
          },
        ],
      )!;
      expect(both.rows.map((r) => r.kind), [
        'reference',
        'colour',
        objectPlain,
      ]);
      expect(objectHintsOf([chooseObject(both, 1)]), [
        {'start': 0, 'end': 7, 'kind': 'colour'},
      ]);
    });
  });

  test('a complete reference at the caret is offered, then plain text', () {
    final c = at('see PROJ-12', 11)!;
    expect((c.start, c.end, c.typed), (4, 11, 'PROJ-12'));
    expect(c.rows.map((r) => r.kind), ['reference', objectPlain]);
    expect(c.rows.first.entity!['url'], 'https://t.example/12');
  });

  test(
    'nothing is offered before the caret, incomplete, in a word or in code',
    () {
      expect(at('see PROJ-12 now', 15), isNull);
      expect(at('see PROJ-', 9), isNull);
      expect(at('xPROJ-12', 8), isNull);
      expect(at('`PROJ-12`', 9), isNull);
    },
  );

  test('a typed date and time is offered, read in the author zone', () {
    final c = at('at 2026-12-24 18:00', 19)!;
    expect(c.rows.map((r) => r.kind), ['instant', objectPlain]);
    expect(c.rows.first.entity!['time'], '2026-12-24T08:00:00.000Z');
  });

  test('decisions: sent as hints, followed, dropped when edited, undone', () {
    final c = at('PROJ-12', 7)!;
    expect(objectHintsOf([chooseObject(c, 0)]), isEmpty);
    expect(at('PROJ-12', 7, [chooseObject(c, 0)]), isNull);
    var d = [chooseObject(c, 1)];
    expect(objectHintsOf(d), [
      {'start': 0, 'end': 7, 'kind': 'plain'},
    ]);
    d = trackObjects(d, 'Re: PROJ-12 ');
    expect(objectHintsOf(d).single['start'], 4);
    expect(
      findObjects('Re: PROJ-12 ', patterns: patterns, hints: objectHintsOf(d)),
      isEmpty,
    );
    expect(trackObjects(d, 'Re: PROJ-123 '), isEmpty);
    expect(
      at('Re: PROJ-123', 12, trackObjects(d, 'Re: PROJ-123'))!.typed,
      'PROJ-123',
    );
    final undone = undoObject([chooseObject(at('PROJ-1', 6)!, 0)], 0, 6);
    expect(objectHintsOf(undone), [
      {'start': 0, 'end': 6, 'kind': 'plain'},
    ]);
  });

  test(
    'class A keys: arrows, finishing the word, Enter, Escape, Backspace',
    () {
      ObjectKeyAction a(
        String k, {
        bool open = true,
        bool after = false,
        bool shift = false,
      }) =>
          objectKeyAction('A', k, open: open, afterAccept: after, shift: shift);
      expect(a('ArrowDown'), ObjectKeyAction.down);
      expect(a('ArrowUp'), ObjectKeyAction.up);
      for (final k in [' ', '.', ',', '!', ')']) {
        expect(a(k), ObjectKeyAction.commit, reason: k);
      }
      for (final k in ['a', '7', 'é']) {
        expect(a(k), ObjectKeyAction.none, reason: k);
      }
      expect(a('Enter'), ObjectKeyAction.commitSend);
      expect(a('Enter', shift: true), ObjectKeyAction.commit);
      expect(a('Escape'), ObjectKeyAction.plain);
      expect(a('Tab'), ObjectKeyAction.accept);
      expect(a('Backspace', open: false, after: true), ObjectKeyAction.undo);
      expect(a('Backspace', open: false), ObjectKeyAction.none);
    },
  );

  test('class B keys: never by itself', () {
    ObjectKeyAction b(String k) => objectKeyAction('B', k);
    expect(b('Tab'), ObjectKeyAction.accept);
    expect(b('Enter'), ObjectKeyAction.accept);
    expect(b(' '), ObjectKeyAction.none);
    expect(b('Escape'), ObjectKeyAction.dismiss);
    expect(b('ArrowUp'), ObjectKeyAction.up);
  });

  test('a sealed chat: the sender finds references and honours plain text', () {
    final e = workOutEntities(
      'fix PROJ-3 at 2026-12-24 18:00Z',
      const {},
      patterns: patterns,
    );
    expect(e.map((x) => x['type']), ['reference', 'instant']);
    final plain = workOutEntities(
      'fix PROJ-3 at 2026-12-24 18:00Z',
      const {},
      patterns: patterns,
      objectHints: [
        {'start': 4, 'end': 10, 'kind': 'plain'},
        {'start': 14, 'end': 31, 'kind': 'plain'},
      ],
    );
    expect(plain, isEmpty);
    expect(instantEntities('2026-12-24 18:00Z', null), hasLength(1));
  });

  test('a sealed chat: the receiver keeps references that hold up', () {
    const text = 'fix PROJ-3 now';
    final got = workOutEntities(
      text,
      const {},
      sent: [
        {
          'type': 'reference',
          'at': 4,
          'len': 6,
          'url': 'https://t.example/3',
          'value': '3',
          'pattern_id': 'p1',
        },
        {
          'type': 'reference',
          'at': 0,
          'len': 3,
          'url': 'javascript:alert(1)',
          'value': 'fix',
        },
        {
          'type': 'reference',
          'at': 11,
          'len': 3,
          'url': 'https://x.example/',
          'value': 'zzz',
        },
        {
          'type': 'reference',
          'at': 12,
          'len': 9,
          'url': 'https://x.example/',
          'value': 'w',
        },
      ],
    );
    expect(got, [
      {
        'type': 'reference',
        'at': 4,
        'len': 6,
        'url': 'https://t.example/3',
        'pattern_id': 'p1',
        'value': '3',
      },
    ]);
  });
}
