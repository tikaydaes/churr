// The entry point run when a push arrives on Android with no screen
// (docs/CLIENTS.md section 2, docs/PUSH.md section 8 "Android"): an FCM
// data message in the Play build, or a UnifiedPush message when the
// distributor started the app in the background (`--unifiedpush-bg`).
//
// It opens what it needs from disk (the list of servers, their tokens,
// the wake routes), fetches the wake-up from the right server, posts the
// notification itself, and asks for the status totals. It shares the
// tokens with the app through the key store; see the note on renewing in
// app/README.md ("Notifications").

import 'dart:convert';
import 'dart:io';

import 'package:flutter/widgets.dart';
import 'package:unifiedpush/unifiedpush.dart';

import 'core/api/client.dart';
import 'core/api/me.dart';
import 'core/notify/wake.dart';
import 'core/session_store.dart';
import 'platform/secret_storage.dart';
import 'platform/notifications.dart';
import 'platform/paths.dart';
import 'platform/push.dart';
import 'state/account_prefs.dart';
import 'state/app_prefs.dart';
import 'state/notify_prefs.dart';

/// The fields of a wake-up as both FCM data and UnifiedPush bodies carry
/// them: `e`/`event_id`, `c`/`collapse_key`, `b`/`badge`.
class WakeFields {
  const WakeFields(this.eventId, this.collapseKey, this.badge);
  final String eventId;
  final String? collapseKey;
  final int? badge;

  /// FCM data (docs/PUSH.md section 7).
  static WakeFields? fromFcm(Map<String, String> d) {
    final e = d['e'];
    if (e == null) return null;
    return WakeFields(e, d['c'], int.tryParse(d['b'] ?? ''));
  }

  /// A UnifiedPush body (docs/PUSH.md section 10): the JSON wake-up.
  static WakeFields? fromUnifiedPush(List<int> body) {
    try {
      final j = jsonDecode(utf8.decode(body)) as Map<String, dynamic>;
      final e = j['event_id'] as String?;
      if (e == null) return null;
      return WakeFields(
        e,
        j['collapse_key'] as String?,
        (j['badge'] as num?)?.toInt(),
      );
    } on Object {
      return null;
    }
  }
}

/// Handles one wake-up with no app running: Firebase's background
/// handler and the UnifiedPush background start both end here.
@pragma('vm:entry-point')
Future<void> handleWakeInBackground(WakeFields w) async {
  WidgetsFlutterBinding.ensureInitialized();
  final storage = await DiskStorage.open();
  final appDb = storage.openAppDb();
  final prefs = AppPrefs(appDb);
  await prefs.load();
  final sessions = SessionStore(platformSecretStore());
  final servers = await appDb.servers();
  final clients = <String, ApiClient>{};
  for (final s in servers) {
    final t = await sessions.load(s.localId);
    if (t == null) continue;
    clients[s.localId] = ApiClient(
      base: Uri.parse(s.address),
      tokens: t,
      onTokens: (n) => sessions.save(s.localId, n),
    );
  }
  final names = {for (final s in servers) s.localId: s.name};
  final routes = {for (final e in prefs.wakeRoutes.entries) e.value: e.key};
  final notifier = await PlatformNotifier.start(onTap: (_) {});
  try {
    final d = await WakeHandler(
      routes: routes,
      clientFor: (id) => clients[id],
      serverName: (id) => names[id] ?? '',
      multiServer: servers.length > 1,
      showText: prefs.notificationText,
    ).handle(w.eventId, badge: w.badge);
    if (notifier != null && prefs.notificationsOn) {
      await showWake(
        notifier,
        d,
        collapseKey: w.collapseKey,
        sound: prefs.notificationSounds,
      );
    }
    // After any wake-up: the status totals (docs/PUSH.md section 11).
    final api = d.serverLocalId == null ? null : clients[d.serverLocalId];
    if (api != null) {
      try {
        await api.getStatusSummary();
      } on Object {
        // The badge from the wake-up stands.
      }
    }
  } finally {
    for (final c in clients.values) {
      c.close();
    }
    await appDb.close();
  }
}

/// `main` with `--unifiedpush-bg`: the distributor started the app to
/// hand it a message. No window is drawn.
Future<void> startUnifiedPushInBackground() async {
  if (!Platform.isAndroid) return;
  WidgetsFlutterBinding.ensureInitialized();
  await UnifiedPush.initialize(
    onMessage: (m, _) {
      final w = WakeFields.fromUnifiedPush(m.content);
      if (w != null) handleWakeInBackground(w);
    },
  );
}
