// The whole app against a real server.
//
// Run it with `tool/integration.sh`, which builds the Go server, fills a
// fresh data directory with `tools/devseed`, starts the server, and
// passes the seed (addresses, ids and app tokens) in the CHURR_SEED
// define as base64 JSON.
import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:churr/core/api/client.dart';
import 'package:churr/core/api/messages.dart';
import 'package:churr/core/ids.dart';
import 'package:churr/core/session_store.dart';
import 'package:churr/platform/keychain_store.dart';
import 'package:churr/platform/paths.dart';
import 'package:churr/state/app_prefs.dart';
import 'package:churr/state/app_state.dart';
import 'package:churr/ui/app.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
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

Finder rich(String text) => find.textContaining(text, findRichText: true);

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('add a server with a dev token, open a channel, send, and see '
      'another account\'s message arrive live', (tester) async {
    expect(_seed, isNotEmpty, reason: 'run this through tool/integration.sh');
    final seed = jsonDecode(utf8.decode(base64.decode(_seed))) as Map<String, dynamic>;
    final accounts = (seed['accounts'] as List).cast<Map<String, dynamic>>();
    final kai = accounts.firstWhere((a) => a['username'] == 'kai');
    final sam = accounts.firstWhere((a) => a['username'] == 'sam');
    final general = (seed['conversations'] as Map)['general'] as String;
    final origin = seed['origin'] as String;

    // The app as it ships, on real storage, but in a folder of its own
    // so a test run never touches (or is confused by) a real profile.
    final root = await Directory.systemTemp.createTemp('churr-it-');
    final storage = DiskStorage(root);
    final state = AppState(appDb: storage.openAppDb(), storage: storage, secrets: KeychainStore());
    await state.load();

    await tester.binding.setSurfaceSize(const Size(1280, 800));
    final shot = GlobalKey();
    await tester.pumpWidget(RepaintBoundary(key: shot, child: ChurrApp(state: state)));
    await tester.pump();

    // No server yet: "Add a server", with the development form.
    expect(find.byKey(const Key('dev-add')), findsOneWidget);
    await tester.enterText(find.byKey(const Key('dev-address')), origin);
    await tester.enterText(find.byKey(const Key('dev-access')), kai['access_token'] as String);
    await tester.enterText(find.byKey(const Key('dev-refresh')), kai['refresh_token'] as String);
    await tester.ensureVisible(find.byKey(const Key('dev-add')));
    await tester.tap(find.byKey(const Key('dev-add')));

    // The first sync fills the sidebar.
    final row = find.byKey(Key('row-$general'));
    await pumpUntil(tester, row, what: 'the General row');
    expect(state.servers, hasLength(1));

    // Open the channel: its history is fetched and drawn.
    await tester.tap(row);
    // The list opens at its newest end; the seeded history ends with
    // the retro notes and the poll just above them.
    await pumpUntil(tester, rich('Notes from the retro.'), what: 'the seeded history');
    expect(find.text('retro-notes.txt'), findsOneWidget);

    // Send a message: it is on screen at once, and confirmed.
    final mine = 'hello from the integration test ${newUuid().substring(24)}';
    await tester.enterText(find.byKey(const Key('composer')), mine);
    await tester.tap(find.byKey(const Key('send')));
    await pumpUntil(tester, rich(mine), what: 'my own message');
    final server = state.servers.single;
    final end = DateTime.now().add(const Duration(seconds: 20));
    while ((await server.db.outbox()).isNotEmpty && DateTime.now().isBefore(end)) {
      await tester.pump(const Duration(milliseconds: 100));
    }
    expect(await server.db.outbox(), isEmpty, reason: 'the server confirmed the message');
    final stored = await server.db.messages(general);
    expect(stored.where((m) => m.text == mine), hasLength(1), reason: 'confirmed exactly once');

    // Another account posts; the message arrives over the WebSocket.
    final other = ApiClient(
      base: Uri.parse(origin),
      tokens: Tokens(
        access: sam['access_token'] as String,
        refresh: sam['refresh_token'] as String,
        expiresAt: DateTime.now().add(const Duration(minutes: 30)),
      ),
    );
    final theirs = 'live from sam ${newUuid().substring(24)}';
    await other.postMessage(general, clientId: newUuid(), text: theirs);
    await pumpUntil(tester, rich(theirs), what: "sam's message, live");
    other.close();

    // Pictures of the real shell, for a person to look at.
    Future<void> capture(String name) async {
      // Several frames, so a theme change has finished animating.
      for (var i = 0; i < 12; i++) {
        await tester.pump(const Duration(milliseconds: 100));
      }
      final boundary = shot.currentContext!.findRenderObject()! as RenderRepaintBoundary;
      final image = await boundary.toImage(pixelRatio: 2);
      final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
      final file = File('${root.path}/$name.png');
      await file.writeAsBytes(bytes!.buffer.asUint8List());
      // ignore: avoid_print
      print('SCREENSHOT ${file.path}');
    }

    await tester.runAsync(() => Future<void>.delayed(const Duration(seconds: 2)));
    await capture('desktop-dark');
    state.prefs.theme = ThemeChoice.light;
    await capture('desktop-light');
    state.prefs.layout = SidebarLayout.merged;
    await capture('desktop-light-merged');
    state.prefs.theme = ThemeChoice.dark;
    state.prefs.layout = SidebarLayout.hybrid;
    await capture('desktop-dark-hybrid');
    state.prefs.layout = SidebarLayout.rail;
    await tester.binding.setSurfaceSize(const Size(390, 800));
    await capture('phone-dark-conversation');
    state.openConversation(null);
    await capture('phone-dark-sidebar');
    await tester.binding.setSurfaceSize(const Size(1280, 800));

    // Sign out: the server's folder and tokens are gone.
    final id = server.localId;
    await tester.runAsync(() => state.removeServer(id));
    await tester.pump(const Duration(milliseconds: 300));
    expect(state.servers, isEmpty);
    expect(Directory('${root.path}/servers/$id').existsSync(), isFalse);
    expect(find.byKey(const Key('server-address')), findsOneWidget);
  });
}
