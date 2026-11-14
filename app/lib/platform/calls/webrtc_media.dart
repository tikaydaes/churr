// The media side of a call on flutter_webrtc (README.md, "Calls"): the
// `send` and `recv` peer connections of docs/CALLS.md section 4, the
// microphone, camera (in simulcast layers) and screen, the remote tracks,
// and frame encryption for a sealed chat's call (docs/CALL-FRAMES.md).
import 'dart:async';
import 'dart:io' show Platform;
import 'dart:math' show Random;

import 'package:flutter/foundation.dart';
import 'package:flutter_webrtc/flutter_webrtc.dart';

import '../../core/calls/call_layout.dart';
import '../../core/calls/call_media.dart';
import '../../core/calls/call_model.dart';
import '../../core/calls/frame_crypto.dart';
import '../../core/calls/sdp.dart';

bool get _desktop =>
    !kIsWeb && (Platform.isMacOS || Platform.isWindows || Platform.isLinux);

/// One remote track as the screen draws it.
class RemoteMedia {
  RemoteMedia(this.mid, this.track, this.stream);
  final String mid;
  final MediaStreamTrack track;
  final MediaStream stream;
  String? trackId;
  String? accountId;

  /// The device the line comes from, where the server said.
  String? deviceId;
}

/// The frame cryptor on one recv line, and whose keys it uses.
class _LineCryptor {
  _LineCryptor(this.cryptor, this.participant);
  final FrameCryptor cryptor;
  final String participant;
}

class WebRtcMedia extends CallMedia with ChangeNotifier {
  WebRtcMedia({
    required this.selfId,
    this.cameraDeviceId,
    this.microphoneDeviceId,
  });

  /// The account. (In the key provider this device's frames go by its
  /// device id, which comes with the keys.)
  final String selfId;
  String? cameraDeviceId;
  String? microphoneDeviceId;

  /// The screen or window to share next (from the desktop picker).
  String? screenSourceId;

  RTCPeerConnection? _send, _recv;
  MediaStream? microphone, camera, screen;
  final _senders = <TrackSource, RTCRtpSender>{};
  final _transceivers = <TrackSource, RTCRtpTransceiver>{};
  final _muted = <TrackSource>{};

  /// Remote tracks by media line of `recv`.
  final remote = <String, RemoteMedia>{};
  Map<String, (String, String)> _lines = {};
  Map<String, String> _lineDevices = {};

  /// The remote media for a track identifier.
  RemoteMedia? remoteTrack(String trackId) {
    for (final r in remote.values) {
      if (r.trackId == trackId) return r;
    }
    return null;
  }

  // Frame encryption.
  KeyProvider? _keys;
  FrameKeys? _frameKeys;
  final _sendCryptors = <FrameCryptor>[];
  final _recvCryptors = <String, _LineCryptor>{};
  final _receivers = <String, RTCRtpReceiver>{};
  final _forgetTimers = <Timer>[];

  @override
  bool get frameEncryption => true;

  @override
  bool get simulcast => true;

  @override
  List<String> get videoCodecs => const ['vp8', 'h264'];

  static const _config = <String, dynamic>{
    'iceServers': <Object>[],
    'sdpSemantics': 'unified-plan',
    'bundlePolicy': 'max-bundle',
    'rtcpMuxPolicy': 'require',
  };

  Future<RTCPeerConnection> _make(PcRole role) async {
    final pc = await createPeerConnection(_config);
    pc.onIceConnectionState = (s) => onIceState?.call(role, _ice(s));
    pc.onIceCandidate = (c) =>
        onCandidate?.call(role, c.candidate, c.sdpMid, c.sdpMLineIndex);
    return pc;
  }

  static IceState _ice(RTCIceConnectionState s) => switch (s) {
    RTCIceConnectionState.RTCIceConnectionStateNew => IceState.fresh,
    RTCIceConnectionState.RTCIceConnectionStateChecking => IceState.checking,
    RTCIceConnectionState.RTCIceConnectionStateConnected => IceState.connected,
    RTCIceConnectionState.RTCIceConnectionStateCompleted => IceState.completed,
    RTCIceConnectionState.RTCIceConnectionStateDisconnected =>
      IceState.disconnected,
    RTCIceConnectionState.RTCIceConnectionStateFailed => IceState.failed,
    RTCIceConnectionState.RTCIceConnectionStateClosed => IceState.closed,
    _ => IceState.checking,
  };

