import 'dart:convert';

import 'sql_db.dart';

/// A server the app is signed in to, as `app.db` lists it.
class ServerEntry {
  ServerEntry({
    required this.localId,
    required this.address,
    required this.name,
    required this.position,
  });

  /// The random identifier naming this server's folder and its tokens.
  final String localId;

  /// The server's origin, such as `https://chat.example.org`.
  final String address;

  /// The community's name, kept so a tile can be drawn before any
  /// request finishes.
  final String name;

  /// The person's order of servers.
  final int position;
}

/// A group the person made in the hybrid layout (docs/CLIENTS.md 9.1).
class HybridGroup {
  HybridGroup(this.id, this.name, this.position);
  final String id;
  final String name;
  final int position;
}

/// Where a conversation sits in the hybrid arrangement. It is named by
/// its server's address and its identifier, never by a local id, so an
/// arrangement copied to another device falls into place there.
class HybridPlacement {
  HybridPlacement(
    this.serverAddress,
    this.conversationId,
    this.groupId,
    this.position,
  );
  final String serverAddress;
  final String conversationId;
  final String groupId;
  final int position;
}

/// `app.db`: what belongs to the app and to no server (docs/CLIENTS.md
/// section 8.2): the list of servers and their order, the preferences,
/// and the hybrid arrangement. No server ever sees any of it.
class AppDb extends SqlDb {
  AppDb(super.executor);

  @override
  int get schemaVersion => 1;

  @override
  List<String> get schema => const [
    'CREATE TABLE servers ('
        'local_id TEXT PRIMARY KEY, address TEXT NOT NULL, '
        'name TEXT NOT NULL, position INTEGER NOT NULL)',
    'CREATE TABLE prefs (key TEXT PRIMARY KEY, value TEXT NOT NULL)',
    'CREATE TABLE hybrid_groups ('
        'id TEXT PRIMARY KEY, name TEXT NOT NULL, position INTEGER NOT NULL)',
    'CREATE TABLE hybrid_items ('
        'server_address TEXT NOT NULL, conversation_id TEXT NOT NULL, '
        'group_id TEXT NOT NULL, position INTEGER NOT NULL, '
        'PRIMARY KEY (server_address, conversation_id))',
  ];

  // ---- servers ----

  Future<List<ServerEntry>> servers() async => [
    for (final r in await rows(
      'SELECT * FROM servers ORDER BY position, local_id',
    ))
      ServerEntry(
        localId: r['local_id'] as String,
        address: r['address'] as String,
        name: r['name'] as String,
        position: r['position'] as int,
      ),
  ];

  Future<void> addServer(ServerEntry s) => run(
    'INSERT OR REPLACE INTO servers (local_id, address, name, position) VALUES (?, ?, ?, ?)',
    [s.localId, s.address, s.name, s.position],
    {'servers'},
  );

  Future<void> removeServer(String localId) =>
      run('DELETE FROM servers WHERE local_id = ?', [localId], {'servers'});

  /// Writes the person's order of servers.
  Future<void> orderServers(List<String> localIds) => transaction(() async {
    for (var i = 0; i < localIds.length; i++) {
      await customStatement(
        'UPDATE servers SET position = ? WHERE local_id = ?',
        [i, localIds[i]],
      );
    }
    touch({'servers'});
  });

  // ---- preferences ----

  Future<Map<String, String>> prefs() async => {
    for (final r in await rows('SELECT key, value FROM prefs'))
      r['key'] as String: r['value'] as String,
  };

  Future<void> putPref(String key, String value) => run(
    'INSERT OR REPLACE INTO prefs (key, value) VALUES (?, ?)',
    [key, value],
    {'prefs'},
  );

  /// A preference holding a JSON list of strings (collapsed headings).
  static List<String> decodeList(String? raw) {
    if (raw == null) return [];
    try {
      return (jsonDecode(raw) as List).cast<String>();
    } on Object {
      return [];
    }
  }

  // ---- the hybrid arrangement ----

  Future<List<HybridGroup>> hybridGroups() async => [
    for (final r in await rows(
      'SELECT * FROM hybrid_groups ORDER BY position, id',
    ))
      HybridGroup(r['id'] as String, r['name'] as String, r['position'] as int),
  ];

  Future<List<HybridPlacement>> hybridPlacements() async => [
    for (final r in await rows('SELECT * FROM hybrid_items ORDER BY position'))
      HybridPlacement(
        r['server_address'] as String,
        r['conversation_id'] as String,
        r['group_id'] as String,
        r['position'] as int,
      ),
  ];

  Future<void> putHybridGroup(HybridGroup g) => run(
    'INSERT OR REPLACE INTO hybrid_groups (id, name, position) VALUES (?, ?, ?)',
    [g.id, g.name, g.position],
    {'hybrid_groups'},
  );

  /// Deletes a group. Its conversations go back to "Everything else":
  /// nothing else is deleted.
  Future<void> deleteHybridGroup(String id) => transaction(() async {
    await customStatement('DELETE FROM hybrid_items WHERE group_id = ?', [id]);
    await customStatement('DELETE FROM hybrid_groups WHERE id = ?', [id]);
    touch({'hybrid_groups', 'hybrid_items'});
  });

  /// Puts a conversation in a group (it is in one at most), or with a
  /// null [groupId] back under "Everything else".
  Future<void> placeConversation(
    String serverAddress,
    String conversationId,
    String? groupId, {
    int position = 0,
  }) => groupId == null
      ? run(
          'DELETE FROM hybrid_items WHERE server_address = ? AND conversation_id = ?',
          [serverAddress, conversationId],
          {'hybrid_items'},
        )
      : run(
          'INSERT OR REPLACE INTO hybrid_items '
          '(server_address, conversation_id, group_id, position) VALUES (?, ?, ?, ?)',
          [serverAddress, conversationId, groupId, position],
          {'hybrid_items'},
        );
}
