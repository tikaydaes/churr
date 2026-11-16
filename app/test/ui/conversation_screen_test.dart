import 'package:churr/core/models/message.dart';
import 'package:churr/core/db/cache_db.dart';
import 'package:churr/ui/theme/shapes.dart';
import 'package:churr/ui/widgets/controls.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

import '../conversation_support.dart';

Finder rich(String s) => find.textContaining(s, findRichText: true);

/// Taps something in a menu or sheet that may need scrolling to first.
Future<void> tapVisible(WidgetTester tester, Finder f) async {
  await tester.ensureVisible(f);
  await tester.pump(const Duration(milliseconds: 300));
  await tester.tap(f);
}

Future<(CacheDb, FakeServer)> setUpCache({String? threadView}) async {
  late CacheDb db;
  final fake = FakeServer();
  db = await conversationCache(threadView: threadView);
  await db.upsertMessages([msg('mine', 25, 'My own words', author: 'me')]);
  // Following the thread, read up to before its replies: the marker
  // counts both (in "neither" it would follow the conversation's read
  // position, which moves as soon as the screen is at its newest).
  await db.upsertThreadStates([
    ThreadState(
      parentId: 'm1',
      conversationId: general,
      state: 'following',
      readSeq: 20,
      unreadCount: 2,
    ),
  ]);
  return (db, fake);
}

