import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:churr/core/api/client.dart';
import 'package:churr/core/live.dart';
import 'package:churr/core/models/json.dart';
import 'package:churr/core/session_store.dart';
import 'package:fake_async/fake_async.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// A socket the test plays the server on.
class FakeSocket implements LiveSocket {
  final _in = StreamController<String>();
  final sent = <Json>[];
  bool closed = false;
  @override
  int? closeCode;

  @override
  Stream<String> get incoming => _in.stream;
  @override
  void send(String text) => sent.add(jsonDecode(text) as Json);
  @override
  Future<void> close() async => serverClose(1000);

  void serverSend(Json frame) => _in.add(jsonEncode(frame));
  void serverClose(int code) {
    if (closed) return;
    closed = true;
    closeCode = code;
    _in.close();
  }

  List<String> get sentTypes => [for (final f in sent) f['type'] as String];
}

class Recorder implements LiveDelegate {
  final log = <String>[];
  Map<String, int> held = {'c1': 4821, 'c2': 96};
  Completer<void>? holdEvents;

  @override
  Future<Map<String, int>> positions() async => held;
  @override
  Future<void> onEvent(Json event, {required bool replay}) async {
    await holdEvents?.future;
    log.add('${replay ? 'replay' : 'live'} ${event['type']} ${event['seq']}');
  }

  @override
  Future<void> onReload(Json frame) async =>
      log.add('reload ${frame['conversation_id']} ${frame['reason']}');
  @override
  Future<void> onReady(List<String> ids) async => log.add('ready $ids');
}

/// A source of randomness that always draws the same number, so a test
/// knows which wait was chosen: 0 is the shortest, just under 1 the longest.
class _Fixed implements Random {
  _Fixed(this.unit);
  double unit;
  @override
  double nextDouble() => unit;
  @override
  bool nextBool() => unit >= 0.5;
  @override
  int nextInt(int max) => (unit * max).floor();
}

const _hello = {
  'type': 'hello',
  'connection_id': 'c_1',
  'account_id': 'a',
  'session_id': 's',
  'heartbeat_seconds': 30,
  'resume_within_seconds': 10,
  'server_time': '2026-11-13T09:30:00.000Z',
};

