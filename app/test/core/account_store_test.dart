import 'dart:convert';

import 'package:churr/core/account_sync.dart';
import 'package:churr/core/api/client.dart';
import 'package:churr/core/api/errors.dart';
import 'package:churr/core/session_store.dart';
import 'package:churr/core/db/account_tables.dart';
import 'package:churr/core/db/cache_db.dart';
import 'package:churr/core/models/account.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/core/models/personal.dart';
import 'package:churr/core/oauth.dart';
import 'package:churr/core/oauth_metadata.dart';
import 'package:churr/core/setup_code.dart';
import 'package:churr/core/sync.dart';
import 'package:drift/native.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:sqlite3/sqlite3.dart';

Message msg(
  String id,
  int seq, {
  String conv = 'c1',
  String? parent,
  String text = 'hi',
  String author = 'sam',
  List<Map<String, dynamic>> entities = const [],
}) => Message.fromJson({
  'id': id,
  'conversation_id': conv,
  'seq': seq,
  'author_id': author,
  'parent_id': ?parent,
  'text': text,
  'entities': entities,
  'created_at': DateTime.utc(2026, 11, 13, 9, 0, seq).toIso8601String(),
});

Conversation conv(
  String id, {
  int lastSeq = 10,
  int badge = 0,
  int readSeq = 0,
  String mode = 'mentions',
}) => Conversation.fromJson({
  'id': id,
  'kind': 'channel',
  'private': false,
  'name': id,
  'last_seq': lastSeq,
  'membership': {
    'level': 'member',
    'read_seq': readSeq,
    'mode': mode,
    'badge_count': badge,
    'unread_count': badge,
  },
});

