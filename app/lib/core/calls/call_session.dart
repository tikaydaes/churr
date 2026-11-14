// One device's part in one call: the signalling of docs/CALLS.md sections
// 10 and 11 as a state machine. Plain Dart, no Flutter: the media stack,
// the live connection and the HTTP operations are handed in
// (call_media.dart), so tests drive it with recorded frame sequences.
import 'dart:async';
import 'dart:collection';

import '../models/json.dart';
import 'call_layout.dart';
import 'call_media.dart';
import 'call_model.dart';

enum CallPhase {
  /// Asking for a ticket and sending `call.join`.
  joining,

  /// In the call; media is being connected.
  connecting,

  /// Media is flowing.
  live,

  /// Media stalled (ICE restarting) or the live connection dropped.
  reconnecting,
  ended,
}

/// Why a session ended.
enum CallEnd {
  /// The person left.
  left,

  /// The call moved to another of the person's devices.
  joinedElsewhere,

  /// Removed from the conversation, lost level `member`, or timed out.
  removed,
  signedOut,

  /// Gone for 30 seconds with neither signalling nor media.
  timeout,

  /// The call ended for everyone.
  callEnded,

  /// The call disappeared without `call.ended`: the server restarted.
  serverRestarted,

  /// No media connection could be made ("This network blocks calls").
  networkBlocks,

  /// The server refused to let the person in; [CallSession.endCode] says
  /// why (`call_full`, `forbidden`, `feature_off`, …).
  refused,

  /// A sealed chat's call, and this device cannot take part (no keys, or
  /// frame encryption not available).
  sealedUnavailable,

  /// Something else went wrong; [CallSession.endCode] has a detail.
  failed,
}

/// Something to tell the person that does not end the call.
class CallNotice {
  const CallNotice(this.code, {this.accountId});

  /// `cameras_full`, `screen_in_use`, `declined`, `poor_connection`,
  /// `cannot_decrypt`, or a `call.error` code.
  final String code;
  final String? accountId;
}

/// A request that could not be sent or was not answered because the live
/// connection dropped.
class SignalLost implements Exception {
  const SignalLost();
}

/// A request answered with `call.error`.
class CallRefused implements Exception {
  const CallRefused(this.code, this.message, [this.frame = const {}]);
  final String code;
  final String message;
  final Json frame;
  @override
  String toString() => 'call.error $code: $message';
}

class CallSession {
  CallSession({
    required this.api,
    required this.signal,
    required this.media,
    required this.conversationId,
    required this.selfId,
    bool microphone = true,
    bool camera = false,
    this.frameKeys,
    this.trace,
    this.sendRate = 15,
    DateTime Function()? now,
  }) : _now = now ?? DateTime.now,
       micOn = microphone,
       cameraOn = camera {
    media.onIceState = _onIce;
    media.onCandidate = _onCandidate;
    media.onDecrypt = _onDecrypt;
  }

  final CallApi api;
  final CallSignal signal;
  final CallMedia media;
  final String conversationId;
  final String selfId;

  /// Keys for a sealed chat's call; null when this device has none.
  final Future<FrameKeys?> Function(CallInfo call)? frameKeys;

  /// Sees every frame sent (`out`) and received (`in`), for recording.
  final void Function(String direction, Map<String, Object?> frame)? trace;

  /// Frames a second this client sends at most (the server allows 20).
  final int sendRate;

  final DateTime Function() _now;

  // ---- what the screen reads ----

  CallPhase phase = CallPhase.joining;
  CallInfo? call;
  CallLimits limits = const CallLimits();
  CallEnd? ended;

  /// The `call.error` code or other detail behind [ended].
  String? endCode;
  bool micOn;
  bool cameraOn;
  bool screenOn = false;

  /// Speakers, loudest first.
  List<String> speakers = const [];

  /// What the server forwards of each subscribed track.
  final Map<String, TrackForwarding> forwarding = {};

  /// Accounts whose frames have not decrypted for five seconds.
  final Set<String> undecryptable = {};

  /// Accounts that declined the ring.
  final Set<String> declined = {};

  /// The live connection is gone and the call is held for it.
  bool signalLost = false;
  CallNotice? notice;

