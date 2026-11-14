// Phone wake-ups (docs/PUSH.md sections 3, 8, 10 and 11): getting a push
// address from the platform, registering it on every signed-in server,
// handling a wake-up while the app runs, the status endpoint at the
// documented moments, and the Android stay-connected service.
//
// Push addresses:
// - iPhone: the APNs device token, from the app's own channel
//   (`ios/Runner/AppDelegate.swift`, "churr/push"), as CLIENTS.md section
//   3 asks ("the raw APNs device token on iPhone").
// - Android, Play build: the FCM token (`lib/platform/fcm.dart`, which the
//   checked-in tree stubs out).
// - Android with a UnifiedPush distributor: its endpoint, through
//   `unifiedpush` 6.2.0 (2025-10-13, published by unifiedpush.org, the
//   connector CLIENTS.md names; no Google library).
// - Android staying connected: `none`, and the foreground service of
//   `lib/background_service.dart` through `flutter_foreground_task`
//   11.0.3 (2026-09-07, the package CLIENTS.md names).
// - Desktop: `none`; the desktop app stays connected.

import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:unifiedpush/unifiedpush.dart';

import '../core/api/account.dart';
import '../core/api/errors.dart';
import '../core/live.dart';
import '../core/notify/content.dart';
import '../core/notify/registration.dart';
import '../core/notify/shared_items.dart';
import '../core/notify/wake.dart';
import '../push_background.dart';
import '../state/account_prefs.dart';
import '../state/app_state.dart';
import '../state/notify_center.dart';
import '../state/notify_prefs.dart';
import '../state/server_state.dart';
import 'background_service_control.dart';
import 'fcm.dart';
import 'notify_wording.dart';

/// The `platform` this build registers as.
String get platformName => Platform.isIOS
    ? 'ios'
    : Platform.isAndroid
    ? 'android'
    : Platform.isMacOS
    ? 'macos'
    : Platform.isWindows
    ? 'windows'
    : 'linux';

bool get isPhone => Platform.isIOS || Platform.isAndroid;

/// Puts one wake-up on screen (docs/PUSH.md section 8), through
/// [notifier]: the real message, a ring, a missed call, nothing (read
/// already, Android only), or "New message" under the collapse key so
/// repeated failures for one conversation do not stack up.
Future<void> showWake(
  Notifier notifier,
  WakeDisplay d, {
  String? collapseKey,
  bool sound = true,
}) async {
  final key = d.serverLocalId != null && d.conversationId != null
      ? '${d.serverLocalId}/${d.conversationId}'
      : 'wake/${collapseKey ?? 'unknown'}';
  final payload = d.serverLocalId == null || d.conversationId == null
      ? ''
      : TapTarget(
          serverLocalId: d.serverLocalId!,
          conversationId: d.conversationId!,
          messageId: d.messageId,
          parentId: d.parentId,
          callId: d.outcome == WakeOutcome.ring ? d.callId : null,
        ).encode();
  switch (d.outcome) {
    case WakeOutcome.nothing:
      await notifier.cancel(key);
    case WakeOutcome.message:
      await notifier.show(
        ShownNotification(
          key: key,
          text: d.text!,
          channel: NotifyChannel.direct,
          payload: payload,
          sound: sound,
        ),
      );
    case WakeOutcome.ring || WakeOutcome.missedCall:
      final missed = d.outcome == WakeOutcome.missedCall;
      await notifier.show(
        ShownNotification(
          key: key,
          text: placeholderText,
          channel: missed ? NotifyChannel.direct : NotifyChannel.call,
          callerName: d.callerName ?? '',
          missed: missed,
          sound: !missed,
          payload: payload,
        ),
      );
    case WakeOutcome.placeholder:
      await notifier.show(
        ShownNotification(
          key: key,
          text: placeholderText,
          channel: NotifyChannel.direct,
          payload: payload,
          sound: sound,
        ),
      );
  }
  if (d.badge != null) await notifier.setBadge(d.badge!);
}

/// The phone side, running with the app.
class PushService {
  PushService._(this.app, this.center);

  final AppState app;
  final NotifyCenter center;
  static const _channel = MethodChannel('churr/push');

  /// Addresses this device has, best first.
  PushAddress? _apns, _fcm, _unified;
  final Set<String> _registered = {};
  bool _disposed = false;

  /// The running service, for the settings screen.
  static PushService? current;

  static Future<PushService?> start(AppState app, NotifyCenter center) async {
    final p = PushService._(app, center);
    current = p;
    app.addListener(p._servers);
    if (Platform.isIOS) {
      _channel.setMethodCallHandler(p._fromIos);
      try {
        // Answers with the tap that opened the app, if one did.
        final tap = await _channel.invokeMethod<String>('register');
        if (tap != null) center.openTapped(tap);
      } on Object catch (e) {
        debugPrint('APNs registration not available: $e');
      }
    }
    if (Platform.isAndroid) await p._android();
    // Registered straight after sign-in, and on every start.
    unawaited(p.registerAll());
    if (isPhone) unawaited(p.refreshStatus());
    return p;
  }

