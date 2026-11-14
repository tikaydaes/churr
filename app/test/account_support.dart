import 'dart:convert';

import 'package:churr/core/db/app_db.dart';
import 'package:churr/core/db/cache_db.dart';
import 'package:churr/core/models/account.dart';
import 'package:churr/core/session_store.dart';
import 'package:churr/l10n/app_localizations.dart';
import 'package:churr/state/account_hub.dart';
import 'package:churr/state/app_state.dart';
import 'package:churr/ui/scopes.dart';
import 'package:churr/ui/theme/theme.dart';
import 'package:drift/native.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'ui/shell_test.dart' show MemoryStorage, seededCache;

/// A fake server for the account area's tests: answers by method and
/// path, and records every request.
class FakeServer {
  final requests = <http.Request>[];

  /// `"PUT /api/v1/me/status"` → handler. Unlisted requests get `{}`.
  final routes = <String, http.Response Function(http.Request)>{};

  /// Servers that cannot be reached: every request fails.
  bool down = false;

  void on(String route, Object? body, {int status = 200}) =>
      routes[route] = (_) => http.Response(
        jsonEncode(body),
        status,
        headers: {'content-type': 'application/json'},
      );

  List<String> get calls => [
    for (final r in requests) '${r.method} ${r.url.path}',
  ];

  http.Client client() => MockClient((r) async {
    if (down) throw http.ClientException('unreachable');
    requests.add(r);
    final h = routes['${r.method} ${r.url.path}'];
    if (h != null) return h(r);
    if (r.method == 'GET' && r.url.path == '/api/v1/me') {
      return http.Response(jsonEncode({'account': meJson()}), 200);
    }
    return http.Response('{}', 200);
  });
}

Map<String, dynamic> meJson({
  String id = 'me',
  Map<String, dynamic> extra = const {},
}) => {
  'id': id,
  'username': 'kai',
  'display_name': 'Kai Tanaka',
  'role': 'owner',
  'prefs': {
    'thread_view': 'threaded',
    'meeting_pauses': false,
    'allow_notify_anyway': true,
  },
  'dnd_state': {'on': false, 'sources': []},
  ...extra,
};

/// An app with [servers] servers on in-memory databases, each answered
/// by its own [FakeServer]. Nothing touches the network.
Future<(AppState, List<FakeServer>)> makeAccountApp(
  WidgetTester? tester, {
  int servers = 2,
}) async {
  late AppState state;
  final fakes = [for (var i = 0; i < servers; i++) FakeServer()];
  Future<void> build() async {
    final appDb = AppDb(NativeDatabase.memory());
    final storage = MemoryStorage();
    final secrets = MemorySecretStore();
    for (var i = 0; i < servers; i++) {
      final id = 'server-$i';
      await appDb.addServer(
        ServerEntry(
          localId: id,
          address: 'https://s$i.example.org',
          name: i == 0 ? 'Work' : 'Friends',
          position: i,
        ),
      );
      final db = await seededCache(prefix: i == 0 ? '' : 'Friends ');
      await db.putMe(Me.fromJson(meJson()));
      storage.caches[id] = db;
      await SessionStore(secrets).save(
        id,
        Tokens(
          access: 'a',
          refresh: 'r',
          expiresAt: DateTime.now().add(const Duration(hours: 1)),
        ),
      );
    }
    var n = 0;
    state = AppState(
      appDb: appDb,
      storage: storage,
      secrets: secrets,
      connectOnStart: false,
      // Each server is opened in order, and each gets its own fake.
      httpClientFactory: () => _Routed(fakes, () => n++),
    );
    await state.load();
    AccountHub.install(state, AccountHub(state, deviceZone: () async => null));
  }

  if (tester != null) {
    await tester.runAsync(build);
  } else {
    await build();
  }
  return (state, fakes);
}

/// Sends each request to the fake of the server whose host it is for.
class _Routed extends http.BaseClient {
  _Routed(this.fakes, this.next);
  final List<FakeServer> fakes;
  final int Function() next;

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    final i =
        int.tryParse(
          RegExp(r'^s(\d+)\.').firstMatch(request.url.host)?.group(1) ?? '',
        ) ??
        0;
    final body = request is http.Request ? request.body : '';
    final copy = http.Request(request.method, request.url)
      ..headers.addAll(request.headers)
      ..body = body;
    final res = await fakes[i].client().send(copy);
    return res;
  }
}

/// Puts [child] in the app's theme, localisation and scopes.
Widget harness(AppState app, Widget child) => AppScope(
  state: app,
  child: MaterialApp(
    theme: buildTheme(dark: true),
    localizationsDelegates: L10n.localizationsDelegates,
    supportedLocales: L10n.supportedLocales,
    home: Scaffold(body: child),
  ),
);

Future<void> settleAll(WidgetTester tester) async {
  for (var i = 0; i < 6; i++) {
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 20)),
    );
    await tester.pump(const Duration(milliseconds: 50));
  }
}

Future<void> sized(WidgetTester tester, Size s) async {
  tester.view.devicePixelRatio = 1;
  tester.view.physicalSize = s;
  addTearDown(tester.view.reset);
}

const phone = Size(390, 800);
const desktop = Size(1280, 860);

CacheDb cacheOf(AppState app, int i) => app.servers[i].db;
