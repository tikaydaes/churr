import 'dart:convert';

import '../api/conversation_ops.dart';
import '../models/json.dart';
import '../models/message.dart';
import 'cache_db.dart';

/// The server limits the composer and the message actions need, from
/// `getServer` (kept in the `me` table under `server_limits`).
class ServerLimits {
  const ServerLimits({
    this.fileSize = 0,
    this.messageText = 16000,
    this.quickCorrectionSeconds = 30,
    this.editWindowSeconds,
  });

  factory ServerLimits.fromJson(Json j) => ServerLimits(
    fileSize: asInt(j['file_size']),
    messageText: asInt(j['message_text'], 16000),
    quickCorrectionSeconds: asInt(j['quick_correction_seconds'], 30),
    editWindowSeconds: j['edit_window_seconds'] == null
        ? null
        : asInt(j['edit_window_seconds']),
  );

  /// The largest upload in bytes; 0 when not known yet.
  final int fileSize;
  final int messageText;
  final int quickCorrectionSeconds;

  /// Null means edits have no time limit.
  final int? editWindowSeconds;
}

/// The queries of the conversation screen, kept beside `CacheDb` in a
/// file of their own. Each `watch…` is what a screen draws from.
extension ConversationQueries on CacheDb {
  Message _m(Map<String, Object?> r) =>
      Message.fromJson(jsonDecode(r['json'] as String) as Json);

  /// A thread: the parent, then its replies sorted by `order`.
  Future<List<Message>> thread(String parentId) async => [
    for (final r in await rows(
      'SELECT json FROM messages WHERE id = ? OR parent_id = ? '
      'ORDER BY (id = ?) DESC, ord, seq',
      [parentId, parentId, parentId],
    ))
      _m(r),
  ];

  Stream<List<Message>> watchThread(String parentId) =>
      watch({'messages'}, () => thread(parentId));

  Stream<Map<String, ThreadState>> watchThreadStates(String conversationId) =>
      watch({'thread_states'}, () => threadStates(conversationId));

  /// The messages with these ids that are cached (quotes of messages
  /// outside the window on screen).
  Future<Map<String, Message>> messagesById(Iterable<String> ids) async {
    final list = ids.toSet().toList();
    if (list.isEmpty) return const {};
    final found = await rows(
      'SELECT json FROM messages WHERE id IN (${List.filled(list.length, '?').join(',')})',
      list,
    );
    return {for (final r in found) _m(r).id: _m(r)};
  }

  // ---- commands ----

  Future<void> putCommands(
    String conversationId,
    List<SlashCommand> commands,
  ) => run(
    'INSERT OR REPLACE INTO commands (conversation_id, json) VALUES (?, ?)',
    [
      conversationId,
      jsonEncode([for (final c in commands) c.raw]),
    ],
    {'commands'},
  );

  Future<List<SlashCommand>> commands(String conversationId) async {
    final r = await rows(
      'SELECT json FROM commands WHERE conversation_id = ?',
      [conversationId],
    );
    if (r.isEmpty) return const [];
    return [
      for (final c in jsonDecode(r.first['json'] as String) as List)
        if (c is Map) SlashCommand(c.cast<String, dynamic>()),
    ];
  }

  Stream<List<SlashCommand>> watchCommands(String conversationId) =>
      watch({'commands'}, () => commands(conversationId));

  // ---- server limits ----

  Future<ServerLimits> serverLimits() async {
    final r = await rows('SELECT json FROM me WHERE key = ?', [
      'server_limits',
    ]);
    return r.isEmpty
        ? const ServerLimits()
        : ServerLimits.fromJson(jsonDecode(r.first['json'] as String) as Json);
  }

  Future<void> putServerLimits(Json limits) => run(
    'INSERT OR REPLACE INTO me (key, json) VALUES (?, ?)',
    ['server_limits', jsonEncode(limits)],
    {'me'},
  );

  // ---- emoji used lately (per server, on this device) ----

  Future<List<String>> recentEmoji() async {
    final v = await setting('recent_emoji');
    if (v == null) return const [];
    return strings(jsonDecode(v));
  }

  /// Puts [emoji] first in the list of emoji used lately, kept to 24.
  Future<void> useEmoji(String emoji) async {
    final now = [
      emoji,
      for (final e in await recentEmoji())
        if (e != emoji) e,
    ].take(24).toList();
    await putSetting('recent_emoji', jsonEncode(now));
  }

  Stream<List<String>> watchRecentEmoji() => watch({'settings'}, recentEmoji);

  // ---- bookmarks (the table is the account area's) ----

  Future<Set<String>> bookmarkedIds(String conversationId) async => {
    for (final r in await rows(
      'SELECT message_id FROM bookmarks WHERE conversation_id = ?',
      [conversationId],
    ))
      r['message_id'] as String,
  };

  Stream<Set<String>> watchBookmarkedIds(String conversationId) =>
      watch({'bookmarks'}, () => bookmarkedIds(conversationId));
}
