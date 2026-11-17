import 'package:clock/clock.dart';

import '../models/message.dart';

/// Which chance results and inline dice arrived live (rulings R67, R73,
/// R87): they play once when first drawn, and never for a message loaded
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

  /// How long a result's animation lasts: the reels' [spin], or the
  /// cards turned over ([revealPlan]).
  static Duration length(String? kind, int cards) =>
      kind == 'draw' || kind == 'tarot'
      ? Duration(milliseconds: revealPlan(kind, cards).total)
      : spin;

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
  /// [length] is how long it plays: the reels' [spin] unless given.
  static Duration? spinAt(String id, {DateTime? now, Duration length = spin}) {
    if (!_notes.containsKey(id)) return null;
    final t = now ?? clock.now();
    final began = _notes[id];
    if (began == null) {
      _notes[id] = t;
      return Duration.zero;
    }
    final d = t.difference(began);
    return d < length ? d : null;
  }
}

/// How cards and tiles are turned over (the web chat's REVEAL): the
/// face-down pause before the first, the turn itself, the time from one
/// card to the next (and the most all the steps may take), a tarot
/// reading's step, the pause before its summary, and a last pause.
abstract final class Reveal {
  static const lead = 200;
  static const flip = 280;
  static const step = 260;
  static const steps = 1800;
  static const tarotStep = 700;
  static const tarotSteps = 3000;
  static const summary = 350;
  static const end = 150;
}

/// When each of [cards] cards starts to turn over ([flips], in ms from
/// the start), when its words appear ([says]: as it lands face up), when
/// a tarot reading's summary appears ([after]) and when it is all over
/// ([total]): the web chat's revealPlan.
typedef RevealPlan = ({List<int> flips, List<int> says, int after, int total});

RevealPlan revealPlan(String? kind, int cards) {
  final tarot = kind == 'tarot';
  final step = cards <= 1
      ? 0.0
      : tarot
      ? (Reveal.tarotStep < Reveal.tarotSteps / cards
            ? Reveal.tarotStep.toDouble()
            : Reveal.tarotSteps / cards)
      : (Reveal.step < Reveal.steps / cards
            ? Reveal.step.toDouble()
            : Reveal.steps / cards);
  final flips = [
    for (var i = 0; i < cards; i++) (Reveal.lead + i * step).round(),
  ];
  final says = [for (final t in flips) t + Reveal.flip];
  final last = cards == 0 ? Reveal.lead : says.last;
  final after = tarot ? last + Reveal.summary : last;
  return (flips: flips, says: says, after: after, total: after + Reveal.end);
}
