import 'dart:async';

import 'package:flutter/foundation.dart';

import '../core/account_sync.dart';
import '../core/api/account.dart';
import '../core/api/errors.dart';
import '../core/db/cache_db.dart';
import '../core/models/conversation.dart';
import '../core/models/message.dart';
import 'app_state.dart';
import 'server_state.dart';

/// Which kinds the list shows (docs/CLIENTS.md 9.24, "Filter").
enum NotificationKinds { all, mentions, threads }

/// One entry of the notifications list: one item, or several from one
/// conversation (and thread) that arrived together, folded with a count.
class NotificationEntry {
  NotificationEntry(this.server, this.rows, this.reason);
  final ServerState server;

  /// Newest first. The entry shows the newest.
  final List<NotificationRow> rows;
  final NotificationReason reason;

  NotificationRow get newest => rows.first;
  int get count => rows.length;
  String get conversationId => newest.conversationId;
  String? get parentId => newest.parentId;
  Message? get message => newest.message;
  bool get cleared => newest.cleared;
  Conversation? get conversation => server.conversation(conversationId);

  /// A key unique across servers: the server's local id is part of it.
  String get key => '${server.localId}/${newest.messageId}';
}

/// The one notifications list across every signed-in server
/// (docs/CLIENTS.md 8.3 and 9.24; ruling R48).
///
/// Each server's items are in its own `cache.db`, written by sync from
/// the `notify` mark on message events; this controller watches them all
/// and merges them newest first. What goes in is decided by mode alone,
/// so hidden conversations are here like any other.
class NotificationsController extends ChangeNotifier {
  NotificationsController(this.app, {DateTime Function()? now})
    : _now = now ?? DateTime.now;

  final AppState app;
  final DateTime Function() _now;

  /// All servers (null) or one, by local id.
  String? serverFilter;
  NotificationKinds kinds = NotificationKinds.all;

  final _rows = <String, List<NotificationRow>>{};
  final _subs = <String, StreamSubscription<List<NotificationRow>>>{};
  bool _disposed = false;

  /// Items from one conversation this close together fold into one.
  static const foldWindow = Duration(minutes: 30);

  /// "Earlier" holds items cleared in the last week, on this device only.
  static const earlierFor = Duration(days: 7);

  void start() {
    app.addListener(_servers);
    _servers();
  }

  void _servers() {
    final ids = {for (final s in app.servers) s.localId};
    for (final id in _subs.keys.toList()) {
      if (!ids.contains(id)) {
        _subs.remove(id)?.cancel();
        _rows.remove(id);
      }
    }
    for (final s in app.servers) {
      _subs[s.localId] ??= s.db
          .watchNotifications(clearedSince: _now().subtract(earlierFor))
          .listen((v) {
            _rows[s.localId] = v;
            _changed();
          });
    }
    _changed();
  }

  void _changed() {
    if (!_disposed) notifyListeners();
  }

  void setServerFilter(String? localId) {
    serverFilter = localId;
    _changed();
  }

  void setKinds(NotificationKinds k) {
    kinds = k;
    _changed();
  }

  /// The servers the filter takes in.
  List<ServerState> get filteredServers => [
    for (final s in app.servers)
      if (serverFilter == null || s.localId == serverFilter) s,
  ];

  NotificationReason reasonOf(ServerState s, NotificationRow r) {
    if (r.kind == 'call') return NotificationReason.missedCall;
    final stored = NotificationReason.values
        .where((x) => x.name == r.reason)
        .firstOrNull;
    if (stored != null) return stored;
    final m = r.message;
    final me = s.me?.id;
    if (m == null || me == null) return r.parentId != null ? NotificationReason.threadReply : NotificationReason.newMessage;
    return notificationReason(
      m,
      myId: me,
      groupsIn: {
        for (final g in s.groups.values)
          if (g.isMember) g.id,
      },
    );
  }

  bool _kindMatches(NotificationReason r) => switch (kinds) {
    NotificationKinds.all => true,
    NotificationKinds.mentions =>
      r == NotificationReason.mentionedYou ||
          r == NotificationReason.mentionedGroup ||
          r == NotificationReason.mentionedChannel,
    NotificationKinds.threads => r == NotificationReason.threadReply,
  };

