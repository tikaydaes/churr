import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';

import '../../core/highlight/languages.dart';
import '../../core/models/message.dart';
import '../../core/themes/theme.dart';
import '../../platform/media_files.dart';
import '../../state/composer_controller.dart' show formatBytes;
import '../../state/server_state.dart';
import '../screens/themes/theme_widgets.dart';
import '../theme/shapes.dart';
import '../theme/tokens.dart';
import '../widgets/controls.dart';
import '../theme/churr_icons.dart';
import 'highlight.dart';
import 'server_image.dart';

/// A message's files, each drawn by its kind (docs/MESSAGE-FORMAT.md
/// section 5): images as thumbnails that open full size (several form a
/// gallery), video and audio as inline players, text files as their
/// first lines highlighted with "show all", anything else as a name, a
/// size and a download button. A file from another server that is not
/// ready says so: arriving, unavailable, or too large for this server.
class MessageFiles extends StatelessWidget {
  const MessageFiles({super.key, required this.server, required this.files});
  final ServerState server;
  final List<FileRef> files;

  @override
  Widget build(BuildContext context) {
    final images = [
      for (final f in files)
        if (f.kind == FileKind.image && _ready(f) && f.thumbUrl != null) f,
    ];
    return Padding(
      padding: const EdgeInsets.only(top: 6),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (images.isNotEmpty) _Gallery(server: server, images: images),
          for (final f in files)
            if (!images.contains(f))
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: !_ready(f)
                    ? _FileBox(server: server, file: f)
                    : f.name.toLowerCase().endsWith('.$churrThemeExtension')
                    // An attached theme is drawn as a theme card.
                    ? ThemeFileCard(server: server, file: f)
                    : switch (f.kind) {
                        FileKind.video => InlineVideo(server: server, file: f),
                        FileKind.audio => InlineAudio(server: server, file: f),
                        FileKind.text => _TextFile(server: server, file: f),
                        _ => _FileBox(server: server, file: f),
                      },
              ),
        ],
      ),
    );
  }
}

bool _ready(FileRef f) =>
    (f.state == null || f.state == 'ready') && f.url != null;

/// What a file that is not ready reads (MESSAGE-FORMAT.md 5).
String fileStateLabel(FileRef f) => switch (f.state) {
  'arriving' => 'Arriving…',
  'unavailable' => 'Unavailable',
  'too_large' => 'Too large for this server',
  _ => f.url == null ? 'Unavailable' : formatBytes(f.size),
};

/// Saves a file through the server, after asking where.
Future<void> downloadFile(
  BuildContext context,
  ServerState server,
  FileRef f,
) async {
  final messenger = ScaffoldMessenger.maybeOf(context);
  try {
    final bytes = await server.api.download(f.url!);
    final where = await saveDownload(f.name, bytes, f.mediaType);
    if (where != null) {
      messenger?.showSnackBar(SnackBar(content: Text('Saved ${f.name}.')));
    }
  } on Object catch (e) {
    messenger?.showSnackBar(
      SnackBar(content: Text('Could not save ${f.name}. ($e)')),
    );
  }
}

class _Gallery extends StatelessWidget {
  const _Gallery({required this.server, required this.images});
  final ServerState server;
  final List<FileRef> images;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final single = images.length == 1;
    return Wrap(
      spacing: 6,
      runSpacing: 6,
      children: [
        for (var i = 0; i < images.length; i++)
          Semantics(
            button: true,
            image: true,
            label:
                'Image: ${images[i].caption ?? images[i].name}. Opens full size.',
            excludeSemantics: true,
            child: InkWell(
              key: Key('image-${images[i].id}'),
              onTap: () => openImageViewer(context, server, images, i),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Container(
                    decoration: BoxDecoration(color: c.raised),
                    child: _thumb(images[i], single ? 360 : 200),
                  ),
                  if (images[i].caption != null &&
                      images[i].caption!.isNotEmpty)
                    ConstrainedBox(
                      constraints: BoxConstraints(maxWidth: single ? 360 : 200),
                      child: Padding(
                        padding: const EdgeInsets.only(top: 2),
                        child: Text(
                          images[i].caption!,
                          style: TextStyle(fontSize: 12, color: c.dim),
                        ),
                      ),
                    ),
                ],
              ),
            ),
          ),
      ],
    );
  }

  Widget _thumb(FileRef f, double maxWidth) {
    // The size is known before it loads, so the list does not jump.
    final w = (f.width ?? 4).toDouble(), h = (f.height ?? 3).toDouble();
    final width = w > maxWidth ? maxWidth : w;
    var height = width * h / w;
    if (height > 480) height = 480;
    return ServerImage(
      server: server,
      path: f.thumbUrl!,
      width: width,
      height: height,
      label: f.caption ?? f.name,
    );
  }
}

