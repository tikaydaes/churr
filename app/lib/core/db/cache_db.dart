import 'dart:convert';

import '../models/account.dart';
import '../models/chat_request.dart';
import '../models/conversation.dart';
import '../models/json.dart';
import '../models/message.dart';
import '../models/personal_layout.dart';
import '../models/server.dart';
import 'sql_db.dart';

/// A conversation as the cache holds it: the API object and what only
/// the device knows about it.
class ConversationRow {
  ConversationRow(this.conversation, this.syncedSeq, this.historyComplete);
  final Conversation conversation;

  /// The last sequence number applied (docs/CLIENTS.md section 7).
  final int syncedSeq;

  /// The cached run of messages reaches the conversation's first message,
  /// so there is nothing older to fetch.
  final bool historyComplete;
}

/// A message written here and not yet confirmed by the server.
class OutboxItem {
  OutboxItem({
    required this.clientId,
    required this.conversationId,
    required this.text,
    required this.createdAt,
    required this.state,
    this.parentId,
    this.error,
    this.extra,
  });

  /// The UUID that makes sending it twice safe, and that the server's
  /// event carries back so the two can be matched.
  final String clientId;
  final String conversationId;
  final String? parentId;
  final String text;
  final DateTime createdAt;

  /// `pending` (waiting to be sent, or being sent) or `failed` (the
  /// server refused it; the screen offers a retry).
  final String state;
  final String? error;

  /// What the post carries beyond its text: `quote` (the Quote object)
  /// and `file_ids`, with `files` (the uploaded File objects) so the
  /// pending row can draw them. Null for plain text.
  final Json? extra;

  bool get failed => state == 'failed';
}

/// `cache.db`: one signed-in server's local copy (docs/CLIENTS.md
/// section 6). It is the single source of truth for what the screens
/// show: sync writes it, screens watch it.
///
/// Objects are stored as the API's JSON with the few columns queries
/// need beside them, so a field this build does not know survives a
/// round trip and a new field needs no migration.
///
/// Tables of section 6 not made yet, because nothing reads them yet:
/// `saved_replies`, `commands`, `scheduled`. Add each with the screen that needs it, as a new schema
/// version.
class CacheDb extends SqlDb {
  CacheDb(super.executor);

  @override
  int get schemaVersion => 4;

  /// Version 2 (the account area): the notifications list keeps the
  /// message, its thread and why it is there; bookmarks, status presets
  /// and the parents of followed threads get tables of their own.
  @override
  Map<int, List<String>> get upgrades => const {
    1: [
      'ALTER TABLE notifications ADD COLUMN parent_id TEXT',
      'ALTER TABLE notifications ADD COLUMN reason TEXT',
      'ALTER TABLE notifications ADD COLUMN json TEXT',
      _bookmarksTable,
      _presetsTable,
      _threadParentsTable,
    ],
    2: _conversationV3,
    3: _orderV4,
  };

  /// Version 3 (the conversation screen): an outbox row carries what a
  /// post needs beyond its text (the quote, the uploaded files) as JSON
  /// in `extra`; the commands that can be typed in a conversation are
  /// kept whole per conversation, as `listCommands` returns them.
  static const _conversationV3 = [
    'ALTER TABLE outbox ADD COLUMN extra TEXT',
    'CREATE TABLE commands (conversation_id TEXT PRIMARY KEY, json TEXT NOT NULL)',
  ];

  /// Version 4 (`order`): messages are shown sorted by the server's
  /// `order`, kept in `ord`. A cached message whose JSON has no `order`
  /// (stored before the server sent it) cannot be placed, so the cached
  /// run of each conversation holding one is dropped whole, keeping
  /// every run unbroken; it is fetched again when the conversation is
  /// next opened.
  static const _orderV4 = [
    "ALTER TABLE messages ADD COLUMN ord TEXT NOT NULL DEFAULT ''",
    "UPDATE messages SET ord = COALESCE(json_extract(json, '\$.order'), '')",
    "UPDATE conversations SET history_complete = 0 WHERE id IN "
        "(SELECT conversation_id FROM messages WHERE ord = '' OR seq IS NULL)",
    "DELETE FROM messages WHERE conversation_id IN "
        "(SELECT conversation_id FROM messages WHERE ord = '' OR seq IS NULL)",
    _messagesByOrd,
  ];

