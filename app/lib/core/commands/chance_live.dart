import 'package:clock/clock.dart';

import '../models/message.dart';

/// Which chance results and inline dice arrived live (rulings R67, R73):
/// their reels spin once when first drawn, and never for a message loaded
/// from history, met again on scrolling back or drawn again.
///
/// A message is noted as it comes in: over the live connection, and for
/// its author also from the server's answer to posting it, whichever is
/// first; noting it twice is one arrival. The spin belongs to the
/// message, not to one widget: the first drawing starts its clock, and a
/// result drawn again while its reels turn carries on from where they
/// were. Once the spin is over nothing turns them again.
abstract final class ChanceLive {
  /// Noted ids, with when their spin began (null: not drawn yet).
  static final _notes = <String, DateTime?>{};

  /// How old a message may be and still count as arriving live: catching
  /// up after a reconnect brings older ones, which do not spin.
  static const fresh = Duration(seconds: 30);

  /// How long the reels turn.
  static const spin = Duration(milliseconds: 1200);

  /// Notes a result that came in live. [createdAt] is the message's.
  static void arrived(String id, DateTime createdAt, {DateTime? now}) {
    if (_notes.containsKey(id)) return;
    if ((now ?? clock.now()).difference(createdAt).abs() <= fresh) {
      _notes[id] = null;
      // Notes are kept a while, so that the same message noted again
      // (its event after the author's answer) does not spin again; a
      // note never drawn (the conversation was not on screen) goes too.
      if (_notes.length > 200) _notes.remove(_notes.keys.first);
    }
  }

  /// Notes the reels of a message that came in live: its chance result
  /// and each of its inline dice.
  static void arrivedMessage(Message m, {DateTime? now}) {
    if (m.chance != null) arrived(m.id, m.createdAt, now: now);
    for (final d in m.entities) {
      if (d.type == 'dice') arrived(diceId(m.id, d.at), m.createdAt, now: now);
    }
  }

  /// The id under which the inline dice at [at] in a message are noted.
  static String diceId(String messageId, int at) => '$messageId#dice@$at';

  /// How far into its one spin [id] is when drawn now: zero the first
  /// time a result that arrived live is drawn, the time since then while
  /// its reels turn, and null when it did not arrive live or has spun.
  static Duration? spinAt(String id, {DateTime? now}) {
    if (!_notes.containsKey(id)) return null;
    final t = now ?? clock.now();
    final began = _notes[id];
    if (began == null) {
      _notes[id] = t;
      return Duration.zero;
    }
    final d = t.difference(began);
    return d < spin ? d : null;
  }
}
