// The real SealedStore (lib/core/sealed/store.dart): `sealed.db`, a SQLite
// file encrypted at rest (docs/CLIENTS.md section 6), beside a server's
// `cache.db`.
//
// - The file is encrypted by SQLite3MultipleCiphers, which package:sqlite3
//   bundles in place of SQLite (pubspec.yaml, `hooks`). Its 32-byte key is
//   made on first open and kept in the platform's secure storage
//   ([SecretStore]); it never touches the disk beside the file.
// - The device's two private keys (its Ed25519 seed and X25519 private
//   key) are not written into the file at all: they are lifted out of the
//   library's device record on the way in, kept in secure storage, and
//   put back on the way out. Without them the record reads as no device.
// - Besides the library's values (every chat's epochs, secrets, pins,
//   verified marks, counters and the decrypted messages it shows), the
//   file keeps what the app adds about each sealed row: the server's row
//   id, its time and its place, so the screens can draw a decrypted
//   message where the server put it.
//
// Plain Dart (no Flutter), so background code can open it too.

import 'dart:convert';
import 'dart:io';
import 'dart:math';
import 'dart:typed_data';

import 'package:sqlite3/sqlite3.dart';

import '../sealed/sealed.dart' show SealedStore;
import '../session_store.dart';

/// What the app remembers about one sealed row it has taken in.
class SealedRowMeta {
  SealedRowMeta({
    required this.conversationId,
    required this.seq,
    required this.messageId,
    required this.reference,
    required this.createdAt,
    this.order = '',
    this.state = 'ok',
  });

  final String conversationId;
  final int seq;

  /// The server's row id; empty for a message still being sent.
  final String messageId;

  /// The sender's message reference (its `client_id`).
  final String reference;
  final DateTime createdAt;
  final String order;

  /// `ok`, `unchecked` (failed its checks), `nokey` (its epoch's secret
  /// has not arrived yet), `quiet` (a reaction, edit, tick and the like),
  /// `dropped` (a replay).
  final String state;
}

/// The names of the secure-storage entries for one server's sealed store.
class SealedSecretNames {
  const SealedSecretNames(this.scope);

  /// The server's local id.
  final String scope;
  String get databaseKey => 'churr.sealed.$scope.dbkey';
  String get deviceKeys => 'churr.sealed.$scope.device';
}

const _deviceKey = 'sealed/device';

class SqliteSealedStore implements SealedStore {
  SqliteSealedStore._(this._db, this._secrets, this.names, this.path);

  final Database _db;
  final SecretStore _secrets;
  final SealedSecretNames names;

  /// The file, or null for one in memory (tests).
  final String? path;

  /// Opens (making if need be) the store at [path], or in memory when
  /// [path] is null. The key comes from [secrets], made there the first
  /// time. A file whose key is lost cannot be read: it is deleted and
  /// started again empty, and the device unlocks again from the backup.
  static Future<SqliteSealedStore> open({
    required String? path,
    required SecretStore secrets,
    required String scope,
  }) async {
    final names = SealedSecretNames(scope);
    var keyHex = await secrets.read(names.databaseKey);
    if (keyHex == null) {
      final r = Random.secure();
      keyHex = [
        for (var i = 0; i < 32; i++)
          r.nextInt(256).toRadixString(16).padLeft(2, '0'),
      ].join();
      if (path != null && File(path).existsSync()) File(path).deleteSync();
      await secrets.write(names.databaseKey, keyHex);
    }
    Database db;
    try {
      db = _openWith(path, keyHex);
    } on SqliteException {
      // The key in secure storage does not open this file.
      if (path != null && File(path).existsSync()) File(path).deleteSync();
      db = _openWith(path, keyHex);
    }
    return SqliteSealedStore._(db, secrets, names, path);
  }

  static Database _openWith(String? path, String keyHex) {
    final db = path == null ? sqlite3.openInMemory() : sqlite3.open(path);
    if (path != null) {
      db.execute("PRAGMA key = \"x'$keyHex'\"");
    }
    // Reading the schema is what tells a wrong key.
    db.select('SELECT count(*) FROM sqlite_master');
    db.execute('''
      CREATE TABLE IF NOT EXISTS kv (key TEXT PRIMARY KEY, value BLOB NOT NULL);
      CREATE TABLE IF NOT EXISTS sealed_rows (
        conversation_id TEXT NOT NULL,
        seq INTEGER NOT NULL,
        message_id TEXT NOT NULL,
        reference TEXT NOT NULL,
        created_at INTEGER NOT NULL,
        ord TEXT NOT NULL DEFAULT '',
        state TEXT NOT NULL,
        PRIMARY KEY (conversation_id, seq)
      );
      CREATE INDEX IF NOT EXISTS sealed_rows_ref ON sealed_rows (conversation_id, reference);
    ''');
    return db;
  }

  bool _closed = false;

  // ---- SealedStore ----