  static const _messagesByOrd =
      'CREATE INDEX messages_by_ord ON messages (conversation_id, ord)';

  static const _bookmarksTable =
      'CREATE TABLE bookmarks ('
      'message_id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, '
      'created_at INTEGER NOT NULL, json TEXT NOT NULL)';
  static const _presetsTable =
      'CREATE TABLE status_presets ('
      'id TEXT PRIMARY KEY, position INTEGER NOT NULL, json TEXT NOT NULL)';

  /// The parent messages of followed threads, as `listThreads` returns
  /// them. Kept apart from `messages`, whose rows must stay one unbroken
  /// run per conversation.
  static const _threadParentsTable =
      'CREATE TABLE thread_parents ('
      'parent_id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, '
      'json TEXT NOT NULL)';

  @override
  List<String> get schema => const [
    // The signed-in account and other single values (the status summary,
    // the server's description), by name.
    'CREATE TABLE me (key TEXT PRIMARY KEY, json TEXT NOT NULL)',
    'CREATE TABLE accounts (id TEXT PRIMARY KEY, json TEXT NOT NULL)',
    'CREATE TABLE groups (id TEXT PRIMARY KEY, json TEXT NOT NULL)',
    'CREATE TABLE emoji (id TEXT PRIMARY KEY, json TEXT NOT NULL)',
    'CREATE TABLE sections (id TEXT PRIMARY KEY, position INTEGER NOT NULL, json TEXT NOT NULL)',
    'CREATE TABLE conversations ('
        'id TEXT PRIMARY KEY, json TEXT NOT NULL, '
        'synced_seq INTEGER NOT NULL DEFAULT 0, '
        'history_complete INTEGER NOT NULL DEFAULT 0, '
        'opened_at INTEGER)',
    'CREATE TABLE messages ('
        'id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, '
        'seq INTEGER, parent_id TEXT, created_at INTEGER NOT NULL, '
        "json TEXT NOT NULL, ord TEXT NOT NULL DEFAULT '')",
    'CREATE INDEX messages_by_seq ON messages (conversation_id, seq)',
    _messagesByOrd,
    'CREATE INDEX messages_by_parent ON messages (parent_id)',
    'CREATE TABLE thread_states ('
        'parent_id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, '
        'json TEXT NOT NULL)',
    'CREATE TABLE notifications ('
        'message_id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, '
        'seq INTEGER NOT NULL, at INTEGER NOT NULL, kind TEXT NOT NULL, '
        'cleared_at INTEGER, parent_id TEXT, reason TEXT, json TEXT)',
    'CREATE TABLE drafts ('
        'conversation_id TEXT NOT NULL, parent_id TEXT NOT NULL DEFAULT \'\', '
        'text TEXT NOT NULL, PRIMARY KEY (conversation_id, parent_id))',
    'CREATE TABLE outbox ('
        'client_id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, '
        'parent_id TEXT, text TEXT NOT NULL, created_at INTEGER NOT NULL, '
        'state TEXT NOT NULL, error TEXT)',
    'CREATE TABLE files (id TEXT PRIMARY KEY, path TEXT NOT NULL, size INTEGER NOT NULL, opened_at INTEGER NOT NULL)',
    'CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)',
    _bookmarksTable,
    _presetsTable,
    _threadParentsTable,
    ..._conversationV3,
  ];

  // ---- single values ----

  Future<Json?> _value(String key) async {
    final r = await rows('SELECT json FROM me WHERE key = ?', [key]);
    return r.isEmpty ? null : jsonDecode(r.first['json'] as String) as Json;
  }