  /// This device's track identifiers, by source.
  final Map<TrackSource, String> myTracks = {};

  final _changes = StreamController<void>.broadcast();

  /// Fires after every change to what the screen reads.
  Stream<void> get changes => _changes.stream;

  String? _resumeToken;
  bool get joined => _resumeToken != null;

  // ---- plumbing ----

  final _subs = <StreamSubscription<Object?>>[];
  final _waiting = <String, Completer<Json>>{};
  int _nextId = 0;
  bool _cameraPublished = false;

  /// Publishing is one offer at a time.
  Future<void> _publishing = Future.value();
  bool _publishAgain = false;

  final _ice = <PcRole, IceState>{};
  final _iceTimers = <PcRole, Timer>{};
  int _restarts = 0;
  int _rejoins = 0;
  bool _restarting = false;
  Timer? _restartCheck;

  List<Subscription> _wanted = const [];
  List<Subscription>? _sent;
  Timer? _subscribeTimer;
  bool _background = false;
  bool _cameraBeforeBackground = false;

  final _lastDecrypt = <String, DateTime>{};
  final _decryptTimers = <String, Timer>{};

  // Pacing: at most [sendRate] frames in any second.
  final _queue = Queue<Map<String, Object?>>();
  final _sentAt = Queue<DateTime>();
  Timer? _pacer;

  void _changed() {
    if (!_changes.isClosed) _changes.add(null);
  }

  /// Joins: asks for a ticket (which starts the call when there is none),
  /// sends `call.join`, publishes the microphone (and camera), and from
  /// then on answers the server's offers.
  Future<void> start() async {
    _subs.add(signal.frames.listen(_onFrame));
    _subs.add(signal.connection.listen(_onConnection));
    phase = CallPhase.joining;
    _changed();
    try {
      final (info, ticket) = await api.joinCall(conversationId);
      if (ended != null) return;
      call = info;
      await _join(info, ticket);
    } on CallRefused catch (e) {
      _end(CallEnd.refused, e.code);
    } on SignalLost {
      _end(CallEnd.failed, 'offline');
    } on Object catch (e) {
      // The HTTP error's code when it has one (ApiError.toString).
      _end(CallEnd.refused, _codeOf(e));
    }
  }

  String _codeOf(Object e) {
    try {
      final code = (e as dynamic).code;
      if (code is String) return code;
    } on Object {
      // Not an API error.
    }
    return e.toString();
  }

  Future<void> _join(CallInfo info, String ticket) async {
    if (info.sealed && !media.frameEncryption) {
      _end(CallEnd.sealedUnavailable, 'frame_encryption');
      return;
    }
    final ok = await _request({
      'type': 'call.join',
      'call_id': info.id,
      'ticket': ticket,
      'client': {
        'simulcast': media.simulcast,
        'video_codecs': media.videoCodecs,
        'frame_encryption': media.frameEncryption,
      },
    });
    _took(ok);
    phase = CallPhase.connecting;
    _changed();
    if (info.sealed) {
      final keys = await frameKeys?.call(call!);
      if (keys == null) {
        await leave(reason: CallEnd.sealedUnavailable, code: 'no_keys');
        return;
      }
      _keyEpoch = keys.keyEpoch;
      await media.setFrameKeys(keys);
    }
    _cameraPublished = cameraOn;
    await _publish();
    _flushSubscriptions();
  }

  /// Takes in the answer to `call.join` or `call.resume`.
  void _took(Json ok) {
    _resumeToken = ok['resume_token'] as String? ?? _resumeToken;
    final c = CallInfo.fromJson(object(ok['call']));
    if (c != null) {
      // `call.ok` carries the participants; the rest is as before.
      call = call == null
          ? c
          : call!.withParticipants(
              c.participants.isEmpty ? call!.participants : c.participants,
            );
    }
    limits = CallLimits.fromJson(object(ok['limits']));
    signalLost = false;
  }

  // ---- requests ----

