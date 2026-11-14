// What the signalling needs from a WebRTC stack. The app's implementation
// is lib/platform/calls/webrtc_media.dart (flutter_webrtc); tests use a
// fake. Plain Dart, no Flutter.
import 'call_model.dart';

/// The two peer connections of docs/CALLS.md section 4.
enum PcRole { send, recv }

/// The ICE connection state of one peer connection.
enum IceState {
  fresh,
  checking,
  connected,
  completed,
  disconnected,
  failed,
  closed,
}

extension IceStateUp on IceState {
  bool get up => this == IceState.connected || this == IceState.completed;
}

/// One media line of an offer on `send`.
class OfferedTrack {
  const OfferedTrack(this.mid, this.source, {this.width = 0, this.height = 0});
  final String mid;
  final TrackSource source;
  final int width;
  final int height;
}

/// A local offer on `send`, already set as the local description.
class LocalOffer {
  const LocalOffer(this.sdp, this.tracks);
  final String sdp;

  /// Every media line that is sending.
  final List<OfferedTrack> tracks;
}

/// Frame keys for a sealed chat's call in one key epoch (docs/SEALED.md
/// section 10, docs/CALL-FRAMES.md section 1): the call sender key of
/// every device of the epoch, this device's among them, and the key index
/// every frame carries.
class FrameKeys {
  const FrameKeys({
    required this.ownDevice,
    required this.keys,
    required this.keyIndex,
    this.keyEpoch = 0,
    this.soleDevice = const {},
  });

  /// This device's identifier. Its frames go out under `keys[ownDevice]`.
  final String ownDevice;

  /// The call sender key of each device of the epoch, by device id. The
  /// server names the device every track comes from (`device_id` in
  /// `call.offer`), and that device's key decrypts it.
  final Map<String, List<int>> keys;
  final int keyIndex;

  /// The epoch the keys belong to; a call re-keys when it changes.
  final int keyEpoch;

  /// For each account with exactly one device in the epoch, that device:
  /// used only when the server names no device for a track (a server
  /// older than `device_id`).
  final Map<String, String> soleDevice;

  List<int>? get ownKey => keys[ownDevice];
}

/// The media side of one participant: its two peer connections, the local
/// capture, and the remote tracks.
abstract class CallMedia {
  /// Called by the media layer.
  void Function(PcRole pc, IceState state)? onIceState;
  void Function(
    PcRole pc,
    String? candidate,
    String? sdpMid,
    int? sdpMLineIndex,
  )?
  onCandidate;

  /// A frame of a sealed call could not be decrypted ([ok] false) or
  /// decrypted again ([ok] true), per account.
  void Function(String accountId, bool ok)? onDecrypt;

  /// Whether this stack can encrypt frames as a sealed chat's call needs.
  bool get frameEncryption;

  /// Whether the camera can be sent in several sizes.
  bool get simulcast;

  /// Video codecs this stack decodes, for `call.join`.
  List<String> get videoCodecs;

  /// Makes the `send` connection (if needed) carry exactly [sources] and
  /// returns its new offer, already set as the local description. [fresh]
  /// sources are captured now.
  Future<LocalOffer> offerSend(Set<TrackSource> sources, {bool iceRestart});

  /// Applies the server's answer to the last offer.
  Future<void> acceptSendAnswer(String sdp);

  /// The server refused the last offer: go back to the state before it,
  /// dropping sources added for it.
  Future<void> rejectSendOffer(Set<TrackSource> keep);

  /// Applies a server offer on `recv` and returns the answer. [tracks]
  /// maps each media line to the track and account it carries, and
  /// [devices] to the device it comes from where the server said: in a
  /// sealed chat's call that device's key decrypts the line.
  Future<String> answerRecv(
    String sdp,
    Map<String, (String, String)> tracks, {
    Map<String, String> devices = const {},
  });

  /// Mutes or unmutes a published source. A muted camera stops capturing.
  Future<void> setMuted(TrackSource source, bool muted);

  /// The simulcast layers anyone receives (`call.demand_changed`); the
  /// others stop being encoded.
  Future<void> setDemand(TrackSource source, List<String> layers);

  /// Starts frame encryption with these keys, or changes them: a new key
  /// epoch began. Senders switch to the new key index at once; receivers
  /// keep the previous keys for ten seconds.
  Future<void> setFrameKeys(FrameKeys keys);

  /// Closes both connections. The local capture stays if [keepCapture]
  /// (a rejoin).
  Future<void> close({bool keepCapture = false});
}

/// The live connection, as a call needs it.
abstract class CallSignal {
  /// Sends a frame; false when there is no open connection.
  bool send(Map<String, Object?> frame);

  /// The `call.` frames that arrive, in order.
  Stream<Map<String, dynamic>> get frames;

  /// True each time a connection reaches `ready`, false when it drops.
  Stream<bool> get connection;

  bool get connected;
}

/// The three HTTP operations (`joinCall`, `getCall`, `leaveCall`).
abstract class CallApi {
  Future<(CallInfo, String)> joinCall(String conversationId);
  Future<CallInfo?> getCall(String conversationId);
  Future<void> leaveCall(String conversationId);
}