  Future<void> _putValue(String key, Json v) => run(
    'INSERT OR REPLACE INTO me (key, json) VALUES (?, ?)',
    [key, jsonEncode(v)],
    {'me'},
  );

  Future<Me?> me() async {
    final j = await _value('account');
    return j == null ? null : Me.fromJson(j);
  }

  Future<void> putMe(Me me) => _putValue('account', me.raw);

  Future<StatusSummary> statusSummary() async =>
      StatusSummary.fromJson(await _value('status') ?? const {});

  Future<void> putStatusSummary(StatusSummary s) =>
      _putValue('status', s.toJson());

  /// Parents of sub-channels the account is in without being in the
  /// parent itself (docs/CLIENTS.md 9.25): public channels the sidebar
  /// needs the name and section of, to draw `parent › name`. They are
  /// not conversations of this account, so they are kept apart from the
  /// `conversations` table and never appear in `positions`.
  Future<Map<String, Conversation>> outsideParents() async => {
    for (final e in (await _value('outside_parents') ?? const {}).entries)
      if (e.value is Map)
        e.key: Conversation.fromJson((e.value as Map).cast<String, dynamic>()),
  };

  Future<void> putOutsideParents(Iterable<Conversation> parents) =>
      _putValue('outside_parents', {for (final c in parents) c.id: c.raw});

  Stream<Map<String, Conversation>> watchOutsideParents() =>
      watch({'me'}, outsideParents);

  Stream<StatusSummary> watchStatusSummary() => watch({'me'}, statusSummary);

  /// The person's own sidebar on this server (ruling R83), as the server
  /// last sent it.
  Future<PersonalLayout> sidebarLayout() async {
    final j = await _value('sidebar');
    return j == null ? PersonalLayout.empty : PersonalLayout.fromJson(j);
  }

  /// Keeps [l] unless the one kept is newer.
  Future<void> putSidebarLayout(PersonalLayout l) async {
    if ((await sidebarLayout()).version > l.version) return;
    await _putValue('sidebar', l.toJson());
  }

  Stream<PersonalLayout> watchSidebarLayout() => watch({'me'}, sidebarLayout);
  Stream<Me?> watchMe() => watch({'me'}, me);

  /// The chat requests waiting for the account (ruling R78): fetched whole
  /// after `ready`, and kept by chat_request.received and .removed.
  Future<List<ChatRequest>> chatRequests() async => listOf(
    (await _value('chat_requests') ?? const {})['requests'],
    ChatRequest.fromJson,
  );

  Future<void> putChatRequests(Iterable<ChatRequest> list) =>
      _putValue('chat_requests', {
        'requests': [for (final r in list) r.raw],
      });

  Stream<List<ChatRequest>> watchChatRequests() => watch({'me'}, chatRequests);

  // ---- per-server choices made on this device ----

  Future<String?> setting(String key) async {
    final r = await rows('SELECT value FROM settings WHERE key = ?', [key]);
    return r.isEmpty ? null : r.first['value'] as String;
  }

  Future<void> putSetting(String key, String value) => run(
    'INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)',
    [key, value],
    {'settings'},
  );

  // ---- accounts and the small lists ----

  Future<void> upsertAccounts(Iterable<Account> accounts) async {
    for (final a in accounts) {
      await customStatement(
        'INSERT OR REPLACE INTO accounts (id, json) VALUES (?, ?)',
        [a.id, jsonEncode(a.raw)],
      );
    }
    touch({'accounts'});
  }

  Future<Map<String, Account>> accounts() async => {
    for (final r in await rows('SELECT json FROM accounts'))
      (jsonDecode(r['json'] as String) as Json)['id'] as String:
          Account.fromJson(jsonDecode(r['json'] as String) as Json),
  };

  Future<Account?> account(String id) async {
    final r = await rows('SELECT json FROM accounts WHERE id = ?', [id]);
    return r.isEmpty
        ? null
        : Account.fromJson(jsonDecode(r.first['json'] as String) as Json);
  }

  Stream<Map<String, Account>> watchAccounts() => watch({'accounts'}, accounts);

