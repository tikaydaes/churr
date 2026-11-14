import 'dart:async';

import 'package:flutter/foundation.dart';

import '../core/api/search.dart';
import '../core/models/message.dart';
import 'app_state.dart';
import 'server_state.dart';

/// One hit, with the server it came from.
class ServerHit {
  ServerHit(this.server, this.hit);
  final ServerState server;
  final SearchHit hit;
  Message get message => hit.message;
}

/// A filter that belongs to one server: a conversation or an author.
class ServerPick {
  const ServerPick(this.serverId, this.id, this.label);
  final String serverId;
  final String id;
  final String label;
}

/// The search screen's state (docs/CLIENTS.md 8.4 and 9.8): the same
/// words and filters sent to every signed-in server at once, each with
/// its own cursor; results merged newest first by message time, shown
/// as each server answers; a slow or failing server named at the top
/// without holding the others back.
///
/// Search is a question to the servers by nature, so this is the one
/// screen whose list does not come from the local database.
class UnifiedSearch extends ChangeNotifier {
  UnifiedSearch(this.app, {this.serverId, this.inConversation});

  final AppState app;

  /// All servers when null, or this one.
  String? serverId;

  /// Filters that belong to one server; setting one narrows the search
  /// to that server.
  ServerPick? inConversation;
  ServerPick? from;
  DateTime? since;
  DateTime? until;
  bool hasFile = false;
  String text = '';

  final Map<String, List<SearchHit>> _hits = {};
  final Map<String, String?> _cursors = {};
  final Set<String> _loading = {};
  final Map<String, String> failures = {};
  int _generation = 0;
  bool _disposed = false;
  bool searched = false;

  void _changed() {
    if (!_disposed) notifyListeners();
  }

  /// The servers asked. A conversation or author filter belongs to one.
  List<ServerState> get scope {
    final only = inConversation?.serverId ?? from?.serverId ?? serverId;
    return [
      for (final s in app.servers)
        if (only == null || s.localId == only) s,
    ];
  }

  bool get loading => _loading.isNotEmpty;

  /// Every hit so far, newest first. Servers rank by their own rules and
  /// their scores cannot be compared, so time is the only fair order.
  List<ServerHit> get results {
    final out = <ServerHit>[
      for (final s in scope)
        for (final h in _hits[s.localId] ?? const <SearchHit>[])
          ServerHit(s, h),
    ];
    out.sort((a, b) => b.message.createdAt.compareTo(a.message.createdAt));
    return out;
  }

  bool get hasMore => scope.any((s) => _cursors[s.localId] != null);

  /// Names of servers that are slow (still asking) once others answered,
  /// or that failed.
  List<String> get slowServers => [
    for (final s in scope)
      if (_loading.contains(s.localId) && _hits.isNotEmpty) s.name,
  ];

  /// Turns the box's words and typed filters into a query for one
  /// server (FEATURES.md 24.1): `from:@sam`, `from:me`, `since:`,
  /// `until:`, `has:file`. A typed filter this server cannot resolve is
  /// left in the words.
  SearchQuery queryFor(ServerState s) {
    final words = <String>[];
    String? fromId = from?.serverId == s.localId ? from!.id : null;
    DateTime? since = this.since, until = this.until;
    var hasFile = this.hasFile;
    for (final w in text.split(RegExp(r'\s+'))) {
      if (w.isEmpty) continue;
      final lower = w.toLowerCase();
      if (lower == 'has:file') {
        hasFile = true;
      } else if (lower.startsWith('from:')) {
        final who = lower.substring(5).replaceFirst('@', '');
        String? id;
        if (who == 'me') {
          id = s.me?.id;
        } else {
          for (final a in s.accounts.values) {
            if (a.username == who) id = a.id;
          }
        }
        if (id == null) {
          words.add(w);
        } else {
          fromId = id;
        }
      } else if (lower.startsWith('since:') || lower.startsWith('until:')) {
        final d = DateTime.tryParse(lower.substring(6));
        if (d == null ||
            !RegExp(r'^\d{4}-\d{2}-\d{2}$').hasMatch(lower.substring(6))) {
          words.add(w);
        } else {
          // The start of that day in the person's time zone.
          final local = DateTime(d.year, d.month, d.day);
          if (lower.startsWith('since:')) {
            since = local;
          } else {
            until = local;
          }
        }
      } else {
        words.add(w);
      }
    }
    return SearchQuery(
      q: words.join(' '),
      inId: inConversation?.serverId == s.localId ? inConversation!.id : null,
      fromId: fromId,
      since: since,
      until: until,
      hasFile: hasFile,
    );
  }

  /// Starts a new search on every server in scope.
  Future<void> run() async {
    final gen = ++_generation;
    _hits.clear();
    _cursors.clear();
    failures.clear();
    _loading.clear();
    searched = true;
    final servers = scope;
    final queries = {for (final s in servers) s.localId: queryFor(s)};
    if (queries.values.every((q) => q.isEmpty)) {
      searched = false;
      _changed();
      return;
    }
    _changed();
    await Future.wait([
      for (final s in servers)
        if (!queries[s.localId]!.isEmpty)
          _page(s, queries[s.localId]!, gen, null),
    ]);
  }

  /// Asks each server with more for its next page and merges again.
  Future<void> loadMore() async {
    final gen = _generation;
    await Future.wait([
      for (final s in scope)
        if (_cursors[s.localId] != null && !_loading.contains(s.localId))
          _page(s, queryFor(s), gen, _cursors[s.localId]),
    ]);
  }

  Future<void> _page(
    ServerState s,
    SearchQuery q,
    int gen,
    String? cursor,
  ) async {
    _loading.add(s.localId);
    _changed();
    try {
      final page = await s.api.search(q, cursor: cursor);
      if (gen != _generation) return;
      (_hits[s.localId] ??= []).addAll(page.hits);
      _cursors[s.localId] = page.nextCursor;
      failures.remove(s.localId);
    } on Exception catch (e) {
      if (gen != _generation) return;
      failures[s.localId] = '${s.name}: $e';
      _cursors[s.localId] = null;
    } finally {
      if (gen == _generation) _loading.remove(s.localId);
      _changed();
    }
  }

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }
}
