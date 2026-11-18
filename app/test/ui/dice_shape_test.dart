import 'dart:io';
import 'dart:math' as m;

import 'package:churr/core/commands/chance_live.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/core/types/dice_roll.dart';
import 'package:churr/l10n/app_localizations.dart';
import 'package:churr/ui/message/chance_view.dart';
import 'package:churr/ui/types/dice_roll/dice_roll_view.dart';
import 'package:churr/ui/types/draw.dart';
import 'package:churr/ui/types/dice_roll/dice_shape.dart';
import 'package:churr/ui/theme/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

/// Dice and coins drawn on the fly (docs/ART.md): a die of N sides is a
/// regular N-gon up to ngonMaxSides, a circle above; a d2 and a /flip are
/// a coin. A roll is a value of kind dice_roll, drawn through the catalogue
/// (docs/DESIGN-MODULES.md section 9.1): as a message's card, and as inline
/// dice.

Future<void> _pump(WidgetTester tester, Widget child) => tester.pumpWidget(
  MaterialApp(
    theme: buildTheme(dark: true),
    localizationsDelegates: L10n.localizationsDelegates,
    supportedLocales: L10n.supportedLocales,
    home: Scaffold(body: SingleChildScrollView(child: child)),
  ),
);

/// The data of a roll of [rolls] on dice of [sides].
Map<String, Object> _roll(int sides, List<int> rolls) => {
  'dice': '${rolls.length}d$sides',
  'rows': [
    {
      'dice': '${rolls.length}d$sides',
      'terms': [
        {
          'sign': 1,
          'count': rolls.length,
          'sides': sides,
          'rolls': rolls,
          'value': rolls.fold<int>(0, (a, b) => a + b),
        },
      ],
      'total': rolls.fold<int>(0, (a, b) => a + b),
    },
  ],
};

/// A message whose view is that roll, drawn as the catalogue draws it.
Widget _card(String id, int sides, List<int> rolls) {
  final m = Message.fromJson({
    'id': id,
    'conversation_id': 'c1',
    'text': 'Rolled',
    'created_at': '2026-11-18T00:00:00.000Z',
    'view': {'kind': 'dice_roll', 'level': 1, 'data': _roll(sides, rolls)},
  });
  return drawView(m).card!;
}

