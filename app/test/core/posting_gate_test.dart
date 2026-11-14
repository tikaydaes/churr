import 'package:churr/core/conversation/posting_gate.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:flutter_test/flutter_test.dart';

Conversation _c({int slow = 0, String? timedOut}) => Conversation.fromJson({
  'id': 'c',
  'kind': 'channel',
  'name': 'ops',
  'slow_mode_seconds': slow,
  'membership': {
    'level': 'member',
    'read_seq': 0,
    'mode': 'mentions',
    'timed_out_until': ?timedOut,
  },
});

void main() {
  final now = DateTime.utc(2026, 11, 14, 12);

  test('open with no slow mode and no timeout', () {
    expect(postingGate(_c(), now: now).kind, GateKind.open);
    expect(postingGate(null, now: now).kind, GateKind.open);
  });

  test('timed out until a time, then open again', () {
    final g = postingGate(_c(timedOut: '2026-11-14T13:00:00Z'), now: now);
    expect(g.kind, GateKind.timedOut);
    expect(g.until, DateTime.utc(2026, 11, 14, 13));
    expect(
      postingGate(_c(timedOut: '2026-11-14T11:00:00Z'), now: now).kind,
      GateKind.open,
    );
  });

  test('slow mode counts down from the last own post; managers are exempt', () {
    final c = _c(slow: 60);
    final g = postingGate(
      c,
      now: now,
      lastOwnPost: now.subtract(const Duration(seconds: 18)),
    );
    expect(g.kind, GateKind.slowMode);
    expect(g.left, const Duration(seconds: 42));
    expect(countdown(g.left), '0:42');
    expect(
      postingGate(
        c,
        now: now,
        lastOwnPost: now.subtract(const Duration(seconds: 61)),
      ).kind,
      GateKind.open,
    );
    expect(
      postingGate(c, now: now, lastOwnPost: now, exempt: true).kind,
      GateKind.open,
    );
    // A wait the server named counts too.
    expect(
      postingGate(
        _c(slow: 60),
        now: now,
        slowUntil: now.add(const Duration(minutes: 2)),
      ).left,
      const Duration(minutes: 2),
    );
  });

  test('countdown wording', () {
    expect(countdown(const Duration(seconds: 5)), '0:05');
    expect(countdown(const Duration(minutes: 12, seconds: 5)), '12:05');
    expect(
      countdown(const Duration(hours: 1, minutes: 2, seconds: 3)),
      '1:02:03',
    );
    expect(countdown(const Duration(milliseconds: 1500)), '0:02');
  });
}
