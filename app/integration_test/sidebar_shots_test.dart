// A person's own sidebar (ruling R83) in the real app against a seeded
// server, for a person to look at: own sections, a heading's menu, and
// reordering mode at the width of a phone.
//
// Run it as tool/integration.sh runs app_test.dart, with this file in its
// place: a seeded `churr serve -dev`, and the seed in CHURR_SEED.
import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:churr/platform/keychain_store.dart';
import 'package:churr/platform/paths.dart';
import 'package:churr/state/app_state.dart';
import 'package:churr/state/personal_sidebar.dart';
import 'package:churr/ui/app.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';

const _seed = String.fromEnvironment('CHURR_SEED');

Future<void> pumpUntil(WidgetTester tester, Finder finder, {String? what}) async {
  final end = DateTime.now().add(const Duration(seconds: 30));
  while (DateTime.now().isBefore(end)) {
    await tester.pump(const Duration(milliseconds: 100));
    if (finder.evaluate().isNotEmpty) return;
  }
  fail('Timed out waiting for ${what ?? finder}');
}

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('own sections, a heading menu and reordering mode', (tester) async {
    expect(_seed, isNotEmpty, reason: 'run this through tool/integration.sh');
    final seed = jsonDecode(utf8.decode(base64.decode(_seed))) as Map<String, dynamic>;
    final accounts = (seed['accounts'] as List).cast<Map<String, dynamic>>();
    final kai = accounts.firstWhere((a) => a['username'] == 'kai');
    final cv = (seed['conversations'] as Map).cast<String, String>();
    final origin = seed['origin'] as String;

    final root = await Directory.systemTemp.createTemp('churr-it-');
    final storage = DiskStorage(root);
    final state = AppState(appDb: storage.openAppDb(), storage: storage, secrets: KeychainStore());
    await state.load();

    await tester.binding.setSurfaceSize(const Size(1280, 860));
    final shot = GlobalKey();
    await tester.pumpWidget(RepaintBoundary(key: shot, child: ChurrApp(state: state)));
    await tester.pump();
    await tester.enterText(find.byKey(const Key('dev-address')), origin);
    await tester.enterText(find.byKey(const Key('dev-access')), kai['access_token'] as String);
    await tester.enterText(find.byKey(const Key('dev-refresh')), kai['refresh_token'] as String);
    await tester.ensureVisible(find.byKey(const Key('dev-add')));
    await tester.tap(find.byKey(const Key('dev-add')));
    await pumpUntil(tester, find.byKey(Key('row-${cv['general']}')), what: 'the General row');
    final server = state.servers.single;

    await tester.runAsync(() async {
      await changeLayout(server, [
        {'op': 'create_section', 'name': 'Focus'},
        {'op': 'create_section', 'name': 'Later'},
        {'op': 'move', 'conversation_id': cv['ops'], 'key': 'p:1'},
        {'op': 'move', 'conversation_id': cv['general'], 'key': 'p:1'},
        {'op': 'move', 'conversation_id': cv['random'], 'key': 'p:1'},
        {'op': 'reorder', 'key': 'p:1', 'ids': [cv['random'], cv['ops'], cv['general']]},
        {'op': 'move', 'conversation_id': cv['history'], 'key': 'p:2'},
      ]);
    });
    await pumpUntil(tester, find.text('FOCUS'), what: 'the own section');

    Future<void> capture(String name) async {
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

    await capture('sidebar-app-personal');
    await tester.tap(find.byKey(const Key('heading-menu-Focus')));
    await capture('sidebar-app-section-menu');
    await tester.tapAt(const Offset(1200, 40));
    await tester.pump(const Duration(milliseconds: 300));

    await tester.binding.setSurfaceSize(const Size(390, 844));
    state.openConversation(null);
    reorderingServer.value = server.localId;
    await capture('sidebar-app-phone-reorder');
    reorderingServer.value = null;

    await tester.runAsync(() => changeLayout(server, [{'op': 'reset_all'}]));
    await tester.binding.setSurfaceSize(const Size(1280, 800));
    await tester.runAsync(() => state.removeServer(server.localId));
    await tester.pump(const Duration(milliseconds: 300));
  });
}