void main() {
  group('cache.db version 2', () {
    test('a version 1 file is upgraded and keeps its rows', () async {
      // A file as version 1 left it: the notifications table without the
      // new columns, and no bookmarks or presets.
      final raw = sqlite3.openInMemory();
      for (final s in [
        'CREATE TABLE me (key TEXT PRIMARY KEY, json TEXT NOT NULL)',
        'CREATE TABLE accounts (id TEXT PRIMARY KEY, json TEXT NOT NULL)',
        'CREATE TABLE groups (id TEXT PRIMARY KEY, json TEXT NOT NULL)',
        'CREATE TABLE emoji (id TEXT PRIMARY KEY, json TEXT NOT NULL)',
        'CREATE TABLE sections (id TEXT PRIMARY KEY, position INTEGER NOT NULL, json TEXT NOT NULL)',
        'CREATE TABLE conversations (id TEXT PRIMARY KEY, json TEXT NOT NULL, synced_seq INTEGER NOT NULL DEFAULT 0, history_complete INTEGER NOT NULL DEFAULT 0, opened_at INTEGER)',
        'CREATE TABLE messages (id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, seq INTEGER, parent_id TEXT, created_at INTEGER NOT NULL, json TEXT NOT NULL)',
        'CREATE TABLE thread_states (parent_id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, json TEXT NOT NULL)',
        'CREATE TABLE notifications (message_id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, seq INTEGER NOT NULL, at INTEGER NOT NULL, kind TEXT NOT NULL, cleared_at INTEGER)',
        'CREATE TABLE drafts (conversation_id TEXT NOT NULL, parent_id TEXT NOT NULL DEFAULT \'\', text TEXT NOT NULL, PRIMARY KEY (conversation_id, parent_id))',
        'CREATE TABLE outbox (client_id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, parent_id TEXT, text TEXT NOT NULL, created_at INTEGER NOT NULL, state TEXT NOT NULL, error TEXT)',
        'CREATE TABLE files (id TEXT PRIMARY KEY, path TEXT NOT NULL, size INTEGER NOT NULL, opened_at INTEGER NOT NULL)',
        'CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)',
        "INSERT INTO notifications VALUES ('old', 'c1', 3, 1000, 'notify', NULL)",
        'PRAGMA user_version = 1',
      ]) {
        raw.execute(s);
      }
      final db = CacheDb(NativeDatabase.opened(raw));
      final items = await db.notifications(clearedSince: DateTime(2000));
      expect(items.single.messageId, 'old');
      expect(items.single.message, isNull);
      await db.putBookmark('m1', DateTime.utc(2026), message: msg('m1', 1));
      expect((await db.bookmarks()).single.message!.text, 'hi');
      await db.close();
    });
  });

  group('cache.db version 4', () {
    test('a version 3 file gets ord from each message\'s order; a conversation with a message that has none loses its cached run', () async {
      final raw = sqlite3.openInMemory();
      String m(String id, String c, int seq, [String? order]) => jsonEncode({
        'id': id,
        'conversation_id': c,
        'seq': seq,
        'order': ?order,
        'author_id': 'sam',
        'text': id,
        'created_at': '2026-11-13T09:00:00.000Z',
      });
      for (final s in [
        'CREATE TABLE conversations (id TEXT PRIMARY KEY, json TEXT NOT NULL, synced_seq INTEGER NOT NULL DEFAULT 0, history_complete INTEGER NOT NULL DEFAULT 0, opened_at INTEGER)',
        'CREATE TABLE messages (id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, seq INTEGER, parent_id TEXT, created_at INTEGER NOT NULL, json TEXT NOT NULL)',
        'CREATE INDEX messages_by_seq ON messages (conversation_id, seq)',
        'CREATE INDEX messages_by_parent ON messages (parent_id)',
        'PRAGMA user_version = 3',
      ]) {
        raw.execute(s);
      }
      raw.execute(
        'INSERT INTO conversations (id, json, synced_seq, history_complete) VALUES (?, ?, 9, 1), (?, ?, 9, 1)',
        [
          'c1',
          jsonEncode(conv('c1').toJson()),
          'c2',
          jsonEncode(conv('c2').toJson()),
        ],
      );
      for (final (id, c, seq, order) in [
        // c1: the server already sent `order`; m3 arrived late.
        ('m1', 'c1', 1, 'b'),
        ('m2', 'c1', 2, 'd'),
        ('m3', 'c1', 3, 'c'),
        // c2: one message from before `order`.
        ('n1', 'c2', 1, 'a'),
        ('n2', 'c2', 2, null),
      ]) {
        raw.execute(
          'INSERT INTO messages (id, conversation_id, seq, created_at, json) VALUES (?, ?, ?, 0, ?)',
          [id, c, seq, m(id, c, seq, order)],
        );
      }
      final db = CacheDb(NativeDatabase.opened(raw));
      expect((await db.messages('c1')).map((m) => m.id), ['m1', 'm3', 'm2']);
      expect((await db.conversation('c1'))!.historyComplete, isTrue);
      expect(await db.messages('c2'), isEmpty);
      expect(
        (await db.conversation('c2'))!.historyComplete,
        isFalse,
        reason: 'fetched again when next opened',
      );
      expect((await db.conversation('c2'))!.syncedSeq, 9);
      final index = await db.rows(
        "SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'messages_by_ord'",
      );
      expect(index, hasLength(1));
      await db.close();
    });

    test('a new file sorts by order from the start', () async {
      final db = CacheDb(NativeDatabase.memory());
      Message o(String id, int seq, String order) =>
          Message.fromJson({...msg(id, seq).raw, 'order': order});
      await db.upsertMessages([o('a', 1, 'b'), o('b', 2, 'a')]);
      expect((await db.messages('c1')).map((m) => m.id), ['b', 'a']);
      await db.close();
    });
  });

  group('the notifications list in the store', () {
    late CacheDb db;
    setUp(() => db = CacheDb(NativeDatabase.memory()));
    tearDown(() => db.close());

    test(
      'keeps the message, and clears by the conversation and by the thread',
      () async {
        await db.addNotification(msg('a', 5), 'notify');
        await db.addNotification(msg('r1', 6, parent: 'p1'), 'notify');
        await db.addNotification(msg('r2', 7, parent: 'p2'), 'badge');
        // p1 is followed (it has a thread state); p2 is not.
        await db.upsertThreadStates([
          ThreadState(
            parentId: 'p1',
            conversationId: 'c1',
            state: 'following',
            readSeq: 0,
            unreadCount: 1,
          ),
        ]);
        final now = DateTime.utc(2026, 11, 14);

        await db.clearNotifications('c1', 10, now);
        var open = (await db.notifications(clearedSince: now))
            .where((n) => !n.cleared)
            .map((n) => n.messageId);
        // The conversation's position passes top-level items and replies in
        // threads with no position of their own, not the followed thread.
        expect(open, ['r1']);

        await db.clearThreadNotifications('p1', 6, now);
        open = (await db.notifications(clearedSince: now))
            .where((n) => !n.cleared)
            .map((n) => n.messageId);
        expect(open, isEmpty);
        final all = await db.notifications(clearedSince: now);
        expect(
          all.first.message!.text,
          'hi',
          reason: 'cleared items stay under Earlier',
        );
      },
    );

    test(
      'sync clears a thread\'s replies when its read position moves',
      () async {
        final sync = SyncEngine(
          db: db,
          api: ApiClient(
            base: Uri.parse('https://x.example'),
            httpClient: MockClient((_) async => http.Response('{}', 200)),
          ),
        );
        await db.upsertThreadStates([
          ThreadState(
            parentId: 'p1',
            conversationId: 'c1',
            state: 'following',
            readSeq: 0,
            unreadCount: 1,
          ),
        ]);
        await db.addNotification(msg('r1', 6, parent: 'p1'), 'notify');
        await sync.applyEvent({
          'type': 'thread_state.changed',
          'conversation_id': 'c1',
          'parent_id': 'p1',
          'thread_state': {
            'parent_id': 'p1',
            'conversation_id': 'c1',
            'state': 'following',
            'read_seq': 6,
            'unread_count': 0,
          },
        });
        expect(
          (await db.notifications(clearedSince: DateTime(2000))).single.cleared,
          isTrue,
        );
      },
    );
  });

  group('trimming', () {
    test('keeps the newest 500 of a conversation opened this month, 50 of an idle one, all of one opened this week', () async {
      final db = CacheDb(NativeDatabase.memory());
      final now = DateTime(2026, 11, 14);
      for (final (id, opened) in [
        ('week', now.subtract(const Duration(days: 2))),
        ('month', now.subtract(const Duration(days: 20))),
        ('idle', null),
      ]) {
        await db.upsertConversation(conv(id, lastSeq: 700), syncedSeq: 700);
        await db.upsertMessages([
          for (var i = 1; i <= 700; i++) msg('$id-$i', i, conv: id),
        ]);
        if (opened != null) await db.markOpened(id, opened);
        await db.setHistoryComplete(id, true);
      }
      final removed = await db.trim(now);
      expect(await db.messageCount('week'), 700);
      expect(await db.messageCount('month'), 500);
      expect(await db.messageCount('idle'), 50);
      expect(removed, 200 + 650);
      // What is kept is the newest, an unbroken run, and older history can
      // be fetched again.
      expect(await db.oldestSeq('month'), 201);
      expect((await db.conversation('month'))!.historyComplete, isFalse);
      await db.close();
    });

    test('drops cleared notifications older than a week', () async {
      final db = CacheDb(NativeDatabase.memory());
      final now = DateTime(2026, 11, 14);
      await db.addNotification(msg('a', 1), 'notify');
      await db.addNotification(msg('b', 2), 'notify');
      await db.clearNotifications(
        'c1',
        1,
        now.subtract(const Duration(days: 8)),
      );
      await db.trim(now);
      expect(
        (await db.notifications(clearedSince: DateTime(2000)))
            .map((n) => n.messageId),
        ['b'],
      );
      await db.close();
    });
  });

  group('account events', () {
    late CacheDb db;
    setUp(() async {
      db = CacheDb(NativeDatabase.memory());
      await db.putMe(
        Me.fromJson({
          'id': 'me',
          'username': 'kai',
          'display_name': 'Kai',
          'role': 'owner',
        }),
      );
      await db.upsertAccounts([
        Account.fromJson({
          'id': 'me',
          'username': 'kai',
          'display_name': 'Kai',
          'role': 'owner',
        }),
        Account.fromJson({
          'id': 'sam',
          'username': 'sam',
          'display_name': 'Sam',
          'role': 'member',
        }),
      ]);
    });
    tearDown(() => db.close());

    test('status, availability and do not disturb reach the account and my own row', () async {
      await applyAccountEvent(db, 'status.changed', {
        'account_id': 'me',
        'status': {
          'emoji': '🌴',
          'text': 'Away',
          'pause_notifications': true,
          'appear_away': false,
        },
      }, myId: 'me');
      await applyAccountEvent(db, 'dnd.changed', {
        'account_id': 'me',
        'dnd': true,
        'dnd_state': {
          'on': true,
          'sources': ['status'],
        },
      }, myId: 'me');
      await applyAccountEvent(db, 'availability.changed', {
        'account_id': 'sam',
        'availability': {
          'value': 'out_of_office',
          'until': '2026-11-20T00:00:00.000Z',
        },
      }, myId: 'me');
      final me = (await db.me())!;
      expect(me.fullStatus!.pauseNotifications, isTrue);
      expect(me.dndState.sources, ['status']);
      expect(me.dndOn, isTrue);
      expect((await db.account('me'))!.fullStatus!.text, 'Away');
      expect((await db.account('sam'))!.availability!.outOfOffice, isTrue);
      await applyAccountEvent(db, 'status.changed', {
        'account_id': 'me',
      }, myId: 'me');
      expect(
        (await db.me())!.fullStatus,
        isNull,
        reason: 'absent means cleared',
      );
    });

    test('blocks and bookmarks', () async {
      await applyAccountEvent(db, 'block.added', {
        'account_id': 'sam',
      }, myId: 'me');
      expect((await db.me())!.blockedIds, ['sam']);
      await applyAccountEvent(db, 'block.removed', {
        'account_id': 'sam',
      }, myId: 'me');
      expect((await db.me())!.blockedIds, isEmpty);
      await db.upsertMessages([msg('m1', 1)]);
      await applyAccountEvent(db, 'bookmark.added', {
        'message_id': 'm1',
        'created_at': '2026-11-14T10:00:00.000Z',
      });
      expect((await db.bookmarks()).single.message!.id, 'm1');
      await applyAccountEvent(db, 'bookmark.removed', {'message_id': 'm1'});
      expect(await db.bookmarks(), isEmpty);
    });

    test(
      'the notifications list is filled in from what the server says now',
      () async {
        await db.upsertConversation(
          conv('loud', badge: 2, readSeq: 3, mode: 'any'),
          syncedSeq: 10,
        );
        await db.upsertConversation(
          conv('quiet', badge: 1, readSeq: 3),
          syncedSeq: 10,
        );
        await db.upsertConversation(conv('read'), syncedSeq: 10);
        final asked = <String>[];
        final api = ApiClient(
          base: Uri.parse('https://x.example'),
          tokens: Tokens(
            access: 'a',
            refresh: 'r',
            expiresAt: DateTime.now().add(const Duration(hours: 1)),
          ),
          httpClient: MockClient((r) async {
            asked.add('${r.url.path}?${r.url.query}');
            final id = r.url.pathSegments[3];
            final messages = id == 'loud'
                ? [
                    msg('l1', 4, conv: 'loud').raw,
                    msg('l2', 5, conv: 'loud').raw,
                  ]
                : [
                    msg('q1', 4, conv: 'quiet').raw,
                    msg(
                      'q2',
                      5,
                      conv: 'quiet',
                      entities: [
                        {
                          'type': 'account',
                          'at': 0,
                          'len': 4,
                          'account_id': 'me',
                        },
                      ],
                    ).raw,
                  ];
            return http.Response(
              jsonEncode({
                'messages': messages,
                'thread_states': [],
                'has_more': false,
              }),
              200,
            );
          }),
        );
        final n = await backfillNotifications(api, db);
        expect(n, 3, reason: 'both under any, only the mention under mentions');
        expect(asked.every((a) => a.contains('after=3')), isTrue);
        expect(
          asked.any((a) => a.contains('/read/')),
          isFalse,
          reason: 'conversations without a badge are not asked',
        );
        final items = await db.notifications(clearedSince: DateTime(2000));
        expect(
          items.firstWhere((i) => i.messageId == 'q2').reason,
          NotificationReason.mentionedYou.name,
        );
      },
    );
  });

  group('OAuth metadata', () {
    final base = Uri.parse('https://chat.example.org');
    Map<String, dynamic> doc([Map<String, dynamic> over = const {}]) => {
      'issuer': 'https://chat.example.org',
      'authorization_endpoint': 'https://chat.example.org/oauth/authorize',
      'token_endpoint': 'https://chat.example.org/oauth/token',
      'revocation_endpoint': 'https://chat.example.org/oauth/revoke',
      'code_challenge_methods_supported': ['S256'],
      'grant_types_supported': [
        'authorization_code',
        'refresh_token',
        'urn:churr:params:oauth:grant-type:pairing-code',
      ],
      ...over,
    };

    test('reads the endpoints and refuses ones on another server', () {
      final m = OAuthMetadata.parse(doc(), base);
      expect(
        m.tokenEndpoint.toString(),
        'https://chat.example.org/oauth/token',
      );
      expect(m.pairing, isTrue);
      expect(
        () => OAuthMetadata.parse(
          doc({'token_endpoint': 'https://evil.example/token'}),
          base,
        ),
        throwsA(isA<OAuthException>()),
      );
      expect(
        () =>
            OAuthMetadata.parse(doc({'issuer': 'https://other.example'}), base),
        throwsA(isA<OAuthException>()),
      );
      expect(
        () => OAuthMetadata.parse(
          doc({
            'code_challenge_methods_supported': ['plain'],
          }),
          base,
        ),
        throwsA(isA<OAuthException>()),
      );
    });

    test(
      'token requests and the authorization page use the endpoints it names',
      () async {
        final posted = <String>[];
        final api = ApiClient(
          base: base,
          httpClient: MockClient((r) async {
            if (r.url.path.contains('well-known')) {
              return http.Response(
                jsonEncode(
                  doc({
                    'token_endpoint': '/auth/t2',
                    'authorization_endpoint': '/auth/a2',
                  }),
                ),
                200,
              );
            }
            posted.add(r.url.path);
            return http.Response(
              jsonEncode({
                'access_token': 'churr_app_x',
                'refresh_token': 'churr_ref_y',
                'token_type': 'Bearer',
                'expires_in': 3600,
              }),
              200,
            );
          }),
        );
        final m = await fetchOAuthMetadata(api);
        final pkce = Pkce.generate();
        final url = api.authorizeUrl(
          pkce: pkce,
          redirectUri: 'http://127.0.0.1:5/callback',
          deviceName: 'd',
          platform: 'macos',
          endpoint: m.authorizationEndpoint,
        );
        expect(url.path, '/auth/a2');
        expect(url.queryParameters['code_challenge_method'], 'S256');
        await api.exchangeCode(
          code: 'c',
          pkce: pkce,
          redirectUri: 'http://127.0.0.1:5/callback',
        );
        expect(posted, ['/auth/t2']);
      },
    );
  });

  group('set-up codes', () {
    test('travel in parts, in any order, and carry nothing secret', () {
      final p = SetupPayload(
        prefs: {'layout': 'hybrid', 'default_show': 'activity'},
        servers: ['https://a.example', 'https://b.example'],
        groups: [('g1', 'Daily', 0)],
        items: [
          for (var i = 0; i < 200; i++)
            ('https://a.example', 'conversation-$i-${'x' * 20}', 'g1', i),
        ],
      );
      final codes = p.encode(partSize: 300);
      expect(codes.length, greaterThan(1));
      expect(codes.every((c) => c.startsWith('churr://setup?')), isTrue);
      final c = SetupCollector();
      for (final code in codes.reversed) {
        expect(c.add(code), isTrue);
      }
      final back = c.payload!;
      expect(back.prefs['layout'], 'hybrid');
      expect(back.servers, p.servers);
      expect(back.items.length, 200);
      expect(back.groups.single.$2, 'Daily');
      expect(jsonEncode(back.toJson()), isNot(contains('churr_')));
    });

    test('a pairing code is told apart', () {
      final pc = PairingCode.parse(
        'churr://pair?server=https%3A%2F%2Fchat.example.org&code=abc',
      );
      expect(pc!.server, 'https://chat.example.org');
      expect(pc.code, 'abc');
      expect(PairingCode.parse('https://example.org'), isNull);
      expect(SetupCollector().add('churr://pair?server=x&code=y'), isFalse);
    });
  });
}
