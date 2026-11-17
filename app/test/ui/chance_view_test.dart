import 'dart:convert';
import 'dart:io';

import 'package:churr/core/commands/chance_live.dart';
import 'package:churr/l10n/app_localizations.dart';
import 'package:churr/ui/message/chance_view.dart';
import 'package:churr/ui/theme/theme.dart';
import 'package:churr/ui/theme/tokens.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
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
    for (final term in (c['terms'] as List).cast<Map>())
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
              (c['terms'] as List).any((x) => ((x as Map)['count'] ?? 0) > 0);
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
    };
    ChanceLive.arrived('total', DateTime.now());
    final t = await _pump(
      tester,
      const ChanceView(messageId: 'total', chance: roll),
    );
    // From the very first frame.
    expect(find.byKey(const Key('chance-total')), findsNothing);
    expect(find.text(t.chanceDropped.toUpperCase()), findsNothing);
    await tester.pump(const Duration(milliseconds: 900));
    expect(find.byKey(const Key('chance-total')), findsNothing);
    await tester.pump(const Duration(milliseconds: 600));
    expect(find.text('= 16'), findsOneWidget);
    expect(find.text(t.chanceDropped.toUpperCase()), findsOneWidget);
  });

  testWidgets(
    'from history it does not play; pressed, nothing happens; with reduced motion it just appears',
    (tester) async {
      final roll = _results().first;
      final t = await _pump(tester, ChanceView(messageId: 'old', chance: roll));
      await tester.pump(const Duration(milliseconds: 100));
      final state = tester.state<ChanceViewState>(find.byType(ChanceView));
      expect(state.spinning, isFalse);
      expect(find.byKey(const Key('chance-total')), findsOneWidget);
      expect(_faces(tester), _want(t, roll));
      await tester.tap(find.byType(ChanceView));
      await tester.pump(const Duration(milliseconds: 100));
      expect(state.spinning, isFalse);
      expect(find.byKey(const Key('chance-total')), findsOneWidget);

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
      expect(find.byKey(const Key('chance-total')), findsOneWidget);
      expect(find.byKey(const Key('back-0')), findsNothing);
      for (var i = 0; i < 3; i++) {
        expect(opacity(tester, Key('say-$i')), 1);
        expect(opacity(tester, Key('meaning-$i')), 1);
      }
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
      expect([state.down(0), state.down(1), state.down(2)], [false, false, false]);
      await tester.pumpAndSettle(const Duration(seconds: 2));
      handle.dispose();
    },
  );

  testWidgets(
    'a tarot reading rolls out: each card, then its words, then the next; the summary last',
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
      List<double> says() => [
        for (var i = 0; i < 3; i++) opacity(tester, Key('say-$i')),
      ];
      expect(says(), [0, 0, 0]);
      expect(opacity(tester, const Key('summary')), 0);
      await tester.pump(Duration(milliseconds: plan.says[0] + 30));
      expect((state.down(0), state.down(1)), (false, true));
      expect(says(), [1, 0, 0]);
      expect(opacity(tester, const Key('meaning-0')), 1);
      expect(opacity(tester, const Key('summary')), 0);
      await tester.pump(Duration(milliseconds: plan.says[1] - plan.says[0]));
      expect(says(), [1, 1, 0]);
      await tester.pump(Duration(milliseconds: plan.says[2] - plan.says[1]));
      expect(says(), [1, 1, 1]);
      expect(opacity(tester, const Key('summary')), 0);
      await tester.pump(Duration(milliseconds: plan.after - plan.says[2]));
      expect(opacity(tester, const Key('summary')), 1);
      await tester.pump(const Duration(seconds: 1));
      expect(state.spinning, isFalse);
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets(
    'a tarot reading: positions, cards, orientation and meanings, at phone width',
    (tester) async {
      final tarot = _results().firstWhere((c) => c['kind'] == 'tarot');
      await _pump(
        tester,
        ChanceView(messageId: 'tarot', chance: tarot),
        size: const Size(390, 1200),
      );
      expect(find.text('PAST'), findsOneWidget);
      expect(find.text('The Tower'), findsOneWidget);
      expect(find.text('Page of wands'), findsOneWidget);
      expect(find.textContaining('Lack of direction'), findsOneWidget);
      expect(find.textContaining('For fun'), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );

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
