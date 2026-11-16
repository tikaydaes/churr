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

/// `app.db`: what belongs to the app and to no server (docs/CLIENTS.md
/// section 8.2): the list of servers and their order, and the
/// preferences. No server ever sees any of it.
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
}
