import 'dart:convert';
import 'dart:io';

import 'package:churr/core/calls/call_layout.dart';
import 'package:churr/core/calls/call_media.dart';
import 'package:churr/core/calls/call_model.dart';
import 'package:churr/core/calls/call_session.dart';
import 'package:fake_async/fake_async.dart';
import 'package:flutter_test/flutter_test.dart';

import 'call_fakes.dart';

class Rig {
  Rig(
    FakeAsync async, {
    bool camera = false,
    bool sealed = false,
    FrameKeys? keys,
    FrameKeys? Function()? keysNow,
  }) {
    api = FakeApi(testCall(sealed: sealed));
    signal.auto = serverLike;
    session = CallSession(
      api: api,
      signal: signal,
      media: media,
      conversationId: convId,
      selfId: me,
      camera: camera,
      frameKeys: keysNow != null
          ? (_) async => keysNow()
          : keys == null
          ? null
          : (_) async => keys,
      now: () => async.getClock(DateTime.utc(2026, 11, 13)).now(),
    );
  }
  final signal = FakeSignal();
  final media = FakeMedia();
  late final FakeApi api;
  late final CallSession session;
}

Map<String, dynamic> ev(String type, Map<String, dynamic> fields) => {
  'type': type,
  'at': '2026-11-13T09:31:00.000Z',
  'conversation_id': convId,
  'call_id': callId,
  ...fields,
};

