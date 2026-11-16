import 'package:churr/core/commands/chance_live.dart';
import 'package:churr/core/db/cache_db.dart';
import 'package:churr/state/app_state.dart';
import 'package:churr/ui/message/chance_view.dart';
import 'package:churr/ui/screens/conversation/chat_commands.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../conversation_support.dart';

/// The built-in commands of ruling R67 typed in the conversation screen:
/// /roll is drawn by the server and shown as a result, a mistyped command
/// is refused before anything is sent, and the server's refusal is shown in
/// its words.
void main() {
  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets(
      '/roll posts a chance request and draws the server\'s result ($label)',
      (tester) async {
        await setSize(tester, size);
        registerChatCommands();
        late CacheDb db;
        late FakeServer fake;
        await tester.runAsync(() async {
          db = await conversationCache();
          fake = FakeServer();
        });
        fake.on(
          'POST /api/v1/conversations/$general/messages',
          (r) => {
            'message': {
              'id': 'rolled',
              'conversation_id': general,
              'seq': 90,
              'order': '0000000000000000!!000000000000005a',
              'author_id': 'me',
              'created_at': DateTime.now().toUtc().toIso8601String(),
              'fallback': 'Rolled 2d6+3: [4, 2] + 3 = 9',
              'chance': {
                'kind': 'roll',
                'dice': '2d6+3',
                'terms': [
                  {
                    'sign': 1,
                    'count': 2,
                    'sides': 6,
                    'rolls': [4, 2],
                    'value': 6,
                  },
                  {'sign': 1, 'value': 3},
                ],
                'total': 9,
              },
            },
          },
        );
        final app = await conversationApp(tester, db, fake);
        await pumpConversation(tester, app);
        final box = find.byKey(const Key('composer'));

        await tester.enterText(box, '/kick');
        await settle(tester, 2);
        await tester.tap(find.byKey(const Key('send')));
        await settle(tester);
        expect(
          find.text('/kick needs a person: type @ and pick one.'),
          findsOneWidget,
        );
        expect(fake.where('POST', '/messages'), isEmpty);
        ScaffoldMessenger.of(tester.element(box)).removeCurrentSnackBar();
        await settle(tester, 2);

        await tester.enterText(box, '/roll 2d6+3');
        await settle(tester, 2);
        await tester.tap(find.byKey(const Key('send')));
        await settle(tester, 10);
        expect(fake.where('POST', '/messages').single.body, {
          'client_id': isA<String>(),
          'chance': {'kind': 'roll', 'dice': '2d6+3'},
        });
        expect(tester.widget<TextField>(box).controller!.text, isEmpty);
        await tester.pump(const Duration(seconds: 2));
        expect(find.byType(ChanceView), findsOneWidget);
        expect(find.text('= 9'), findsOneWidget);
        expect(tester.state<ChanceViewState>(find.byType(ChanceView)).faces, [
          '4',
          '2',
          '3',
        ]);
        expect(tester.takeException(), isNull);
      },
    );
  }

  // The sender's own result spins once as it first appears, whether the
  // server's answer to posting it or the live event comes first, and is
  // never spun again by the other.
  for (final (order, eventFirst, changed) in [
    ('answer first', false, false),
    ('event first', true, false),
    ('event first, inside conversation.changed', true, true),
  ]) {
    testWidgets('the sender sees their own /roll spin once ($order)', (
      tester,
    ) async {
      await setSize(tester, desktop);
      registerChatCommands();
      late CacheDb db;
      late FakeServer fake;
      await tester.runAsync(() async {
        db = await conversationCache();
        fake = FakeServer();
      });
      final id = 'rolled-$eventFirst-$changed';
      final message = {
        'id': id,
        'conversation_id': general,
        'seq': 31,
        'order': '0000000000000000!!000000000000001f',
        'author_id': 'me',
        'created_at': DateTime.now().toUtc().toIso8601String(),
        'fallback': 'Rolled 1d6: [5] = 5',
        'chance': {
          'kind': 'roll',
          'dice': '1d6',
          'terms': [
            {
              'sign': 1,
              'count': 1,
              'sides': 6,
              'rolls': [5],
              'value': 5,
            },
          ],
          'total': 5,
        },
      };
      late AppState app;
      final created = {
        'type': 'message.created',
        'conversation_id': general,
        'seq': 31,
        'client_id': 'c',
        'message': message,
      };
      // A server that delivers lazily may carry the author's own message
      // back inside `conversation.changed`.
      Future<void> event() => serverOf(app).sync.applyEvent(
        changed
            ? {
                'type': 'conversation.changed',
                'conversation_id': general,
                'last_seq': 31,
                'event': created,
              }
            : created,
      );
      fake.on('POST /api/v1/conversations/$general/messages', (r) async {
        if (eventFirst) await event();
        return {'message': message};
      });
      app = await conversationApp(tester, db, fake);
      await pumpConversation(tester, app);
      await tester.enterText(find.byKey(const Key('composer')), '/roll 1d6');
      await settle(tester, 2);
      await tester.tap(find.byKey(const Key('send')));
      // Watch the result from the moment it is drawn.
      var starts = 0;
      var was = false;
      Future<void> watch(int frames) async {
        for (var i = 0; i < frames; i++) {
          await tester.runAsync(
            () => Future<void>.delayed(const Duration(milliseconds: 5)),
          );
          await tester.pump(const Duration(milliseconds: 40));
          final f = find.byType(ChanceView);
          final now =
              f.evaluate().isNotEmpty &&
              tester.state<ChanceViewState>(f).spinning;
          if (now && !was) starts++;
          was = now;
        }
      }

      await watch(20);
      expect(find.byType(ChanceView), findsOneWidget);
      expect(starts, 1, reason: 'the reels turned as it first appeared');
      if (!eventFirst) {
        await tester.runAsync(event);
      }
      await watch(60);
      expect(starts, 1, reason: 'nothing turned them again');
      expect(ChanceLive.spinAt(id), isNull);
      // Drawn again from scratch, as on opening the conversation again,
      // it stays still.
      await tester.pumpWidget(const SizedBox());
      was = false;
      await pumpConversation(tester, app);
      await watch(20);
      expect(find.byType(ChanceView), findsOneWidget);
      expect(starts, 1, reason: 'drawn again, it did not turn');
      expect(tester.state<ChanceViewState>(find.byType(ChanceView)).faces, [
        '5',
      ]);
      expect(tester.takeException(), isNull);
    });
  }
}
