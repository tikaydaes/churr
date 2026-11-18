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
      // The last section: the things people can type.
      expect(find.text('THINGS YOU CAN TYPE'), findsOneWidget);
      await tester.tap(find.byKey(const Key('help-jump')));
      await tester.pumpAndSettle();
      final dice = find.byKey(const Key('help-thing-dice'));
      await tester.ensureVisible(dice);
      await tester.tap(dice);
      await tester.pumpAndSettle();
      expect(find.text('I hit for [[2d6+3]] damage'), findsOneWidget);
      expect(
        find.text('Not in sealed chats: the text stays as typed.'),
        findsOneWidget,
      );
      await tester.enterText(find.byKey(const Key('help-search')), 'welcome');
      await tester.pump();
      expect(find.byKey(const Key('help-ban')), findsNothing);
      // Choosing a command shows its entry; from there it goes in the box.
      await tester.tap(find.byKey(const Key('help-hello')));
      await tester.pump();
      expect(find.byKey(const Key('help-entry-hello')), findsOneWidget);
      expect(find.text('Welcome!'), findsOneWidget);
      await tester.tap(find.byKey(const Key('help-put')));
      await settleAsync(tester);
      expect(chosen, 'hello');
      expect(tester.takeException(), isNull);
    });

    testWidgets('/help roll shows the full entry; a hidden command has none '
        '($label)', (tester) async {
      late final ServerState s;
      final table = [
        // /roll is the dice module's command: its entry is the help the
        // server lists with it, names written in.
        SlashCommand({
          'name': 'roll',
          'kind': 'module',
          'module': 'dice',
          'description': 'Rolls dice, rolled by the server',
          'hint': '[dice]',
          'help': {
            'description': 'Rolls dice, rolled by the server.',
            'examples': [
              {'type': '/roll', 'then': 'Rolls one d20.'},
              {
                'type': '/roll 4d6kh3',
                'then':
                    'Rolls four six-sided dice and keeps the highest three; '
                    'the lowest is shown struck through.',
              },
            ],
            'where': 'Channels and chats. Not in sealed chats.',
            'who': 'Anyone who can post. Not bots.',
          },
        }),
        cmd('flip', d: 'Flips a coin'),
        cmd('pick', d: 'Picks one of the options'),
        cmd('ban', d: 'Bans someone from this channel'),
        cmd('draw', d: 'Draws cards'),
      ];
      await tester.runAsync(() async {
        s = await fakeServerState(
          FakeServer((_) => null),
          conversations: [channel('general', 'General')],
        );
        await s.db.putCommands('general', table);
      });
      final opened = <String?>[];
      Future<void> open(String query) async {
        await pumpScreen(
          tester,
          appWith([s]),
          Builder(
            builder: (context) => TextButton(
              onPressed: () async => opened.add(
                await showHelpDialog(context, s, 'general', query: query),
              ),
              child: const Text('open'),
            ),
          ),
          size: size,
        );
        await tester.tap(find.text('open'));
        await settleAsync(tester);
      }

      await open('roll');
      expect(find.byKey(const Key('help-entry-roll')), findsOneWidget);
      // The title, and the example that rolls one d20.
      expect(find.text('/roll'), findsNWidgets(2));
      expect(find.text('/roll [dice]'), findsOneWidget);
      expect(find.text('/roll 4d6kh3'), findsOneWidget);
      expect(
        find.text(
          'Rolls four six-sided dice and keeps the highest three; the lowest is shown struck through.',
        ),
        findsOneWidget,
      );
      expect(find.textContaining('Not in sealed chats.'), findsOneWidget);
      await tester.tap(find.byKey(const Key('help-back')));
      await tester.pump();
      expect(find.byKey(const Key('help-roll')), findsOneWidget);
      Navigator.of(tester.element(find.byKey(const Key('help-search')))).pop();
      await settleAsync(tester);

      // A related command opens its own entry; back goes to the list.
      await open('flip');
      final pick = find.byKey(const Key('help-related-pick'));
      await tester.ensureVisible(pick);
      await tester.tap(pick);
      await tester.pump();
      expect(find.byKey(const Key('help-entry-pick')), findsOneWidget);
      await tester.tap(find.byKey(const Key('help-back')));
      await tester.pump();
      expect(find.byKey(const Key('help-search')), findsOneWidget);
      expect(find.byKey(const Key('help-flip')), findsOneWidget);
      Navigator.of(tester.element(find.byKey(const Key('help-search')))).pop();
      await settleAsync(tester);

      // /help draw lists every deck of the registry, with its count.
      await open('draw');
      final hanafuda = find.byKey(const Key('help-more-hanafuda'));
      await tester.ensureVisible(hanafuda);
      expect(
        find.descendant(
          of: hanafuda,
          matching: find.textContaining('48 cards'),
        ),
        findsOneWidget,
      );
      Navigator.of(tester.element(hanafuda)).pop();
      await settleAsync(tester);

      // Someone who is not an admin is not listed /exile: no entry for it,
      // only the search, which finds nothing, and /ban does not name it.
      await open('exile');
      expect(find.byKey(const Key('help-entry-exile')), findsNothing);
      expect(find.text('No command matches.'), findsOneWidget);
      Navigator.of(tester.element(find.byKey(const Key('help-search')))).pop();
      await settleAsync(tester);
      await open('kb');
      expect(find.byKey(const Key('help-entry-ban')), findsOneWidget);
      expect(find.byKey(const Key('help-related-exile')), findsNothing);
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

  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets('/knock picks a knock channel and asks to join ($label)', (
      tester,
    ) async {
      final requests = <http.Request>[];
      late final ServerState s;
      await tester.runAsync(() async {
        s = await fakeServerState(
          FakeServer((r) {
            requests.add(r);
            if (r.method == 'POST' && r.url.path.endsWith('/knocks')) {
              return (
                200,
                {
                  'knock': <String, Object?>{'id': 'k1'},
                },
              );
            }
            if (r.url.path.endsWith('/channels')) {
              return (
                200,
                {
                  'conversations': [
                    {
                      'id': 'council',
                      'kind': 'channel',
                      'name': 'Council',
                      'private': true,
                      'access': 'knock',
                      'topic': 'Where it is decided',
                      'member_count': 4,
                      'manager_ids': ['sam'],
                    },
                    {
                      'id': 'open',
                      'kind': 'channel',
                      'name': 'open',
                      'private': false,
                      'access': 'public',
                      'member_count': 9,
                    },
                  ],
                },
              );
            }
            return null;
          }),
        );
      });
      await pumpScreen(
        tester,
        appWith([s]),
        Builder(
          builder: (context) => TextButton(
            onPressed: () => showKnockPicker(context, s),
            child: const Text('open'),
          ),
        ),
        size: size,
      );
      await tester.tap(find.text('open'));
      await settleAsync(tester);
      expect(find.text('#Council'), findsOneWidget);
      expect(find.text('#open'), findsNothing);
      expect(find.textContaining('Managers: Sam Okafor'), findsOneWidget);
      await tester.tap(find.text('Ask to join'));
      await settleAsync(tester);
      expect(
        requests.where(
          (r) =>
              r.method == 'POST' &&
              r.url.path == '/api/v1/conversations/council/knocks',
        ),
        hasLength(1),
      );
      expect(find.textContaining('You asked to join #Council'), findsOneWidget);
      expect(tester.takeException(), isNull);
    });
  }

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
