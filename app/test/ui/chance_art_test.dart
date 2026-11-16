import 'dart:convert';
import 'dart:io';

import 'package:churr/l10n/app_localizations.dart';
import 'package:churr/ui/message/chance_art.dart';
import 'package:churr/ui/message/chance_view.dart';
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
const _poly = {
  'sheet': 'dice-poly',
  'image': 'dice-poly.webp',
  'cell': [128, 128],
  'columns': 7,
  'rows': 1,
  'size': [896, 128],
  'faces': {
    'd4': [0, 0],
    'd20': [4, 0],
    'dn': [6, 0],
  },
  'text': {
    'd20': {'x': 46, 'y': 56, 'w': 36, 'h': 22},
    'dn': {'x': 36, 'y': 50, 'w': 56, 'h': 30},
  },
  'sides': {'4': 'd4', '20': 'd20'},
  'other': 'dn',
  'text_colour': '#0d1026',
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

_Bundle _withSheets() => _Bundle({
  'assets/chance/french.json': utf8.encode(jsonEncode(_cards)),
  'assets/chance/french.webp': _png,
  'assets/chance/dice-poly.json': utf8.encode(jsonEncode(_poly)),
  'assets/chance/dice-poly.webp': _png,
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
    final d20 = slotArt('roll', null, sides: 20)!;
    expect((d20.sheet, d20.text), ('dice-poly', true));
    expect(slotArt('roll', null, sides: 20, still: true), isNull);
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
    final p = ChanceSheet.fromJson(_poly);
    expect(p.polyFace(20), 'd20');
    expect(p.polyFace(7), 'dn');
    expect(p.text['d20'], const Rect.fromLTWH(46, 56, 36, 22));
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
    // The names stay as text.
    expect(find.textContaining('Queen of hearts'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a die shape carries its number, and the reel changes it', (
    tester,
  ) async {
    const roll = {
      'kind': 'roll',
      'dice': '1d20',
      'terms': [
        {
          'sign': 1,
          'count': 1,
          'sides': 20,
          'rolls': [17],
          'value': 17,
        },
      ],
      'total': 17,
    };
    await _pump(
      tester,
      _withSheets(),
      const ChanceView(messageId: 'poly', chance: roll),
    );
    await _settle(tester);
    final sprite = tester.widget<ChanceSprite>(find.byKey(const Key('art-0')));
    expect((sprite.face, sprite.text), ('d20', '17'));
    final number = tester.widget<Text>(
      find.descendant(
        of: find.byKey(const Key('art-0')),
        matching: find.text('17'),
      ),
    );
    expect(number.style!.color, cardInk);
    final box = tester.widget<Positioned>(
      find.ancestor(of: find.text('17'), matching: find.byType(Positioned)),
    );
    expect((box.left, box.top, box.width, box.height), (23, 28, 18, 11));
    final state = tester.state<ChanceViewState>(find.byType(ChanceView));
    state.spin();
    await tester.pump(const Duration(milliseconds: 130));
    expect(state.spinning, isTrue);
    final frame = state.faces[0];
    expect(
      tester.widget<ChanceSprite>(find.byKey(const Key('art-0'))).text,
      frame,
    );
    await tester.pump(const Duration(seconds: 2));
    expect(
      tester.widget<ChanceSprite>(find.byKey(const Key('art-0'))).text,
      '17',
    );
  });
}
