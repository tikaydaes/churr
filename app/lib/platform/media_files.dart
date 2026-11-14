import 'dart:async';
import 'dart:io';
import 'dart:typed_data';

import 'package:desktop_drop/desktop_drop.dart';
import 'package:file_picker/file_picker.dart';
import 'package:flutter/widgets.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';
import 'package:pasteboard/pasteboard.dart';
import 'package:path_provider/path_provider.dart';

/// Files in and out of the app for the conversation screen: picking,
/// pasting and dropping attachments, saving downloads, and taking a
/// video's poster frame. Each is a thin wrapper over its package, so
/// the screens can be tested without the platform.

/// Whether this process is a `flutter test` run, where the platform
/// plugins are absent.
final bool inFlutterTest = Platform.environment.containsKey('FLUTTER_TEST');

bool _mediaKitReady = false;

/// media_kit must be initialised once before the first player.
void ensureMediaKit() {
  if (_mediaKitReady) return;
  MediaKit.ensureInitialized();
  _mediaKitReady = true;
}

/// Lets the person pick files to attach. Returns their names and bytes.
Future<List<(String, Uint8List)>> pickFiles() async {
  final picked = await FilePicker.pickFiles();
  return [for (final f in picked) (f.name, await f.xFile.readAsBytes())];
}

/// An image on the clipboard, if there is one, as a PNG.
Future<(String, Uint8List)?> pastedImage() async {
  if (inFlutterTest) return null;
  try {
    final bytes = await Pasteboard.image;
    if (bytes == null || bytes.isEmpty) return null;
    return ('pasted-${DateTime.now().millisecondsSinceEpoch}.png', bytes);
  } on Object {
    return null;
  }
}

/// Wraps [child] so files dropped on it are attached. Drawn plainly in
/// tests and where dropping is not supported.
Widget fileDropTarget({
  required Widget child,
  required void Function(List<(String, Uint8List)> files) onDrop,
  void Function(bool over)? onHover,
}) {
  if (inFlutterTest) return child;
  return DropTarget(
    onDragEntered: (_) => onHover?.call(true),
    onDragExited: (_) => onHover?.call(false),
    onDragDone: (d) async {
      onHover?.call(false);
      onDrop([for (final f in d.files) (f.name, await f.readAsBytes())]);
    },
    child: child,
  );
}

/// Asks where to save [bytes] and writes them there. Returns where, or
/// null if the person cancelled.
Future<String?> saveDownload(
  String name,
  Uint8List bytes,
  String mediaType,
) async {
  final uri = await FilePicker.saveFile(
    fileName: name,
    bytes: bytes,
    mimeType: mediaType,
  );
  if (uri == null) return null;
  if (uri.scheme == 'file') {
    final f = File(uri.toFilePath());
    // Some desktops only choose the place; the bytes are written here.
    if (!f.existsSync() || f.lengthSync() != bytes.length) await f.writeAsBytes(bytes);
    return f.path;
  }
  return uri.toString();
}

/// Takes one frame of a video as a JPEG, to send as its poster
/// (docs/CLIENTS.md 9.5: the server does not make one). Null when the
/// video cannot be read here; the message then goes without a poster.
Future<Uint8List?> takePosterFrame(String name, Uint8List bytes) async {
  if (inFlutterTest) return null;
  ensureMediaKit();
  final dir = await getTemporaryDirectory();
  final file = File(
    '${dir.path}/churr-poster-${DateTime.now().microsecondsSinceEpoch}-$name',
  );
  await file.writeAsBytes(bytes);
  final player = Player(configuration: const PlayerConfiguration(muted: true));
  try {
    // A video output is needed for a frame to exist.
    final controller = VideoController(player);
    await player.open(Media(file.uri.toString()), play: false);
    await controller.waitUntilFirstFrameRendered.timeout(
      const Duration(seconds: 10),
    );
    return await player.screenshot(format: 'image/jpeg');
  } on Object {
    return null;
  } finally {
    await player.dispose();
    try {
      await file.delete();
    } on FileSystemException {
      // The temporary folder is cleared by the system.
    }
  }
}
