import 'package:flutter/material.dart';
import 'package:flutter_webrtc/flutter_webrtc.dart';

import '../../../core/calls/call_media.dart';
import '../../../platform/calls/webrtc_media.dart';
import '../../theme/tokens.dart';

/// Draws video for the call screen. The default draws flutter_webrtc's
/// streams; widget tests put a stand-in here so no platform view is made.
abstract final class CallVideo {
  /// Someone else's video track, by its identifier; null when it has not
  /// arrived.
  static Widget? Function(CallMedia media, String trackId, {bool cover})
  remote = _remote;

  /// The person's own camera ([screen] false) or shared screen.
  static Widget? Function(CallMedia media, {bool screen}) local = _local;

  static Widget? _remote(CallMedia media, String trackId, {bool cover = true}) {
    if (media is! WebRtcMedia) return null;
    final r = media.remoteTrack(trackId);
    if (r == null) return null;
    return RtcVideo(stream: r.stream, cover: cover, key: ValueKey(r.stream.id));
  }

  static Widget? _local(CallMedia media, {bool screen = false}) {
    if (media is! WebRtcMedia) return null;
    final s = screen ? media.screen : media.camera;
    if (s == null) return null;
    return RtcVideo(
      stream: s,
      mirror: !screen,
      cover: !screen,
      key: ValueKey(s.id),
    );
  }
}

/// One stream in a renderer it owns.
class RtcVideo extends StatefulWidget {
  const RtcVideo({
    super.key,
    required this.stream,
    this.mirror = false,
    this.cover = true,
  });
  final MediaStream stream;
  final bool mirror;
  final bool cover;

  @override
  State<RtcVideo> createState() => _RtcVideoState();
}

class _RtcVideoState extends State<RtcVideo> {
  final _renderer = RTCVideoRenderer();
  bool _ready = false;

  @override
  void initState() {
    super.initState();
    _renderer.initialize().then((_) {
      if (!mounted) return;
      _renderer.srcObject = widget.stream;
      setState(() => _ready = true);
    });
  }

  @override
  void didUpdateWidget(RtcVideo old) {
    super.didUpdateWidget(old);
    if (_ready && old.stream != widget.stream) {
      _renderer.srcObject = widget.stream;
    }
  }

  @override
  void dispose() {
    _renderer.srcObject = null;
    _renderer.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!_ready) return ColoredBox(color: ChurrColors.of(context).raised);
    return RTCVideoView(
      _renderer,
      mirror: widget.mirror,
      objectFit: widget.cover
          ? RTCVideoViewObjectFit.RTCVideoViewObjectFitCover
          : RTCVideoViewObjectFit.RTCVideoViewObjectFitContain,
    );
  }
}
