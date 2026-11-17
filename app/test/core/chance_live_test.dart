import 'package:churr/core/commands/chance_live.dart';
import 'package:flutter_test/flutter_test.dart';

/// The one live spin of a chance result (rulings R67, R73): it belongs to
/// the message, starts when the result is first drawn, carries on when it
/// is drawn again while turning, and never comes back.
void main() {
  final t0 = DateTime.utc(2026, 11, 16, 9);

  test(
    'the first drawing starts the spin; a drawing mid-spin carries it on',
    () {
      ChanceLive.arrived('a', t0, now: t0);
      expect(ChanceLive.spinAt('a', now: t0), Duration.zero);
      expect(
        ChanceLive.spinAt('a', now: t0.add(const Duration(milliseconds: 500))),
        const Duration(milliseconds: 500),
      );
      expect(
        ChanceLive.spinAt('a', now: t0.add(const Duration(milliseconds: 1300))),
        isNull,
      );
    },
  );

  test('noted twice (the answer and the event) is one arrival', () {
    ChanceLive.arrived('b', t0, now: t0);
    expect(ChanceLive.spinAt('b', now: t0), Duration.zero);
    final later = t0.add(const Duration(seconds: 2));
    ChanceLive.arrived('b', t0, now: later);
    expect(ChanceLive.spinAt('b', now: later), isNull);
  });

  test('from history, or too old to be live, it does not spin', () {
    expect(ChanceLive.spinAt('never', now: t0), isNull);
    ChanceLive.arrived('old', t0, now: t0.add(const Duration(minutes: 5)));
    expect(ChanceLive.spinAt('old', now: t0), isNull);
  });

  test('cards are turned over by the web chat\'s plan, briefly', () {
    // The same numbers as e2e/chancereveal.mjs reads from the web chat.
    final three = revealPlan('draw', 3);
    expect(three.flips, [200, 460, 720]);
    expect(three.total, 1150);
    expect(revealPlan('draw', 52).total, lessThanOrEqualTo(2400));
    final reading = revealPlan('tarot', 3);
    expect(reading.flips, [200, 900, 1600]);
    expect(reading.says, [480, 1180, 1880]);
    expect((reading.after, reading.total), (2230, 2380));
    expect(revealPlan('tarot', 10).total, 3680);
    expect(ChanceLive.length('roll', 4), ChanceLive.spin);
    expect(ChanceLive.length('tarot', 3), const Duration(milliseconds: 2380));
    // A reading plays for its own length, not the reels'.
    ChanceLive.arrived('t', t0, now: t0);
    final len = ChanceLive.length('tarot', 3);
    expect(ChanceLive.spinAt('t', now: t0, length: len), Duration.zero);
    expect(
      ChanceLive.spinAt(
        't',
        now: t0.add(const Duration(milliseconds: 2000)),
        length: len,
      ),
      const Duration(milliseconds: 2000),
    );
    expect(
      ChanceLive.spinAt(
        't',
        now: t0.add(const Duration(milliseconds: 2400)),
        length: len,
      ),
      isNull,
    );
  });
}
