// How the catalogue's values look in the app (docs/DESIGN-MODULES.md
// sections 3.2 and 9.1), as e2e/types.mjs holds the web chat's: a roll as
// a message's card and as inline dice, through the catalogue, with the
// text drawn as text; and what cannot be drawn left as its text, saying
// where a newer app would show more. The checks themselves are
// test/core/types_test.dart.

import 'package:churr/core/commands/chance_live.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/l10n/app_localizations.dart';
import 'package:churr/state/server_state.dart';
import 'package:churr/ui/message/chance_view.dart';
import 'package:churr/ui/message/markdown_view.dart';
import 'package:churr/ui/message/message_tile.dart';
import 'package:churr/ui/theme/theme.dart';
import 'package:churr/ui/types/dice_roll/dice_roll_view.dart';
import 'package:churr/ui/types/draw.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../forms_support.dart';

const phone = Size(390, 800), desktop = Size(1440, 900);

Map<String, dynamic> _data(String dice, List<int> rolls, int plus) => {
  'dice': dice,
  'rows': [
    {
      'dice': dice,
      'terms': [
        {
          'sign': 1,
          'count': rolls.length,
          'sides': 6,
          'rolls': rolls,
          'value': rolls.fold<int>(0, (a, b) => a + b),
        },
        {'sign': 1, 'value': plus},
      ],
      'total': rolls.fold<int>(0, (a, b) => a + b) + plus,
    },
  ],
};

final _roll = {
  'kind': 'dice_roll',
  'level': 1,
  'by': 'dice',
  'data': _data('2d6+3', [4, 4], 3),
};

Map<String, dynamic> _inline(Object object, {int at = 10, int len = 9}) => {
  'type': 'object',
  'at': at,
  'len': len,
  'object': object,
};

Future<void> _text(
  WidgetTester tester,
  String text,
  List<Map<String, dynamic>> entities, {
  Size size = phone,
  MarkdownContext mc = const MarkdownContext(),
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    MaterialApp(
      theme: buildTheme(dark: false),
      localizationsDelegates: L10n.localizationsDelegates,
      home: Scaffold(
        body: MarkdownView(
          text: text,
          entities: [for (final e in entities) Entity.fromJson(e)],
          context_: mc,
        ),
      ),
    ),
  );
}

/// All the text drawn, joined.
String _drawn(WidgetTester tester) => tester
    .widgetList<RichText>(find.byType(RichText))
    .map((r) => r.text.toPlainText())
    .join('|');