void main() {
  replayMain();

  test(
    'join: ticket, call.join with the client, publish, answer the '
    'server\'s offers, subscribe (the recorded sequence of the real check)',
    () {
      fakeAsync((async) {
        final r = Rig(async, camera: true);
        r.session.start();
        async.flushMicrotasks();
        expect(r.api.joins, 1);
        final join = r.signal.sent.first;
        expect(join['type'], 'call.join');
        expect(join['call_id'], callId);
        expect(join['ticket'], 'ticket1');
        expect(join['client'], {
          'simulcast': true,
          'video_codecs': ['vp8', 'h264'],
          'frame_encryption': true,
        });
        expect(r.session.phase, CallPhase.connecting);
        final offer = r.signal.last('call.offer');
        expect(offer['pc'], 'send');
        expect(offer['ice_restart'], false);
        expect(offer['tracks'], [
          {'mid': '0', 'source': 'microphone'},
          {'mid': '1', 'source': 'camera'},
        ]);
        expect(r.session.myTracks, {
          TrackSource.microphone: 't0',
          TrackSource.camera: 't1',
        });
        // Recorded from the real check: the others' tracks arrive as an
        // event, then the server's offer on recv.
        r.signal.deliver(
          ev('call.participant_joined', {
            'participant': {
              'account_id': sam,
              'joined_at': '2026-11-13T09:31:02.000Z',
            },
          }),
        );
        r.signal.deliver(
          ev('call.track_published', {
            'account_id': sam,
            'track': {
              'id': 't4',
              'source': 'camera',
              'kind': 'video',
              'muted': false,
              'codec': 'vp8',
              'layers': ['q', 'h', 'f'],
              'width': 1280,
              'height': 720,
            },
          }),
        );
        r.signal.deliver({
          'type': 'call.offer',
          'pc': 'recv',
          'offer_id': 2,
          'sdp': 'v=0 offer',
          'ice_restart': false,
          'tracks': [
            {'mid': '0', 'track_id': 't3', 'account_id': sam},
            {'mid': '1', 'track_id': 't4', 'account_id': sam},
          ],
        });
        async.flushMicrotasks();
        final answer = r.signal.last('call.answer');
        expect(answer['pc'], 'recv');
        expect(answer['offer_id'], 2);
        expect(answer['sdp'], 'v=0 answer');
        expect(r.media.log, contains('recv 2'));
        expect(r.session.call!.participants.map((p) => p.accountId), [me, sam]);

        r.session.setSubscriptions(const [Subscription('t4', 1280, 720)]);
        async.elapse(const Duration(milliseconds: 199));
        expect(
          r.signal.types.where((t) => t == 'call.subscribe').length,
          1,
          reason: 'only the empty list sent on joining',
        );
        async.elapse(const Duration(milliseconds: 2));
        expect(r.signal.last('call.subscribe')['video'], [
          {'track_id': 't4', 'width': 1280, 'height': 720},
        ]);
        async.flushMicrotasks();
        expect(r.session.forwarding['t4']!.layer, 'h');

        r.media.ice(PcRole.send, IceState.connected);
        r.media.ice(PcRole.recv, IceState.connected);
        expect(r.session.phase, CallPhase.live);
      });
    },
  );

  test('subscriptions wait 200 ms after the last change and are not sent '
      'again unchanged', () {
    fakeAsync((async) {
      final r = Rig(async);
      r.session.start();
      async.flushMicrotasks();
      final before = r.signal.types.where((t) => t == 'call.subscribe').length;
      for (var w = 100; w < 600; w += 100) {
        r.session.setSubscriptions([Subscription('t8', w, w)]);
        async.elapse(const Duration(milliseconds: 150));
      }
      expect(r.signal.types.where((t) => t == 'call.subscribe').length, before);
      async.elapse(const Duration(milliseconds: 60));
      expect(
        r.signal.types.where((t) => t == 'call.subscribe').length,
        before + 1,
      );
      expect(r.signal.last('call.subscribe')['video'], [
        {'track_id': 't8', 'width': 500, 'height': 500},
      ]);
      r.session.setSubscriptions(const [Subscription('t8', 500, 500)]);
      async.elapse(const Duration(seconds: 1));
      expect(
        r.signal.types.where((t) => t == 'call.subscribe').length,
        before + 1,
      );
    });
  });

  test(
    'the call moving to another device ends this one without call.leave',
    () {
      fakeAsync((async) {
        final r = Rig(async);
        r.session.start();
        async.flushMicrotasks();
        r.signal.deliver(
          ev('call.participant_left', {
            'account_id': me,
            'reason': 'joined_elsewhere',
          }),
        );
        async.flushMicrotasks();
        expect(r.session.ended, CallEnd.joinedElsewhere);
        expect(r.media.closed, true);
        expect(r.signal.types, isNot(contains('call.leave')));
      });
    },
  );

  test('cameras_full: the camera stays off and the person is told', () {
    fakeAsync((async) {
      final r = Rig(async);
      r.signal.auto = (f) =>
          f['type'] == 'call.offer' && (f['tracks'] as List).length > 1
          ? {'type': 'call.error', 'code': 'cameras_full', 'message': 'full'}
          : serverLike(f);
      r.session.start();
      async.flushMicrotasks();
      r.session.setCamera(true);
      async.flushMicrotasks();
      expect(r.session.cameraOn, false);
      expect(r.session.notice?.code, 'cameras_full');
      expect(r.media.log, contains('reject'));
      expect(r.media.published, {TrackSource.microphone});
    });
  });

  test('screen_in_use names who is sharing; stopping a share is an offer '
      'without the line', () {
    fakeAsync((async) {
      final r = Rig(async);
      r.session.start();
      async.flushMicrotasks();
      r.signal.auto = (f) => f['type'] == 'call.offer'
          ? {
              'type': 'call.error',
              'code': 'screen_in_use',
              'message': '',
              'account_id': sam,
            }
          : serverLike(f);
      r.session.setScreen(true);
      async.flushMicrotasks();
      expect(r.session.screenOn, false);
      expect(r.session.notice?.accountId, sam);
      r.signal.auto = serverLike;
      r.session.setScreen(true);
      async.flushMicrotasks();
      expect(r.session.myTracks[TrackSource.screen], 't2');
      r.session.setScreen(false);
      async.flushMicrotasks();
      expect(r.signal.last('call.offer')['tracks'], [
        {'mid': '0', 'source': 'microphone'},
      ]);
    });
  });

  test('a microphone joined muted is muted on the server after publishing', () {
    fakeAsync((async) {
      final r = Rig(async);
      r.session.micOn = false;
      r.session.start();
      async.flushMicrotasks();
      final mute = r.signal.last('call.mute');
      expect(mute['source'], 'microphone');
      expect(mute['muted'], true);
      expect(r.media.muted, contains(TrackSource.microphone));
    });
  });

  test('ICE disconnected for three seconds restarts both connections; '
      'three failed restarts join again with a new ticket', () {
    fakeAsync((async) {
      final r = Rig(async);
      r.session.start();
      async.flushMicrotasks();
      r.media.ice(PcRole.send, IceState.connected);
      r.media.ice(PcRole.recv, IceState.connected);
      r.media.ice(PcRole.send, IceState.disconnected);
      async.elapse(const Duration(milliseconds: 2900));
      expect(r.signal.types, isNot(contains('call.restart')));
      async.elapse(const Duration(milliseconds: 200));
      expect(r.signal.last('call.offer')['ice_restart'], true);
      expect(r.signal.last('call.restart')['pc'], 'recv');
      expect(r.session.phase, CallPhase.reconnecting);
      // Still down: again after five seconds, twice more, then a rejoin.
      async.elapse(const Duration(seconds: 5));
      async.elapse(const Duration(seconds: 5));
      expect(r.signal.types.where((t) => t == 'call.restart').length, 3);
      expect(r.api.joins, 1);
      async.elapse(const Duration(seconds: 5));
      async.flushMicrotasks();
      expect(r.api.joins, 2);
      expect(r.media.log, contains('close keep'));
      expect(r.signal.last('call.join')['ticket'], 'ticket2');
    });
  });

  test('a restart that works puts the call back to live', () {
    fakeAsync((async) {
      final r = Rig(async);
      r.session.start();
      async.flushMicrotasks();
      r.media.ice(PcRole.send, IceState.connected);
      r.media.ice(PcRole.send, IceState.failed);
      async.flushMicrotasks();
      expect(r.signal.types, contains('call.restart'));
      r.media.ice(PcRole.send, IceState.connected);
      expect(r.session.phase, CallPhase.live);
      async.elapse(const Duration(seconds: 20));
      expect(r.signal.types.where((t) => t == 'call.restart').length, 1);
    });
  });

  test('a new WebSocket sends call.resume and the subscriptions again', () {
    fakeAsync((async) {
      final r = Rig(async);
      r.session.start();
      async.flushMicrotasks();
      r.session.setSubscriptions(const [Subscription('t9', 320, 180)]);
      async.elapse(const Duration(milliseconds: 300));
      r.signal.drop();
      async.flushMicrotasks();
      expect(r.session.signalLost, true);
      r.signal.sent.clear();
      r.signal.reconnect();
      async.flushMicrotasks();
      final resume = r.signal.sent.first;
      expect(resume['type'], 'call.resume');
      expect(resume['resume_token'], 'rt-c1');
      expect(r.session.signalLost, false);
      expect(r.signal.last('call.subscribe')['video'], [
        {'track_id': 't9', 'width': 320, 'height': 180},
      ]);
    });
  });

  test(
    'resume refused and no call on the conversation: the server restarted',
    () {
      fakeAsync((async) {
        final r = Rig(async);
        r.session.start();
        async.flushMicrotasks();
        r.signal.drop();
        r.signal.auto = (f) => f['type'] == 'call.resume'
            ? {'type': 'call.error', 'code': 'ticket_invalid', 'message': ''}
            : serverLike(f);
        r.api.call = null;
        r.signal.reconnect();
        async.flushMicrotasks();
        expect(r.session.ended, CallEnd.serverRestarted);
      });
    },
  );

  test('call.ended ends the session; demand_changed reaches the encoder', () {
    fakeAsync((async) {
      final r = Rig(async, camera: true);
      r.session.start();
      async.flushMicrotasks();
      r.signal.deliver(
        ev('call.demand_changed', {
          'track_id': 't1',
          'layers': ['q'],
        }),
      );
      async.flushMicrotasks();
      expect(r.media.log, contains('demand camera q'));
      r.signal.deliver(
        ev('call.ended', {'reason': 'empty', 'duration_seconds': 3}),
      );
      async.flushMicrotasks();
      expect(r.session.ended, CallEnd.callEnded);
    });
  });

  test('mute, speakers and layers are tracked from events', () {
    fakeAsync((async) {
      final r = Rig(async);
      r.session.start();
      async.flushMicrotasks();
      r.signal.deliver(
        ev('call.track_published', {
          'account_id': sam,
          'track': {
            'id': 't7',
            'source': 'microphone',
            'kind': 'audio',
            'muted': false,
          },
        }),
      );
      r.signal.deliver(
        ev('call.mute_changed', {
          'account_id': sam,
          'track_id': 't7',
          'source': 'microphone',
          'muted': true,
        }),
      );
      r.signal.deliver(
        ev('call.speakers_changed', {
          'account_ids': [sam, me],
        }),
      );
      r.signal.deliver(
        ev('call.layer_changed', {
          'track_id': 't8',
          'state': 'paused',
          'reason': 'bandwidth',
        }),
      );
      final p = r.session.call!.participants.firstWhere(
        (p) => p.accountId == sam,
      );
      expect(p.track(TrackSource.microphone)!.muted, true);
      expect(r.session.speakers, [sam, me]);
      expect(r.session.forwarding['t8']!.state, TrackState.paused);
      // Events for another call are ignored.
      r.signal.deliver({
        ...ev('call.speakers_changed', {'account_ids': []}),
        'call_id': 'other',
      });
      expect(r.session.speakers, [sam, me]);
    });
  });

  test('media_timeout from the server: this network blocks calls', () {
    fakeAsync((async) {
      final r = Rig(async);
      r.session.start();
      async.flushMicrotasks();
      r.signal.deliver({
        'type': 'call.error',
        'code': 'media_timeout',
        'message': '',
      });
      async.flushMicrotasks();
      expect(r.session.ended, CallEnd.networkBlocks);
      expect(r.signal.types, contains('call.leave'));
    });
  });

  test('a sealed call sets frame keys before publishing; without keys it '
      'leaves', () {
    fakeAsync((async) {
      const keys = FrameKeys(
        ownDevice: 'mine',
        keys: {
          'mine': [1],
          'sams': [2],
        },
        keyIndex: 3,
        keyEpoch: 3,
      );
      final r = Rig(async, sealed: true, keys: keys);
      r.session.start();
      async.flushMicrotasks();
      expect(r.media.keys, same(keys));
      expect(r.signal.types, contains('call.offer'));

      final r2 = Rig(async, sealed: true);
      r2.session.start();
      async.flushMicrotasks();
      expect(r2.session.ended, CallEnd.sealedUnavailable);
    });
  });

  test('a new key epoch during a sealed call re-keys it, once', () {
    fakeAsync((async) {
      FrameKeys at(int epoch) => FrameKeys(
        ownDevice: 'mine',
        keys: {
          'mine': [epoch],
          'sams': [epoch + 100],
        },
        keyIndex: epoch % 16,
        keyEpoch: epoch,
      );
      var now = at(3);
      final r = Rig(async, sealed: true, keysNow: () => now);
      r.session.start();
      async.flushMicrotasks();
      expect(r.media.keySets.map((k) => k.keyEpoch), [3]);

      // Something else changed in the sealed chat: the same epoch.
      r.session.refreshKeys();
      async.flushMicrotasks();
      expect(r.media.keySets.map((k) => k.keyEpoch), [3]);

      // sealed.epoch_started was taken in: the keys are of epoch 4 now.
      now = at(4);
      r.session.refreshKeys();
      r.session.refreshKeys();
      async.flushMicrotasks();
      expect(r.media.keySets.map((k) => (k.keyEpoch, k.keyIndex)), [
        (3, 3),
        (4, 4),
      ]);
      expect(r.media.keys!.keys['sams'], [104]);

      // Once the call is over nothing is asked for.
      r.session.leave();
      async.flushMicrotasks();
      now = at(5);
      r.session.refreshKeys();
      async.flushMicrotasks();
      expect(r.media.keySets.length, 2);
    });
  });

  test('an ordinary call never asks for frame keys', () {
    fakeAsync((async) {
      var asked = 0;
      final r = Rig(
        async,
        keysNow: () {
          asked++;
          return null;
        },
      );
      r.session.start();
      async.flushMicrotasks();
      r.session.refreshKeys();
      async.flushMicrotasks();
      expect(asked, 0);
      expect(r.media.keySets, isEmpty);
    });
  });

  test('the device the server names for each track reaches the media side, '
      'and the participant', () {
    fakeAsync((async) {
      final r = Rig(async);
      r.session.start();
      async.flushMicrotasks();
      r.signal.deliver(
        ev('call.participant_joined', {
          'participant': {
            'account_id': sam,
            'device_id': 'sams-phone',
            'joined_at': '2026-11-13T09:31:00.000Z',
          },
        }),
      );
      async.flushMicrotasks();
      CallParticipant samNow() =>
          r.session.call!.participants.firstWhere((p) => p.accountId == sam);
      expect(samNow().deviceId, 'sams-phone');

      r.signal.deliver({
        'type': 'call.offer',
        'pc': 'recv',
        'offer_id': 7,
        'sdp': 'v=0 offer',
        'ice_restart': false,
        'tracks': [
          {
            'mid': '0',
            'track_id': 't1',
            'account_id': sam,
            'device_id': 'sams-phone',
          },
          // A server that names no device for a track.
          {'mid': '1', 'track_id': 't2', 'account_id': sam},
        ],
      });
      async.flushMicrotasks();
      expect(r.media.devices, {'0': 'sams-phone'});

      // The call moved to Sam's laptop: what it publishes says so.
      r.signal.deliver(
        ev('call.track_published', {
          'account_id': sam,
          'device_id': 'sams-laptop',
          'track': {
            'id': 't9',
            'source': 'microphone',
            'kind': 'audio',
            'muted': false,
          },
        }),
      );
      async.flushMicrotasks();
      expect(samNow().deviceId, 'sams-laptop');
      expect(samNow().tracks.map((t) => t.id), ['t9']);
    });
  });

  test('five seconds with no frame decrypted from someone marks them', () {
    fakeAsync((async) {
      final r = Rig(async);
      r.session.start();
      async.flushMicrotasks();
      r.media.onDecrypt!(sam, false);
      async.elapse(const Duration(seconds: 4));
      expect(r.session.undecryptable, isEmpty);
      async.elapse(const Duration(seconds: 2));
      expect(r.session.undecryptable, {sam});
      r.media.onDecrypt!(sam, true);
      expect(r.session.undecryptable, isEmpty);
    });
  });

  test('frames are paced under the server\'s limit, candidates last', () {
    fakeAsync((async) {
      final r = Rig(async);
      r.session.start();
      async.flushMicrotasks();
      r.signal.sent.clear();
      for (var i = 0; i < 30; i++) {
        r.media.onCandidate!(PcRole.send, 'candidate:$i', '0', 0);
      }
      r.session.setMicrophone(false);
      async.flushMicrotasks();
      expect(r.signal.sent.length, lessThanOrEqualTo(15));
      async.elapse(const Duration(seconds: 3));
      expect(r.signal.types.where((t) => t == 'call.candidate').length, 30);
      // The mute went ahead of the candidates still waiting.
      expect(
        r.signal.types.indexOf('call.mute'),
        lessThan(r.signal.types.lastIndexOf('call.candidate')),
      );
    });
  });

  test('leave sends call.leave, or leaveCall when the socket is down', () {
    fakeAsync((async) {
      final r = Rig(async);
      r.session.start();
      async.flushMicrotasks();
      r.session.leave();
      async.flushMicrotasks();
      expect(r.signal.types, contains('call.leave'));
      expect(r.session.ended, CallEnd.left);

      final r2 = Rig(async);
      r2.session.start();
      async.flushMicrotasks();
      r2.signal.up = false;
      r2.session.leave();
      async.flushMicrotasks();
      expect(r2.api.leaves, 1);
    });
  });
}

