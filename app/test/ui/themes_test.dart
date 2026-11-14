// Themes in the app: choosing (built-in, saved, each server's default,
// follow the system), local copies, the editor, the library, theme cards,
// attached .churrtheme files and theme links. Fake servers, no network.
import 'dart:convert';

import 'package:churr/core/models/message.dart';
import 'package:churr/core/models/server.dart';
import 'package:churr/core/themes/palette.dart';
import 'package:churr/core/themes/theme.dart';
import 'package:churr/state/app_prefs.dart';
import 'package:churr/ui/app.dart';
import 'package:churr/ui/message/markdown_view.dart';
import 'package:churr/ui/message/media.dart';
import 'package:churr/ui/message/message_tile.dart';
import 'package:churr/ui/screens/themes/theme_editor.dart';
import 'package:churr/ui/screens/themes/theme_library.dart';
import 'package:churr/ui/screens/themes/theme_link.dart';
import 'package:churr/ui/screens/themes/themes_screen.dart';
import 'package:churr/ui/theme/tokens.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import '../account_support.dart';

const tid = '0193b000-0000-7000-8000-000000000001';
const lid = '0193b000-0000-7000-8000-000000000002';

Map<String, dynamic> themeJson(
  String id,
  String name, {
  String kind = 'dark',
  Map<String, String>? colours,
  bool published = false,
  String owner = 'me',
}) => {
  'id': id,
  'owner_id': owner,
  'name': name,
  'kind': kind,
  'colours': colours ?? {...BuiltInThemes.dark.colours, 'pink': '#ff70d6'},
  'published': published,
  'created_at': '2026-11-01T00:00:00.000Z',
  'updated_at': '2026-11-01T00:00:00.000Z',
};

/// Scrolls the first scrollable until [f] is built and in view.
Future<void> reveal(WidgetTester tester, Finder f) async {
  await tester.scrollUntilVisible(
    f,
    200,
    scrollable: find.byType(Scrollable).first,
  );
  await tester.pump();
}

