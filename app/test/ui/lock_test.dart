import 'dart:convert';

import 'package:churr/core/db/app_db.dart';
import 'package:churr/core/session_store.dart';
import 'package:churr/state/app_lock.dart';
import 'package:churr/state/app_state.dart';
import 'package:churr/ui/app.dart';
import 'package:churr/ui/layout/shell.dart';
import 'package:churr/ui/screens/lock/lock_screen.dart';
import 'package:drift/native.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'shell_test.dart' show MemoryStorage, seededCache, settle, size, rich;

/// The lock (ruling R75, docs/CLIENTS.md section 9.27): while locked the
/// whole widget tree is the lock screen, the servers hold nothing of the
/// account in memory, and unlocking goes through the server's page.

/// A server that locks and unlocks: the session is locked once
/// `lockSession` is called, and unlocked once the test says the browser
/// did its part.
class LockServer {
  bool locked = false;
  bool browserDone = false;
  final seen = <String>[];

  http.Response _json(int status, Object body) =>
      http.Response(jsonEncode(body), status);

  MockClient client() => MockClient((r) async {
    seen.add('${r.method} ${r.url.path}');
    switch (r.url.path) {
      case '/api/v1/session/lock':
        locked = true;
        return http.Response('', 204);
      case '/api/v1/session/unlock/begin':
        return _json(200, {
          'ceremony_id': 'c1',
          'public_key': <String, Object?>{},
          'unlock_url': 'https://s0.example.org/locked/#ceremony=c1',
        });
      case '/api/v1/me':
        if (locked && browserDone) locked = false;
        if (locked) {
          return _json(401, {
            'error': {'code': 'session_locked', 'message': 'Locked.'},
          });
        }
        return _json(200, {
          'account': {
            'id': 'me',
            'username': 'kai',
            'display_name': 'Kai Tanaka',
            'role': 'owner',
          },
        });
    }
    return _json(200, {'messages': [], 'thread_states': [], 'has_more': false});
  });
}

Future<AppState> lockApp(
  WidgetTester tester,
  LockServer server, {
  bool startLocked = false,
}) async {
  late AppState state;
  await tester.runAsync(() async {
    final appDb = AppDb(NativeDatabase.memory());
    final storage = MemoryStorage();
    final secrets = MemorySecretStore();
    await appDb.addServer(
      ServerEntry(
        localId: 'server-0',
        address: 'https://s0.example.org',
        name: 'Work',
        position: 0,
      ),
    );
    storage.caches['server-0'] = await seededCache();
    await SessionStore(secrets).save(
      'server-0',
      Tokens(
        access: 'a',
        refresh: 'r',
        expiresAt: DateTime.now().add(const Duration(hours: 1)),
      ),
    );
    if (startLocked) await appDb.putPref('lock.locked', '1');
    state = AppState(
      appDb: appDb,
      storage: storage,
      secrets: secrets,
      connectOnStart: false,
      httpClientFactory: server.client,
    );
    await state.load();
  });
  return state;
}

void expectNothingOfTheAccount(WidgetTester tester, AppState state) {
  expect(find.byType(LockScreen), findsOneWidget);
  expect(find.byType(Shell), findsNothing);
  expect(find.byType(Navigator), findsNothing);
  for (final words in [
    'Kai Tanaka',
    'Sam Okafor',
    'General',
    'Design',
    'Work',
    'Agenda',
  ]) {
    expect(rich(words), findsNothing, reason: '"$words" is on the lock screen');
  }
  expect(find.text('LOCKED'), findsOneWidget);
  expect(find.byKey(const Key('lock-unlock')), findsOneWidget);
  expect(find.byType(FilledButton), findsOneWidget);
  for (final s in state.servers) {
    expect(s.me, isNull);
    expect(s.conversations, isEmpty);
    expect(s.accounts, isEmpty);
    expect(s.summary.badge, 0);
  }
  expect(state.open, isNull);
}

void main() {
  for (final (label, screen) in [
    ('phone', const Size(390, 844)),
    ('desktop', const Size(1280, 800)),
  ]) {
    testWidgets(
      '$label: locking replaces the whole tree, and nothing of the account is left',
      (tester) async {
        await size(tester, screen);
        final server = LockServer();
        final state = await lockApp(tester, server);
        await tester.pumpWidget(ChurrApp(state: state));
        await settle(tester);
        expect(rich('Sam Okafor'), findsWidgets);

        await tester.runAsync(state.lock.lock);
        await settle(tester);
        expectNothingOfTheAccount(tester, state);
        expect(server.seen, contains('POST /api/v1/session/lock'));
        expect(state.prefs.raw('lock.locked'), '1');
      },
    );
  }

  testWidgets('a restart while locked stays locked and reads nothing', (
    tester,
  ) async {
    await size(tester, const Size(1280, 800));
    final server = LockServer()..locked = true;
    final state = await lockApp(tester, server, startLocked: true);
    await tester.pumpWidget(ChurrApp(state: state));
    await settle(tester);
    expectNothingOfTheAccount(tester, state);
  });

  testWidgets(
    'Unlock opens the server\'s page, waits for it, then everything comes back',
    (tester) async {
      await size(tester, const Size(1280, 800));
      final server = LockServer();
      final opened = <Uri>[];
      AppLock.openUnlockPage = (u) async => opened.add(u);
      AppLock.pollEvery = const Duration(milliseconds: 10);
      addTearDown(() {
        AppLock.openUnlockPage = (_) async {};
        AppLock.pollEvery = const Duration(seconds: 2);
      });
      final state = await lockApp(tester, server);
      await tester.pumpWidget(ChurrApp(state: state));
      await settle(tester);
      await tester.runAsync(state.lock.lock);
      await settle(tester);

      // The page is open in the browser; the session is still locked.
      final waiting = tester.runAsync(() async {
        final f = state.lock.unlock();
        await Future<void>.delayed(const Duration(milliseconds: 60));
        expect(state.lock.unlocking, isTrue);
        expect(state.lock.locked, isTrue);
        server.browserDone = true;
        await f;
      });
      await waiting;
      await settle(tester);
      expect(opened, [Uri.parse('https://s0.example.org/locked/#ceremony=c1')]);
      expect(state.lock.locked, isFalse);
      expect(find.byType(LockScreen), findsNothing);
      expect(rich('Sam Okafor'), findsWidgets);
      expect(state.servers.single.conversations, isNotEmpty);
    },
  );

  testWidgets(
    'the automatic lock: off by default, then after the chosen time without use',
    (tester) async {
      await size(tester, const Size(1280, 800));
      final state = await lockApp(tester, LockServer());
      await tester.pumpWidget(ChurrApp(state: state));
      await settle(tester);
      final lock = state.lock;
      expect(lock.autoMinutes, 0);
      lock.checkIdle(DateTime.now().add(const Duration(days: 1)));
      expect(lock.locked, isFalse);
      lock.autoMinutes = 5;
      lock.noteUse();
      lock.checkIdle(DateTime.now().add(const Duration(minutes: 4)));
      expect(lock.locked, isFalse);
      await tester.runAsync(() async {
        lock.checkIdle(DateTime.now().add(const Duration(minutes: 6)));
        await Future<void>.delayed(const Duration(milliseconds: 50));
      });
      await settle(tester);
      expect(lock.locked, isTrue);
      expect(find.byType(LockScreen), findsOneWidget);
    },
  );
}
