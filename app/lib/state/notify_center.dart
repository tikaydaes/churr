import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';

import '../core/api/approvals.dart';
import '../core/db/cache_db.dart';
import '../core/live.dart';
import '../core/models/chat_request.dart';
import '../core/models/json.dart';
import '../core/models/message.dart';
import '../core/notify/content.dart';
import '../core/notify/decision.dart';
import '../core/notify/tray_state.dart';
import 'account_prefs.dart';
import 'app_state.dart';
import 'server_state.dart';
import 'sidebar_model.dart';

/// How loud a notification is.
enum NotifyChannel {
  /// A mention, a chat with people, a followed reply: Android's
  /// "Mentions and chats" channel.
  direct,

  /// Chatter in a named channel on Any activity: "Channel activity".
  activity,

  /// A ringing call: "Calls", with the ring sound.
  call,

  /// "Send a test notification".
  test,
}

/// One notification to put on screen.
class ShownNotification {
  const ShownNotification({
    required this.key,
    required this.text,
    required this.channel,
    required this.payload,
    this.sound = true,
    this.callerName,
    this.missed = false,
  });

  /// [groupKey] of its server and conversation: notifications of one
  /// conversation are grouped under it and a newer one replaces the older
  /// where the platform shows one per key.
  final String key;
  final NotificationText text;
  final NotifyChannel channel;

  /// What tapping it opens ([TapTarget.encode]).
  final String payload;
  final bool sound;

  /// For a ring or a missed call.
  final String? callerName;
  final bool missed;
}

/// What a tap on a notification opens.
class TapTarget {
  const TapTarget({
    required this.serverLocalId,
    required this.conversationId,
    this.messageId,
    this.parentId,
    this.callId,
  });

  final String serverLocalId;
  final String conversationId;
  final String? messageId;
  final String? parentId;

  /// Set for a ring: opening it shows the incoming-call prompt.
  final String? callId;

  String encode() => jsonEncode({
    's': serverLocalId,
    'c': conversationId,
    'm': ?messageId,
    'p': ?parentId,
    'call': ?callId,
  });

  static TapTarget? decode(String? payload) {
    if (payload == null || payload.isEmpty) return null;
    try {
      final j = jsonDecode(payload) as Map<String, dynamic>;
      final s = j['s'] as String?, c = j['c'] as String?;
      if (s == null || c == null) return null;
      return TapTarget(
        serverLocalId: s,
        conversationId: c,
        messageId: j['m'] as String?,
        parentId: j['p'] as String?,
        callId: j['call'] as String?,
      );
    } on Object {
      return null;
    }
  }
}

/// The operating system's notifications and icon badge, as the app uses
/// them. `lib/platform/notifications.dart` is the real one; tests give a
/// fake.
abstract class Notifier {
  Future<void> show(ShownNotification n);

  /// Removes what is shown under [key].
  Future<void> cancel(String key);

  /// The icon badge (the dock on macOS, the app icon on phones); 0 clears.
  Future<void> setBadge(int count);
}

/// A [Notifier] that shows nothing: tests, and platforms that failed to
/// start their notifications.
class SilentNotifier implements Notifier {
  @override
  Future<void> show(ShownNotification n) async {}
  @override
  Future<void> cancel(String key) async {}
  @override
  Future<void> setBadge(int count) async {}
}

/// The incoming-call prompt belongs to the calls code
/// (app/docs/NOTIFY-SHARED.md). It sets [openPrompt]; a tap on a ring
/// notification, or a ring found on waking, calls it.
abstract final class IncomingCallHook {
  static void Function(
    String serverLocalId,
    String conversationId,
    String callId,
  )?
  openPrompt;
}

/// Shows notifications for events the server marked `notify` while the
/// app is running, removes them when they are read here or elsewhere,
/// keeps the icon badge at the servers' total, and works out the tray's
/// state (docs/PUSH.md sections 12 and 13).
class NotifyCenter extends ChangeNotifier {
  NotifyCenter(this.app, {Notifier? notifier, DateTime Function()? now})
    : notifier = notifier ?? SilentNotifier(),
      _now = now ?? DateTime.now;

  static final _byApp = Expando<NotifyCenter>();

  /// The one center of [app], made on first use.
  static NotifyCenter of(AppState app) =>
      _byApp[app] ??= NotifyCenter(app)..start();

  /// Puts [center] in place for its app (main.dart with the platform's
  /// notifier; tests with a fake).
  static void install(NotifyCenter center) {
    _byApp[center.app]?.dispose();
    _byApp[center.app] = center;
    center.start();
  }

  final AppState app;
  Notifier notifier;
  final DateTime Function() _now;

  /// The app's window is in front and focused (on phones: the app is in
  /// the foreground). Kept current by `NotifyWatcher` (lib/ui).
  bool windowInFront = true;

