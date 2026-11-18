// The second a draw is asked for (lib/core/fair/clock.dart): the device's
// clock corrected by the server's time from ordinary traffic, a time far
// from the device's clock refused once another is known, and a refused draw
// never sent again, as the web chat's e2e/fair.mjs holds it.

import 'dart:io' show HttpDate;

import 'package:churr/core/api/errors.dart';
import 'package:churr/core/fair/clock.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  final now = DateTime.fromMillisecondsSinceEpoch(1790000000000, isUtc: true);

  setUp(resetClock);

  test('the clock is corrected by the server\'s time from traffic', () {
    expect(drawT(now), 1790000000);
    learnDateHeader(HttpDate.format(now.add(const Duration(seconds: 4))), now);
    expect(clockOffsetMs, 4500);
    learnServerTime(
      now.add(const Duration(seconds: 30)).toIso8601String(),
      now,
      const Duration(milliseconds: 400),
    );
    expect(clockOffsetMs, 30200);
    expect(drawT(now), 1790000030);
    learnDateHeader(HttpDate.format(now), now);
    expect(
      clockOffsetMs,
      30200,
      reason: 'a Date header does not overrule the live connection',
    );
    learnServerTime('not a time', now);
    expect(clockOffsetMs, 30200);
    final why = learnServerTime(
      now.add(const Duration(minutes: 6)).toIso8601String(),
      now,
    );
    expect(clockOffsetMs, 30200);
    expect(why, contains('6 minutes'));
  });

  test('with nothing else learnt, a time far off is taken', () {
    expect(
      learnServerTime(
        now.add(const Duration(minutes: 6)).toIso8601String(),
        now,
      ),
      isNull,
    );
    expect(clockOffsetMs, 360000);
  });

  test('a second is inside its window for 10 seconds', () {
    expect(
      inDrawWindow(1790000000, now.add(const Duration(seconds: 10))),
      isTrue,
    );
    expect(
      inDrawWindow(1790000000, now.add(const Duration(seconds: 11))),
      isFalse,
    );
    expect(mayRoll('I hit for [[2d6]]'), isTrue);
    expect(mayRoll('hello'), isFalse);
  });

  test('a refused draw is shown and never sent again', () async {
    var sent = 0;
    final server = DateTime.now().add(const Duration(seconds: 20));
    Object? err;
    try {
      await sendDraw(() async {
        sent++;
        throw ApiException(
          status: 409,
          code: 'clock',
          message: 'off',
          serverTime: server.toIso8601String(),
        );
      });
    } on ApiException catch (e) {
      err = e;
    }
    expect(sent, 1);
    expect(
      (err as ApiException).message,
      startsWith('The server refused this roll.'),
    );
    expect(
      (clockOffsetMs - 20000).abs() < 2000,
      isTrue,
      reason: 'its time is learnt for later posts',
    );
    final slow = drawRefusal(
      ApiException(status: 409, code: 'conflict', message: 'taken'),
      const Duration(seconds: 12),
    );
    expect(slow!.message, contains('the network, not a refusal'));
    expect(
      drawRefusal(
        ApiException(status: 403, code: 'forbidden', message: 'no'),
        Duration.zero,
      ),
      isNull,
    );
  });
}
