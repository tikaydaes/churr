import 'dart:convert';

import 'package:churr/core/db/app_db.dart';
import 'package:churr/core/db/cache_db.dart';
import 'package:churr/core/models/account.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/core/models/personal_layout.dart';
import 'package:churr/core/models/server.dart';
import 'package:churr/core/session_store.dart';
import 'package:churr/state/app_prefs.dart';
import 'package:churr/state/app_state.dart';
import 'package:churr/ui/app.dart';
import 'package:churr/ui/layout/server_rail.dart';
import 'package:drift/native.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// Server storage in memory: each server's cache is filled before the
/// app opens it, as if an earlier run had synced.
class MemoryStorage implements ServerStorage {
  final caches = <String, CacheDb>{};
  final deleted = <String>[];

  @override
  Future<CacheDb> openCache(String localId) async =>
      caches[localId] ??= CacheDb(NativeDatabase.memory());
  @override
  Future<String?> filesDirectory(String localId) async => null;
  @override
  Future<void> delete(String localId) async => deleted.add(localId);
}

Conversation ch(
  String id,
  String name, {
  String? section,
  String? parent,
  String mode = 'mentions',
  String? show,
  int unread = 0,
  int badge = 0,
  bool starred = false,
}) => Conversation.fromJson({
  'id': id,
  'kind': 'channel',
  'private': false,
  'name': name,
  'topic': id == 'general' ? 'Everything that fits nowhere else' : '',
  'section_id': ?section,
  'parent_id': ?parent,
  'created_at': '2026-03-01T00:00:00.000Z',
  'last_seq': 20,
  'member_count': 5,
  'membership': {
    'level': 'member',
    'read_seq': 20,
    'mode': mode,
    'show': ?show,
    'starred': starred,
    'unread_count': unread,
    'mention_count': 0,
    'badge_count': badge,
  },
});

Future<CacheDb> seededCache({String prefix = ''}) async {
  final db = CacheDb(NativeDatabase.memory());
  await db.putMe(
    Me.fromJson({
      'id': 'me',
      'username': 'kai',
      'display_name': 'Kai Tanaka',
      'role': 'owner',
    }),
  );
  await db.upsertAccounts([
    Account.fromJson({
      'id': 'me',
      'username': 'kai',
      'display_name': 'Kai Tanaka',
      'role': 'owner',
    }),
    Account.fromJson({
      'id': 'sam',
      'username': 'sam',
      'display_name': 'Sam Okafor',
      'role': 'member',
      'presence': 'online',
    }),
  ]);
  await db.replaceSections([Section(id: 's1', name: 'Projects', position: 1)]);
  for (final c in [
    ch('general', '${prefix}General', unread: 2),
    ch(
      'design',
      '${prefix}Design reviews',
      section: 's1',
      badge: 3,
      unread: 3,
      mode: 'any',
    ),
    ch('icons', 'Icons', parent: 'design'),
    ch('random', 'Random', mode: 'nothing', unread: 1),
    ch('secret', 'Old plans', show: 'hidden'),
    Conversation.fromJson({
      'id': 'chat',
      'kind': 'channel',
      'private': true,
      'member_ids': ['me', 'sam'],
      'created_at': '2026-03-01T00:00:00.000Z',
      'last_seq': 2,
      'member_count': 2,
      'membership': {
        'level': 'member',
        'read_seq': 2,
        'mode': 'any',
        'starred': false,
        'unread_count': 0,
        'mention_count': 0,
        'badge_count': 0,
      },
    }),
  ]) {
    await db.upsertConversation(c, syncedSeq: c.lastSeq);
  }
  await db.setHistoryComplete('general', true);
  await db.upsertMessages([
    Message.fromJson({
      'id': 'm1',
      'conversation_id': 'general',
      'seq': 19,
      'author_id': 'sam',
      'text': 'Agenda: the **new sidebar**',
      'created_at': '2026-11-13T09:30:00.000Z',
    }),
    Message.fromJson({
      'id': 'm2',
      'conversation_id': 'general',
      'seq': 20,
      'author_id': 'sam',
      'text': 'and then icons',
      'created_at': '2026-11-13T09:31:00.000Z',
      'reactions': [
        {
          'emoji': '👍',
          'count': 2,
          'me': true,
          'account_ids': ['me', 'sam'],
        },
      ],
    }),
  ]);
  await db.putStatusSummary(const StatusSummary(badge: 3));
  return db;
}

