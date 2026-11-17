import 'package:churr/core/models/message.dart';
import 'package:churr/core/notify/content.dart';
import 'package:churr/core/notify/tray_state.dart';
import 'package:churr/state/app_state.dart';
import 'package:churr/state/notify_center.dart';
import 'package:flutter_test/flutter_test.dart';

import '../account_support.dart';

/// Records what the center asked the platform for.
class FakeNotifier implements Notifier {
  final shown = <ShownNotification>[];
  final cancelled = <String>[];
  final badges = <int>[];

  @override
  Future<void> show(ShownNotification n) async => shown.add(n);
  @override
  Future<void> cancel(String key) async => cancelled.add(key);
  @override
  Future<void> setBadge(int count) async => badges.add(count);
}

Future<void> _settle() =>
    Future<void>.delayed(const Duration(milliseconds: 30));

void main() {
  late AppState app;
  late FakeNotifier fake;
  late NotifyCenter center;

  setUp(() async {
    (app, _) = await makeAccountApp(null);
    fake = FakeNotifier();
    center = NotifyCenter(
      app,
      notifier: fake,
      now: () => DateTime.now().toUtc(),
    )..windowInFront = false;
    NotifyCenter.install(center);
  });

  tearDown(() async {
    center.dispose();
    for (final s in app.servers) {
      await s.shutDown();
    }
  });

  Future<int> nextSeq(int server, String conversation) async =>
      (await app.servers[server].db.conversation(conversation))!.syncedSeq + 1;

  Map<String, dynamic> created(
    int seq, {
    String notify = 'notify',
    String author = 'sam',
    String conv = 'general',
  }) => {
    'type': 'message.created',
    'conversation_id': conv,
    'seq': seq,
    'notify': notify,
    'message': {
      'id': 'n$seq',
      'conversation_id': conv,
      'seq': seq,
      'author_id': author,
      'text': 'Ship it',
      'created_at': DateTime.now().toUtc().toIso8601String(),
    },
  };

  test('an event marked notify, with the window away, is shown and grouped per conversation', () async {
    final s = app.servers[0];
    await s.sync.applyEvent(created(await nextSeq(0, 'general')));
    await _settle();
    expect(fake.shown, hasLength(1));
    final n = fake.shown.single;
    expect(n.key, 'server-0/general');
    expect(n.text.sender, 'Sam Okafor');
    expect(n.text.channel, 'General');
    expect(n.text.text, 'Ship it');
    // Two servers are signed in: the server is named.
    expect(n.text.server, 'Work');
    final tap = TapTarget.decode(n.payload)!;
    expect(tap.serverLocalId, 'server-0');
    expect(tap.messageId, startsWith('n'));
  });

  test('badge and none are not shown; neither is my own message', () async {
    final s = app.servers[0];
    await s.sync.applyEvent(
      created(await nextSeq(0, 'general'), notify: 'badge'),
    );
    await s.sync.applyEvent(
      created(await nextSeq(0, 'general'), notify: 'none'),
    );
    await s.sync.applyEvent(created(await nextSeq(0, 'general'), author: 'me'));
    await _settle();
    expect(fake.shown, isEmpty);
  });

  test('the open conversation in a window in front is not shown', () async {
    center.windowInFront = true;
    app.openConversation(const OpenTarget('server-0', 'general'));
    await app.servers[0].sync.applyEvent(created(await nextSeq(0, 'general')));
    await _settle();
    expect(fake.shown, isEmpty);
    // Another conversation is.
    final m = Message.fromJson(
      created(1, conv: 'design')['message'] as Map<String, dynamic>,
    );
    await center.considerForTest(app.servers[0], m);
    expect(fake.shown, hasLength(1));
  });

  test('the device switch, sounds and "only the sender"', () async {
    app.prefs.putRaw('notification_sounds', '0');
    app.prefs.putRaw('notification_text', '0');
    await app.servers[0].sync.applyEvent(created(await nextSeq(0, 'general')));
    await _settle();
    expect(fake.shown.single.sound, isFalse);
    expect(fake.shown.single.text.kind, BodyKind.placeholder);
    app.prefs.putRaw('notifications_on', '0');
    await app.servers[0].sync.applyEvent(created(await nextSeq(0, 'general')));
    await _settle();
    expect(fake.shown, hasLength(1));
  });

  test('read here or on another device: the notification is removed', () async {
    final s = app.servers[0];
    final seq = await nextSeq(0, 'general');
    await s.sync.applyEvent(created(seq));
    await _settle();
    expect(fake.shown, hasLength(1));
    // Another device moved the read position past it.
    await s.sync.applyEvent({
      'type': 'read_position.moved',
      'conversation_id': 'general',
      'read_seq': seq,
      'unread_count': 0,
      'mention_count': 0,
      'badge_count': 0,
    });
    await _settle();
    expect(fake.cancelled, contains('server-0/general'));
  });

  test(
    'the icon badge is the sum of the servers, and follows the totals',
    () async {
      await _settle();
      final before = fake.badges.isEmpty ? 0 : fake.badges.last;
      await app.servers[1].sync.applyEvent({
        'type': 'badge.changed',
        'conversation_id': 'design',
        'unread_count': 9,
        'mention_count': 1,
        'badge_count': 9,
        'total': {
          'badge': 12,
          'mentions': 1,
          'unread_conversations': 2,
          'unread_threads': 0,
        },
      });
      await _settle();
      expect(fake.badges.last, isNot(before));
      expect(fake.badges.last, app.totalBadge);
      expect(center.tray.icon, anyOf(TrayIcon.mention, TrayIcon.disconnected));
    },
  );

  test('a ring, then missed or answered; the test notification', () async {
    await center.showRing(
      serverLocalId: 'server-0',
      conversationId: 'chat',
      callId: 'call-1',
      callerName: 'Sam Okafor',
    );
    expect(fake.shown.last.channel, NotifyChannel.call);
    expect(TapTarget.decode(fake.shown.last.payload)!.callId, 'call-1');
    await center.stopRing(
      serverLocalId: 'server-0',
      conversationId: 'chat',
      callerName: 'Sam Okafor',
      answered: false,
    );
    expect(fake.shown.last.missed, isTrue);
    await center.stopRing(
      serverLocalId: 'server-0',
      conversationId: 'chat',
      callerName: 'Sam Okafor',
      answered: true,
    );
    expect(fake.cancelled.last, 'server-0/chat');
    await center.sendTest();
    expect(fake.shown.last.text.kind, BodyKind.test);
  });

  test(
    'tapping opens the message in place, or the incoming-call prompt',
    () async {
      center.openTapped(
        const TapTarget(
          serverLocalId: 'server-1',
          conversationId: 'general',
          messageId: 'm2',
          parentId: 'm1',
        ).encode(),
      );
      expect(app.open, const OpenTarget('server-1', 'general'));
      expect(app.focusMessageId, 'm2');
      expect(app.focusThreadId, 'm1');
      String? prompted;
      IncomingCallHook.openPrompt = (s, c, call) => prompted = '$s/$c/$call';
      addTearDown(() => IncomingCallHook.openPrompt = null);
      center.openTapped(
        const TapTarget(
          serverLocalId: 'server-0',
          conversationId: 'chat',
          callId: 'k',
        ).encode(),
      );
      expect(prompted, 'server-0/chat/k');
      // A server that is gone, or garbage: nothing happens.
      center.openTapped('nonsense');
      center.openTapped(
        const TapTarget(serverLocalId: 'gone', conversationId: 'x').encode(),
      );
    },
  );

  test('a request for approval is notified, with what is asked', () async {
    final now = DateTime.now().toUtc().toIso8601String();
    await app.servers[0].sync.applyEvent({
      'type': 'approval.created',
      'approval': {
        'id': 'a1',
        'kind': 'channel',
        'requester_id': 'sam',
        'requester': {'id': 'sam', 'username': 'sam', 'display_name': 'Sam'},
        'summary': 'launch',
        'state': 'waiting',
        'created_at': now,
      },
    });
    await _settle();
    final n = fake.shown.single;
    expect(n.text.kind, BodyKind.approval);
    expect(n.text.text, 'launch');
    // The device's switch decides, as for anything addressed to one person.
    app.prefs.putRaw('notifications_on', '0');
    await app.servers[0].sync.applyEvent({
      'type': 'approval.created',
      'approval': {'id': 'a2', 'kind': 'registration', 'created_at': now},
    });
    await _settle();
    expect(fake.shown, hasLength(1));
  });

  test('a request to join is notified to who answers it, and opens the '
      'channel; not while that channel is open in front', () async {
    final now = DateTime.now().toUtc().toIso8601String();
    Map<String, dynamic> knock(String who) => {
      'type': 'knock.created',
      'knock': {
        'conversation_id': 'general',
        'account_id': who,
        'created_at': now,
      },
      'conversation_name': 'general',
    };
    await app.servers[0].sync.applyEvent(knock('sam'));
    await _settle();
    final n = fake.shown.single;
    expect(n.text.kind, BodyKind.knock);
    expect(n.text.channel, 'general');
    expect(TapTarget.decode(n.payload)?.conversationId, 'general');

    center.windowInFront = true;
    app.openConversation(const OpenTarget('server-0', 'general'));
    await app.servers[0].sync.applyEvent(knock('lee'));
    await _settle();
    expect(fake.shown, hasLength(1));
  });
}
