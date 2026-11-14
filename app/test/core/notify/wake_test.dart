import 'dart:convert';

import 'package:churr/core/api/client.dart';
import 'package:churr/core/notify/content.dart';
import 'package:churr/core/notify/registration.dart';
import 'package:churr/core/notify/shared_items.dart';
import 'package:churr/core/notify/wake.dart';
import 'package:churr/core/session_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// A server answering `fetchWakeUp` and `registerDevice`, recording what
/// it was asked.
class FakeWakeServer {
  final seen = <http.Request>[];
  Map<String, dynamic>? wakeUp;
  int status = 200;
  String? errorCode;
  bool slow = false;

  /// `registerDevice`: refuse these push kinds with `feature_off`.
  Set<String> refuse = {};

  ApiClient client() => ApiClient(
    base: Uri.parse('https://chat.example.org'),
    tokens: Tokens(
      access: 'churr_app_x',
      refresh: '',
      expiresAt: DateTime.now().add(const Duration(hours: 1)),
    ),
    httpClient: MockClient((r) async {
      seen.add(r);
      if (slow) await Future<void>.delayed(const Duration(milliseconds: 300));
      if (r.url.path.startsWith('/api/v1/wake-ups/')) {
        if (errorCode != null) {
          return http.Response(
            jsonEncode({
              'error': {'code': errorCode, 'message': 'no'},
            }),
            status,
          );
        }
        return http.Response(jsonEncode({'wake_up': wakeUp}), 200);
      }
      if (r.url.path == '/api/v1/me/devices/current') {
        final body = jsonDecode(r.body) as Map<String, dynamic>;
        if (refuse.contains(body['push_kind'])) {
          return http.Response(
            jsonEncode({
              'error': {'code': 'feature_off', 'message': 'off'},
            }),
            403,
          );
        }
        return http.Response(
          jsonEncode({
            'device': {
              'id': 'dev-1',
              'name': body['name'] ?? 'x',
              'platform': body['platform'] ?? 'ios',
              'created_at': '2026-11-14T00:00:00Z',
              'push_kind': body['push_kind'],
              'wake_route': 'q83vEjRWeJA',
            },
          }),
          200,
        );
      }
      return http.Response('{}', 404);
    }),
  );
}

Map<String, dynamic> _msg(
  String id,
  int seq,
  String text, {
  Map<String, dynamic>? extra,
}) => {
  'message': {
    'id': id,
    'conversation_id': 'c1',
    'seq': seq,
    'author_id': 'sam',
    'text': text,
    'created_at': '2026-11-14T12:00:00Z',
    ...?extra,
  },
  'author_name': 'Sam Okafor',
};

