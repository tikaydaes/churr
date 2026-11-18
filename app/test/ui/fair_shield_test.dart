// The shield (docs/DESIGN-FAIR.md section 13), as e2e/types.mjs holds the
// web chat's: drawn from this device's own check of the message's draw
// (testdata/fair/message.json), a shield with a tick when it checks out, a
// warning triangle when a check fails, a shield with a question mark when
// the drawing server's history is not known here, each its own shape and
// spoken label; a card's fair_mark cannot make it green; tapped, it opens
// Validate result with the draw filled in and checked.

import 'dart:convert';
import 'dart:io';

import 'package:churr/core/models/message.dart';
import 'package:churr/state/server_state.dart';
import 'package:churr/ui/fair/validate_screen.dart';
import 'package:churr/ui/theme/theme.dart';
import 'package:churr/ui/types/fair_mark.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../forms_support.dart';

void main() {
  final mv = jsonDecode(
    File('../testdata/fair/message.json').readAsStringSync(),
  ) as Map<String, dynamic>;

  Future<ServerState> server(WidgetTester tester) async {
    late ServerState s;
    await tester.runAsync(() async {
      s = await fakeServerState(FakeServer((_) => null));
    });
    s.vrfHistories.show(
      mv['server'] as String,
      (mv['keys'] as List).cast<Map<String, dynamic>>(),
    );
    return s;
  }

  // Each variant its own message: the check is kept per message and proof.
  Message message(String id, [void Function(Map<String, dynamic> m)? change]) {
    final m = jsonDecode(jsonEncode(mv['message'])) as Map<String, dynamic>;
    m['id'] = id;
    change?.call(m);
    return Message.fromJson(m);
  }

  Future<void> pump(WidgetTester tester, Widget w) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: buildTheme(dark: false),
        home: Scaffold(body: Center(child: w)),
      ),
    );
    await tester.pump();
  }

  String label(WidgetTester tester) =>
      tester.getSemantics(find.byType(InkWell)).label;

  testWidgets('its state is its own check: shape, word and spoken label', (
    tester,
  ) async {
    final s = await server(tester);
    await pump(tester, FairShield(server: s, message: message('good')));
    expect(find.byKey(const Key('fair-shield-verified')), findsOneWidget);
    expect(find.text('Verified'), findsOneWidget);
    expect(label(tester), startsWith('Verified. '));

    await pump(
      tester,
      FairShield(
        server: s,
        message: message('moved', (m) => (m['fair'] as Map)['t'] += 1),
      ),
    );
    expect(find.byKey(const Key('fair-shield-failed')), findsOneWidget);
    expect(label(tester), startsWith('Not verified. '));

    await pump(
      tester,
      FairShield(
        server: s,
        message: message(
          'far',
          (m) => (m['fair'] as Map)['owner'] = 'b.example',
        ),
      ),
    );
    expect(find.byKey(const Key('fair-shield-unknown')), findsOneWidget);
    expect(label(tester), startsWith('Cannot be checked here. '));
  });

  testWidgets('a card cannot make it green', (tester) async {
    final s = await server(tester);
    // Naming an outcome the draw has: green; one it has not: the warning.
    await pump(
      tester,
      FairShield(server: s, message: message('named'), objects: const [1]),
    );
    expect(find.byKey(const Key('fair-shield-verified')), findsOneWidget);
    await pump(
      tester,
      FairShield(server: s, message: message('stray'), objects: const [7]),
    );
    expect(find.byKey(const Key('fair-shield-failed')), findsOneWidget);
    // A bot's card on a message with no draw, whatever it says.
    final plain = Message.fromJson({
      'id': 'plain',
      'conversation_id': 'c1',
      'author_id': 'sam',
      'created_at': mv['message']['created_at'],
      'cards': [
        {
          'title': 'Verified fair',
          'fair_mark': {
            'objects': [0],
          },
        },
      ],
    });
    expect(plain.cards.single.fairMark, [0]);
    await pump(
      tester,
      FairShield(
        server: s,
        message: plain,
        objects: plain.cards.single.fairMark,
      ),
    );
    expect(find.byKey(const Key('fair-shield-failed')), findsOneWidget);
  });

  testWidgets('tapped, it opens Validate result with the draw, checked', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(900, 5000);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final s = await server(tester);
    await pump(tester, FairShield(server: s, message: message('tap')));
    await tester.tap(find.byKey(const Key('fair-shield-tap')));
    await tester.pumpAndSettle();
    expect(find.byType(ValidateScreen), findsOneWidget);
    expect(find.text(mv['message']['fair']['alpha'] as String), findsOneWidget);
    for (final id in [
      'proof',
      'beta',
      'outcome-0-0',
      'outcome-1-0',
      'alpha',
      'key',
    ]) {
      final v = find.byKey(Key('verdict-$id'));
      expect(tester.getSemantics(v).label, contains(': Pass.'), reason: id);
    }
  });
}
