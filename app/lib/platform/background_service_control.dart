import 'dart:io';

import 'package:flutter_foreground_task/flutter_foreground_task.dart';

import '../background_service.dart';
import 'notify_wording.dart';

/// Starting and stopping the Android stay-connected service
/// (`lib/background_service.dart`, docs/PUSH.md section 10). Its type is
/// `remoteMessaging` (AndroidManifest.xml): a connection to a messaging
/// server kept open, which Android 14 and later allow without the
/// six-hour limit put on `dataSync`. PUSH.md section 18 lists the type
/// as to be verified for F-Droid.
Future<void> startStayConnected(int servers) async {
  if (!Platform.isAndroid) return;
  final t = notifyL10n();
  FlutterForegroundTask.init(
    androidNotificationOptions: AndroidNotificationOptions(
      channelId: 'service',
      channelName: t.notifyChannelService,
      channelImportance: NotificationChannelImportance.MIN,
      priority: NotificationPriority.MIN,
    ),
    iosNotificationOptions: const IOSNotificationOptions(),
    foregroundTaskOptions: ForegroundTaskOptions(
      eventAction: ForegroundTaskEventAction.nothing(),
      autoRunOnBoot: true,
      autoRunOnMyPackageReplaced: true,
      allowWakeLock: true,
      allowWifiLock: true,
    ),
  );
  FlutterForegroundTask.initCommunicationPort();
  if (await FlutterForegroundTask.isRunningService) {
    await FlutterForegroundTask.updateService(
      notificationTitle: t.notifyServiceConnected(servers),
      notificationText: '',
    );
    return;
  }
  await FlutterForegroundTask.startService(
    serviceId: 7101,
    serviceTypes: const [ForegroundServiceTypes.remoteMessaging],
    notificationTitle: t.notifyServiceConnected(servers),
    notificationText: '',
    callback: stayConnectedEntry,
  );
}

Future<void> stopStayConnected() async {
  if (!Platform.isAndroid) return;
  try {
    if (await FlutterForegroundTask.isRunningService) {
      await FlutterForegroundTask.stopService();
    }
  } on Object {
    // Not running.
  }
}

/// Asks Android to let the service run in the background (without it,
/// Android stops the service).
Future<void> askBatteryExemption() async {
  if (!Platform.isAndroid) return;
  if (!await FlutterForegroundTask.isIgnoringBatteryOptimizations) {
    await FlutterForegroundTask.requestIgnoreBatteryOptimization();
  }
}

/// Tells the service whether the app's screen is in front, and which
/// conversation is open, so the two do not both notify.
void tellService({required bool front, String? open}) {
  if (!Platform.isAndroid) return;
  try {
    FlutterForegroundTask.sendDataToTask({'front': front, 'open': open});
  } on Object {
    // No service.
  }
}
