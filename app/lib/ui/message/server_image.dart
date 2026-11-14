import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../../state/server_state.dart';
import '../theme/tokens.dart';
import '../theme/churr_icons.dart';

/// Bytes fetched from a server, kept for the life of the process, so a
/// row scrolled away and back is not fetched twice.
final Map<String, Future<Uint8List>> _memory = {};

Future<Uint8List> _load(ServerState server, String path) =>
    _memory['${server.localId}|$path'] ??= () async {
      // The server's `files/` folder is the disk cache. Its size cap
      // (1 GB, least recently opened first) is the file screens' work;
      // thumbnails and emoji are small.
      final dir = server.filesDirectory;
      final file = dir == null
          ? null
          : File(
              '$dir/${path.hashCode.toUnsigned(32).toRadixString(16)}-'
              '${path.split('/').last}',
            );
      if (file != null && file.existsSync()) return file.readAsBytes();
      final bytes = await server.api.download(path);
      if (file != null) {
        try {
          await file.writeAsBytes(bytes);
        } on FileSystemException {
          // The cache is a convenience; the image still draws.
        }
      }
      return bytes;
    }();

/// An image at a path the server gave (`thumb_url`, an emoji's `url`,
/// an avatar). It is fetched through the server's API client, with its
/// credential, because files are served only to those who may read
/// them; a plain network image could not authenticate.
class ServerImage extends StatelessWidget {
  const ServerImage({
    super.key,
    required this.server,
    required this.path,
    this.width,
    this.height,
    this.fit = BoxFit.cover,
    this.label,
  });

  final ServerState server;
  final String path;
  final double? width;
  final double? height;
  final BoxFit fit;

  /// What a screen reader reads: an image's caption.
  final String? label;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return FutureBuilder<Uint8List>(
      future: _load(server, path),
      builder: (context, snap) {
        if (snap.hasData) {
          return Image.memory(
            snap.data!,
            width: width,
            height: height,
            fit: fit,
            semanticLabel: label,
            gaplessPlayback: true,
            errorBuilder: (_, _, _) => _placeholder(c, ChurrIcons.imageBroken),
          );
        }
        return _placeholder(c, snap.hasError ? ChurrIcons.imageBroken : null);
      },
    );
  }

  Widget _placeholder(ChurrColors c, ChurrGlyph? icon) => Container(
    width: width,
    height: height,
    color: c.raised,
    alignment: Alignment.center,
    child: icon == null ? null : Glyph(icon, color: c.dim),
  );
}
