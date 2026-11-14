import 'dart:async';

import 'package:drift/drift.dart';

/// The base of the app's two kinds of database (`cache.db` per server,
/// `app.db` for the app): SQLite through drift, with hand-written SQL.
///
/// drift is used for what it gives without generated code: opening and
/// migrating by `user_version`, transactions, and the table-update
/// notifications that watched queries are built on. Tables and queries
/// are plain SQL in the subclasses, so building the app needs no code
/// generation step. Every write names the tables it touched and every
/// watched query names the tables it reads; a query runs again when one
/// of its tables is touched, after the transaction that touched it
/// commits.
abstract class SqlDb extends GeneratedDatabase {
  SqlDb(super.executor);

  /// The `CREATE` statements of the current schema, run on a new file.
  List<String> get schema;

  /// Statements that take an older file to the current schema, keyed by
  /// the version they upgrade from. Empty while there is only version 1.
  Map<int, List<String>> get upgrades => const {};

  @override
  Iterable<TableInfo<Table, dynamic>> get allTables => const [];

  @override
  MigrationStrategy get migration => MigrationStrategy(
    onCreate: (_) async {
      for (final s in schema) {
        await customStatement(s);
      }
    },
    onUpgrade: (_, from, to) async {
      for (var v = from; v < to; v++) {
        for (final s in upgrades[v] ?? const <String>[]) {
          await customStatement(s);
        }
      }
    },
  );

  /// Runs a query and returns its rows as maps.
  Future<List<Map<String, Object?>>> rows(
    String sql, [
    List<Object?> args = const [],
  ]) async {
    final found = await customSelect(
      sql,
      variables: [for (final a in args) Variable<Object>(a)],
    ).get();
    return [for (final r in found) r.data];
  }

  /// Runs a statement that changes [tables], and tells watchers so.
  Future<void> run(String sql, List<Object?> args, Set<String> tables) async {
    await customStatement(sql, args);
    touch(tables);
  }

  /// Tells watched queries that [tables] changed.
  void touch(Set<String> tables) =>
      notifyUpdates({for (final t in tables) TableUpdate(t)});

  /// A stream of [query]'s result: once at once, and again whenever one
  /// of [tables] is touched. A burst of touches while the query is
  /// running costs one more run, not one each.
  Stream<T> watch<T>(Set<String> tables, Future<T> Function() query) {
    late final StreamController<T> out;
    StreamSubscription<Set<TableUpdate>>? sub;
    var running = false, dirty = false;

    Future<void> runQuery() async {
      if (running) {
        dirty = true;
        return;
      }
      running = true;
      do {
        dirty = false;
        try {
          final v = await query();
          if (!out.isClosed) out.add(v);
        } on Object catch (e, s) {
          if (!out.isClosed) out.addError(e, s);
        }
      } while (dirty && !out.isClosed);
      running = false;
    }

    out = StreamController<T>(
      onListen: () {
        sub = tableUpdates().listen((updates) {
          if (updates.any((u) => tables.contains(u.table))) runQuery();
        });
        runQuery();
      },
      onCancel: () async {
        await sub?.cancel();
        await out.close();
      },
    );
    return out.stream;
  }
}
