// The second a draw is asked for (docs/FAIR.md section 6), as the web
// chat's shared/fair/clock.js: the device's clock, corrected to the server's.
// A post that takes chance carries it as draw_t beside its client_id, and the
// two are its draw's input.
//
// An honest client is never refused, so the correction is known before the
// first draw and kept current from ordinary traffic: the live connection's
// hello and every pong say the server's time, and every answer of the API
// its Date header (whole seconds, taken only until the live connection has
// said better). A `clock` or `conflict` refusal is not routine: it is shown
// to the person and never answered by sending the draw again
// ([drawRefusal]); the time a refusal states is learnt for later posts only.
//
// A stated time more than five minutes from this device's own clock is not
// taken unless nothing else has been learnt. Plain Dart, tested without
// Flutter.

import 'dart:developer' as developer;
import 'dart:io' show HttpDate;

import '../api/errors.dart';

/// How far a stated time may be from the device's clock.
const maxStated = Duration(minutes: 5);

/// The window of a draw, in seconds: as the server's (FAIR.md section 6).
const drawWindow = 10;

var _offsetMs = 0;
// What the offset was learnt from: 'none', 'date' or 'live'.
var _source = 'none';

/// The second to give a draw now: Unix time, by the corrected clock.
int drawT([DateTime? now]) =>
    ((now ?? DateTime.now()).millisecondsSinceEpoch + _offsetMs) ~/ 1000;

/// Learns the offset from a time the server stated, an RFC 3339 string, as
/// received at [now]; half of [rtt], when known, is taken as its age.
/// Returns null when it was taken (or could not be read), or why it was not:
/// a time more than five minutes from this device's clock while another has
/// been learnt.
String? learnServerTime(
  String? serverTime, [
  DateTime? now,
  Duration rtt = Duration.zero,
]) {
  final t = DateTime.tryParse(serverTime ?? '');
  if (t == null) return null;
  return _take(
    t.millisecondsSinceEpoch +
        rtt.inMilliseconds ~/ 2 -
        (now ?? DateTime.now()).millisecondsSinceEpoch,
    'live',
  );
}

/// Learns from an HTTP Date header, until the live connection has spoken.
void learnDateHeader(String? date, [DateTime? now]) {
  if (_source == 'live' || date == null) return;
  final DateTime t;
  try {
    t = HttpDate.parse(date);
  } on FormatException {
    return;
  }
  // The header is whole seconds, cut down: half a second is its middle.
  _take(
    t.millisecondsSinceEpoch +
        500 -
        (now ?? DateTime.now()).millisecondsSinceEpoch,
    'date',
  );
}

String? _take(int off, String from) {
  if (off.abs() > maxStated.inMilliseconds && _source != 'none') {
    final minutes = (off.abs() / 60000).round();
    return 'The server stated a time $minutes minutes from this device\'s clock, which this device does not take.';
  }
  _offsetMs = off;
  _source = from;
  return null;
}

/// Whether a second given to a draw is still inside its window now.
bool inDrawWindow(int second, [DateTime? now]) =>
    (drawT(now) - second).abs() <= drawWindow;

/// Whether a message's text may roll: it holds inline dice, `[[...]]`.
bool mayRoll(String text) => text.contains('[[');

/// What a message that may roll says when its second has left the window
/// before any answer came: it is not sent again.
const rollDidNotGoThrough =
    'The roll did not go through. Send it again as a new message to roll again.';

/// The offset in use, for tests.
int get clockOffsetMs => _offsetMs;

/// Forgets what was learnt, for tests.
void resetClock() {
  _offsetMs = 0;
  _source = 'none';
}

/// What a post that draws makes of a refusal [e] of its draw, the request
/// having taken [took] to be answered: null for any other failure.
/// The refusal's time is learnt for later posts, the refusal is noted on the
/// device, and it is said plainly: the server refused this roll, or, when
/// the request took longer than the window, that it was the network.
ApiException? drawRefusal(ApiException e, Duration took) {
  if (e.code != 'clock' && e.code != 'conflict') return null;
  final why = e.code == 'clock' ? learnServerTime(e.serverTime) : null;
  developer.log(
    'a draw was refused: ${e.code} ${e.message} (server_time ${e.serverTime}, took ${took.inMilliseconds} ms)',
    name: 'churr.fair',
  );
  final message = took.inSeconds > drawWindow
      ? 'The roll took ${took.inSeconds} seconds to reach the server and come back, longer than a draw may take: that is the network, not a refusal. Roll again.'
      : 'The server refused this roll. ${e.message}${why == null ? '' : ' $why'}';
  return ApiException(
    status: e.status,
    code: e.code,
    message: message,
    serverTime: e.serverTime,
  );
}

/// Sends a post that may take chance once: a refusal of its draw is thrown
/// as [drawRefusal] says, never sent again by itself.
Future<T> sendDraw<T>(Future<T> Function() send) async {
  final took = Stopwatch()..start();
  try {
    return await send();
  } on ApiException catch (e) {
    final r = drawRefusal(e, took.elapsed);
    if (r != null) throw r;
    rethrow;
  }
}
