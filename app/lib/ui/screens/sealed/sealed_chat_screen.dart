import 'dart:async';
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';

import '../../../core/commands/commands.dart';
import '../../../core/db/conversation_queries.dart';
import '../../../core/models/conversation.dart';
import '../../../core/models/message.dart';
import '../../../core/sealed_client/content_view.dart';
import '../../../core/sealed_client/session.dart';
import '../../../l10n/app_localizations.dart';
import '../../../platform/media_files.dart';
import '../../../state/composer_controller.dart';
import '../../../state/sealed_state.dart';
import '../../../state/server_state.dart';
import '../../../state/sidebar_model.dart';
import '../../layout/breakpoints.dart';
import '../../message/markdown_view.dart';
import '../../scopes.dart';
import '../../theme/churr_icons.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import '../account/common.dart';
import '../conversation/composer.dart';
import '../conversation/conversation_message.dart';
import '../conversation/message_menu.dart';
import '../themes/theme_link.dart';
import 'compare_code_screen.dart';
import 'sealed_devices_screen.dart';
import 'sealed_unlock_screen.dart';
import 'sealed_words.dart';

/// A sealed chat (docs/CLIENTS.md 9.19, docs/SEALED.md): the conversation
/// screen's message widgets and composer over the messages this device
/// decrypted, with what a sealed chat adds: a lock in the header, the
/// notices of device-list changes, "Compare codes", files encrypted and
/// decrypted on the device. No cards, forms, previews, search or bots.
///
/// It reads the sealed store (through [SealedController]) and the local
/// database; writes are sealed messages, posted by the session.
class SealedChatScreen extends StatefulWidget {
  const SealedChatScreen({
    super.key,
    required this.server,
    required this.conversationId,
    this.onBack,
  });

  final ServerState server;
  final String conversationId;
  final VoidCallback? onBack;

  @override
  State<SealedChatScreen> createState() => _SealedChatScreenState();
}

/// What the sealed composer needs of its screen.
abstract interface class SealedChatHost {
  SealedController? get ctl;
  String? get other;
}

