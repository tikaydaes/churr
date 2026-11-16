// Objects (ruling R73) in the app's composer: an exact token complete at
// the cursor is offered with "Plain text" after it; the arrows wrap;
// finishing the word takes the row chosen; Escape takes plain text;
// Backspace straight after accepting undoes it; what was chosen goes with
// the post as object hints. A phrase (class B) has a Plain text row too.

import 'package:churr/core/db/cache_db.dart';
import 'package:churr/core/instant/zone.dart';
import 'package:churr/ui/screens/conversation/pickers.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

import '../conversation_support.dart';

void main() {
  setUp(() {
    InstantEnv.deviceZone = 'Australia/Brisbane';
    InstantEnv.now = () => DateTime.utc(2026, 11, 18, 1);
  });
  tearDown(() {
    InstantEnv.deviceZone = null;
    InstantEnv.now = DateTime.now;
  });

  Future<CacheDb> open(WidgetTester tester, {Size size = desktop}) async {
    await setSize(tester, size);
    late CacheDb db;
    late FakeServer fake;
    await tester.runAsync(() async {
      db = await conversationCache();
      fake = FakeServer()
        ..on(
          'GET /api/v1/reference-patterns',
          (_) => {
            'patterns': [
              {
                'id': '0193a000-0000-7000-8000-000000000101',
                'name': 'Tracker',
                'prefix': 'PROJ-',
                'url_template': 'https://tracker.example/browse/PROJ-{value}',
                'created_at': '2026-11-16T00:00:00.000Z',
              },
            ],
          },
        );
    });
    final app = await conversationApp(tester, db, fake);
    await pumpConversation(tester, app, platform: TargetPlatform.macOS);
    return db;
  }

  Finder row(String kind) => find.byKey(Key('object-row-$kind'));

  /// The row the list has chosen.
  int chosen(WidgetTester tester) => tester
      .widget<SuggestionList>(find.byKey(const Key('object-offer')))
      .selected;

  Future<Map<String, dynamic>?> sent(WidgetTester tester, CacheDb db) async {
    await tester.tap(find.byKey(const Key('send')));
    await settle(tester);
    final outbox = await tester.runAsync(
      () => db.outbox(conversationId: general),
    );
    return outbox!.single.extra;
  }

  Future<void> type(WidgetTester tester, String text) async {
    await tester.enterText(find.byKey(const Key('composer')), text);
    await settle(tester, 2);
  }

  testWidgets('a reference is offered and taken by finishing the word', (
    tester,
  ) async {
    final db = await open(tester);
    await type(tester, 'Fixed PROJ-12');
    expect(find.byKey(const Key('object-offer')), findsOneWidget);
    expect(
      find.text('Reference PROJ-12, opens tracker.example'),
      findsOneWidget,
    );
    expect(find.text('Plain text: “PROJ-12”'), findsOneWidget);
    expect(find.text('WHAT IS “PROJ-12”?'), findsOneWidget);
    await tester.sendKeyEvent(LogicalKeyboardKey.space);
    await settle(tester, 2);
    expect(find.byKey(const Key('object-offer')), findsNothing);
    await type(tester, 'Fixed PROJ-12 today');
    expect(
      find.byKey(const Key('object-offer')),
      findsNothing,
      reason: 'decided once',
    );
    final extra = await sent(tester, db);
    expect(
      extra?.containsKey('object_hints') ?? false,
      isFalse,
      reason: 'the first reading is the server\'s own',
    );
  });

  testWidgets('a colour is offered though # starts a channel link', (
    tester,
  ) async {
    final db = await open(tester);
    await type(tester, 'Paint it #ff8800');
    expect(find.byKey(const Key('object-offer')), findsOneWidget);
    expect(find.text('Colour #ff8800'), findsOneWidget);
    await tester.sendKeyEvent(LogicalKeyboardKey.escape);
    await settle(tester, 2);
    final extra = await sent(tester, db);
    expect(extra!['object_hints'], [
      {'start': 9, 'end': 16, 'kind': 'plain'},
    ]);
  });

  testWidgets('Escape keeps a reference as plain text', (tester) async {
    final db = await open(tester);
    await type(tester, 'Not PROJ-13');
    await tester.sendKeyEvent(LogicalKeyboardKey.escape);
    await settle(tester, 2);
    expect(find.byKey(const Key('object-offer')), findsNothing);
    final extra = await sent(tester, db);
    expect(extra!['object_hints'], [
      {'start': 4, 'end': 11, 'kind': 'plain'},
    ]);
  });

  testWidgets('Backspace straight after accepting makes it plain text', (
    tester,
  ) async {
    final db = await open(tester);
    await type(tester, 'Undo PROJ-14');
    await tester.sendKeyEvent(LogicalKeyboardKey.tab);
    await settle(tester, 2);
    await tester.sendKeyEvent(LogicalKeyboardKey.backspace);
    await settle(tester, 2);
    final field = tester.widget<TextField>(find.byKey(const Key('composer')));
    expect(
      field.controller!.text,
      'Undo PROJ-14',
      reason: 'the undo deletes nothing',
    );
    final extra = await sent(tester, db);
    expect(extra!['object_hints'], [
      {'start': 5, 'end': 12, 'kind': 'plain'},
    ]);
  });

  testWidgets('the arrows wrap, and the row chosen is what goes', (
    tester,
  ) async {
    final db = await open(tester, size: phone);
    await type(tester, 'Wrap PROJ-15');
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowUp);
    await settle(tester, 2);
    expect(
      chosen(tester),
      1,
      reason: 'up from the first row wraps to the last',
    );
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
    await settle(tester, 2);
    expect(
      chosen(tester),
      0,
      reason: 'down from the last row wraps to the first',
    );
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
    await settle(tester, 2);
    await tester.sendKeyEvent(LogicalKeyboardKey.period);
    await settle(tester, 2);
    final extra = await sent(tester, db);
    expect(extra!['object_hints'], [
      {'start': 5, 'end': 12, 'kind': 'plain'},
    ]);
  });

  testWidgets('a typed time is offered the same way; a row can be tapped', (
    tester,
  ) async {
    final db = await open(tester);
    await type(tester, 'Party at 2026-12-24 18:00 Europe/Berlin');
    expect(find.byKey(const Key('object-offer')), findsOneWidget);
    expect(find.textContaining('Time: Fri, Dec 25, 3:00'), findsOneWidget);
    await tester.tap(row('plain'));
    await settle(tester, 2);
    final extra = await sent(tester, db);
    expect(extra!['object_hints'], [
      {'start': 9, 'end': 39, 'kind': 'plain'},
    ]);
  });

  testWidgets('a phrase has a Plain text row, reached with the arrows', (
    tester,
  ) async {
    final db = await open(tester);
    await type(tester, 'Lunch tomorrow at 1pm');
    expect(find.byKey(const Key('instant-offer-plain')), findsOneWidget);
    await tester.sendKeyEvent(LogicalKeyboardKey.space);
    await settle(tester, 2);
    expect(
      find.byKey(const Key('instant-offer')),
      findsOneWidget,
      reason: 'space does not take a phrase',
    );
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
    await tester.sendKeyEvent(LogicalKeyboardKey.enter);
    await settle(tester, 2);
    expect(find.byKey(const Key('instant-offer')), findsNothing);
    final extra = await sent(tester, db);
    expect(extra!.containsKey('instant_hints'), isFalse);
  });
}
