import 'dart:async';

import '../live.dart';
import '../models/json.dart';
import 'call_media.dart';

/// [CallSignal] over the app's live connection: frames go out with
/// [LiveConnection.sendFrame]; `call.` frames come in through the sync
/// engine's local events ([events]); the connection is "up" whenever it
/// reaches `ready`.
class LiveCallSignal implements CallSignal {
  LiveCallSignal(this.live, Stream<Json> events)
    : frames = events.where(
        (e) => (e['type'] as String? ?? '').startsWith('call.'),
      );

  final LiveConnection live;

  @override
  final Stream<Map<String, dynamic>> frames;

  @override
  bool send(Map<String, Object?> frame) => live.sendFrame(frame);

  @override
  bool get connected => live.state == LiveState.ready;

  @override
  Stream<bool> get connection =>
      live.states.map((s) => s == LiveState.ready).distinct();
}