void main() {
  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets(
      'threaded view: replies hidden, one marker, the thread opens ($label)',
      (tester) async {
        await setSize(tester, size);
        late CacheDb db;
        late FakeServer fake;
        await tester.runAsync(() async => (db, fake) = await setUpCache());
        fake.on(
          'GET /api/v1/messages/m1/thread',
          (_) => {
            'parent': msg(
              'm1',
              10,
              'Deploy plan for Friday',
              thread: {'reply_count': 2, 'last_reply_seq': 24},
            ).raw,
            'messages': [
              msg('r1', 22, 'I can do Friday', author: 'lee', parent: 'm1').raw,
              msg(
                'r2',
                24,
                'Me too',
                author: 'lee',
                parent: 'm1',
                quote: {'message_id': 'r1', 'start': 0, 'end': 5},
              ).raw,
            ],
            'has_more': false,
          },
        );
        final app = await conversationApp(tester, db, fake);
        await pumpConversation(tester, app);

        expect(find.text('Deploy plan for Friday'), findsOneWidget);
        expect(
          find.text('I can do Friday'),
          findsNothing,
          reason: 'replies are not in the body',
        );
        expect(
          rich('2 new replies to: Deploy plan for Friday'),
          findsOneWidget,
        );
        expect(
          rich('2 replies'),
          findsOneWidget,
          reason: 'the summary line under the parent',
        );

        await tester.tap(find.byKey(const Key('marker-m1')));
        await settle(tester);
        expect(find.byKey(const Key('thread-pane')), findsOneWidget);
        expect(find.text('I can do Friday'), findsWidgets);
        // The quote of the second reply is drawn from the first's text.
        expect(
          find.descendant(
            of: find.byKey(const Key('quote')),
            matching: find.text('I can'),
          ),
          findsOneWidget,
        );
        final paneWidth = tester
            .getSize(find.byKey(const Key('thread-pane')))
            .width;
        if (size == desktop) {
          expect(paneWidth, 480, reason: 'beside the conversation');
          expect(find.byKey(const Key('message-list')), findsOneWidget);
        } else {
          expect(paneWidth, size.width, reason: 'over the conversation');
        }
        // Reading to the end of the thread moves the thread's position.
        expect(fake.where('PUT', '/thread/read'), isNotEmpty);
        await tester.tap(find.byKey(const Key('close-thread')));
        await settle(tester);
        expect(find.byKey(const Key('thread-pane')), findsNothing);
      },
    );

    testWidgets('flat view: replies in order with their headers ($label)', (
      tester,
    ) async {
      await setSize(tester, size);
      late CacheDb db;
      late FakeServer fake;
      await tester.runAsync(
        () async => (db, fake) = await setUpCache(threadView: 'flat'),
      );
      final app = await conversationApp(tester, db, fake);
      await pumpConversation(tester, app);
      expect(find.text('I can do Friday'), findsOneWidget);
      expect(
        rich('new replies'),
        findsNothing,
        reason: 'no markers in the flat view',
      );
      expect(find.byKey(const Key('reply-header')), findsNWidgets(2));
      expect(
        rich('in the thread of: Deploy plan for Friday'),
        findsOneWidget,
        reason: 'the second reply quotes the first, so the top message is linked too',
      );
      expect(find.text('Open thread'), findsNWidgets(2));
    });

    testWidgets(
      'the channel picker links by id and shows the detail line ($label)',
      (tester) async {
        await setSize(tester, size);
        late CacheDb db;
        late FakeServer fake;
        await tester.runAsync(() async => (db, fake) = await setUpCache());
        final app = await conversationApp(tester, db, fake);
        await pumpConversation(tester, app);
        await tester.enterText(find.byKey(const Key('composer')), 'see #des');
        await settle(tester, 2);
        expect(find.text('LINK A CHANNEL?'), findsOneWidget);
        expect(find.byKey(const Key('channel-option-$design')), findsOneWidget);
        expect(
          find.byKey(const Key('channel-option-$design2')),
          findsOneWidget,
        );
        expect(rich("14 members, you're in it"), findsOneWidget);
        expect(
          rich('General ›'),
          findsOneWidget,
          reason: 'the sub-channel is told apart by its parent',
        );
        await tester.tap(find.byKey(const Key('channel-option-$design2')));
        await settle(tester, 2);
        expect(find.byKey(const Key('suggestions')), findsNothing);
        final field = tester.widget<TextField>(
          find.byKey(const Key('composer')),
        );
        expect(
          field.controller!.text.length,
          'see '.length + 2,
          reason: 'the chip is one character, then a space',
        );
        await tester.tap(find.byKey(const Key('send')));
        await settle(tester);
        final outbox = await tester.runAsync(
          () => db.outbox(conversationId: general),
        );
        expect(outbox!.single.text, 'see <#$design2>');
      },
    );

    testWidgets('dismissing the picker leaves the text as typed ($label)', (
      tester,
    ) async {
      await setSize(tester, size);
      late CacheDb db;
      late FakeServer fake;
      await tester.runAsync(() async => (db, fake) = await setUpCache());
      final app = await conversationApp(tester, db, fake);
      await pumpConversation(tester, app);
      await tester.enterText(find.byKey(const Key('composer')), '#games');
      await settle(tester, 2);
      expect(
        find.byKey(const Key('suggestions')),
        findsNothing,
        reason: 'nothing matches, so no picker',
      );
      await tester.enterText(find.byKey(const Key('composer')), '#Des');
      await settle(tester, 2);
      expect(find.byKey(const Key('suggestions')), findsOneWidget);
      await tester.tap(find.byTooltip('Dismiss'));
      await settle(tester, 2);
      expect(find.byKey(const Key('suggestions')), findsNothing);
      final field = tester.widget<TextField>(find.byKey(const Key('composer')));
      expect(field.controller!.text, '#Des');
    });

    testWidgets('mentions list people with what picking them means ($label)', (
      tester,
    ) async {
      await setSize(tester, size);
      late CacheDb db;
      late FakeServer fake;
      await tester.runAsync(() async => (db, fake) = await setUpCache());
      final app = await conversationApp(tester, db, fake);
      await pumpConversation(tester, app);
      await tester.enterText(find.byKey(const Key('composer')), 'hi @le');
      await settle(tester, 2);
      final inPicker = find.descendant(
        of: find.byKey(const Key('suggestions')),
        matching: find.text('Lee Park'),
      );
      expect(inPicker, findsOneWidget);
      expect(find.text('In do not disturb.'), findsOneWidget);
      await tester.tap(inPicker);
      await settle(tester, 2);
      expect(
        tester
            .widget<TextField>(find.byKey(const Key('composer')))
            .controller!
            .text,
        'hi @lee ',
      );
    });
  }

  testWidgets('long-press on a phone opens the actions; delete asks first', (
    tester,
  ) async {
    await setSize(tester, phone);
    late CacheDb db;
    late FakeServer fake;
    await tester.runAsync(() async => (db, fake) = await setUpCache());
    final mine = (await tester.runAsync(() => db.message('mine')))!;
    fake.on(
      'DELETE /api/v1/messages/mine',
      (_) => {
        'message': {
          ...mine.raw,
          'text': '',
          'deleted_at': '2026-11-13T10:00:00.000Z',
          'deleted_by_id': 'me',
        },
      },
    );
    final app = await conversationApp(tester, db, fake);
    await pumpConversation(tester, app, platform: TargetPlatform.iOS);
    await tester.longPress(find.text('My own words'));
    await settle(tester, 3);
    expect(find.byKey(const Key('message-actions')), findsOneWidget);
    expect(find.byKey(const Key('action-edit')), findsOneWidget);
    expect(
      find.byKey(const Key('action-report')),
      findsNothing,
      reason: 'not on your own message',
    );
    await tester.ensureVisible(find.byKey(const Key('action-delete')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('action-delete')));
    await settle(tester, 3);
    expect(rich('kept as history until it is purged'), findsOneWidget);
    await tester.tap(find.byKey(const Key('confirm')));
    await settle(tester);
    expect(fake.where('DELETE', '/messages/mine'), hasLength(1));
    expect(rich('Deleted by its author'), findsOneWidget);
  });

  testWidgets(
    'desktop: the actions menu edits in place and reports with a reason',
    (tester) async {
      await setSize(tester, desktop);
      late CacheDb db;
      late FakeServer fake;
      await tester.runAsync(() async => (db, fake) = await setUpCache());
      final app = await conversationApp(tester, db, fake);
      await pumpConversation(tester, app, platform: TargetPlatform.macOS);
      final gesture = await tester.createGesture(kind: PointerDeviceKind.mouse);
      await gesture.addPointer(location: Offset.zero);
      await gesture.moveTo(tester.getCenter(find.text('My own words')));
      await settle(tester, 2);
      expect(find.byKey(const Key('hover-actions')), findsOneWidget);
      await tester.tap(find.byKey(const Key('more-actions')));
      await settle(tester, 3);
      await tapVisible(tester, find.byKey(const Key('action-edit')));
      await settle(tester, 2);
      expect(
        tester
            .widget<TextField>(find.byKey(const Key('composer')))
            .controller!
            .text,
        'My own words',
      );
      expect(rich('Editing a message'), findsOneWidget);
      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await settle(tester, 2);
      await gesture.removePointer();

      // Someone else's message: report asks why and says who is told.
      await tester.tap(find.text('Lunch?'), buttons: kSecondaryButton);
      await settle(tester, 3);
      await tapVisible(tester, find.byKey(const Key('action-report')));
      await settle(tester, 3);
      expect(rich("this channel's managers and to the admins"), findsOneWidget);
      await tester.enterText(find.byKey(const Key('report-reason')), 'spam');
      await tester.tap(find.byKey(const Key('confirm')));
      await settle(tester);
      expect(fake.where('POST', '/messages/m2/report').single.body, {
        'reason': 'spam',
      });
    },
  );

  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets(
      'shared conversation: unsent is dashed, late is tagged, "New" skips the late one ($label)',
      (tester) async {
        await setSize(tester, size);
        late CacheDb db;
        late FakeServer fake;
        await tester.runAsync(() async {
          (db, fake) = await setUpCache();
          await db.upsertMessages([
            msg(
              'mine',
              25,
              'My own words',
              author: 'me',
              extra: {'unsent': true},
            ),
            // Read to 20 at open. Arrived last, ordered first.
            msg(
              'late',
              26,
              'Sent while we were cut off',
              author: 'lee',
              extra: {'order': 'o000005', 'late': true},
            ),
          ]);
        });
        final app = await conversationApp(tester, db, fake);
        await pumpConversation(tester, app, platform: TargetPlatform.macOS);

        // Unsent: drawn dashed, and said to a screen reader.
        final unsent = find.byKey(const Key('unsent'));
        expect(unsent, findsOneWidget);
        expect(
          find.descendant(of: unsent, matching: find.byType(DashedBox)),
          findsOneWidget,
        );
        expect(
          find.descendant(of: unsent, matching: find.text('My own words')),
          findsOneWidget,
        );
        expect(
          find.bySemanticsLabel('Not yet passed on to the other servers'),
          findsOneWidget,
        );
        expect(
          find.byType(DashedBox),
          findsOneWidget,
          reason: 'nothing settled is dashed',
        );

        // Late: in its place at the top, with its tag.
        expect(find.byKey(const Key('late-tag')), findsOneWidget);
        expect(find.text('ARRIVED LATE'), findsOneWidget);
        final lateY = tester
            .getTopLeft(find.text('Sent while we were cut off'))
            .dy;
        final firstY = tester
            .getTopLeft(find.text('Deploy plan for Friday'))
            .dy;
        expect(lateY, lessThan(firstY), reason: 'sorted by order, not seq');
        expect(
          tester.getTopLeft(find.byKey(const Key('late-tag'))).dy,
          lessThan(lateY),
        );

        // "New" goes before the first unread that is not late (Lunch?,
        // seq 21), not before the late message.
        final newY = tester.getTopLeft(find.byType(NewMarker)).dy;
        expect(find.byType(NewMarker), findsOneWidget);
        expect(newY, greaterThan(firstY));
        expect(newY, lessThan(tester.getTopLeft(find.text('Lunch?')).dy));

        // An unsent message is a message like any other: it has its
        // actions.
        await tester.tap(find.text('My own words'), buttons: kSecondaryButton);
        await settle(tester, 3);
        expect(find.byKey(const Key('action-edit')), findsOneWidget);
        expect(find.byKey(const Key('action-delete')), findsOneWidget);
      },
    );
  }

  testWidgets('a settled message has neither the dashed edge nor the tag', (
    tester,
  ) async {
    await setSize(tester, desktop);
    late CacheDb db;
    late FakeServer fake;
    await tester.runAsync(() async => (db, fake) = await setUpCache());
    final app = await conversationApp(tester, db, fake);
    await pumpConversation(tester, app, platform: TargetPlatform.macOS);
    expect(find.text('My own words'), findsOneWidget);
    expect(find.byKey(const Key('unsent')), findsNothing);
    expect(find.byKey(const Key('late-tag')), findsNothing);
    expect(find.byType(DashedBox), findsNothing);
    final newY = tester.getTopLeft(find.byType(NewMarker)).dy;
    expect(newY, lessThan(tester.getTopLeft(find.text('Lunch?')).dy));
    expect(
      newY,
      greaterThan(tester.getTopLeft(find.text('Deploy plan for Friday')).dy),
    );
  });

  testWidgets('purge wording says what happens in a public channel', (
    tester,
  ) async {
    await setSize(tester, desktop);
    late CacheDb db;
    late FakeServer fake;
    await tester.runAsync(() async {
      (db, fake) = await setUpCache();
      await db.upsertMessages([
        msg(
          'edited',
          26,
          'Edited once',
          author: 'me',
          extra: {'has_history': true, 'edited_at': '2026-11-13T09:30:00.000Z'},
        ),
      ]);
    });
    final app = await conversationApp(tester, db, fake);
    await pumpConversation(tester, app, platform: TargetPlatform.macOS);
    expect(find.text('edited'), findsWidgets);
    await tester.tap(find.text('Edited once'), buttons: kSecondaryButton);
    await settle(tester, 3);
    expect(find.byKey(const Key('action-history')), findsOneWidget);
    await tapVisible(tester, find.byKey(const Key('action-purge')));
    await settle(tester, 3);
    expect(
      rich('hidden from everyone except admins as soon as you ask'),
      findsOneWidget,
    );
    expect(rich('You are not told which'), findsOneWidget);
    await tester.tap(find.text('Cancel'));
    await settle(tester, 2);
  });

  testWidgets('the thread view can be set for this conversation', (
    tester,
  ) async {
    await setSize(tester, desktop);
    late CacheDb db;
    late FakeServer fake;
    await tester.runAsync(() async => (db, fake) = await setUpCache());
    fake.on(
      'PATCH /api/v1/conversations/$general/membership',
      (_) => {
        'membership': {
          'level': 'member',
          'read_seq': 20,
          'mode': 'mentions',
          'thread_view': 'flat',
        },
      },
    );
    final app = await conversationApp(tester, db, fake);
    await pumpConversation(tester, app, platform: TargetPlatform.macOS);
    await tester.tap(find.byKey(const Key('thread-view')));
    await settle(tester, 3);
    await tester.tap(find.text('Flat here'), warnIfMissed: false);
    await settle(tester);
    expect(fake.where('PATCH', '/membership').single.body, {
      'thread_view': 'flat',
    });
    expect(
      find.text('I can do Friday'),
      findsOneWidget,
      reason: 'the body redraws flat',
    );
  });

  testWidgets(
    'files: an image opens full size; other servers\' files say how they are',
    (tester) async {
      await setSize(tester, desktop);
      late CacheDb db;
      late FakeServer fake;
      await tester.runAsync(() async {
        (db, fake) = await setUpCache();
        await db.upsertMessages([
          Message.fromJson({
            'id': 'files',
            'conversation_id': general,
            'seq': 27,
            'order': 'o000027',
            'author_id': 'sam',
            'text': '',
            'created_at': '2026-11-13T09:40:00.000Z',
            'files': [
              {
                'id': 'i1',
                'name': 'plan.png',
                'size': 5,
                'media_type': 'image/png',
                'kind': 'image',
                'width': 10,
                'height': 10,
                'url': '/api/v1/files/i1/plan.png',
                'thumb_url': '/api/v1/files/i1/thumb',
              },
              {
                'id': 'a1',
                'name': 'far.zip',
                'size': 5,
                'media_type': 'application/zip',
                'kind': 'file',
                'state': 'arriving',
              },
              {
                'id': 'u1',
                'name': 'gone.mp4',
                'size': 5,
                'media_type': 'video/mp4',
                'kind': 'video',
                'state': 'unavailable',
              },
              {
                'id': 't1',
                'name': 'huge.bin',
                'size': 5,
                'media_type': 'application/octet-stream',
                'kind': 'file',
                'state': 'too_large',
              },
              {
                'id': 'n1',
                'name': 'notes.py',
                'size': 20,
                'media_type': 'text/plain',
                'kind': 'text',
                'preview_text': 'print("hi")',
                'url': '/api/v1/files/n1/notes.py',
              },
              {
                'id': 'v1',
                'name': 'clip.mp4',
                'size': 20,
                'media_type': 'video/mp4',
                'kind': 'video',
                'width': 16,
                'height': 9,
                'url': '/api/v1/files/v1/clip.mp4',
              },
            ],
          }),
        ]);
      });
      final app = await conversationApp(tester, db, fake);
      await pumpConversation(tester, app, platform: TargetPlatform.macOS);
      expect(find.text('Arriving…'), findsOneWidget);
      expect(find.text('Unavailable'), findsOneWidget);
      expect(find.text('Too large for this server'), findsOneWidget);
      expect(rich('print'), findsOneWidget);
      expect(find.text('Show all'), findsOneWidget);
      expect(
        find.byKey(const Key('play-v1')),
        findsOneWidget,
        reason: 'a video without a poster has a plain placeholder',
      );
      await tester.tap(find.byKey(const Key('image-i1')));
      await settle(tester, 3);
      expect(find.byTooltip('Download'), findsOneWidget);
      await tester.tap(find.byTooltip('Close'));
      await settle(tester, 2);
    },
  );

  testWidgets('send later opens the schedule sheet', (tester) async {
    await setSize(tester, desktop);
    late CacheDb db;
    late FakeServer fake;
    await tester.runAsync(() async => (db, fake) = await setUpCache());
    final app = await conversationApp(tester, db, fake);
    await pumpConversation(tester, app, platform: TargetPlatform.macOS);
    await tester.enterText(find.byKey(const Key('composer')), 'later');
    await tester.tap(find.byKey(const Key('send-menu')));
    await settle(tester, 3);
    await tapVisible(tester, find.text('Send later…'));
    await settle(tester, 3);
    expect(find.byKey(const Key('schedule-sheet')), findsOneWidget);
    await tapVisible(tester, find.text('Tomorrow, 09:00'));
    await settle(tester);
    expect(fake.where('POST', '/api/v1/scheduled'), hasLength(1));
  });

  testWidgets(
    'a public channel read without joining has one bar in place of the composer',
    (tester) async {
      await setSize(tester, phone);
      late CacheDb db;
      late FakeServer fake;
      await tester.runAsync(() async {
        (db, fake) = await setUpCache();
        await db.upsertConversation(
          channel(design, 'Design', joined: false),
          syncedSeq: 30,
        );
      });
      final app = await conversationApp(tester, db, fake);
      await pumpConversation(tester, app, id: design);
      expect(find.text('Join to take part.'), findsOneWidget);
      expect(find.byKey(const Key('composer')), findsNothing);
    },
  );

  testWidgets(
    'the empty composer shows the hint and the system caret, nothing else',
    (tester) async {
      await setSize(tester, desktop);
      late CacheDb db;
      late FakeServer fake;
      await tester.runAsync(() async => (db, fake) = await setUpCache());
      final app = await conversationApp(tester, db, fake);
      await pumpConversation(tester, app, platform: TargetPlatform.macOS);
      final box = find.byKey(const Key('composer'));
      final field = tester.widget<TextField>(box);
      expect(field.controller!.text, isEmpty);
      // The caret is the platform's own, empty or not.
      expect(field.showCursor, isNull);
      // The hint is the words alone: no block beside them, and nothing
      // in the box keeps redrawing while it waits.
      expect(field.decoration!.hint, isA<Text>());
      await tester.pump(const Duration(seconds: 1));
      expect(
        find.descendant(of: box, matching: find.byType(Container)),
        findsNothing,
      );
      expect(tester.takeException(), isNull);
    },
  );
}
