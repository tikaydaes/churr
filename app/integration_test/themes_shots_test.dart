// Themes and the hub directory in the real app against a seeded server,
// in dark and light, for a person to look at.
//
// Run it as tool/integration.sh runs app_test.dart, with this file in its
// place: a seeded `churr serve -dev`, and the seed in CHURR_SEED. It
// saves a theme on the server, publishes it, publishes another as a
// second account, posts a theme link (which the server turns into a theme
// card), and pictures the editor, the library, the card and the hub
// directory. The seeded server is on no hub, so the hub directory is
// drawn from fake answers (a stand-in HTTP client), as the widget tests
// do; everything else talks to the real server.
import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:churr/core/api/client.dart';
import 'package:churr/core/api/hubs.dart';
import 'package:churr/core/api/messages.dart';
import 'package:churr/core/api/themes.dart';
import 'package:churr/core/db/app_db.dart';
import 'package:churr/core/db/cache_db.dart';
import 'package:churr/core/ids.dart';
import 'package:churr/core/session_store.dart';
import 'package:churr/core/themes/theme.dart';
import 'package:churr/platform/keychain_store.dart';
import 'package:churr/platform/paths.dart';
import 'package:churr/state/app_prefs.dart';
import 'package:churr/state/app_state.dart';
import 'package:churr/state/server_state.dart';
import 'package:churr/ui/app.dart';
import 'package:churr/ui/screens/account/common.dart';
import 'package:churr/ui/screens/hubs/hub_screens.dart';
import 'package:churr/ui/screens/themes/theme_editor.dart';
import 'package:churr/ui/screens/themes/theme_library.dart';
import 'package:churr/ui/screens/themes/theme_widgets.dart';
import 'package:drift/native.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:integration_test/integration_test.dart';

const _seed = String.fromEnvironment('CHURR_SEED');

Future<void> pumpUntil(
  WidgetTester tester,
  Finder finder, {
  Duration timeout = const Duration(seconds: 30),
  String? what,
}) async {
  final end = DateTime.now().add(timeout);
  while (DateTime.now().isBefore(end)) {
    await tester.pump(const Duration(milliseconds: 100));
    if (finder.evaluate().isNotEmpty) return;
  }
  fail('Timed out waiting for ${what ?? finder}');
}

/// A server that answers the hub directory's requests with fake data.
ServerState fakeHubServer() {
  const hub = '0193c000-0000-7000-8000-0000000000a1';
  Map<String, dynamic> mod(String id, String server, String name) => {
    'account_id': id,
    'server': server,
    'username': name,
  };
  final routes = <String, Object>{
    '/api/v1/admin/peers/$hub/directory': {
      'channels': [
        {
          'stream_id': '0193d100-0000-7000-8000-000000000001',
          'name': 'Design guild',
          'description': 'Design reviews shared between the studios.',
          'listing': 'request',
          'moderators': [
            mod('a1', 'b.example.com', 'robin'),
            mod('a2', 'c.example.com', 'lee'),
          ],
          'servers': 4,
          'state': 'none',
        },
        {
          'stream_id': '0193d100-0000-7000-8000-000000000002',
          'name': 'Open source',
          'description': 'Anyone on the hub may join.',
          'listing': 'open',
          'moderators': [mod('a3', 'c.example.com', 'lee')],
          'servers': 9,
          'state': 'member',
        },
        {
          'stream_id': '0193d100-0000-7000-8000-000000000003',
          'name': 'Security notices',
          'description': '',
          'listing': 'request',
          'moderators': [mod('a4', 'd.example.org', 'ravi')],
          'servers': 2,
          'state': 'requested',
        },
      ],
    },
    '/api/v1/admin/streams': {
      'streams': [
        {
          'id': '0193d100-0000-7000-8000-000000000004',
          'peer_id': hub,
          'state': 'removed',
          'offered_name': 'Finance',
        },
      ],
    },
  };
  final api = ApiClient(
    base: Uri.parse('https://fake.example.org'),
    tokens: Tokens(
      access: 'a',
      refresh: 'r',
      expiresAt: DateTime.now().add(const Duration(hours: 1)),
    ),
    httpClient: MockClient(
      (r) async => http.Response(jsonEncode(routes[r.url.path] ?? {}), 200),
    ),
  );
  return ServerState(
    entry: ServerEntry(
      localId: 'fake',
      address: 'https://fake.example.org',
      name: 'Fake',
      position: 0,
    ),
    db: CacheDb(NativeDatabase.memory()),
    api: api,
  );
}

