import 'dart:convert';
import 'dart:io';

import 'package:churr/core/commands/chance_live.dart';
import 'package:churr/l10n/app_localizations.dart';
import 'package:churr/ui/message/chance_view.dart';
import 'package:churr/ui/theme/theme.dart';
import 'package:churr/ui/theme/tokens.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

/// The chance results of rulings R67, R68 and R87: a result that arrived
/// live plays once (reels for dice, cards turned over one after another)
/// and lands on the server's value in every slot; a roll's total waits for
/// the dice; no names are listed, each card's pops up; nothing replays on
/// a press; reduced motion shows it all at once; the last frame is the
/// static drawing.
List<Map<String, dynamic>> _results() {
  final v = jsonDecode(
    File('../testdata/commands/dice.json').readAsStringSync(),
  ) as Map;
  return [
    for (final f in (v['fallback'] as List).cast<Map>())
      (f['chance'] as Map).cast<String, dynamic>(),
  ];
}

Future<L10n> _pump(
  WidgetTester tester,
  Widget child, {
  bool reduced = false,
  Size size = const Size(800, 900),
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  late L10n t;
  await tester.pumpWidget(
    MaterialApp(
      theme: buildTheme(dark: true),
      localizationsDelegates: L10n.localizationsDelegates,
      supportedLocales: L10n.supportedLocales,
      home: MediaQuery(
        data: MediaQueryData(size: size, disableAnimations: reduced),
        child: Scaffold(
          body: Builder(
            builder: (context) {
              t = L10n.of(context);
              return SingleChildScrollView(child: child);
            },
          ),
        ),
      ),
    ),
  );
  return t;
}

/// What each slot shows: its text face, or, where its set's artwork is
/// bundled, the face its picture is drawn from (a die's number, a card's
/// short form).
List<String> _faces(WidgetTester tester) {
  final state = tester.state<ChanceViewState>(find.byType(ChanceView));
  final shown = <String>[];
  for (var i = 0; i < state.faces.length; i++) {
    final text = find.byKey(Key('face-$i'));
    final art = find.byKey(Key('art-$i'));
    expect(
      text.evaluate().isNotEmpty || art.evaluate().isNotEmpty,
      isTrue,
      reason: 'slot $i is drawn',
    );
    shown.add(
      text.evaluate().isNotEmpty
          ? tester.widget<Text>(text).data!
          : state.faces[i],
    );
  }
  return shown;
}

/// What each slot must land on, from the server's result.
List<String> _want(L10n t, Map<String, dynamic> c) => switch (c['kind']) {
  'roll' => [
    for (final row in (c['rows'] as List).cast<Map>())
      for (final term in (row['terms'] as List).cast<Map>())
        if ((term['count'] ?? 0) == 0)
          '${term['value']}'
        else
          for (final r in term['rolls'] as List) '$r',
  ],
  'flip' => [c['outcome'] == 'tails' ? t.chanceTails : t.chanceHeads],
  'pick' => [(c['options'] as List)[c['picked'] as int] as String],
  'shuffle' => [
    for (final i in c['order'] as List)
      (c['options'] as List)[i as int] as String,
  ],
  _ => [
    for (final x in c['cards'] as List)
      cardLabel(t, c['deck'] as String, x as String).short,
  ],
};

String _capped(String s) => s.isEmpty ? s : s[0].toUpperCase() + s.substring(1);

void main() {
  bool cards(Map c) => c['kind'] == 'draw' || c['kind'] == 'tarot';
  double opacity(WidgetTester tester, Key key) =>
      tester.widget<AnimatedOpacity>(find.byKey(key)).opacity;

  for (final c in _results()) {
    testWidgets(
      '${c['kind']} ${c['dice'] ?? c['deck'] ?? ''}: it lands on the server\'s values',
      (tester) async {
        final id = 'm-${c.hashCode}';
        ChanceLive.arrived(id, DateTime.now());
        final t = await _pump(tester, ChanceView(messageId: id, chance: c));
        final still = _want(t, c);
        final state = tester.state<ChanceViewState>(find.byType(ChanceView));
        await tester.pump(const Duration(milliseconds: 100));
        if (cards(c)) {
          // Face down, every one of them, at first.
          expect([
            for (var i = 0; i < still.length; i++) state.down(i),
          ], everyElement(isTrue));
          for (var i = 0; i < still.length; i++) {
            expect(find.byKey(Key('back-$i')), findsOneWidget);
          }
        } else {
          final anyMoving =
              c['kind'] != 'roll' ||
              (c['rows'] as List).any(
                (r) => ((r as Map)['terms'] as List).any(
                  (x) => ((x as Map)['count'] ?? 0) > 0,
                ),
              );
          if (anyMoving) expect(state.spinning, isTrue);
          // Every slot keeps its size while it spins.
          final sizes = [
            for (var i = 0; i < still.length; i++)
              tester.getSize(find.byKey(Key('slot-$i'))),
          ];
          await tester.pump(const Duration(milliseconds: 600));
          expect([
            for (var i = 0; i < still.length; i++)
              tester.getSize(find.byKey(Key('slot-$i'))),
          ], sizes);
        }
        await tester.pump(const Duration(seconds: 4));
        expect(state.spinning, isFalse);
        expect(_faces(tester), still);
        // It played once: drawing it again does not play, and noting it
        // again (its event after the author's answer) changes nothing.
        ChanceLive.arrived(id, DateTime.now());
        expect(ChanceLive.spinAt(id), isNull);
      },
    );
  }

  testWidgets('a roll\'s total and dropped dice wait for the dice to stop', (
    tester,
  ) async {
    const roll = {
      'kind': 'roll',
      'dice': '4d6kh3+2',
      'rows': [
        {
          'dice': '4d6kh3+2',
          'terms': [
            {
              'sign': 1,
              'count': 4,
              'sides': 6,
              'rolls': [5, 2, 6, 3],
              'dropped': [1],
              'value': 14,
            },
            {'sign': 1, 'value': 2},
          ],
          'total': 16,
        },
      ],
    };
    ChanceLive.arrived('total', DateTime.now());
    final handle = tester.ensureSemantics();
    final t = await _pump(
      tester,
      const ChanceView(messageId: 'total', chance: roll),
    );
    // From the very first frame.
    expect(find.byKey(const Key('chance-total-0')), findsNothing);
    expect(find.text(t.chanceDropped.toUpperCase()), findsNothing);
    await tester.pump(const Duration(milliseconds: 900));
    expect(find.byKey(const Key('chance-total-0')), findsNothing);
    await tester.pump(const Duration(milliseconds: 600));
    expect(find.text('= 16'), findsOneWidget);
    // The dropped die is struck through, with no word beside it; its
    // label says it.
    expect(find.text(t.chanceDropped.toUpperCase()), findsNothing);
    expect(find.text(t.chanceDropped), findsNothing);
    expect(
      tester.getSemantics(find.byType(ChanceView)).label,
      contains('2 (${t.chanceDropped})'),
    );
    handle.dispose();
  });

  testWidgets('a roll of two rows: a line each, each total once its row stops', (
    tester,
  ) async {
    final rows = _results().firstWhere(
      (c) => c['kind'] == 'roll' && (c['rows'] as List).length > 1,
    );
    ChanceLive.arrived('rows', DateTime.now());
    final handle = tester.ensureSemantics();
    final t = await _pump(tester, ChanceView(messageId: 'rows', chance: rows));
    expect(find.byKey(const Key('row-0')), findsOneWidget);
    expect(find.byKey(const Key('row-1')), findsOneWidget);
    expect(find.text('4d4+1d6'), findsOneWidget);
    expect(find.text('3d10kh2'), findsOneWidget);
    expect(find.byKey(const Key('chance-total-0')), findsNothing);
    expect(find.byKey(const Key('chance-total-1')), findsNothing);
    await tester.pump(const Duration(milliseconds: 1500));
    expect(find.text('= 15'), findsOneWidget);
    expect(find.text('= 16'), findsOneWidget);
    // Row 1's dice are in row 1's line, the dropped one struck through.
    final state = tester.state<ChanceViewState>(find.byType(ChanceView));
    expect(state.rowStopped(0) && state.rowStopped(1), isTrue);
    expect(
      tester.getSemantics(find.byType(ChanceView)).label,
      'Rolled 4d4+1d6, 3d10kh2: 4d4+1d6: 1, 4, 2, 3, 5; ${t.chanceTotalSpoken(15)}. '
      '3d10kh2: 7, 2 (${t.chanceDropped}), 9; ${t.chanceTotalSpoken(16)}',
    );
    handle.dispose();
  });

  testWidgets(
    'from history it does not play; pressed, nothing happens; with reduced motion it just appears',
    (tester) async {
      final roll = _results().first;
      final t = await _pump(tester, ChanceView(messageId: 'old', chance: roll));
      await tester.pump(const Duration(milliseconds: 100));
      final state = tester.state<ChanceViewState>(find.byType(ChanceView));
      expect(state.spinning, isFalse);
      expect(find.byKey(const Key('chance-total-0')), findsOneWidget);
      expect(_faces(tester), _want(t, roll));
      await tester.tap(find.byType(ChanceView));
      await tester.pump(const Duration(milliseconds: 100));
      expect(state.spinning, isFalse);
      expect(find.byKey(const Key('chance-total-0')), findsOneWidget);

      // Reduced motion: a live result, all of it at once.
      final tarot = _results().firstWhere((c) => c['kind'] == 'tarot');
      ChanceLive.arrived('reduced', DateTime.now());
      ChanceLive.arrived('reduced-roll', DateTime.now());
      await _pump(
        tester,
        Column(
          children: [
            ChanceView(messageId: 'reduced-roll', chance: roll),
            ChanceView(messageId: 'reduced', chance: tarot),
          ],
        ),
        reduced: true,
        size: const Size(800, 2000),
      );
      final states = tester
          .stateList<ChanceViewState>(find.byType(ChanceView))
          .toList();
      expect(states.map((s) => s.spinning), [false, false]);
      expect(find.byKey(const Key('chance-total-0')), findsOneWidget);
      expect(find.byKey(const Key('back-0')), findsNothing);
      expect(find.byKey(const Key('spread')), findsOneWidget);
      expect(states[1].justLanded, -1);
      expect(opacity(tester, const Key('summary')), 1);
    },
  );

  testWidgets(
    'a draw: face down, turned over in order, no names listed, each name on hover and tap',
    (tester) async {
      const draw = {
        'kind': 'draw',
        'deck': 'standard',
        'cards': ['spades-ace', 'hearts-queen', 'clubs-7'],
      };
      ChanceLive.arrived('draw', DateTime.now());
      final handle = tester.ensureSemantics();
      await _pump(tester, const ChanceView(messageId: 'draw', chance: draw));
      final state = tester.state<ChanceViewState>(find.byType(ChanceView));
      final plan = revealPlan('draw', 3);
      expect([state.down(0), state.down(1), state.down(2)], [true, true, true]);
      final order = <int>[];
      var was = [true, true, true];
      for (var ms = 0; ms < plan.total + 100; ms += 30) {
        await tester.pump(const Duration(milliseconds: 30));
        for (var i = 0; i < 3; i++) {
          if (was[i] && !state.down(i)) order.add(i);
        }
        was = [state.down(0), state.down(1), state.down(2)];
      }
      expect(order, [0, 1, 2]);
      expect(state.spinning, isFalse);
      // No names listed: the only words are the headline and the tag.
      expect(find.byKey(const Key('name-0')), findsNothing);
      expect(find.text('Queen of hearts'), findsNothing);
      // Each card carries its name for screen readers.
      expect(
        tester.getSemantics(find.byKey(const Key('card-1'))).label,
        'Queen of hearts',
      );
      // Hover (a mouse) shows the name.
      final mouse = await tester.createGesture(kind: PointerDeviceKind.mouse);
      await mouse.addPointer(location: Offset.zero);
      addTearDown(mouse.removePointer);
      await mouse.moveTo(tester.getCenter(find.byKey(const Key('card-1'))));
      await tester.pump(const Duration(seconds: 1));
      expect(find.text('Queen of hearts'), findsOneWidget);
      await mouse.moveTo(Offset.zero);
      await tester.pump(const Duration(seconds: 1));
      await tester.pumpAndSettle();
      expect(find.text('Queen of hearts'), findsNothing);
      // A tap shows it, and turns nothing over again.
      await tester.tap(find.byKey(const Key('card-2')));
      await tester.pump(const Duration(milliseconds: 100));
      expect(find.text('Seven of clubs'), findsOneWidget);
      expect(state.spinning, isFalse);
      expect(
        [state.down(0), state.down(1), state.down(2)],
        [false, false, false],
      );
      await tester.pumpAndSettle(const Duration(seconds: 2));
      handle.dispose();
    },
  );

  testWidgets(
    'a tarot reading rolls out in its spread: each card in order, marked as it lands; the summary last',
    (tester) async {
      final tarot = _results().firstWhere((c) => c['kind'] == 'tarot');
      final plan = revealPlan('tarot', 3);
      expect(plan.total, lessThan(3000));
      expect(revealPlan('tarot', 10).total, lessThan(4000));
      ChanceLive.arrived('reading', DateTime.now());
      await _pump(
        tester,
        ChanceView(messageId: 'reading', chance: tarot),
        size: const Size(800, 1600),
      );
      final state = tester.state<ChanceViewState>(find.byType(ChanceView));
      expect(state.justLanded, -1);
      expect(opacity(tester, const Key('summary')), 0);
      await tester.pump(Duration(milliseconds: plan.says[0] + 30));
      expect((state.down(0), state.down(1)), (false, true));
      expect(state.justLanded, 0);
      await tester.pump(Duration(milliseconds: plan.says[1] - plan.says[0]));
      expect(state.justLanded, 1);
      await tester.pump(Duration(milliseconds: plan.says[2] - plan.says[1]));
      expect(state.justLanded, 2);
      expect(opacity(tester, const Key('summary')), 0);
      await tester.pump(Duration(milliseconds: plan.after - plan.says[2]));
      expect(opacity(tester, const Key('summary')), 1);
      await tester.pump(const Duration(seconds: 1));
      expect(state.spinning, isFalse);
      expect(state.justLanded, -1);
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets(
    'a tarot reading: no words listed; a press opens a card\'s position, name, orientation and meaning',
    (tester) async {
      final tarot = _results().firstWhere((c) => c['kind'] == 'tarot');
      final handle = tester.ensureSemantics();
      final t = await _pump(
        tester,
        ChanceView(messageId: 'tarot', chance: tarot),
        size: const Size(390, 1200),
      );
      final slots = chanceSlots(t, tarot);
      expect(find.text('PAST'), findsNothing);
      expect(find.text(_capped(slots[0].caption)), findsNothing);
      expect(find.textContaining('For fun'), findsNothing);
      // Three in a row, left to right.
      final at = [
        for (var i = 0; i < 3; i++)
          tester.getTopLeft(find.byKey(Key('card-$i'))),
      ];
      expect(at.map((o) => o.dy).toSet().length, 1);
      expect(at[0].dx < at[1].dx && at[1].dx < at[2].dx, isTrue);
      final s0 = tester.state<ChanceViewState>(find.byType(ChanceView));
      final label = tester.getSemantics(find.byKey(const Key('card-0'))).label;
      expect(label, s0.tarotLabel(t, slots[0]));
      expect(label, startsWith('Past: '));
      Finder inDetail(int i, String text) => find.descendant(
        of: find.byKey(Key('detail-$i')),
        matching: find.text(text),
      );
      await tester.tap(find.byKey(const Key('card-0')));
      await tester.pump();
      expect(inDetail(0, 'PAST'), findsOneWidget);
      expect(inDetail(0, _capped(slots[0].caption)), findsOneWidget);
      // One open at a time.
      await tester.tap(find.byKey(const Key('card-2')));
      await tester.pump();
      expect(find.byKey(const Key('detail-0')), findsNothing);
      expect(inDetail(2, _capped(slots[2].caption)), findsOneWidget);
      // Escape closes it; an arrow moves on in reading order; Enter opens.
      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pump();
      expect(find.byKey(const Key('detail-2')), findsNothing);
      await tester.sendKeyEvent(LogicalKeyboardKey.arrowRight);
      await tester.sendKeyEvent(LogicalKeyboardKey.enter);
      await tester.pump();
      expect(find.byKey(const Key('detail-0')), findsOneWidget);
      // A press elsewhere closes it.
      await tester.tapAt(const Offset(380, 1100));
      await tester.pump();
      expect(find.byKey(const Key('detail-0')), findsNothing);
      expect(tester.takeException(), isNull);
      handle.dispose();
    },
  );

  for (final width in [390.0, 800.0]) {
    testWidgets(
      'a Celtic cross at ${width.toInt()} wide: the cross and the staff, fitting the message',
      (tester) async {
        final celtic = {
          'kind': 'tarot',
          'deck': 'tarot',
          'spread': 'celtic',
          'cards': [for (var i = 0; i < 10; i++) 'major-$i'],
          'reversed': [
            false,
            true,
            false,
            false,
            false,
            false,
            false,
            false,
            false,
            false,
          ],
        };
        await _pump(
          tester,
          ChanceView(messageId: 'celtic-$width', chance: celtic),
          size: Size(width, 1400),
        );
        Rect r(int i) => tester.getRect(find.byKey(Key('card-$i')));
        expect(r(0).center, r(1).center);
        expect(find.byKey(const Key('cross-1')), findsOneWidget);
        expect(find.byKey(const Key('cross-0')), findsNothing);
        expect(r(2).center.dx < r(0).left, isTrue);
        expect(r(3).center.dx > r(0).right, isTrue);
        expect(r(4).bottom <= r(0).top, isTrue);
        expect(r(5).top >= r(0).bottom, isTrue);
        for (final i in [6, 7, 8, 9]) {
          expect(r(i).left, r(6).left);
          expect(r(i).left > r(3).right, isTrue);
        }
        expect(
          r(6).top > r(7).top && r(7).top > r(8).top && r(8).top > r(9).top,
          isTrue,
        );
        for (var i = 0; i < 10; i++) {
          expect(r(i).right <= width, isTrue, reason: 'card $i fits');
        }
        expect(tester.takeException(), isNull);
      },
    );
  }

  testWidgets('hearts and diamonds are red; spades and clubs are ink', (
    tester,
  ) async {
    const cards = ['hearts-ace', 'diamonds-10', 'spades-king', 'clubs-2'];
    await _pump(
      tester,
      const ChanceView(
        messageId: 'm-red',
        chance: {'kind': 'draw', 'deck': 'standard', 'cards': cards},
      ),
    );
    final ctx = tester.element(find.byType(ChanceView));
    final c = ChurrColors.of(ctx);
    for (var i = 0; i < cards.length; i++) {
      final face = tester.widget<Text>(find.byKey(Key('face-$i')));
      expect(face.style!.color, i < 2 ? c.pink : c.ink, reason: cards[i]);
    }
    expect(isRedCard('joker-red'), isTrue);
    expect(isRedCard('joker-black'), isFalse);
  });
}
