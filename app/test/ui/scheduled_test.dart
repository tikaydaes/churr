import 'dart:convert';

import 'package:churr/ui/screens/account/scheduled_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../account_support.dart';

// Scheduled messages and reminders (docs/CLIENTS.md 9.16).

const _message = {
  'id': 's1',
  'kind': 'message',
  'conversation_id': 'general',
  'account_id': 'me',
  'content': {'text': 'Morning all'},
  'next_run_at': '2026-11-18T09:00:00.000Z',
  'created_at': '2026-11-14T09:00:00.000Z',
};

/// The JSON body of the last [route] the fake server was sent.
Map<String, dynamic>? sentBody(FakeServer f, String route) {
  final r = f.requests.lastWhere((r) => '${r.method} ${r.url.path}' == route);
  return jsonDecode(r.body) as Map<String, dynamic>;
}

void main() {
  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    group('at $label size', () {
      testWidgets('a scheduled message: send now, edit, cancel', (
        tester,
      ) async {
        await sized(tester, size);
        final (app, fakes) = await makeAccountApp(tester);
        fakes[0].on('GET /api/v1/scheduled', {
          'scheduled': [
            _message,
            // Watches have their own screen.
            {'id': 'w1', 'kind': 'watch', 'watch_account_id': 'sam'},
          ],
        });
        fakes[0].on('POST /api/v1/scheduled/s1/send', {});
        fakes[0].on('PATCH /api/v1/scheduled/s1', {'scheduled': _message});
        fakes[0].on('DELETE /api/v1/scheduled/s1', {});
        await tester.pumpWidget(
          harness(app, Scaffold(body: ScheduledScreen(server: app.servers[0]))),
        );
        await settleAll(tester);
        expect(find.text('Morning all'), findsOneWidget);
        expect(find.byKey(const Key('scheduled-w1')), findsNothing);

        await tester.tap(find.byKey(const Key('send-s1')));
        await settleAll(tester);
        expect(fakes[0].calls, contains('POST /api/v1/scheduled/s1/send'));

        await tester.tap(find.byKey(const Key('edit-s1')));
        await settleAll(tester);
        await tester.enterText(
          find.byKey(const Key('scheduled-text')),
          'Morning, everyone',
        );
        await tester.tap(find.byKey(const Key('scheduled-save')));
        await settleAll(tester);
        expect(fakes[0].calls, contains('PATCH /api/v1/scheduled/s1'));
        final body = sentBody(fakes[0], 'PATCH /api/v1/scheduled/s1');
        expect(body?['content'], {'text': 'Morning, everyone'});
        expect(body?['run_at'], '2026-11-18T09:00:00.000Z');

        await tester.tap(find.byKey(const Key('cancel-s1')));
        await settleAll(tester);
        await tester.tap(find.byKey(const Key('confirm')));
        await settleAll(tester);
        expect(fakes[0].calls, contains('DELETE /api/v1/scheduled/s1'));
        expect(tester.takeException(), isNull);
      });

      testWidgets('nothing waiting is said', (tester) async {
        await sized(tester, size);
        final (app, fakes) = await makeAccountApp(tester);
        fakes[0].on('GET /api/v1/scheduled', {'scheduled': []});
        await tester.pumpWidget(
          harness(app, Scaffold(body: ScheduledScreen(server: app.servers[0]))),
        );
        await settleAll(tester);
        expect(find.textContaining('Nothing is waiting'), findsOneWidget);
      });
    });
  }
}