void main() {
  test('the breakpoint is the web chat\'s', () {
    final js = File(
      '../internal/web/static/shared/types/dice-roll/diceshape.js',
    ).readAsStringSync();
    expect(
      RegExp(r'NGON_MAX_SIDES = (\d+);').firstMatch(js)!.group(1),
      '$ngonMaxSides',
    );
    expect(ngonMaxSides, 30);
  });

  test('a die of 3 to 30 sides is a polygon of that many corners', () {
    for (var n = 3; n <= ngonMaxSides; n++) {
      final o = dieOutline(n);
      expect(o.circle, isFalse, reason: 'd$n');
      expect(o.points.length, n, reason: 'd$n');
      // One corner straight up, and all inside the box.
      expect(o.points.first.dx, closeTo(dieBox / 2, 0.001));
      for (final p in o.points) {
        expect(p.dy, greaterThanOrEqualTo(o.points.first.dy));
        expect(
          p.dx >= 1 && p.dx <= dieBox - 1 && p.dy >= 1 && p.dy <= dieBox - 1,
          isTrue,
          reason: 'd$n $p',
        );
      }
    }
    for (final n in [2, 31, 100, 1000]) {
      expect(dieOutline(n).circle, isTrue, reason: 'd$n');
    }
  });

  test('the number fits inside the shape, however many figures', () {
    for (final (n, v) in [
      (3, '1'),
      (4, '3'),
      (20, '1'),
      (20, '20'),
      (30, '27'),
      (100, '100'),
      (1000, '1000'),
    ]) {
      final o = dieOutline(n);
      final rho = o.inner - 3;
      final b = fitBox(v.length, rho, o.centre);
      final corner = m.sqrt(b.width * b.width + b.height * b.height) / 2;
      expect(corner, lessThanOrEqualTo(rho + 0.01), reason: 'd$n $v');
      expect(b.height / 0.72, greaterThan(6));
    }
    expect(
      fitBox(4, 20, Offset.zero).height,
      lessThan(fitBox(1, 20, Offset.zero).height),
    );
  });

  test('colours and coin faces', () {
    // One colour for every die and the coin: paper, with the ink edge.
    for (final n in [2, 4, 6, 8, 10, 12, 13, 20, 100, 1000]) {
      expect(dieFill(n), CardColours.paper, reason: 'd$n');
    }
    expect(coinFace('1'), 'heads');
    expect(coinFace('2'), 'tails');
    expect(coinFace('tails'), 'tails');
    expect(coinFace('heads'), 'heads');
    expect(coinReel(['1', '2'], '2').last, '2');
    final r = coinReel(['heads', 'tails'], 'heads');
    for (var i = 1; i < r.length; i++) {
      expect(r[i], isNot(r[i - 1]));
    }
  });

  testWidgets('a d20 shows a 20-cornered shape with its number', (
    tester,
  ) async {
    await _pump(tester, _card('d20', 20, [17]));
    expect(find.byType(DiceRollCard), findsOneWidget);
    final die = tester.widget<DieFace>(find.byKey(const Key('art-0')));
    expect((die.sides, die.value), (20, '17'));
    final paint = tester.widget<CustomPaint>(
      find.descendant(
        of: find.byKey(const Key('art-0')),
        matching: find.byType(CustomPaint),
      ),
    );
    expect((paint.painter! as DiePainter).sides, 20);
    expect(find.text('17'), findsOneWidget);
  });

  testWidgets('a d2 is a coin: heads for 1, tails for 2, spoken as such', (
    tester,
  ) async {
    await _pump(tester, _card('d2', 2, [1, 2]));
    final a = tester.widget<DieFace>(find.byKey(const Key('art-0')));
    final b = tester.widget<DieFace>(find.byKey(const Key('art-1')));
    expect((a.sides, a.value, b.value), (2, '1', '2'));
    expect(find.text('H'), findsOneWidget);
    expect(find.text('T'), findsOneWidget);
    final painter =
        tester
                .widget<CustomPaint>(
                  find.descendant(
                    of: find.byKey(const Key('art-1')),
                    matching: find.byType(CustomPaint),
                  ),
                )
                .painter!
            as DiePainter;
    expect(painter.coinFace, 'tails');
    expect(
      find.bySemanticsLabel(RegExp(r'heads \(1\), tails \(2\).*3')),
      findsOneWidget,
    );
  });

  testWidgets('inline dice draw the same shapes, the height of the line', (
    tester,
  ) async {
    const style = TextStyle(fontSize: 16);
    await _pump(
      tester,
      DiceRollChip(
        data: diceRollData(_roll(20, [17])),
        typed: '[[d20]]',
        style: style,
      ),
    );
    final die = tester.widget<DieFace>(
      find.descendant(
        of: find.byKey(const Key('inline-die-0')),
        matching: find.byType(DieFace),
      ),
    );
    expect((die.sides, die.value), (20, '17'));
    expect(
      tester.getSize(find.byKey(const Key('inline-die-0'))),
      const Size.square(16 * dieInLine),
    );
    await _pump(
      tester,
      DiceRollChip(
        key: const Key('coin'),
        data: diceRollData(_roll(2, [2])),
        typed: '[[d2]]',
        style: style,
      ),
    );
    final coin = tester.widget<DieFace>(
      find.descendant(
        of: find.byKey(const Key('inline-die-0')),
        matching: find.byType(DieFace),
      ),
    );
    expect((coin.sides, coin.value), (2, '2'));
  });

  testWidgets('a flip is the same coin, its words kept for screen readers', (
    tester,
  ) async {
    await _pump(
      tester,
      const ChanceView(
        messageId: 'flip',
        chance: {'kind': 'flip', 'outcome': 'tails'},
      ),
    );
    final c = tester.widget<DieFace>(find.byKey(const Key('art-0')));
    expect((c.sides, c.value), (2, 'tails'));
    expect(find.text('T'), findsOneWidget);
    expect(find.bySemanticsLabel(RegExp('Tails')), findsOneWidget);
    // Arriving live, the reel turns the coin over: the frames alternate.
    ChanceLive.arrived('flip-live', DateTime.now());
    await _pump(
      tester,
      const ChanceView(
        messageId: 'flip-live',
        chance: {'kind': 'flip', 'outcome': 'tails'},
      ),
    );
    final seen = <String>[];
    for (var i = 0; i < 8; i++) {
      await tester.pump(const Duration(milliseconds: 70));
      seen.add(tester.widget<DieFace>(find.byKey(const Key('art-0'))).value);
    }
    expect(seen.toSet(), {'heads', 'tails'});
    await tester.pump(const Duration(seconds: 2));
    expect(
      tester.widget<DieFace>(find.byKey(const Key('art-0'))).value,
      'tails',
    );
  });
}
