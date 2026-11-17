import 'package:churr/core/db/account_tables.dart';
import 'package:churr/core/models/account.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/core/session_store.dart';
import 'package:churr/state/account_prefs.dart';
import 'package:churr/state/app_state.dart';
import 'package:churr/ui/layout/account_entries.dart';
import 'package:churr/ui/screens/account/bookmarks_screen.dart';
import 'package:churr/ui/screens/account/conversation_settings.dart';
import 'package:churr/ui/screens/account/dnd_screen.dart';
import 'package:churr/ui/screens/account/my_profile_screen.dart';
import 'package:churr/ui/screens/account/notifications_screen.dart';
import 'package:churr/ui/screens/account/person_profile.dart';
import 'package:churr/ui/screens/account/server_settings_screen.dart';
import 'package:churr/ui/screens/account/sessions_screen.dart';
import 'package:churr/ui/screens/account/sign_in_screens.dart';
import 'package:churr/ui/screens/account/status_screen.dart';
import 'package:churr/ui/screens/account/threads_screen.dart';
import 'package:churr/ui/screens/add_server/add_server_screen.dart';
import 'package:churr/ui/screens/settings/app_settings_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../account_support.dart';

Message m(String id, String conv, int seq, {String? parent}) =>
    Message.fromJson({
      'id': id,
      'conversation_id': conv,
      'seq': seq,
      'author_id': 'sam',
      'parent_id': ?parent,
      'text': 'Words of $id',
      'created_at': DateTime.utc(2026, 11, 14, 9, seq).toIso8601String(),
    });

