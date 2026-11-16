import 'dart:convert';
import 'dart:io';

import 'package:churr/core/commands/chance_live.dart';
import 'package:churr/l10n/app_localizations.dart';
import 'package:churr/ui/message/chance_view.dart';
import 'package:churr/ui/theme/theme.dart';
import 'package:churr/ui/theme/tokens.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

/// The chance results of rulings R67 and R68: the reels spin once for a
/// result that arrived live, land on the server's value in every slot, and
/// the last frame is the static drawing.
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

List<String> _faces(WidgetTester tester) => [
  for (final e in tester.widgetList<Text>(
    find.byWidgetPredicate(
      (w) =>
          w is Text &&
          w.key is ValueKey<String> &&
          (w.key! as ValueKey<String>).value.startsWith('face-'),
    ),
  ))
    e.data!,
];

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
  for (final c in _results()) {
    testWidgets(
      '${c['kind']} ${c['dice'] ?? c['deck'] ?? ''}: the reels land on the server\'s values',
      (tester) async {
        ChanceLive.arrived('m-${c.hashCode}', DateTime.now());
        final t = await _pump(
          tester,
          ChanceView(messageId: 'm-${c.hashCode}', chance: c),
        );
        final still = _want(t, c);
        await tester.pump(); // the first frame, then the spin starts
        await tester.pump(const Duration(milliseconds: 200));
        final state = tester.state<ChanceViewState>(find.byType(ChanceView));
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
        await tester.pump(const Duration(milliseconds: 1500));
        expect(state.spinning, isFalse);
        expect(_faces(tester), still);
        // It played once: drawing it again does not spin, and noting it
        // again (its event after the author's answer) changes nothing.
        ChanceLive.arrived('m-${c.hashCode}', DateTime.now());
        expect(ChanceLive.spinAt('m-${c.hashCode}'), isNull);
      },
    );
  }

  testWidgets(
    'from history it does not spin; pressed, it does; with reduced motion it just appears',
    (tester) async {
      final roll = _results().first;
      final t = await _pump(tester, ChanceView(messageId: 'old', chance: roll));
      await tester.pump(const Duration(milliseconds: 100));
      final state = tester.state<ChanceViewState>(find.byType(ChanceView));
      expect(state.spinning, isFalse);
      expect(_faces(tester), _want(t, roll));
      await tester.tap(find.byType(ChanceView));
      await tester.pump(const Duration(milliseconds: 100));
      expect(state.spinning, isTrue);
      await tester.pump(const Duration(seconds: 2));
      expect(_faces(tester), _want(t, roll));

      await _pump(
        tester,
        ChanceView(messageId: 'reduced', chance: roll),
        reduced: true,
      );
      final s2 = tester.state<ChanceViewState>(find.byType(ChanceView));
      await tester.tap(find.byType(ChanceView));
      await tester.pump(const Duration(milliseconds: 50));
      expect(s2.spinning, isFalse);
      expect(_faces(tester), _want(t, roll));
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
      final name = tester.widget<Text>(find.byKey(Key('name-$i')));
      final glyph = (name.textSpan! as TextSpan).children![1] as TextSpan;
      expect(glyph.style?.color, i < 2 ? c.pink : null, reason: cards[i]);
    }
    expect(isRedCard('joker-red'), isTrue);
    expect(isRedCard('joker-black'), isFalse);
    // Every frame of the reel has its own card's colour.
    final state = tester.state<ChanceViewState>(find.byType(ChanceView));
    state.spin();
    await tester.pump(const Duration(milliseconds: 130));
    for (var i = 0; i < cards.length; i++) {
      final red = RegExp('[♥♦]').hasMatch(state.faces[i]);
      expect(state.faceRed[i], red, reason: state.faces[i]);
    }
    await tester.pump(const Duration(seconds: 2));
  });
}
