// The operating system's notifications on all five targets, through
// `flutter_local_notifications`, and the icon badge through the app's own
// small channel (docs/CLIENTS.md section 3: "the icon badge on desktop").
//
// Why this package: it is the one maintained package that shows
// notifications on Android, iOS, macOS, Windows and Linux alike (23.0.2,
// published 2026-11-13, not discontinued, checked on pub.dev 2026-11-14),
// it is the one CLIENTS.md names, and its Android part needs no Google
// library, so the F-Droid build can use it.

import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

import '../core/notify/decision.dart';
import '../state/notify_center.dart';
import 'notify_wording.dart';

/// Android notification channels (docs/PUSH.md section 8): the person
/// can tune each in Android's settings.
abstract final class AndroidChannels {
  static const direct = 'direct';
  static const activity = 'activity';
  static const calls = 'calls';
  static const service = 'service';
}

/// The icon badge, drawn by the app's own code on iOS and macOS
/// (`AppDelegate.swift` in each). Android takes its launcher count from
/// the notifications; Windows and Linux show the number in the tray.
abstract final class IconBadge {
  static const _channel = MethodChannel('churr/badge');

  static Future<void> set(int count) async {
    if (!(Platform.isIOS || Platform.isMacOS)) return;
    try {
      await _channel.invokeMethod<void>('set', count);
    } on Object {
      // No badge is not an error.
    }
  }
}

/// The real [Notifier].
class PlatformNotifier implements Notifier {
  PlatformNotifier._();

  final _plugin = FlutterLocalNotificationsPlugin();

  /// The last lines shown per conversation, for Android's messaging
  /// style, which adds new lines to one notification.
  final Map<String, List<Message>> _lines = {};

  /// Starts the plugin. [onTap] gets the payload of a tapped
  /// notification; one that started the app is handed over at once.
  /// Null when notifications cannot work here.
  static Future<PlatformNotifier?> start({
    required void Function(String? payload) onTap,
    bool askPermission = false,
  }) async {
    final n = PlatformNotifier._();
    try {
      final ok = await n._plugin.initialize(
        settings: InitializationSettings(
          android: const AndroidInitializationSettings('ic_notification'),
          iOS: DarwinInitializationSettings(
            requestAlertPermission: askPermission,
            requestBadgePermission: askPermission,
            requestSoundPermission: askPermission,
          ),
          macOS: DarwinInitializationSettings(
            requestAlertPermission: askPermission,
            requestBadgePermission: askPermission,
            requestSoundPermission: askPermission,
          ),
          linux: const LinuxInitializationSettings(defaultActionName: 'Open'),
          windows: const WindowsInitializationSettings(
            appName: 'Churr',
            // Placeholders that go with the bundle identifier
            // (app/README.md): change them together.
            appUserModelId: 'example.notfinal.churr',
            guid: '6f0d3a52-5b1e-4c55-9d7c-3f1f6b0e8a21',
          ),
        ),
        onDidReceiveNotificationResponse: (r) => onTap(r.payload),
      );
      if (ok == false) return null;
      if (Platform.isAndroid) await n._androidChannels();
      final launch = await n._plugin.getNotificationAppLaunchDetails();
      if (launch?.didNotificationLaunchApp ?? false) {
        final p = launch!.notificationResponse?.payload;
        scheduleMicrotask(() => onTap(p));
      }
      return n;
    } on Object catch (e) {
      debugPrint('Notifications are not available: $e');
      return null;
    }
  }

  Future<void> _androidChannels() async {
    final a = _plugin
        .resolvePlatformSpecificImplementation<
          AndroidFlutterLocalNotificationsPlugin
        >();
    if (a == null) return;
    final t = notifyL10n();
    await a.createNotificationChannel(
      AndroidNotificationChannel(
        AndroidChannels.direct,
        t.notifyChannelDirect,
        importance: Importance.high,
      ),
    );
    await a.createNotificationChannel(
      AndroidNotificationChannel(
        AndroidChannels.activity,
        t.notifyChannelActivity,
      ),
    );
    await a.createNotificationChannel(
      AndroidNotificationChannel(
        AndroidChannels.calls,
        t.notifyChannelCalls,
        importance: Importance.max,
        sound: const RawResourceAndroidNotificationSound('churr_ring'),
        audioAttributesUsage: AudioAttributesUsage.notificationRingtone,
      ),
    );
  }

