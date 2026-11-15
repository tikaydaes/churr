import 'package:churr/core/db/cache_db.dart';
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
}
