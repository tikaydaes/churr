// The call objects of docs/CALLS.md section 10 ("The call object") and
// docs/openapi.yaml (`Call`, `CallParticipant`, `CallTrack`). Plain Dart,
// no Flutter, so the signalling and its tests can use them.
import '../models/json.dart';

/// What a track carries.
enum TrackSource {
  microphone,
  camera,
  screen;

  static TrackSource? parse(Object? v) => switch (v) {
    'microphone' => microphone,
    'camera' => camera,
    'screen' => screen,
    _ => null,
  };

  bool get isVideo => this != microphone;
}

/// One media track a participant is sending (`CallTrack`).
class CallTrack {
  const CallTrack({
    required this.id,
    required this.source,
    this.muted = false,
    this.codec,
    this.layers = const [],
    this.width = 0,
    this.height = 0,
  });

  /// Null when the source is one this build does not know.
  static CallTrack? fromJson(Json j) {
    final source = TrackSource.parse(j['source']);
    final id = j['id'];
    if (source == null || id is! String) return null;
    return CallTrack(
      id: id,
      source: source,
      muted: j['muted'] == true,
      codec: j['codec'] as String?,
      layers: strings(j['layers']),
      width: asInt(j['width']),
      height: asInt(j['height']),
    );
  }

  final String id;
  final TrackSource source;
  final bool muted;
  final String? codec;

  /// The simulcast layers (`q`, `h`, `f`) the publisher sends; empty for
  /// audio and for a track sent in one size.
  final List<String> layers;
  final int width;
  final int height;

  CallTrack copyWith({bool? muted}) => CallTrack(
    id: id,
    source: source,
    muted: muted ?? this.muted,
    codec: codec,
    layers: layers,
    width: width,
    height: height,
  );
}

/// An account in a call (`CallParticipant`). [tracks] is filled only in
/// copies sent to participants.
class CallParticipant {
  const CallParticipant({
    required this.accountId,
    this.deviceId,
    this.joinedAt,
    this.tracks = const [],
  });

  factory CallParticipant.fromJson(Json j) => CallParticipant(
    accountId: j['account_id'] as String? ?? '',
    deviceId: j['device_id'] as String?,
    joinedAt: parseTime(j['joined_at']),
    tracks: [for (final t in listOf(j['tracks'], CallTrack.fromJson)) ?t],
  );

  final String accountId;

  /// The device the account is in the call on, when the server says. It
  /// changes if the call moves to the account's other device.
  final String? deviceId;
  final DateTime? joinedAt;
  final List<CallTrack> tracks;

  CallTrack? track(TrackSource s) {
    for (final t in tracks) {
      if (t.source == s) return t;
    }
    return null;
  }

  CallParticipant withTracks(List<CallTrack> tracks, {String? deviceId}) =>
      CallParticipant(
        accountId: accountId,
        deviceId: deviceId ?? this.deviceId,
        joinedAt: joinedAt,
        tracks: tracks,
      );
}

/// A call in a conversation (`Call`).
class CallInfo {
  const CallInfo({
    required this.id,
    required this.conversationId,
    this.messageId,
    this.startedById,
    this.startedAt,
    this.sealed = false,
    this.hostedBy,
    this.participants = const [],
  });

  static CallInfo? fromJson(Json? j) {
    if (j == null || j['id'] is! String) return null;
    return CallInfo(
      id: j['id'] as String,
      conversationId: j['conversation_id'] as String? ?? '',
      messageId: j['message_id'] as String?,
      startedById: j['started_by_id'] as String?,
      startedAt: parseTime(j['started_at']),
      sealed: j['sealed'] == true,
      hostedBy: j['hosted_by'] as String?,
      participants: listOf(j['participants'], CallParticipant.fromJson),
    );
  }

  final String id;
  final String conversationId;
  final String? messageId;
  final String? startedById;
  final DateTime? startedAt;
  final bool sealed;

  /// The server carrying the media when that is not this one.
  final String? hostedBy;
  final List<CallParticipant> participants;

  bool has(String accountId) =>
      participants.any((p) => p.accountId == accountId);

  CallInfo withParticipants(List<CallParticipant> participants) => CallInfo(
    id: id,
    conversationId: conversationId,
    messageId: messageId,
    startedById: startedById,
    startedAt: startedAt,
    sealed: sealed,
    hostedBy: hostedBy,
    participants: participants,
  );

  /// The call after `call.participant_joined`.
  CallInfo joined(CallParticipant p) => withParticipants([
    for (final x in participants)
      if (x.accountId != p.accountId) x,
    p,
  ]);

  /// The call after `call.participant_left`.
  CallInfo left(String accountId) => withParticipants([
    for (final x in participants)
      if (x.accountId != accountId) x,
  ]);
}

/// The `limits` of `call.ok`.
class CallLimits {
  const CallLimits({
    this.maxParticipants = 50,
    this.maxCameras = 12,
    this.maxSubscriptions = 16,
  });

  factory CallLimits.fromJson(Json? j) => CallLimits(
    maxParticipants: asInt(j?['max_participants'], 50),
    maxCameras: asInt(j?['max_cameras'], 12),
    maxSubscriptions: asInt(j?['max_subscriptions'], 16),
  );

  final int maxParticipants;
  final int maxCameras;
  final int maxSubscriptions;
}

/// What the server says about one subscribed track (`call.subscribe`'s
/// answer and `call.layer_changed`).
enum TrackState {
  forwarding,
  paused,
  muted,
  unavailable;

  static TrackState parse(Object? v) => switch (v) {
    'forwarding' => forwarding,
    'paused' => paused,
    'muted' => muted,
    _ => unavailable,
  };
}

class TrackForwarding {
  const TrackForwarding(this.state, this.layer);
  final TrackState state;
  final String? layer;
}