  // ---- capture ----

  /// Opens the microphone (and camera) before joining, for the preview.
  Future<void> openMicrophone() async {
    microphone ??= await navigator.mediaDevices.getUserMedia({
      'audio': microphoneDeviceId == null
          ? true
          : {
              'deviceId': {'exact': microphoneDeviceId},
            },
      'video': false,
    });
  }

  Future<void> openCamera() async {
    if (camera != null) return;
    final phone = !_desktop;
    camera = await navigator.mediaDevices.getUserMedia({
      'audio': false,
      'video': {
        if (cameraDeviceId != null) 'deviceId': {'exact': cameraDeviceId},
        'width': phone ? 640 : 1280,
        'height': phone ? 360 : 720,
        'frameRate': 30,
      },
    });
    notifyListeners();
  }

  Future<void> closeCamera() async {
    final c = camera;
    camera = null;
    if (c != null) {
      for (final t in c.getTracks()) {
        await t.stop();
      }
      await c.dispose();
    }
    notifyListeners();
  }

  Future<void> _openScreen() async {
    if (screen != null) return;
    screen = await navigator.mediaDevices.getDisplayMedia({
      'audio': false,
      'video': screenSourceId == null
          ? true
          : {
              'deviceId': {'exact': screenSourceId},
              'mandatory': {'frameRate': 15.0},
            },
    });
    notifyListeners();
  }

  Future<void> _closeScreen() async {
    final s = screen;
    screen = null;
    if (s != null) {
      for (final t in s.getTracks()) {
        await t.stop();
      }
      await s.dispose();
    }
    notifyListeners();
  }

  /// Switches between the front and back camera on a phone.
  Future<void> switchCamera() async {
    final t = camera?.getVideoTracks().firstOrNull;
    if (t != null) await Helper.switchCamera(t);
  }

  /// Uses another camera: capture again and put the new track on the
  /// sender.
  Future<void> useCamera(String deviceId) async {
    cameraDeviceId = deviceId;
    if (camera == null) return;
    await closeCamera();
    await openCamera();
    final s = _senders[TrackSource.camera];
    if (s != null && !_muted.contains(TrackSource.camera)) {
      await s.replaceTrack(camera!.getVideoTracks().first);
    }
  }

  Future<void> useMicrophone(String deviceId) async {
    microphoneDeviceId = deviceId;
    if (_desktop) {
      await Helper.selectAudioInput(deviceId);
    }
  }

  Future<void> useSpeaker(String deviceId) =>
      Helper.selectAudioOutput(deviceId);

  /// Phones: the loudspeaker or the earpiece.
  Future<void> setSpeakerphone(bool on) => Helper.setSpeakerphoneOn(on);

  // ---- send ----

  @override
  Future<LocalOffer> offerSend(
    Set<TrackSource> sources, {
    bool iceRestart = false,
  }) async {
    final pc = _send ??= await _make(PcRole.send);
    for (final s in sources) {
      if (_transceivers.containsKey(s)) continue;
      await _add(pc, s);
    }
    for (final s in _transceivers.keys.toList()) {
      if (!sources.contains(s)) await _remove(pc, s);
    }
    final made = await pc.createOffer({if (iceRestart) 'iceRestart': true});
    // VP8 first (docs/CALLS.md section 6): the library on macOS offers
    // H.264 first and the server's answer takes the first codec it
    // shares (found in the real check). setCodecPreferences fails on this
    // library (error 7), so the offer's codec order is changed instead.
    final offer = RTCSessionDescription(
      // Reordered only: dropping the other codecs makes the library
      // refuse the description (found in the real check). VP8 first is
      // enough, since the server takes the first codec it shares.
      preferVp8(made.sdp ?? ''),
      made.type,
    );
    // Media lines are in transceiver order; each line's mid is in the SDP.
    final mids = midsOf(offer.sdp ?? '');
    final all = await pc.getTransceivers();
    final tracks = <OfferedTrack>[];
    for (var i = 0; i < all.length && i < mids.length; i++) {
      for (final e in _transceivers.entries) {
        if (all[i].sender.senderId == e.value.sender.senderId) {
          tracks.add(OfferedTrack(mids[i], e.key));
        }
      }
    }
    _pending = offer;
    return LocalOffer(offer.sdp ?? '', tracks);
  }