/// The frames of a real call (integration_test/call_real_test.dart beside
/// tools/callpeer, 2026-11-14), SDPs cut to their media lines.
List<(String, Map<String, dynamic>)> recorded() => [
  for (final l in File('test/core/calls/recorded_call.txt').readAsLinesSync())
    if (l.isNotEmpty)
      (
        l.substring(0, l.indexOf(' ')),
        (jsonDecode(l.substring(l.indexOf(' ') + 1)) as Map)
            .cast<String, dynamic>(),
      ),
];

void replayMain() {
  test('replaying a recorded call: the same requests go out, and the state '
      'follows the server', () {
    fakeAsync((async) {
      final frames = recorded();
      final ins = [
        for (final f in frames)
          if (f.$1 == 'in') f.$2,
      ];
      final outs = [
        for (final f in frames)
          if (f.$1 == 'out') f.$2,
      ];
      final join = outs.first;
      final kai = (ins.first['call'] as Map)['started_by_id'] as String;
      final callIdRec = join['call_id'] as String;
      final convRec = ins.first['conversation_id'] as String;
      Map<String, dynamic> replyTo(String id) =>
          ins.firstWhere((f) => f['reply_to'] == id);
      final outIds = {
        for (final o in outs) o['type'] as String: o['id'] as String,
      };
      final signal = FakeSignal();
      signal.auto = (f) {
        final r = switch (f['type']) {
          'call.join' => replyTo(outIds['call.join']!),
          'call.offer' => replyTo(outIds['call.offer']!),
          'call.subscribe' =>
            (f['video'] as List).isEmpty ? replyTo('c6') : replyTo('c11'),
          'call.candidate' => null,
          _ => <String, dynamic>{'type': 'call.ok'},
        };
        return r == null ? null : ({...r}..remove('reply_to'));
      };
      final media = FakeMedia();
      final api = FakeApi(
        CallInfo.fromJson((ins.first['call'] as Map).cast<String, dynamic>()),
      );
      final s = CallSession(
        api: api,
        signal: signal,
        media: media,
        conversationId: convRec,
        selfId: kai,
        camera: true,
        now: () => async.getClock(DateTime.utc(2026, 11, 14)).now(),
      );
      s.start();
      async.flushMicrotasks();
      expect(s.call!.id, callIdRec);
      expect(s.myTracks, {
        TrackSource.microphone: 't1',
        TrackSource.camera: 't2',
      });
      // The events and offers, in the order they came (replies were
      // answered above).
      // The second recv offer came because of the subscription: the
      // screen subscribes to sam's camera before it.
      for (final f in ins.skip(1)) {
        if (f.containsKey('reply_to')) continue;
        if (f['type'] == 'call.offer' && f['offer_id'] == 2) {
          final sam = s.call!.participants.firstWhere(
            (p) => p.accountId != kai,
          );
          final cam = sam.track(TrackSource.camera)!;
          expect(cam.id, 't4');
          s.setSubscriptions([Subscription(cam.id, 1280, 720)]);
          async.elapse(const Duration(milliseconds: 300));
        }
        signal.deliver(f);
        async.flushMicrotasks();
      }
      expect(signal.types.where((t) => t != 'call.mute'), [
        for (final o in outs) o['type'],
      ]);
      expect(signal.last('call.subscribe')['video'], outs[4]['video']);
      expect(s.forwarding['t4']!.layer, 'f');
      expect(
        media.log,
        containsAllInOrder([
          'offer microphone,camera',
          'answer',
          'recv 1',
          'demand camera f',
          'demand camera h',
          'recv 2',
        ]),
      );
    });
  });
}
