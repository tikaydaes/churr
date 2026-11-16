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
}