Future<AppState> makeApp(WidgetTester tester, {int servers = 1}) async {
  late AppState state;
  await tester.runAsync(() async {
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
      storage.caches[id] = await seededCache(prefix: i == 0 ? '' : 'Friends ');
      await SessionStore(secrets).save(
        id,
        Tokens(
          access: 'a',
          refresh: 'r',
          expiresAt: DateTime.now().add(const Duration(hours: 1)),
        ),
      );
    }
    state = AppState(
      appDb: appDb,
      storage: storage,
      secrets: secrets,
      // Tests never touch the network or open a socket.
      connectOnStart: false,
      httpClientFactory: () => MockClient(
        (r) async => http.Response(
          jsonEncode({
            'messages': [],
            'thread_states': [],
            'has_more': false,
            'membership': {
              'level': 'member',
              'read_seq': 20,
              'mode': 'mentions',
            },
          }),
          200,
        ),
      ),
    );
    await state.load();
  });
  return state;
}

/// Lets database streams and the frames they cause settle.
Future<void> settle(WidgetTester tester) async {
  for (var i = 0; i < 5; i++) {
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 20)),
    );
    await tester.pump(const Duration(milliseconds: 50));
  }
}

Future<void> size(WidgetTester tester, Size s) async {
  tester.view.devicePixelRatio = 1;
  tester.view.physicalSize = s;
  addTearDown(tester.view.reset);
}

Finder rich(String s) => find.textContaining(s, findRichText: true);