  List<NotificationEntry> _entries({required bool cleared}) {
    final all = <(ServerState, NotificationRow, NotificationReason)>[];
    for (final s in filteredServers) {
      for (final r in _rows[s.localId] ?? const <NotificationRow>[]) {
        if (r.cleared != cleared) continue;
        final why = reasonOf(s, r);
        if (_kindMatches(why)) all.add((s, r, why));
      }
    }
    // Newest first across all servers, by the message's time.
    all.sort((a, b) {
      final t = b.$2.at.compareTo(a.$2.at);
      return t != 0 ? t : b.$2.seq.compareTo(a.$2.seq);
    });
    final out = <NotificationEntry>[];
    for (final (s, r, why) in all) {
      final last = out.isEmpty ? null : out.last;
      if (last != null &&
          last.server == s &&
          last.conversationId == r.conversationId &&
          last.parentId == r.parentId &&
          last.reason == why &&
          last.rows.last.at.difference(r.at) <= foldWindow) {
        last.rows.add(r);
      } else {
        out.add(NotificationEntry(s, [r], why));
      }
    }
    return out;
  }

  /// Items not yet read, newest first, folded.
  List<NotificationEntry> get unread => _entries(cleared: false);

  /// Items cleared in the last week.
  List<NotificationEntry> get earlier => _entries(cleared: true);

  /// The number of unread items under the current filter.
  int get unreadCount => unread.fold(0, (n, e) => n + e.count);

  /// Opens an entry in place, which reads it.
  Future<void> open(NotificationEntry e) async {
    final m = e.message;
    app.openMessage(
      OpenTarget(e.server.localId, e.conversationId),
      m?.id ?? e.newest.messageId,
      parentId: e.parentId,
    );
    await markRead(e);
  }

  /// Marks one entry read without opening it: the conversation's (or the
  /// thread's) read position moves to its newest item, on every device.
  Future<void> markRead(NotificationEntry e) =>
      _markRead(e.server, e.conversationId, e.parentId, e.newest.seq);

  Future<void> _markRead(
    ServerState s,
    String conversationId,
    String? parentId,
    int seq,
  ) async {
    if (parentId != null &&
        await _hasThreadState(s, conversationId, parentId)) {
      await s.db.clearThreadNotifications(parentId, seq, _now());
      try {
        await s.db.upsertThreadStates([
          await s.api.putThreadRead(parentId, seq),
        ]);
      } on ApiException {
        // The thread's next event puts it right.
      }
    } else {
      final current = s.conversation(conversationId)?.membership?.readSeq ?? 0;
      await s.sync.markRead(conversationId, seq > current ? seq : current);
      await s.db.clearNotifications(conversationId, seq, _now());
    }
  }

  Future<bool> _hasThreadState(
    ServerState s,
    String conversationId,
    String parentId,
  ) async => (await s.db.threadStates(conversationId)).containsKey(parentId);

  /// "Mark all read" for the current filter: the mark-read operation for
  /// each conversation and thread with items, on each server in it.
  Future<void> markAllRead() async {
    final targets = <(ServerState, String, String?), int>{};
    for (final e in unread) {
      for (final r in e.rows) {
        final k = (e.server, r.conversationId, r.parentId);
        if ((targets[k] ?? -1) < r.seq) targets[k] = r.seq;
      }
    }
    await Future.wait([
      for (final t in targets.entries)
        _markRead(t.key.$1, t.key.$2, t.key.$3, t.value),
    ]);
  }

  /// Follows or ignores the thread an entry is in (`none` for neither).
  Future<void> setThreadState(NotificationEntry e, String state) async {
    final parent = e.parentId;
    if (parent == null) return;
    final s = await e.server.api.putThreadState(parent, state);
    if (s != null) {
      await e.server.db.upsertThreadStates([s]);
    } else {
      await e.server.db.deleteThreadState(parent);
    }
  }

  @override
  void dispose() {
    _disposed = true;
    app.removeListener(_servers);
    for (final s in _subs.values) {
      s.cancel();
    }
    super.dispose();
  }
}
