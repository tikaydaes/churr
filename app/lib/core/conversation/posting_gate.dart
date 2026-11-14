// When the message box gives way to a notice because the person may not
// post yet (docs/CLIENTS.md 9.5, docs/FEATURES.md sections 15 and 16):
// timed out in the channel until a time, or slow mode counting down to
// the next allowed post. Plain Dart: the composer words it.

import '../models/conversation.dart';
import '../models/json.dart';

/// Why the box is closed for now.
enum GateKind { open, timedOut, slowMode }

class Gate {
  const Gate(this.kind, {this.until, this.left = Duration.zero});
  static const open = Gate(GateKind.open);
  final GateKind kind;

  /// The end of a timeout.
  final DateTime? until;

  /// What is left of slow mode's interval.
  final Duration left;
}

/// The gate for [c] at [now]. [lastOwnPost] is the person's newest post
/// in it as this device knows it; [slowUntil] an end the server named
/// with `slow_mode` and `retry_after_seconds`. Managers, who are exempt
/// from slow mode, pass [exempt].
Gate postingGate(
  Conversation? c, {
  required DateTime now,
  DateTime? lastOwnPost,
  DateTime? slowUntil,
  bool exempt = false,
}) {
  final m = c?.membership;
  if (c == null || m == null) return Gate.open;
  final timedOut = m.timedOutUntil;
  if (timedOut != null && timedOut.isAfter(now)) {
    return Gate(GateKind.timedOut, until: timedOut);
  }
  if (exempt) return Gate.open;
  final seconds = asInt(c.raw['slow_mode_seconds']);
  DateTime? end = slowUntil;
  if (seconds > 0 && lastOwnPost != null) {
    final byPost = lastOwnPost.add(Duration(seconds: seconds));
    if (end == null || byPost.isAfter(end)) end = byPost;
  }
  if (end != null && end.isAfter(now)) {
    return Gate(GateKind.slowMode, until: end, left: end.difference(now));
  }
  return Gate.open;
}

/// `0:42`, `12:05`, `1:02:03`: what is left, for the countdown.
String countdown(Duration d) {
  final s = d.inSeconds + (d.inMilliseconds % 1000 > 0 ? 1 : 0);
  final h = s ~/ 3600, m = (s % 3600) ~/ 60, sec = s % 60;
  String two(int v) => v.toString().padLeft(2, '0');
  return h > 0 ? '$h:${two(m)}:${two(sec)}' : '$m:${two(sec)}';
}
