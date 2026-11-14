import 'dart:async';

import 'package:flutter/foundation.dart';

import '../core/db/conversation_queries.dart';
import '../core/models/message.dart';
import 'server_state.dart';

/// What the conversation screen draws beside its messages, each a
/// watched query: the reader's thread states (markers, follow and
/// ignore, the flat view's left-out replies), which messages are
/// bookmarked, and the server's limits.
///
/// Quoted messages older than the window on screen are looked up in the
/// cache by [quoted].
class ConversationExtras extends ChangeNotifier {
  ConversationExtras(this.server, this.conversationId);

  final ServerState server;
  final String conversationId;

  Map<String, ThreadState> threadStates = const {};
  Set<String> bookmarked = const {};
  ServerLimits limits = const ServerLimits();
  Map<String, Message> _quoted = const {};

  final _subs = <StreamSubscription<Object?>>[];
  bool _disposed = false;

  void _changed() {
    if (!_disposed) notifyListeners();
  }

  void start() {
    _subs.add(
      server.db.watchThreadStates(conversationId).listen((v) {
        threadStates = v;
        _changed();
      }),
    );
    _subs.add(
      server.db.watchBookmarkedIds(conversationId).listen((v) {
        bookmarked = v;
        _changed();
      }),
    );
    _subs.add(
      server.db.watch({'me'}, server.db.serverLimits).listen((v) {
        limits = v;
        _changed();
      }),
    );
  }

  /// A quoted message that is not among [onScreen]: looked up once in the
  /// cache. Null until found, or when it is not cached.
  Message? quoted(String id, Map<String, Message> onScreen) {
    final m = onScreen[id] ?? _quoted[id];
    if (m != null) return m;
    if (!_looking.contains(id)) {
      _looking.add(id);
      server.db.messagesById([id]).then((found) {
        if (found.isEmpty) return;
        _quoted = {..._quoted, ...found};
        _changed();
      });
    }
    return null;
  }

  final _looking = <String>{};

  @override
  void dispose() {
    _disposed = true;
    for (final s in _subs) {
      s.cancel();
    }
    super.dispose();
  }
}