  // ---- addresses ----

  Future<dynamic> _fromIos(MethodCall call) async {
    switch (call.method) {
      case 'token':
        final hex = call.arguments as String?;
        if (hex == null || hex.isEmpty) return null;
        final next = PushAddress(PushKind.apns, hex);
        if (_apns?.address != hex) {
          _apns = next;
          _registered.clear(); // a new address: register again everywhere
          await registerAll();
        }
      case 'tapped':
        // A notification the extension completed, tapped.
        final m = (call.arguments as Map?)?.cast<String, dynamic>();
        center.openTapped(m?['churr'] as String?);
      case 'wake':
        // A push that arrived while the app was in front: the extension
        // has fetched it; the app only brings its totals up to date.
        await refreshStatus();
      case 'backgroundRefresh':
        return refreshStatus(quietNewActivity: true);
    }
    return null;
  }

  Future<void> _android() async {
    final choice = app.prefs.androidPush;
    _fcm = null;
    _unified = null;
    if (choice == AndroidPush.automatic && fcmAvailable) {
      final token = await startFcm(
        onToken: (t) {
          _fcm = PushAddress(PushKind.fcm, t);
          _registered.clear();
          unawaited(registerAll());
        },
        onWake: (d) {
          final w = WakeFields.fromFcm(d);
          if (w != null) {
            unawaited(
              handleWake(w.eventId, badge: w.badge, collapseKey: w.collapseKey),
            );
          }
        },
      );
      if (token != null) _fcm = PushAddress(PushKind.fcm, token);
    }
    if (_fcm == null &&
        (choice == AndroidPush.automatic ||
            choice == AndroidPush.unifiedPush)) {
      try {
        await UnifiedPush.initialize(
          onNewEndpoint: (e, _) {
            _unified = PushAddress(
              PushKind.unifiedpush,
              e.url,
              publicKey: e.pubKeySet?.pubKey,
              auth: e.pubKeySet?.auth,
            );
            _registered.clear();
            unawaited(registerAll());
          },
          onUnregistered: (_) {
            _unified = null;
            _registered.clear();
            unawaited(registerAll());
          },
          onMessage: (m, _) {
            final w = WakeFields.fromUnifiedPush(m.content);
            if (w != null) {
              unawaited(
                handleWake(
                  w.eventId,
                  badge: w.badge,
                  collapseKey: w.collapseKey,
                ),
              );
            }
          },
        );
        if (await UnifiedPush.tryUseCurrentOrDefaultDistributor()) {
          await UnifiedPush.register();
        }
      } on Object catch (e) {
        debugPrint('UnifiedPush not available: $e');
      }
    }
    final stay = choice == AndroidPush.stayConnected;
    await (stay ? startStayConnected(app.servers.length) : stopStayConnected());
  }

  /// Whether a UnifiedPush distributor is installed (the choice screen).
  static Future<bool> hasDistributor() async {
    if (!Platform.isAndroid) return false;
    try {
      return (await UnifiedPush.getDistributors()).isNotEmpty;
    } on Object {
      return false;
    }
  }

  /// The person changed the Android choice.
  Future<void> androidChoiceChanged() async {
    if (!Platform.isAndroid) return;
    await _android();
    _registered.clear();
    await registerAll();
  }

  List<PushAddress> get _available => [?_apns, ?_fcm, ?_unified];

  // ---- registration ----

  void _servers() {
    if (_disposed) return;
    final ids = {for (final s in app.servers) s.localId};
    _registered.removeWhere((id) => !ids.contains(id));
    if (app.servers.any((s) => !_registered.contains(s.localId))) {
      unawaited(registerAll());
    }
  }

  bool _registering = false;

  /// Registers on every server not yet registered with the current
  /// address. Idempotent on the server's side.
  Future<void> registerAll() async {
    if (_registering) return;
    _registering = true;
    try {
      for (final s in List.of(app.servers)) {
        if (_registered.contains(s.localId)) continue;
        if (await registerOn(s)) _registered.add(s.localId);
      }
      await _share();
    } finally {
      _registering = false;
    }
  }

