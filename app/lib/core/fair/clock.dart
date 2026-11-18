// The second a draw is asked for (docs/FAIR.md section 6), as the web
// chat's shared/fair/clock.js: the device's clock, corrected to the server's.
// A post that takes chance carries it as draw_t beside its client_id, and the
// two are its draw's input. The correction is learnt from the server: a post
// refused with `clock` says the server's time, and the offset to it is kept
// for the rest of the run. Plain Dart, tested without Flutter.

import '../api/errors.dart';

var _offsetMs = 0;

/// The second to give a draw now: Unix time, by the corrected clock.
int drawT([DateTime? now]) =>
    ((now ?? DateTime.now()).millisecondsSinceEpoch + _offsetMs) ~/ 1000;

/// Learns the offset from the server's time, an RFC 3339 string.
void learnServerTime(String? serverTime, [DateTime? now]) {
  final t = DateTime.tryParse(serverTime ?? '');
  if (t != null) {
    _offsetMs =
        t.millisecondsSinceEpoch -
        (now ?? DateTime.now()).millisecondsSinceEpoch;
  }
}

/// The offset in use, for tests.
int get clockOffsetMs => _offsetMs;

/// Sends a post that may take chance, with [t] its second, and once more,
/// with the clock corrected by the server's time, if the server refuses it
/// with `clock`: a second later if the corrected second is the same one.
Future<T> withDrawClock<T>(int t, Future<T> Function(int t) send) async {
  try {
    return await send(t);
  } on ApiException catch (e) {
    if (e.code != 'clock') rethrow;
    learnServerTime(e.serverTime);
    var next = drawT();
    if (next <= t) {
      await Future<void>.delayed(const Duration(milliseconds: 1100));
      next = drawT();
    }
    return send(next);
  }
}
