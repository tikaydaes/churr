import 'package:churr/l10n/app_localizations.dart';
import 'package:churr/ui/widgets/reconnect_countdown.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  Widget app(DateTime? Function() retryAt, DateTime Function() now) =>
      MaterialApp(
    localizationsDelegates: L10n.localizationsDelegates,
    supportedLocales: L10n.supportedLocales,
    home: ReconnectCountdown(
      retryAt: retryAt,
      now: now,
      builder: (context, seconds) => Text(
        seconds == null
            ? L10n.of(context).notConnected
            : L10n.of(context).reconnectingIn(seconds),
      ),
    ),
  );

  testWidgets(
    'shows the seconds really left, rounded up, and follows a new wait',
    (tester) async {
      // The test's own clock, moved together with the timers the tester
      // controls. With the real clock the number shown depended on how long
      // the first frame took to draw: more than 400 ms, as in a full run on
      // a busy machine, and "8 seconds" was already 7.
      var now = DateTime.utc(2026, 11, 15, 12);
      Future<void> pass(Duration d) async {
        now = now.add(d);
        await tester.pump(d);
      }

      DateTime? at = now.add(const Duration(milliseconds: 7400));
      await tester.pumpWidget(app(() => at, () => now));
      expect(find.text('Reconnecting in 8 seconds'), findsOneWidget);
      // It counts down as time passes: 6.9 seconds are left, then 5.9.
      await pass(const Duration(milliseconds: 500));
      expect(find.text('Reconnecting in 7 seconds'), findsOneWidget);
      await pass(const Duration(milliseconds: 1000));
      expect(find.text('Reconnecting in 6 seconds'), findsOneWidget);
      // The connection chose again (a failed attempt, the next step): the
      // line follows on its next look, a quarter of a second later.
      at = now.add(const Duration(milliseconds: 21900));
      await pass(const Duration(milliseconds: 300));
      expect(find.text('Reconnecting in 22 seconds'), findsOneWidget);
      at = now.add(const Duration(milliseconds: 600));
      await pass(const Duration(milliseconds: 300));
      expect(find.text('Reconnecting in 1 second'), findsOneWidget);
      // The attempt is being made: nothing is counted.
      at = null;
      await pass(const Duration(milliseconds: 300));
      expect(find.text('Not connected'), findsOneWidget);
    },
  );

  testWidgets('the banner wording', (tester) async {
    late L10n l;
    await tester.pumpWidget(
      MaterialApp(
        localizationsDelegates: L10n.localizationsDelegates,
        supportedLocales: L10n.supportedLocales,
        home: Builder(
          builder: (context) {
            l = L10n.of(context);
            return const SizedBox();
          },
        ),
      ),
    );
    expect(
      l.reconnectingBanner(12),
      'Not connected. Reconnecting in 12 seconds. Messages you send will go when it is back.',
    );
    expect(
      l.reconnectingBanner(1),
      'Not connected. Reconnecting in 1 second. Messages you send will go when it is back.',
    );
    expect(
      l.notConnectedBanner,
      'Not connected. Messages you send will go when it is back.',
    );
  });
}