  /// Registers on one server; false when it could not be reached.
  Future<bool> registerOn(ServerState s) async {
    var accepted = s.info?.pushKinds;
    if (accepted == null) {
      await s.refreshInfo();
      accepted = s.info?.pushKinds ?? const [];
    }
    final push = choosePush(_available, accepted);
    try {
      final r = await registerThisDevice(
        s.api,
        name: deviceName(_deviceName()),
        platform: platformName,
        push: push,
      );
      app.prefs.setWakeRoute(s.localId, r.wakeRoute);
      app.prefs.setPushRefused(
        s.localId,
        r.refused || (_available.isNotEmpty && push.kind == PushKind.none),
      );
      if (r.deviceId != null) _deviceIds[s.localId] = r.deviceId!;
      return true;
    } on ApiException catch (e) {
      debugPrint('registerDevice on ${s.name}: ${e.code}');
      return e.code != 'network' && !e.isTransient;
    } on Object catch (e) {
      debugPrint('registerDevice on ${s.name}: $e');
      return false;
    }
  }

  final Map<String, String> _deviceIds = {};

  /// Signing out of a server: the device is removed there, which also
  /// ends the session, and its shared items go (docs/PUSH.md section 3).
  Future<void> removeFrom(ServerState s) async {
    final id = _deviceIds.remove(s.localId);
    if (id != null) {
      try {
        await s.api.removeDevice(id);
      } on Object {
        // Revoking the session, which follows, deletes the row anyway.
      }
    }
    app.prefs.setWakeRoute(s.localId, null);
    app.prefs.setPushRefused(s.localId, false);
    await forgetSharedEpochs(app.sessions.secrets, s.localId);
    _registered.remove(s.localId);
    await _share();
  }

  /// The server list for the iPhone extension (app/docs/NOTIFY-SHARED.md).
  Future<void> _share() async {
    if (!Platform.isIOS) return;
    final routes = app.prefs.wakeRoutes;
    await writeSharedServers(app.sessions.secrets, [
      for (final s in app.servers)
        if (routes[s.localId] != null)
          SharedServer(
            localId: s.localId,
            address: s.entry.address,
            name: s.name,
            route: routes[s.localId]!,
          ),
    ], showText: app.prefs.notificationText);
  }

  String _deviceName() {
    final host = Platform.localHostname;
    if (Platform.isIOS) return 'iPhone';
    if (Platform.isAndroid) return 'Android phone';
    return host.isEmpty ? 'Churr' : host;
  }

  // ---- wake-ups while running ----

  /// A wake-up that reached the running app (Android, or iPhone in
  /// front).
  Future<void> handleWake(
    String eventId, {
    int? badge,
    String? collapseKey,
  }) async {
    final routes = {
      for (final e in app.prefs.wakeRoutes.entries) e.value: e.key,
    };
    final d = await WakeHandler(
      routes: routes,
      clientFor: (id) => app.server(id)?.api,
      serverName: (id) => app.server(id)?.name ?? '',
      multiServer: app.servers.length > 1,
      showText: app.prefs.notificationText,
    ).handle(eventId, badge: badge);
    if (d.outcome == WakeOutcome.ring &&
        IncomingCallHook.openPrompt != null &&
        center.windowInFront) {
      IncomingCallHook.openPrompt!(
        d.serverLocalId!,
        d.conversationId!,
        d.callId!,
      );
    } else if (app.prefs.notificationsOn && !Platform.isIOS) {
      // On iPhone the extension has drawn it already.
      await showWake(
        center.notifier,
        d,
        collapseKey: collapseKey,
        sound: app.prefs.notificationSounds,
      );
    }
    final s = d.serverLocalId == null ? null : app.server(d.serverLocalId!);
    if (s != null) await s.sync.refreshStatusSummary();
  }

  // ---- the status endpoint ----

  /// Asks every server for its totals: on waking, on background refresh,
  /// when the app comes to the front. With [quietNewActivity], a server
  /// whose `newest` moved gets one quiet "New activity on …".
  Future<void> refreshStatus({bool quietNewActivity = false}) async {
    for (final s in List.of(app.servers)) {
      await s.sync.refreshStatusSummary();
      final newest = s.summary.newest;
      final before = app.prefs.lastNewest(s.localId);
      if (newest != null && newest != before) {
        app.prefs.setLastNewest(s.localId, newest);
        if (quietNewActivity &&
            before != null &&
            before.isNotEmpty &&
            app.prefs.notificationsOn) {
          final t = notifyL10n();
          await center.notifier.show(
            ShownNotification(
              key: 'activity/${s.localId}',
              text: NotificationText(
                sender: 'Churr',
                kind: BodyKind.text,
                text: t.notifyNewActivity(s.name),
              ),
              channel: NotifyChannel.activity,
              payload: '',
              sound: false,
            ),
          );
        }
      }
    }
  }

  /// The app came to the front: totals before the connection is up, and
  /// connect without waiting out the back-off.
  void resumed() {
    if (isPhone) unawaited(refreshStatus());
    for (final s in app.servers) {
      if (s.liveState != LiveState.ready) s.live.connectNow();
    }
  }

  void dispose() {
    _disposed = true;
    app.removeListener(_servers);
  }
}
