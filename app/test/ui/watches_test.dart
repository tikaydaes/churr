import 'package:churr/core/models/account.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/l10n/app_localizations_en.dart';
import 'package:churr/state/message_actions.dart';
import 'package:churr/ui/screens/account/person_profile.dart';
import 'package:churr/ui/screens/account/watches_screen.dart';
import 'package:churr/ui/screens/conversation/message_menu.dart';
import 'package:churr/core/db/conversation_queries.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../account_support.dart';

// Watches: "tell me when Ada is around" (ruling R76, docs/CLIENTS.md 9.11).

Account sam() => Account.fromJson({
  'id': 'sam',
  'username': 'sam',
  'display_name': 'Sam Okafor',
  'role': 'member',
  'presence': 'offline',
});

void main() {
  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    group('at $label size', () {
      testWidgets('the profile offers the watch, then stopping it', (
        tester,
      ) async {
        await sized(tester, size);
        final (app, fakes) = await makeAccountApp(tester);
        await tester.runAsync(() => app.servers[0].db.upsertAccounts([sam()]));
        fakes[0].on('GET /api/v1/scheduled', {'scheduled': []});
        fakes[0].on('PUT /api/v1/me/watches/sam', {
          'around': false,
          'scheduled': {
            'id': 'w1',
            'kind': 'watch',
            'watch_account_id': 'sam',
            'next_run_at': '2026-11-18T09:00:00.000Z',
          },
        }, status: 201);
        await tester.pumpWidget(
          harness(app, PersonProfile(server: app.servers[0], accountId: 'sam')),
        );
        await settleAll(tester);
        final toggle = find.byKey(const Key('watch-toggle'));
        await tester.ensureVisible(toggle);
        expect(find.text('Tell me when Sam Okafor is around'), findsOneWidget);
        await tester.tap(toggle);
        await settleAll(tester);
        expect(fakes[0].calls, contains('PUT /api/v1/me/watches/sam'));
        expect(find.text('Stop watching for Sam Okafor'), findsOneWidget);
        await tester.tap(toggle);
        await settleAll(tester);
        expect(fakes[0].calls, contains('DELETE /api/v1/me/watches/sam'));
        expect(find.text('Tell me when Sam Okafor is around'), findsOneWidget);
        expect(tester.takeException(), isNull);
      });

      testWidgets('someone around already: said, and nothing to stop', (
        tester,
      ) async {
        await sized(tester, size);
        final (app, fakes) = await makeAccountApp(tester);
        await tester.runAsync(() => app.servers[0].db.upsertAccounts([sam()]));
        fakes[0].on('GET /api/v1/scheduled', {'scheduled': []});
        fakes[0].on('PUT /api/v1/me/watches/sam', {'around': true});
        await tester.pumpWidget(
          harness(
            app,
            Scaffold(
              body: PersonProfile(server: app.servers[0], accountId: 'sam'),
            ),
          ),
        );
        await settleAll(tester);
        final toggle = find.byKey(const Key('watch-toggle'));
        await tester.ensureVisible(toggle);
        await tester.tap(toggle);
        await tester.pump();
        expect(find.text('Sam Okafor is around now.'), findsOneWidget);
        expect(find.text('Tell me when Sam Okafor is around'), findsOneWidget);
        await settleAll(tester);
      });

      testWidgets('the list of watches, and stopping one', (tester) async {
        await sized(tester, size);
        final (app, fakes) = await makeAccountApp(tester);
        await tester.runAsync(() => app.servers[0].db.upsertAccounts([sam()]));
        fakes[0].on('GET /api/v1/scheduled', {
          'scheduled': [
            {
              'id': 'w1',
              'kind': 'watch',
              'watch_account_id': 'sam',
              'next_run_at': '2026-11-18T09:00:00.000Z',
            },
            // A watch that has fired is not waiting, and not listed.
            {'id': 'w0', 'kind': 'watch', 'watch_account_id': 'sam'},
          ],
        });
        await tester.pumpWidget(
          harness(app, WatchesScreen(server: app.servers[0])),
        );
        await settleAll(tester);
        expect(find.byKey(const Key('watch-sam')), findsOneWidget);
        expect(find.textContaining('until'), findsOneWidget);
        await tester.tap(find.text('Stop watching'));
        await settleAll(tester);
        expect(fakes[0].calls, contains('DELETE /api/v1/scheduled/w1'));
        expect(tester.takeException(), isNull);
      });
    });
  }

  test(
    'a message by another person offers the watch; one of mine does not',
    () async {
      final (app, _) = await makeAccountApp(null);
      final s = app.servers[0];
      List<MsgAction> actionsFor(String author, {bool person = true}) =>
          availableActions(
            m: Message.fromJson({
              'id': 'm1',
              'conversation_id': 'general',
              'seq': 3,
              'author_id': author,
              'created_at': '2026-11-14T09:00:00.000Z',
              'text': 'hello',
            }),
            c: s.conversation('general'),
            myId: 'me',
            actions: MessageActions(s),
            limits: const ServerLimits(),
            threadState: null,
            bookmarked: false,
            inThread: false,
            now: DateTime.utc(2026, 11, 14, 9, 1),
            authorIsPerson: person,
          );
      expect(actionsFor('sam'), contains(MsgAction.watchAuthor));
      expect(actionsFor('me'), isNot(contains(MsgAction.watchAuthor)));
      expect(
        actionsFor('helper', person: false),
        isNot(contains(MsgAction.watchAuthor)),
        reason: 'not for a bot',
      );
      expect(
        actionLabel(L10nEn(), MsgAction.watchAuthor),
        'Tell me when they are around',
      );
    },
  );
}