  /// Replaces a small list whole, as the server sends it.
  Future<void> _replaceList(
    String table,
    Iterable<(String id, int position, Json json)> rows,
  ) => transaction(() async {
    await customStatement('DELETE FROM $table');
    for (final (id, position, json) in rows) {
      if (table == 'sections') {
        await customStatement(
          'INSERT INTO sections (id, position, json) VALUES (?, ?, ?)',
          [id, position, jsonEncode(json)],
        );
      } else {
        await customStatement('INSERT INTO $table (id, json) VALUES (?, ?)', [
          id,
          jsonEncode(json),
        ]);
      }
    }
    touch({table});
  });

  Future<void> replaceSections(List<Section> s) => _replaceList('sections', [
    for (final x in s) (x.id, x.position, x.toJson()),
  ]);
  Future<void> replaceGroups(List<Group> g) =>
      _replaceList('groups', [for (final x in g) (x.id, 0, x.toJson())]);
  Future<void> replaceEmoji(List<CustomEmoji> e) =>
      _replaceList('emoji', [for (final x in e) (x.id, 0, x.toJson())]);

  Future<List<Section>> sections() async => [
    for (final r in await rows(
      'SELECT json FROM sections ORDER BY position, id',
    ))
      Section.fromJson(jsonDecode(r['json'] as String) as Json),
  ];

  Future<Map<String, Group>> groups() async => {
    for (final r in await rows('SELECT id, json FROM groups'))
      r['id'] as String: Group.fromJson(
        jsonDecode(r['json'] as String) as Json,
      ),
  };

  Future<Map<String, CustomEmoji>> emoji() async => {
    for (final r in await rows('SELECT id, json FROM emoji'))
      r['id'] as String: CustomEmoji.fromJson(
        jsonDecode(r['json'] as String) as Json,
      ),
  };

  Stream<List<Section>> watchSections() => watch({'sections'}, sections);
  Stream<Map<String, Group>> watchGroups() => watch({'groups'}, groups);
  Stream<Map<String, CustomEmoji>> watchEmoji() => watch({'emoji'}, emoji);

  // ---- conversations ----

  ConversationRow _conversationRow(Map<String, Object?> r) => ConversationRow(
    Conversation.fromJson(jsonDecode(r['json'] as String) as Json),
    r['synced_seq'] as int,
    r['history_complete'] == 1,
  );

  Future<List<ConversationRow>> conversations() async => [
    for (final r in await rows(
      'SELECT json, synced_seq, history_complete FROM conversations',
    ))
      _conversationRow(r),
  ];

  Future<ConversationRow?> conversation(String id) async {
    final r = await rows(
      'SELECT json, synced_seq, history_complete FROM conversations WHERE id = ?',
      [id],
    );
    return r.isEmpty ? null : _conversationRow(r.first);
  }

  Stream<List<ConversationRow>> watchConversations() =>
      watch({'conversations'}, conversations);

  Stream<ConversationRow?> watchConversation(String id) =>
      watch({'conversations'}, () => conversation(id));

  /// The `positions` map of the `resume` frame: every conversation held
  /// and the last sequence number applied to it.
  Future<Map<String, int>> positions() async => {
    for (final r in await rows('SELECT id, synced_seq FROM conversations'))
      r['id'] as String: r['synced_seq'] as int,
  };

  /// Writes a conversation as the server sent it. An existing row keeps
  /// its `synced_seq` unless [syncedSeq] is given: the API object says
  /// where the conversation is, not what this device has applied.
  Future<void> upsertConversation(Conversation c, {int? syncedSeq}) async {
    await customStatement(
      'INSERT INTO conversations (id, json, synced_seq) VALUES (?, ?, ?) '
      'ON CONFLICT (id) DO UPDATE SET json = excluded.json'
      '${syncedSeq == null ? '' : ', synced_seq = excluded.synced_seq'}',
      [c.id, jsonEncode(c.toJson()), syncedSeq ?? 0],
    );
    touch({'conversations'});
  }