void main() {
  testWidgets('desktop width: rail, list and conversation side by side', (
    tester,
  ) async {
    await size(tester, const Size(1280, 800));
    final state = await makeApp(tester);
    await tester.pumpWidget(ChurrApp(state: state));
    await settle(tester);

    // The rail is the layout out of the box: a 64-wide column of tiles.
    final rail = tester.getSize(find.byType(ServerRail));
    expect(rail.width, 64);
    expect(rail.height, 800);
    expect(tester.getSize(find.byKey(const Key('sidebar-list'))).width, 280);
    expect(find.text('Pick a conversation.'), findsOneWidget);

    // Groups in order, sub-channel under its parent, hidden row absent.
    expect(find.text('PROJECTS'), findsOneWidget);
    expect(find.text('CHANNELS'), findsOneWidget);
    expect(find.text('CHATS'), findsOneWidget);
    expect(find.byKey(const Key('row-icons')), findsOneWidget);
    expect(
      tester.getTopLeft(find.byKey(const Key('row-icons'))).dy,
      greaterThan(tester.getTopLeft(find.byKey(const Key('row-design'))).dy),
    );
    expect(find.byKey(const Key('row-secret')), findsNothing);
    // An ad-hoc chat is named by the other person.
    expect(rich('Sam Okafor'), findsOneWidget);
    // Badges: the row's own, and the server's total on Notifications.
    expect(find.text('3'), findsNWidgets(3));

    // Opening a conversation keeps the sidebar and fills the right.
    await tester.tap(find.byKey(const Key('row-general')));
    await settle(tester);
    expect(find.byKey(const Key('sidebar-list')), findsOneWidget);
    expect(find.byKey(const Key('back')), findsNothing);
    expect(rich('Agenda: the new sidebar'), findsOneWidget);
    expect(rich('and then icons'), findsOneWidget);
    expect(find.text('Everything that fits nowhere else'), findsOneWidget);
    expect(find.text('2'), findsWidgets, reason: 'the reaction count');
    expect(find.byKey(const Key('composer')), findsOneWidget);
  });

  testWidgets(
    'phone width: one pane at a time, the rail a strip across the top',
    (tester) async {
      await size(tester, const Size(390, 800));
      final state = await makeApp(tester);
      await tester.pumpWidget(ChurrApp(state: state));
      await settle(tester);

      final rail = tester.getSize(find.byType(ServerRail));
      expect(rail.width, 390);
      expect(rail.height, 60);
      expect(tester.getSize(find.byKey(const Key('sidebar-list'))).width, 390);
      expect(find.byKey(const Key('composer')), findsNothing);

      // The conversation is pushed over the sidebar, with a way back.
      await tester.tap(find.byKey(const Key('row-general')));
      await settle(tester);
      expect(find.byKey(const Key('sidebar-list')), findsNothing);
      expect(rich('and then icons'), findsOneWidget);
      expect(
        tester.takeException(),
        isNull,
        reason: 'nothing overflows at phone width',
      );
      await tester.tap(find.byKey(const Key('back')));
      await settle(tester);
      expect(find.byKey(const Key('sidebar-list')), findsOneWidget);
      expect(state.open, isNull);
    },
  );

  testWidgets(
    'the show rules: hidden rows appear with "Show hidden", and an open hidden one keeps its row',
    (tester) async {
      await size(tester, const Size(1280, 800));
      final state = await makeApp(tester);
      await tester.pumpWidget(ChurrApp(state: state));
      await settle(tester);

      expect(find.byKey(const Key('row-secret')), findsNothing);
      await tester.tap(find.byKey(const Key('show-hidden')));
      await settle(tester);
      expect(find.byKey(const Key('row-secret')), findsOneWidget);
      expect(find.text('HIDDEN'), findsWidgets);
      await tester.tap(find.byKey(const Key('show-hidden')));
      await settle(tester);
      expect(find.byKey(const Key('row-secret')), findsNothing);

      // Opened some other way (a link, search), it has its row while open.
      state.openConversation(const OpenTarget('server-0', 'secret'));
      await settle(tester);
      expect(find.byKey(const Key('row-secret')), findsOneWidget);
      state.openConversation(const OpenTarget('server-0', 'general'));
      await settle(tester);
      expect(find.byKey(const Key('row-secret')), findsNothing);
    },
  );

  testWidgets(
    'a row whose mode is nothing is dimmed and never bold; an unread one is bold',
    (tester) async {
      await size(tester, const Size(1280, 800));
      final state = await makeApp(tester);
      await tester.pumpWidget(ChurrApp(state: state));
      await settle(tester);
      // The name's own text, not the icon beside it (icons are text too).
      TextStyle style(String key, String name) => tester
          .widget<RichText>(
            find.descendant(
              of: find.byKey(Key(key)),
              matching: find.byWidgetPredicate(
                (w) => w is RichText && w.text.toPlainText().contains(name),
              ),
            ),
          )
          .text
          .style!;
      expect(style('row-general', 'General').fontWeight, FontWeight.w700);
      expect(style('row-random', 'Random').fontWeight, FontWeight.w500);
      expect(
        style('row-random', 'Random').color,
        isNot(style('row-general', 'General').color),
      );
    },
  );

  testWidgets(
    'a collapsed parent hides its sub-channels; opening one expands it again',
    (tester) async {
      await size(tester, const Size(1280, 800));
      final state = await makeApp(tester);
      await tester.pumpWidget(ChurrApp(state: state));
      await settle(tester);
      state.prefs.setCollapsed('server-0/design', true);
      await settle(tester);
      expect(find.byKey(const Key('row-icons')), findsNothing);
      state.openConversation(const OpenTarget('server-0', 'icons'));
      await settle(tester);
      expect(find.byKey(const Key('row-icons')), findsOneWidget);
    },
  );

  testWidgets(
    'two servers: the rail switches; the merged tree shows both',
    (tester) async {
      // Tall enough that both servers' rows are laid out at once (the
      // sidebar's foot holds Show hidden, the tools and the "me" block).
      await size(tester, const Size(1280, 1100));
      final state = await makeApp(tester, servers: 2);
      await tester.pumpWidget(ChurrApp(state: state));
      await settle(tester);

      // Rail: one server at a time.
      expect(rich('Friends General'), findsNothing);
      await tester.tap(find.byKey(const Key('server-tile-server-1')));
      await settle(tester);
      expect(rich('Friends General'), findsOneWidget);

      // Merged tree: every server at once, no rail, headings that fold.
      state.prefs.layout = SidebarLayout.merged;
      await settle(tester);
      expect(find.byType(ServerRail), findsNothing);
      expect(find.byKey(const Key('server-heading-server-0')), findsOneWidget);
      expect(find.byKey(const Key('row-general')), findsNWidgets(2));
      await tester.tap(find.byKey(const Key('server-heading-server-1')));
      await settle(tester);
      expect(find.byKey(const Key('row-general')), findsOneWidget);
    },
  );

  testWidgets(
    'a person\'s own sections and order come from their server, under each server, with a heading menu',
    (tester) async {
      await size(tester, const Size(1280, 1100));
      final state = await makeApp(tester);
      await tester.pumpWidget(ChurrApp(state: state));
      await settle(tester);
      final s = state.servers.first;
      await tester.runAsync(
        () => s.db.putSidebarLayout(
          PersonalLayout.fromJson({
            'version': 2,
            'sections': [
              {'id': 1, 'name': 'Focus'},
            ],
            'placements': [
              {'conversation_id': 'random', 'key': 'p:1'},
              {'conversation_id': 'general', 'key': 'p:1'},
            ],
            'groups': [
              {'key': 'p:1', 'sort': 'custom', 'order': ['random', 'general']},
            ],
          }),
        ),
      );
      await settle(tester);
      expect(find.text('FOCUS'), findsOneWidget);
      final focus = tester.getTopLeft(find.text('FOCUS')).dy;
      final random = tester.getTopLeft(find.byKey(const Key('row-random'))).dy;
      final general = tester.getTopLeft(find.byKey(const Key('row-general'))).dy;
      expect(focus < random && random < general, isTrue, reason: 'custom order under the own section');

      // Move… offers every section, the admin's and Channels too, not
      // only the person's own (ruling R84).
      await tester.longPress(find.byKey(const Key('row-random')));
      await settle(tester);
      await tester.tap(find.text('Move…'));
      await settle(tester);
      expect(find.text('Focus'), findsOneWidget);
      expect(find.text('Projects'), findsOneWidget);
      expect(find.textContaining('(where the server lists it)'), findsOneWidget);
      await tester.tapAt(const Offset(1200, 50));
      await settle(tester);

      await tester.tap(find.byKey(const Key('heading-menu-Focus')));
      await settle(tester);
      expect(find.text('Last activity'), findsOneWidget);
      expect(find.text('Rename…'), findsOneWidget);
      expect(find.text('Reset my sidebar…'), findsOneWidget);
      await tester.tapAt(const Offset(1200, 50));
      await settle(tester);

      // The layouts are two; the arrangement under each server is the same.
      state.prefs.layout = SidebarLayout.merged;
      await settle(tester);
      expect(find.text('FOCUS'), findsOneWidget);
    },
  );

  testWidgets('light theme and medium width draw without errors', (
    tester,
  ) async {
    await size(tester, const Size(800, 600));
    final state = await makeApp(tester);
    state.prefs.theme = ThemeChoice.light;
    await tester.pumpWidget(ChurrApp(state: state));
    await settle(tester);
    expect(
      Theme.of(tester.element(find.byKey(const Key('sidebar-list'))))
          .brightness,
      Brightness.light,
    );
    await tester.tap(find.byKey(const Key('row-design')));
    await settle(tester);
    expect(
      find.byKey(const Key('sidebar-list')),
      findsOneWidget,
      reason: 'two panes from 600 up',
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('with no server the app opens on "Add a server"', (tester) async {
    await size(tester, const Size(1280, 800));
    final state = await makeApp(tester, servers: 0);
    await tester.pumpWidget(ChurrApp(state: state));
    await settle(tester);
    expect(find.byKey(const Key('server-address')), findsOneWidget);
    expect(
      find.byKey(const Key('dev-add')),
      findsOneWidget,
      reason: 'debug builds offer the devseed token form',
    );
  });
}
