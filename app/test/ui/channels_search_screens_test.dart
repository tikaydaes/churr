import 'package:churr/state/server_state.dart';
import 'package:churr/ui/forms/preset_dialogs.dart';
import 'package:churr/ui/screens/channels/browse_screen.dart';
import 'package:churr/ui/screens/channels/channel_settings_screen.dart';
import 'package:churr/ui/screens/channels/new_channel_dialog.dart';
import 'package:churr/ui/screens/search/quick_switcher_dialog.dart';
import 'package:churr/ui/screens/search/search_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import '../forms_support.dart';

final aboutList = find
    .descendant(
      of: find.byKey(const Key('about-list')),
      matching: find.byType(Scrollable),
    )
    .first;

void main() {
  quietDrift();

  Future<ServerState> server(
    WidgetTester tester,
    (int, Object?)? Function(http.Request r) answer, {
    String id = 'server-0',
    String name = 'Work',
    List<dynamic>? conversations,
  }) async {
    late ServerState s;
    await tester.runAsync(() async {
      s = await fakeServerState(
        FakeServer(answer),
        id: id,
        name: name,
        conversations: [
          for (final c
              in conversations ??
                  [
                    channel('general', 'General'),
                    channel('design', 'Design', section: 's1'),
                  ])
            c,
        ],
      );
    });
    return s;
  }

  for (final size in [phone, desktop]) {
    final label = size == phone ? 'phone' : 'desktop';

    testWidgets(
      'search merges servers newest first and names a failing one ($label)',
      (tester) async {
        Map<String, dynamic> hit(
          String id,
          String conv,
          String at,
          String text,
        ) => {
          'message': {
            'id': id,
            'conversation_id': conv,
            'seq': 1,
            'author_id': 'sam',
            'text': text,
            'created_at': at,
          },
        };
        final a = await server(
          tester,
          (r) => (
            200,
            {
              'results': [
                hit(
                  'a1',
                  'general',
                  '2026-11-10T10:00:00.000Z',
                  'deploy on monday',
                ),
              ],
            },
          ),
        );
        final b = await server(
          tester,
          id: 'server-1',
          name: 'Friends',
          (r) => (
            200,
            {
              'results': [
                hit(
                  'b1',
                  'design',
                  '2026-11-12T10:00:00.000Z',
                  'deploy the garden',
                ),
              ],
            },
          ),
        );
        final c = await server(
          tester,
          id: 'server-2',
          name: 'Club',
          (r) => (
            503,
            {
              'error': {'code': 'unavailable', 'message': 'Down'},
            },
          ),
        );
        final app = appWith([a, b, c]);
        await pumpScreen(tester, app, SearchScreen(app: app), size: size);
        await tester.enterText(find.byKey(const Key('search-box')), 'deploy');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await settleAsync(tester);
        final garden = tester.getTopLeft(find.text('deploy the garden'));
        final monday = tester.getTopLeft(find.text('deploy on monday'));
        expect(garden.dy, lessThan(monday.dy));
        expect(find.textContaining('Could not search Club'), findsOneWidget);
        expect(find.text('Friends'), findsWidgets);
      },
    );

    testWidgets('the quick switcher finds across servers and opens ($label)', (
      tester,
    ) async {
      final a = await server(tester, (_) => (200, {'accounts': <Object>[]}));
      final b = await server(
        tester,
        id: 'server-1',
        name: 'Friends',
        (_) => (200, {'accounts': <Object>[]}),
        conversations: [
          channel('garden', 'Garden'),
          channel('veg', 'Vegetables', parent: 'garden'),
        ],
      );
      final app = appWith([a, b]);
      await pumpScreen(tester, app, QuickSwitcherView(app: app), size: size);
      await tester.enterText(
        find.byKey(const Key('switcher-box')),
        'garden veg',
      );
      await settleAsync(tester);
      expect(find.text('Garden › Vegetables'), findsOneWidget);
      await tester.testTextInput.receiveAction(TextInputAction.done);
      await settleAsync(tester);
      expect(app.open?.conversationId, 'veg');
      expect(app.open?.serverId, 'server-1');
    });

    testWidgets(
      'browse lists the tree with detail lines and joins, offering sub-channels ($label)',
      (tester) async {
        final browse = [
          channel('art', 'Art', member: false).toJson(),
          channel('books', 'Books', member: false, members: 9).toJson(),
          channel('scifi', 'Sci-fi', member: false, parent: 'books').toJson(),
          channel('books2', 'books', member: true).toJson(),
        ];
        final s = await server(tester, (r) {
          if (r.url.path.endsWith('/join')) {
            return (200, {'conversation': channel('books', 'Books').toJson()});
          }
          if (r.url.queryParameters['parent_id'] == 'books') {
            return (
              200,
              {
                'conversations': [browse[2]],
              },
            );
          }
          return (200, {'conversations': browse});
        });
        final app = appWith([s]);
        await pumpScreen(
          tester,
          app,
          BrowseScreen(app: app, server: s),
          size: size,
        );
        expect(find.text('Sci-fi'), findsOneWidget);
        expect(
          find.textContaining("1 member, you're in it", findRichText: true),
          findsNothing,
        );
        expect(
          find.textContaining('9 members · since March 2026'),
          findsOneWidget,
        );
        // Same-named channels together, the one the person is in first.
        final mine = tester.getTopLeft(find.byKey(const Key('browse-books2')));
        final other = tester.getTopLeft(find.byKey(const Key('browse-books')));
        expect(mine.dy, lessThan(other.dy));
        await tester.tap(find.byKey(const Key('join-books')));
        await settleAsync(tester);
        await tester.pumpAndSettle();
        expect(find.text('Sub-channels of Books'), findsOneWidget);
        expect(find.byKey(const Key('offer-scifi')), findsOneWidget);
      },
    );

    testWidgets(
      'a new channel name already in use is listed, not refused ($label)',
      (tester) async {
        final s = await server(tester, (r) {
          if (r.method == 'GET') return (200, {'conversations': <Object>[]});
          return (
            201,
            {
              'conversation': channel(
                'new',
                'design',
                level: 'manager',
              ).toJson(),
            },
          );
        });
        final app = appWith([s]);
        await pumpScreen(
          tester,
          app,
          NewChannelForm(app: app, server: s),
          size: size,
        );
        await tester.enterText(find.byKey(const Key('channel-name')), 'design');
        await tester.pump(const Duration(milliseconds: 400));
        await settleAsync(tester);
        expect(find.byKey(const Key('same-name')), findsOneWidget);
        await tester.ensureVisible(find.byKey(const Key('channel-create')));
        await tester.tap(find.byKey(const Key('channel-create')));
        await settleAsync(tester);
        expect(app.open?.conversationId, 'new');
      },
    );

    testWidgets(
      'locking needs the word "lock" and says it cannot be undone ($label)',
      (tester) async {
        final requests = <http.Request>[];
        final s = await server(
          tester,
          (r) {
            requests.add(r);
            if (r.url.path.endsWith('/lock')) {
              return (
                200,
                {
                  'conversation': channel(
                    'secret',
                    'Secret',
                    level: 'manager',
                    private: true,
                    lockedAt: '2026-11-14T00:00:00.000Z',
                  ).toJson(),
                },
              );
            }
            if (r.url.path.endsWith('/members')) return (200, {'members': <Object>[]});
            if (r.url.path.endsWith('/groups')) return (200, {'groups': <Object>[]});
            if (r.url.path.endsWith('/pins')) return (200, {'pins': <Object>[]});
            return null;
          },
          conversations: [
            channel('secret', 'Secret', level: 'manager', private: true),
          ],
        );
        final app = appWith([s]);
        await pumpScreen(
          tester,
          app,
          ChannelSettingsScreen(server: s, conversationId: 'secret'),
          size: size,
        );
        expect(find.byKey(const Key('settings-make-private')), findsNothing);
        await tester.scrollUntilVisible(
          find.byKey(const Key('settings-lock')),
          200,
          scrollable: aboutList,
        );
        // The settings above it have grown (who may use broad mentions):
        // the button is brought wholly into view before it is pressed.
        await tester.ensureVisible(find.byKey(const Key('settings-lock')));
        await tester.pump(const Duration(milliseconds: 300));
        await tester.tap(find.byKey(const Key('settings-lock')));
        await tester.pumpAndSettle();
        expect(find.text('This cannot be undone.'), findsOneWidget);
        final go = tester.widget<FilledButton>(
          find.byKey(const Key('confirm-go')),
        );
        expect(go.onPressed, isNull);
        await tester.enterText(find.byKey(const Key('confirm-word')), 'lock');
        await tester.pump();
        await tester.tap(find.byKey(const Key('confirm-go')));
        await settleAsync(tester);
        expect(
          requests.where((r) => r.url.path.endsWith('/lock')),
          hasLength(1),
        );
        expect(
          (await tester.runAsync(() => s.db.conversation('secret')))!
              .conversation
              .isLocked,
          isTrue,
        );
        // In a locked channel the add buttons are gone and a line says why.
        await tester.tap(find.text('Members (5)'));
        await tester.pumpAndSettle();
        expect(find.byKey(const Key('members-locked')), findsOneWidget);
        expect(find.byKey(const Key('members-add')), findsNothing);
      },
    );
  }

  testWidgets('making a public channel private takes its name to confirm', (
    tester,
  ) async {
    final requests = <http.Request>[];
    final s = await server(tester, (r) {
      requests.add(r);
      if (r.method == 'PATCH') {
        return (
          200,
          {
            'conversation': channel(
              'general',
              'General',
              level: 'manager',
              private: true,
            ).toJson(),
          },
        );
      }
      if (r.url.path.endsWith('/members')) return (200, {'members': <Object>[]});
      if (r.url.path.endsWith('/groups')) return (200, {'groups': <Object>[]});
      if (r.url.path.endsWith('/pins')) return (200, {'pins': <Object>[]});
      return null;
    }, conversations: [channel('general', 'General', level: 'manager')]);
    final app = appWith([s]);
    await pumpScreen(
      tester,
      app,
      ChannelSettingsScreen(server: s, conversationId: 'general'),
    );
    await tester.scrollUntilVisible(
      find.byKey(const Key('settings-make-private')),
      200,
      scrollable: aboutList,
    );
    // The knock rows above it (ruling R79) push it further down.
    await tester.ensureVisible(find.byKey(const Key('settings-make-private')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('settings-make-private')));
    await tester.pumpAndSettle();
    expect(
      find.textContaining('can never be made public again'),
      findsOneWidget,
    );
    await tester.enterText(find.byKey(const Key('confirm-word')), 'General');
    await tester.pump();
    await tester.tap(find.byKey(const Key('confirm-go')));
    await settleAsync(tester);
    final patch = requests.singleWhere((r) => r.method == 'PATCH');
    expect(body(patch), {'private': true});
  });

  testWidgets('a manager makes a public channel a knock channel (R79)', (
    tester,
  ) async {
    final requests = <http.Request>[];
    final s = await server(tester, (r) {
      requests.add(r);
      if (r.method == 'PATCH') {
        return (
          200,
          {
            'conversation': {
              ...channel(
                'general',
                'General',
                level: 'manager',
                private: true,
              ).toJson(),
              'access': 'knock',
            },
          },
        );
      }
      if (r.url.path.endsWith('/members')) return (200, {'members': <Object>[]});
      if (r.url.path.endsWith('/groups')) return (200, {'groups': <Object>[]});
      if (r.url.path.endsWith('/pins')) return (200, {'pins': <Object>[]});
      return null;
    }, conversations: [channel('general', 'General', level: 'manager')]);
    final app = appWith([s]);
    await pumpScreen(
      tester,
      app,
      ChannelSettingsScreen(server: s, conversationId: 'general'),
    );
    await tester.scrollUntilVisible(
      find.byKey(const Key('settings-make-knock')),
      200,
      scrollable: aboutList,
    );
    await tester.tap(find.byKey(const Key('settings-make-knock')));
    await settleAsync(tester);
    final patch = requests.singleWhere((r) => r.method == 'PATCH');
    expect(body(patch), {'access': 'knock'});
  });

  testWidgets('the poll dialog posts the same form the command would', (
    tester,
  ) async {
    final requests = <http.Request>[];
    final s = await server(tester, (r) {
      requests.add(r);
      return (
        201,
        {
          'message': {
            'id': 'p1',
            'conversation_id': 'general',
            'seq': 11,
            'author_id': 'me',
            'created_at': '2026-11-14T00:00:00.000Z',
          },
        },
      );
    });
    final app = appWith([s]);
    await pumpScreen(
      tester,
      app,
      Builder(
        builder: (context) => TextButton(
          onPressed: () => runFormCommand(
            context,
            server: s,
            conversationId: 'general',
            command: 'poll',
          ),
          child: const Text('go'),
        ),
      ),
    );
    await tester.tap(find.text('go'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const Key('preset-title')), 'Lunch?');
    await tester.enterText(find.byKey(const Key('preset-option-0')), 'Noodles');
    // One option only: refused in the dialog.
    await tester.tap(find.byKey(const Key('preset-post')));
    await tester.pump();
    expect(find.byKey(const Key('preset-error')), findsOneWidget);
    await tester.enterText(find.byKey(const Key('preset-option-1')), 'Tacos');
    await tester.tap(find.byKey(const Key('preset-post')));
    await settleAsync(tester);
    final form = body(requests.single)['form'] as Map;
    expect(form['preset'], 'poll');
    expect(form['title'], 'Lunch?');
    expect(((form['fields'] as List).single as Map)['options'], [
      {'id': 'o1', 'label': 'Noodles'},
      {'id': 'o2', 'label': 'Tacos'},
    ]);
  });

  testWidgets('a command that reads completely posts without a dialog', (
    tester,
  ) async {
    final requests = <http.Request>[];
    final s = await server(tester, (r) {
      requests.add(r);
      return (
        201,
        {
          'message': {
            'id': 'p1',
            'conversation_id': 'general',
            'seq': 11,
            'author_id': 'me',
            'created_at': '2026-11-14T00:00:00.000Z',
          },
        },
      );
    });
    final app = appWith([s]);
    await pumpScreen(
      tester,
      app,
      Builder(
        builder: (context) => TextButton(
          onPressed: () => runFormCommand(
            context,
            server: s,
            conversationId: 'general',
            command: 'signup',
            args: 'Shifts | Morning (2) | Evening',
          ),
          child: const Text('go'),
        ),
      ),
    );
    await tester.tap(find.text('go'));
    await settleAsync(tester);
    expect(find.byKey(const Key('preset-title')), findsNothing);
    expect((body(requests.single)['form'] as Map)['preset'], 'signup');
  });
}
