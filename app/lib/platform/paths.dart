import 'dart:io';

import 'package:drift/native.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

import '../core/db/app_db.dart';
import '../core/db/cache_db.dart';
import '../state/app_state.dart';

/// Where the app keeps things on disk (docs/CLIENTS.md section 6):
///
///     <support directory>/
///       app.db                 app preferences, the list of servers
///       servers/<local id>/
///         cache.db             that server's local copy
///         files/               downloaded files and thumbnails
///
/// `sealed.db` belongs beside `cache.db` and is made by the sealed-chat
/// code.
class DiskStorage implements ServerStorage {
  DiskStorage(this.root);

  /// The app's support directory.
  final Directory root;

  static Future<DiskStorage> open() async =>
      DiskStorage(await getApplicationSupportDirectory());

  /// Opens `app.db`.
  AppDb openAppDb() {
    root.createSync(recursive: true);
    return AppDb(NativeDatabase(File(p.join(root.path, 'app.db'))));
  }

  Directory _server(String localId) =>
      Directory(p.join(root.path, 'servers', localId));

  @override
  Future<CacheDb> openCache(String localId) async {
    final dir = _server(localId)..createSync(recursive: true);
    return CacheDb(NativeDatabase(File(p.join(dir.path, 'cache.db'))));
  }

  @override
  Future<String?> filesDirectory(String localId) async {
    final dir = Directory(p.join(_server(localId).path, 'files'))
      ..createSync(recursive: true);
    return dir.path;
  }

  @override
  Future<void> delete(String localId) async {
    final dir = _server(localId);
    if (dir.existsSync()) await dir.delete(recursive: true);
  }
}