  /// Asks for permission to notify (the first-run screen, docs/CLIENTS.md
  /// 9.18; Android 13 and later; iOS and macOS).
  Future<bool> requestPermission() async {
    try {
      if (Platform.isAndroid) {
        return await _plugin
                .resolvePlatformSpecificImplementation<
                  AndroidFlutterLocalNotificationsPlugin
                >()
                ?.requestNotificationsPermission() ??
            false;
      }
      if (Platform.isIOS) {
        return await _plugin
                .resolvePlatformSpecificImplementation<
                  IOSFlutterLocalNotificationsPlugin
                >()
                ?.requestPermissions(alert: true, badge: true, sound: true) ??
            false;
      }
      if (Platform.isMacOS) {
        return await _plugin
                .resolvePlatformSpecificImplementation<
                  MacOSFlutterLocalNotificationsPlugin
                >()
                ?.requestPermissions(alert: true, badge: true, sound: true) ??
            false;
      }
    } on Object {
      return false;
    }
    return true;
  }

  bool _asked = false;

  @override
  Future<void> show(ShownNotification n) async {
    // Permission is asked for the first time something is to be shown
    // (on iPhone, macOS and Android 13 and later), not at start.
    if (!_asked) {
      _asked = true;
      await requestPermission();
    }
    final t = notifyL10n();
    final apple = Platform.isIOS || Platform.isMacOS;
    var words = wordNotification(t, n.text, subtitleForServer: apple);
    if (n.callerName != null) {
      words = NotifyWords(
        n.missed
            ? t.notifyMissedCall(n.callerName!)
            : t.notifyCalling(n.callerName!),
        n.missed ? '' : t.notifyCallBody,
      );
    }
    final ring = n.channel == NotifyChannel.call && !n.missed;
    final id = notificationId(n.key);
    final lines = _lines[n.key] ??= [];
    if (n.callerName == null && n.channel != NotifyChannel.test) {
      lines.add(
        Message(words.body, DateTime.now(), Person(name: n.text.sender)),
      );
      if (lines.length > 5) lines.removeAt(0);
    }
    final details = NotificationDetails(
      android: AndroidNotificationDetails(
        switch (n.channel) {
          NotifyChannel.call => AndroidChannels.calls,
          NotifyChannel.activity => AndroidChannels.activity,
          _ => AndroidChannels.direct,
        },
        switch (n.channel) {
          NotifyChannel.call => t.notifyChannelCalls,
          NotifyChannel.activity => t.notifyChannelActivity,
          _ => t.notifyChannelDirect,
        },
        tag: n.key,
        groupKey: n.key,
        silent: !n.sound,
        category: ring
            ? AndroidNotificationCategory.call
            : AndroidNotificationCategory.message,
        fullScreenIntent: ring,
        ongoing: ring,
        timeoutAfter: ring ? 45000 : null,
        styleInformation: lines.isEmpty || n.callerName != null
            ? null
            : MessagingStyleInformation(
                const Person(name: 'You'),
                conversationTitle: words.title,
                groupConversation: n.text.channel != null,
                messages: List.of(lines),
              ),
      ),
      iOS: DarwinNotificationDetails(
        threadIdentifier: n.key,
        subtitle: words.subtitle,
        presentSound: n.sound,
        sound: ring ? 'churr-ring.wav' : null,
        interruptionLevel: ring
            ? InterruptionLevel.timeSensitive
            : InterruptionLevel.active,
      ),
      macOS: DarwinNotificationDetails(
        threadIdentifier: n.key,
        subtitle: words.subtitle,
        presentSound: n.sound,
        sound: ring ? 'churr-ring.wav' : null,
      ),
      linux: LinuxNotificationDetails(
        suppressSound: !n.sound,
        category: LinuxNotificationCategory.imReceived,
        urgency: ring ? LinuxNotificationUrgency.critical : null,
      ),
    );
    await _plugin.show(
      id: id,
      title: words.title,
      body: words.body,
      notificationDetails: details,
      payload: n.payload,
    );
  }

  @override
  Future<void> cancel(String key) async {
    _lines.remove(key);
    try {
      await _plugin.cancel(id: notificationId(key), tag: key);
    } on Object {
      // Already gone.
    }
  }

  @override
  Future<void> setBadge(int count) => IconBadge.set(count);
}
