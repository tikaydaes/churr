import 'package:churr/core/models/message.dart';
import 'package:churr/core/notify/content.dart';
import 'package:churr/core/notify/decision.dart';
import 'package:churr/core/notify/registration.dart';
import 'package:churr/core/notify/tray_state.dart';
import 'package:churr/core/notify/wake.dart';
import 'package:flutter_test/flutter_test.dart';

final _now = DateTime.utc(2026, 11, 14, 12);

NotifyInputs _in({
  String? notify = 'notify',
  Duration age = const Duration(seconds: 5),
  bool own = false,
  bool on = true,
  bool muted = false,
  bool dnd = false,
  bool front = false,
  bool onScreen = false,
}) => NotifyInputs(
  notify: notify,
  messageAt: _now.subtract(age),
  now: _now,
  own: own,
  notificationsOn: on,
  serverMuted: muted,
  dnd: dnd,
  windowInFront: front,
  conversationOnScreen: onScreen,
);

Message _m(Map<String, dynamic> extra) => Message.fromJson({
  'id': 'm1',
  'conversation_id': 'c1',
  'seq': 5,
  'author_id': 'sam',
  'created_at': '2026-11-14T12:00:00Z',
  ...extra,
});

void main() {
  group('the decision', () {
    test('shows an event the server marked notify', () {
      expect(decide(_in()), NotifyVerdict.show);
    });

    test('the server has the last word: none and badge never pop up', () {
      expect(decide(_in(notify: 'badge')), NotifyVerdict.serverSaidNo);
      expect(decide(_in(notify: 'none')), NotifyVerdict.serverSaidNo);
      expect(decide(_in(notify: null)), NotifyVerdict.serverSaidNo);
    });

    test("the person's own message, the switch, mute and do not disturb", () {
      expect(decide(_in(own: true)), NotifyVerdict.own);
      expect(decide(_in(on: false)), NotifyVerdict.switchedOff);
      expect(decide(_in(muted: true)), NotifyVerdict.quiet);
      expect(decide(_in(dnd: true)), NotifyVerdict.quiet);
    });

    test(
      'on screen in a window in front: nothing; in front elsewhere: shown',
      () {
        expect(
          decide(_in(front: true, onScreen: true)),
          NotifyVerdict.onScreen,
        );
        expect(decide(_in(front: true)), NotifyVerdict.show);
        // Open but the window is behind another: shown.
        expect(decide(_in(onScreen: true)), NotifyVerdict.show);
      },
    );

    test('replayed news older than ten minutes is not news', () {
      expect(
        decide(_in(age: const Duration(minutes: 11))),
        NotifyVerdict.stale,
      );
      expect(decide(_in(age: const Duration(minutes: 9))), NotifyVerdict.show);
    });
  });

  group('grouping and clearing', () {
    test('one key per server and conversation, a stable id', () {
      expect(groupKey('s1', 'c1'), 's1/c1');
      expect(groupKey('s1', 'c1'), isNot(groupKey('s2', 'c1')));
      final id = notificationId('s1/c1');
      expect(id, notificationId('s1/c1'));
      expect(id, isNot(notificationId('s1/c2')));
      expect(id, inInclusiveRange(0, 0x7fffffff));
      // FNV-1a of the empty string, masked to 31 bits.
      expect(notificationId(''), 0x811c9dc5 & 0x7fffffff);
    });

    test('a shown conversation clears when its items are all read', () {
      final gone = conversationsToClear(
        {'a', 'b', 'c'},
        const [
          PendingItem('a', cleared: true),
          PendingItem('b', cleared: true),
          PendingItem('b', cleared: false),
        ],
      );
      expect(gone, {'a', 'c'});
    });
  });

  group('what a notification says', () {
    test('a named channel: sender and channel, plain text', () {
      final n = describeMessage(
        _m({'text': 'Look at **this** and @sam'}),
        sender: 'Sam',
        channel: 'ops',
      );
      expect(n.channel, 'ops');
      expect(n.kind, BodyKind.text);
      expect(n.text, contains('Look at this'));
      expect(n.text, isNot(contains('**')));
    });

    test('a chat with one person has no separate title', () {
      final n = describeMessage(
        _m({'text': 'hi'}),
        sender: 'Sam',
        chatTitle: 'Sam',
      );
      expect(n.chatTitle, isNull);
      final g = describeMessage(
        _m({'text': 'hi'}),
        sender: 'Sam',
        chatTitle: 'Sam, Ana',
      );
      expect(g.chatTitle, 'Sam, Ana');
    });

    test('a line for what it holds, and the placeholder', () {
      expect(
        describeMessage(
          _m({
            'files': [
              {'id': 'f', 'name': 'a.png'},
            ],
          }),
          sender: 'S',
        ).kind,
        BodyKind.file,
      );
      expect(
        describeMessage(
          _m({
            'form': {'kind': 'poll'},
          }),
          sender: 'S',
        ).kind,
        BodyKind.form,
      );
      expect(
        describeMessage(
          _m({'text': 'secret'}),
          sender: 'S',
          showText: false,
        ).kind,
        BodyKind.placeholder,
      );
      expect(
        describeMessage(
          _m({
            'sealed': {'key_epoch': 1, 'ciphertext': 'AA'},
          }),
          sender: 'S',
        ).kind,
        BodyKind.placeholder,
      );
    });

    test('at most 300 characters, spoilers hidden', () {
      final long = plainText('word ' * 200);
      expect(long.runes.length, maxBodyLength);
      expect(long.endsWith('…'), isTrue);
      expect(plainText('the end is ||Bruce||'), isNot(contains('Bruce')));
    });
  });

  group('the tray', () {
    TrayServer s({
      int badge = 0,
      int mentions = 0,
      int direct = 0,
      int unread = 0,
      bool connected = true,
      bool dnd = false,
      bool muted = false,
    }) => TrayServer(
      localId: 'x',
      name: 'X',
      badge: badge,
      mentions: mentions,
      directBadge: direct,
      unreadConversations: unread,
      connected: connected,
      dnd: dnd,
      muted: muted,
    );

    test('each state', () {
      expect(trayState([s()]).icon, TrayIcon.normal);
      expect(trayState([s(unread: 2)]).icon, TrayIcon.unread);
      expect(trayState([s(badge: 3, mentions: 1)]).icon, TrayIcon.mention);
      expect(trayState([s(badge: 1, direct: 1)]).icon, TrayIcon.mention);
      expect(trayState([s(dnd: true, mentions: 4)]).icon, TrayIcon.dnd);
      expect(trayState([s(connected: false)]).icon, TrayIcon.disconnected);
      expect(trayState(const []).icon, TrayIcon.disconnected);
    });

    test('a quiet server does not raise the mention mark, but counts', () {
      final t = trayState([
        s(muted: true, mentions: 2, badge: 2),
        s(unread: 1, badge: 1),
      ]);
      expect(t.icon, TrayIcon.unread);
      expect(t.count, 3);
    });

    test('one disconnected server of two is still connected', () {
      expect(trayState([s(connected: false), s()]).icon, TrayIcon.normal);
    });

    test('icon names and the short count', () {
      expect(trayIconName(trayState([s(badge: 3, mentions: 1)])), 'mention-3');
      expect(
        trayIconName(trayState([s(badge: 30, mentions: 1)])),
        'mention-9plus',
      );
      expect(trayState([s(badge: 30, mentions: 1)]).shortCount, '9+');
      expect(trayIconName(trayState([s(dnd: true)])), 'dnd');
    });
  });

  group('wake-ups and registration', () {
    test('event identifiers split into route and token', () {
      expect(splitEventId('q83vEjRWeJA.AAECAwQ'), ('q83vEjRWeJA', 'AAECAwQ'));
      expect(splitEventId('noroute'), isNull);
      expect(splitEventId('.x'), isNull);
      expect(splitEventId('a b.c'), isNull);
      expect(splitEventId('a.${'x' * 300}'), isNull);
    });

    test('the push kind: the best address the server accepts, else none', () {
      const apns = PushAddress(PushKind.apns, 'aa');
      const up = PushAddress(PushKind.unifiedpush, 'https://up');
      const fcm = PushAddress(PushKind.fcm, 'tok');
      expect(choosePush([apns], ['apns', 'fcm']).kind, PushKind.apns);
      expect(choosePush([apns], []).kind, PushKind.none);
      expect(choosePush([fcm, up], ['unifiedpush']).kind, PushKind.unifiedpush);
      expect(choosePush([], ['apns']).kind, PushKind.none);
    });

    test('device names are 1 to 60 characters', () {
      expect(deviceName('  '), 'Churr');
      expect(deviceName('x' * 80).length, 60);
    });
  });
}
