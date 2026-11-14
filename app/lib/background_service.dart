// The entry point of the Android foreground service (docs/CLIENTS.md
// section 2, docs/PUSH.md section 10, "Foreground service"): for phones
// without Google services, it keeps one WebSocket open per server in its
// own isolate, with a permanent notification, and shows notifications
// from the live events' `notify` hint. It never reports activity, so it
// never makes the person look online or counts as an active client.
//
// It does not write the servers' databases; the app catches up from the
// server when opened. It reads them only for names.

import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_foreground_task/flutter_foreground_task.dart';

import 'core/api/client.dart';
import 'core/db/cache_db.dart';
import 'core/live.dart';
import 'core/models/json.dart';
import 'core/models/message.dart';
import 'core/notify/content.dart';
import 'core/notify/decision.dart';
import 'core/session_store.dart';
import 'platform/secret_storage.dart';
import 'platform/notifications.dart';
import 'platform/paths.dart';
import 'state/account_prefs.dart';
import 'state/app_prefs.dart';
import 'state/notify_center.dart';
import 'state/sidebar_model.dart';

/// Started by `flutter_foreground_task` in the service's isolate.
@pragma('vm:entry-point')
void stayConnectedEntry() {
  FlutterForegroundTask.setTaskHandler(StayConnectedHandler());
}

/// One server's connection inside the service.
class ServiceServer implements LiveDelegate {
  ServiceServer(
    this.handler,
    this.localId,
    this.address,
    this.name,
    this.api,
    this.db,
  );
  final StayConnectedHandler handler;
  final String localId;
  final String address;
  final String name;
  final ApiClient api;
  final CacheDb db;
  late final LiveConnection live = LiveConnection(api: api, delegate: this);
  Map<String, int>? _positions;
  String? myId;

  @override
  Future<Map<String, int>> positions() async =>
      _positions ??= await db.positions();

  @override
  Future<void> onEvent(Json e, {required bool replay}) async {
    final id = e['conversation_id'] as String?;
    final seq = e['seq'];
    if (id != null && seq is int) {
      final p = await positions();
      if ((p[id] ?? 0) >= seq) return; // seen already
      p[id] = seq;
    }
    if (e['type'] != 'message.created' || e['notify'] != 'notify') return;
    final raw = object(e['message']);
    if (raw != null) await handler.show(this, Message.fromJson(raw));
  }

  @override
  Future<void> onReload(Json frame) async {}

  @override
  Future<void> onReady(List<String> newConversationIds) async {
    final p = await positions();
    for (final id in newConversationIds) {
      p.putIfAbsent(id, () => 0);
    }
  }
}

/// The service's work.
class StayConnectedHandler extends TaskHandler {
  final _servers = <ServiceServer>[];
  PlatformNotifier? _notifier;
  AppPrefs? _prefs;

  /// What the app's screen reports (`sendDataToTask`): whether it is in
  /// front, and the conversation open (`<server>/<conversation>`). While
  /// it is in front the app shows its own notifications.
  bool _uiInFront = false;
  String? _uiOpen;

  @override
  Future<void> onStart(DateTime timestamp, TaskStarter starter) async {
    WidgetsFlutterBinding.ensureInitialized();
    final storage = await DiskStorage.open();
    final appDb = storage.openAppDb();
    final prefs = _prefs = AppPrefs(appDb);
    await prefs.load();
    _notifier = await PlatformNotifier.start(onTap: (_) {});
    final sessions = SessionStore(platformSecretStore());
    for (final e in await appDb.servers()) {
      // The app and the service share one saved session per server:
      // each reads the store before using it and saves what it renews.
      final t = await sessions.load(e.localId);
      if (t == null) continue;
      final api = ApiClient(
        base: Uri.parse(e.address),
        tokens: t,
        onTokens: (n) => sessions.save(e.localId, n),
      );
      final s = ServiceServer(
        this,
        e.localId,
        e.address,
        e.name,
        api,
        await storage.openCache(e.localId),
      );
      s.myId = (await s.db.me())?.id;
      _servers.add(s);
      s.live.start();
    }
  }

  Future<void> show(ServiceServer s, Message m) async {
    final prefs = _prefs, n = _notifier;
    if (prefs == null || n == null) return;
    final v = decide(
      NotifyInputs(
        notify: 'notify',
        messageAt: m.createdAt,
        now: DateTime.now(),
        own: m.authorId == s.myId,
        notificationsOn: prefs.notificationsOn,
        serverMuted: prefs.isServerMuted(s.address, DateTime.now()),
        windowInFront: _uiInFront,
        conversationOnScreen: _uiOpen == '${s.localId}/${m.conversationId}',
      ),
    );
    if (v != NotifyVerdict.show || _uiInFront) return;
    final c = (await s.db.conversation(m.conversationId))?.conversation;
    final author = await s.db.account(m.authorId);
    final accounts = {?author?.id: ?author};
    await n.show(
      ShownNotification(
        key: groupKey(s.localId, m.conversationId),
        text: describeMessage(
          m,
          sender: author?.displayName ?? '…',
          channel: c != null && c.isNamed ? c.name : null,
          chatTitle: c == null || c.isNamed
              ? null
              : conversationName(c, accounts, s.myId),
          server: _servers.length > 1 ? s.name : null,
          showText: prefs.notificationText,
        ),
        channel: c != null && c.isNamed && m.parentId == null
            ? NotifyChannel.activity
            : NotifyChannel.direct,
        payload: TapTarget(
          serverLocalId: s.localId,
          conversationId: m.conversationId,
          messageId: m.id,
          parentId: m.parentId,
        ).encode(),
        sound: prefs.notificationSounds,
      ),
    );
  }

  @override
  void onReceiveData(Object data) {
    if (data is Map) {
      _uiInFront = data['front'] == true;
      _uiOpen = data['open'] as String?;
    }
  }

  @override
  void onRepeatEvent(DateTime timestamp) {}

  @override
  void onNotificationPressed() => FlutterForegroundTask.launchApp();

  @override
  Future<void> onDestroy(DateTime timestamp, bool isTimeout) async {
    for (final s in _servers) {
      await s.live.dispose();
      s.api.close();
      await s.db.close();
    }
    _servers.clear();
  }
}