  RTCSessionDescription? _pending;

  Future<void> _add(RTCPeerConnection pc, TrackSource s) async {
    MediaStreamTrack? track;
    MediaStream? stream;
    switch (s) {
      case TrackSource.microphone:
        await openMicrophone();
        stream = microphone;
        track = microphone!.getAudioTracks().first;
      case TrackSource.camera:
        await openCamera();
        stream = camera;
        track = camera!.getVideoTracks().first;
      case TrackSource.screen:
        await _openScreen();
        stream = screen;
        track = screen!.getVideoTracks().first;
    }
    final layers = _desktop ? cameraLayersDesktop : cameraLayersPhone;
    final t = await pc.addTransceiver(
      track: track,
      kind: s == TrackSource.microphone
          ? RTCRtpMediaType.RTCRtpMediaTypeAudio
          : RTCRtpMediaType.RTCRtpMediaTypeVideo,
      init: RTCRtpTransceiverInit(
        direction: TransceiverDirection.SendOnly,
        streams: [?stream],
        sendEncodings: switch (s) {
          TrackSource.camera => [
            for (final l in layers)
              RTCRtpEncoding(
                rid: l.rid,
                active: true,
                maxBitrate: l.maxKbps * 1000,
                maxFramerate: l.maxFps,
                scaleResolutionDownBy: l.scaleDown,
              ),
          ],
          TrackSource.screen => [
            RTCRtpEncoding(active: true, maxBitrate: 2500000, maxFramerate: 15),
          ],
          TrackSource.microphone => [
            RTCRtpEncoding(active: true, maxBitrate: 32000),
          ],
        },
      ),
    );
    _transceivers[s] = t;
    _senders[s] = t.sender;
    if (_frameKeys != null) await _encryptSender(t.sender, s);
  }

  Future<void> _remove(RTCPeerConnection pc, TrackSource s) async {
    final t = _transceivers.remove(s);
    _senders.remove(s);
    if (t != null) {
      await pc.removeTrack(t.sender);
      await t.stop();
    }
    if (s == TrackSource.screen) await _closeScreen();
  }

  @override
  Future<void> acceptSendAnswer(String sdp) async {
    final pc = _send!;
    final offer = _pending;
    _pending = null;
    if (offer != null) await pc.setLocalDescription(offer);
    await pc.setRemoteDescription(RTCSessionDescription(sdp, 'answer'));
  }

  @override
  Future<void> rejectSendOffer(Set<TrackSource> keep) async {
    _pending = null;
    final pc = _send;
    if (pc == null) return;
    for (final s in _transceivers.keys.toList()) {
      if (!keep.contains(s)) await _remove(pc, s);
    }
    if (!keep.contains(TrackSource.camera)) await closeCamera();
  }

  @override
  Future<void> setMuted(TrackSource source, bool muted) async {
    muted ? _muted.add(source) : _muted.remove(source);
    switch (source) {
      case TrackSource.microphone:
        for (final t in microphone?.getAudioTracks() ?? <MediaStreamTrack>[]) {
          t.enabled = !muted;
        }
      case TrackSource.camera:
        // A camera that is off stops capturing (its light goes out) and
        // its sender sends nothing.
        final s = _senders[TrackSource.camera];
        if (muted) {
          await s?.replaceTrack(null);
          await closeCamera();
        } else {
          await openCamera();
          await s?.replaceTrack(camera!.getVideoTracks().first);
        }
      case TrackSource.screen:
        break;
    }
    notifyListeners();
  }

  @override
  Future<void> setDemand(TrackSource source, List<String> layers) async {
    final s = _senders[source];
    if (s == null || source != TrackSource.camera) return;
    final p = s.parameters;
    final encodings = p.encodings;
    if (encodings == null) return;
    var changed = false;
    for (final e in encodings) {
      final want = layers.contains(e.rid);
      if (e.active != want) {
        e.active = want;
        changed = true;
      }
    }
    if (changed) await s.setParameters(p);
  }

  // ---- recv ----

