import 'dart:convert';

import 'package:churr/core/commands/chance_live.dart';
import 'package:churr/core/instant/zone.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:churr/core/models/json.dart';
import 'package:churr/core/models/server.dart';
import 'package:churr/core/db/cache_db.dart';
import 'package:churr/state/app_state.dart';
import 'package:churr/ui/message/chance_view.dart';
import 'package:churr/ui/screens/conversation/chat_commands.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../conversation_support.dart';

/// The built-in commands of ruling R67 typed in the conversation screen:
/// /roll is sent to the server's dice module, which rolls it, and shown as a
/// result, a mistyped command
/// is refused before anything is sent, and the server's refusal is shown in
/// its words.
void main() {
  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets(
      '/roll runs the dice module on the server and draws its result ($label)',
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
          'POST /api/v1/conversations/$general/commands',
          (r) => {
            'message': {
              'id': 'rolled',
              'conversation_id': general,
              'seq': 90,
              'order': '0000000000000000!!000000000000005a',
              'author_id': 'me',
              'created_at': DateTime.now().toUtc().toIso8601String(),
              'text': 'Rolled 2d6+3: [4, 2] + 3 = 9',
              'view': {
                'kind': 'dice_roll',
                'level': 1,
                'by': 'dice',
                'data': {
                  'dice': '2d6+3',
                  'rows': [
                    {
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
                  ],
                },
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
        expect(fake.where('POST', '/messages'), isEmpty);
        // The draw's input goes with it: its client_id and second.
        final sent = fake.where('POST', '/commands').single.body as Map;
        expect(
          {'name': sent['name'], 'text': sent['text']},
          {'name': 'roll', 'text': '2d6+3'},
        );
        expect(sent['client_id'], isA<String>());
        expect(sent['draw_t'], isA<int>());
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
        'text': 'Rolled 1d6: [5] = 5',
        'view': {
          'kind': 'dice_roll',
          'level': 1,
          'by': 'dice',
          'data': {
            'dice': '1d6',
            'rows': [
              {
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
            ],
          },
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
      fake.on('POST /api/v1/conversations/$general/commands', (r) async {
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

  testWidgets('/ban asks first, naming the person and the channel', (
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
    fake.on(
      'POST /api/v1/conversations/$general/bans',
      (r) => <String, Object?>{},
    );
    final app = await conversationApp(tester, db, fake);
    await pumpConversation(tester, app);
    final box = find.byKey(const Key('composer'));

    await tester.enterText(box, '/ban @lee flooding');
    await settle(tester, 2);
    await tester.tap(find.byKey(const Key('send')));
    await settle(tester);
    expect(find.text('Ban Lee Park from General?'), findsOneWidget);
    expect(find.textContaining('kept from coming back'), findsOneWidget);
    await tester.tap(find.byKey(const Key('ban-cancel')));
    await settle(tester);
    expect(fake.where('POST', '/bans'), isEmpty);

    await tester.enterText(box, '/ban @lee flooding');
    await settle(tester, 2);
    await tester.tap(find.byKey(const Key('send')));
    await settle(tester);
    await tester.tap(find.byKey(const Key('ban-yes')));
    await settle(tester, 4);
    expect(fake.where('POST', '/bans').single.body, {
      'account_id': 'lee',
      'reason': 'flooding',
    });
    expect(tester.takeException(), isNull);
  });

  testWidgets(
    '/remind reads its grammar, and opens the form only when nothing is typed',
    (tester) async {
      await setSize(tester, desktop);
      registerChatCommands();
      final was = InstantEnv.now;
      // 10:00 in Brisbane, the server's zone, which an account with no time
      // zone of its own reads typed times in, whatever the device's.
      InstantEnv.now = () => DateTime.utc(2026, 11, 17);
      addTearDown(() => InstantEnv.now = was);
      late CacheDb db;
      late FakeServer fake;
      await tester.runAsync(() async {
        db = await conversationCache();
        fake = FakeServer();
      });
      fake.on('POST /api/v1/chats', (r) {
        final ids = ((jsonDecode(r.body) as Map)['account_ids'] as List)
            .cast<String>();
        return {
          'conversation': {
            'id': ids.isEmpty ? 'mine' : 'with-${ids.join()}',
            'kind': 'channel',
            'member_ids': ['me', ...ids],
          },
        };
      });
      fake.on(
        'POST /api/v1/scheduled',
        (r) => {
          'scheduled': {'id': 's1'},
        },
      );
      final app = await conversationApp(tester, db, fake);
      serverOf(app).info = ServerInfo(
        name: 'chat.example.org',
        communityName: 'Work',
        version: '1',
        minAppVersion: '0.0.0',
        setupNeeded: false,
        timeZone: 'Australia/Brisbane',
      );
      await pumpConversation(tester, app);
      final box = find.byKey(const Key('composer'));
      Future<void> send(String text) async {
        ScaffoldMessenger.of(tester.element(box)).removeCurrentSnackBar();
        await settle(tester, 2);
        await tester.enterText(box, text);
        await settle(tester, 2);
        await tester.tap(find.byKey(const Key('send')));
        await settle(tester, 4);
      }

      await send('/remind @sam tomorrow at 9 | Review is due');
      expect(fake.where('POST', '/chats').single.body, {
        'account_ids': ['sam'],
      });
      expect(fake.where('POST', '/scheduled').single.body, {
        'conversation_id': 'with-sam',
        'kind': 'reminder',
        'content': {'text': 'Review is due'},
        'run_at': formatTime(DateTime.utc(2026, 11, 17, 23)),
      });
      expect(tester.widget<TextField>(box).controller!.text, isEmpty);

      await send('/remind every weekday at 09:30 | Stand-up');
      expect(fake.where('POST', '/scheduled').last.body, {
        'conversation_id': 'mine',
        'kind': 'reminder',
        'content': {'text': 'Stand-up'},
        'repeat': {
          'every': 'day',
          'days': '1111100',
          'at': '09:30',
          'time_zone': 'Australia/Brisbane',
        },
      });

      await send('/remind tomorrow');
      expect(find.textContaining('Say what to be reminded of'), findsOneWidget);
      expect(fake.where('POST', '/scheduled'), hasLength(2));
      expect(
        tester.widget<TextField>(box).controller!.text,
        '/remind tomorrow',
      );

      await send('/remind');
      expect(find.byKey(const Key('remind-what')), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets('/mod and /unmod set the moderator level and back (R97)', (
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
    fake.on(
      'PATCH /api/v1/conversations/$general/members/lee',
      (r) => <String, Object?>{},
    );
    final app = await conversationApp(tester, db, fake);
    await pumpConversation(tester, app);
    final box = find.byKey(const Key('composer'));
    for (final (text, level, said) in [
      ('/mod @lee', 'moderator', 'Lee Park is a moderator of this channel.'),
      (
        '/unmod @lee',
        'member',
        'Lee Park is a member of this channel, no longer a moderator.',
      ),
    ]) {
      await tester.enterText(box, text);
      await settle(tester, 2);
      await tester.tap(find.byKey(const Key('send')));
      await settle(tester, 4);
      expect(fake.where('PATCH', '/members/lee').last.body, {'level': level});
      expect(find.text(said), findsOneWidget);
      ScaffoldMessenger.of(tester.element(box)).removeCurrentSnackBar();
      await settle(tester, 2);
    }
    expect(tester.takeException(), isNull);
  });

  testWidgets('/invite in a private channel on a hub offers admission', (
    tester,
  ) async {
    await setSize(tester, desktop);
    registerChatCommands();
    late CacheDb db;
    late FakeServer fake;
    await tester.runAsync(() async {
      db = await conversationCache();
      final c = channel(general, 'General', private: true);
      await db.upsertConversation(
        Conversation.fromJson({
          ...c.raw,
          'shared': {'stream_id': 'st1'},
        }),
        syncedSeq: c.lastSeq,
      );
      fake = FakeServer();
    });
    fake.on(
      'POST /api/v1/streams/st1/channel/members',
      (r) => <String, Object?>{},
    );
    // The hub's record: private, with the reader on its moderator list.
    fake.on(
      'GET /api/v1/streams/st1/channel',
      (r) => {
        'channel': {
          'stream_id': 'st1',
          'version': 1,
          'private': true,
          'moderators': [
            {'account_id': 'me', 'server': 'here', 'username': 'kai'},
          ],
        },
        'manager': false,
        'moderator': true,
      },
    );
    fake.on(
      'GET /api/v1/streams/st1/channel/requests',
      (r) => {'requests': <Object>[]},
    );
    final app = await conversationApp(tester, db, fake);
    await pumpConversation(tester, app);
    final box = find.byKey(const Key('composer'));
    await tester.enterText(box, '/invite @lee');
    await settle(tester, 2);
    await tester.tap(find.byKey(const Key('send')));
    await settle(tester);
    expect(find.text('Admit Lee Park to #General?'), findsOneWidget);
    await tester.tap(find.byKey(const Key('confirm-admit')));
    await settle(tester, 4);
    expect(fake.where('POST', '/channel/members').single.body, {
      'action': 'admit',
      'account_id': 'lee',
    });
    // Never the ordinary add, which the server refuses there.
    expect(fake.where('POST', '/conversations/$general/members'), isEmpty);
    expect(
      find.text('Lee Park is admitted. Their instance adds them.'),
      findsOneWidget,
    );
    expect(tester.takeException(), isNull);
  });
}