class _SealedChatScreenState extends State<SealedChatScreen>
    implements MessageHost, SealedChatHost {
  @override
  SealedController? get ctl => SealedHub.instance.of(widget.server);
  SealedSession? get session => ctl?.session;
  late final SealedComposerController composer;
  final composerKey = GlobalKey<ComposerState>();
  final scroll = ScrollController();
  final _keys = <String, GlobalKey>{};
  ServerLimits _limits = const ServerLimits();
  List<SealedNotice> notices = const [];
  String? highlighted;
  int _readSeq = 0;

  @override
  String? get other =>
      ctl?.otherOf(widget.conversationId) ??
      conversation?.memberIds.firstWhere(
        (m) => m != server.me?.id,
        orElse: () => '',
      );

  @override
  void initState() {
    super.initState();
    composer = SealedComposerController(
      widget.server,
      widget.conversationId,
      this,
    )..start();
    unawaited(
      widget.server.db.serverLimits().then((l) {
        if (mounted) setState(() => _limits = l);
      }),
    );
    ctl?.addListener(_changed);
    _loadNotices();
  }

  void _changed() {
    _loadNotices();
    if (mounted) setState(() {});
  }

  Future<void> _loadNotices() async {
    final s = session, o = other;
    if (s == null || o == null || o.isEmpty) return;
    final n = await s.noticesFor(widget.conversationId, o);
    if (mounted) setState(() => notices = n);
  }

  @override
  void dispose() {
    ctl?.removeListener(_changed);
    composer.dispose();
    scroll.dispose();
    super.dispose();
  }

  // ---- MessageHost ----

  @override
  ServerState get server => widget.server;

  @override
  Conversation? get conversation => server.conversation(widget.conversationId);

  @override
  ServerLimits get limits => _limits;

  @override
  bool get flat => true;

  @override
  bool get inThread => false;

  @override
  ThreadState? threadState(String parentId) => null;

  @override
  bool isBookmarked(String messageId) => false;

  @override
  void reply(Message m, {int? start, int? end}) {
    composer.setQuote(m, start: start, end: end);
    composerKey.currentState?.focusBox();
  }

  @override
  void edit(Message m) {
    composer.startEdit(m);
    composerKey.currentState?.focusBox();
  }

  @override
  void openThread(String parentId) => jumpTo(parentId);

  @override
  void jumpTo(String messageId) {
    final k = _keys[messageId];
    if (k?.currentContext != null) {
      Scrollable.ensureVisible(k!.currentContext!, alignment: 0.5);
    }
    setState(() => highlighted = messageId);
    Future<void>.delayed(const Duration(seconds: 2), () {
      if (mounted && highlighted == messageId) {
        setState(() => highlighted = null);
      }
    });
  }

  @override
  void openChannel(String conversationId) {}

  // ---- drawing ----

  void _markRead() {
    final seq = session?.highestSeq(widget.conversationId);
    if (seq == null || seq <= _readSeq) return;
    if (scroll.hasClients && scroll.position.pixels > 80) return;
    _readSeq = seq;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) unawaited(server.sync.markRead(widget.conversationId, seq));
    });
  }

  MarkdownContext _markdown(Message m) {
    final app = AppScope.of(context);
    // Mentions are drawn only for the two people (SEALED.md 8.3).
    final two = {server.me?.id, other};
    return MarkdownContext(
      accounts: {
        for (final e in server.accounts.entries)
          if (two.contains(e.key)) e.key: e.value,
      },
      myId: server.me?.id,
      onOpenLink: (url) => openMessageLink(context, app, url),
      onToggleTask: m.isDeleted || ctl == null
          ? null
          : (item, checked) => _run(() => ctl!.tick(m, item, checked)),
    );
  }

  Future<void> _run(Future<void> Function() f) async {
    try {
      await f();
    } on Object catch (e) {
      if (mounted) {
        ScaffoldMessenger.maybeOf(context)
            ?.showSnackBar(SnackBar(content: Text('$e')));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final app = AppScope.of(context);
    final ctl = this.ctl;
    if (ctl == null) {
      return EmptyState(title: t.sealedChatTitle, body: t.sealedNotRunning);
    }
    return ListenableBuilder(
      listenable: Listenable.merge([ctl, server, composer]),
      builder: (context, _) {
        final conv = conversation;
        final o = other ?? '';
        final name = conv == null
            ? ''
            : conversationName(conv, server.accounts, server.me?.id);
        final s = session;
        final status = ctl.status;
        _markRead();
        return Container(
          color: c.bg,
          child: Column(
            children: [
              _SealedHeader(
                title: name,
                verified: s?.verified(o) ?? false,
                onBack: widget.onBack,
                onCompare: () => openCompareCodes(
                  context,
                  server,
                  widget.conversationId,
                  o,
                  name,
                ),
                onDevices: () => openSealedDevices(context, server),
              ),
              if (status == SealedStatus.removed)
                _Banner(
                  text: t.sealedRemovedHere,
                  action: TextButton(
                    onPressed: ctl.restart,
                    child: Text(t.sealedSetUpAgain),
                  ),
                )
              else if (status == SealedStatus.locked ||
                  status == SealedStatus.noBackup)
                _Banner(
                  key: const Key('sealed-locked'),
                  text: t.sealedLockedHere,
                  action: TextButton(
                    key: const Key('sealed-unlock'),
                    onPressed: () => openSealedUnlock(context, server),
                    child: Text(t.sealedUnlock),
                  ),
                ),
              if (s != null && s.held(o))
                _Banner(
                  text: t.sealedReset(name),
                  action: TextButton(
                    onPressed: () => _run(() => s.acknowledgeReset(o)),
                    child: Text(t.sealedAcknowledge),
                  ),
                ),
              if (s != null && s.otherHasBrowser(o))
                _Banner(text: t.sealedBrowserAmong(name), quiet: true),
              Expanded(child: _body(t, s, name)),
              if (s != null && s.troubles(widget.conversationId).waiting > 0)
                _Banner(text: t.sealedWaiting(name), quiet: true),
              if (composer.error != null) ErrorLine(composer.error!),
              if (status == SealedStatus.unlocked &&
                  conv?.raw['read_only'] != true)
                Composer(
                  key: composerKey,
                  controller: composer,
                  enterSends: app.prefs.enterSends,
                  hint: t.sealedMessageHint(name),
                ),
            ],
          ),
        );
      },
    );
  }

  Widget _body(L10n t, SealedSession? s, String name) {
    final c = ChurrColors.of(context);
    final view = s?.view(widget.conversationId) ?? const [];
    final byId = {for (final (m, _, _) in view) m.id: m};
    // Messages and notices, oldest first, by time.
    final items = <(DateTime, Widget)>[];
    for (final (m, files, outOfOrder) in view) {
      if (m.isDeleted && files.isEmpty && m.text.isEmpty) {
        // A deleted message is no longer shown (SEALED.md 8.3).
        continue;
      }
      final parent = m.parentId == null ? null : byId[m.parentId];
      final key = _keys.putIfAbsent(m.id, GlobalKey.new);
      items.add((
        m.createdAt,
        KeyedSubtree(
          key: key,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              if (parent != null)
                Padding(
                  padding: const EdgeInsets.fromLTRB(56, 6, 16, 0),
                  child: InkWell(
                    onTap: () => jumpTo(parent.id),
                    child: Text(
                      '${t.sealedReplyingTo(_nameOf(parent.authorId))}: ${_snippet(parent.text)}',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(color: c.dim, fontSize: 12.5),
                    ),
                  ),
                ),
              ConversationMessage(
                host: this,
                message: m,
                markdown: _markdown(m),
                highlighted: highlighted == m.id,
                showThreadLine: false,
              ),
              if (files.isNotEmpty && s != null)
                Padding(
                  padding: const EdgeInsets.fromLTRB(56, 0, 16, 6),
                  child: Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      for (final f in files)
                        SealedFileView(session: s, file: f),
                    ],
                  ),
                ),
              if (outOfOrder)
                Padding(
                  padding: const EdgeInsets.fromLTRB(56, 0, 16, 4),
                  child: Text(
                    t.sealedOutOfOrder,
                    style: TextStyle(color: c.yellowInk, fontSize: 12),
                  ),
                ),
            ],
          ),
        ),
      ));
    }
    for (final n in notices) {
      items.add((
        n.at,
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
          child: Row(
            children: [
              Glyph(ChurrIcons.shield, size: 14, color: c.dim),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  noticeWords(t, n, name, mine: n.account == server.me?.id),
                  style: TextStyle(color: c.dim, fontSize: 13),
                ),
              ),
            ],
          ),
        ),
      ));
    }
    final unchecked = s?.troubles(widget.conversationId).unchecked ?? 0;
    if (unchecked > 0) {
      items.add((
        DateTime.now(),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
          child: Text(
            t.sealedUnchecked(unchecked),
            style: TextStyle(color: c.pink, fontSize: 13),
          ),
        ),
      ));
    }
    items.sort((a, b) => a.$1.compareTo(b.$1));
    if (items.isEmpty) {
      return EmptyState(title: t.sealedChatTitle, body: t.sealedEmpty);
    }
    final entries = items.reversed.map((x) => x.$2).toList();
    return Align(
      alignment: Alignment.topCenter,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: maxTextColumn),
        child: ListView.builder(
          key: const Key('sealed-message-list'),
          controller: scroll,
          reverse: true,
          padding: const EdgeInsets.symmetric(vertical: 8),
          itemCount: entries.length,
          itemBuilder: (context, i) => entries[i],
        ),
      ),
    );
  }

  String _nameOf(String id) =>
      server.accounts[id]?.name ?? (id == server.me?.id ? server.me!.name : '');

  static String _snippet(String s) {
    final one = s.replaceAll('\n', ' ');
    return one.length > 80 ? '${one.substring(0, 80)}…' : one;
  }
}

