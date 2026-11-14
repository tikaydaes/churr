import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:path/path.dart' as p;

import 'composer_controller.dart';
import 'server_state.dart';

/// The attachments of a draft, kept with it (docs/CLIENTS.md 9.5: "a
/// draft is kept per conversation and per thread"): leaving a
/// conversation and coming back, or restarting the app, finds the chosen
/// files still there with their captions.
///
/// The files are copied under the server's files folder,
/// `drafts/<conversation>/<thread or "top">/`, with an index in that
/// server's `settings` table. A server with no folder (tests) keeps them
/// in memory for the run.
class DraftFiles {
  DraftFiles(this.server, this.conversationId, this.parentId);

  final ServerState server;
  final String conversationId;
  final String? parentId;

  // Per server, so two apps (as in tests) never share drafts.
  static final _byServer = Expando<Map<String, List<_Saved>>>();
  Map<String, List<_Saved>> get _memory => _byServer[server] ??= {};

  String get _key => 'draft_files.$conversationId.${parentId ?? 'top'}';

  Directory? get _dir {
    final base = server.filesDirectory;
    if (base == null) return null;
    return Directory(p.join(base, 'drafts', conversationId, parentId ?? 'top'));
  }

  /// Puts the saved attachments back into [into] (empty when none).
  Future<void> restore(List<Attachment> into) async {
    final dir = _dir;
    if (dir == null) {
      for (final s in _memory[_key] ?? const <_Saved>[]) {
        into.add(s.toAttachment(s.bytes!, s.poster));
      }
      return;
    }
    final raw = await server.db.setting(_key);
    if (raw == null || raw.isEmpty) return;
    try {
      for (final j in jsonDecode(raw) as List) {
        final s = _Saved.fromJson((j as Map).cast());
        final f = File(p.join(dir.path, s.file));
        if (!f.existsSync()) continue;
        final poster = s.posterFile == null
            ? null
            : File(p.join(dir.path, s.posterFile!));
        into.add(
          s.toAttachment(
            await f.readAsBytes(),
            poster != null && poster.existsSync()
                ? await poster.readAsBytes()
                : null,
          ),
        );
      }
    } on Object {
      // A damaged index: the draft goes on without its files.
    }
  }

  /// Keeps [attachments] as the draft's files (none clears them).
  Future<void> save(List<Attachment> attachments) async {
    final dir = _dir;
    if (dir == null) {
      if (attachments.isEmpty) {
        _memory.remove(_key);
      } else {
        _memory[_key] = [
          for (final a in attachments) _Saved.of(a, bytes: a.bytes),
        ];
      }
      return;
    }
    if (attachments.isEmpty) return clear();
    await dir.create(recursive: true);
    final keep = <String>{};
    final index = <Map<String, Object?>>[];
    for (final a in attachments) {
      final file = '${a.localId}-${_safe(a.name)}';
      keep.add(file);
      final f = File(p.join(dir.path, file));
      if (!f.existsSync()) await f.writeAsBytes(a.bytes, flush: true);
      String? posterFile;
      if (a.poster != null) {
        posterFile = '$file.poster';
        keep.add(posterFile);
        final pf = File(p.join(dir.path, posterFile));
        if (!pf.existsSync()) await pf.writeAsBytes(a.poster!, flush: true);
      }
      index.add(_Saved.of(a, file: file, posterFile: posterFile).toJson());
    }
    for (final e in dir.listSync()) {
      if (e is File && !keep.contains(p.basename(e.path))) e.deleteSync();
    }
    await server.db.putSetting(_key, jsonEncode(index));
  }

  /// The message was sent or the draft emptied.
  Future<void> clear() async {
    _memory.remove(_key);
    final dir = _dir;
    if (dir == null) return;
    await server.db.putSetting(_key, '');
    if (dir.existsSync()) await dir.delete(recursive: true);
  }

  static String _safe(String name) =>
      name.replaceAll(RegExp(r'[^A-Za-z0-9._-]'), '_');
}

class _Saved {
  _Saved({
    required this.name,
    required this.caption,
    this.file = '',
    this.posterFile,
    this.bytes,
    this.poster,
  });

  factory _Saved.of(
    Attachment a, {
    String file = '',
    String? posterFile,
    Uint8List? bytes,
  }) => _Saved(
    name: a.name,
    caption: a.caption,
    file: file,
    posterFile: posterFile,
    bytes: bytes,
    poster: a.poster,
  );

  factory _Saved.fromJson(Map<String, dynamic> j) => _Saved(
    name: j['name'] as String? ?? 'file',
    caption: j['caption'] as String? ?? '',
    file: j['file'] as String? ?? '',
    posterFile: j['poster'] as String?,
  );

  final String name;
  final String caption;
  final String file;
  final String? posterFile;
  final Uint8List? bytes;
  final Uint8List? poster;

  Map<String, Object?> toJson() => {
    'name': name,
    'caption': caption,
    'file': file,
    'poster': ?posterFile,
  };

  Attachment toAttachment(Uint8List bytes, Uint8List? poster) =>
      Attachment(name: name, bytes: bytes, poster: poster)..caption = caption;
}
