import 'package:churr/core/notify/content.dart';
import 'package:churr/core/notify/registration.dart';
import 'package:churr/state/notify_center.dart';
import 'package:churr/state/notify_prefs.dart';
import 'package:churr/ui/screens/settings/notify_settings.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../account_support.dart';
import '../state/notify_center_test.dart' show FakeNotifier;

void main() {
  for (final size in [phone, desktop]) {
    final label = size == phone ? 'phone' : 'desktop';

    testWidgets('desktop settings at $label width: test, login, tray', (
      tester,
    ) async {
      await sized(tester, size);
      final (app, _) = await makeAccountApp(tester);
      final fake = FakeNotifier();
      NotifyCenter.install(NotifyCenter(app, notifier: fake));
      await tester.pumpWidget(
        harness(
          app,
          const SingleChildScrollView(
            child: NotifySettingsSection(platform: NotifyPlatform.desktop),
          ),
        ),
      );
      await settleAll(tester);
      expect(find.text('Send a test notification'), findsOneWidget);
      expect(find.text('Start Churr when you log in'), findsOneWidget);
      expect(
        find.text('Closing the window keeps Churr running'),
        findsOneWidget,
      );
      expect(find.text('Automatic'), findsNothing);
      expect(tester.takeException(), isNull);

      expect(app.prefs.startAtLogin, isFalse);
      await tester.tap(find.byKey(const Key('start-at-login')));
      await tester.pump();
      expect(app.prefs.startAtLogin, isTrue);

      expect(app.prefs.closeToTray, isTrue);
      await tester.tap(find.byKey(const Key('close-to-tray')));
      await tester.pump();
      expect(app.prefs.closeToTray, isFalse);

      await tester.tap(find.byKey(const Key('send-test-notification')));
      await settleAll(tester);
      expect(fake.shown.single.text.kind, BodyKind.test);
      expect(find.textContaining('If nothing appeared'), findsOneWidget);
    });

    testWidgets('Android push choice at $label width', (tester) async {
      await sized(tester, size);
      final (app, _) = await makeAccountApp(tester);
      app.prefs.setPushRefused('server-1', true);
      await tester.pumpWidget(
        harness(
          app,
          const SingleChildScrollView(
            child: NotifySettingsSection(platform: NotifyPlatform.android),
          ),
        ),
      );
      await settleAll(tester);
      expect(
        find.text('How this phone hears about new messages'.toUpperCase()),
        findsOneWidget,
      );
      for (final t in [
        'Automatic',
        'UnifiedPush',
        'Stay connected',
        'Only when Churr is open',
      ]) {
        expect(find.text(t), findsOneWidget);
      }
      expect(find.text('Start Churr when you log in'), findsNothing);
      // The server that refused the push kind is named.
      expect(find.textContaining('Friends does not send'), findsOneWidget);
      expect(app.prefs.androidPush, AndroidPush.automatic);
      await tester.tap(find.byKey(const Key('android-push-stayConnected')));
      await tester.pump();
      expect(app.prefs.androidPush, AndroidPush.stayConnected);
      expect(tester.takeException(), isNull);
    });
  }

  testWidgets('iPhone: only the test notification', (tester) async {
    await sized(tester, phone);
    final (app, _) = await makeAccountApp(tester);
    await tester.pumpWidget(
      harness(
        app,
        const NotifySettingsSection(platform: NotifyPlatform.iphone),
      ),
    );
    await settleAll(tester);
    expect(find.text('Send a test notification'), findsOneWidget);
    expect(find.text('Automatic'), findsNothing);
    expect(find.text('Start Churr when you log in'), findsNothing);
  });
}
