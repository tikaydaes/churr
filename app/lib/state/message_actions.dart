import '../core/api/conversation_ops.dart';
import '../core/api/conversations.dart';
import '../core/api/errors.dart';
import '../core/db/account_tables.dart';
import '../core/db/conversation_queries.dart';
import '../core/models/account.dart';
import '../core/models/conversation.dart';
import '../core/models/json.dart';
import '../core/models/message.dart';
import 'sealed_state.dart';
import 'server_state.dart';

/// What can be done to a message, a thread or a conversation's thread
/// view from the conversation screen (docs/CLIENTS.md 9.4 and 9.6).
///
/// Each write changes the database first where the result is known, then
/// calls the server, whose answer (and event) confirms or corrects it.
/// Errors are thrown as [ApiException] for the screen to word.
class MessageActions {
  MessageActions(this.server);
  final ServerState server;

  String? get myId => server.me?.id;

  /// In a sealed chat, edits, deletes and reactions are sealed messages
  /// (lib/state/sealed_state.dart).
  SealedController? _sealed(Message m) =>
      server.conversation(m.conversationId)?.isSealed ?? false
      ? SealedHub.instance.of(server)
      : null;

  /// The message as the server now has it, written to the cache if the
  /// cache holds it.
  Future<void> _store(Message m) async {
    if (m.id.isEmpty) return;
    if (await server.db.message(m.id) != null) {
      await server.db.upsertMessages([m]);
    }
  }

  // ---- who may do what ----

  /// Whether the reader acts as manager of [c]: its level for them is
  /// manager. The account's role gives nothing in a channel (ruling R99).
  bool isManager(Conversation? c) =>
      c != null && !c.isSealed && c.membership?.level == 'manager';

  /// Whether the reader keeps the peace in [c]: a moderator or a manager
  /// of it, who deletes others' messages and is exempt from slow mode.
  bool keepsPeace(Conversation? c) =>
      c != null &&
      !c.isSealed &&
      (c.membership?.level == 'manager' || c.membership?.level == 'moderator');

  /// Whether the reader may still edit [m] (FEATURES.md 4.1).
  bool canEdit(Message m, Conversation? c, ServerLimits limits, DateTime now) {
    if (m.authorId != myId || m.isDeleted || m.isSystem) return false;
    if (c == null ||
        c.isArchived ||
        c.membership == null ||
        c.membership!.level == 'reader') {
      return false;
    }
    final window = limits.editWindowSeconds;
    return window == null || now.difference(m.createdAt).inSeconds <= window;
  }

  bool canDelete(Message m, Conversation? c) {
    if (m.isDeleted || m.isSystem) return false;
    if (m.authorId == myId) return true;
    return keepsPeace(c);
  }

  // ---- edits, deletes, history and purge ----

  /// Edits [m] in place. The new text is shown at once; the server's
  /// answer replaces it with its entities worked out again.
  Future<void> edit(
    Message m,
    String text, {
    List<String>? fileIds,
    String? timeZone,
    List<Object?>? instantHints,
    List<Object?>? objectHints,
  }) async {
    final sealed = _sealed(m);
    if (sealed != null) return sealed.edit(m, text);
    final before = await server.db.message(m.id);
    if (before != null) {
      await server.db.upsertMessages([
        Message.fromJson({
          ...before.raw,
          'text': text,
          'entities': const <Object>[],
        }),
      ]);
    }
    try {
      await _store(
        await server.api.editMessage(
          m.id,
          text: text,
          fileIds: fileIds,
          timeZone: timeZone,
          instantHints: instantHints,
          objectHints: objectHints,
        ),
      );
    } on Object {
      if (before != null) await server.db.upsertMessages([before]);
      rethrow;
    }
  }

  /// Deletes [m]. It stays in place as a marker saying who deleted it.
  Future<void> delete(Message m) async {
    final sealed = _sealed(m);
    if (sealed != null) return sealed.delete(m);
    final before = await server.db.message(m.id);
    if (before != null) {
      await server.db.upsertMessages([
        Message.fromJson({
          ...before.raw,
          'text': '',
          'entities': const <Object>[],
          'files': const <Object>[],
          'cards': const <Object>[],
          'reactions': const <Object>[],
          'deleted_at': formatTime(DateTime.now().toUtc()),
          'deleted_by_id': myId,
        }),
      ]);
    }
    try {
      await _store(await server.api.deleteMessage(m.id));
    } on Object {
      if (before != null) await server.db.upsertMessages([before]);
      rethrow;
    }
  }

  /// Not kept on the device (docs/CLIENTS.md section 6).
  Future<List<MessageVersion>> history(Message m) =>
      server.api.getMessageHistory(m.id);