  Future<void> setSyncedSeq(String id, int seq) => run(
    'UPDATE conversations SET synced_seq = ? WHERE id = ?',
    [seq, id],
    {'conversations'},
  );

  Future<void> setHistoryComplete(String id, bool complete) => run(
    'UPDATE conversations SET history_complete = ? WHERE id = ?',
    [complete ? 1 : 0, id],
    {'conversations'},
  );

  /// Drops a conversation and everything held for it.
  Future<void> deleteConversation(String id) => transaction(() async {
    for (final t in const [
      'messages',
      'thread_states',
      'notifications',
      'outbox',
      'drafts',
    ]) {
      await customStatement('DELETE FROM $t WHERE conversation_id = ?', [id]);
    }
    await customStatement('DELETE FROM conversations WHERE id = ?', [id]);
    touch({
      'conversations',
      'messages',
      'thread_states',
      'notifications',
      'outbox',
    });
  });

  // ---- messages ----

  Future<void> upsertMessages(Iterable<Message> messages) async {
    var any = false;
    for (final m in messages) {
      any = true;
      await customStatement(
        'INSERT OR REPLACE INTO messages '
        '(id, conversation_id, seq, parent_id, created_at, json, ord) '
        'VALUES (?, ?, ?, ?, ?, ?, ?)',
        [
          m.id,
          m.conversationId,
          m.seq,
          m.parentId,
          m.createdAt.millisecondsSinceEpoch,
          jsonEncode(m.raw),
          m.order,
        ],
      );
    }
    if (any) touch({'messages'});
  }

  Future<Message?> message(String id) async {
    final r = await rows('SELECT json FROM messages WHERE id = ?', [id]);
    return r.isEmpty
        ? null
        : Message.fromJson(jsonDecode(r.first['json'] as String) as Json);
  }

  /// The [limit] cached messages of a conversation that arrived last
  /// (the window is chosen by `seq`, which is what paging goes by),
  /// sorted for display by `order`. Replies are among them; the screen
  /// decides whether to draw them.
  Future<List<Message>> messages(
    String conversationId, {
    int limit = 100,
  }) async {
    final found = await rows(
      'SELECT json FROM (SELECT json, ord, seq FROM messages '
      'WHERE conversation_id = ? ORDER BY seq DESC LIMIT ?) '
      'ORDER BY ord, seq',
      [conversationId, limit],
    );
    return [
      for (final r in found)
        Message.fromJson(jsonDecode(r['json'] as String) as Json),
    ];
  }

  Stream<List<Message>> watchMessages(
    String conversationId, {
    int limit = 100,
  }) => watch({'messages'}, () => messages(conversationId, limit: limit));

  /// The lowest sequence number cached for a conversation, or null.
  Future<int?> oldestSeq(String conversationId) async {
    final r = await rows(
      'SELECT MIN(seq) AS s FROM messages WHERE conversation_id = ?',
      [conversationId],
    );
    return r.first['s'] as int?;
  }

  Future<int> messageCount(String conversationId) async {
    final r = await rows(
      'SELECT COUNT(*) AS n FROM messages WHERE conversation_id = ?',
      [conversationId],
    );
    return r.first['n'] as int;
  }

  Future<void> deleteMessages(String conversationId) async {
    await customStatement('DELETE FROM messages WHERE conversation_id = ?', [
      conversationId,
    ]);
    await customStatement(
      'UPDATE conversations SET history_complete = 0 WHERE id = ?',
      [conversationId],
    );
    touch({'messages', 'conversations'});
  }

  Future<void> deleteReplies(String parentId) =>
      run('DELETE FROM messages WHERE parent_id = ?', [parentId], {'messages'});

  // ---- thread states ----

  Future<void> upsertThreadStates(Iterable<ThreadState> states) async {
    for (final s in states) {
      await customStatement(
        'INSERT OR REPLACE INTO thread_states (parent_id, conversation_id, json) '
        'VALUES (?, ?, ?)',
        [s.parentId, s.conversationId, jsonEncode(s.toJson())],
      );
    }
    touch({'thread_states'});
  }