Peer fakeHub() => Peer({
  'id': '0193c000-0000-7000-8000-0000000000a1',
  'kind': 'hub',
  'name': 'hub.example.net',
  'state': 'active',
});

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('themes and the hub directory, pictured in dark and light', (
    tester,
  ) async {
    expect(_seed, isNotEmpty, reason: 'run this through tool/integration.sh');
    final seed =
        jsonDecode(utf8.decode(base64.decode(_seed))) as Map<String, dynamic>;
    final accounts = (seed['accounts'] as List).cast<Map<String, dynamic>>();
    final kai = accounts.firstWhere((a) => a['username'] == 'kai');
    final sam = accounts.firstWhere((a) => a['username'] == 'sam');
    final general = (seed['conversations'] as Map)['general'] as String;
    final origin = seed['origin'] as String;

    final root = await Directory.systemTemp.createTemp('churr-shots-');
    final storage = DiskStorage(root);
    final state = AppState(
      appDb: storage.openAppDb(),
      storage: storage,
      secrets: KeychainStore(),
    );
    await state.load();
    await tester.binding.setSurfaceSize(const Size(1280, 820));
    final shot = GlobalKey();
    await tester.pumpWidget(
      RepaintBoundary(
        key: shot,
        child: ChurrApp(state: state),
      ),
    );
    await tester.pump();

    await tester.enterText(find.byKey(const Key('dev-address')), origin);
    await tester.enterText(
      find.byKey(const Key('dev-access')),
      kai['access_token'] as String,
    );
    await tester.enterText(
      find.byKey(const Key('dev-refresh')),
      kai['refresh_token'] as String,
    );
    await tester.ensureVisible(find.byKey(const Key('dev-add')));
    await tester.tap(find.byKey(const Key('dev-add')));
    final row = find.byKey(Key('row-$general'));
    await pumpUntil(tester, row, what: 'the General row');
    final server = state.servers.single;

    // A theme saved and published on the real server, and one published
    // by another account, for the library.
    final harbour = await tester.runAsync(() async {
      final t = await state.themes.save(
        server,
        ChurrTheme(
          id: '',
          name: 'Harbour',
          kind: 'dark',
          colours: {
            ...BuiltInThemes.dark.colours,
            'bg': '#071a24',
            'panel': '#0d2633',
            'raise': '#143444',
            'lav': '#9fc3ff',
            'cyan': '#5ff0d0',
          },
        ),
      );
      return state.themes.setPublished(t, true);
    });
    final other = ApiClient(
      base: Uri.parse(origin),
      tokens: Tokens(
        access: sam['access_token'] as String,
        refresh: sam['refresh_token'] as String,
        expiresAt: DateTime.now().add(const Duration(minutes: 30)),
      ),
    );
    await tester.runAsync(() async {
      final paper = await other.createTheme(
        ChurrTheme(
          id: '',
          name: 'Morning paper',
          kind: 'light',
          colours: {
            ...BuiltInThemes.light.colours,
            'bg': '#fbf7ee',
            'raise': '#efe8d8',
            'lav': '#5a3cc8',
          },
        ),
      );
      await other.publishTheme(paper.id);
      // A theme link in a message: the server makes it a theme card.
      await other.postMessage(
        general,
        clientId: newUuid(),
        text: 'My new theme: ${themeLink(origin, harbour!.id)}',
      );
    });
    other.close();

    Future<void> capture(String name) async {
      for (var i = 0; i < 12; i++) {
        await tester.pump(const Duration(milliseconds: 100));
      }
      final boundary =
          shot.currentContext!.findRenderObject()! as RenderRepaintBoundary;
      final image = await boundary.toImage(pixelRatio: 2);
      final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
      final file = File('${root.path}/$name.png');
      await file.writeAsBytes(bytes!.buffer.asUint8List());
      // ignore: avoid_print
      print('SCREENSHOT ${file.path}');
    }

    BuildContext ctx() => tester.element(find.byType(Scaffold).first);
    Future<void> close() async {
      Navigator.of(ctx()).pop();
      await tester.pump(const Duration(milliseconds: 200));
    }

    // The theme card in the conversation.
    await tester.tap(row);
    await pumpUntil(
      tester,
      find.byType(ThemeCardView),
      what: 'the theme card',
      timeout: const Duration(seconds: 40),
    );
    for (final theme in [ThemeChoice.dark, ThemeChoice.light]) {
      state.prefs.theme = theme;
      await capture('theme-card-${theme.name}');
    }

    // The editor: the saved theme with a secondary text too dark, so a
    // failing pairing and its "Fix" show; then the light built-in.
    state.prefs.theme = ThemeChoice.dark;
    await tester.pump();
    final saved = state.themes.mine[server.localId]!.firstWhere(
      (t) => t.name == 'Harbour',
    );
    openThemeEditor(ctx(), start: saved);
    await pumpUntil(
      tester,
      find.byKey(const Key('hex-dim')),
      what: 'the editor',
    );
    await tester.enterText(find.byKey(const Key('hex-dim')), '#6a7c88');
    await capture('editor-dark');
    await close();
    state.prefs.theme = ThemeChoice.light;
    await tester.pump();
    openThemeEditor(ctx(), start: BuiltInThemes.light, server: server);
    await pumpUntil(
      tester,
      find.byKey(const Key('hex-dim')),
      what: 'the editor',
    );
    await capture('editor-light');
    await close();

    // The library.
    for (final theme in [ThemeChoice.dark, ThemeChoice.light]) {
      state.prefs.theme = theme;
      await tester.pump();
      openThemeLibrary(ctx(), server);
      await pumpUntil(tester, find.text('Morning paper'), what: 'the library');
      await capture('library-${theme.name}');
      await close();
    }

    // The hub directory, on fake answers: the seeded server has no hub.
    final fake = fakeHubServer();
    for (final theme in [ThemeChoice.dark, ThemeChoice.light]) {
      state.prefs.theme = theme;
      await tester.pump();
      showAccountScreen<void>(
        ctx(),
        title: 'Directory of hub.example.net',
        width: 680,
        child: HubDirectoryScreen(server: fake, hub: fakeHub()),
      );
      await pumpUntil(tester, find.text('Design guild'), what: 'the directory');
      await capture('hub-directory-${theme.name}');
      await close();
    }
    // What the real server says of hubs: none.
    final peers = await tester.runAsync(() => server.api.listPeers());
    expect(peers!.where((p) => p.isHub), isEmpty);
    // Leave the server as it was found.
    await tester.runAsync(() => server.api.deleteTheme(harbour!.id));
    state.prefs.theme = ThemeChoice.dark;
  });
}
