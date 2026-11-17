import 'dart:async';

import 'package:churr/core/models/chat_request.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:churr/core/models/json.dart';
import 'package:churr/ui/screens/conversation/chat_state_bar.dart';
import 'package:churr/ui/screens/requests/requests_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../account_support.dart';

// Chat requests and closed chats (rulings R77, R78, R79; docs/CLIENTS.md
// 9.27): the list of requests with its four answers, what stands in for the
// composer of a chat that waits, was declined or was closed, and what sync
// does with chat_request.received and .removed.

Json requestJson(String id, String who, {String kind = 'direct'}) => {
  'id': id,
  'kind': kind,
  'account': {
    'id': who,
    'username': who,
    'display_name': '${who[0].toUpperCase()}${who.substring(1)} Okafor',
    'role': 'member',
  },
  'created_at': '2026-11-16T09:00:00.000Z',
};

Json chatJson(String id, {String? state, String kind = 'channel'}) => {
  'id': id,
  'kind': kind,
  'private': true,
  'member_ids': ['me', 'sam'],
  'member_count': 2,
  'last_seq': 1,
  'created_at': '2026-11-16T09:00:00.000Z',
  'chat_state': ?state,
  'membership': {
    'level': 'member',
    'read_seq': 0,
    'mode': 'any',
    'unread_count': 0,
    'mention_count': 0,
    'badge_count': 0,
  },
};

