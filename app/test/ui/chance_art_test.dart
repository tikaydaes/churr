import 'dart:convert';
import 'dart:io';

import 'package:churr/core/commands/chance_live.dart';
import 'package:churr/l10n/app_localizations.dart';
import 'package:churr/ui/message/chance_art.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/ui/message/chance_view.dart';
import 'package:churr/ui/message/dice_view.dart';
import 'package:churr/ui/message/dice_shape.dart';
import 'package:churr/ui/theme/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

/// The chance artwork (docs/ART.md): which sheet each deck and die draws
/// from (against testdata/art/sheets.json), where a face sits in its sheet,
/// and that without a sheet a slot keeps its text face. The sheets here
/// are placeholders made in the test, in the shape pack.py writes.

const _cards = {
  'sheet': 'french',
  'image': 'french.webp',
  'cell': [176, 246],
  'columns': 8,
  'rows': 7,
  'size': [1408, 1722],
  'faces': {
    'spades-ace': [0, 0],
    'hearts-queen': [3, 1],
    'back': [7, 6],
  },
};

// A one-pixel PNG: the picture itself does not matter here.
final _png = base64Decode(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==',
);

class _Bundle extends CachingAssetBundle {
  _Bundle(this.files);
  final Map<String, List<int>> files;
  final asked = <String>[];

  @override
  Future<ByteData> load(String key) async {
    asked.add(key);
    final f = files[key];
    if (f == null) throw FlutterError('Unable to load asset: "$key".');
    return ByteData.sublistView(Uint8List.fromList(f));
  }
}

// The real tarot index, so the reel's frames find their cards too.
final _tarot = File('assets/chance/tarot.json').readAsBytesSync();

_Bundle _withSheets() => _Bundle({
  'assets/chance/tarot.json': _tarot,
  'assets/chance/tarot.webp': _png,
  'assets/chance/french.json': utf8.encode(jsonEncode(_cards)),
  'assets/chance/french.webp': _png,
});

Future<void> _pump(WidgetTester tester, AssetBundle bundle, Widget child) =>
    tester.pumpWidget(
      MaterialApp(
        theme: buildTheme(dark: true),
        localizationsDelegates: L10n.localizationsDelegates,
        supportedLocales: L10n.supportedLocales,
        home: DefaultAssetBundle(
          bundle: bundle,
          child: Scaffold(body: SingleChildScrollView(child: child)),
        ),
      ),
    );

/// Lets the bundle's futures and the picture's decoding finish.
Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 3; i++) {
    await tester.runAsync(() => Future<void>.delayed(Duration.zero));
    await tester.pump();
  }
}

