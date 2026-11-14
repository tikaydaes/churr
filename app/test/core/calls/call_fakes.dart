import 'dart:async';

import 'package:churr/core/calls/call_media.dart';
import 'package:churr/core/calls/call_model.dart';

/// The live connection, with the frames sent kept in [sent].
class FakeSignal implements CallSignal {
  final sent = <Map<String, Object?>>[];
  final _frames = StreamController<Map<String, dynamic>>.broadcast(sync: true);
  final _conn = StreamController<bool>.broadcast(sync: true);
  bool up = true;

  /// Answers each request as it is sent, if set.
  Map<String, dynamic>? Function(Map<String, Object?> frame)? auto;

  @override
  bool send(Map<String, Object?> frame) {
    if (!up) return false;
    sent.add(frame);
    final a = auto?.call(frame);
    if (a != null) {
      scheduleMicrotask(() => deliver({...a, 'reply_to': frame['id']}));
    }
    return true;
  }

  void deliver(Map<String, dynamic> f) => _frames.add(f);

  void drop() {
    up = false;
    _conn.add(false);
  }

  void reconnect() {
    up = true;
    _conn.add(true);
  }

  @override
  Stream<Map<String, dynamic>> get frames => _frames.stream;
  @override
  Stream<bool> get connection => _conn.stream;
  @override
  bool get connected => up;

  List<String> get types => [for (final f in sent) f['type'] as String];
  Map<String, Object?> last(String type) =>
      sent.lastWhere((f) => f['type'] == type);
}

class FakeMedia extends CallMedia {
  final log = <String>[];
  final muted = <TrackSource>{};
  Set<TrackSource> published = {};
  FrameKeys? keys;

  /// Every set of keys given, in order.
  final keySets = <FrameKeys>[];

  /// The device of each recv line in the last offer answered.
  Map<String, String> devices = {};
  bool closed = false;
  bool encrypts = true;

  @override
  bool get frameEncryption => encrypts;
  @override
  bool get simulcast => true;
  @override
  List<String> get videoCodecs => const ['vp8', 'h264'];

  @override
  Future<LocalOffer> offerSend(
    Set<TrackSource> sources, {
    bool iceRestart = false,
  }) async {
    log.add(
      'offer ${sources.map((s) => s.name).join(',')}${iceRestart ? ' restart' : ''}',
    );
    published = {...sources};
    final order = [
      TrackSource.microphone,
      TrackSource.camera,
      TrackSource.screen,
    ];
    return LocalOffer('v=0 fake', [
      for (final s in order)
        if (sources.contains(s)) OfferedTrack('${order.indexOf(s)}', s),
    ]);
  }

  @override
  Future<void> acceptSendAnswer(String sdp) async => log.add('answer');

  @override
  Future<void> rejectSendOffer(Set<TrackSource> keep) async {
    log.add('reject');
    published = published.intersection(keep);
  }

  @override
  Future<String> answerRecv(
    String sdp,
    Map<String, (String, String)> tracks, {
    Map<String, String> devices = const {},
  }) async {
    this.devices = devices;
    log.add('recv ${tracks.length}');
    return 'v=0 answer';
  }

  @override
  Future<void> setMuted(TrackSource source, bool m) async {
    m ? muted.add(source) : muted.remove(source);
  }

  @override
  Future<void> setDemand(TrackSource source, List<String> layers) async =>
      log.add('demand ${source.name} ${layers.join(',')}');

  @override
  Future<void> setFrameKeys(FrameKeys k) async {
    keys = k;
    keySets.add(k);
  }

  @override
  Future<void> close({bool keepCapture = false}) async {
    log.add('close${keepCapture ? ' keep' : ''}');
    closed = !keepCapture;
  }

  void ice(PcRole pc, IceState s) => onIceState?.call(pc, s);
}

class FakeApi implements CallApi {
  FakeApi(this.call);
  CallInfo? call;
  int joins = 0;
  int leaves = 0;

  @override
  Future<(CallInfo, String)> joinCall(String conversationId) async {
    joins++;
    return (call!, 'ticket$joins');
  }

  @override
  Future<CallInfo?> getCall(String conversationId) async => call;

  @override
  Future<void> leaveCall(String conversationId) async => leaves++;
}

const convId = '0193a6f0-1111-7aaa-8bbb-000000000001';
const callId = '0193b100-2c1e-7b3a-9f0e-2a4d5c6b7e8f';
const me = '0193a000-0000-7000-8000-000000000001';
const sam = '0193a000-0000-7000-8000-000000000002';

CallInfo testCall({bool sealed = false}) => CallInfo(
  id: callId,
  conversationId: convId,
  startedById: me,
  sealed: sealed,
  participants: const [],
);

/// Answers the usual requests the way the server does.
Map<String, dynamic>? serverLike(Map<String, Object?> f) => switch (f['type']) {
  'call.join' || 'call.resume' => {
    'type': 'call.ok',
    'resume_token': 'rt-${f['id']}',
    'call': {
      'id': callId,
      'conversation_id': convId,
      'started_by_id': me,
      'sealed': false,
      'participants': [
        {'account_id': me, 'joined_at': '2026-11-13T09:30:00.412Z'},
      ],
    },
    'limits': {
      'max_participants': 50,
      'max_cameras': 12,
      'max_subscriptions': 16,
    },
  },
  'call.offer' => {
    'type': 'call.answer',
    'pc': 'send',
    'sdp': 'v=0 server',
    'tracks': [
      for (final t in (f['tracks'] as List).cast<Map<String, Object?>>())
        {'mid': t['mid'], 'track_id': 't${t['mid']}'},
    ],
  },
  'call.subscribe' => {
    'type': 'call.ok',
    'video': [
      for (final v in (f['video'] as List).cast<Map<String, Object>>())
        {'track_id': v['track_id'], 'state': 'forwarding', 'layer': 'h'},
    ],
  },
  'call.candidate' => null,
  _ => {'type': 'call.ok'},
};