  Future<Json> _request(
    Map<String, Object?> frame, {
    Duration timeout = const Duration(seconds: 10),
  }) async {
    final id = 'c${++_nextId}';
    frame['id'] = id;
    final c = Completer<Json>();
    _waiting[id] = c;
    if (!_send(frame)) {
      _waiting.remove(id);
      throw const SignalLost();
    }
    final Json f;
    try {
      f = await c.future.timeout(timeout);
    } on TimeoutException {
      throw const SignalLost();
    } finally {
      _waiting.remove(id);
    }
    if (f['type'] == 'call.error') {
      throw CallRefused(
        f['code'] as String? ?? 'internal',
        f['message'] as String? ?? '',
        f,
      );
    }
    return f;
  }

  /// Sends now or after the pacer's wait; false when there is no live
  /// connection to send on.
  bool _send(Map<String, Object?> frame, {bool low = false}) {
    if (!signal.connected) return false;
    if (low) {
      _queue.addLast(frame);
    } else {
      // Ahead of waiting candidates, behind other requests.
      final i = _queue.toList().indexWhere(
        (f) => f['type'] == 'call.candidate',
      );
      if (i < 0) {
        _queue.addLast(frame);
      } else {
        final l = _queue.toList()..insert(i, frame);
        _queue
          ..clear()
          ..addAll(l);
      }
    }
    _pump();
    return true;
  }

  void _pump() {
    _pacer?.cancel();
    _pacer = null;
    while (_queue.isNotEmpty) {
      final now = _now();
      while (_sentAt.isNotEmpty &&
          now.difference(_sentAt.first) >= const Duration(seconds: 1)) {
        _sentAt.removeFirst();
      }
      if (_sentAt.length >= sendRate) {
        final wait = const Duration(seconds: 1) - now.difference(_sentAt.first);
        _pacer = Timer(wait, _pump);
        return;
      }
      final f = _queue.removeFirst();
      if (!signal.send(f)) {
        _queue.clear();
        return;
      }
      _sentAt.addLast(now);
      trace?.call('out', f);
    }
  }

  // ---- frames from the server ----

  void _onFrame(Json f) {
    trace?.call('in', f);
    final reply = f['reply_to'];
    if (reply is String && _waiting.containsKey(reply)) {
      _waiting[reply]!.complete(f);
      return;
    }
    final type = f['type'] as String? ?? '';
    if (ended != null) return;
    if (type == 'call.offer') {
      unawaited(_answerRecv(f));
      return;
    }
    if (type == 'call.error') {
      _onServerError(f);
      return;
    }
    final c = call;
    if (c == null) return;
    if (f['conversation_id'] != null &&
        f['conversation_id'] != conversationId) {
      return;
    }
    final callId = f['call_id'] ?? object(f['call'])?['id'];
    if (callId != null && callId != c.id) return;
    switch (type) {
      case 'call.participant_joined':
        final p = CallParticipant.fromJson(object(f['participant']) ?? {});
        final had = c.participants
            .where((x) => x.accountId == p.accountId)
            .firstOrNull;
        call = c.joined(had == null ? p : p.withTracks(had.tracks));
        declined.remove(p.accountId);
      case 'call.participant_left':
        final who = f['account_id'] as String?;
        if (who == selfId) {
          if (!joined) return;
          _end(switch (f['reason']) {
            'joined_elsewhere' => CallEnd.joinedElsewhere,
            'removed' => CallEnd.removed,
            'signed_out' => CallEnd.signedOut,
            'timeout' => CallEnd.timeout,
            _ => CallEnd.left,
          });
          return;
        }
        if (who != null) call = c.left(who);
        speakers = [
          for (final s in speakers)
            if (s != who) s,
        ];
      case 'call.track_published':
        final t = CallTrack.fromJson(object(f['track']) ?? {});
        final who = f['account_id'] as String?;
        if (t == null || who == null) return;
        call = _editTracks(
          c,
          who,
          (ts) => [
            for (final x in ts)
              if (x.id != t.id && x.source != t.source) x,
            t,
          ],
          // The call may have moved to the account's other device.
          deviceId: f['device_id'] as String?,
        );
      case 'call.track_unpublished':
        final id = f['track_id'];
        final who = f['account_id'] as String?;
        if (who == null) return;
        call = _editTracks(
          c,
          who,
          (ts) => [
            for (final x in ts)
              if (x.id != id) x,
          ],
        );
        forwarding.remove(id);
      case 'call.mute_changed':
        final id = f['track_id'];
        final who = f['account_id'] as String?;
        if (who == null) return;
        call = _editTracks(
          c,
          who,
          (ts) => [
            for (final x in ts)
              x.id == id ? x.copyWith(muted: f['muted'] == true) : x,
          ],
        );
      case 'call.speakers_changed':
        speakers = strings(f['account_ids']);
      case 'call.layer_changed':
        final id = f['track_id'];
        if (id is String) {
          forwarding[id] = TrackForwarding(
            TrackState.parse(f['state']),
            f['layer'] as String?,
          );
        }
      case 'call.demand_changed':
        final id = f['track_id'];
        for (final e in myTracks.entries) {
          if (e.value == id) {
            unawaited(media.setDemand(e.key, strings(f['layers'])));
          }
        }
        return;
      case 'call.declined':
        final who = f['account_id'] as String?;
        if (who != null) {
          declined.add(who);
          notice = CallNotice('declined', accountId: who);
        }
      case 'call.ended':
        _end(CallEnd.callEnded, f['reason'] as String?);
        return;
      default:
        return;
    }
    _changed();
  }

