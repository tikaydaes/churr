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

  LiveConnection make() {
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
      random: Random(7),
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
          const Duration(seconds: 2),
        ); // the first backoff is about a second
        expect(sockets, hasLength(2));
        live.stop();
        async.flushMicrotasks();
      });
    },
  );

  test(
    'reconnecting backs off 1, 2, 5, 10, then 30 seconds, with a random part',
    () {
      fakeAsync((async) {
        final live = make();
        for (var i = 0; i < 7; i++) {
          final base = [1, 2, 5, 10, 30, 30, 30][i];
          final d = live.backoff(i).inMilliseconds;
          expect(
            d,
            inInclusiveRange(base * 800, base * 1200),
            reason: 'attempt $i',
          );
        }
        failDial = true;
        live.start();
        async.flushMicrotasks();
        expect(dials, hasLength(1));
        async.elapse(const Duration(milliseconds: 1300));
        expect(dials, hasLength(2));
        async.elapse(const Duration(milliseconds: 2500));
        expect(dials, hasLength(3));
        async.elapse(const Duration(seconds: 2));
        expect(
          dials,
          hasLength(3),
          reason: 'the third wait is about five seconds',
        );
        async.elapse(const Duration(milliseconds: 4500));
        expect(dials, hasLength(4));
        live.stop();
        async.flushMicrotasks();
      });
    },
  );

  test('connectNow skips the wait: the network is back, or the app came to the front', () {
    fakeAsync((async) {
      failDial = true;
      final live = make()..start();
      async.flushMicrotasks();
      async.elapse(const Duration(milliseconds: 1300));
      async.elapse(const Duration(milliseconds: 2500));
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
      expect(sockets, hasLength(2), reason: 'back to a one-second wait');
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

  test('4029: the client sent too much; wait thirty seconds', () {
    fakeAsync((async) {
      final live = make()..start();
      async.flushMicrotasks();
      sockets.single.serverClose(4029);
      async.flushMicrotasks();
      async.elapse(const Duration(seconds: 29));
      expect(dials, hasLength(1));
      async.elapse(const Duration(seconds: 2));
      expect(dials, hasLength(2));
      live.stop();
      async.flushMicrotasks();
    });
  });

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
}