void main() {
  setUp(ChanceArt.reset);

  testWidgets('inline dice are drawn as /roll\'s, the height of the line', (
    tester,
  ) async {
    final cube = File('assets/chance/dice-cube.json').readAsBytesSync();
    final entity = Entity.fromJson({
      'type': 'dice',
      'at': 0,
      'len': 15,
      'value': '2d6+1d20+3',
      'terms': [
        {
          'sign': 1,
          'count': 2,
          'sides': 6,
          'rolls': [4, 1],
          'value': 5,
        },
        {
          'sign': 1,
          'count': 1,
          'sides': 20,
          'rolls': [17],
          'value': 17,
        },
        {'sign': 1, 'value': 3},
      ],
      'total': 25,
    });
    const style = TextStyle(fontSize: 16);
    Future<void> chip(AssetBundle bundle) => _pump(
      tester,
      bundle,
      DiceChip(
        key: ObjectKey(bundle),
        entity: entity,
        typed: '[[2d6+d20+3]]',
        style: style,
      ),
    );
    // With the cube sheet: two d6 faces from it, the d20 drawn on the fly,
    // each 1.35 times the text's size; the plain 3 stays text.
    final handle = tester.ensureSemantics();
    await chip(
      _Bundle({
        'assets/chance/dice-cube.json': cube,
        'assets/chance/dice-cube.webp': _png,
      }),
    );
    await _settle(tester);
    for (final i in [0, 1, 2]) {
      expect(find.byKey(Key('inline-die-$i')), findsOneWidget);
      expect(
        tester.getSize(find.byKey(Key('inline-die-$i'))),
        const Size.square(16 * dieInLine),
      );
    }
    expect(
      find.descendant(
        of: find.byKey(const Key('inline-die-0')),
        matching: find.byType(ChanceSprite),
      ),
      findsOneWidget,
    );
    expect(
      tester.widget<ChanceSprite>(find.byType(ChanceSprite).first).face,
      'd6-4',
    );
    expect(
      find.descendant(
        of: find.byKey(const Key('inline-die-2')),
        matching: find.byType(DieFace),
      ),
      findsOneWidget,
    );
    expect(find.byKey(const Key('inline-die-3')), findsNothing);
    final label = tester.getSemantics(find.byKey(const Key('dice-0'))).label;
    expect(label, contains('[[2d6+d20+3]]'));
    expect(label, contains('25'));
    handle.dispose();
    // Without the sheet a d6 keeps its number; the d20 is still drawn.
    ChanceArt.reset();
    await chip(_Bundle({}));
    await _settle(tester);
    expect(find.byKey(const Key('inline-die-0')), findsNothing);
    expect(find.byKey(const Key('inline-die-2')), findsOneWidget);
  });

  test('every deck draws from the sheet the inventory gives it', () {
    final table = jsonDecode(
      File('../testdata/art/sheets.json').readAsStringSync(),
    ) as Map;
    final want = <String, String>{
      for (final e in (table['sheets'] as Map).entries)
        for (final d in ((e.value as Map)['decks'] as List).cast<String>())
          d: e.key as String,
    };
    expect(chanceDeckSheets, want);
    final cube = slotArt('roll', null, sides: 6)!;
    expect(cube.sheet, 'dice-cube');
    expect([
      for (var n = 1; n <= 6; n++) cube.face('$n'),
    ], ((table['sheets'] as Map)['dice-cube'] as Map)['faces']);
    expect(slotArt('roll', null, sides: 20), isNull);
    expect(slotDie('roll', sides: 20), 20);
    expect(slotDie('roll', sides: 6), isNull);
    expect(slotDie('roll', sides: 20, still: true), isNull);
    expect(slotDie('flip'), 2);
    expect(slotArt('flip', null), isNull);
    expect(slotArt('draw', 'uno'), isNull);
    expect(slotArt('draw', 'italian')!.face('cups-fante'), 'cups-fante');
  });

  test('a face is found in its cell; a die takes its shape', () {
    final s = ChanceSheet.fromJson(_cards);
    expect(s.image, 'assets/chance/french.webp');
    expect(s.cellOf('hearts-queen'), const Rect.fromLTWH(528, 246, 176, 246));
    expect(s.cellOf('back'), const Rect.fromLTWH(1232, 1476, 176, 246));
    expect(s.cellOf('joker-red'), isNull);
  });

  test('a sheet that is not bundled reads as none, once', () async {
    final b = _Bundle({});
    expect(await ChanceArt.load(b, 'french'), isNull);
    expect(await ChanceArt.load(b, 'french'), isNull);
    expect(b.asked, ['assets/chance/french.json']);
    ChanceArt.reset();
    final noPicture = _Bundle({
      'assets/chance/french.json': utf8.encode(jsonEncode(_cards)),
    });
    expect(await ChanceArt.load(noPicture, 'french'), isNull);
  });

  testWidgets('with no sheets the slots are text, as before', (tester) async {
    await _pump(
      tester,
      _Bundle({}),
      const ChanceView(
        messageId: 'none',
        chance: {
          'kind': 'draw',
          'deck': 'standard',
          'cards': ['hearts-queen'],
        },
      ),
    );
    await _settle(tester);
    expect(find.byType(ChanceSprite), findsNothing);
    expect(tester.widget<Text>(find.byKey(const Key('face-0'))).data, 'Q♥');
  });

  testWidgets('with its sheet a card is drawn from its cell', (tester) async {
    await _pump(
      tester,
      _withSheets(),
      const ChanceView(
        messageId: 'cards',
        chance: {
          'kind': 'draw',
          'deck': 'standard',
          'cards': ['hearts-queen', 'joker-red'],
        },
      ),
    );
    await _settle(tester);
    final sprite = tester.widget<ChanceSprite>(find.byKey(const Key('art-0')));
    expect(sprite.face, 'hearts-queen');
    expect(
      tester.getSize(find.byKey(const Key('slot-0'))),
      const Size(88, 123),
    );
    final pos = tester.widget<Positioned>(
      find.descendant(
        of: find.byKey(const Key('art-0')),
        matching: find.byType(Positioned),
      ),
    );
    expect((pos.left, pos.top, pos.width, pos.height), (-264, -123, 704, 861));
    // A card the sheet lacks keeps its text face.
    expect(find.byKey(const Key('art-1')), findsNothing);
    expect(tester.widget<Text>(find.byKey(const Key('face-1'))).data, 'JK');
    // The names are not listed; each card is labelled with its own.
    expect(find.textContaining('Queen of hearts'), findsNothing);
    expect(find.bySemanticsLabel('Queen of hearts'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a d20 is drawn on the fly, and the reel changes its number', (
    tester,
  ) async {
    const roll = {
      'kind': 'roll',
      'dice': '1d20+1d6',
      'rows': [
        {
          'dice': '1d20+1d6',
          'terms': [
            {
              'sign': 1,
              'count': 1,
              'sides': 20,
              'rolls': [17],
              'value': 17,
            },
            {
              'sign': 1,
              'count': 1,
              'sides': 6,
              'rolls': [4],
              'value': 4,
            },
          ],
          'total': 21,
        },
      ],
    };
    // No sheet for it at all; the d6 still takes its cube face.
    final bundle = _Bundle({
      'assets/chance/dice-cube.json': utf8.encode(
        jsonEncode({
          'sheet': 'dice-cube',
          'image': 'dice-cube.webp',
          'cell': [128, 128],
          'columns': 6,
          'rows': 1,
          'size': [768, 128],
          'faces': {
            for (var n = 1; n <= 6; n++) 'd6-$n': [n - 1, 0],
          },
        }),
      ),
      'assets/chance/dice-cube.webp': _png,
    });
    await _pump(
      tester,
      bundle,
      const ChanceView(messageId: 'poly', chance: roll),
    );
    await _settle(tester);
    final die = tester.widget<DieFace>(find.byKey(const Key('art-0')));
    expect((die.sides, die.value), (20, '17'));
    expect(tester.getSize(find.byKey(const Key('art-0'))), const Size(64, 64));
    expect(
      tester.widget<ChanceSprite>(find.byKey(const Key('art-1'))).face,
      'd6-4',
    );
    final number = tester.widget<Text>(
      find.descendant(
        of: find.byKey(const Key('art-0')),
        matching: find.byKey(const Key('die-number')),
      ),
    );
    expect((number.data, number.style!.color), ('17', CardColours.black));
    // Arriving live, its reel turns.
    ChanceLive.arrived('poly-live', DateTime.now());
    await _pump(
      tester,
      bundle,
      const ChanceView(messageId: 'poly-live', chance: roll),
    );
    final state = tester.state<ChanceViewState>(find.byType(ChanceView));
    await tester.pump(const Duration(milliseconds: 130));
    expect(state.spinning, isTrue);
    expect(
      tester.widget<DieFace>(find.byKey(const Key('art-0'))).value,
      state.keys[0],
    );
    await tester.pump(const Duration(seconds: 2));
    expect(tester.widget<DieFace>(find.byKey(const Key('art-0'))).value, '17');
  });

  testWidgets('a reversed card lands upside down as it is turned face up', (
    tester,
  ) async {
    const reading = {
      'kind': 'tarot',
      'deck': 'tarot',
      'spread': 'three',
      'cards': ['major-0', 'cups-2', 'wands-ace'],
      'reversed': [true, false, true],
    };
    await _pump(
      tester,
      _withSheets(),
      const ChanceView(messageId: 'tarot', chance: reading),
    );
    await _settle(tester);
    bool turned(int i) =>
        tester.widget<ChanceSprite>(find.byKey(Key('art-$i'))).turned;
    bool rotated(int i) => find
        .descendant(
          of: find.byKey(Key('art-$i')),
          matching: find.byWidgetPredicate(
            (w) => w is RotatedBox && w.quarterTurns == 2,
          ),
        )
        .evaluate()
        .isNotEmpty;
    expect([turned(0), turned(1), turned(2)], [true, false, true]);
    expect(find.byKey(const Key('back-0')), findsNothing);
    expect([rotated(0), rotated(1), rotated(2)], [true, false, true]);
    // The labels still say which are reversed.
    expect(
      [
        for (var i = 0; i < 3; i++)
          tester
              .getSemantics(find.byKey(Key('card-$i')))
              .label
              .endsWith('reversed'),
      ],
      [true, false, true],
    );
    // Arriving live, the cards lie face down, showing the french card
    // back at the tarot card's size; the first turned over lands upside
    // down.
    ChanceLive.arrived('tarot-live', DateTime.now());
    await _pump(
      tester,
      _withSheets(),
      const ChanceView(messageId: 'tarot-live', chance: reading),
    );
    await _settle(tester);
    final state = tester.state<ChanceViewState>(find.byType(ChanceView));
    expect(state.spinning, isTrue);
    for (var i = 0; i < 3; i++) {
      final b = tester.widget<ChanceSprite>(find.byKey(Key('back-$i')));
      expect((b.sheet.name, b.face, b.turned), ('french', 'back', false));
      expect(tester.getSize(find.byKey(Key('back-$i'))), const Size(88, 123));
    }
    final plan = revealPlan('tarot', 3);
    await tester.pump(Duration(milliseconds: plan.says[0] + 40));
    expect(turned(0), isTrue);
    expect(find.byKey(const Key('back-1')), findsOneWidget);
    await tester.pump(const Duration(seconds: 3));
    expect(state.spinning, isFalse);
    expect([turned(0), turned(1), turned(2)], [true, false, true]);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a card from a deck without orientation is never turned', (
    tester,
  ) async {
    await _pump(
      tester,
      _withSheets(),
      const ChanceView(
        messageId: 'plain',
        chance: {
          'kind': 'draw',
          'deck': 'standard',
          'cards': ['hearts-queen'],
        },
      ),
    );
    await _settle(tester);
    expect(
      tester.widget<ChanceSprite>(find.byKey(const Key('art-0'))).turned,
      isFalse,
    );
  });
}