  /// True while another process shows notifications when the app is not
  /// in front (the Android stay-connected service).
  bool Function()? deferWhenAway;

  /// Notifications shown, by server local id: conversation ids.
  final Map<String, Set<String>> _shown = {};
  final Map<String, List<StreamSubscription<Object?>>> _subs = {};
  final Map<String, LiveState> _lastLive = {};
  bool _started = false;
  bool _disposed = false;
  int? _badge;
  TrayState? _tray;
  String? _traySig;

  static String _signature(TrayState t) => [
    t.icon.name,
    t.count,
    for (final s in t.servers)
      '${s.localId}:${s.name}:${s.badge}:${s.connected}:${s.dnd}:${s.muted}',
  ].join('|');

  /// What the tray icon shows now.
  TrayState get tray => _tray ?? trayState(const []);

  void start() {
    if (_started) return;
    _started = true;
    app.addListener(_changed);
    app.prefs.addListener(_changed);
    _changed();
  }

  void _changed() {
    if (_disposed) return;
    final ids = {for (final s in app.servers) s.localId};
    for (final id in _subs.keys.toList()) {
      if (!ids.contains(id)) {
        for (final s in _subs.remove(id)!) {
          s.cancel();
        }
        _shown.remove(id);
        _lastLive.remove(id);
      }
    }
    for (final s in app.servers) {
      _subs[s.localId] ??= [
        s.sync.localEvents.listen((e) => _onLocal(s, e)),
        s.db
            .watchNotifications(clearedSince: _now())
            .listen((rows) => _onItems(s, rows)),
        s.live.states.listen((v) => _onLive(s, v)),
      ];
    }
    final badge = app.totalBadge;
    if (badge != _badge) {
      _badge = badge;
      unawaited(notifier.setBadge(badge));
    }
    final next = trayState([for (final s in app.servers) trayServer(s)]);
    final sig = _signature(next);
    if (sig != _traySig) {
      _tray = next;
      _traySig = sig;
      notifyListeners();
    }
  }

  /// One server as the tray sees it.
  TrayServer trayServer(ServerState s) {
    var direct = 0;
    for (final r in s.conversations) {
      final c = r.conversation;
      if (!c.isNamed) direct += c.membership?.badgeCount ?? 0;
    }
    return TrayServer(
      localId: s.localId,
      name: s.name,
      badge: s.summary.badge,
      mentions: s.summary.mentions,
      directBadge: direct,
      unreadConversations: s.summary.unreadConversations,
      connected: s.liveState == LiveState.ready,
      dnd: s.me?.dndOn ?? false,
      muted: app.prefs.isServerMuted(s.entry.address, _now()),
    );
  }

  // ---- showing ----

  void _onLocal(ServerState s, Json e) {
    if (e['type'] == 'local.request') {
      final r = object(e['request']);
      if (r != null) unawaited(_considerRequest(s, ChatRequest.fromJson(r)));
      return;
    }
    if (e['type'] == 'approval.created') {
      final a = object(e['approval']);
      if (a != null) unawaited(_considerApproval(s, Approval(a)));
      return;
    }
    if (e['type'] == 'knock.created') {
      final k = object(e['knock']);
      if (k != null) {
        unawaited(_considerKnock(s, k, e['conversation_name'] as String?));
      }
      return;
    }
    if (e['type'] != 'local.notify') return;
    final raw = object(e['message']);
    if (raw == null) return;
    unawaited(_consider(s, Message.fromJson(raw)));
  }

  /// Decides and, if the answer is yes, shows. Returns the verdict.
  Future<NotifyVerdict> _consider(ServerState s, Message m) async {
    final open = app.open;
    final verdict = decide(
      NotifyInputs(
        notify: 'notify',
        messageAt: m.createdAt,
        now: _now(),
        own: m.authorId == s.me?.id,
        notificationsOn: app.prefs.notificationsOn,
        serverMuted: app.prefs.isServerMuted(s.entry.address, _now()),
        dnd: s.me?.dndOn ?? false,
        windowInFront: windowInFront,
        conversationOnScreen:
            open != null &&
            open.serverId == s.localId &&
            open.conversationId == m.conversationId,
      ),
    );
    if (verdict != NotifyVerdict.show) return verdict;
    // Android staying connected: the service shows it while away.
    if (!windowInFront && (deferWhenAway?.call() ?? false)) return verdict;
    final c = s.conversation(m.conversationId);
    final sender = s.accounts[m.authorId]?.displayName;
    final text = describeMessage(
      m,
      sender: sender == null || sender.isEmpty ? '…' : sender,
      channel: c != null && c.isNamed ? c.name : null,
      chatTitle: c == null || c.isNamed
          ? null
          : conversationName(c, s.accounts, s.me?.id),
      server: app.servers.length > 1 ? s.name : null,
      showText: app.prefs.notificationText,
    );
    final named = c?.isNamed ?? false;
    final mine = m.entities.any(
      (x) => x.type == 'mention' && x.accountId == s.me?.id,
    );
    final key = groupKey(s.localId, m.conversationId);
    (_shown[s.localId] ??= {}).add(m.conversationId);
    await notifier.show(
      ShownNotification(
        key: key,
        text: text,
        channel: named && !mine && m.parentId == null
            ? NotifyChannel.activity
            : NotifyChannel.direct,
        payload: TapTarget(
          serverLocalId: s.localId,
          conversationId: m.conversationId,
          messageId: m.id,
          parentId: m.parentId,
        ).encode(),
        sound: app.prefs.notificationSounds,
      ),
    );
    return verdict;
  }

