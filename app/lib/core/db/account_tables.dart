import 'dart:convert';

import '../models/json.dart';
import '../models/message.dart';
import '../models/personal.dart';
import 'cache_db.dart';

/// A bookmark as `cache.db` holds it: the message saved, and when.
class BookmarkRow {
  BookmarkRow(
    this.messageId,
    this.conversationId,
    this.createdAt,
    this.message,
  );
  final String messageId;
  final String conversationId;
  final DateTime createdAt;

  /// Null when only the identifier is known yet (a `bookmark.added`
  /// event from another device, before the list is fetched again).
  final Message? message;
}

/// A followed thread for the Threads view (docs/CLIENTS.md 9.9): its
/// state and its parent message.
class ThreadRow {
  ThreadRow(this.state, this.parent);
  final ThreadState state;
  final Message? parent;
}

/// The account area's tables in `cache.db` (schema version 2) and the
/// local store's housekeeping (docs/CLIENTS.md section 6, "How much is
/// kept").
extension AccountTables on CacheDb {
  // ---- bookmarks ----

  Future<void> replaceBookmarks(List<Bookmark> list) => transaction(() async {
    await customStatement('DELETE FROM bookmarks');
    for (final b in list) {
      await customStatement(
        'INSERT OR REPLACE INTO bookmarks (message_id, conversation_id, created_at, json) '
        'VALUES (?, ?, ?, ?)',
        [
          b.messageId,
          b.message?.conversationId ?? '',
          b.createdAt.millisecondsSinceEpoch,
          jsonEncode(b.message?.raw ?? const {}),
        ],
      );
    }
    touch({'bookmarks'});
  });

  /// Adds one. [message] may be null when an event names only the id;
  /// an existing row keeps its message then.
  Future<void> putBookmark(
    String messageId,
    DateTime createdAt, {
    Message? message,
  }) async {
    final existing = await rows(
      'SELECT json, conversation_id FROM bookmarks WHERE message_id = ?',
      [messageId],
    );
    final keptJson = existing.isEmpty ? '{}' : existing.first['json'] as String;
    final keptConv = existing.isEmpty
        ? ''
        : existing.first['conversation_id'] as String;
    await run(
      'INSERT OR REPLACE INTO bookmarks (message_id, conversation_id, created_at, json) VALUES (?, ?, ?, ?)',
      [
        messageId,
        message?.conversationId ?? keptConv,
        createdAt.millisecondsSinceEpoch,
        message == null ? keptJson : jsonEncode(message.raw),
      ],
      {'bookmarks'},
    );
  }

  Future<void> deleteBookmark(String messageId) => run(
    'DELETE FROM bookmarks WHERE message_id = ?',
    [messageId],
    {'bookmarks'},
  );

  /// Newest bookmark first.
  Future<List<BookmarkRow>> bookmarks() async => [
    for (final r in await rows(
      'SELECT * FROM bookmarks ORDER BY created_at DESC',
    ))
      BookmarkRow(
        r['message_id'] as String,
        r['conversation_id'] as String,
        DateTime.fromMillisecondsSinceEpoch(r['created_at'] as int),
        _message(r['json'] as String),
      ),
  ];

  Stream<List<BookmarkRow>> watchBookmarks() => watch({'bookmarks'}, bookmarks);

  Future<bool> isBookmarked(String messageId) async => (await rows(
    'SELECT 1 FROM bookmarks WHERE message_id = ?',
    [messageId],
  )).isNotEmpty;

  // ---- status presets ----

  Future<void> replaceStatusPresets(List<StatusPreset> list) =>
      transaction(() async {
        await customStatement('DELETE FROM status_presets');
        for (final p in list) {
          await customStatement(
            'INSERT INTO status_presets (id, position, json) VALUES (?, ?, ?)',
            [p.id, p.position, jsonEncode(p.raw)],
          );
        }
        touch({'status_presets'});
      });

  Future<List<StatusPreset>> statusPresets() async => [
    for (final r in await rows(
      'SELECT json FROM status_presets ORDER BY position, id',
    ))
      StatusPreset.fromJson(jsonDecode(r['json'] as String) as Json),
  ];

  Stream<List<StatusPreset>> watchStatusPresets() =>
      watch({'status_presets'}, statusPresets);

  // ---- the Threads view ----