void main() {
  group('choosing', () {
    test(
      'a saved theme becomes the one in use, and a local copy is kept',
      () async {
        final (app, fakes) = await makeAccountApp(null);
        fakes[0].on('GET /api/v1/me/themes', {
          'themes': [themeJson(tid, 'Night shift')],
        });
        final themes = app.themes;
        expect(themes.current(dark: true), BuiltInThemes.dark);
        await themes.refresh(app.servers[0]);
        final saved = themes.mine['server-0']!.single;
        expect(saved.serverAddress, 'https://s0.example.org');
        await themes.choose(saved);
        expect(app.prefs.theme, ThemeChoice.dark);
        expect(themes.current(dark: true).ref, saved.ref);
        expect(app.prefs.raw('theme_copies'), contains('Night shift'));
        // Offline at the next start: nothing fetched, the copy still applies.
        themes.mine.clear();
        expect(themes.current(dark: true).name, 'Night shift');
        expect(
          ChurrColors.forTheme(themes.current(dark: true)).pink,
          const Color(0xFFFF70D6),
        );
      },
    );

    test(
      'follow the system picks between the chosen dark and light themes',
      () async {
        final (app, _) = await makeAccountApp(null);
        final light = ChurrTheme(
          id: '',
          name: 'Paper',
          kind: kindLight,
          colours: {...BuiltInThemes.light.colours, 'cyan': '#00635c'},
        );
        app.prefs.theme = ThemeChoice.system;
        await app.themes.choose(light);
        // Choosing under "follow the system" keeps following it.
        expect(app.prefs.theme, ThemeChoice.system);
        expect(app.themes.current(dark: false).name, 'Paper');
        expect(app.themes.current(dark: true), BuiltInThemes.dark);
        await app.themes.setChosen(BuiltInThemes.darkHd);
        expect(app.themes.current(dark: true), BuiltInThemes.darkHd);
      },
    );

    test("each server's default, changing with the selected server", () async {
      final (app, _) = await makeAccountApp(null);
      final themes = app.themes;
      themes.perServer = true;
      app.servers[0].info = ServerInfo.fromJson({
        'name': 's0.example.org',
        'default_themes': {
          'dark': {
            'id': tid,
            'name': 'Work night',
            'kind': 'dark',
            'author_id': 'me',
            'colours': {...BuiltInThemes.dark.colours, 'cyan': '#66f2e6'},
          },
        },
      });
      app.selectServer('server-0');
      expect(themes.current(dark: true).name, 'Work night');
      // The other server has no default: the person's own choice.
      app.selectServer('server-1');
      expect(themes.current(dark: true), BuiltInThemes.dark);
      // The defaults are kept on the device for an offline start.
      expect(app.prefs.raw('server_default_themes'), contains('Work night'));
      app.servers[0].info = null;
      app.selectServer('server-0');
      expect(themes.current(dark: true).name, 'Work night');
    });

    test('a theme that fails is never applied, tried or chosen', () async {
      final (app, _) = await makeAccountApp(null);
      final bad = ChurrTheme(
        id: '',
        name: 'Murky',
        kind: kindDark,
        colours: {...BuiltInThemes.dark.colours, 'dim': '#6a6890'},
      );
      app.themes.tryTheme(bad);
      await app.themes.choose(bad);
      expect(app.themes.trying, isNull);
      expect(app.themes.current(dark: true), BuiltInThemes.dark);
    });
  });

  testWidgets('the app draws with the theme in use, and with one being tried', (
    tester,
  ) async {
    await sized(tester, desktop);
    final (app, _) = await makeAccountApp(tester);
    await tester.pumpWidget(ChurrApp(state: app));
    await settleAll(tester);
    Color bg() =>
        ChurrColors.of(tester.element(find.byType(Scaffold).first)).bg;
    expect(bg(), const Color(0xFF0D1026));
    final tried = ChurrTheme(
      id: '',
      name: 'Deep sea',
      kind: kindDark,
      colours: {
        ...BuiltInThemes.dark.colours,
        'bg': '#06121f',
        'panel': '#0c1d30',
        'raise': '#13283f',
      },
    );
    app.themes.tryTheme(tried);
    await tester.pump();
    expect(bg(), const Color(0xFF06121F));
    expect(find.byKey(const Key('trying-bar')), findsOneWidget);
    await tester.tap(find.byKey(const Key('trying-stop')));
    await tester.pump();
    expect(bg(), const Color(0xFF0D1026));
    expect(find.byKey(const Key('trying-bar')), findsNothing);
  });

  for (final size in [phone, desktop]) {
    final label = size == phone ? 'phone' : 'desktop';

    testWidgets(
      '$label: the editor measures, fixes and saves to the chosen server',
      (tester) async {
        await sized(tester, size);
        final (app, fakes) = await makeAccountApp(tester);
        fakes[1].on('POST /api/v1/me/themes', {
          'theme': themeJson(tid, 'Mine'),
        }, status: 201);
        await tester.pumpWidget(
          harness(
            app,
            ThemeEditorScreen(
              start: BuiltInThemes.dark,
              server: app.servers[1],
            ),
          ),
        );
        await settleAll(tester);
        expect(find.byKey(const Key('theme-preview')), findsOneWidget);
        // A pink too dark to read on the ground.
        await reveal(tester, find.byKey(const Key('hex-pink')));
        await tester.enterText(find.byKey(const Key('hex-pink')), '#c0207f');
        await tester.pump();
        await reveal(tester, find.byKey(const Key('pair-pink-bg-4.5')));
        expect(find.text('3.35:1'), findsOneWidget);
        final save = find.byKey(const Key('theme-save'));
        await reveal(tester, save);
        expect(tester.widget<FilledButton>(save).onPressed, isNull);
        // "Fix" makes the smallest change that passes.
        await tester.scrollUntilVisible(
          find.byKey(const Key('fix-pink-bg')),
          -200,
          scrollable: find.byType(Scrollable).first,
        );
        await tester.tap(find.byKey(const Key('fix-pink-bg')));
        await tester.pump();
        await tester.scrollUntilVisible(
          find.byKey(const Key('hex-pink')),
          -200,
          scrollable: find.byType(Scrollable).first,
        );
        final pink =
            (tester
                    .widget<TextField>(find.byKey(const Key('hex-pink')))
                    .controller!)
                .text;
        final parsed = parseColours({
          ...BuiltInThemes.dark.colours,
          'pink': pink,
        }).$1!;
        expect(check(kindDark, parsed), isNull);
        expect(find.byKey(const Key('fix-pink-bg')), findsNothing);
        await tester.scrollUntilVisible(
          find.byKey(const Key('theme-name')),
          -300,
          scrollable: find.byType(Scrollable).first,
        );
        await tester.enterText(find.byKey(const Key('theme-name')), 'Mine');
        await tester.pump();
        await reveal(tester, save);
        await tester.tap(save);
        await settleAll(tester);
        final post = fakes[1].requests.lastWhere(
          (r) => r.url.path == '/api/v1/me/themes',
        );
        final body = jsonDecode(post.body) as Map;
        expect(body['name'], 'Mine');
        expect(body['kind'], 'dark');
        expect((body['colours'] as Map)['pink'], pink);
        expect(app.themes.mine['server-1']!.single.id, tid);
      },
    );
  }

  testWidgets("the editor shows the server's words when it refuses", (
    tester,
  ) async {
    await sized(tester, desktop);
    final (app, fakes) = await makeAccountApp(tester);
    fakes[0].on('POST /api/v1/me/themes', {
      'error': {
        'code': 'conflict',
        'message': 'You have 50 themes already. Delete one first.',
      },
    }, status: 409);
    await tester.pumpWidget(
      harness(
        app,
        ThemeEditorScreen(start: BuiltInThemes.light, server: app.servers[0]),
      ),
    );
    await settleAll(tester);
    await reveal(tester, find.byKey(const Key('theme-save')));
    await tester.tap(find.byKey(const Key('theme-save')));
    await settleAll(tester);
    expect(
      find.text('You have 50 themes already. Delete one first.'),
      findsOneWidget,
    );
  });

  testWidgets('editing one of my own: publish, unpublish, delete', (
    tester,
  ) async {
    await sized(tester, desktop);
    final (app, fakes) = await makeAccountApp(tester);
    fakes[0].on('GET /api/v1/me/themes', {
      'themes': [themeJson(tid, 'Night shift')],
    });
    fakes[0].on('POST /api/v1/me/themes/$tid/publish', {
      'theme': themeJson(tid, 'Night shift', published: true),
    });
    fakes[0].routes['DELETE /api/v1/me/themes/$tid'] = (_) =>
        http.Response('', 204);
    await tester.runAsync(() => app.themes.refresh(app.servers[0]));
    final mine = app.themes.mine['server-0']!.single;
    await tester.pumpWidget(harness(app, ThemeEditorScreen(start: mine)));
    await settleAll(tester);
    expect(
      (tester.widget<TextField>(find.byKey(const Key('theme-name'))))
          .controller!
          .text,
      'Night shift',
    );
    await reveal(tester, find.byKey(const Key('theme-publish')));
    await tester.tap(find.byKey(const Key('theme-publish')));
    await settleAll(tester);
    expect(fakes[0].calls, contains('POST /api/v1/me/themes/$tid/publish'));
    expect(app.themes.mine['server-0']!.single.published, isTrue);
    await reveal(tester, find.byKey(const Key('theme-delete')));
    await tester.tap(find.byKey(const Key('theme-delete')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('confirm-delete-theme')));
    await settleAll(tester);
    expect(fakes[0].calls, contains('DELETE /api/v1/me/themes/$tid'));
    expect(app.themes.mine['server-0'], isEmpty);
  });

  testWidgets(
    'the picker lists every server\'s themes, marked with their server',
    (tester) async {
      await sized(tester, desktop);
      final (app, fakes) = await makeAccountApp(tester);
      fakes[0].on('GET /api/v1/me/themes', {
        'themes': [themeJson(tid, 'Night shift')],
      });
      fakes[1].on('GET /api/v1/me/themes', {
        'themes': [
          themeJson(
            lid,
            'Paper',
            kind: 'light',
            colours: BuiltInThemes.light.colours,
          ),
        ],
      });
      await tester.pumpWidget(harness(app, const ThemesScreen()));
      await settleAll(tester);
      expect(find.text('SAVED ON WORK'), findsOneWidget);
      expect(find.text('SAVED ON FRIENDS'), findsOneWidget);
      expect(find.text('Night shift'), findsOneWidget);
      expect(find.text('Paper'), findsOneWidget);
      await tester.tap(find.text('Paper'));
      await settleAll(tester);
      expect(app.themes.chosen(dark: false).name, 'Paper');
      expect(app.prefs.theme, ThemeChoice.light);
    },
  );

  for (final size in [phone, desktop]) {
    testWidgets(
      '${size == phone ? 'phone' : 'desktop'}: the library: search, try, add',
      (tester) async {
        await sized(tester, size);
        final (app, fakes) = await makeAccountApp(tester);
        fakes[0].routes['GET /api/v1/themes'] = (r) => http.Response(
          jsonEncode({
            'themes': [
              if ((r.url.queryParameters['q'] ?? '').isEmpty ||
                  'Sunrise'.toLowerCase().contains(
                    r.url.queryParameters['q']!.toLowerCase(),
                  ))
                themeJson(
                  lid,
                  'Sunrise',
                  kind: 'light',
                  colours: BuiltInThemes.light.colours,
                  published: true,
                  owner: 'sam',
                ),
              if ((r.url.queryParameters['q'] ?? '').isEmpty)
                themeJson(tid, 'Night shift', published: true),
            ],
          }),
          200,
        );
        fakes[0].on('POST /api/v1/themes/$lid/add', {
          'theme': themeJson(
            '0193b000-0000-7000-8000-000000000003',
            'Sunrise',
            kind: 'light',
            colours: BuiltInThemes.light.colours,
          ),
        }, status: 201);
        await tester.pumpWidget(
          harness(app, ThemeLibraryScreen(server: app.servers[0])),
        );
        await settleAll(tester);
        expect(find.text('Sunrise'), findsOneWidget);
        expect(find.text('Night shift'), findsOneWidget);
        expect(find.text('by Sam Okafor'), findsOneWidget);
        await tester.enterText(find.byKey(const Key('library-search')), 'sun');
        await tester.pump(const Duration(milliseconds: 350));
        await settleAll(tester);
        expect(find.text('Night shift'), findsNothing);
        await tester.tap(find.byKey(const Key('library-try-$lid')));
        expect(app.themes.trying?.name, 'Sunrise');
        await tester.tap(find.byKey(const Key('library-add-$lid')));
        await settleAll(tester);
        expect(fakes[0].calls, contains('POST /api/v1/themes/$lid/add'));
        expect(app.themes.mine['server-0']!.single.name, 'Sunrise');
      },
    );
  }

  testWidgets(
    'a message card carrying a theme, and an attached .churrtheme file',
    (tester) async {
      await sized(tester, desktop);
      final (app, fakes) = await makeAccountApp(tester);
      final file = jsonEncode({
        'name': 'Harbour',
        'kind': 'dark',
        'colours': {...BuiltInThemes.dark.colours, 'cyan': '#66f2e6'},
      });
      fakes[0].routes['GET /api/v1/files/f1/Harbour.churrtheme'] = (_) =>
          http.Response(file, 200);
      final m = Message.fromJson({
        'id': 'm1',
        'conversation_id': 'general',
        'seq': 5,
        'author_id': 'sam',
        'created_at': '2026-11-14T09:00:00.000Z',
        'text': '',
        'cards': [
          {
            'preview_url': 'https://s0.example.org/themes/$tid',
            'theme': {
              'id': tid,
              'name': 'Night shift',
              'kind': 'dark',
              'author_id': 'sam',
              'colours': {...BuiltInThemes.dark.colours, 'pink': '#ff70d6'},
            },
          },
        ],
        'files': [
          {
            'id': 'f1',
            'name': 'Harbour.churrtheme',
            'size': file.length,
            'media_type': 'application/octet-stream',
            'kind': 'file',
            'url': '/api/v1/files/f1/Harbour.churrtheme',
          },
        ],
      });
      fakes[0].on('POST /api/v1/themes/$tid/add', {
        'theme': themeJson(
          '0193b000-0000-7000-8000-000000000009',
          'Night shift',
        ),
      }, status: 201);
      await tester.pumpWidget(
        harness(
          app,
          SingleChildScrollView(
            child: MessageTile(
              server: app.servers[0],
              message: m,
              markdown: const MarkdownContext(),
            ),
          ),
        ),
      );
      await settleAll(tester);
      expect(find.text('Night shift'), findsOneWidget);
      expect(find.text('by Sam Okafor'), findsOneWidget);
      expect(find.byType(ThemeFileCard), findsOneWidget);
      expect(find.text('Harbour'), findsOneWidget);
      expect(find.byKey(const Key('theme-card-try')), findsNWidgets(2));
      // Files come before cards in a message: the card is the last.
      await tester.tap(find.byKey(const Key('theme-card-try')).last);
      expect(app.themes.trying?.name, 'Night shift');
      await tester.tap(find.byKey(const Key('theme-card-add')).last);
      await settleAll(tester);
      expect(fakes[0].calls, contains('POST /api/v1/themes/$tid/add'));
    },
  );

  testWidgets('a tapped theme link on a signed-in server opens its card', (
    tester,
  ) async {
    await sized(tester, desktop);
    final (app, fakes) = await makeAccountApp(tester);
    fakes[1].on('GET /api/v1/themes/$lid', {
      'theme': themeJson(
        lid,
        'Paper',
        kind: 'light',
        colours: BuiltInThemes.light.colours,
        owner: 'sam',
      ),
    });
    await tester.pumpWidget(
      harness(
        app,
        Builder(
          builder: (context) => TextButton(
            onPressed: () => openMessageLink(
              context,
              app,
              'https://s1.example.org/themes/$lid',
            ),
            child: const Text('link'),
          ),
        ),
      ),
    );
    await tester.tap(find.text('link'));
    await settleAll(tester);
    expect(fakes[1].calls, contains('GET /api/v1/themes/$lid'));
    expect(find.text('Paper'), findsOneWidget);
    expect(find.byKey(const Key('theme-card-add')), findsOneWidget);
  });

  testWidgets(
    'App settings: dark, light or system; the chosen pair; each server\'s default',
    (tester) async {
      await sized(tester, phone);
      final (app, _) = await makeAccountApp(tester);
      await tester.pumpWidget(
        harness(app, const SingleChildScrollView(child: ThemeChoiceSection())),
      );
      await settleAll(tester);
      expect(find.text('DARK THEME'), findsOneWidget);
      expect(find.text('LIGHT THEME'), findsOneWidget);
      await tester.tap(find.byKey(const Key('theme-per-server')));
      await tester.pump();
      expect(app.themes.perServer, isTrue);
      await tester.tap(find.byKey(const Key('change-light')));
      await settleAll(tester);
      await tester.tap(find.byKey(const Key('pick-theme-builtin:light-hd')));
      await settleAll(tester);
      expect(app.themes.chosen(dark: false), BuiltInThemes.lightHd);
      // Changing the light theme does not switch the app to light.
      expect(app.prefs.theme, ThemeChoice.dark);
    },
  );
}
