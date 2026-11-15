import 'dart:async';

import 'package:flutter/foundation.dart';

import '../core/api/conversation_ops.dart';
import '../core/api/errors.dart';
import '../core/conversation/threads.dart';
import '../core/db/conversation_queries.dart';
import '../core/models/message.dart';
import 'message_actions.dart';
import 'server_state.dart';

/// The controller of the thread pane (docs/CLIENTS.md 9.4): the parent
/// and its replies, the reader's state for the thread, and its read
/// position.
///
/// Replies are drawn from the cache. `getThread` fills it on opening;
/// replies older than the cached run of the conversation (and a parent
/// older than it) are held here instead, because the cache must stay
/// one unbroken run per conversation for paging back to work.
class ThreadController extends ChangeNotifier {
  ThreadController(this.server, this.conversationId, this.parentId)
    : actions = MessageActions(server);

  final ServerState server;
  final String conversationId;
  final String parentId;
  final MessageActions actions;

  Message? _cachedParent;
  List<Message> _cachedReplies = const [];
  Message? _heldParent;
  List<Message> _heldReplies = const [];
  ThreadState? state;
  bool loading = false;
  String? error;

  final _subs = <StreamSubscription<Object?>>[];
  bool _disposed = false;
  bool _shown = false;

  Message? get parent => _cachedParent ?? _heldParent;

  /// The replies, sorted by `order`.
  List<Message> get replies {
    final ids = {for (final r in _cachedReplies) r.id};
    final all = [
      for (final r in _heldReplies)
        if (!ids.contains(r.id)) r,
      ..._cachedReplies,
    ];
    all.sort(Message.compareOrder);
    return all;
  }

  /// The channel the thread was moved to, or null (ruling R20).
  String? get movedToId => movedTo(replies);

  String get followState => state?.state ?? 'none';

  void _changed() {
    if (!_disposed) notifyListeners();
  }

  Future<void> start() async {
    // A thread on screen is its conversation on screen: the server is
    // told, so a thread of a large channel follows along live whether or
    // not the channel itself is open beside it (lazy delivery).
    server.showConversation(conversationId);
    _shown = true;
    _subs.add(
      server.db.watchThread(parentId).listen((list) {
        _cachedParent = list.isNotEmpty && list.first.id == parentId
            ? list.first
            : null;
        _cachedReplies = [
          for (final m in list)
            if (m.id != parentId) m,
        ];
        _changed();
      }),
    );
    _subs.add(
      server.db.watchThreadStates(conversationId).listen((s) {
        state = s[parentId];
        _changed();
      }),
    );
    await refresh();
  }

  /// Fetches the whole thread and keeps what belongs in the cache there.
  Future<void> refresh() async {
    if (_disposed) return;
    loading = true;
    error = null;
    _changed();
    try {
      final replies = <Message>[];
      Message? p;
      ThreadState? st;
      int? after;
      for (var i = 0; i < 20; i++) {
        final page = await server.api.getThread(parentId, after: after);
        p = page.parent;
        st ??= page.state;
        replies.addAll(page.replies);
        if (!page.hasMore || page.replies.isEmpty) break;
        // Paged by `seq`, which need not follow the order shown.
        after = page.replies.map((r) => r.seq).reduce((a, b) => a > b ? a : b);
      }
      final oldest = await server.db.oldestSeq(conversationId);
      bool inRun(Message m) => oldest != null && m.seq >= oldest;
      final keep = [
        if (p != null && inRun(p)) p,
        for (final r in replies)
          if (inRun(r)) r,
      ];
      await server.db.upsertMessages(keep);
      if (st != null) await server.db.upsertThreadStates([st]);
      _heldParent = p != null && !inRun(p) ? p : null;
      _heldReplies = [
        for (final r in replies)
          if (!inRun(r)) r,
      ];
    } on ApiException catch (e) {
      error = e.message;
    } on Exception catch (e) {
      error = '$e';
    } finally {
      loading = false;
      _changed();
    }
  }

  /// The pane shows the newest reply: the thread is read to there.
  Future<void> markRead() async {
    // By the highest `seq`: the last reply shown need not be the last to
    // arrive.
    final t = parent?.thread;
    final seq = [
      for (final r in replies) r.seq,
      ?t?.lastReplySeq,
    ].fold<int>(0, (a, b) => a > b ? a : b);
    if (seq == 0) return;
    if (state != null && state!.readSeq >= seq) return;
    await actions.markThreadRead(conversationId, parentId, seq);
  }

  Future<void> setFollow(String to) async {
    final p = parent;
    if (p == null) return;
    try {
      await actions.setThreadState(p, to, readSeq: p.thread?.lastReplySeq ?? 0);
    } on ApiException catch (e) {
      error = e.message;
      _changed();
    }
  }

  @override
  void dispose() {
    _disposed = true;
    if (_shown) server.hideConversation(conversationId);
    for (final s in _subs) {
      s.cancel();
    }
    super.dispose();
  }
}
