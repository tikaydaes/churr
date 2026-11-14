import 'dart:io';

/// The file cache's cap (docs/CLIENTS.md section 6): each server's
/// `files/` folder together stays under the cap, least recently opened
/// first out. "Opened" is the file's access time where the file system
/// keeps one, else its modification time.

/// Total bytes in [dirs].
Future<int> fileCacheSize(Iterable<String> dirs) async {
  var total = 0;
  for (final f in await _files(dirs)) {
    total += (await f.stat()).size;
  }
  return total;
}

/// Deletes the least recently opened files in [dirs] until they hold no
/// more than [capBytes]. Returns the bytes freed.
Future<int> trimFileCache(Iterable<String> dirs, int capBytes) async {
  final entries = <(File, FileStat)>[];
  var total = 0;
  for (final f in await _files(dirs)) {
    final st = await f.stat();
    entries.add((f, st));
    total += st.size;
  }
  if (total <= capBytes) return 0;
  DateTime opened(FileStat s) =>
      s.accessed.isAfter(s.modified) ? s.accessed : s.modified;
  entries.sort((a, b) => opened(a.$2).compareTo(opened(b.$2)));
  var freed = 0;
  for (final (f, st) in entries) {
    if (total - freed <= capBytes) break;
    try {
      await f.delete();
      freed += st.size;
    } on FileSystemException {
      // In use; the next trim tries again.
    }
  }
  return freed;
}

/// "Clear cache": empties [dirs].
Future<void> clearFileCache(Iterable<String> dirs) async {
  for (final f in await _files(dirs)) {
    try {
      await f.delete();
    } on FileSystemException {
      // In use.
    }
  }
}

Future<List<File>> _files(Iterable<String> dirs) async {
  final out = <File>[];
  for (final d in dirs) {
    final dir = Directory(d);
    if (!await dir.exists()) continue;
    await for (final e in dir.list(recursive: true, followLinks: false)) {
      if (e is File) out.add(e);
    }
  }
  return out;
}
