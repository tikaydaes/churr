import 'dart:convert';

import '../core/notify/registration.dart';
import 'app_prefs.dart';

/// The notification area's app preferences (docs/CLIENTS.md 9.12 "For
/// this device" and 9.23), kept in `app.db` beside the others.
extension NotifyPrefs on AppPrefs {
  /// Desktop: start when the person logs in, with the window hidden. Off
  /// out of the box (docs/PUSH.md section 13).
  bool get startAtLogin => raw('start_at_login') == '1';
  set startAtLogin(bool v) => putRaw('start_at_login', v ? '1' : '0');

  /// Desktop: closing the window hides it and the app stays connected in
  /// the tray. On out of the box, as PUSH.md section 13 describes.
  bool get closeToTray => raw('close_to_tray') != '0';
  set closeToTray(bool v) => putRaw('close_to_tray', v ? '1' : '0');

  /// Android: how the phone is told (docs/PUSH.md section 10).
  AndroidPush get androidPush => AndroidPush.values.firstWhere(
    (e) => e.name == raw('android_push'),
    orElse: () => AndroidPush.automatic,
  );
  set androidPush(AndroidPush v) => putRaw('android_push', v.name);

  /// Each server's wake route (local id to route), as the last
  /// registration returned it.
  Map<String, String> get wakeRoutes {
    try {
      return (jsonDecode(raw('wake_routes') ?? '{}') as Map).map(
        (k, v) => MapEntry(k as String, v as String),
      );
    } on Object {
      return {};
    }
  }

  void setWakeRoute(String localId, String? route) {
    final next = wakeRoutes;
    route == null ? next.remove(localId) : next[localId] = route;
    putRaw('wake_routes', jsonEncode(next));
  }

  /// Servers that refused this device's push kind at the last
  /// registration (local ids): the push choice screen names them.
  Set<String> get pushRefused {
    try {
      return {
        ...(jsonDecode(raw('push_refused') ?? '[]') as List).cast<String>(),
      };
    } on Object {
      return {};
    }
  }

  void setPushRefused(String localId, bool refused) {
    final next = pushRefused;
    refused ? next.add(localId) : next.remove(localId);
    putRaw('push_refused', jsonEncode(next.toList()));
  }

  /// The `newest` of each server's status as last seen, for the quiet
  /// "New activity" notification after a background refresh.
  String? lastNewest(String localId) => raw('newest.$localId');
  void setLastNewest(String localId, String? v) =>
      putRaw('newest.$localId', v ?? '');
}