class _SealedHeader extends StatelessWidget {
  const _SealedHeader({
    required this.title,
    required this.verified,
    required this.onCompare,
    required this.onDevices,
    this.onBack,
  });
  final String title;
  final bool verified;
  final VoidCallback onCompare, onDevices;
  final VoidCallback? onBack;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    return Container(
      color: c.panel,
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
      child: Row(
        children: [
          if (onBack != null)
            IconButton(
              tooltip: t.back,
              icon: const Glyph(ChurrIcons.arrowLeft),
              onPressed: onBack,
            ),
          const SizedBox(width: 8),
          Semantics(
            label: t.sealedEncrypted,
            child: Glyph(ChurrIcons.locked, size: 16, color: c.cyan),
          ),
          const SizedBox(width: 8),
          Flexible(
            child: Text(
              title,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                fontWeight: FontWeight.w800,
                fontSize: 16,
                color: c.ink,
              ),
            ),
          ),
          if (verified) ...[
            const SizedBox(width: 8),
            Glyph(ChurrIcons.check, size: 14, color: c.cyan),
            const SizedBox(width: 2),
            Text(
              t.sealedVerified,
              style: TextStyle(color: c.cyan, fontSize: 12),
            ),
          ],
          const SizedBox(width: 8),
          Flexible(
            child: Text(
              t.sealedEncrypted,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(color: c.dim, fontSize: 12),
            ),
          ),
          const Spacer(),
          PopupMenuButton<String>(
            key: const Key('sealed-menu'),
            popUpAnimationStyle: AnimationStyle.noAnimation,
            tooltip: t.sealedChatMenu,
            icon: const Glyph(ChurrIcons.moreV),
            onSelected: (v) => v == 'compare' ? onCompare() : onDevices(),
            itemBuilder: (_) => [
              PopupMenuItem(
                value: 'compare',
                child: Text(t.sealedCompareCodes),
              ),
              PopupMenuItem(
                value: 'devices',
                child: Text(t.sealedChatsAndDevices),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _Banner extends StatelessWidget {
  const _Banner({
    super.key,
    required this.text,
    this.action,
    this.quiet = false,
  });
  final String text;
  final Widget? action;
  final bool quiet;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Container(
      width: double.infinity,
      color: c.raised,
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
      child: Row(
        children: [
          Glyph(
            quiet ? ChurrIcons.info : ChurrIcons.shield,
            size: 14,
            color: c.yellowInk,
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(text, style: TextStyle(fontSize: 13, color: c.ink)),
          ),
          ?action,
        ],
      ),
    );
  }
}

// ---- the composer ----

/// The conversation's composer, sending sealed messages: text, a reply,
/// files encrypted on the device with a thumbnail made here, and edits.
/// Drafts are not written to the unencrypted cache.
class SealedComposerController extends ComposerController {
  SealedComposerController(super.server, super.conversationId, this._host);

  final SealedChatHost _host;

  @override
  Future<void> saveDraft() async {}

  @override
  Future<SendOutcome> send() async {
    final ctl = _host.ctl;
    final s = ctl?.session;
    final other = _host.other;
    if (s == null || other == null || other.isEmpty || sending) {
      return const NothingToSend();
    }
    final wire = text.wireText.trim();
    if (editing != null) {
      final m = editing!;
      if (wire.isEmpty) return const NothingToSend();
      sending = true;
      notifyListeners();
      try {
        await ctl!.edit(m, wire);
        cancelEdit();
        text.clear();
        return const Sent();
      } on Object catch (e) {
        return SendFailed('$e');
      } finally {
        sending = false;
        notifyListeners();
      }
    }
    if (wire.isEmpty && attachments.isEmpty) return const NothingToSend();
    // Built-in commands (ruling R67): /me and the text conveniences are
    // sealed like any message; the rest go to the screen, which carries
    // out those that work here and says why the others do not.
    var body = wire;
    var action = false;
    // Whitespace then / is text, never a command (ruling R74); the trim
    // above has dropped the whitespace.
    final cmd = attachments.isEmpty && literalSlash(text.wireText) == null
        ? parseCommand(wire, hidden: hiddenBuiltinNames)
        : null;
    if (cmd != null) {
      if (cmd['error'] == null && cmd['post'] is String) {
        body = cmd['post']! as String;
        action = cmd['action'] == true;
      } else {
        return RunBuiltin(cmd['name']! as String, '', parsed: cmd);
      }
    }
    sending = true;
    error = null;
    notifyListeners();
    try {
      final files = <dynamic>[];
      for (final a in attachments) {
        a.progress = 0;
        notifyListeners();
        final upload = await sealedUploadOf(
          a.name,
          a.bytes,
          caption: a.caption,
        );
        files.add(
          await s.upload(
            conversationId,
            upload,
            onProgress: (p) {
              a.progress = p;
              notifyListeners();
            },
          ),
        );
        a.progress = null;
      }
      final q = quoted;
      await s.sendText(
        conversationId,
        other,
        body,
        files: files.cast(),
        parent: q == null ? null : (q.parentId ?? q.id),
        action: action,
      );
      text.clear();
      attachments.clear();
      clearQuote();
      return const Sent();
    } on Object catch (e) {
      error = '$e';
      return SendFailed('$e');
    } finally {
      sending = false;
      notifyListeners();
    }
  }

  @override
  void dispose() {
    // Nothing typed in a sealed chat is kept in the unencrypted cache.
    text.clear();
    super.dispose();
  }
}

const _imageTypes = {
  'png': 'image/png',
  'jpg': 'image/jpeg',
  'jpeg': 'image/jpeg',
  'gif': 'image/gif',
  'webp': 'image/webp',
};
const _videoTypes = {
  'mp4': 'video/mp4',
  'm4v': 'video/mp4',
  'mov': 'video/quicktime',
  'webm': 'video/webm',
};
const _audioTypes = {
  'mp3': 'audio/mpeg',
  'm4a': 'audio/mp4',
  'ogg': 'audio/ogg',
  'wav': 'audio/wav',
};

/// A file to send, with its size and thumbnail worked out on the device:
/// an image is decoded and re-encoded at most 720 pixels on its long
/// edge, which leaves its location and camera details behind.
Future<SealedUpload> sealedUploadOf(
  String name,
  Uint8List bytes, {
  String caption = '',
}) async {
  final dot = name.lastIndexOf('.');
  final ext = dot < 0 ? '' : name.substring(dot + 1).toLowerCase();
  final image = _imageTypes[ext];
  if (image == null) {
    final video = _videoTypes[ext], audio = _audioTypes[ext];
    return SealedUpload(
      name: name,
      bytes: bytes,
      mediaType: video ?? audio ?? 'application/octet-stream',
      kind: video != null
          ? 'video'
          : audio != null
          ? 'audio'
          : 'file',
      caption: caption,
    );
  }
  final thumb = await makeThumbnail(bytes);
  return SealedUpload(
    name: name,
    bytes: bytes,
    mediaType: image,
    kind: 'image',
    caption: caption,
    width: thumb?.$2 ?? 0,
    height: thumb?.$3 ?? 0,
    thumbnail: thumb?.$1,
    thumbWidth: thumb?.$4 ?? 0,
    thumbHeight: thumb?.$5 ?? 0,
  );
}

/// The thumbnail (PNG) of an image, with the image's size and its own;
/// null if the image cannot be decoded here.
Future<(Uint8List, int, int, int, int)?> makeThumbnail(Uint8List bytes) async {
  try {
    final probe = await ui.instantiateImageCodec(bytes);
    final first = await probe.getNextFrame();
    final w = first.image.width, h = first.image.height;
    first.image.dispose();
    probe.dispose();
    final scale = (w > h ? w : h) > 720 ? 720 / (w > h ? w : h) : 1.0;
    final tw = (w * scale).round().clamp(1, 720),
        th = (h * scale).round().clamp(1, 720);
    final codec = await ui.instantiateImageCodec(
      bytes,
      targetWidth: tw,
      targetHeight: th,
    );
    final frame = await codec.getNextFrame();
    final png = await frame.image.toByteData(format: ui.ImageByteFormat.png);
    frame.image.dispose();
    codec.dispose();
    if (png == null) return null;
    return (png.buffer.asUint8List(), w, h, tw, th);
  } on Object {
    return null;
  }
}

// ---- files ----

/// One file of a sealed message: the thumbnail decrypted on the device for
/// an image, otherwise its name and size with a way to save it, which
/// fetches the blob and decrypts it with progress. A file is saved only
/// once its last chunk has checked out.
class SealedFileView extends StatefulWidget {
  const SealedFileView({super.key, required this.session, required this.file});
  final SealedSession session;
  final SealedFile file;

  @override
  State<SealedFileView> createState() => _SealedFileViewState();
}

final _thumbs = <String, Uint8List>{};

class _SealedFileViewState extends State<SealedFileView> {
  double? progress;
  String? error;
  Uint8List? thumb;

  SealedFile get f => widget.file;

  @override
  void initState() {
    super.initState();
    thumb = _thumbs[f.thumbId ?? ''];
    if (thumb == null &&
        f.drawsAsImage &&
        f.thumbId != null &&
        f.thumbKey?.length == 32) {
      unawaited(_loadThumb());
    }
  }

  Future<void> _loadThumb() async {
    try {
      final b = await widget.session.download(f.thumbId!, f.thumbKey!);
      if (_thumbs.length > 200) _thumbs.clear();
      _thumbs[f.thumbId!] = b;
      if (mounted) setState(() => thumb = b);
    } on Object {
      if (mounted) setState(() => error = 'thumb');
    }
  }

  Future<void> _save() async {
    final t = L10n.of(context);
    setState(() {
      progress = 0;
      error = null;
    });
    try {
      final bytes = await widget.session.download(
        f.id,
        f.key,
        onProgress: (p) {
          if (mounted) setState(() => progress = p);
        },
      );
      await saveDownload(f.name, bytes, f.mediaType);
    } on Object {
      if (mounted) setState(() => error = t.sealedFileFailed);
    } finally {
      if (mounted) setState(() => progress = null);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final busy = progress != null;
    return Semantics(
      button: true,
      label: '${f.name}, ${t.sealedFileDownload}',
      child: InkWell(
        onTap: busy ? null : _save,
        child: Container(
          width: thumb != null ? null : 260,
          color: c.panel,
          padding: const EdgeInsets.all(8),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              if (thumb != null)
                ConstrainedBox(
                  constraints: const BoxConstraints(
                    maxWidth: 320,
                    maxHeight: 240,
                  ),
                  child: Image.memory(thumb!, fit: BoxFit.contain),
                ),
              Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Glyph(
                    f.drawsAsImage ? ChurrIcons.image : ChurrIcons.file,
                    size: 16,
                    color: c.dim,
                  ),
                  const SizedBox(width: 6),
                  Flexible(
                    child: Text(
                      f.name,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(color: c.ink),
                    ),
                  ),
                  const SizedBox(width: 6),
                  Text(
                    _size(f.size),
                    style: TextStyle(color: c.dim, fontSize: 12),
                  ),
                  const SizedBox(width: 4),
                  Glyph(ChurrIcons.download, size: 16, color: c.lavender),
                ],
              ),
              if (f.caption.isNotEmpty)
                Text(f.caption, style: TextStyle(color: c.dim, fontSize: 12.5)),
              if (busy)
                SizedBox(
                  width: 240,
                  child: LinearProgressIndicator(value: progress, minHeight: 2),
                ),
              if (error != null && error != 'thumb')
                Text(error!, style: TextStyle(color: c.pink, fontSize: 12)),
            ],
          ),
        ),
      ),
    );
  }

  static String _size(int n) {
    if (n < 1024) return '$n B';
    if (n < 1024 * 1024) return '${(n / 1024).toStringAsFixed(0)} KB';
    return '${(n / 1024 / 1024).toStringAsFixed(1)} MB';
  }
}
