import 'dart:async';

import 'package:flutter/widgets.dart';

import '../core/db/cache_db.dart';
import '../core/models/message.dart';
import '../core/sync.dart';
import 'server_state.dart';

/// The controller of the conversation screen, and the example to copy
/// when adding a screen (see README.md, "How to add a screen").
///
/// The pattern, which every screen controller follows:
///
/// 1. **Reads are watched queries.** [start] subscribes to the database
///    queries the screen draws from and keeps their latest answers in
///    plain fields. Nothing is fetched into memory "for the screen": a
///    new message appears because sync wrote it to the database.
/// 2. **Writes go to the database first, then the server.** [send] puts
///    the message in the outbox (through the sync engine) and returns at
///    once; the row is on screen as pending before any request starts.
/// 3. **Only what is not in the database lives here:** the composer's
///    text, how far back the person has scrolled, whether a page is
///    being fetched.
/// 4. **It is a plain `ChangeNotifier`,** created by the screen's State,
///    listened to with `ListenableBuilder`, and disposed with it.
class ConversationController extends ChangeNotifier {
  ConversationController(
    this.server,
    this.conversationId, {
    this.keepsDraft = true,
  });

  /// False when the screen's own composer keeps the draft (the
  /// conversation screen's `ComposerController`): this controller then
  /// neither restores nor saves it.
  final bool keepsDraft;

  final ServerState server;
  final String conversationId;

  CacheDb get _db => server.db;
  SyncEngine get _sync => server.sync;

  /// The cached messages on screen, sorted by `order`, replies included.
  List<Message> messages = const [];

  /// Messages written here that the server has not confirmed yet.
  List<OutboxItem> pending = const [];
  ConversationRow? row;

  /// The text being typed. Its draft is kept per conversation.
  final composer = TextEditingController();

  bool loadingOlder = false;
  String? error;

  /// How many messages the watched query asks for. It grows as the
  /// person scrolls back.
  int _window = SyncEngine.pageSize * 2;

  StreamSubscription<List<Message>>? _messagesSub;
  final _subs = <StreamSubscription<Object?>>[];
  bool _disposed = false;

  /// Whether older messages may exist beyond what is on screen.
  bool get hasOlder =>
      !(row?.historyComplete ?? false) || messages.length >= _window;

  /// The messages the body draws in the threaded view: replies are in
  /// their thread, not in the body (docs/CLIENTS.md 9.3).
  List<Message> get body => [
    for (final m in messages)
      if (!m.isReply) m,
  ];

  void _changed() {
    if (!_disposed) notifyListeners();
  }

  void _watchMessages() {
    _messagesSub?.cancel();
    _messagesSub = _db.watchMessages(conversationId, limit: _window).listen((
      v,
    ) {
      messages = v;
      _changed();
    });
  }

  /// Opens the watches, restores the draft, and fetches the first page
  /// if the cache has less than a screenful.
  Future<void> start() async {
    _watchMessages();
    _subs.add(
      _db.watchOutbox(conversationId).listen((v) {
        pending = v;
        _changed();
      }),
    );
    _subs.add(
      _db.watchConversation(conversationId).listen((v) {
        row = v;
        _changed();
      }),
    );
    if (keepsDraft) composer.text = await _db.draft(conversationId);
    if (await _db.messageCount(conversationId) < SyncEngine.pageSize) {
      await loadOlder();
    }
  }

  /// Scrolling back: widens the window over the cache, and fetches an
  /// older page from the server when the cache runs out.
  Future<void> loadOlder() async {
    if (loadingOlder || _disposed) return;
    loadingOlder = true;
    error = null;
    _changed();
    try {
      final cached = await _db.messageCount(conversationId);
      if (cached <= messages.length) await _sync.loadOlder(conversationId);
      if (messages.length >= _window - SyncEngine.pageSize) {
        _window += SyncEngine.pageSize;
        if (!_disposed) _watchMessages();
      }
    } on Exception catch (e) {
      error = 'Could not load older messages. ($e)';
    } finally {
      loadingOlder = false;
      _changed();
    }
  }

  /// Sends what is in the composer.
  Future<void> send() async {
    final text = composer.text.trim();
    if (text.isEmpty) return;
    composer.clear();
    await _db.putDraft(conversationId, '');
    await _sync.send(conversationId, text);
  }

  Future<void> retry(OutboxItem item) => _sync.retry(item.clientId);
  Future<void> discard(OutboxItem item) => _sync.discard(item.clientId);

  /// The newest message has been on screen with the app in front: the
  /// read position moves to the highest `seq` loaded. That is not the
  /// last message drawn when a late message arrived: it sorts further up
  /// by `order` but is numbered last.
  Future<void> markRead() async {
    final seq = highestSeq(messages);
    if (seq != null) await _sync.markRead(conversationId, seq);
  }

  Future<void> toggleReaction(Message m, String emoji) async {
    try {
      await _sync.toggleReaction(m, emoji);
    } on Exception catch (e) {
      error = '$e';
      _changed();
    }
  }

  Future<void> setChecklistItem(Message m, String itemId, bool checked) async {
    try {
      await _sync.setChecklistItem(m, itemId, checked);
    } on Exception catch (e) {
      error = '$e';
      _changed();
    }
  }

  @override
  void dispose() {
    _disposed = true;
    // The draft outlives the screen. Fire and forget: the database
    // outlives it too.
    if (keepsDraft) unawaited(_db.putDraft(conversationId, composer.text));
    _messagesSub?.cancel();
    for (final s in _subs) {
      s.cancel();
    }
    composer.dispose();
    super.dispose();
  }
}

/// The highest `seq` among [messages], or null when there are none.
int? highestSeq(Iterable<Message> messages) {
  int? top;
  for (final m in messages) {
    if (top == null || m.seq > top) top = m.seq;
  }
  return top;
}