  Future<void> deleteThreadState(String parentId) => run(
    'DELETE FROM thread_states WHERE parent_id = ?',
    [parentId],
    {'thread_states'},
  );

  Future<Map<String, ThreadState>> threadStates(String conversationId) async =>
      {
        for (final r in await rows(
          'SELECT parent_id, json FROM thread_states WHERE conversation_id = ?',
          [conversationId],
        ))
          r['parent_id'] as String: ThreadState.fromJson(
            jsonDecode(r['json'] as String) as Json,
          ),
      };

  // ---- the notifications list (docs/CLIENTS.md section 8.3) ----

  /// Records a message that notified or would have. [kind] is the
  /// event's `notify` value: `notify` or `badge`. The message is kept
  /// with the item, so the list outlives the trimming of `messages`.
  /// [reason] is why it is here (`lib/state/notifications.dart`); null
  /// lets the list work it out from the message.
  Future<void> addNotification(Message m, String kind, {String? reason}) => run(
    'INSERT OR IGNORE INTO notifications '
    '(message_id, conversation_id, seq, at, kind, parent_id, reason, json) '
    'VALUES (?, ?, ?, ?, ?, ?, ?, ?)',
    [
      m.id,
      m.conversationId,
      m.seq,
      m.createdAt.millisecondsSinceEpoch,
      kind,
      m.parentId,
      reason,
      jsonEncode(m.raw),
    ],
    {'notifications'},
  );

  /// Clears the items the conversation's read position has passed: its
  /// top-level messages, and replies in threads that have no position of
  /// their own (docs/FEATURES.md 2.1: a thread's position is the
  /// conversation's unless the account has a row for it).
  Future<void> clearNotifications(
    String conversationId,
    int readSeq,
    DateTime now,
  ) => run(
    'UPDATE notifications SET cleared_at = ? '
    'WHERE conversation_id = ? AND seq <= ? AND cleared_at IS NULL '
    'AND (parent_id IS NULL OR parent_id NOT IN '
    '(SELECT parent_id FROM thread_states WHERE conversation_id = ?))',
    [now.millisecondsSinceEpoch, conversationId, readSeq, conversationId],
    {'notifications'},
  );

  /// Clears the replies a thread's read position has passed.
  Future<void> clearThreadNotifications(
    String parentId,
    int readSeq,
    DateTime now,
  ) => run(
    'UPDATE notifications SET cleared_at = ? '
    'WHERE parent_id = ? AND seq <= ? AND cleared_at IS NULL',
    [now.millisecondsSinceEpoch, parentId, readSeq],
    {'notifications'},
  );

  /// The items of the notifications list, newest first: those not
  /// cleared, and those cleared since [clearedSince] ("Earlier").
  Future<List<NotificationRow>> notifications({
    required DateTime clearedSince,
  }) async => [
    for (final r in await rows(
      'SELECT * FROM notifications WHERE cleared_at IS NULL OR cleared_at >= ? '
      'ORDER BY at DESC, seq DESC',
      [clearedSince.millisecondsSinceEpoch],
    ))
      NotificationRow.fromRow(r),
  ];

  Stream<List<NotificationRow>> watchNotifications({
    required DateTime clearedSince,
  }) =>
      watch({'notifications'}, () => notifications(clearedSince: clearedSince));

  Future<void> deleteClearedNotifications(DateTime before) => run(
    'DELETE FROM notifications WHERE cleared_at IS NOT NULL AND cleared_at < ?',
    [before.millisecondsSinceEpoch],
    {'notifications'},
  );

  Future<int> unclearedNotifications() async {
    final r = await rows(
      'SELECT COUNT(*) AS n FROM notifications WHERE cleared_at IS NULL',
    );
    return r.first['n'] as int;
  }

  // ---- drafts ----

  Future<String> draft(String conversationId, {String parentId = ''}) async {
    final r = await rows(
      'SELECT text FROM drafts WHERE conversation_id = ? AND parent_id = ?',
      [conversationId, parentId],
    );
    return r.isEmpty ? '' : r.first['text'] as String;
  }