void main() {
  quietDrift();

  group('a roll message is the kind\'s card', () {
    Future<ServerState> server(WidgetTester tester) async {
      late ServerState s;
      await tester.runAsync(() async {
        s = await fakeServerState(FakeServer((_) => null));
      });
      return s;
    }

    Message message(Object? view) => Message.fromJson({
      'id': 'm-roll',
      'conversation_id': 'c1',
      'author_id': 'sam',
      'text': 'Rolled 2d6+3: [4, 4] + 3 = 11',
      'view': view,
      'created_at': '2026-11-18T09:30:00.000Z',
    });

    for (final size in [phone, desktop]) {
      testWidgets('its faces and total in place of the text, '
          '${size.width.toInt()} wide', (tester) async {
        final s = await server(tester);
        await pumpScreen(
          tester,
          appWith([s]),
          MessageTile(
            server: s,
            message: message(_roll),
            markdown: const MarkdownContext(),
          ),
          size: size,
        );
        expect(find.byType(DiceRollCard), findsOneWidget);
        expect(find.byType(ChanceView), findsOneWidget);
        expect(find.text('Rolled 2d6+3: [4, 4] + 3 = 11'), findsNothing);
        expect(find.text('= 11'), findsOneWidget);
        expect(find.byKey(const Key('needs-newer')), findsNothing);
      });
    }

    testWidgets('a view this client cannot draw: the text, with the note', (
      tester,
    ) async {
      final s = await server(tester);
      await pumpScreen(
        tester,
        appWith([s]),
        MessageTile(
          server: s,
          message: message({'kind': 'tarot_spread', 'level': 4, 'data': {}}),
          markdown: const MarkdownContext(),
        ),
      );
      expect(find.byType(ChanceView), findsNothing);
      expect(_drawn(tester), contains('Rolled 2d6+3: [4, 4] + 3 = 11'));
      expect(find.byKey(const Key('needs-newer')), findsOneWidget);
      expect(
        find.text('Shown as text: this needs a newer version of Churr.'),
        findsOneWidget,
      );
    });

    testWidgets('a view that does not hold together: the text, no note', (
      tester,
    ) async {
      final s = await server(tester);
      await pumpScreen(
        tester,
        appWith([s]),
        MessageTile(
          server: s,
          message: message({..._roll, 'data': 'GgUyZDYrMw'}),
          markdown: const MarkdownContext(),
        ),
      );
      expect(find.byType(ChanceView), findsNothing);
      expect(_drawn(tester), contains('Rolled 2d6+3: [4, 4] + 3 = 11'));
      expect(find.byKey(const Key('needs-newer')), findsNothing);
    });

    testWidgets('arrived live, the card spins once on the message\'s clock', (
      tester,
    ) async {
      final s = await server(tester);
      final m = Message.fromJson({
        ...message(_roll).raw,
        'id': 'm-live',
        'created_at': DateTime.now().toUtc().toIso8601String(),
      });
      ChanceLive.arrivedMessage(m);
      await pumpScreen(
        tester,
        appWith([s]),
        MessageTile(server: s, message: m, markdown: const MarkdownContext()),
      );
      final state = tester.state<ChanceViewState>(find.byType(ChanceView));
      // pumpScreen's settling has carried the spin some way, not to its
      // end: the total waits for the dice.
      expect(state.spinning, isTrue);
      expect(find.byKey(const Key('chance-total-0')), findsNothing);
      await tester.pump(const Duration(seconds: 2));
      expect(state.spinning, isFalse);
      expect(find.text('= 11'), findsOneWidget);
    });

    test('markup in the data is its characters, nothing more', () {
      final m = message({
        ..._roll,
        'data': _data('<img src=x onerror=alert(1)>', [4, 4], 3),
      });
      final card = drawView(m).card! as DiceRollCard;
      expect(card.data['dice'], '<img src=x onerror=alert(1)>');
    });
  });

  group('inline dice are the kind\'s chip', () {
    for (final size in [phone, desktop]) {
      testWidgets('the server\'s roll in the sentence, '
          '${size.width.toInt()} wide', (tester) async {
        final handle = tester.ensureSemantics();
        await _text(tester, 'I hit for [[2d6+3]] damage', [
          _inline(_roll),
        ], size: size);
        final chip = find.byKey(const Key('dice-10'));
        expect(chip, findsOneWidget);
        expect(_drawn(tester), contains('[[2d6+3]] 4 4 +3 = 11'));
        expect(
          tester.getSemantics(find.byType(DiceRollChip)).label,
          'Dice [[2d6+3]]. Rolled 2d6+3: 4, 4, 3; total 11. Opens the roll',
        );
        handle.dispose();
      });
    }

    testWidgets('its tooltip is the roll in words', (tester) async {
      await _text(tester, 'I hit for [[2d6+3]] damage', [_inline(_roll)]);
      final tip = tester.widget<Tooltip>(
        find.ancestor(
          of: find.byKey(const Key('dice-10')),
          matching: find.byType(Tooltip),
        ),
      );
      expect(tip.message, 'Rolled 2d6+3: 4, 4, 3; total 11');
    });

    for (final size in [phone, desktop]) {
      testWidgets('pressed, it opens the whole roll, '
          '${size.width.toInt()} wide', (tester) async {
        await _text(tester, 'I hit for [[2d6+3]] damage', [
          _inline(_roll),
        ], size: size);
        await tester.tap(find.byKey(const Key('dice-10')));
        await tester.pump();
        final dialog = find.byKey(const Key('dice-roll-dialog'));
        expect(dialog, findsOneWidget);
        expect(
          find.descendant(of: dialog, matching: find.text('Dice')),
          findsOneWidget,
        );
        expect(
          find.descendant(of: dialog, matching: find.text('“[[2d6+3]]”')),
          findsOneWidget,
        );
        expect(
          find.descendant(of: dialog, matching: find.byType(ChanceView)),
          findsOneWidget,
        );
        // The roll is shown as it landed: it does not spin.
        expect(
          tester.state<ChanceViewState>(find.byType(ChanceView)).spinning,
          isFalse,
        );
      });
    }

    testWidgets('it spins once when it arrived live', (tester) async {
      var asked = 0;
      await _text(
        tester,
        'I hit for [[2d6+3]] damage',
        [_inline(_roll)],
        mc: MarkdownContext(
          liveObject: (at) => asked++ == 0 ? Duration.zero : null,
        ),
      );
      // From its first frame it rolls, and the total waits for it.
      final state = tester.state<DiceRollChipState>(find.byType(DiceRollChip));
      expect(state.spinning, isTrue);
      expect(_drawn(tester), isNot(contains('= 11')));
      await tester.pump(const Duration(milliseconds: 600));
      expect(state.spinning, isTrue);
      await tester.pump(const Duration(seconds: 2));
      expect(state.spinning, isFalse);
      expect(_drawn(tester), contains('= 11'));
      // A press shows the roll and does not roll it again.
      await tester.tap(find.byKey(const Key('dice-10')));
      await tester.pump(const Duration(milliseconds: 60));
      expect(state.spinning, isFalse);
    });

    testWidgets('dice an edit wrote say nothing was rolled', (tester) async {
      await _text(tester, 'now [[1d6]]', [
        _inline(
          {'kind': 'dice_roll', 'level': 1, 'by': 'dice', 'removed': true},
          at: 4,
          len: 7,
        ),
      ]);
      expect(
        _drawn(tester),
        contains('[[1d6]] (not rolled: written in an edit)'),
      );
    });
  });

  group('what the app cannot draw is its text', () {
    testWidgets('a kind it does not know: its typed text, marked as '
        'needing a newer app', (tester) async {
      await _text(tester, 'track X1 now', [
        _inline(
          {
            'kind': 'parcel',
            'level': 7,
            'data': {'id': 'X1'},
          },
          at: 6,
          len: 2,
        ),
      ]);
      final newer = find.byKey(const Key('object-newer-6'));
      expect(newer, findsOneWidget);
      expect(
        tester.widget<Tooltip>(newer).message,
        'This needs a newer version of Churr to be shown as more than text',
      );
      expect(_drawn(tester), contains('X1'));
    });

    testWidgets('data that does not hold together: its typed text only', (
      tester,
    ) async {
      await _text(tester, 'I hit for [[2d6+3]] damage', [
        _inline({
          ..._roll,
          'data': {'dice': 'x'},
        }),
      ]);
      expect(find.byType(DiceRollChip), findsNothing);
      expect(find.byKey(const Key('object-newer-10')), findsNothing);
      expect(_drawn(tester), contains('I hit for [[2d6+3]] damage'));
    });

    testWidgets('a rendition the app knows is drawn', (tester) async {
      await _text(tester, 'I hit for [[2d6+3]] damage', [
        _inline({'kind': 'later_dice', 'level': 9, 'data': {}, 'alt': _roll}),
      ]);
      expect(find.byType(DiceRollChip), findsOneWidget);
    });
  });
}
