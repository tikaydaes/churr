import 'dart:convert';

import 'package:churr/core/forms/checkin.dart';
import 'package:churr/ui/screens/channels/checkins_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../account_support.dart';

// Recurring check-ins set up by a channel's managers (docs/FEATURES.md
// section 11). The web chat's e2e/checkin.mjs checks the same rules.

void main() {
  group('the setup', () {
    test('what is sent', () {
      final v = CheckinSetup.blank('Europe/Dublin')
        ..title = ' Stand-up '
        ..text = 'Morning!'
        ..questions = [
          CheckinQuestion('Yesterday', required: true),
          CheckinQuestion('  '),
          CheckinQuestion('Today', long: true),
        ];
      expect(v.problem, isNull);
      expect(v.createBody('c1'), {
        'conversation_id': 'c1',
        'kind': 'form',
        'content': {
          'text': 'Morning!',
          'form': {
            'preset': 'checkin',
            'title': 'Stand-up',
            'action': {'type': 'thread'},
            'one_answer': true,
            'fields': [
              {
                'id': 'q1',
                'type': 'text',
                'label': 'Yesterday',
                'required': true,
              },
              {'id': 'q2', 'type': 'longtext', 'label': 'Today'},
            ],
          },
        },
        'repeat': {
          'every': 'day',
          'days': '1111100',
          'at': '09:00',
          'time_zone': 'Europe/Dublin',
        },
      });
      final back = CheckinSetup.fromItem(v.content, v.repeat);
      expect(back.title, 'Stand-up');
      expect(back.questions.map((q) => q.label), ['Yesterday', 'Today']);
      expect(back.questions[1].long, isTrue);
    });

    test('what is refused', () {
      CheckinSetup ok() => CheckinSetup.blank('UTC');
      expect((ok()..title = ' ').problem, 'Give it a title.');
      expect(
        (ok()..questions = [CheckinQuestion('')]).problem,
        'Ask at least one question.',
      );
      expect((ok()..days = '0000000').problem, 'Choose at least one day.');
      expect((ok()..at = '9am').problem, 'Give the time as HH:MM.');
      expect(
        (ok()
              ..questions = [
                for (var i = 0; i < 21; i++) CheckinQuestion('q$i'),
              ])
            .problem,
        'At most 20 questions.',
      );
    });

    test('when it runs, in words', () {
      Map<String, dynamic> r(String days) => {
        'every': 'day',
        'days': days,
        'at': '08:00',
        'time_zone': 'UTC',
      };
      expect(repeatWords(r('1111111')), 'Every day at 08:00 (UTC)');
      expect(repeatWords(r('1111100')), 'Weekdays at 08:00 (UTC)');
      expect(repeatWords(r('1001000')), 'Monday, Thursday at 08:00 (UTC)');
    });
  });

  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets('at $label size: set one up, see it listed, stop it', (
      tester,
    ) async {
      await sized(tester, size);
      final (app, fakes) = await makeAccountApp(tester);
      final s = app.servers[0];
      final stored = {
        'id': 's1',
        'kind': 'form',
        'account_id': 'me',
        'conversation_id': 'general',
        'content': {
          'form': {
            'preset': 'checkin',
            'title': 'Stand-up',
            'fields': [
              {'id': 'q1', 'type': 'text', 'label': 'Yesterday'},
            ],
          },
        },
        'repeat': {
          'every': 'day',
          'days': '1111100',
          'at': '09:30',
          'time_zone': 'Europe/Dublin',
        },
        'next_run_at': '2026-11-18T09:30:00.000Z',
      };
      fakes[0].on('GET /api/v1/scheduled', {
        'scheduled': [stored],
      });
      fakes[0].on('POST /api/v1/scheduled', {'scheduled': stored}, status: 201);
      await tester.pumpWidget(
        harness(
          app,
          Scaffold(
            body: CheckinsScreen(
              server: s,
              conversationId: 'general',
              zone: 'Europe/Dublin',
            ),
          ),
        ),
      );
      await settleAll(tester);
      expect(find.text('Stand-up'), findsOneWidget);
      expect(find.textContaining('Weekdays at 09:30'), findsOneWidget);
      final listed = fakes[0].requests.firstWhere(
        (r) => r.url.path == '/api/v1/scheduled',
      );
      expect(listed.url.queryParameters['conversation_id'], 'general');
      expect(listed.url.queryParameters['kind'], 'form');

      await tester.tap(find.byKey(const Key('new-checkin')));
      await settleAll(tester);
      await tester.enterText(find.byKey(const Key('checkin-title')), 'Retro');
      await tester.ensureVisible(find.byKey(const Key('checkin-day-5')));
      await tester.tap(find.byKey(const Key('checkin-day-5')));
      await tester.pump();
      await tester.ensureVisible(find.byKey(const Key('checkin-save')));
      await tester.tap(find.byKey(const Key('checkin-save')));
      await settleAll(tester);
      final made = fakes[0].requests.lastWhere(
        (r) => r.method == 'POST' && r.url.path == '/api/v1/scheduled',
      );
      final body = jsonDecode(made.body) as Map<String, dynamic>;
      expect(body['kind'], 'form');
      expect(body['conversation_id'], 'general');
      expect((body['content'] as Map)['form']['title'], 'Retro');
      expect((body['repeat'] as Map)['days'], '1111110');
      expect((body['repeat'] as Map)['time_zone'], 'Europe/Dublin');

      await tester.tap(find.byKey(const Key('stop-checkin-s1')));
      await settleAll(tester);
      await tester.tap(find.byKey(const Key('confirm')));
      await settleAll(tester);
      expect(fakes[0].calls, contains('DELETE /api/v1/scheduled/s1'));
      expect(tester.takeException(), isNull);
    });
  }
}