  @override
  Future<Uint8List?> read(String key) async {
    final rows = _db.select('SELECT value FROM kv WHERE key = ?', [key]);
    if (rows.isEmpty) return null;
    final v = rows.first['value'] as Uint8List;
    if (key != _deviceKey) return v;
    // Put the private keys back.
    final keys = await _secrets.read(names.deviceKeys);
    if (keys == null) return null;
    final j = (jsonDecode(utf8.decode(v)) as Map).cast<String, Object?>();
    final k = (jsonDecode(keys) as Map).cast<String, Object?>();
    j['sign_seed'] = k['sign_seed'];
    j['box'] = k['box'];
    return Uint8List.fromList(utf8.encode(jsonEncode(j)));
  }

  @override
  Future<void> write(String key, Uint8List value) async {
    var v = value;
    if (key == _deviceKey) {
      final j = (jsonDecode(utf8.decode(value)) as Map).cast<String, Object?>();
      final k = {'sign_seed': j['sign_seed'], 'box': j['box']};
      final had = await _secrets.read(names.deviceKeys);
      final next = jsonEncode(k);
      if (had != next) await _secrets.write(names.deviceKeys, next);
      j['sign_seed'] = '';
      j['box'] = '';
      v = Uint8List.fromList(utf8.encode(jsonEncode(j)));
    }
    _db.execute('INSERT OR REPLACE INTO kv (key, value) VALUES (?, ?)', [
      key,
      v,
    ]);
  }

  @override
  Future<void> delete(String key) async {
    _db.execute('DELETE FROM kv WHERE key = ?', [key]);
    if (key == _deviceKey) await _secrets.delete(names.deviceKeys);
  }

  @override
  Future<List<String>> keys() async => [
    for (final r in _db.select('SELECT key FROM kv')) r['key'] as String,
  ];

  // ---- what the app adds ----

  void putRow(SealedRowMeta m) => _db.execute(
    'INSERT OR REPLACE INTO sealed_rows '
    '(conversation_id, seq, message_id, reference, created_at, ord, state) '
    'VALUES (?, ?, ?, ?, ?, ?, ?)',
    [
      m.conversationId,
      m.seq,
      m.messageId,
      m.reference,
      m.createdAt.millisecondsSinceEpoch,
      m.order,
      m.state,
    ],
  );

  SealedRowMeta _meta(Row r) => SealedRowMeta(
    conversationId: r['conversation_id'] as String,
    seq: r['seq'] as int,
    messageId: r['message_id'] as String,
    reference: r['reference'] as String,
    createdAt: DateTime.fromMillisecondsSinceEpoch(
      r['created_at'] as int,
      isUtc: true,
    ),
    order: r['ord'] as String,
    state: r['state'] as String,
  );

  /// Every row taken in for a chat, by seq.
  List<SealedRowMeta> rows(String conversationId) => [
    for (final r in _db.select(
      'SELECT * FROM sealed_rows WHERE conversation_id = ? ORDER BY seq',
      [conversationId],
    ))
      _meta(r),
  ];

  SealedRowMeta? row(String conversationId, int seq) {
    final r = _db.select(
      'SELECT * FROM sealed_rows WHERE conversation_id = ? AND seq = ?',
      [conversationId, seq],
    );
    return r.isEmpty ? null : _meta(r.first);
  }

  SealedRowMeta? rowByReference(String conversationId, String reference) {
    final r = _db.select(
      'SELECT * FROM sealed_rows WHERE conversation_id = ? AND reference = ? ORDER BY seq LIMIT 1',
      [conversationId, reference],
    );
    return r.isEmpty ? null : _meta(r.first);
  }

  /// The row the server gave this id.
  SealedRowMeta? rowByMessageId(String messageId) {
    final r = _db.select(
      'SELECT * FROM sealed_rows WHERE message_id = ? LIMIT 1',
      [messageId],
    );
    return r.isEmpty ? null : _meta(r.first);
  }

  /// Rows still waiting for their epoch's secret.
  List<SealedRowMeta> waiting(String conversationId) => [
    for (final r in _db.select(
      "SELECT * FROM sealed_rows WHERE conversation_id = ? AND state = 'nokey' ORDER BY seq",
      [conversationId],
    ))
      _meta(r),
  ];

  /// Runs [f] in one transaction.
  T transaction<T>(T Function() f) {
    _db.execute('BEGIN');
    try {
      final out = f();
      _db.execute('COMMIT');
      return out;
    } on Object {
      _db.execute('ROLLBACK');
      rethrow;
    }
  }

  /// Empties the file and forgets the device's private keys, keeping the
  /// file and its key (a new device starts in it).
  Future<void> clearAll() async {
    _db.execute('DELETE FROM kv; DELETE FROM sealed_rows;');
    await _secrets.delete(names.deviceKeys);
  }

  void close() {
    if (_closed) return;
    _closed = true;
    _db.close();
  }

  /// Signing out, or this device removed: closes the file, deletes it,
  /// and forgets its key and the device's private keys.
  Future<void> wipe() async {
    close();
    final p = path;
    if (p != null) {
      for (final f in [p, '$p-wal', '$p-shm', '$p-journal']) {
        if (File(f).existsSync()) File(f).deleteSync();
      }
    }
    await _secrets.delete(names.databaseKey);
    await _secrets.delete(names.deviceKeys);
  }
}