  CallInfo _editTracks(
    CallInfo c,
    String accountId,
    List<CallTrack> Function(List<CallTrack>) edit, {
    String? deviceId,
  }) {
    final had = c.participants
        .where((p) => p.accountId == accountId)
        .firstOrNull;
    final p = (had ?? CallParticipant(accountId: accountId)).withTracks(
      edit(had?.tracks ?? const []),
      deviceId: deviceId,
    );
    return c.withParticipants([
      for (final x in c.participants) x.accountId == accountId ? p : x,
      if (had == null) p,
    ]);
  }

  void _onServerError(Json f) {
    final code = f['code'] as String? ?? 'internal';
    if (code == 'media_timeout') {
      // Signalling worked, media never connected: a blocked port.
      unawaited(leave(reason: CallEnd.networkBlocks, code: code));
      return;
    }
    notice = CallNotice(code);
    _changed();
  }

  /// Answers one server offer on `recv`. The server sends one at a time.
  Future<void> _answerRecv(Json f) async {
    final tracks = <String, (String, String)>{};
    // The device each line comes from: its key decrypts a sealed call.
    final devices = <String, String>{};
    for (final t in listOf(f['tracks'], (j) => j)) {
      final mid = t['mid'], id = t['track_id'], who = t['account_id'];
      if (mid is String && id is String && who is String) {
        tracks[mid] = (id, who);
        final device = t['device_id'];
        if (device is String && device.isNotEmpty) devices[mid] = device;
      }
    }
    try {
      final sdp = await media.answerRecv(
        f['sdp'] as String? ?? '',
        tracks,
        devices: devices,
      );
      await _request({
        'type': 'call.answer',
        'pc': 'recv',
        'offer_id': f['offer_id'],
        'sdp': sdp,
      });
    } on SignalLost {
      // The server sends the offer again after `call.resume`.
    } on CallRefused {
      // An answer to an offer the server has replaced; the next comes.
    } on Object catch (e) {
      notice = CallNotice('media', accountId: '$e');
      _changed();
    }
  }

  // ---- publishing ----

  Set<TrackSource> get _sources => {
    TrackSource.microphone,
    if (_cameraPublished) TrackSource.camera,
    if (screenOn) TrackSource.screen,
  };

  /// Sends a new offer on `send` for what should be published now. One
  /// offer is out at a time; a change while one is out makes another.
  Future<void> _publish({bool iceRestart = false}) {
    final next = _publishing.then((_) => _offer(iceRestart));
    _publishing = next.catchError((Object _) {});
    return next;
  }

