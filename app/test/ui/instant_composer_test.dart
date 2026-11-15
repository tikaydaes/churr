// Instants (ruling R69) in the composer: a typed time shows what it will
// mean and can go as plain text; a phrase is offered as a time, accepted
// with Enter or dismissed with Escape; the post carries the author's zone
// and the accepted phrases as hints.

import 'package:churr/core/db/cache_db.dart';
import 'package:churr/core/instant/zone.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

import '../conversation_support.dart';

Finder rich(String s) => find.textContaining(s, findRichText: true);

void main() {
  setUp(() {
    // The author is in Brisbane, at 11 in the morning of Wednesday 18
    // November 2026.
    InstantEnv.deviceZone = 'Australia/Brisbane';
    InstantEnv.now = () => DateTime.utc(2026, 11, 18, 1);
  });
  tearDown(() {
    InstantEnv.deviceZone = null;
    InstantEnv.now = DateTime.now;
  });

  Future<(CacheDb, FakeServer)> open(WidgetTester tester) async {
    await setSize(tester, desktop);
    late CacheDb db;
    late FakeServer fake;
    await tester.runAsync(() async {
      db = await conversationCache();
      fake = FakeServer();
    });
    final app = await conversationApp(tester, db, fake);
    await pumpConversation(tester, app, platform: TargetPlatform.macOS);
    return (db, fake);
  }

  String said(WidgetTester tester, Key key) => tester
      .widgetList<Text>(
        find.descendant(of: find.byKey(key), matching: find.byType(Text)),
      )
      .map((t) => t.data)
      .join(' ')
      .replaceAll(' ', ' ');

  testWidgets('a typed time says what it will mean, and can go as text', (
    tester,
  ) async {
    await open(tester);
    await tester.enterText(
      find.byKey(const Key('composer')),
      'Party at 2026-12-24 18:00 Europe/Berlin',
    );
    await settle(tester, 2);
    expect(
      said(tester, const Key('instant-chip-9')),
      contains(
        '2026-12-24 18:00 Europe/Berlin will show as Fri, Dec 25, 3:00 AM your time',
      ),
    );
    await tester.tap(find.text('Keep as text'));
    await settle(tester, 2);
    final field = tester.widget<TextField>(find.byKey(const Key('composer')));
    expect(field.controller!.text, r'Party at 2026\-12-24 18:00 Europe/Berlin');
    expect(find.byKey(const Key('instant-chip-9')), findsNothing);
  });

  testWidgets(
    'a phrase is offered, accepted with Enter, and sent as a hint with the zone',
    (tester) async {
      final (db, _) = await open(tester);
      await tester.enterText(
        find.byKey(const Key('composer')),
        'Lunch tomorrow at 1pm',
      );
      await settle(tester, 2);
      expect(find.byKey(const Key('instant-offer')), findsOneWidget);
      expect(rich('Make this a time: Tomorrow, 1:00'), findsOneWidget);
      await tester.sendKeyEvent(LogicalKeyboardKey.enter);
      await settle(tester, 2);
      expect(find.byKey(const Key('instant-offer')), findsNothing);
      expect(
        said(tester, const Key('instant-hint-6')),
        contains('“tomorrow at 1pm” will show as Tomorrow, 1:00'),
      );
      final field = tester.widget<TextField>(find.byKey(const Key('composer')));
      expect(
        field.controller!.text,
        'Lunch tomorrow at 1pm',
        reason: 'the words stay',
      );
      await tester.tap(find.byKey(const Key('send')));
      await settle(tester);
      final outbox = await tester.runAsync(
        () => db.outbox(conversationId: general),
      );
      final item = outbox!.single;
      expect(item.text, 'Lunch tomorrow at 1pm');
      expect(item.extra!['time_zone'], 'Australia/Brisbane');
      expect(item.extra!['instant_hints'], [
        {
          'start': 6,
          'end': 21,
          'time': '2026-11-19T03:00:00.000Z',
          'precision': 'minute',
          'zone': 'Australia/Brisbane',
        },
      ]);
    },
  );

  testWidgets(
    'a phrase dismissed with Escape, or edited, is not sent as a time',
    (tester) async {
      final (db, _) = await open(tester);
      await tester.enterText(
        find.byKey(const Key('composer')),
        'Maybe Friday at 5pm',
      );
      await settle(tester, 2);
      expect(find.byKey(const Key('instant-offer')), findsOneWidget);
      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await settle(tester, 2);
      expect(find.byKey(const Key('instant-offer')), findsNothing);
      // Accepted, then the words changed: the time goes with them.
      await tester.enterText(
        find.byKey(const Key('composer')),
        'Or Saturday at 6pm',
      );
      await settle(tester, 2);
      await tester.sendKeyEvent(LogicalKeyboardKey.tab);
      await settle(tester, 2);
      expect(find.byKey(const Key('instant-hint-3')), findsOneWidget);
      await tester.enterText(
        find.byKey(const Key('composer')),
        'Or Saturday at 7pm or so',
      );
      await settle(tester, 2);
      expect(find.byKey(const Key('instant-hint-3')), findsNothing);
      await tester.tap(find.byKey(const Key('send')));
      await settle(tester);
      final outbox = await tester.runAsync(
        () => db.outbox(conversationId: general),
      );
      expect(outbox!.single.extra!.containsKey('instant_hints'), isFalse);
    },
  );
}