void main() {
  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    group('at $label size', () {
      testWidgets('the requests: who is asking, and the four answers', (
        tester,
      ) async {
        await sized(tester, size);
        final (app, fakes) = await makeAccountApp(tester);
        final s = app.servers[0];
        await tester.runAsync(
          () => s.db.putChatRequests([
            ChatRequest.fromJson(requestJson('r1', 'sam')),
            ChatRequest.fromJson(requestJson('r2', 'ada', kind: 'sealed')),
            ChatRequest.fromJson(requestJson('r3', 'bea')),
            ChatRequest.fromJson(requestJson('r4', 'cy')),
          ]),
        );
        fakes[0].on('POST /api/v1/me/chat-requests/r1/accept', {
          'conversation': chatJson('c1'),
        });
        await tester.pumpWidget(harness(app, const RequestsScreen()));
        await settleAll(tester);
        expect(find.textContaining('Sam Okafor'), findsOneWidget);
        expect(find.textContaining('@sam'), findsOneWidget);
        expect(find.textContaining('Asks for a sealed chat'), findsOneWidget);
        expect(find.textContaining('Asks for a direct chat'), findsNWidgets(3));
        expect(
          find.text('Decline tells them; dismiss and block do not.'),
          findsOneWidget,
        );

        Future<void> answer(String id, String how) async {
          final b = find.descendant(
            of: find.byKey(Key('request-$id')),
            matching: find.byKey(Key('request-$how')),
          );
          await tester.ensureVisible(b);
          await tester.tap(b);
          await settleAll(tester);
        }

        await answer('r2', 'decline');
        await answer('r3', 'dismiss');
        await answer('r4', 'block');
        await answer('r1', 'accept');
        expect(
          fakes[0].calls,
          containsAll([
            'POST /api/v1/me/chat-requests/r2/decline',
            'POST /api/v1/me/chat-requests/r3/dismiss',
            'POST /api/v1/me/chat-requests/r4/block',
            'POST /api/v1/me/chat-requests/r1/accept',
          ]),
        );
        expect(s.chatRequests, isEmpty);
        expect(find.byKey(const Key('requests-empty')), findsOneWidget);
        expect(s.conversation('c1'), isNotNull, reason: 'the accepted chat');
        expect(app.open?.conversationId, 'c1');
        expect(tester.takeException(), isNull);
      });

      testWidgets('invitations: a group chat and channels (R103)', (
        tester,
      ) async {
        await sized(tester, size);
        final (app, fakes) = await makeAccountApp(tester);
        final s = app.servers[0];
        Json channel(String id, String access) => {
          'id': id,
          'kind': 'channel',
          'name': 'garden-$access',
          'topic': 'Seeds and soil',
          'access': access,
          'private': access != 'public',
          'member_count': 7,
        };
        await tester.runAsync(
          () => s.db.putChatRequests([
            ChatRequest.fromJson({
              ...requestJson('g1', 'sam', kind: 'group'),
              'conversation_id': 'gc',
              'people': ['Sam Okafor', 'Ada Lovelace'],
            }),
            ChatRequest.fromJson({
              ...requestJson('c1', 'ada', kind: 'channel'),
              'conversation_id': 'pub',
              'channel': channel('pub', 'public'),
              'level': 'moderator',
            }),
            ChatRequest.fromJson({
              ...requestJson('c2', 'bea', kind: 'channel'),
              'conversation_id': 'priv',
              'channel': channel('priv', 'private'),
              'level': 'member',
              'override': true,
            }),
          ]),
        );
        fakes[0].on('GET /api/v1/conversations/pub', {
          'conversation': {...channel('pub', 'public'), 'last_seq': 3},
        });
        fakes[0].on('POST /api/v1/me/chat-requests/g1/accept', {
          'conversation': {
            ...chatJson('gc'),
            'member_ids': ['me', 'sam', 'ada'],
          },
        });
        await tester.pumpWidget(harness(app, RequestsScreen(onOpened: () {})));
        await settleAll(tester);
        Finder inside(String id, Finder f) =>
            find.descendant(of: find.byKey(Key('request-$id')), matching: f);

        expect(
          inside('g1', find.textContaining('Included you in a group chat')),
          findsOneWidget,
        );
        expect(
          inside('g1', find.text('With Sam Okafor, Ada Lovelace')),
          findsOneWidget,
        );
        expect(
          inside('c1', find.textContaining('Invites you to a channel')),
          findsOneWidget,
        );
        expect(inside('c1', find.text('#garden-public')), findsOneWidget);
        expect(
          inside('c1', find.text('Public channel · 7 members')),
          findsOneWidget,
        );
        expect(inside('c1', find.text('Seeds and soil')), findsOneWidget);
        expect(
          inside('c1', find.text('You would join as moderator.')),
          findsOneWidget,
        );
        expect(
          inside('c2', find.text('Private channel · 7 members')),
          findsOneWidget,
        );
        expect(
          inside('c2', find.byKey(const Key('request-override'))),
          findsOneWidget,
        );
        expect(
          inside('c1', find.byKey(const Key('request-override'))),
          findsNothing,
        );
        // Only the public channel can be read before accepting.
        expect(
          inside('c1', find.byKey(const Key('request-preview'))),
          findsOneWidget,
        );
        expect(
          inside('c2', find.byKey(const Key('request-preview'))),
          findsNothing,
        );
        expect(
          inside('g1', find.byKey(const Key('request-preview'))),
          findsNothing,
        );
        // Four answers for every kind.
        for (final id in ['g1', 'c1', 'c2']) {
          for (final how in ['accept', 'decline', 'dismiss', 'block']) {
            expect(inside(id, find.byKey(Key('request-$how'))), findsOneWidget);
          }
        }

        final preview = inside('c1', find.byKey(const Key('request-preview')));
        await tester.ensureVisible(preview);
        await tester.tap(preview);
        await settleAll(tester);
        expect(fakes[0].calls, contains('GET /api/v1/conversations/pub'));
        expect(app.open?.conversationId, 'pub');
        expect(s.conversation('pub')?.membership, isNull);
        expect(
          [for (final r in s.chatRequests) r.id],
          ['g1', 'c1', 'c2'],
          reason: 'previewing answers nothing',
        );

        // Declining after a preview drops the channel read without joining.
        final decline = inside('c1', find.byKey(const Key('request-decline')));
        await tester.ensureVisible(decline);
        await tester.tap(decline);
        await settleAll(tester);
        expect(s.conversation('pub'), isNull);

        final accept = inside('g1', find.byKey(const Key('request-accept')));
        await tester.ensureVisible(accept);
        await tester.tap(accept);
        await settleAll(tester);
        expect(app.open?.conversationId, 'gc');
        expect(s.conversation('gc'), isNotNull);
        expect([for (final r in s.chatRequests) r.id], ['c2']);
        expect(tester.takeException(), isNull);
      });

      testWidgets('a waiting chat: no composer, and withdrawing', (
        tester,
      ) async {
        await sized(tester, size);
        final (app, fakes) = await makeAccountApp(tester);
        final s = app.servers[0];
        final conv = Conversation.fromJson(chatJson('w1', state: 'waiting'));
        await tester.runAsync(() => s.db.upsertConversation(conv));
        await tester.pumpWidget(
          harness(app, ChatStateBar(server: s, conversation: conv)),
        );
        await settleAll(tester);
        expect(find.textContaining('to accept your request'), findsOneWidget);
        await tester.tap(find.byKey(const Key('withdraw-request')));
        await settleAll(tester);
        expect(find.text('Withdraw this request?'), findsOneWidget);
        await tester.tap(find.byKey(const Key('confirm')));
        await settleAll(tester);
        expect(
          fakes[0].calls,
          contains('POST /api/v1/conversations/w1/withdraw'),
        );
        expect(s.conversation('w1'), isNull);
        expect(tester.takeException(), isNull);
      });

      testWidgets('a closed chat: ask to chat again', (tester) async {
        await sized(tester, size);
        final (app, fakes) = await makeAccountApp(tester);
        final s = app.servers[0];
        final conv = Conversation.fromJson(chatJson('x1', state: 'closed'));
        await tester.runAsync(() => s.db.upsertConversation(conv));
        fakes[0].on('POST /api/v1/chats', {
          'conversation': chatJson('x2', state: 'waiting'),
        }, status: 201);
        await tester.pumpWidget(
          harness(app, ChatStateBar(server: s, conversation: conv)),
        );
        await settleAll(tester);
        expect(
          find.text('This chat is closed. You can both still read it.'),
          findsOneWidget,
        );
        await tester.tap(find.byKey(const Key('ask-again')));
        await settleAll(tester);
        expect(fakes[0].calls, contains('POST /api/v1/chats'));
        expect(app.open?.conversationId, 'x2');
        expect(tester.takeException(), isNull);
      });

      testWidgets('a declined chat says so and offers nothing', (tester) async {
        await sized(tester, size);
        final (app, _) = await makeAccountApp(tester);
        final s = app.servers[0];
        final conv = Conversation.fromJson(chatJson('d1', state: 'declined'));
        await tester.pumpWidget(
          harness(app, ChatStateBar(server: s, conversation: conv)),
        );
        await settleAll(tester);
        expect(find.textContaining('declined this chat'), findsOneWidget);
        expect(find.byType(FilledButton), findsNothing);
        expect(find.byType(OutlinedButton), findsNothing);
      });
    });
  }

  test('a conversation says whether it is direct and how it stands', () {
    expect(Conversation.fromJson(chatJson('a')).isDirect, isTrue);
    expect(Conversation.fromJson(chatJson('a')).chatState, isNull);
    expect(
      Conversation.fromJson(chatJson('a', state: 'closed')).chatState,
      'closed',
    );
    expect(
      Conversation.fromJson({
        ...chatJson('a'),
        'member_ids': ['me', 'a', 'b'],
      }).isDirect,
      isFalse,
    );
    expect(
      Conversation.fromJson({...chatJson('a'), 'name': 'den'}).isDirect,
      isFalse,
    );
    expect(
      Conversation.fromJson(chatJson('a', kind: 'sealed')).isDirect,
      isTrue,
    );
  });

  test('a request reads its kind and what comes with it (R103)', () {
    final g = ChatRequest.fromJson({
      ...requestJson('g', 'sam', kind: 'group'),
      'conversation_id': 'gc',
      'people': ['Sam', 'Ada'],
    });
    expect(g.isGroup, isTrue);
    expect(g.conversationId, 'gc');
    expect(g.people, ['Sam', 'Ada']);
    expect(g.previewable, isFalse);
    final c = ChatRequest.fromJson({
      ...requestJson('c', 'sam', kind: 'channel'),
      'conversation_id': 'ch',
      'channel': {'id': 'ch', 'name': 'garden', 'access': 'public'},
      'level': 'reader',
      'override': true,
    });
    expect(c.isChannel, isTrue);
    expect(c.channel?.name, 'garden');
    expect(c.level, 'reader');
    expect(c.override, isTrue);
    expect(c.previewable, isTrue);
    final k = ChatRequest.fromJson({
      ...requestJson('k', 'sam', kind: 'channel'),
      'channel': {'id': 'k', 'name': 'den', 'access': 'knock'},
    });
    expect(k.previewable, isFalse);
    expect(k.override, isFalse);
    final d = ChatRequest.fromJson(requestJson('d', 'sam'));
    expect(d.channel, isNull);
    expect(d.people, isEmpty);
  });

  test('sync keeps the requests and notifies once when told', () async {
    final (app, _) = await makeAccountApp(null);
    final s = app.servers[0];
    final local = <Json>[];
    final sub = s.sync.localEvents.listen(local.add);
    await s.sync.applyEvent({
      'type': 'chat_request.received',
      'at': '2026-11-16T09:00:00.000Z',
      'request': requestJson('r1', 'sam'),
      'notify': true,
    });
    await s.sync.applyEvent({
      'type': 'chat_request.received',
      'at': '2026-11-16T09:00:00.000Z',
      'request': requestJson('r2', 'ada'),
      'notify': false,
    });
    await s.sync.applyEvent({
      'type': 'chat_request.received',
      'at': '2026-11-16T09:00:00.000Z',
      'request': requestJson('r2', 'ada'),
      'notify': false,
    });
    await Future<void>.delayed(Duration.zero);
    var held = await s.db.chatRequests();
    expect([for (final r in held) r.id], ['r1', 'r2']);
    expect((await s.db.account('sam'))?.displayName, 'Sam Okafor');
    expect([for (final e in local) e['type']], ['local.request']);
    await s.sync.applyEvent({
      'type': 'chat_request.removed',
      'at': '2026-11-16T09:00:00.000Z',
      'id': 'r1',
    });
    held = await s.db.chatRequests();
    expect([for (final r in held) r.id], ['r2']);
    await sub.cancel();
    unawaited(Future<void>.value());
  });
}