  Future<void> _offer(bool iceRestart) async {
    if (ended != null) return;
    final before = myTracks.keys.toSet()..add(TrackSource.microphone);
    final want = _sources;
    final offer = await media.offerSend(want, iceRestart: iceRestart);
    Json answer;
    try {
      answer = await _request({
        'type': 'call.offer',
        'pc': 'send',
        'sdp': offer.sdp,
        'ice_restart': iceRestart,
        'tracks': [
          for (final t in offer.tracks) {'mid': t.mid, 'source': t.source.name},
        ],
      });
    } on CallRefused catch (e) {
      await media.rejectSendOffer(before);
      switch (e.code) {
        case 'cameras_full':
          _cameraPublished = false;
          cameraOn = false;
        case 'screen_in_use':
          screenOn = false;
      }
      notice = CallNotice(e.code, accountId: e.frame['account_id'] as String?);
      _changed();
      return;
    } on SignalLost {
      await media.rejectSendOffer(before);
      _publishAgain = true;
      return;
    }
    await media.acceptSendAnswer(answer['sdp'] as String? ?? '');
    final bySource = {for (final t in offer.tracks) t.mid: t.source};
    myTracks.clear();
    for (final t in listOf(answer['tracks'], (j) => j)) {
      final s = bySource[t['mid']];
      if (s != null && t['track_id'] is String) {
        myTracks[s] = t['track_id'] as String;
      }
    }
    // A track starts unmuted on the server.
    if (!micOn && myTracks.containsKey(TrackSource.microphone)) {
      await media.setMuted(TrackSource.microphone, true);
      unawaited(_mute(TrackSource.microphone, true));
    }
    if (_cameraPublished && !cameraOn) {
      unawaited(_mute(TrackSource.camera, true));
    }
    _changed();
  }

  Future<void> _mute(TrackSource s, bool muted) async {
    try {
      await _request({'type': 'call.mute', 'source': s.name, 'muted': muted});
    } on CallRefused catch (e) {
      if (e.code == 'cameras_full' && s == TrackSource.camera) {
        cameraOn = false;
        await media.setMuted(TrackSource.camera, true);
        notice = CallNotice(e.code);
        _changed();
      }
    } on SignalLost {
      // Sent again with the state after `call.resume`.
    }
  }

  /// Turns the microphone on or off. A muted microphone sends nothing.
  Future<void> setMicrophone(bool on) async {
    micOn = on;
    _changed();
    await media.setMuted(TrackSource.microphone, !on);
    if (myTracks.containsKey(TrackSource.microphone)) {
      await _mute(TrackSource.microphone, !on);
    }
  }

  /// Turns the camera on or off. The first time it is turned on it is
  /// published; after that, off is a mute.
  Future<void> setCamera(bool on) async {
    cameraOn = on;
    _changed();
    if (!joined) return;
    if (on && !_cameraPublished) {
      _cameraPublished = true;
      await _publish();
      return;
    }
    await media.setMuted(TrackSource.camera, !on);
    if (myTracks.containsKey(TrackSource.camera)) {
      await _mute(TrackSource.camera, !on);
    }
  }

  /// Starts or stops sharing a screen. The media layer has already been
  /// told which screen or window ([CallMedia.offerSend] captures it).
  /// Stopping is an unpublish: a new offer without that line.
  Future<void> setScreen(bool on) async {
    if (screenOn == on) return;
    screenOn = on;
    _changed();
    if (joined) await _publish();
  }

  // ---- subscriptions ----

  /// The tiles on screen changed. The list is sent 200 milliseconds after
  /// the last change (docs/CALLS.md section 7).
  void setSubscriptions(List<Subscription> list) {
    final max = limits.maxSubscriptions;
    _wanted = list.length > max ? list.sublist(0, max) : list;
    _subscribeTimer?.cancel();
    _subscribeTimer = Timer(
      const Duration(milliseconds: 200),
      _flushSubscriptions,
    );
  }

  void _flushSubscriptions() {
    _subscribeTimer?.cancel();
    if (!joined || ended != null) return;
    final list = _background ? const <Subscription>[] : _wanted;
    if (_sent != null && _same(_sent!, list)) return;
    _sent = list;
    unawaited(_subscribe(list));
  }

  static bool _same(List<Subscription> a, List<Subscription> b) {
    if (a.length != b.length) return false;
    for (var i = 0; i < a.length; i++) {
      if (a[i] != b[i]) return false;
    }
    return true;
  }

  Future<void> _subscribe(List<Subscription> list) async {
    try {
      final ok = await _request({
        'type': 'call.subscribe',
        'video': [for (final s in list) s.toJson()],
      });
      for (final v in listOf(ok['video'], (j) => j)) {
        final id = v['track_id'];
        if (id is String) {
          forwarding[id] = TrackForwarding(
            TrackState.parse(v['state']),
            v['layer'] as String?,
          );
        }
      }
      _changed();
    } on SignalLost {
      _sent = null; // sent again after `call.resume`
    } on CallRefused catch (e) {
      notice = CallNotice(e.code);
      _changed();
    }
  }