  /// A chat request, notified once like a mention (ruling R78): the
  /// server said `notify`, so do not disturb is already accounted for on
  /// its side; this device's own switches still apply. It shows who is
  /// asking and nothing more, and opens the list of requests.
  Future<NotifyVerdict> _considerRequest(ServerState s, ChatRequest r) async {
    final verdict = decide(
      NotifyInputs(
        notify: 'notify',
        messageAt: r.createdAt ?? _now(),
        now: _now(),
        own: false,
        notificationsOn: app.prefs.notificationsOn,
        serverMuted: app.prefs.isServerMuted(s.entry.address, _now()),
        dnd: s.me?.dndOn ?? false,
        windowInFront: windowInFront,
        conversationOnScreen: false,
      ),
    );
    if (verdict != NotifyVerdict.show) return verdict;
    final name = r.account.displayName.isEmpty
        ? r.account.username
        : r.account.displayName;
    await notifier.show(
      ShownNotification(
        key: 'request:${s.localId}:${r.id}',
        text: NotificationText(
          sender: name,
          kind: BodyKind.request,
          server: app.servers.length > 1 ? s.name : null,
        ),
        channel: NotifyChannel.direct,
        payload: '',
        sound: app.prefs.notificationSounds,
      ),
    );
    return verdict;
  }

  /// The verdict for something addressed to the person alone (a chat
  /// request, a request for approval, a request to join): the person's
  /// own switches apply, as for a mention.
  NotifyVerdict _personal(
    ServerState s,
    DateTime? at, {
    bool onScreen = false,
  }) => decide(
    NotifyInputs(
      notify: 'notify',
      messageAt: at ?? _now(),
      now: _now(),
      own: false,
      notificationsOn: app.prefs.notificationsOn,
      serverMuted: app.prefs.isServerMuted(s.entry.address, _now()),
      dnd: s.me?.dndOn ?? false,
      windowInFront: windowInFront,
      conversationOnScreen: onScreen,
    ),
  );

  /// A new request for the person to approve (FEATURES.md section 31):
  /// who asks and what for. It opens nothing; the Approvals entry lists
  /// it.
  Future<NotifyVerdict> _considerApproval(ServerState s, Approval a) async {
    final verdict = _personal(s, a.createdAt);
    if (verdict != NotifyVerdict.show) return verdict;
    final who = s.accounts[a.requesterId] ?? a.requester;
    await notifier.show(
      ShownNotification(
        key: 'approval:${s.localId}:${a.id}',
        text: NotificationText(
          sender: who?.name ?? a.summary,
          kind: BodyKind.approval,
          text: a.summary,
          server: app.servers.length > 1 ? s.name : null,
        ),
        channel: NotifyChannel.direct,
        payload: '',
        sound: app.prefs.notificationSounds,
      ),
    );
    return verdict;
  }

  /// A request to join a channel the person moderates or manages (rulings
  /// R74, R99): who asks and where. It opens the channel, whose bar lists
  /// the requests; nothing is shown while that channel is on screen.
  Future<NotifyVerdict> _considerKnock(
    ServerState s,
    Json k,
    String? channelName,
  ) async {
    final cid = k['conversation_id'] as String? ?? '';
    final open = app.open;
    final verdict = _personal(
      s,
      parseTime(k['created_at']),
      onScreen:
          open != null &&
          open.serverId == s.localId &&
          open.conversationId == cid,
    );
    if (verdict != NotifyVerdict.show) return verdict;
    final who = s.accounts[k['account_id']];
    await notifier.show(
      ShownNotification(
        key: 'knock:${s.localId}:$cid:${k['account_id']}',
        text: NotificationText(
          sender: who?.name ?? '…',
          kind: BodyKind.knock,
          channel: channelName ?? s.conversation(cid)?.name,
          server: app.servers.length > 1 ? s.name : null,
        ),
        channel: NotifyChannel.direct,
        payload: cid.isEmpty
            ? ''
            : TapTarget(serverLocalId: s.localId, conversationId: cid).encode(),
        sound: app.prefs.notificationSounds,
      ),
    );
    return verdict;
  }

