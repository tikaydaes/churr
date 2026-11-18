import 'dart:convert';

import 'package:churr/core/db/app_db.dart';
import 'package:churr/core/db/cache_db.dart';
import 'package:churr/core/models/account.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/core/session_store.dart';
import 'package:churr/l10n/app_localizations.dart';
import 'package:churr/state/app_state.dart';
import 'package:churr/state/server_state.dart';
import 'package:churr/ui/scopes.dart';
import 'package:churr/ui/screens/conversation/conversation_screen.dart';
import 'package:churr/ui/theme/theme.dart';
import 'package:drift/drift.dart' show driftRuntimeOptions;
import 'package:drift/native.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// Test support for the conversation screen: one server on in-memory
/// databases, a fake server that records requests and answers from a
/// table, and the screen pumped on its own.

const general = '0193a6f0-0000-7000-8000-000000000001';
const design = '0193a6f0-0000-7000-8000-000000000002';
const design2 = '0193a6f0-0000-7000-8000-000000000003';

class Recorded {
  Recorded(this.method, this.path, this.body);
  final String method;
  final String path;
  final Object? body;
  @override
  String toString() => '$method $path $body';
}

/// A fake server: answers by method and path prefix, records requests.
class FakeServer {
  final requests = <Recorded>[];
  final answers = <String, Object? Function(http.Request)>{};

  /// Answers requests that start with [methodAndPath]. The answer may be
  /// a Future, for one that does something before it answers.
  void on(String methodAndPath, Object? Function(http.Request r) answer) =>
      answers[methodAndPath] = answer;

  http.Client client() => MockClient((r) async {
    Object? body;
    if (r.body.isNotEmpty &&
        (r.headers['content-type'] ?? '').contains('json')) {
      body = jsonDecode(r.body);
    }
    requests.add(
      Recorded(
        r.method,
        r.url.path,
        body ?? (r.body.isEmpty ? null : '<${r.headers['content-type']}>'),
      ),
    );
    final key = answers.keys
        .where((k) => '${r.method} ${r.url.path}'.startsWith(k))
        .fold<String?>(
          null,
          (best, k) => best == null || k.length > best.length ? k : best,
        );
    var out = key == null ? _default(r) : answers[key]!(r);
    if (out is Future) out = await out;
    if (out is http.Response) return out;
    return http.Response(
      jsonEncode(out ?? {}),
      200,
      headers: {'content-type': 'application/json'},
    );
  });

  Object? _default(http.Request r) {
    final p = r.url.path;
    if (p.endsWith('/messages') && r.method == 'GET') {
      return {'messages': [], 'thread_states': [], 'has_more': false};
    }
    if (p.endsWith('/thread')) {
      return {'parent': {}, 'messages': [], 'has_more': false};
    }
    if (p == '/api/v1/commands') {
      return {
        'commands': [
          {'name': 'poll', 'kind': 'builtin', 'description': 'Ask a question'},
          {
            'name': 'standup',
            'kind': 'saved_reply',
            'description': 'Post the standup template',
          },
          {
            'name': 'deploy',
            'kind': 'bot',
            'description': 'Deploy a branch',
            'hint': '<branch>',
          },
          // The dice module's, which the server runs.
          {
            'name': 'roll',
            'kind': 'module',
            'module': 'dice',
            'description': 'Rolls dice',
            'hint': '[dice]',
          },
        ],
      };
    }
    // The read position: answered as refused, so the cached membership
    // (with its thread view) stays as the test set it.
    if (p.endsWith('/read') && r.method == 'PUT') {
      return http.Response(
        jsonEncode({
          'error': {'code': 'internal', 'message': 'test'},
        }),
        500,
      );
    }
    if (p == '/api/v1/server') {
      return {
        'limits': {
          'file_size': 1000,
          'message_text': 16000,
          'quick_correction_seconds': 30,
        },
      };
    }
    return {};
  }

  List<Recorded> where(String method, String pathPart) => [
    for (final r in requests)
      if (r.method == method && r.path.contains(pathPart)) r,
  ];
}

Conversation channel(
  String id,
  String name, {
  String? parent,
  int members = 5,
  String level = 'member',
  bool joined = true,
  String? threadView,
  bool private = false,
}) => Conversation.fromJson({
  'id': id,
  'kind': 'channel',
  'private': private,
  'name': name,
  'parent_id': ?parent,
  'created_at': '2026-03-01T00:00:00.000Z',
  'last_seq': 30,
  'member_count': members,
  if (joined)
    'membership': {
      'level': level,
      'read_seq': 20,
      'mode': 'mentions',
      'thread_view': ?threadView,
      'unread_count': 0,
      'mention_count': 0,
      'badge_count': 0,
    },
});

Message msg(
  String id,
  int seq,
  String text, {
  String author = 'sam',
  String? parent,
  Map<String, Object?>? thread,
  Map<String, Object?>? quote,
  Map<String, Object?> extra = const {},
  String conv = general,
}) => Message.fromJson({
  'id': id,
  'conversation_id': conv,
  'seq': seq,
  'author_id': author,
  'parent_id': ?parent,
  'text': text,
  'thread': ?thread,
  'quote': ?quote,
  'created_at': DateTime.utc(2026, 11, 13, 9, seq).toIso8601String(),
  // In a conversation that is not shared, `order` follows `seq`.
  'order': 'o${seq.toString().padLeft(6, '0')}',
  ...extra,
});