  /// The app went to the background on a phone, or came back: no video is
  /// subscribed and the camera is muted while away (docs/CALLS.md section
  /// 11).
  Future<void> setBackground(bool away) async {
    if (_background == away) return;
    _background = away;
    if (away) {
      _cameraBeforeBackground = cameraOn;
      if (cameraOn) await setCamera(false);
    } else if (_cameraBeforeBackground) {
      await setCamera(true);
    }
    _flushSubscriptions();
  }

  // ---- ICE ----

  void _onCandidate(PcRole pc, String? cand, String? mid, int? index) {
    if (!joined) return;
    _send({
      'type': 'call.candidate',
      'id': 'c${++_nextId}',
      'pc': pc.name,
      'candidate': cand,
      'sdp_mid': mid,
      'sdp_mline_index': index,
    }, low: true);
  }

  void _onIce(PcRole pc, IceState s) {
    _ice[pc] = s;
    if (ended != null) return;
    if (s.up) {
      _iceTimers.remove(pc)?.cancel();
      final send = _ice[PcRole.send]?.up ?? false;
      final recv = _ice[PcRole.recv];
      if (send && (recv == null || recv.up)) {
        if (_restarting) {
          _restarting = false;
          _restartCheck?.cancel();
        }
        _restarts = 0;
        if (phase != CallPhase.live) {
          phase = CallPhase.live;
          _changed();
        }
      }
    } else if (s == IceState.disconnected) {
      _iceTimers[pc] ??= Timer(const Duration(seconds: 3), _restartIce);
    } else if (s == IceState.failed) {
      _iceTimers.remove(pc)?.cancel();
      _restartIce();
    }
  }

  /// Restarts ICE on both connections (docs/CALLS.md section 11): up to
  /// three times, five seconds apart, then joins again with a new ticket.
  void _restartIce() {
    if (ended != null || _restarting || !joined) return;
    for (final t in _iceTimers.values) {
      t.cancel();
    }
    _iceTimers.clear();
    if (_restarts >= 3) {
      unawaited(_rejoin());
      return;
    }
    _restarts++;
    _restarting = true;
    phase = CallPhase.reconnecting;
    _changed();
    unawaited(_publish(iceRestart: true));
    unawaited(
      _request({'type': 'call.restart', 'pc': 'recv'})
          .catchError((Object _) => <String, dynamic>{}),
    );
    _restartCheck?.cancel();
    _restartCheck = Timer(const Duration(seconds: 5), () {
      _restarting = false;
      final up =
          (_ice[PcRole.send]?.up ?? false) && (_ice[PcRole.recv]?.up ?? true);
      if (!up) _restartIce();
    });
  }

  /// Tears both connections down and joins again with a new ticket.
  Future<void> _rejoin() async {
    if (ended != null) return;
    _rejoins++;
    if (_rejoins > 2) {
      await leave(reason: CallEnd.networkBlocks, code: 'ice_failed');
      return;
    }
    _restarts = 0;
    _restarting = false;
    _restartCheck?.cancel();
    phase = CallPhase.reconnecting;
    _changed();
    _ice.clear();
    myTracks.clear();
    _sent = null;
    await media.close(keepCapture: true);
    try {
      final (info, ticket) = await api.joinCall(conversationId);
      if (ended != null) return;
      if (info.id != call?.id) {
        // The call this device was in has gone.
        _end(CallEnd.callEnded);
        return;
      }
      await _join(info, ticket);
    } on CallRefused catch (e) {
      _end(CallEnd.refused, e.code);
    } on Object catch (e) {
      _end(CallEnd.failed, _codeOf(e));
    }
  }

  // ---- the live connection ----

  void _onConnection(bool up) {
    if (ended != null) return;
    if (!up) {
      for (final c in _waiting.values) {
        if (!c.isCompleted) c.completeError(const SignalLost());
      }
      _waiting.clear();
      _queue.clear();
      if (joined) {
        signalLost = true;
        _changed();
      }
      return;
    }
    if (joined) unawaited(_resume());
  }

