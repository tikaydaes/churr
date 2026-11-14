import 'dart:async';

import 'package:flutter/foundation.dart';

import '../core/api/channels.dart';
import '../core/models/account.dart';
import '../core/models/conversation.dart';
import 'app_state.dart';
import 'channel_detail.dart';
import 'server_state.dart';
import 'sidebar_model.dart';

/// One thing the quick switcher found: a conversation or a person, on
/// one server.
class SwitchHit {
  SwitchHit.conversation(this.server, Conversation c)
    : conversation = c,
      account = null;
  SwitchHit.person(this.server, Account a) : account = a, conversation = null;

  final ServerState server;
  final Conversation? conversation;
  final Account? account;

  DateTime get recency =>
      conversation?.lastMessageAt ??
      conversation?.createdAt ??
      DateTime.fromMillisecondsSinceEpoch(0);
}

/// The quick switcher's state (docs/CLIENTS.md 8.5 and 9.17): looks
/// through the local databases of every signed-in server at once,
/// conversations by name (hidden ones too) and people the app has seen,
/// most recent first. Once three characters are typed, people not yet
/// seen on this device are looked for with `listAccounts` on each server.
class QuickSwitcher extends ChangeNotifier {
  QuickSwitcher(this.app);
  final AppState app;

  String query = '';
  final Map<String, List<Account>> _remote = {};
  Timer? _debounce;
  bool _disposed = false;

  void _changed() {
    if (!_disposed) notifyListeners();
  }

  void setQuery(String q) {
    query = q;
    _changed();
    _debounce?.cancel();
    if (q.trim().length >= 3) {
      _debounce = Timer(
        const Duration(milliseconds: 250),
        () => _findPeople(q.trim()),
      );
    } else {
      _remote.clear();
    }
  }

  Future<void> _findPeople(String q) async {
    await Future.wait([
      for (final s in app.servers)
        () async {
          try {
            final found = await s.api.findAccounts(q);
            if (query.trim() != q) return;
            _remote[s.localId] = found;
            _changed();
          } on Exception {
            // Unreachable servers simply add nothing.
          }
        }(),
    ]);
  }

  List<SwitchHit> get results {
    final q = query.trim();
    final out = <SwitchHit>[];
    for (final s in app.servers) {
      final seenPeople = <String>{};
      for (final r in s.conversations) {
        final c = r.conversation;
        final name = conversationName(c, s.accounts, s.me?.id);
        final parent = c.parentId == null
            ? null
            : s.conversation(c.parentId!)?.name;
        if (nameMatches(q, name, parentName: parent)) out.add(SwitchHit.conversation(s, c));
      }
      for (final a in [...s.accounts.values, ...?_remote[s.localId]]) {
        if (a.id == s.me?.id || a.removedAt != null || !seenPeople.add(a.id)) continue;
        if (q.isEmpty) continue;
        if (nameMatches(q, a.name) ||
            foldName(a.username).startsWith(foldName(q))) {
          out.add(SwitchHit.person(s, a));
        }
      }
    }
    // Most recent first; people (who have no time) after conversations.
    out.sort((a, b) {
      if ((a.account == null) != (b.account == null)) return a.account == null ? -1 : 1;
      return b.recency.compareTo(a.recency);
    });
    return out.take(50).toList();
  }

  @override
  void dispose() {
    _disposed = true;
    _debounce?.cancel();
    super.dispose();
  }
}
