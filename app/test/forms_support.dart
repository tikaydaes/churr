import 'dart:convert';

import 'package:churr/l10n/app_localizations.dart';
import 'package:churr/core/api/client.dart';
import 'package:churr/core/db/app_db.dart';
import 'package:churr/core/db/cache_db.dart';
import 'package:churr/core/models/account.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:churr/core/models/server.dart';
import 'package:churr/core/session_store.dart';
import 'package:churr/state/server_state.dart';
import 'package:churr/state/app_state.dart';
import 'package:churr/ui/scopes.dart';
import 'package:churr/ui/theme/theme.dart';
import 'package:drift/drift.dart' show driftRuntimeOptions;
import 'package:drift/native.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// Many in-memory databases are opened on purpose in these tests.
void quietDrift() => driftRuntimeOptions.dontWarnAboutMultipleDatabases = true;

/// An [AppState] holding already-started servers, with no storage or
/// network of its own.
AppState appWith(List<ServerState> servers) {
  final state = AppState(
    appDb: AppDb(NativeDatabase.memory()),
    storage: _NoStorage(),
    secrets: MemorySecretStore(),
    connectOnStart: false,
  );
  state.servers.addAll(servers);
  state.loaded = true;
  return state;
}

class _NoStorage implements ServerStorage {
  @override
  Future<CacheDb> openCache(String localId) async =>
      CacheDb(NativeDatabase.memory());
  @override
  Future<String?> filesDirectory(String localId) async => null;
  @override
  Future<void> delete(String localId) async {}
}

/// Draws [child] in the app's theme with [state] above it, at [size].
Future<void> pumpScreen(
  WidgetTester tester,
  AppState state,
  Widget child, {
  Size size = const Size(1280, 800),
  bool dark = false,
}) async {
  tester.view.devicePixelRatio = 1;
  tester.view.physicalSize = size;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    AppScope(
      state: state,
      child: MaterialApp(
        localizationsDelegates: L10n.localizationsDelegates,
        supportedLocales: L10n.supportedLocales,
        theme: buildTheme(dark: dark),
        home: Scaffold(body: child),
      ),
    ),
  );
  await settleAsync(tester);
}

/// Lets database streams, fake requests and the frames they cause settle.
Future<void> settleAsync(WidgetTester tester) async {
  for (var i = 0; i < 6; i++) {
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 20)),
    );
    await tester.pump(const Duration(milliseconds: 50));
  }
}

const phone = Size(390, 844);
const desktop = Size(1280, 800);

/// Requests seen by a fake server, and how it answers them.
class FakeServer {
  FakeServer(this.answer);

  /// Returns the status and body for a request; null means 404.
  final (int, Object?)? Function(http.Request r) answer;
  final requests = <http.Request>[];

  http.Client get client => MockClient((r) async {
    requests.add(r);
    final a = answer(r);
    if (a == null) {
      return http.Response(
        jsonEncode({
          'error': {'code': 'not_found', 'message': 'Not found.'},
        }),
        404,
      );
    }
    return http.Response(
      a.$2 == null ? '' : jsonEncode(a.$2),
      a.$1,
      headers: {'content-type': 'application/json'},
    );
  });
}

Json body(http.Request r) =>
    r.body.isEmpty ? {} : (jsonDecode(r.body) as Map).cast<String, dynamic>();

typedef Json = Map<String, dynamic>;

Conversation channel(
  String id,
  String name, {
  String level = 'member',
  bool private = false,
  String? parent,
  String? section,
  bool member = true,
  int members = 5,
  String? lockedAt,
}) => Conversation.fromJson({
  'id': id,
  'kind': 'channel',
  'private': private,
  'name': name,
  'topic': '',
  'parent_id': ?parent,
  'section_id': ?section,
  'created_at': '2026-03-01T00:00:00.000Z',
  'last_seq': 10,
  'member_count': members,
  'locked_at': ?lockedAt,
  'default_level': 'member',
  'slow_mode_seconds': 0,
  if (member)
    'membership': {
      'level': level,
      'read_seq': 10,
      'mode': 'mentions',
      'unread_count': 0,
      'mention_count': 0,
      'badge_count': 0,
    },
});

/// A started [ServerState] on an in-memory database, talking to [fake].
Future<ServerState> fakeServerState(
  FakeServer fake, {
  String id = 'server-0',
  String name = 'Work',
  List<Conversation> conversations = const [],
  String role = 'member',
}) async {
  final db = CacheDb(NativeDatabase.memory());
  await db.putMe(
    Me.fromJson({
      'id': 'me',
      'username': 'kai',
      'display_name': 'Kai',
      'role': role,
    }),
  );
  await db.upsertAccounts([
    Account.fromJson({
      'id': 'me',
      'username': 'kai',
      'display_name': 'Kai',
      'role': role,
    }),
    Account.fromJson({
      'id': 'sam',
      'username': 'sam',
      'display_name': 'Sam Okafor',
      'role': 'member',
    }),
    Account.fromJson({
      'id': 'lee',
      'username': 'lee',
      'display_name': 'Lee',
      'role': 'member',
    }),
  ]);
  await db.replaceSections([Section(id: 's1', name: 'Projects', position: 1)]);
  await db.replaceGroups([Group(id: 'g1', handle: 'design', isMember: true)]);
  for (final c in conversations) {
    await db.upsertConversation(c, syncedSeq: c.lastSeq);
  }
  final s = ServerState(
    entry: ServerEntry(
      localId: id,
      address: 'https://$id.example.org',
      name: name,
      position: 0,
    ),
    db: db,
    api: ApiClient(
      base: Uri.parse('https://$id.example.org'),
      httpClient: fake.client,
      tokens: Tokens(
        access: 'a',
        refresh: 'r',
        expiresAt: DateTime.now().add(const Duration(hours: 1)),
      ),
    ),
  );
  await s.start(connect: false);
  await Future<void>.delayed(const Duration(milliseconds: 30));
  return s;
}
