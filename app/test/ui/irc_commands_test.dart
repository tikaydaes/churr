import 'package:churr/core/api/conversation_ops.dart';
import 'package:churr/core/db/conversation_queries.dart';
import 'package:churr/state/server_state.dart';
import 'package:churr/ui/layout/server_notices.dart';
import 'package:churr/ui/screens/conversation/command_dialogs.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import '../forms_support.dart';

/// The dialogs and notices of ruling R74: /help (grouped, searchable, a
/// command chosen goes back to the box), /banlist with lifting, the
/// requests to join a channel that its managers answer, the server
/// broadcast banner (shown, said once to a screen reader, dismissed), and
/// the answer to a knock.
void main() {
  quietDrift();

  SlashCommand cmd(String name, {String kind = 'builtin', String d = ''}) =>
      SlashCommand({'name': name, 'kind': kind, 'description': d});

  test('help groups the commands in the table\'s order and searches', () {
    final listed = [
      cmd('poll', d: 'Posts a poll'),
      cmd('ban', d: 'Bans someone from this channel'),
      cmd('me', d: 'Says what you are doing'),
      cmd('part', d: 'as /leave'),
      cmd('hello', kind: 'saved_reply', d: 'Welcome!'),
      cmd('deploy', kind: 'bot', d: 'Ships it'),
    ];
    final all = helpGroups(listed, '');
    expect(
      [for (final (g, _) in all) g],
      ['post', 'members', 'forms', 'saved', 'bots'],
    );
    // An alias row is shown with its command, and is found by its name.
    expect(all.expand((g) => g.$2).map((c) => c.name), isNot(contains('part')));
    expect(helpGroups(listed, '/kb').single.$2.single.name, 'ban');
    expect(helpGroups(listed, 'ships').single.$1, 'bots');
    expect(helpGroups(listed, 'nothing like it'), isEmpty);
  });

  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets('/help lists, searches and hands back a command ($label)', (
      tester,
    ) async {
      late final ServerState s;
      await tester.runAsync(() async {
        s = await fakeServerState(
          FakeServer((_) => null),
          conversations: [channel('general', 'General')],
        );
        await s.db.putCommands('general', [
          cmd('me', d: 'Says what you are doing'),
          cmd('ban', d: 'Bans someone from this channel'),
          cmd('hello', kind: 'saved_reply', d: 'Welcome!'),
        ]);
      });
      String? chosen;
      await pumpScreen(
        tester,
        appWith([s]),
        Builder(
          builder: (context) => TextButton(
            onPressed: () async =>
                chosen = await showHelpDialog(context, s, 'general'),
            child: const Text('open'),
          ),
        ),
        size: size,
      );
      await tester.tap(find.text('open'));
      await settleAsync(tester);
      expect(find.text('POSTING'), findsOneWidget);
      expect(find.text('MEMBERS AND MODERATION'), findsOneWidget);
      expect(find.text('SAVED REPLIES AND COMMUNITY COMMANDS'), findsOneWidget);
      expect(find.textContaining('also /kickban, /kb'), findsOneWidget);
      await tester.enterText(find.byKey(const Key('help-search')), 'welcome');
      await tester.pump();
      expect(find.byKey(const Key('help-ban')), findsNothing);
      await tester.tap(find.byKey(const Key('help-hello')));
      await settleAsync(tester);
      expect(chosen, 'hello');
      expect(tester.takeException(), isNull);
    });

    testWidgets('/banlist lists the bans and lifts one ($label)', (
      tester,
    ) async {
      final requests = <http.Request>[];
      late final ServerState s;
      await tester.runAsync(() async {
        s = await fakeServerState(
          FakeServer((r) {
            requests.add(r);
            if (r.method == 'DELETE') return (204, null);
            if (r.url.path.endsWith('/bans')) {
              return (
                200,
                {
                  'bans': [
                    {
                      'id': 'b1',
                      'conversation_id': 'general',
                      'account_id': 'sam',
                      'reason': 'spam',
                      'created_by_id': 'me',
                      'created_at': '2026-11-14T09:30:00.000Z',
                    },
                  ],
                },
              );
            }
            return null;
          }),
          conversations: [channel('general', 'General', level: 'manager')],
        );
      });
      await pumpScreen(
        tester,
        appWith([s]),
        Builder(
          builder: (context) => TextButton(
            onPressed: () => showBanListDialog(context, s, 'general'),
            child: const Text('open'),
          ),
        ),
        size: size,
      );
      await tester.tap(find.text('open'));
      await settleAsync(tester);
      expect(find.text('Banned from #General'), findsOneWidget);
      expect(find.textContaining('“spam”'), findsOneWidget);
      await tester.tap(find.byKey(const Key('banlist-lift-sam')));
      await settleAsync(tester);
      expect(
        requests.where(
          (r) => r.method == 'DELETE' && r.url.path.endsWith('/bans/sam'),
        ),
        hasLength(1),
      );
      expect(find.text('Nobody is banned from this channel.'), findsOneWidget);
      expect(tester.takeException(), isNull);
    });

    testWidgets('requests to join: the bar, adding and declining ($label)', (
      tester,
    ) async {
      final requests = <http.Request>[];
      final waiting = [
        {
          'id': 'k1',
          'conversation_id': 'council',
          'account_id': 'sam',
          'outcome': 'pending',
          'created_at': '2026-11-16T09:00:00.000Z',
        },
        {
          'id': 'k2',
          'conversation_id': 'council',
          'account_id': 'lee',
          'outcome': 'pending',
          'created_at': '2026-11-16T09:05:00.000Z',
        },
      ];
      late final ServerState s;
      await tester.runAsync(() async {
        s = await fakeServerState(
          FakeServer((r) {
            requests.add(r);
            if (r.method == 'POST' && r.url.path.contains('/knocks/')) {
              final id = r.url.pathSegments.last;
              waiting.removeWhere((k) => k['id'] == id);
              return (200, {'knock': <String, Object?>{}});
            }
            if (r.url.path.endsWith('/knocks')) {
              return (200, {'knocks': waiting});
            }
            return null;
          }),
          conversations: [
            channel('council', 'Council', level: 'manager', private: true),
          ],
        );
      });
      late final KnocksWatcher watcher;
      await tester.runAsync(() async {
        watcher = KnocksWatcher(s, 'council');
        await watcher.refresh();
      });
      await pumpScreen(
        tester,
        appWith([s]),
        Column(children: [KnocksBar(watcher: watcher)]),
        size: size,
      );
      await tester.pump();
      expect(find.text('2 people ask to join'), findsOneWidget);
      await tester.tap(find.byKey(const Key('knocks-review')));
      await settleAsync(tester);
      expect(find.text('Asking to join #Council'), findsOneWidget);
      await tester.tap(find.byKey(const Key('knock-accept-sam')));
      await settleAsync(tester);
      await tester.tap(find.byKey(const Key('knock-decline-lee')));
      await settleAsync(tester);
      final answers = [
        for (final r in requests)
          if (r.method == 'POST') '${r.url.pathSegments.last} ${r.body}',
      ];
      expect(answers, ['k1 {"accept":true}', 'k2 {"accept":false}']);
      expect(find.text('Nobody is asking to join.'), findsOneWidget);
      watcher.dispose();
      expect(tester.takeException(), isNull);
    });

    testWidgets('the server broadcast: shown, said once, dismissed ($label)', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      late final ServerState s;
      await tester.runAsync(() async {
        s = await fakeServerState(FakeServer((_) => null));
      });
      final app = appWith([s]);
      await pumpScreen(
        tester,
        app,
        ServerNoticesHost(app: app, child: const Text('the chat')),
        size: size,
      );
      final until = DateTime.now()
          .toUtc()
          .add(const Duration(minutes: 10))
          .toIso8601String();
      await tester.runAsync(
        () => s.sync.applyEvent({
          'type': 'server.broadcast',
          'broadcast': {
            'id': 'b1',
            'text': 'Upgrading in **5 minutes**',
            'by_id': 'sam',
            'created_at': '2026-11-16T09:00:00.000Z',
            'until': until,
          },
        }),
      );
      await settleAsync(tester);
      expect(find.byKey(const Key('broadcast-b1')), findsOneWidget);
      expect(find.textContaining('SERVER NOTICE'), findsOneWidget);
      expect(find.textContaining('from Sam Okafor'), findsOneWidget);
      expect(find.textContaining('5 minutes'), findsOneWidget);
      expect(find.text('the chat'), findsOneWidget);
      expect(tester.takeAnnouncements().map((a) => a.message), [
        'Server notice: Upgrading in **5 minutes**',
      ]);
      // Not said again when something else changes.
      s.dismissedBroadcasts.add('other');
      await tester.pump();
      expect(tester.takeAnnouncements(), isEmpty);

      await tester.tap(find.byKey(const Key('broadcast-dismiss-b1')));
      await tester.pump();
      expect(find.byKey(const Key('broadcast-b1')), findsNothing);
      handle.dispose();
      expect(tester.takeException(), isNull);
    });
  }

  testWidgets('a broadcast that ran out, or was cleared, is not shown', (
    tester,
  ) async {
    late final ServerState s;
    await tester.runAsync(() async {
      s = await fakeServerState(FakeServer((_) => null));
      await s.sync.applyEvent({
        'type': 'server.broadcast',
        'broadcast': {
          'id': 'old',
          'text': 'gone',
          'by_id': 'sam',
          'until': '2020-01-01T00:00:00.000Z',
        },
      });
      await Future<void>.delayed(const Duration(milliseconds: 20));
    });
    expect(s.broadcast, isNotNull);
    expect(s.broadcastShowing, isNull);
    await tester.runAsync(() async {
      await s.sync.applyEvent({'type': 'server.broadcast'});
      await Future<void>.delayed(const Duration(milliseconds: 20));
    });
    expect(s.broadcast, isNull);
  });

  testWidgets('the one who knocked is told the answer', (tester) async {
    late final ServerState s;
    await tester.runAsync(() async {
      s = await fakeServerState(FakeServer((_) => null));
    });
    final app = appWith([s]);
    await pumpScreen(
      tester,
      app,
      ServerNoticesHost(app: app, child: const Text('the chat')),
    );
    await tester.runAsync(
      () => s.sync.applyEvent({
        'type': 'knock.answered',
        'conversation_name': 'council',
        'knock': {'id': 'k1', 'account_id': 'me', 'outcome': 'accepted'},
      }),
    );
    await settleAsync(tester);
    expect(find.text('You were added to #council.'), findsOneWidget);
  });
}