  /// Asks for a purge, or purges. Returns `purged` or `requested`.
  Future<String> purge(Message m) async {
    final (state, after) = await server.api.purgeMessage(m.id);
    await _store(after);
    return state;
  }

  // ---- reactions, pins, bookmarks, reports ----

  Future<void> react(Message m, String emoji) async {
    await server.db.useEmoji(emoji);
    final sealed = _sealed(m);
    if (sealed != null) return sealed.react(m, emoji);
    await server.sync.toggleReaction(m, emoji);
  }

  Future<List<Reaction>> reactions(Message m) => server.api.listReactions(m.id);

  Future<void> setPinned(Message m, bool pinned) async {
    await _store(m.copyWith(pinned: pinned));
    try {
      if (pinned) {
        await server.api.pinMessage(m.conversationId, m.id);
      } else {
        await server.api.unpinMessage(m.conversationId, m.id);
      }
    } on Object {
      await _store(m.copyWith(pinned: !pinned));
      rethrow;
    }
  }

  Future<void> setBookmarked(Message m, bool on) async {
    if (on) {
      await server.db.putBookmark(m.id, DateTime.now(), message: m);
      await server.api.addBookmark(m.id);
    } else {
      await server.db.deleteBookmark(m.id);
      await server.api.removeBookmark(m.id);
    }
  }

  Future<void> report(Message m, String reason) =>
      server.api.reportMessage(m.id, reason);

  /// Always appears to work; the mark on the message is all that shows
  /// (FEATURES.md 3.8).
  Future<void> notifyAnyway(Message m, String accountId) async =>
      _store(await server.api.notifyAnyway(m.id, accountId));

  // ---- threads ----

  /// `following`, `ignored`, or `none` for neither.
  Future<void> setThreadState(
    Message parent,
    String state, {
    int readSeq = 0,
  }) async {
    final existing = (await server.db.threadStates(
      parent.conversationId,
    ))[parent.id];
    if (state == 'none') {
      await server.db.deleteThreadState(parent.id);
    } else {
      await server.db.upsertThreadStates([
        ThreadState(
          parentId: parent.id,
          conversationId: parent.conversationId,
          state: state,
          readSeq: existing?.readSeq ?? readSeq,
          unreadCount: existing?.unreadCount ?? 0,
        ),
      ]);
    }
    try {
      await server.api.setThreadState(parent.id, state);
    } on Object {
      if (existing != null) {
        await server.db.upsertThreadStates([existing]);
      } else {
        await server.db.deleteThreadState(parent.id);
      }
      rethrow;
    }
  }

  /// The thread has been read to [seq]. Raises the row's position if
  /// there is one; for "neither" only the server is told.
  Future<void> markThreadRead(
    String conversationId,
    String parentId,
    int seq,
  ) async {
    final existing = (await server.db.threadStates(conversationId))[parentId];
    if (existing != null &&
        existing.readSeq >= seq &&
        existing.unreadCount == 0) {
      return;
    }
    if (existing != null) {
      await server.db.upsertThreadStates([
        ThreadState(
          parentId: parentId,
          conversationId: conversationId,
          state: existing.state,
          readSeq: seq > existing.readSeq ? seq : existing.readSeq,
          unreadCount: 0,
        ),
      ]);
    }
    try {
      await server.api.setThreadReadPosition(parentId, seq);
    } on ApiException {
      // The next page or event puts it right.
    }
  }

  /// Moves a thread out to a channel of its own (FEATURES.md 1.6).
  /// Returns the new channel, which is also written to the cache.
  Future<Conversation> moveThread(Message parent, String name) async {
    final c = await server.api.moveThreadToChannel(parent.id, name);
    await server.db.upsertConversation(c, syncedSeq: c.lastSeq);
    return c;
  }

  /// "Mark unread from here": the read position moves to just before
  /// [m].
  Future<void> markUnreadFrom(Message m) async {
    final seq = m.seq - 1;
    final membership = await server.api.setReadPosition(
      m.conversationId,
      seq < 0 ? 0 : seq,
    );
    final row = await server.db.conversation(m.conversationId);
    if (row != null) {
      await server.db.upsertConversation(
        row.conversation.withMembership(membership),
      );
    }
  }

  // ---- the thread view ----

  /// Sets this conversation's thread view: `threaded`, `flat`, or null
  /// for the person's default.
  Future<void> setConversationThreadView(String conversationId, String? view) =>
      server.sync.updateMembership(conversationId, {'thread_view': view});

  /// Sets the person's default thread view on this server.
  Future<void> setDefaultThreadView(String view) async {
    await server.api.setDefaultThreadView(view);
    final me = server.me;
    if (me != null) {
      final prefs = {...?object(me.raw['prefs']), 'thread_view': view};
      await server.db.putMe(Me.fromJson({...me.raw, 'prefs': prefs}));
    }
  }
}