void main() {
  late List<FakeSocket> sockets;
  late List<(Uri, Map<String, String>, List<String>)> dials;
  late Recorder delegate;
  late int refreshes;
  late bool refuseRefresh;
  late bool failDial;

  // Unless a test says otherwise every wait is the shortest the rule
  // allows: half the retry interval.
  LiveConnection make({Random? random}) {
    final api = ApiClient(
      base: Uri.parse('https://chat.example.org'),
      tokens: Tokens(
        access: 'churr_app_A',
        refresh: 'churr_ref_A',
        expiresAt: DateTime.now().add(const Duration(hours: 1)),
      ),
      httpClient: MockClient((r) async {
        if (r.url.path == '/oauth/token') {
          refreshes++;
          return refuseRefresh
              ? http.Response('{"error":"invalid_grant"}', 400)
              : http.Response(
                  '{"access_token":"churr_app_B","refresh_token":"churr_ref_B","expires_in":3600}',
                  200,
                );
        }
        return http.Response('{"badge":0}', 200);
      }),
    );
    return LiveConnection(
      api: api,
      delegate: delegate,
      random: random ?? _Fixed(0),
      connector: (url, headers, protocols) async {
        dials.add((url, headers, protocols));
        if (failDial) throw Exception('no route to host');
        final s = FakeSocket();
        sockets.add(s);
        return s;
      },
    );
  }

  setUp(() {
    sockets = [];
    dials = [];
    delegate = Recorder();
    refreshes = 0;
    refuseRefresh = false;
    failDial = false;
  });

  test('the upgrade: wss, the subprotocol, the token in the header and never in the address', () {
    fakeAsync((async) {
      final live = make()..start();
      async.flushMicrotasks();
      final (url, headers, protocols) = dials.single;
      expect(url.toString(), 'wss://chat.example.org/api/v1/events');
      expect(headers, {'Authorization': 'Bearer churr_app_A'});
      expect(protocols, ['churr.v1']);
      expect(live.state, LiveState.connecting);
      live.stop();
      async.flushMicrotasks();
    });
  });

  test(
    'hello, resume with positions, replay in order, reload, ready, then live',
    () {
      fakeAsync((async) {
        final live = make()..start();
        final states = <LiveState>[];
        live.states.listen(states.add);
        async.flushMicrotasks();
        final s = sockets.single;

        s.serverSend(_hello);
        async.flushMicrotasks();
        expect(s.sent.single, {
          'type': 'resume',
          'positions': {'c1': 4821, 'c2': 96},
        });
        expect(live.state, LiveState.catchingUp);
        expect(live.connectionId, 'c_1');

        s.serverSend({
          'type': 'message.created',
          'conversation_id': 'c1',
          'seq': 4822,
          'at': 't',
        });
        s.serverSend({
          'type': 'reaction.added',
          'conversation_id': 'c1',
          'seq': 4823,
          'at': 't',
        });
        s.serverSend({
          'type': 'reload',
          'conversation_id': 'c2',
          'reason': 'too_far_behind',
          'last_seq': 9120,
        });
        s.serverSend({
          'type': 'ready',
          'new_conversation_ids': ['c9'],
        });
        s.serverSend({
          'type': 'message.created',
          'conversation_id': 'c1',
          'seq': 4824,
          'at': 't',
        });
        s.serverSend({
          'type': 'something_new',
          'x': 1,
        }); // an unknown control frame
        async.flushMicrotasks();

        expect(delegate.log, [
          'replay message.created 4822',
          'replay reaction.added 4823',
          'reload c2 too_far_behind',
          'ready [c9]',
          'live message.created 4824',
        ]);
        expect(states, [LiveState.catchingUp, LiveState.ready]);
        live.stop();
        async.flushMicrotasks();
      });
    },
  );

  test(
    'events are applied one at a time, in order, even when applying is slow',
    () {
      fakeAsync((async) {
        final live = make()..start();
        async.flushMicrotasks();
        final s = sockets.single..serverSend(_hello);
        async.flushMicrotasks();
        delegate.holdEvents = Completer<void>();
        for (var seq = 1; seq <= 3; seq++) {
          s.serverSend({
            'type': 'message.created',
            'conversation_id': 'c1',
            'seq': seq,
            'at': 't',
          });
        }
        async.flushMicrotasks();
        expect(delegate.log, isEmpty);
        delegate.holdEvents!.complete();
        async.flushMicrotasks();
        expect(delegate.log, [
          'replay message.created 1',
          'replay message.created 2',
          'replay message.created 3',
        ]);
        live.stop();
        async.flushMicrotasks();
      });
    },
  );

  test(
    'heartbeats: a ping every heartbeat, and a pong keeps the connection',
    () {
      fakeAsync((async) {
        final live = make()..start();
        async.flushMicrotasks();
        final s = sockets.single..serverSend(_hello);
        async.flushMicrotasks();
        s.serverSend({'type': 'ready', 'new_conversation_ids': []});
        async.elapse(const Duration(seconds: 30));
        expect(s.sentTypes, ['resume', 'ping']);
        s.serverSend({'type': 'pong', 'server_time': 't'});
        async.elapse(const Duration(seconds: 30));
        expect(s.sentTypes, ['resume', 'ping', 'ping']);
        expect(s.closed, isFalse);
        s.serverSend({'type': 'pong', 'server_time': 't'});
        async.flushMicrotasks();
        live.stop();
        async.flushMicrotasks();
      });
    },
  );

  test(
    'no pong within one heartbeat: the connection is dead, and is opened again',
    () {
      fakeAsync((async) {
        final live = make()..start();
        async.flushMicrotasks();
        final s = sockets.single..serverSend(_hello);
        async.flushMicrotasks();
        s.serverSend({'type': 'ready', 'new_conversation_ids': []});
        async.elapse(const Duration(seconds: 30)); // ping
        async.elapse(const Duration(seconds: 30)); // no pong
        expect(s.closed, isTrue);
        expect(live.state, LiveState.offline);
        async.elapse(
          const Duration(seconds: 1),
        ); // the shortest first wait is half a second
        expect(sockets, hasLength(2));
        live.stop();
        async.flushMicrotasks();
      });
    },
  );

  test('every wait is drawn between half the retry interval and the interval plus the spread', () {
    const steps = [1, 2, 5, 10, 30];
    for (final spread in [2, 5, 20, 60]) {
      for (final interval in steps) {
        Duration at(double unit) => LiveConnection.reconnectDelay(
          intervalSeconds: interval,
          spreadSeconds: spread,
          unit: unit,
        );
        final low = interval * 500, high = (interval + spread) * 1000;
        expect(
          at(0).inMilliseconds,
          low,
          reason: '$interval s, spread $spread',
        );
        expect(
          at(1).inMilliseconds,
          high,
          reason: '$interval s, spread $spread',
        );
        expect(at(0.5).inMilliseconds, (low + high) ~/ 2);
        final random = Random(interval * 100 + spread);
        for (var i = 0; i < 500; i++) {
          expect(
            at(random.nextDouble()).inMilliseconds,
            inInclusiveRange(low, high),
          );
        }
      }
    }
    // The lower bounds the ruling names: 0.5, 1, 2.5, 5 and 15 seconds.
    expect(
      [
        for (final interval in steps)
          LiveConnection.reconnectDelay(
            intervalSeconds: interval,
            spreadSeconds: 5,
            unit: 0,
          ).inMilliseconds,
      ],
      [500, 1000, 2500, 5000, 15000],
    );
  });

  test(
    'the steps are 1, 2, 5, 10, then 30 seconds, with the default spread of 5',
    () {
      final shortest = make(), longest = make(random: _Fixed(1));
      expect(shortest.spreadSeconds, 5);
      for (var i = 0; i < 7; i++) {
        final interval = [1, 2, 5, 10, 30, 30, 30][i];
        expect(shortest.backoff(i).inMilliseconds, interval * 500);
        expect(longest.backoff(i).inMilliseconds, (interval + 5) * 1000);
      }
      final drawn = make(random: Random(7));
      for (var n = 0; n < 200; n++) {
        for (var i = 0; i < 5; i++) {
          final interval = LiveConnection.backoffSeconds[i];
          expect(
            drawn.backoff(i).inMilliseconds,
            inInclusiveRange(interval * 500, (interval + 5) * 1000),
            reason: 'attempt $i',
          );
        }
      }
    },
  );

  test('hello sets the spread; it is kept for later reconnects, and a hello without one changes nothing', () {
    fakeAsync((async) {
      final random = _Fixed(1);
      final live = make(random: random)..start();
      async.flushMicrotasks();
      // An older server says nothing: the default stands.
      sockets.single
        ..serverSend(_hello)
        ..serverSend({'type': 'ready', 'new_conversation_ids': []});
      async.flushMicrotasks();
      expect(live.spreadSeconds, LiveConnection.defaultSpreadSeconds);
      expect(live.backoff(0), const Duration(seconds: 6));
      sockets.single.serverClose(1012);
      async.flushMicrotasks();
      async.elapse(const Duration(seconds: 6));
      expect(sockets, hasLength(2));
      // This server asks for twenty seconds.
      sockets.last
        ..serverSend({..._hello, 'reconnect_spread_seconds': 20})
        ..serverSend({'type': 'ready', 'new_conversation_ids': []});
      async.flushMicrotasks();
      expect(live.spreadSeconds, 20);
      expect(live.backoff(0), const Duration(seconds: 21));
      expect(live.backoff(4), const Duration(seconds: 50));
      random.unit = 0;
      expect(live.backoff(0), const Duration(milliseconds: 500));
      expect(live.backoff(4), const Duration(seconds: 15));
      random.unit = 1;
      // The next reconnect uses it: nothing before 21 seconds.
      sockets.last.serverClose(1012);
      async.flushMicrotasks();
      async.elapse(const Duration(milliseconds: 20900));
      expect(sockets, hasLength(2));
      async.elapse(const Duration(milliseconds: 200));
      expect(sockets, hasLength(3));
      // A hello without the field (the server was rolled back) keeps
      // the last value heard.
      sockets.last.serverSend(_hello);
      async.flushMicrotasks();
      expect(live.spreadSeconds, 20);
      // The documented limits, and nonsense.
      for (final (said, kept) in [
        (2, 2),
        (60, 60),
        (600, 60),
        (-3, 60),
        ('x', 60),
      ]) {
        sockets.last.serverSend({..._hello, 'reconnect_spread_seconds': said});
        async.flushMicrotasks();
        expect(live.spreadSeconds, kept, reason: 'told $said');
      }
      live.stop();
      async.flushMicrotasks();
    });
  });

  test('the wait that was chosen is the one the countdown shows', () {
    fakeAsync((async) {
      final random = _Fixed(0.5);
      final live = make(random: random)..start();
      async.flushMicrotasks();
      expect(live.retryAt, isNull, reason: 'connected: nothing to count');
      expect(live.retrySeconds(), isNull);
      final seen = <(LiveState, DateTime?)>[];
      live.states.listen((s) => seen.add((s, live.retryAt)));
      sockets.single.serverSend({..._hello, 'reconnect_spread_seconds': 20});
      async.flushMicrotasks();
      // Interval 1, spread 20, the middle draw: (0.5 + 21) / 2 seconds.
      final before = DateTime.now();
      sockets.single.serverClose(1012);
      async.flushMicrotasks();
      expect(live.state, LiveState.offline);
      final at = live.retryAt!;
      expect(
        at.difference(before).inMilliseconds,
        inInclusiveRange(10750, 10750 + 2000),
      );
      // Whoever redraws on the change to offline already has the moment.
      expect(seen.last.$1, LiveState.offline);
      expect(seen.last.$2, at);
      // Counted in whole seconds, rounded up, from the moment given.
      expect(
        live.retrySeconds(at.subtract(const Duration(milliseconds: 10750))),
        11,
      );
      expect(live.retrySeconds(at.subtract(const Duration(seconds: 10))), 10);
      expect(
        live.retrySeconds(at.subtract(const Duration(milliseconds: 9999))),
        10,
      );
      expect(
        live.retrySeconds(at.subtract(const Duration(milliseconds: 1001))),
        2,
      );
      expect(live.retrySeconds(at.subtract(const Duration(seconds: 1))), 1);
      expect(
        live.retrySeconds(at.subtract(const Duration(milliseconds: 1))),
        1,
      );
      expect(live.retrySeconds(at), 1, reason: 'never zero while waiting');
      expect(live.retrySeconds(at.add(const Duration(seconds: 3))), 1);
      // The attempt is made when the wait is over, not before.
      async.elapse(const Duration(milliseconds: 10700));
      expect(sockets, hasLength(1));
      async.elapse(const Duration(milliseconds: 100));
      expect(sockets, hasLength(2));
      expect(live.retryAt, isNull);
      // Coming to the front cuts the wait short, and the count goes.
      sockets.last.serverClose(1012);
      async.flushMicrotasks();
      expect(live.retryAt, isNotNull);
      live.connectNow();
      async.flushMicrotasks();
      expect(sockets, hasLength(3));
      expect(live.retryAt, isNull);
      live.stop();
      async.flushMicrotasks();
    });
  });

  test('the attempts follow the steps', () {
    fakeAsync((async) {
      final live = make();
      failDial = true;
      live.start();
      async.flushMicrotasks();
      expect(dials, hasLength(1));
      async.elapse(const Duration(milliseconds: 600)); // 0.5
      expect(dials, hasLength(2));
      async.elapse(const Duration(milliseconds: 1000)); // 1
      expect(dials, hasLength(3));
      async.elapse(const Duration(seconds: 2));
      expect(dials, hasLength(3), reason: 'the third wait is 2.5 seconds');
      async.elapse(const Duration(milliseconds: 600));
      expect(dials, hasLength(4));
      live.stop();
      async.flushMicrotasks();
    });
  });

  test('connectNow skips the wait: the network is back, or the app came to the front', () {
    fakeAsync((async) {
      failDial = true;
      final live = make()..start();
      async.flushMicrotasks();
      async.elapse(const Duration(milliseconds: 600));
      async.elapse(const Duration(milliseconds: 1000));
      expect(dials, hasLength(3));
      failDial = false;
      live.connectNow();
      async.flushMicrotasks();
      expect(dials, hasLength(4));
      expect(sockets, hasLength(1));
      live.stop();
      async.flushMicrotasks();
    });
  });

  test('reaching ready resets the backoff', () {
    fakeAsync((async) {
      failDial = true;
      final live = make()..start();
      async.elapse(const Duration(seconds: 20)); // several failed attempts
      final before = dials.length;
      expect(before, greaterThanOrEqualTo(4));
      failDial = false;
      live.connectNow();
      async.flushMicrotasks();
      sockets.last
        ..serverSend(_hello)
        ..serverSend({'type': 'ready', 'new_conversation_ids': []});
      async.flushMicrotasks();
      sockets.last.serverClose(1012); // the server is restarting
      async.flushMicrotasks();
      async.elapse(const Duration(milliseconds: 1300));
      expect(sockets, hasLength(2), reason: 'back to the first step');
      live.stop();
      async.flushMicrotasks();
    });
  });

  test(
    '4001: the credential ended, so the token is refreshed before reconnecting',
    () {
      fakeAsync((async) {
        final live = make()..start();
        async.flushMicrotasks();
        sockets.single.serverClose(4001);
        async.flushMicrotasks();
        expect(refreshes, 1);
        async.elapse(const Duration(seconds: 2));
        expect(dials.last.$2, {'Authorization': 'Bearer churr_app_B'});
        live.stop();
        async.flushMicrotasks();
      });
    },
  );

  test('4001 and a refused refresh: signed out, and no more attempts', () {
    fakeAsync((async) {
      refuseRefresh = true;
      final live = make()..start();
      async.flushMicrotasks();
      sockets.single.serverClose(4001);
      async.flushMicrotasks();
      expect(live.state, LiveState.signedOut);
      async.elapse(const Duration(minutes: 5));
      expect(dials, hasLength(1));
    });
  });

  test('4003: the account was removed; no reconnect', () {
    fakeAsync((async) {
      final live = make()..start();
      async.flushMicrotasks();
      sockets.single.serverClose(4003);
      async.flushMicrotasks();
      expect(live.state, LiveState.removed);
      async.elapse(const Duration(minutes: 5));
      expect(dials, hasLength(1));
    });
  });

  test(
    '4029: the server has too much to do; the same rule on thirty seconds',
    () {
      fakeAsync((async) {
        // The shortest: half of thirty.
        final live = make()..start();
        async.flushMicrotasks();
        sockets.single.serverClose(4029);
        async.flushMicrotasks();
        expect(live.retrySeconds(), 15);
        async.elapse(const Duration(milliseconds: 14900));
        expect(dials, hasLength(1));
        async.elapse(const Duration(milliseconds: 200));
        expect(dials, hasLength(2));
        live.stop();
        async.flushMicrotasks();
      });
      fakeAsync((async) {
        // The longest: thirty and the spread, here the default five.
        sockets = [];
        dials = [];
        final live = make(random: _Fixed(1))..start();
        async.flushMicrotasks();
        sockets.single.serverClose(4029);
        async.flushMicrotasks();
        expect(live.retrySeconds(), 35);
        async.elapse(const Duration(milliseconds: 34900));
        expect(dials, hasLength(1));
        async.elapse(const Duration(milliseconds: 200));
        expect(dials, hasLength(2));
        // And it does not count as a step: the next ordinary wait is the first.
        sockets.last.serverClose(1012);
        async.flushMicrotasks();
        expect(live.retrySeconds(), 6);
        live.stop();
        async.flushMicrotasks();
      });
    },
  );

  test('a server that never says hello is hung up on', () {
    fakeAsync((async) {
      final live = make()..start();
      async.flushMicrotasks();
      async.elapse(const Duration(seconds: 16));
      expect(sockets.first.closed, isTrue);
      live.stop();
      async.flushMicrotasks();
    });
  });

  test('activity is sent at most once a minute, and only when ready', () {
    fakeAsync((async) {
      final live = make()..start();
      async.flushMicrotasks();
      final s = sockets.single;
      live.noteActivity();
      expect(s.sent, isEmpty);
      s
        ..serverSend(_hello)
        ..serverSend({'type': 'ready', 'new_conversation_ids': []});
      async.flushMicrotasks();
      live.noteActivity();
      live.noteActivity();
      expect(s.sentTypes, ['resume', 'activity']);
      live.stop();
      async.flushMicrotasks();
    });
  });

  test('presence.subscribe: never to an older server', () {
    fakeAsync((async) {
      final live = make()..start();
      async.flushMicrotasks();
      final s = sockets.single;
      s
        ..serverSend(_hello)
        ..serverSend({'type': 'ready', 'new_conversation_ids': []});
      async.flushMicrotasks();
      live.watchPresence(['b', 'a']);
      async.elapse(const Duration(seconds: 1));
      expect(s.sentTypes, ['resume']);
      live.stop();
      async.flushMicrotasks();
    });
  });

  test('presence.subscribe: the accounts on screen, once ready, once for a '
      'view still changing, and again on a new connection', () {
    fakeAsync((async) {
      final live = make()..start();
      async.flushMicrotasks();
      final s = sockets.single;
      List<Object?> subs(FakeSocket s) => [
        for (final f in s.sent)
          if (f['type'] == 'presence.subscribe') f['account_ids'],
      ];
      s.serverSend({..._hello, 'presence_subscribe_max': 3});
      live.watchPresence(['c', 'a', 'a', 'b']);
      async.elapse(const Duration(seconds: 1));
      expect(subs(s), isEmpty, reason: 'nothing before ready');
      s.serverSend({'type': 'ready', 'new_conversation_ids': []});
      async.flushMicrotasks();
      expect(subs(s), [
        ['a', 'b', 'c'],
      ]);

      live.watchPresence(['a']);
      live.watchPresence(['a', 'd']);
      live.watchPresence(['d', 'a', 'e']);
      expect(subs(s), hasLength(1), reason: 'a change waits a moment');
      async.elapse(LiveConnection.watchDelay * 2);
      expect(subs(s).last, ['a', 'd', 'e']);
      expect(subs(s), hasLength(2));
      live.watchPresence(['e', 'd', 'a']);
      async.elapse(LiveConnection.watchDelay * 2);
      expect(subs(s), hasLength(2), reason: 'the same set is not sent again');
      live.watchPresence(['a', 'b', 'c', 'd', 'e']);
      async.elapse(LiveConnection.watchDelay * 2);
      expect(
        subs(s).last,
        hasLength(3),
        reason: 'no more than the server takes',
      );
      live.watchPresence([]);
      async.elapse(LiveConnection.watchDelay * 2);
      expect(subs(s).last, isEmpty);

      live.watchPresence(['a']);
      async.elapse(LiveConnection.watchDelay * 2);
      s.serverClose(1012);
      async.elapse(const Duration(seconds: 10));
      final t = sockets.last;
      expect(identical(t, s), isFalse);
      t
        ..serverSend({..._hello, 'presence_subscribe_max': 500})
        ..serverSend({'type': 'ready', 'new_conversation_ids': []});
      async.flushMicrotasks();
      expect(subs(t), [
        ['a'],
      ]);
      live.stop();
      async.flushMicrotasks();
    });
  });
  test(
    'conversations.open: never to an older server, whose resume is plain',
    () {
      fakeAsync((async) {
        final live = make()..start();
        async.flushMicrotasks();
        final s = sockets.single;
        live.openConversations({'c1': 7});
        s
          ..serverSend(_hello)
          ..serverSend({'type': 'ready', 'new_conversation_ids': []});
        async.flushMicrotasks();
        live.openConversations({'c2': 3});
        expect(s.sentTypes, ['resume']);
        expect(s.sent.single.containsKey('open'), isFalse);
        live.stop();
        async.flushMicrotasks();
      });
    },
  );

  test('conversations.open: what is on screen goes in resume, then only '
      'when it changes, with where the cache ends', () {
    fakeAsync((async) {
      final live = make()..start();
      async.flushMicrotasks();
      final s = sockets.single;
      List<Object?> opens(FakeSocket s) => [
        for (final f in s.sent)
          if (f['type'] == 'conversations.open') f['positions'],
      ];
      // Nothing on screen: an empty list still says "this client says".
      s.serverSend({..._hello, 'conversations_open_max': 2});
      async.flushMicrotasks();
      expect(s.sent.single['open'], isEmpty);
      live.openConversations({'c1': 4821});
      expect(opens(s), isEmpty, reason: 'nothing before ready');
      s.serverSend({'type': 'ready', 'new_conversation_ids': []});
      async.flushMicrotasks();
      expect(opens(s), [
        {'c1': 4821},
      ]);
      live.openConversations({'c1': 4900});
      expect(opens(s), hasLength(1), reason: 'the same set is not said again');
      live.openConversations({'c2': 96});
      expect(opens(s).last, {'c2': 96});
      live.openConversations({});
      expect(opens(s).last, isEmpty);
      live.openConversations({'c1': 1, 'c2': 2, 'c3': 3});
      expect((opens(s).last! as Map).keys, [
        'c1',
        'c2',
      ], reason: 'no more than the server takes');

      // A new connection names what is on screen in its resume, and does
      // not repeat it after ready.
      live.openConversations({'c1': 4821});
      s.serverClose(1012);
      async.elapse(const Duration(seconds: 10));
      final t = sockets.last;
      t.serverSend({..._hello, 'conversations_open_max': 8});
      async.flushMicrotasks();
      expect(t.sent.single['open'], ['c1']);
      t.serverSend({'type': 'ready', 'new_conversation_ids': []});
      async.flushMicrotasks();
      expect(opens(t), isEmpty);
      live.openConversations({});
      expect(opens(t), hasLength(1));
      live.stop();
      async.flushMicrotasks();
    });
  });
}