  Future<void> putDraft(
    String conversationId,
    String text, {
    String parentId = '',
  }) => text.isEmpty
      ? run(
          'DELETE FROM drafts WHERE conversation_id = ? AND parent_id = ?',
          [conversationId, parentId],
          {'drafts'},
        )
      : run(
          'INSERT OR REPLACE INTO drafts (conversation_id, parent_id, text) VALUES (?, ?, ?)',
          [conversationId, parentId, text],
          {'drafts'},
        );

  // ---- the outbox ----

  OutboxItem _outboxItem(Map<String, Object?> r) => OutboxItem(
    clientId: r['client_id'] as String,
    conversationId: r['conversation_id'] as String,
    parentId: r['parent_id'] as String?,
    text: r['text'] as String,
    createdAt: DateTime.fromMillisecondsSinceEpoch(r['created_at'] as int),
    state: r['state'] as String,
    error: r['error'] as String?,
    extra: r['extra'] == null ? null : jsonDecode(r['extra'] as String) as Json,
  );

  Future<void> addToOutbox(OutboxItem item) => run(
    'INSERT INTO outbox (client_id, conversation_id, parent_id, text, created_at, state, extra) '
    'VALUES (?, ?, ?, ?, ?, ?, ?)',
    [
      item.clientId,
      item.conversationId,
      item.parentId,
      item.text,
      item.createdAt.millisecondsSinceEpoch,
      item.state,
      item.extra == null ? null : jsonEncode(item.extra),
    ],
    {'outbox'},
  );

  /// The outbox, oldest first; of one conversation, or all of it.
  Future<List<OutboxItem>> outbox({String? conversationId}) async => [
    for (final r in await rows(
      'SELECT * FROM outbox ${conversationId == null ? '' : 'WHERE conversation_id = ? '}'
      'ORDER BY created_at, client_id',
      [?conversationId],
    ))
      _outboxItem(r),
  ];

  Stream<List<OutboxItem>> watchOutbox(String conversationId) =>
      watch({'outbox'}, () => outbox(conversationId: conversationId));

  Future<void> setOutboxState(String clientId, String state, {String? error}) =>
      run(
        'UPDATE outbox SET state = ?, error = ? WHERE client_id = ?',
        [state, error, clientId],
        {'outbox'},
      );

  Future<void> removeFromOutbox(String clientId) =>
      run('DELETE FROM outbox WHERE client_id = ?', [clientId], {'outbox'});
}

/// One item of the notifications list as `cache.db` holds it
/// (docs/CLIENTS.md 8.3).
class NotificationRow {
  NotificationRow({
    required this.messageId,
    required this.conversationId,
    required this.seq,
    required this.at,
    required this.kind,
    this.parentId,
    this.reason,
    this.message,
    this.clearedAt,
  });

  factory NotificationRow.fromRow(Map<String, Object?> r) {
    final raw = r['json'] as String?;
    return NotificationRow(
      messageId: r['message_id'] as String,
      conversationId: r['conversation_id'] as String,
      seq: r['seq'] as int,
      at: DateTime.fromMillisecondsSinceEpoch(r['at'] as int),
      kind: r['kind'] as String,
      parentId: r['parent_id'] as String?,
      reason: r['reason'] as String?,
      message: raw == null ? null : Message.fromJson(jsonDecode(raw) as Json),
      clearedAt: r['cleared_at'] == null
          ? null
          : DateTime.fromMillisecondsSinceEpoch(r['cleared_at'] as int),
    );
  }

  final String messageId;
  final String conversationId;
  final int seq;
  final DateTime at;

  /// The event's `notify` value: `notify` or `badge`.
  final String kind;
  final String? parentId;
  final String? reason;

  /// The message as it was when the item was written; null for items
  /// written before version 2.
  final Message? message;
  final DateTime? clearedAt;

  bool get cleared => clearedAt != null;
}