/// A cache as if an earlier run had synced: two people, three channels
/// (two named "Design"), a thread with replies.
Future<CacheDb> conversationCache({
  String? threadView,
  String meView = 'threaded',
}) async {
  driftRuntimeOptions.dontWarnAboutMultipleDatabases = true;
  final db = CacheDb(NativeDatabase.memory());
  final me = {
    'id': 'me',
    'username': 'kai',
    'display_name': 'Kai Tanaka',
    'role': 'member',
    'prefs': {'thread_view': meView},
  };
  await db.putMe(Me.fromJson(me));
  await db.upsertAccounts([
    Account.fromJson(me),
    Account.fromJson({
      'id': 'sam',
      'username': 'sam',
      'display_name': 'Sam Okafor',
      'role': 'member',
      'presence': 'online',
    }),
    Account.fromJson({
      'id': 'lee',
      'username': 'lee',
      'display_name': 'Lee Park',
      'role': 'member',
      'dnd': true,
    }),
  ]);
  for (final c in [
    channel(general, 'General', threadView: threadView),
    channel(design, 'Design', members: 14),
    channel(design2, 'Design', parent: general, members: 3),
  ]) {
    await db.upsertConversation(c, syncedSeq: c.lastSeq);
  }
  await db.setHistoryComplete(general, true);
  await db.upsertMessages([
    msg(
      'm1',
      10,
      'Deploy plan for Friday',
      thread: {
        'reply_count': 2,
        'last_reply_seq': 24,
        'last_reply_at': '2026-11-13T09:24:00.000Z',
        'participant_ids': ['lee'],
      },
    ),
    msg('m2', 21, 'Lunch?', author: 'lee'),
    msg('r1', 22, 'I can do Friday', author: 'lee', parent: 'm1'),
    msg('m3', 23, 'Sounds good'),
    msg(
      'r2',
      24,
      'Me too',
      author: 'lee',
      parent: 'm1',
      quote: {'message_id': 'r1', 'start': 0, 'end': 5},
    ),
  ]);
  return db;
}

class _Storage implements ServerStorage {
  _Storage(this.db);
  final CacheDb db;
  @override
  Future<CacheDb> openCache(String localId) async => db;
  @override
  Future<String?> filesDirectory(String localId) async => null;
  @override
  Future<void> delete(String localId) async {}
}

/// An app with one server whose cache is [db], talking to [fake].
Future<AppState> conversationApp(
  WidgetTester tester,
  CacheDb db,
  FakeServer fake,
) async {
  late AppState state;
  await tester.runAsync(() async {
    final appDb = AppDb(NativeDatabase.memory());
    final secrets = MemorySecretStore();
    await appDb.addServer(
      ServerEntry(
        localId: 's',
        address: 'https://chat.example.org',
        name: 'Work',
        position: 0,
      ),
    );
    await SessionStore(secrets).save(
      's',
      Tokens(
        access: 'a',
        refresh: 'r',
        expiresAt: DateTime.now().add(const Duration(hours: 1)),
      ),
    );
    state = AppState(
      appDb: appDb,
      storage: _Storage(db),
      secrets: secrets,
      connectOnStart: false,
      httpClientFactory: fake.client,
    );
    await state.load();
    state.openConversation(const OpenTarget('s', general));
  });
  return state;
}

ServerState serverOf(AppState app) => app.server('s')!;

/// Lets database streams and the frames they cause settle.
Future<void> settle(WidgetTester tester, [int rounds = 6]) async {
  for (var i = 0; i < rounds; i++) {
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 20)),
    );
    await tester.pump(const Duration(milliseconds: 50));
  }
}

Future<void> setSize(WidgetTester tester, Size s) async {
  tester.view.devicePixelRatio = 1;
  tester.view.physicalSize = s;
  addTearDown(tester.view.reset);
}

const phone = Size(390, 800);
const desktop = Size(1440, 900);

/// Pumps the conversation screen alone, as the shell would place it.
Future<void> pumpConversation(
  WidgetTester tester,
  AppState app, {
  String id = general,
  TargetPlatform? platform,
}) async {
  final server = serverOf(app);
  await tester.pumpWidget(
    AppScope(
      state: app,
      child: MaterialApp(
        localizationsDelegates: L10n.localizationsDelegates,
        supportedLocales: L10n.supportedLocales,
        theme: buildTheme(dark: false).copyWith(platform: platform),
        home: Scaffold(
          body: ConversationScreen(
            key: ValueKey(id),
            server: server,
            conversationId: id,
          ),
        ),
      ),
    ),
  );
  await settle(tester);
}

extension ErrorAnswer on Map<String, Object?> {
  /// This body as an answer with [status].
  http.Response toResponse(int status) => http.Response(
    jsonEncode(this),
    status,
    headers: {'content-type': 'application/json'},
  );
}