/// The full-size viewer: one image at a time, zoomable, with the others
/// of the message a swipe or an arrow key away, and a download button.
Future<void> openImageViewer(
  BuildContext context,
  ServerState server,
  List<FileRef> images,
  int start,
) => showDialog<void>(
  animationStyle: AnimationStyle.noAnimation,
  context: context,
  useSafeArea: false,
  builder: (_) => ImageViewer(server: server, images: images, start: start),
);

class ImageViewer extends StatefulWidget {
  const ImageViewer({
    super.key,
    required this.server,
    required this.images,
    required this.start,
  });
  final ServerState server;
  final List<FileRef> images;
  final int start;

  @override
  State<ImageViewer> createState() => _ImageViewerState();
}

class _ImageViewerState extends State<ImageViewer> {
  late final pages = PageController(initialPage: widget.start);
  late int index = widget.start;

  @override
  void dispose() {
    pages.dispose();
    super.dispose();
  }

  void _go(int to) {
    if (to < 0 || to >= widget.images.length) return;
    pages.jumpToPage(to);
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final f = widget.images[index];
    return CallbackShortcuts(
      bindings: {
        const SingleActivator(LogicalKeyboardKey.arrowLeft): () =>
            _go(index - 1),
        const SingleActivator(LogicalKeyboardKey.arrowRight): () =>
            _go(index + 1),
      },
      child: Focus(
        autofocus: true,
        child: Material(
          color: c.bg,
          child: SafeArea(
            child: Column(
              children: [
                Container(
                  decoration: BoxDecoration(
                    border: Border(
                      bottom: BorderSide(color: c.line, width: 1.5),
                    ),
                  ),
                  padding: const EdgeInsets.symmetric(
                    horizontal: 8,
                    vertical: 4,
                  ),
                  child: Row(
                    children: [
                      IconButton(
                        tooltip: 'Close',
                        icon: const Glyph(ChurrIcons.close),
                        onPressed: () => Navigator.of(context).pop(),
                      ),
                      Expanded(
                        child: Text(
                          widget.images.length > 1
                              ? '${f.name}  ·  ${index + 1} of ${widget.images.length}'
                              : f.name,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(fontWeight: FontWeight.w700),
                        ),
                      ),
                      IconButton(
                        tooltip: 'Download',
                        icon: const Glyph(ChurrIcons.download),
                        onPressed: () =>
                            downloadFile(context, widget.server, f),
                      ),
                    ],
                  ),
                ),
                Expanded(
                  child: PageView.builder(
                    controller: pages,
                    itemCount: widget.images.length,
                    onPageChanged: (i) => setState(() => index = i),
                    itemBuilder: (context, i) => InteractiveViewer(
                      maxScale: 6,
                      child: Center(
                        child: ServerImage(
                          server: widget.server,
                          path: widget.images[i].url!,
                          fit: BoxFit.contain,
                          label:
                              widget.images[i].caption ?? widget.images[i].name,
                        ),
                      ),
                    ),
                  ),
                ),
                if (f.caption != null && f.caption!.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.all(12),
                    child: Text(f.caption!),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// Name, size or state, and a download button.
class _FileBox extends StatelessWidget {
  const _FileBox({required this.server, required this.file, this.child});
  final ServerState server;
  final FileRef file;
  final Widget? child;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final f = file;
    final ready = _ready(f);
    return Container(
      constraints: const BoxConstraints(maxWidth: 480),
      decoration: BoxDecoration(color: c.panel),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(10, 4, 4, 4),
            child: Row(
              children: [
                Glyph(switch (f.kind) {
                  FileKind.image => ChurrIcons.image,
                  FileKind.video => ChurrIcons.movie,
                  FileKind.audio => ChurrIcons.audio,
                  FileKind.text => ChurrIcons.fileText,
                  _ => ChurrIcons.file,
                }, color: ready ? c.lavender : c.dim),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    f.name,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(fontWeight: FontWeight.w700),
                  ),
                ),
                const SizedBox(width: 8),
                Text(
                  fileStateLabel(f),
                  key: Key('file-state-${f.id}'),
                  style: monoStyle(context, color: ready ? c.dim : c.yellowInk),
                ),
                if (ready)
                  IconButton(
                    tooltip: 'Download ${f.name}',
                    icon: const Glyph(ChurrIcons.download, size: 20),
                    onPressed: () => downloadFile(context, server, f),
                  )
                else
                  const SizedBox(height: 48),
              ],
            ),
          ),
          ?child,
        ],
      ),
    );
  }
}

/// A text file: its first lines, highlighted when the extension names a
/// language, and "show all" for the whole file.
class _TextFile extends StatelessWidget {
  const _TextFile({required this.server, required this.file});
  final ServerState server;
  final FileRef file;

  String get _language {
    final dot = file.name.lastIndexOf('.');
    return dot < 0 ? '' : canonicalLanguage(file.name.substring(dot + 1));
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final preview = file.previewText ?? '';
    return _FileBox(
      server: server,
      file: file,
      child: preview.isEmpty
          ? null
          : Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Container(
                  color: c.raised,
                  padding: const EdgeInsets.all(10),
                  child: SingleChildScrollView(
                    scrollDirection: Axis.horizontal,
                    child: Text.rich(
                      TextSpan(children: highlightCode(preview, _language, c)),
                      style: TextStyle(
                        fontFamily: fontMono,
                        fontSize: 12,
                        height: 1.5,
                        color: c.ink,
                      ),
                    ),
                  ),
                ),
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton(
                    onPressed: () => _showAll(context),
                    child: const Text('Show all'),
                  ),
                ),
              ],
            ),
    );
  }