  @override
  Future<String> answerRecv(
    String sdp,
    Map<String, (String, String)> tracks, {
    Map<String, String> devices = const {},
  }) async {
    var pc = _recv;
    if (pc == null) {
      pc = _recv = await _make(PcRole.recv);
      pc.onTrack = _onTrack;
    }
    _lines = tracks;
    _lineDevices = devices;
    await pc.setRemoteDescription(RTCSessionDescription(sdp, 'offer'));
    final answer = await pc.createAnswer();
    await pc.setLocalDescription(answer);
    for (final r in remote.values) {
      final line = _lines[r.mid];
      if (line != null && (r.trackId != line.$1 || r.accountId != line.$2)) {
        r.trackId = line.$1;
        r.accountId = line.$2;
      }
      if (line != null) r.deviceId = _lineDevices[r.mid];
    }
    // A line may now carry another device's track: its key changes.
    await _routeReceivers();
    notifyListeners();
    return answer.sdp ?? '';
  }

  Future<void> _onTrack(RTCTrackEvent e) async {
    final mid = e.transceiver?.mid;
    if (mid == null) return;
    final stream = e.streams.isNotEmpty
        ? e.streams.first
        : await createLocalMediaStream('recv-$mid');
    if (e.streams.isEmpty) await stream.addTrack(e.track);
    final r = RemoteMedia(mid, e.track, stream);
    final line = _lines[mid];
    r.trackId = line?.$1;
    r.accountId = line?.$2;
    r.deviceId = _lineDevices[mid];
    remote[mid] = r;
    final receiver = e.receiver ?? e.transceiver?.receiver;
    if (receiver != null) _receivers[mid] = receiver;
    await _routeReceivers();
    notifyListeners();
  }

  // ---- frame encryption ----

  @override
  Future<void> setFrameKeys(FrameKeys keys) async {
    final old = _frameKeys;
    _frameKeys = keys;
    final kp = _keys ??= await frameCryptorFactory.createDefaultKeyProvider(
      callKeyProviderOptions(),
    );
    final change = frameKeyChange(old, keys);
    for (final k in change.set) {
      await kp.setKey(
        participantId: k.device,
        index: k.index,
        key: Uint8List.fromList(k.key),
      );
    }
    if (old == null || old.ownDevice != keys.ownDevice) {
      // The first keys of the call: every sender gets its cryptor.
      for (final c in _sendCryptors) {
        await c.dispose();
      }
      _sendCryptors.clear();
      for (final e in _senders.entries) {
        await _encryptSender(e.value, e.key);
      }
    } else {
      // A new epoch: frames go out under the new key index at once.
      for (final c in _sendCryptors) {
        await c.setKeyIndex(keys.keyIndex);
      }
    }
    await _routeReceivers();
    if (change.forget.isNotEmpty) {
      late final Timer t;
      t = Timer(callFrameKeepOldKeys, () {
        _forgetTimers.remove(t);
        unawaited(_forget(change.forget));
      });
      _forgetTimers.add(t);
    }
  }

  /// Drops keys a new epoch replaced, ten seconds on. The key provider
  /// has no way to remove a key, so a random one nobody holds is put in
  /// its place. A place the current epoch has since filled is left alone.
  Future<void> _forget(List<({String device, int index})> slots) async {
    final kp = _keys, now = _frameKeys;
    if (kp == null || now == null) return;
    final random = Random.secure();
    for (final s in slots) {
      final current = now.keys[s.device];
      if (current != null && now.keyIndex % callFrameKeyRing == s.index) {
        continue;
      }
      await kp.setKey(
        participantId: s.device,
        index: s.index,
        key: Uint8List.fromList(List.generate(32, (_) => random.nextInt(256))),
      );
      // Setting a key also makes its index the participant's current one
      // in the provider: put the current key back last.
      if (current != null) {
        await kp.setKey(
          participantId: s.device,
          index: now.keyIndex % callFrameKeyRing,
          key: Uint8List.fromList(current),
        );
      }
    }
  }

  Future<void> _encryptSender(RTCRtpSender sender, TrackSource s) async {
    final keys = _frameKeys!;
    final c = await frameCryptorFactory.createFrameCryptorForRtpSender(
      participantId: keys.ownDevice,
      sender: sender,
      algorithm: Algorithm.kAesGcm,
      keyProvider: _keys!,
    );
    await c.setKeyIndex(keys.keyIndex);
    await c.setEnabled(true);
    _sendCryptors.add(c);
  }

