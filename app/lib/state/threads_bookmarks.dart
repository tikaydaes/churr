import 'dart:async';

import 'package:flutter/foundation.dart';

import '../core/account_sync.dart';
import '../core/api/account.dart';
import '../core/api/errors.dart';
import '../core/db/account_tables.dart';
import '../core/models/message.dart';
import 'app_state.dart';
import 'server_state.dart';

/// Watches one query per signed-in server and keeps the latest answers,
/// following servers as they are added and removed.
abstract class _PerServer<T> extends ChangeNotifier {
  _PerServer(this.app);
  final AppState app;
  final answers = <String, List<T>>{};
  final _subs = <String, StreamSubscription<List<T>>>{};
  bool _disposed = false;

  Stream<List<T>> watchOf(ServerState s);

  void start() {
    app.addListener(_servers);
    _servers();
    // The lists have no replay: fetch them fresh as the screen opens.
    for (final s in app.servers) {
      unawaited(refreshPersonalLists(s.api, s.db).catchError((_) {}));
    }
  }

  void _servers() {
    final ids = {for (final s in app.servers) s.localId};
    for (final id in _subs.keys.toList()) {
      if (!ids.contains(id)) {
        _subs.remove(id)?.cancel();
        answers.remove(id);
      }
    }
    for (final s in app.servers) {
      _subs[s.localId] ??= watchOf(s).listen((v) {
        answers[s.localId] = v;
        changed();
      });
    }
  }

  void changed() {
    if (!_disposed) notifyListeners();
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

/// A followed thread with its server.
class ThreadItem {
  ThreadItem(this.server, this.row);
  final ServerState server;
  final ThreadRow row;
  String get parentId => row.state.parentId;
  String get conversationId => row.state.conversationId;
  int get unread => row.state.unreadCount;
  DateTime get lastReplyAt =>
      row.parent?.thread?.lastReplyAt ??
      row.parent?.createdAt ??
      DateTime(2000);
  String get key => '${server.localId}/$parentId';
}

/// The Threads view (docs/CLIENTS.md 9.9, docs/FEATURES.md 1.5): every
/// followed thread with unread replies, newest reply first; with
/// [showRead], those with nothing new too.
class ThreadsController extends _PerServer<ThreadRow> {
  ThreadsController(super.app);

  @override
  Stream<List<ThreadRow>> watchOf(ServerState s) => s.db.watchFollowedThreads();

  bool showRead = false;

  /// All servers (null) or one, by local id.
  String? serverFilter;

  void setShowRead(bool v) {
    showRead = v;
    changed();
  }

  void setServerFilter(String? id) {
    serverFilter = id;
    changed();
  }

  List<ThreadItem> get items {
    final out = <ThreadItem>[
      for (final s in app.servers)
        if (serverFilter == null || serverFilter == s.localId)
          for (final r in answers[s.localId] ?? const <ThreadRow>[])
            // The account must still be able to read the conversation.
            if (s.conversation(r.state.conversationId) != null &&
                (showRead || r.state.unreadCount > 0))
              ThreadItem(s, r),
    ];
    out.sort((a, b) => b.lastReplyAt.compareTo(a.lastReplyAt));
    return out;
  }

  /// Opens the thread in its conversation.
  void open(ThreadItem t) => app.openMessage(
    OpenTarget(t.server.localId, t.conversationId),
    t.parentId,
    parentId: t.parentId,
  );

  /// Marks the thread read to its newest reply.
  Future<void> markRead(ThreadItem t) async {
    final seq = t.row.parent?.thread?.lastReplySeq ?? t.row.state.readSeq;
    await t.server.db.upsertThreadStates([
      // The database first, so the entry leaves the list at once.
      ThreadState(
        parentId: t.parentId,
        conversationId: t.conversationId,
        state: t.row.state.state,
        readSeq: seq,
        unreadCount: 0,
      ),
    ]);
    await t.server.db.clearThreadNotifications(t.parentId, seq, DateTime.now());
    try {
      await t.server.db.upsertThreadStates([
        await t.server.api.putThreadRead(t.parentId, seq),
      ]);
    } on ApiException {
      // `thread_state.changed` or the next list puts it right.
    }
  }

  /// Stops following: the thread goes back to "neither".
  Future<void> stopFollowing(ThreadItem t) async {
    await t.server.db.deleteThreadState(t.parentId);
    await t.server.api.putThreadState(t.parentId, 'none');
  }

  /// Replies in place.
  Future<void> reply(ThreadItem t, String text) =>
      t.server.sync.send(t.conversationId, text.trim(), parentId: t.parentId);
}

/// A bookmark with its server.
class BookmarkItem {
  BookmarkItem(this.server, this.row);
  final ServerState server;
  final BookmarkRow row;
  String get key => '${server.localId}/${row.messageId}';
}

/// Bookmarks (docs/CLIENTS.md 9.10): the person's bookmarked messages,
/// newest bookmark first, across every signed-in server.
class BookmarksController extends _PerServer<BookmarkRow> {
  BookmarksController(super.app);

  @override
  Stream<List<BookmarkRow>> watchOf(ServerState s) => s.db.watchBookmarks();

  List<BookmarkItem> get items {
    final out = <BookmarkItem>[
      for (final s in app.servers)
        for (final r in answers[s.localId] ?? const <BookmarkRow>[])
          BookmarkItem(s, r),
    ];
    out.sort((a, b) => b.row.createdAt.compareTo(a.row.createdAt));
    return out;
  }

  void open(BookmarkItem b) {
    final m = b.row.message;
    app.openMessage(
      OpenTarget(b.server.localId, b.row.conversationId),
      b.row.messageId,
      parentId: m?.parentId,
    );
  }

  /// Removes it: the database first, then the server.
  Future<void> remove(BookmarkItem b) async {
    await b.server.db.deleteBookmark(b.row.messageId);
    try {
      await b.server.api.dropBookmark(b.row.messageId);
    } on ApiException {
      await b.server.db.putBookmark(
        b.row.messageId,
        b.row.createdAt,
        message: b.row.message,
      );
      rethrow;
    }
  }
}