void main() {
  late FakeWakeServer server;
  late ApiClient api;
  WakeHandler handler({
    bool multi = false,
    bool showText = true,
    SealedOpener? sealed,
  }) => WakeHandler(
    routes: {'q83vEjRWeJA': 'server-1'},
    clientFor: (id) => id == 'server-1' ? api : null,
    serverName: (_) => 'Work',
    multiServer: multi,
    showText: showText,
    openSealed: sealed,
    timeout: const Duration(milliseconds: 100),
  );

  setUp(() {
    server = FakeWakeServer();
    api = server.client();
  });

  test(
    'fetches by event id from the server of the route and shows the message',
    () async {
      server.wakeUp = {
        'kind': 'message',
        'conversation': {'id': 'c1', 'kind': 'channel', 'name': 'ops'},
        'messages': [_msg('m1', 4, 'first'), _msg('m2', 5, 'Deploy **now**')],
        'more': 2,
        'read': false,
        'badge': 7,
      };
      final d = await handler(multi: true)
          .handle('q83vEjRWeJA.TOKEN', badge: 3);
      expect(server.seen.single.url.path, '/api/v1/wake-ups/q83vEjRWeJA.TOKEN');
      expect(server.seen.single.headers['authorization'], 'Bearer churr_app_x');
      expect(d.outcome, WakeOutcome.message);
      expect(d.serverLocalId, 'server-1');
      expect(d.conversationId, 'c1');
      expect(d.messageId, 'm2');
      expect(d.text!.sender, 'Sam Okafor');
      expect(d.text!.channel, 'ops');
      expect(d.text!.text, 'Deploy now');
      expect(d.text!.server, 'Work');
      expect(d.text!.more, 3); // one more in the list, two beyond it
      expect(d.badge, 7); // the fetch's total, not the push's
    },
  );

  test(
    'an unknown route falls back to "New message" without asking anyone',
    () async {
      final d = await handler().handle('ZZZZZZZZZZZ.TOKEN', badge: 4);
      expect(d.outcome, WakeOutcome.placeholder);
      expect(d.badge, 4);
      expect(server.seen, isEmpty);
    },
  );

  test(
    'not_found, a server error and a timeout fall back to "New message"',
    () async {
      server.errorCode = 'not_found';
      server.status = 404;
      expect(
        (await handler().handle('q83vEjRWeJA.T')).outcome,
        WakeOutcome.placeholder,
      );
      server.errorCode = 'internal';
      server.status = 500;
      expect(
        (await handler().handle('q83vEjRWeJA.T')).outcome,
        WakeOutcome.placeholder,
      );
      server
        ..errorCode = null
        ..slow = true
        ..wakeUp = {
          'kind': 'message',
          'conversation': {'id': 'c1', 'kind': 'channel'},
          'messages': [],
          'more': 0,
          'read': false,
          'badge': 0,
        };
      expect(
        (await handler().handle('q83vEjRWeJA.T')).outcome,
        WakeOutcome.placeholder,
      );
    },
  );

  test('read already: nothing to show', () async {
    server.wakeUp = {
      'kind': 'message',
      'conversation': {'id': 'c1', 'kind': 'channel'},
      'messages': [],
      'more': 0,
      'read': true,
      'badge': 0,
    };
    expect(
      (await handler().handle('q83vEjRWeJA.T')).outcome,
      WakeOutcome.nothing,
    );
  });

  test('a ring that still rings, and one that stopped', () async {
    server.wakeUp = {
      'kind': 'ring',
      'conversation': {'id': 'c1', 'kind': 'channel', 'title': 'Sam Okafor'},
      'messages': [],
      'more': 0,
      'read': false,
      'call': {
        'id': 'call-1',
        'from_account_id': 'sam',
        'from_name': 'Sam Okafor',
        'ringing': true,
      },
      'badge': 1,
    };
    var d = await handler().handle('q83vEjRWeJA.T');
    expect(d.outcome, WakeOutcome.ring);
    expect(d.callId, 'call-1');
    expect(d.callerName, 'Sam Okafor');
    (server.wakeUp!['call'] as Map)['ringing'] = false;
    d = await handler().handle('q83vEjRWeJA.T');
    expect(d.outcome, WakeOutcome.missedCall);
  });

  test(
    'a sealed message: opened by the sealed code, else the placeholder',
    () async {
      server.wakeUp = {
        'kind': 'message',
        'conversation': {'id': 'c1', 'kind': 'sealed', 'title': 'Sam Okafor'},
        'messages': [
          _msg(
            'm9',
            9,
            '',
            extra: {
              'sealed': {'key_epoch': 1, 'ciphertext': 'AQ'},
            },
          ),
        ],
        'more': 0,
        'read': false,
        'badge': 1,
      };
      var d = await handler().handle('q83vEjRWeJA.T');
      expect(d.text!.kind, BodyKind.placeholder);
      d = await handler(
        sealed: (s, m) async => s == 'server-1' ? 'hello there' : null,
      ).handle('q83vEjRWeJA.T');
      expect(d.text!.kind, BodyKind.text);
      expect(d.text!.text, 'hello there');
      // With previews off, the sealed code is not even asked.
      var asked = false;
      d = await handler(
        showText: false,
        sealed: (s, m) async {
          asked = true;
          return 'x';
        },
      ).handle('q83vEjRWeJA.T');
      expect(asked, isFalse);
      expect(d.text!.kind, BodyKind.placeholder);
    },
  );

  test(
    'registration sends the push kind and address and keeps the route',
    () async {
      final r = await registerThisDevice(
        api,
        name: 'iPhone',
        platform: 'ios',
        push: const PushAddress(PushKind.apns, 'abcd'),
      );
      final body = jsonDecode(server.seen.single.body) as Map<String, dynamic>;
      expect(server.seen.single.method, 'PUT');
      expect(body, {
        'name': 'iPhone',
        'platform': 'ios',
        'push_kind': 'apns',
        'push_address': 'abcd',
      });
      expect(r.wakeRoute, 'q83vEjRWeJA');
      expect(r.deviceId, 'dev-1');
      expect(r.refused, isFalse);
    },
  );

  test('a refused push kind falls back to none, without an address', () async {
    server.refuse = {'apns'};
    final r = await registerThisDevice(
      api,
      name: 'iPhone',
      platform: 'ios',
      push: const PushAddress(PushKind.apns, 'abcd'),
    );
    expect(r.kind, PushKind.none);
    expect(r.refused, isTrue);
    final second = jsonDecode(server.seen.last.body) as Map<String, dynamic>;
    expect(second['push_kind'], 'none');
    expect(second.containsKey('push_address'), isFalse);
  });

  test(
    'items for the iPhone extension: servers and epochs, and forgetting them',
    () async {
      final store = MemorySecretStore();
      await writeSharedServers(store, const [
        SharedServer(
          localId: 's1',
          address: 'https://a',
          name: 'A',
          route: 'r1',
        ),
      ], showText: true);
      final servers =
          jsonDecode(store.values[serversItem]!) as Map<String, dynamic>;
      expect(servers['multi'], isFalse);
      expect((servers['servers'] as List).single, {
        'id': 's1',
        'address': 'https://a',
        'name': 'A',
        'route': 'r1',
        'token_key': 'churr.tokens.s1',
      });
      await shareEpochForNotifications(
        store,
        serverLocalId: 's1',
        conversationId: 'c1',
        keyEpoch: 3,
        epochSeq: 10,
        contentKey: List.filled(32, 255),
        senders: [
          SharedSender(
            accountId: 'a',
            deviceId: 'd',
            signKey: List.filled(32, 0),
          ),
        ],
      );
      final e = jsonDecode(
        store.values['churr.notify.epoch.s1.c1.3']!,
      ) as Map<String, dynamic>;
      expect(e['content_key'], '_' * 42 + '8');
      expect(e['epoch_seq'], 10);
      expect((e['senders'] as List).single['sign_key'], 'A' * 43);
      await forgetSharedEpochs(store, 's1');
      expect(store.values.containsKey('churr.notify.epoch.s1.c1.3'), isFalse);
    },
  );
}