  /// Takes the call up again on a new live connection.
  Future<void> _resume() async {
    final c = call;
    if (c == null || _resumeToken == null) return;
    try {
      final ok = await _request({
        'type': 'call.resume',
        'call_id': c.id,
        'resume_token': _resumeToken,
      });
      _took(ok);
      _changed();
      _sent = null;
      _flushSubscriptions();
      if (micOn == false && myTracks.containsKey(TrackSource.microphone)) {
        unawaited(_mute(TrackSource.microphone, true));
      }
      if (_publishAgain) {
        _publishAgain = false;
        unawaited(_publish());
      }
    } on SignalLost {
      // Tried again on the next connection.
    } on CallRefused catch (e) {
      if (e.code != 'ticket_invalid') {
        _end(CallEnd.refused, e.code);
        return;
      }
      // Held for too long, or the call is gone.
      CallInfo? now;
      try {
        now = await api.getCall(conversationId);
      } on Object {
        now = null;
      }
      if (now == null || now.id != c.id) {
        _end(CallEnd.serverRestarted);
      } else {
        _rejoins = 0;
        await _rejoin();
      }
    }
  }

  // ---- sealed calls ----

  void _onDecrypt(String accountId, bool ok) {
    if (ok) {
      _lastDecrypt[accountId] = _now();
      _decryptTimers.remove(accountId)?.cancel();
      if (undecryptable.remove(accountId)) _changed();
      return;
    }
    _decryptTimers[accountId] ??= Timer(const Duration(seconds: 5), () {
      _decryptTimers.remove(accountId);
      final last = _lastDecrypt[accountId];
      if (last == null ||
          _now().difference(last) >= const Duration(seconds: 5)) {
        undecryptable.add(accountId);
        notice = CallNotice('cannot_decrypt', accountId: accountId);
        _changed();
      }
    });
  }

  /// The key epoch the call's frame keys are from.
  int? _keyEpoch;

  /// Something changed in the sealed chat. If it is in a new key epoch,
  /// the call derives and uses the new keys: senders switch at once and
  /// receivers keep the old keys for ten seconds (docs/SEALED.md section
  /// 10). Safe to call on every change: the same epoch does nothing.
  Future<void> refreshKeys() async {
    final c = call;
    if (c == null || !c.sealed || ended != null || _keyEpoch == null) return;
    final keys = await frameKeys?.call(c);
    if (keys == null || keys.keyEpoch == _keyEpoch) return;
    if (call == null || ended != null) return;
    _keyEpoch = keys.keyEpoch;
    await media.setFrameKeys(keys);
  }

  // ---- leaving ----

  /// Leaves the call: `call.leave` on the live connection, or `leaveCall`
  /// over HTTP when it is down.
  Future<void> leave({CallEnd reason = CallEnd.left, String? code}) async {
    if (ended != null) return;
    final wasIn = joined;
    final c = call;
    if (wasIn && c != null) {
      try {
        await _request({
          'type': 'call.leave',
          'call_id': c.id,
        }, timeout: const Duration(seconds: 3));
      } on Object {
        try {
          await api.leaveCall(conversationId);
        } on Object {
          // The server drops the participant after 30 seconds anyway.
        }
      }
    }
    _end(reason, code);
  }

  void _end(CallEnd reason, [String? code]) {
    if (ended != null) return;
    ended = reason;
    endCode = code;
    phase = CallPhase.ended;
    _resumeToken = null;
    _subscribeTimer?.cancel();
    _restartCheck?.cancel();
    _pacer?.cancel();
    for (final t in [..._iceTimers.values, ..._decryptTimers.values]) {
      t.cancel();
    }
    for (final w in _waiting.values) {
      if (!w.isCompleted) w.completeError(const SignalLost());
    }
    _waiting.clear();
    unawaited(media.close());
    _changed();
  }

  /// Stops listening. Call after [ended] is set, or to drop a session.
  Future<void> dispose() async {
    if (ended == null) _end(CallEnd.left);
    for (final s in _subs) {
      await s.cancel();
    }
    await _changes.close();
  }
}