  /// Gives every recv line the cryptor of the device it carries now. A
  /// line with no cryptor would hand encrypted frames to the decoder, so
  /// every line of a sealed call has one, even when no key is known for
  /// it: then its frames are dropped and reported.
  Future<void> _routeReceivers() async {
    final keys = _frameKeys;
    if (keys == null) return;
    for (final r in remote.values) {
      final receiver = _receivers[r.mid];
      final account = r.accountId;
      if (receiver == null || account == null) continue;
      final participant = frameParticipant(
        keys,
        accountId: account,
        deviceId: r.deviceId,
      );
      final had = _recvCryptors[r.mid];
      if (had != null && had.participant == participant) continue;
      if (had != null) {
        _recvCryptors.remove(r.mid);
        await had.cryptor.setEnabled(false);
        await had.cryptor.dispose();
      }
      _recvCryptors[r.mid] = _LineCryptor(
        await _decryptReceiver(receiver, participant, account),
        participant,
      );
    }
  }

  Future<FrameCryptor> _decryptReceiver(
    RTCRtpReceiver receiver,
    String participant,
    String account,
  ) async {
    final c = await frameCryptorFactory.createFrameCryptorForRtpReceiver(
      participantId: participant,
      receiver: receiver,
      algorithm: Algorithm.kAesGcm,
      keyProvider: _keys!,
    );
    // Reported by account: that is who the screen names.
    c.onFrameCryptorStateChanged = (_, state) {
      switch (state) {
        case FrameCryptorState.FrameCryptorStateOk:
          onDecrypt?.call(account, true);
        case FrameCryptorState.FrameCryptorStateDecryptionFailed ||
            FrameCryptorState.FrameCryptorStateMissingKey:
          onDecrypt?.call(account, false);
        default:
          break;
      }
    };
    // A receiver takes the key index from each frame; this is only where
    // it starts.
    await c.setKeyIndex(_frameKeys!.keyIndex);
    await c.setEnabled(true);
    return c;
  }

  // ---- counters (the real check) ----

  /// Bytes sent and received so far, by kind: `audio_out`, `video_out`,
  /// `audio_in`, `video_in`.
  Future<Map<String, int>> counters() async {
    final out = <String, int>{};
    Future<void> add(
      RTCPeerConnection? pc,
      String type,
      String field,
      String dir,
    ) async {
      if (pc == null) return;
      for (final r in await pc.getStats()) {
        if (r.type != type) continue;
        final kind = r.values['kind'] ?? r.values['mediaType'];
        final n = r.values[field];
        if (kind is String && n is num) {
          out['${kind}_$dir'] = (out['${kind}_$dir'] ?? 0) + n.toInt();
        }
      }
    }

    await add(_send, 'outbound-rtp', 'bytesSent', 'out');
    await add(_recv, 'inbound-rtp', 'bytesReceived', 'in');
    return out;
  }

  // ---- closing ----

  @override
  Future<void> close({bool keepCapture = false}) async {
    for (final t in _forgetTimers) {
      t.cancel();
    }
    _forgetTimers.clear();
    for (final c in [
      ..._sendCryptors,
      for (final l in _recvCryptors.values) l.cryptor,
    ]) {
      await c.dispose();
    }
    _sendCryptors.clear();
    _recvCryptors.clear();
    _receivers.clear();
    _transceivers.clear();
    _senders.clear();
    remote.clear();
    final s = _send, r = _recv;
    _send = _recv = null;
    await s?.close();
    await r?.close();
    if (!keepCapture) {
      await _keys?.dispose();
      _keys = null;
      _frameKeys = null;
      for (final m in [microphone, camera, screen]) {
        for (final t in m?.getTracks() ?? <MediaStreamTrack>[]) {
          await t.stop();
        }
        await m?.dispose();
      }
      microphone = camera = screen = null;
    }
    notifyListeners();
  }
}

/// The key provider's settings for a call (docs/CALL-FRAMES.md).
KeyProviderOptions callKeyProviderOptions() => KeyProviderOptions(
  sharedKey: false,
  ratchetSalt: Uint8List.fromList(callFrameRatchetSalt),
  // No ratcheting: keys change only with the sealed chat's epoch.
  ratchetWindowSize: 0,
  failureTolerance: -1,
  keyRingSize: 16,
  discardFrameWhenCryptorNotReady: true,
  keyDerivationAlgorithm: callFrameUsesHkdf
      ? KeyDerivationAlgorithm.kHKDF
      : KeyDerivationAlgorithm.kPBKDF2,
);
