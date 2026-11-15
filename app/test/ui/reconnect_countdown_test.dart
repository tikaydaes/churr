import 'package:churr/l10n/app_localizations.dart';
import 'package:churr/ui/widgets/reconnect_countdown.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  Widget app(DateTime? Function() retryAt) => MaterialApp(
    localizationsDelegates: L10n.localizationsDelegates,
    supportedLocales: L10n.supportedLocales,
    home: ReconnectCountdown(
      retryAt: retryAt,
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
      DateTime? at = DateTime.now().add(const Duration(milliseconds: 7400));
      await tester.pumpWidget(app(() => at));
      expect(find.text('Reconnecting in 8 seconds'), findsOneWidget);
      // The connection chose again (a failed attempt, the next step): the
      // line follows on its next look, a quarter of a second later.
      at = DateTime.now().add(const Duration(milliseconds: 21900));
      await tester.pump(const Duration(milliseconds: 300));
      expect(find.text('Reconnecting in 22 seconds'), findsOneWidget);
      at = DateTime.now().add(const Duration(milliseconds: 600));
      await tester.pump(const Duration(milliseconds: 300));
      expect(find.text('Reconnecting in 1 second'), findsOneWidget);
      // The attempt is being made: nothing is counted.
      at = null;
      await tester.pump(const Duration(milliseconds: 300));
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