  Future<void> _showAll(BuildContext context) => showDialog<void>(
    animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (context) {
      final c = ChurrColors.of(context);
      return Dialog(
        child: SizedBox(
          width: 760,
          height: 600,
          child: Column(
            children: [
              ListTile(
                title: Text(file.name, overflow: TextOverflow.ellipsis),
                trailing: IconButton(
                  tooltip: 'Close',
                  icon: const Glyph(ChurrIcons.close),
                  onPressed: () => Navigator.of(context).pop(),
                ),
              ),
              Expanded(
                child: FutureBuilder<Uint8List>(
                  future: server.api.download(file.url!),
                  builder: (context, snap) {
                    if (snap.hasError) {
                      return Center(
                        child: Text('Could not load the file. (${snap.error})'),
                      );
                    }
                    if (!snap.hasData) return const Center(child: Busy());
                    final text = utf8.decode(snap.data!, allowMalformed: true);
                    return SingleChildScrollView(
                      padding: const EdgeInsets.all(12),
                      child: SelectableText.rich(
                        TextSpan(children: highlightCode(text, _language, c)),
                        style: TextStyle(
                          fontFamily: fontMono,
                          fontSize: 12,
                          height: 1.5,
                          color: c.ink,
                        ),
                      ),
                    );
                  },
                ),
              ),
            ],
          ),
        ),
      );
    },
  );
}

/// Opens a file of the server in a media_kit player, with the
/// credential files are served under.
Future<Media> _media(ServerState server, FileRef f) async {
  final token = await server.api.accessToken();
  return Media(
    server.api.base.resolve(f.url!).toString(),
    httpHeaders: {'Authorization': 'Bearer $token'},
  );
}

String _clock(Duration d) {
  final m = d.inMinutes, s = d.inSeconds % 60;
  return '$m:${s.toString().padLeft(2, '0')}';
}

/// A video: its poster frame (or a plain placeholder when it came
/// without one) with a play button; pressing it starts an inline player.
///
/// Played with media_kit, which plays MP4 (H.264/AAC) and WebM on all
/// five targets (README.md, "Packages").
class InlineVideo extends StatefulWidget {
  const InlineVideo({super.key, required this.server, required this.file});
  final ServerState server;
  final FileRef file;

  @override
  State<InlineVideo> createState() => _InlineVideoState();
}

class _InlineVideoState extends State<InlineVideo> {
  Player? player;
  VideoController? controller;
  String? error;

  Future<void> _play() async {
    try {
      ensureMediaKit();
      final p = Player();
      final v = VideoController(p);
      setState(() {
        player = p;
        controller = v;
      });
      await p.open(await _media(widget.server, widget.file));
    } on Object catch (e) {
      setState(() => error = 'This video cannot be played here. ($e)');
    }
  }

  @override
  void dispose() {
    player?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final f = widget.file;
    final w = (f.width ?? 16).toDouble(), h = (f.height ?? 9).toDouble();
    return _FileBox(
      server: widget.server,
      file: f,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxHeight: 360),
        child: AspectRatio(
          aspectRatio: w / h,
          child: controller != null
              ? Video(controller: controller!)
              : Semantics(
                  button: true,
                  label: 'Play video ${f.name}',
                  excludeSemantics: true,
                  child: InkWell(
                    key: Key('play-${f.id}'),
                    onTap: _play,
                    child: Stack(
                      fit: StackFit.expand,
                      children: [
                        if (f.posterUrl != null)
                          ServerImage(
                            server: widget.server,
                            path: f.posterUrl!,
                            fit: BoxFit.cover,
                            label: f.name,
                          )
                        else
                          ColoredBox(color: c.raised),
                        Center(
                          child: Container(
                            decoration: ShapeDecoration(
                              color: c.cyan,
                              shape: const CutCornerBorder(cut: 7),
                            ),
                            padding: const EdgeInsets.all(10),
                            child: Glyph(
                              ChurrIcons.play,
                              color: c.onCyan,
                              size: 32,
                            ),
                          ),
                        ),
                        if (f.durationSeconds != null)
                          Positioned(
                            right: 6,
                            bottom: 6,
                            child: Container(
                              color: c.bg,
                              padding: const EdgeInsets.symmetric(
                                horizontal: 4,
                              ),
                              child: Text(
                                _clock(Duration(seconds: f.durationSeconds!)),
                                style: monoStyle(context),
                              ),
                            ),
                          ),
                        if (error != null)
                          Positioned(
                            left: 6,
                            right: 6,
                            bottom: 6,
                            child: Container(
                              color: c.bg,
                              child: Text(
                                error!,
                                style: TextStyle(color: c.pink, fontSize: 12),
                              ),
                            ),
                          ),
                      ],
                    ),
                  ),
                ),
        ),
      ),
    );
  }
}

