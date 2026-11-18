// The second a draw is asked for (lib/core/fair/clock.dart): the device's
// clock corrected by the server's time a `clock` refusal gives, and the one
// resend with the corrected second, as the web chat's e2e/fair.mjs holds it.

import 'package:churr/core/api/errors.dart';
import 'package:churr/core/fair/clock.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('the clock is corrected by the server\'s time', () {
    final now = DateTime.fromMillisecondsSinceEpoch(1790000000000, isUtc: true);
    expect(drawT(now), 1790000000);
    learnServerTime(
      now.add(const Duration(seconds: 30)).toIso8601String(),
      now,
    );
    expect(clockOffsetMs, 30000);
    expect(drawT(now), 1790000030);
    learnServerTime('not a time', now);
    expect(clockOffsetMs, 30000);
  });

  test('a clock refusal is sent once more with the corrected second', () async {
    final sent = <int>[];
    final server = DateTime.now().add(const Duration(minutes: 5));
    final out = await withDrawClock(1, (t) async {
      sent.add(t);
      if (sent.length == 1) {
        throw ApiException(
          status: 409,
          code: 'clock',
          message: 'off',
          serverTime: server.toIso8601String(),
        );
      }
      return 'posted';
    });
    expect(out, 'posted');
    expect(sent.length, 2);
    expect(
      (sent[1] - server.millisecondsSinceEpoch ~/ 1000).abs() <= 1,
      isTrue,
    );
  });
}
