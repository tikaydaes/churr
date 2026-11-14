import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';

import '../../../core/notify/registration.dart';
import '../../../l10n/app_localizations.dart';
import '../../../platform/background_service_control.dart';
import '../../../platform/desktop.dart' as desktop;
import '../../../platform/push.dart';
import '../../../state/notify_center.dart';
import '../../../state/notify_prefs.dart';
import '../../scopes.dart';
import '../../theme/churr_icons.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import '../account/common.dart';

/// Which settings this device offers.
enum NotifyPlatform { desktop, android, iphone }

NotifyPlatform get currentNotifyPlatform => Platform.isAndroid
    ? NotifyPlatform.android
    : Platform.isIOS
    ? NotifyPlatform.iphone
    : NotifyPlatform.desktop;

/// The notification settings App settings adds under its own switches
/// (docs/CLIENTS.md 9.12 "For this device" and 9.23): "Send a test
/// notification"; on desktop, start at login and close to the tray; on
/// Android, how the phone is told about new messages (docs/PUSH.md
/// section 10).
class NotifySettingsSection extends StatelessWidget {
  const NotifySettingsSection({super.key, this.platform});

  /// Tests choose; the app uses the device's.
  final NotifyPlatform? platform;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final app = AppScope.of(context);
    final prefs = app.prefs;
    final kind = platform ?? currentNotifyPlatform;
    return ListenableBuilder(
      listenable: prefs,
      builder: (context, _) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Align(
            alignment: AlignmentDirectional.centerStart,
            child: OutlinedButton.icon(
              key: const Key('send-test-notification'),
              icon: const Glyph(ChurrIcons.notify, size: 16),
              label: Text(t.sendTestNotification),
              onPressed: () async {
                final messenger = ScaffoldMessenger.maybeOf(context);
                await NotifyCenter.of(app).sendTest();
                messenger?.showSnackBar(
                  SnackBar(content: Text(t.testNotificationSent)),
                );
              },
            ),
          ),
          if (kind == NotifyPlatform.desktop) ...[
            SectionHead(t.desktopSection),
            ChurrSwitchTile(
              key: const Key('start-at-login'),
              contentPadding: EdgeInsets.zero,
              title: Text(t.startAtLogin),
              subtitle: Text(t.startAtLoginNote),
              value: prefs.startAtLogin,
              onChanged: (v) {
                prefs.startAtLogin = v;
                if (platform == null) desktop.applyStartAtLogin(v);
              },
            ),
            ChurrSwitchTile(
              key: const Key('close-to-tray'),
              contentPadding: EdgeInsets.zero,
              title: Text(t.closeToTray),
              subtitle: Text(t.closeToTrayNote),
              value: prefs.closeToTray,
              onChanged: (v) => prefs.closeToTray = v,
            ),
          ],
          if (kind == NotifyPlatform.android)
            AndroidPushChoice(live: platform == null),
        ],
      ),
    );
  }
}

/// The Android push choice (docs/PUSH.md section 10): Firebase in the
/// Play build or UnifiedPush or nothing, staying connected, or only when
/// open. Also the first-run choice on Android without Google services
/// (docs/CLIENTS.md 9.18).
class AndroidPushChoice extends StatefulWidget {
  const AndroidPushChoice({super.key, this.live = true});

  /// False in tests: nothing is asked of the platform.
  final bool live;

  @override
  State<AndroidPushChoice> createState() => _AndroidPushChoiceState();
}

class _AndroidPushChoiceState extends State<AndroidPushChoice> {
  bool? _distributor;

  @override
  void initState() {
    super.initState();
    if (widget.live) {
      unawaited(
        PushService.hasDistributor().then((v) {
          if (mounted) setState(() => _distributor = v);
        }),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final app = AppScope.of(context);
    final prefs = app.prefs;
    final c = ChurrColors.of(context);
    String title(AndroidPush p) => switch (p) {
      AndroidPush.automatic => t.androidPushAutomatic,
      AndroidPush.unifiedPush => t.androidPushUnified,
      AndroidPush.stayConnected => t.androidPushStay,
      AndroidPush.off => t.androidPushOff,
    };
    String note(AndroidPush p) => switch (p) {
      AndroidPush.automatic => t.androidPushAutomaticNote,
      AndroidPush.unifiedPush => t.androidPushUnifiedNote,
      AndroidPush.stayConnected => t.androidPushStayNote,
      AndroidPush.off => t.androidPushOffNote,
    };
    final refused = [
      for (final s in app.servers)
        if (prefs.pushRefused.contains(s.localId)) s.name,
    ];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SectionHead(t.androidPushTitle),
        RadioGroup<AndroidPush>(
          groupValue: prefs.androidPush,
          onChanged: (v) async {
            if (v == null) return;
            prefs.androidPush = v;
            if (!widget.live) return;
            if (v == AndroidPush.stayConnected) await askBatteryExemption();
            await PushService.current?.androidChoiceChanged();
          },
          child: Column(
            children: [
              for (final p in AndroidPush.values)
                RadioListTile<AndroidPush>(
                  key: Key('android-push-${p.name}'),
                  contentPadding: EdgeInsets.zero,
                  value: p,
                  title: Text(title(p)),
                  subtitle: Text(note(p)),
                ),
            ],
          ),
        ),
        if (_distributor == false &&
            prefs.androidPush == AndroidPush.unifiedPush)
          Text(t.androidPushNoDistributor, style: TextStyle(color: c.dim)),
        for (final name in refused)
          Padding(
            padding: const EdgeInsets.only(top: 4),
            child: Text(
              t.androidPushRefused(name),
              style: TextStyle(color: c.dim),
            ),
          ),
        if (prefs.androidPush == AndroidPush.stayConnected && widget.live)
          Align(
            alignment: AlignmentDirectional.centerStart,
            child: TextButton(
              onPressed: askBatteryExemption,
              child: Text(t.allowBackground),
            ),
          ),
      ],
    );
  }
}