  Future<void> putThreadParents(Iterable<Message> parents) async {
    for (final m in parents) {
      await customStatement(
        'INSERT OR REPLACE INTO thread_parents (parent_id, conversation_id, json) VALUES (?, ?, ?)',
        [m.id, m.conversationId, jsonEncode(m.raw)],
      );
    }
    touch({'thread_parents'});
  }

  /// Every followed thread, with its parent from the thread list or from
  /// the cached messages.
  Future<List<ThreadRow>> followedThreads() async {
    final out = <ThreadRow>[];
    for (final r in await rows(
      'SELECT s.json AS state, p.json AS parent, m.json AS cached FROM thread_states s '
      'LEFT JOIN thread_parents p ON p.parent_id = s.parent_id '
      'LEFT JOIN messages m ON m.id = s.parent_id',
    )) {
      final state = ThreadState.fromJson(
        jsonDecode(r['state'] as String) as Json,
      );
      if (state.state != 'following') continue;
      // The cached message is the fresher one: sync keeps it current.
      final raw = (r['cached'] ?? r['parent']) as String?;
      out.add(ThreadRow(state, raw == null ? null : _message(raw)));
    }
    return out;
  }

  Stream<List<ThreadRow>> watchFollowedThreads() =>
      watch({'thread_states', 'thread_parents', 'messages'}, followedThreads);

  /// The last [limit] cached replies of one thread, sorted by `order`.
  Future<List<Message>> cachedReplies(String parentId, {int limit = 3}) async {
    final found = await rows(
      'SELECT json FROM messages WHERE parent_id = ? '
      'ORDER BY ord DESC, seq DESC LIMIT ?',
      [parentId, limit],
    );
    return [for (final r in found.reversed) _message(r['json'] as String)!];
  }

  // ---- housekeeping ----

  /// Notes that a conversation was opened now: what it keeps depends on
  /// when it was last opened.
  Future<void> markOpened(String conversationId, DateTime now) => run(
    'UPDATE conversations SET opened_at = ? WHERE id = ?',
    [now.millisecondsSinceEpoch, conversationId],
    {},
  );

  /// The trimming rules (docs/CLIENTS.md section 6). For each
  /// conversation the cache keeps an unbroken run of messages ending at
  /// its newest:
  ///
  /// - opened in the last 7 days: everything (what the person scrolled
  ///   back through this week);
  /// - opened in the last 30 days: the newest [keep];
  /// - not opened for 30 days (or never): the newest [keepIdle].
  ///
  /// Thread replies are messages like any other and go with their
  /// conversation. Cleared notifications older than a week are dropped
  /// ("Earlier" holds a week). Returns how many messages were removed.
  Future<int> trim(DateTime now, {int keep = 500, int keepIdle = 50}) async {
    final week = now.subtract(const Duration(days: 7)).millisecondsSinceEpoch;
    final month = now.subtract(const Duration(days: 30)).millisecondsSinceEpoch;
    var removed = 0;
    await transaction(() async {
      for (final r in await rows('SELECT id, opened_at FROM conversations')) {
        final opened = r['opened_at'] as int?;
        if (opened != null && opened >= week) continue;
        final n = opened != null && opened >= month ? keep : keepIdle;
        final cut = await rows(
          'SELECT seq FROM messages WHERE conversation_id = ? AND seq IS NOT NULL '
          'ORDER BY seq DESC LIMIT 1 OFFSET ?',
          [r['id'], n],
        );
        if (cut.isEmpty) continue;
        final before =
            (await rows(
                  'SELECT COUNT(*) AS n FROM messages WHERE conversation_id = ?',
                  [r['id']],
                )).first['n']
                as int;
        await customStatement(
          'DELETE FROM messages WHERE conversation_id = ? AND seq <= ?',
          [r['id'], cut.first['seq']],
        );
        await customStatement(
          'UPDATE conversations SET history_complete = 0 WHERE id = ?',
          [r['id']],
        );
        final after =
            (await rows(
                  'SELECT COUNT(*) AS n FROM messages WHERE conversation_id = ?',
                  [r['id']],
                )).first['n']
                as int;
        removed += before - after;
      }
      await customStatement(
        'DELETE FROM notifications WHERE cleared_at IS NOT NULL AND cleared_at < ?',
        [week],
      );
    });
    if (removed > 0) touch({'messages', 'conversations'});
    touch({'notifications'});
    return removed;
  }
}

Message? _message(String raw) {
  final j = jsonDecode(raw);
  if (j is! Map || j['id'] == null) return null;
  return Message.fromJson(j.cast<String, dynamic>());
}
