import 'package:churr/core/forms/presets.dart';
import 'package:churr/core/models/form.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  // A Wednesday, 10:00 local time.
  final now = DateTime(2026, 11, 11, 10, 0);

  group('command text', () {
    test('splits on bars, trims, and keeps an escaped bar', () {
      expect(splitParts(r' Lunch? | Noodles \| rice | Tacos '), [
        'Lunch?',
        'Noodles | rice',
        'Tacos',
      ]);
    });

    test('a poll with flags', () {
      final f = pollForm(
        'Lunch on Friday? | Noodles | Tacos --anonymous --multiple --add --results after_close',
        now,
      );
      expect(f['preset'], 'poll');
      expect(f['title'], 'Lunch on Friday?');
      expect(f['anonymous'], true);
      expect(f['results'], 'after_close');
      expect(f['allow_added_options'], true);
      expect(f['action'], {'type': 'tally'});
      final field = (f['fields'] as List).single as Map;
      expect(field['id'], 'choice');
      expect(field['required'], true);
      expect(field['multiple'], true);
      expect(field['options'], [
        {'id': 'o1', 'label': 'Noodles'},
        {'id': 'o2', 'label': 'Tacos'},
      ]);
    });

    test('a poll closes at --until', () {
      final f = pollForm('Q? | A | B --until tomorrow at 17:00', now);
      expect(
        DateTime.parse(f['closes_at'] as String),
        DateTime(2026, 11, 12, 17).toUtc(),
      );
    });

    test('refuses before sending', () {
      expect(() => pollForm('Q? | only one', now), throwsA(isA<PresetError>()));
      expect(() => pollForm(' | A | B', now), throwsA(isA<PresetError>()));
      expect(
        () => pollForm('Q? | A | B --shout', now),
        throwsA(isA<PresetError>()),
      );
      // A flag that belongs to another command is unknown here.
      expect(
        () => signupForm('Q | A --anonymous', now),
        throwsA(isA<PresetError>()),
      );
      expect(
        () => rsvpForm('Party | yesterday', now),
        throwsA(isA<PresetError>()),
      );
      expect(
        () => rsvpForm('Party | someday soon', now),
        throwsA(isA<PresetError>()),
      );
      expect(
        () => signupForm('Shifts | Morning (0)', now),
        throwsA(isA<PresetError>()),
      );
      expect(
        () => pollForm('Q? | A | B --until 2020-01-01', now),
        throwsA(isA<PresetError>()),
      );
    });

    test('an RSVP has Going, Maybe and Can\'t and closes at its time', () {
      final f = rsvpForm('Team dinner | friday at 7pm', now);
      expect(f['results'], 'live');
      final opts = ((f['fields'] as List).single as Map)['options'] as List;
      expect(
        [for (final o in opts) (o as Map)['id']],
        ['going', 'maybe', 'cant'],
      );
      expect(
        DateTime.parse(f['closes_at'] as String),
        DateTime(2026, 11, 13, 19).toUtc(),
      );
    });

    test('a sign-up slot takes its places from brackets', () {
      final f = signupForm('Shifts | Morning (3) | Evening', now);
      final opts = ((f['fields'] as List).single as Map)['options'] as List;
      expect(opts, [
        {'id': 'o1', 'label': 'Morning', 'max': 3},
        {'id': 'o2', 'label': 'Evening'},
      ]);
    });

    test('approve has two buttons and an optional group', () {
      final f = approveForm(
        'Ship 2.0 | @design',
        now,
        groupIdOf: (h) => h == 'design' ? 'g1' : null,
      );
      expect(f['fields'], isEmpty);
      expect(
        [for (final b in f['buttons'] as List) (b as Map)['id']],
        ['approve', 'decline'],
      );
      expect(f['answerer_group_id'], 'g1');
      expect(
        () => approveForm('Ship | @nobody', now, groupIdOf: (_) => null),
        throwsA(isA<PresetError>()),
      );
    });

    test('the dialog and the command make the same poll', () {
      expect(
        buildPoll(question: 'Lunch?', options: ['A', 'B'], now: now),
        pollForm('Lunch? | A | B', now),
      );
    });
  });

  group('moments', () {
    test('the once grammar', () {
      expect(
        parseMoment('in 2h 30m', now),
        now.add(const Duration(hours: 2, minutes: 30)),
      );
      expect(parseMoment('tomorrow', now), DateTime(2026, 11, 12, 9));
      expect(
        parseMoment('friday at 14:30', now),
        DateTime(2026, 11, 13, 14, 30),
      );
      // A weekday means its next occurrence after today.
      expect(parseMoment('wed', now), DateTime(2026, 11, 18, 9));
      expect(parseMoment('2026-12-01 at 9am', now), DateTime(2026, 12, 1, 9));
      expect(parseMoment('at 8', now), DateTime(2026, 11, 12, 8));
      expect(parseMoment('at 12pm', now), DateTime(2026, 11, 11, 12));
      expect(parseMoment('next tuesday-ish', now), isNull);
      expect(parseMoment('2026-02-30', now), isNull);
    });
  });

  group('form model', () {
    Map<String, dynamic> poll({
      String? type,
      String action = 'tally',
      String? preset = 'poll',
    }) => {
      'id': 'f1',
      'preset': ?preset,
      'title': 'Lunch?',
      'fields': [
        {
          'id': 'choice',
          'type': type ?? 'choice',
          'label': 'Where',
          'options': [
            {'id': 'a', 'label': 'Noodles'},
            {'id': 'b', 'label': 'Tacos'},
          ],
        },
      ],
      'action': {'type': action},
      'answer_count': 3,
      'tally': {
        'choice': {'a': 2, 'b': 1},
      },
      'my_answer': {
        'choice': ['a'],
      },
    };

    test('reads the tally and the reader\'s answer', () {
      final f = FormModel.fromJson(poll());
      expect(f.drawable, isTrue);
      expect(f.inlineField?.id, 'choice');
      expect(f.count('choice', 'a'), 2);
      expect(f.fieldTotal('choice'), 3);
      expect(f.mine('choice'), ['a']);
      expect(f.voters, isNull);
    });

    test('an unknown field type, action or preset cannot be drawn', () {
      expect(FormModel.fromJson(poll(type: 'colour')).drawable, isFalse);
      expect(FormModel.fromJson(poll(action: 'lottery')).drawable, isFalse);
      expect(FormModel.fromJson(poll(preset: 'quiz')).drawable, isFalse);
    });

    test('values are checked as the server checks them', () {
      final text = FormFieldDef.fromJson({
        'id': 't',
        'type': 'text',
        'label': 'T',
        'required': true,
      });
      expect(checkValue(text, ''), 'Required.');
      expect(checkValue(text, 'a\nb'), isNotNull);
      final n = FormFieldDef.fromJson({
        'id': 'n',
        'type': 'number',
        'label': 'N',
        'min': 1,
        'max': 10,
        'step': 2,
      });
      expect(checkValue(n, 3), isNull);
      expect(checkValue(n, 4), isNotNull);
      expect(checkValue(n, 11), isNotNull);
      final box = FormFieldDef.fromJson({
        'id': 'c',
        'type': 'checkbox',
        'label': 'C',
        'required': true,
      });
      expect(checkValue(box, false), 'Required.');
      final date = FormFieldDef.fromJson({
        'id': 'd',
        'type': 'date',
        'label': 'D',
        'min': '2026-01-01',
      });
      expect(checkValue(date, '2025-12-31'), isNotNull);
      expect(checkValue(date, '2026-02-01'), isNull);
      final time = FormFieldDef.fromJson({
        'id': 'h',
        'type': 'time',
        'label': 'H',
      });
      expect(checkValue(time, '24:00'), isNotNull);
      expect(checkValue(time, '09:30'), isNull);
    });

    test('a built form is checked before sending', () {
      expect(
        () => checkBuiltForm({
          'fields': [],
          'action': {'type': 'collect'},
        }),
        throwsA(isA<PresetError>()),
      );
      expect(
        () => checkBuiltForm({
          'fields': [
            {'id': 'f1', 'type': 'choice', 'label': 'Pick'},
          ],
        }),
        throwsA(isA<PresetError>()),
      );
      checkBuiltForm({
        'fields': [
          {'id': 'f1', 'type': 'text', 'label': 'Name'},
        ],
      });
    });
  });
}
