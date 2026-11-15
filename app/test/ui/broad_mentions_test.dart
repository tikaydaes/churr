import 'package:churr/core/db/cache_db.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../conversation_support.dart';

/// The composer and broad mentions (ruling R64): the warning that
/// `@channel` will go as plain text, and "This will notify N people".

const _check = 'POST /api/v1/conversations/$general/broad-mentions/check';

/// The cache of the conversation screen's tests, with #General saying
/// whether its reader may use broad mentions.
Future<(CacheDb, FakeServer)> _cache({required bool allowed}) async {
  final db = await conversationCache();
  final c = channel(general, 'General', members: 5000);
  await db.upsertConversation(
    Conversation.fromJson({
      ...c.raw,
      'broad_mentions': 'server',
      'broad_mentions_allowed': allowed,
    }),
    syncedSeq: c.lastSeq,
  );
  return (db, FakeServer());
}

Future<int> _waiting(WidgetTester tester, CacheDb db) async =>
    (await tester.runAsync(() => db.outbox(conversationId: general)))!.length;

void main() {
  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets(
      'not allowed: the box says it will go as plain text, and sends ($label)',
      (tester) async {
        await setSize(tester, size);
        late CacheDb db;
        late FakeServer fake;
        await tester.runAsync(
          () async => (db, fake) = await _cache(allowed: false),
        );
        final app = await conversationApp(tester, db, fake);
        await pumpConversation(tester, app);
        final box = find.byKey(const Key('composer'));
        expect(find.byKey(const Key('broad-hint')), findsNothing);

        await tester.enterText(box, 'lunch @channel today?');
        await settle(tester, 2);
        expect(find.byKey(const Key('broad-hint')), findsOneWidget);
        expect(
          find.textContaining('sent as plain text and will notify nobody'),
          findsOneWidget,
        );
        // In code it is not a mention, and a name is not a broad one.
        await tester.enterText(box, 'type `@channel` to shout');
        await settle(tester, 2);
        expect(find.byKey(const Key('broad-hint')), findsNothing);
        await tester.enterText(box, 'hi @sam ');
        await settle(tester, 2);
        expect(find.byKey(const Key('broad-hint')), findsNothing);

        await tester.enterText(box, 'lunch @here today?');
        await settle(tester, 2);
        expect(find.byKey(const Key('broad-hint')), findsOneWidget);
        await tester.tap(find.byKey(const Key('send')));
        await settle(tester);
        // Sent as it is: the server is not asked, nobody is asked.
        expect(await _waiting(tester, db), 1);
        expect(fake.where('POST', 'broad-mentions/check'), isEmpty);
        expect(find.byKey(const Key('broad-confirm')), findsNothing);
        expect(find.byKey(const Key('broad-hint')), findsNothing);
        expect(tester.takeException(), isNull);
      },
    );

    testWidgets(
      'allowed and many: "This will notify N people" before sending ($label)',
      (tester) async {
        await setSize(tester, size);
        late CacheDb db;
        late FakeServer fake;
        await tester.runAsync(
          () async => (db, fake) = await _cache(allowed: true),
        );
        fake.on(
          _check,
          (_) => {
            'broad': true,
            'allowed': true,
            'recipients': 4200,
            'confirm': true,
          },
        );
        final app = await conversationApp(tester, db, fake);
        await pumpConversation(tester, app);
        final box = find.byKey(const Key('composer'));
        await tester.enterText(box, 'fire drill @channel now');
        await settle(tester, 2);
        expect(
          find.byKey(const Key('broad-hint')),
          findsNothing,
          reason: 'someone who may is not warned',
        );

        await tester.tap(find.byKey(const Key('send')));
        await settle(tester);
        expect(find.byKey(const Key('broad-confirm')), findsOneWidget);
        expect(find.text('This will notify 4,200 people.'), findsOneWidget);
        expect(fake.where('POST', 'broad-mentions/check').single.body, {
          'text': 'fire drill @channel now',
        });
        // Cancel: nothing is sent and the text is still there.
        await tester.tap(find.byKey(const Key('broad-confirm-cancel')));
        await settle(tester);
        expect(find.byKey(const Key('broad-confirm')), findsNothing);
        expect(await _waiting(tester, db), 0);
        expect(
          tester.widget<TextField>(box).controller!.text,
          'fire drill @channel now',
        );

        // Send: it goes.
        await tester.tap(find.byKey(const Key('send')));
        await settle(tester);
        await tester.tap(find.byKey(const Key('broad-confirm-send')));
        await settle(tester);
        final outbox = await tester.runAsync(
          () => db.outbox(conversationId: general),
        );
        expect(outbox!.single.text, 'fire drill @channel now');
        expect(tester.widget<TextField>(box).controller!.text, isEmpty);
        expect(tester.takeException(), isNull);
      },
    );
  }

  testWidgets('allowed and few: sent without a question', (tester) async {
    await setSize(tester, desktop);
    late CacheDb db;
    late FakeServer fake;
    await tester.runAsync(() async => (db, fake) = await _cache(allowed: true));
    fake.on(
      _check,
      (_) => {
        'broad': true,
        'allowed': true,
        'recipients': 12,
        'confirm': false,
      },
    );
    final app = await conversationApp(tester, db, fake);
    await pumpConversation(tester, app);
    await tester.enterText(find.byKey(const Key('composer')), 'hi @here');
    await settle(tester, 2);
    await tester.tap(find.byKey(const Key('send')));
    await settle(tester);
    expect(find.byKey(const Key('broad-confirm')), findsNothing);
    expect(fake.where('POST', 'broad-mentions/check'), hasLength(1));
    expect(await _waiting(tester, db), 1);

    // A message with no broad mention asks the server nothing.
    await tester.enterText(find.byKey(const Key('composer')), 'plain words');
    await settle(tester, 2);
    await tester.tap(find.byKey(const Key('send')));
    await settle(tester);
    expect(fake.where('POST', 'broad-mentions/check'), hasLength(1));
    expect(await _waiting(tester, db), 2);
    expect(tester.takeException(), isNull);
  });
}