  /// For tests: runs the decision for a request for approval as if its
  /// event had arrived.
  @visibleForTesting
  Future<NotifyVerdict> considerApprovalForTest(ServerState s, Approval a) =>
      _considerApproval(s, a);

  /// For tests: runs the decision for a request to join as if its event
  /// had arrived.
  @visibleForTesting
  Future<NotifyVerdict> considerKnockForTest(
    ServerState s,
    Json knock, {
    String? channelName,
  }) => _considerKnock(s, knock, channelName);

  /// For tests: runs the decision for a request as if its event had
  /// arrived marked `notify`.
  @visibleForTesting
  Future<NotifyVerdict> considerRequestForTest(ServerState s, ChatRequest r) =>
      _considerRequest(s, r);

  /// For tests: runs the decision for a message as if its event had
  /// arrived marked `notify`.
  @visibleForTesting
  Future<NotifyVerdict> considerForTest(ServerState s, Message m) =>
      _consider(s, m);

  // ---- clearing ----

  void _onItems(ServerState s, List<NotificationRow> rows) {
    final shown = _shown[s.localId];
    if (shown == null || shown.isEmpty) return;
    final gone = conversationsToClear(shown, [
      for (final r in rows) PendingItem(r.conversationId, cleared: r.cleared),
    ]);
    for (final c in gone) {
      shown.remove(c);
      unawaited(notifier.cancel(groupKey(s.localId, c)));
    }
  }

  // ---- status after reconnecting ----

  void _onLive(ServerState s, LiveState v) {
    final before = _lastLive[s.localId];
    _lastLive[s.localId] = v;
    // After reconnecting, the totals set the tray and the dock badge
    // before catch-up finishes (docs/PUSH.md section 11).
    if (v == LiveState.connecting && before != null && before != v) {
      unawaited(s.sync.refreshStatusSummary());
    }
    _changed();
  }

  // ---- taps ----

  /// Opens what a tapped notification points at.
  void openTapped(String? payload) {
    final t = TapTarget.decode(payload);
    if (t == null || app.server(t.serverLocalId) == null) return;
    if (t.callId != null && IncomingCallHook.openPrompt != null) {
      IncomingCallHook.openPrompt!(
        t.serverLocalId,
        t.conversationId,
        t.callId!,
      );
      return;
    }
    final target = OpenTarget(t.serverLocalId, t.conversationId);
    if (t.messageId != null) {
      app.openMessage(target, t.messageId!, parentId: t.parentId);
    } else {
      app.openConversation(target);
    }
  }

  // ---- calls ----

  /// A ring while the app is not in front: a notification on the calls
  /// channel with the ring sound; tapping it opens the calls code's
  /// incoming-call prompt (docs/PUSH.md section 13, "Incoming calls").
  /// The calls code calls this, and [stopRing] when the ring ends.
  Future<void> showRing({
    required String serverLocalId,
    required String conversationId,
    required String callId,
    required String callerName,
  }) async {
    if (!app.prefs.notificationsOn) return;
    await notifier.show(
      ShownNotification(
        key: groupKey(serverLocalId, conversationId),
        text: NotificationText(sender: callerName, kind: BodyKind.text),
        channel: NotifyChannel.call,
        callerName: callerName,
        payload: TapTarget(
          serverLocalId: serverLocalId,
          conversationId: conversationId,
          callId: callId,
        ).encode(),
      ),
    );
  }

  /// The ring stopped: answered here or elsewhere removes it; otherwise
  /// it becomes "Missed call from Sam".
  Future<void> stopRing({
    required String serverLocalId,
    required String conversationId,
    required String callerName,
    required bool answered,
  }) async {
    final key = groupKey(serverLocalId, conversationId);
    if (answered) return notifier.cancel(key);
    await notifier.show(
      ShownNotification(
        key: key,
        text: NotificationText(sender: callerName, kind: BodyKind.text),
        channel: NotifyChannel.direct,
        callerName: callerName,
        missed: true,
        sound: false,
        payload: TapTarget(
          serverLocalId: serverLocalId,
          conversationId: conversationId,
        ).encode(),
      ),
    );
  }

  /// "Send a test notification" (docs/CLIENTS.md 9.12).
  Future<void> sendTest() => notifier.show(
    ShownNotification(
      key: 'test',
      text: const NotificationText(sender: 'Churr', kind: BodyKind.test),
      channel: NotifyChannel.test,
      payload: '',
      sound: app.prefs.notificationSounds,
    ),
  );

  @override
  void dispose() {
    _disposed = true;
    app.removeListener(_changed);
    app.prefs.removeListener(_changed);
    for (final l in _subs.values) {
      for (final s in l) {
        s.cancel();
      }
    }
    super.dispose();
  }
}