/// An audio file: play and pause, where it is, and how long it is.
class InlineAudio extends StatefulWidget {
  const InlineAudio({super.key, required this.server, required this.file});
  final ServerState server;
  final FileRef file;

  @override
  State<InlineAudio> createState() => _InlineAudioState();
}

class _InlineAudioState extends State<InlineAudio> {
  Player? player;
  bool playing = false;
  Duration position = Duration.zero;
  Duration? duration;
  String? error;
  final _subs = <StreamSubscription<Object?>>[];

  Future<void> _toggle() async {
    try {
      if (player == null) {
        ensureMediaKit();
        final p = Player();
        player = p;
        _subs
          ..add(
            p.stream.playing.listen(
              (v) => mounted ? setState(() => playing = v) : null,
            ),
          )
          ..add(
            p.stream.position.listen(
              (v) => mounted ? setState(() => position = v) : null,
            ),
          )
          ..add(
            p.stream.duration.listen(
              (v) => mounted ? setState(() => duration = v) : null,
            ),
          );
        await p.open(await _media(widget.server, widget.file));
        return;
      }
      await player!.playOrPause();
    } on Object catch (e) {
      setState(() => error = 'This audio cannot be played here. ($e)');
    }
  }

  @override
  void dispose() {
    for (final s in _subs) {
      s.cancel();
    }
    player?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final f = widget.file;
    final total =
        duration ??
        (f.durationSeconds == null
            ? null
            : Duration(seconds: f.durationSeconds!));
    final max = (total?.inMilliseconds ?? 0).toDouble();
    return _FileBox(
      server: widget.server,
      file: f,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(4, 0, 12, 6),
        child: Row(
          children: [
            IconButton(
              key: Key('play-${f.id}'),
              tooltip: playing ? 'Pause' : 'Play',
              icon: Glyph(
                playing ? ChurrIcons.pause : ChurrIcons.play,
                color: c.lavender,
              ),
              onPressed: _toggle,
            ),
            Expanded(
              child: error != null
                  ? Text(error!, style: TextStyle(color: c.pink, fontSize: 12))
                  : Slider(
                      value: max == 0
                          ? 0
                          : position.inMilliseconds.clamp(0, max).toDouble(),
                      max: max == 0 ? 1 : max,
                      label: _clock(position),
                      semanticFormatterCallback: (_) =>
                          '${_clock(position)} of ${total == null ? '?' : _clock(total)}',
                      onChanged: player == null || max == 0
                          ? null
                          : (v) =>
                                player!.seek(Duration(milliseconds: v.round())),
                    ),
            ),
            Text(
              '${_clock(position)} / ${total == null ? '–:––' : _clock(total)}',
              style: monoStyle(context),
            ),
          ],
        ),
      ),
    );
  }
}

/// An attached `.churrtheme` file, drawn as a theme card once fetched;
/// a file that is not a usable theme stays a plain file box.
class ThemeFileCard extends StatefulWidget {
  const ThemeFileCard({super.key, required this.server, required this.file});
  final ServerState server;
  final FileRef file;

  @override
  State<ThemeFileCard> createState() => _ThemeFileCardState();
}

class _ThemeFileCardState extends State<ThemeFileCard> {
  ChurrTheme? theme;
  String? problem;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final bytes = await widget.server.api.download(widget.file.url!);
      final t = decodeThemeFile(utf8.decode(bytes, allowMalformed: true));
      if (mounted) setState(() => theme = t);
    } on ThemeFileError catch (e) {
      if (mounted) setState(() => problem = e.message);
    } on Object catch (e) {
      if (mounted) setState(() => problem = '$e');
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = theme;
    if (t == null) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _FileBox(server: widget.server, file: widget.file),
          if (problem != null)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text(
                problem!,
                style: monoStyle(context, color: ChurrColors.of(context).pink),
              ),
            ),
        ],
      );
    }
    return ThemeCardView(server: widget.server, theme: t);
  }
}