void main() {
  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    group('at $label size', () {
      testWidgets('Notifications: items from both servers, filter, mark read', (
        tester,
      ) async {
        await sized(tester, size);
        final (app, fakes) = await makeAccountApp(tester);
        await tester.runAsync(() async {
          await app.servers[0].db.addNotification(
            m('w1', 'general', 21),
            'notify',
          );
          await app.servers[1].db.addNotification(
            m('f1', 'secret', 22),
            'notify',
          );
        });
        await tester.pumpWidget(harness(app, const NotificationsScreen()));
        await settleAll(tester);
        expect(find.text('Words of w1'), findsOneWidget);
        expect(
          find.text('Words of f1'),
          findsOneWidget,
          reason: 'a hidden conversation notifies here',
        );
        expect(
          find.byKey(const Key('notifications-server-filter')),
          findsOneWidget,
        );
        await tester.tap(find.text('Mentions').first);
        await settleAll(tester);
        expect(find.text('Words of w1'), findsNothing);
        await tester.tap(find.text('All').first);
        await settleAll(tester);
        fakes[0].on('PUT /api/v1/conversations/general/read', {
          'membership': {'level': 'member', 'read_seq': 21, 'mode': 'mentions'},
        });
        await tester.tap(find.byKey(const Key('mark-read-server-0/w1')));
        await settleAll(tester);
        expect(find.text('EARLIER'), findsOneWidget);
        expect(
          fakes[0].calls,
          contains('PUT /api/v1/conversations/general/read'),
        );
        expect(tester.takeException(), isNull);
      });

      testWidgets('Threads and Bookmarks draw and open in place', (
        tester,
      ) async {
        await sized(tester, size);
        final (app, _) = await makeAccountApp(tester);
        await tester.runAsync(() async {
          final db = app.servers[0].db;
          await db.putThreadParents([
            Message.fromJson({
              ...m('p1', 'general', 5).raw,
              'thread': {
                'reply_count': 3,
                'last_reply_seq': 9,
                'last_reply_at': '2026-11-14T10:00:00.000Z',
                'participant_ids': [],
              },
            }),
          ]);
          await db.upsertThreadStates([
            ThreadState(
              parentId: 'p1',
              conversationId: 'general',
              state: 'following',
              readSeq: 6,
              unreadCount: 3,
            ),
          ]);
        });
        await tester.pumpWidget(harness(app, const ThreadsScreen()));
        await settleAll(tester);
        expect(find.text('Words of p1'), findsOneWidget);
        expect(find.text('3 unread replies'), findsOneWidget);
        await tester.tap(find.byKey(const Key('open-thread-server-0/p1')));
        await settleAll(tester);
        expect(app.open, const OpenTarget('server-0', 'general'));
        expect(app.focusThreadId, 'p1');

        await tester.pumpWidget(harness(app, const BookmarksScreen()));
        await settleAll(tester);
        expect(find.textContaining('No bookmarks yet'), findsOneWidget);
        expect(tester.takeException(), isNull);
      });

      testWidgets(
        'Conversation settings: the two settings, the summary, mute',
        (tester) async {
          await sized(tester, size);
          final (app, fakes) = await makeAccountApp(tester);
          fakes[0].on('PATCH /api/v1/conversations/general/membership', {
            'membership': {
              'level': 'member',
              'read_seq': 20,
              'mode': 'mentions',
              'show': 'hidden',
              'unread_count': 2,
            },
          });
          await tester.pumpWidget(
            harness(
              app,
              ConversationSettings(
                server: app.servers[0],
                conversationId: 'general',
              ),
            ),
          );
          await settleAll(tester);
          expect(find.byKey(const Key('mode-any')), findsOneWidget);
          expect(find.text('App default (Always)'), findsOneWidget);
          expect(
            find.textContaining('The conversation is always in the sidebar'),
            findsOneWidget,
          );
          await tester.ensureVisible(find.byKey(const Key('show-hidden')));
          await tester.tap(find.byKey(const Key('show-hidden')));
          await settleAll(tester);
          expect(
            find.textContaining(
              'Its notifications will be in the Notifications list',
            ),
            findsOneWidget,
          );
          expect(
            fakes[0].calls,
            contains('PATCH /api/v1/conversations/general/membership'),
          );
          expect(tester.takeException(), isNull);
        },
      );

      testWidgets('Status and do not disturb', (tester) async {
        await sized(tester, size);
        final (app, fakes) = await makeAccountApp(tester);
        await tester.pumpWidget(harness(app, const StatusScreen()));
        await settleAll(tester);
        await tester.enterText(
          find.byKey(const Key('status-text')),
          'In the garden',
        );
        await tester.tap(find.byKey(const Key('set-status')));
        await settleAll(tester);
        expect(fakes[0].calls, contains('PUT /api/v1/me/status'));
        expect(fakes[1].calls, contains('PUT /api/v1/me/status'));

        await tester.pumpWidget(harness(app, const DndScreen()));
        await settleAll(tester);
        expect(find.text('You are available on every server.'), findsOneWidget);
        expect(find.textContaining('Quiet hours on Work'), findsOneWidget);
        await tester.tap(find.byKey(const Key('dnd-turn-on')));
        await settleAll(tester);
        await tester.tap(find.byKey(const Key('until-forever')));
        await settleAll(tester);
        expect(fakes[0].calls, contains('PUT /api/v1/me/dnd'));
        expect(fakes[1].calls, contains('PUT /api/v1/me/dnd'));
        expect(tester.takeException(), isNull);
      });

      testWidgets('Own profile applies to the servers chosen', (tester) async {
        await sized(tester, size);
        final (app, fakes) = await makeAccountApp(tester);
        await tester.pumpWidget(
          harness(
            app,
            MyProfileScreen(
              pickImage: () async => null,
              deviceZone: () async => 'Europe/London',
            ),
          ),
        );
        await settleAll(tester);
        expect(find.text('Work'), findsWidgets, reason: 'one tab per server');
        await tester.enterText(
          find.byKey(const Key('profile-name')).first,
          'Kai T',
        );
        await tester.ensureVisible(find.byKey(const Key('save-profile')).first);
        await tester.tap(find.byKey(const Key('save-profile')).first);
        await settleAll(tester);
        await tester.tap(find.byKey(const Key('apply-server-1')));
        await tester.pump();
        await tester.tap(find.byKey(const Key('apply-profile')));
        await settleAll(tester);
        expect(fakes[0].calls, contains('PATCH /api/v1/me'));
        expect(fakes[1].calls, isNot(contains('PATCH /api/v1/me')));
        expect(tester.takeException(), isNull);
      });

      testWidgets(
        'Someone else\'s profile: status, local time, out of office, block',
        (tester) async {
          await sized(tester, size);
          final (app, fakes) = await makeAccountApp(tester);
          await tester.runAsync(
            () => app.servers[0].db.upsertAccounts([
              Account.fromJson({
                'id': 'sam',
                'username': 'sam',
                'display_name': 'Sam Okafor',
                'role': 'member',
                'presence': 'away',
                'time_zone': 'Asia/Tokyo',
                'status': {
                  'emoji': '🌴',
                  'text': 'On holiday',
                  'pause_notifications': true,
                  'appear_away': true,
                },
                'availability': {
                  'value': 'out_of_office',
                  'until': '2026-11-20T00:00:00.000Z',
                },
              }),
            ]),
          );
          await tester.pumpWidget(
            harness(
              app,
              PersonProfile(
                server: app.servers[0],
                accountId: 'sam',
                timeIn: (z, {now}) => DateTime(2026, 11, 14, 18, 5),
              ),
            ),
          );
          await settleAll(tester);
          expect(find.text('Sam Okafor'), findsOneWidget);
          expect(find.byKey(const Key('person-status')), findsOneWidget);
          expect(find.textContaining('Local time 6:05'), findsOneWidget);
          expect(find.textContaining('Out of office until'), findsOneWidget);
          // A person may be reported to the admins; the dialog says who
          // is shown what.
          await tester.ensureVisible(find.byKey(const Key('report-person')));
          await tester.tap(find.byKey(const Key('report-person')));
          await settleAll(tester);
          expect(find.textContaining("this instance's admins"), findsOneWidget);
          await tester.enterText(
            find.byKey(const Key('report-reason')),
            'the name',
          );
          await tester.tap(find.byKey(const Key('confirm-report')));
          await settleAll(tester);
          expect(fakes[0].calls, contains('POST /api/v1/accounts/sam/report'));
          await tester.ensureVisible(find.byKey(const Key('block-toggle')));
          await tester.tap(find.byKey(const Key('block-toggle')));
          await settleAll(tester);
          await tester.tap(find.byKey(const Key('confirm-block')));
          await settleAll(tester);
          expect(fakes[0].calls, contains('PUT /api/v1/me/blocks/sam'));
          expect(find.text('Unblock'), findsOneWidget);
          expect(tester.takeException(), isNull);
        },
      );

      testWidgets(
        'Server settings: mute says plainly who sees it; the conversations table',
        (tester) async {
          await sized(tester, size);
          final (app, fakes) = await makeAccountApp(tester);
          await tester.pumpWidget(
            harness(app, ServerSettingsScreen(server: app.servers[1])),
          );
          await settleAll(tester);
          await tester.ensureVisible(find.byKey(const Key('mute-server')));
          await tester.tap(find.byKey(const Key('mute-server')));
          await settleAll(tester);
          await tester.tap(find.byKey(const Key('until-hour')));
          await settleAll(tester);
          expect(
            find.text(
              'People on Friends will see that you are in do not disturb.',
            ),
            findsOneWidget,
          );
          await tester.tap(find.byKey(const Key('confirm-mute-server')));
          await settleAll(tester);
          expect(fakes[1].calls, contains('PUT /api/v1/me/dnd'));
          expect(fakes[0].calls, isNot(contains('PUT /api/v1/me/dnd')));
          expect(
            app.prefs.isServerMuted('https://s1.example.org', DateTime.now()),
            isTrue,
          );
          await tester.scrollUntilVisible(
            find.byKey(const Key('conv-row-secret')),
            300,
            scrollable: find.byType(Scrollable).first,
          );
          expect(
            find.byKey(const Key('conv-row-secret')),
            findsOneWidget,
            reason: 'hidden ones included',
          );
          expect(tester.takeException(), isNull);
        },
      );

      testWidgets('Sessions and devices', (tester) async {
        await sized(tester, size);
        final (app, fakes) = await makeAccountApp(tester);
        fakes[0].on('GET /api/v1/me/sessions', {
          'sessions': [
            {
              'id': 's1',
              'kind': 'app',
              'label': 'Kai\'s laptop',
              'current': true,
              'created_at': '2026-11-01T00:00:00.000Z',
            },
            {
              'id': 's2',
              'kind': 'web',
              'label': 'Firefox',
              'created_at': '2026-11-01T00:00:00.000Z',
            },
          ],
        });
        fakes[0].on('GET /api/v1/me/devices', {'devices': []});
        await tester.pumpWidget(
          harness(app, SessionsScreen(server: app.servers[0])),
        );
        await settleAll(tester);
        expect(find.text('Kai\'s laptop'), findsOneWidget);
        await tester.tap(find.text('Sign out'));
        await settleAll(tester);
        expect(fakes[0].calls, contains('DELETE /api/v1/me/sessions/s2'));
        // Every other session at once, after a confirmation.
        await tester.tap(find.byKey(const Key('end-other-sessions')));
        await settleAll(tester);
        await tester.tap(find.byKey(const Key('confirm')));
        await settleAll(tester);
        expect(fakes[0].calls, contains('DELETE /api/v1/me/sessions'));
        expect(tester.takeException(), isNull);
      });

      testWidgets('App settings, and the set-up code', (tester) async {
        await sized(tester, size);
        final (app, _) = await makeAccountApp(tester);
        await tester.pumpWidget(harness(app, const AppSettingsScreen()));
        await settleAll(tester);
        expect(find.byKey(const Key('server-order')), findsOneWidget);
        await tester.scrollUntilVisible(
          find.byKey(const Key('startup-sound')),
          300,
          scrollable: find.byType(Scrollable).first,
        );
        await tester.ensureVisible(find.byKey(const Key('startup-sound')));
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const Key('startup-sound')));
        await settleAll(tester);
        expect(app.prefs.startupSound, isFalse);

        await tester.pumpWidget(harness(app, const SetupCodeScreen()));
        await settleAll(tester);
        expect(find.byKey(const Key('setup-qr')), findsOneWidget);
        expect(tester.takeException(), isNull);
      });

      testWidgets(
        'Add a server takes an invite link; signed out shows a way back in',
        (tester) async {
          await sized(tester, size);
          final (app, fakes) = await makeAccountApp(tester);
          fakes[1].on('GET /api/v1/server', {
            'server': {
              'name': 's1.example.org',
              'community_name': 'Friends',
              'version': '1.0',
            },
          });
          await tester.pumpWidget(
            harness(
              app,
              AddServerScreen(signIn: (_) async => throw UnimplementedError()),
            ),
          );
          await settleAll(tester);
          await tester.enterText(
            find.byKey(const Key('server-address')),
            'https://s1.example.org/join/abc123',
          );
          await tester.tap(find.byKey(const Key('look-up')));
          await settleAll(tester);
          expect(find.byKey(const Key('join-in-browser')), findsOneWidget);
          expect(find.byKey(const Key('copy-from-device')), findsOneWidget);

          // The server ends this device's session.
          await tester.runAsync(() async {
            fakes[0].on('POST /oauth/token', {
              'error': 'invalid_grant',
            }, status: 400);
            await app.servers[0].api.setTokens(
              Tokens(
                access: 'a',
                refresh: 'r',
                expiresAt: DateTime.now().subtract(const Duration(minutes: 1)),
              ),
            );
            try {
              await app.servers[0].api.accessToken();
            } on Object {
              // Signed out.
            }
          });
          await tester.pumpWidget(harness(app, const SignedOutBanners()));
          await settleAll(tester);
          expect(find.byKey(const Key('signed-out-server-0')), findsOneWidget);
          expect(find.text('Sign in again'), findsOneWidget);
          expect(tester.takeException(), isNull);
        },
      );
    });
  }

  testWidgets(
    'the sidebar entries open Notifications, and do not disturb is one tap away',
    (tester) async {
      await sized(tester, desktop);
      final (app, fakes) = await makeAccountApp(tester);
      await tester.pumpWidget(
        harness(
          app,
          Builder(
            builder: (context) => Column(
              children: [
                const AccountStrip(),
                TextButton(
                  onPressed: () => openNotifications(context),
                  child: const Text('open'),
                ),
              ],
            ),
          ),
        ),
      );
      await settleAll(tester);
      await tester.tap(find.byKey(const Key('dnd-toggle')));
      await settleAll(tester);
      await tester.tap(find.byKey(const Key('until-min30')));
      await settleAll(tester);
      expect(fakes[0].calls, contains('PUT /api/v1/me/dnd'));
      expect(fakes[1].calls, contains('PUT /api/v1/me/dnd'));
      await tester.tap(find.text('open'));
      await settleAll(tester);
      expect(find.byType(NotificationsScreen), findsOneWidget);
    },
  );
}
