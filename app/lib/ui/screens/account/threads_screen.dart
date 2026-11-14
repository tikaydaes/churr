import 'package:flutter/material.dart';

import '../../../l10n/app_localizations.dart';
import '../../../state/sidebar_model.dart';
import '../../../state/threads_bookmarks.dart';
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import '../../theme/churr_icons.dart';
import '../../widgets/badge.dart';
import 'common.dart';
import 'notifications_screen.dart' show previewOf;

/// The Threads view (docs/CLIENTS.md 9.9): followed threads with unread
/// replies, newest reply first, with the parent's first lines, where it
/// is, and the latest replies. Reply in place, mark read, stop following,
/// or open the thread in its conversation.
class ThreadsScreen extends StatefulWidget {
  const ThreadsScreen({super.key, this.onOpened, this.serverFilter});
  final VoidCallback? onOpened;

  /// Starts filtered to one server (the rail layout's).
  final String? serverFilter;

  @override
  State<ThreadsScreen> createState() => _ThreadsScreenState();
}

class _ThreadsScreenState extends State<ThreadsScreen> {
  late final ThreadsController controller;

  @override
  void initState() {
    super.initState();
    controller = ThreadsController(AppScope.read(context))
      ..serverFilter = widget.serverFilter
      ..start();
  }

  @override
  void dispose() {
    controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final app = AppScope.of(context);
    return ListenableBuilder(
      listenable: controller,
      builder: (context, _) {
        final items = controller.items;
        return Container(
          color: c.bg,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Container(
                color: c.panel,
                padding: const EdgeInsets.symmetric(
                  horizontal: 12,
                  vertical: 4,
                ),
                child: Wrap(
                  crossAxisAlignment: WrapCrossAlignment.center,
                  spacing: 12,
                  children: [
                    if (app.servers.length > 1)
                      DropdownButton<String?>(
                        elevation: 0,
                        icon: const Glyph(ChurrIcons.chevronDown, size: 16),
                        value: controller.serverFilter,
                        items: [
                          DropdownMenuItem(
                            value: null,
                            child: Text(t.allServers),
                          ),
                          for (final s in app.servers)
                            DropdownMenuItem(
                              value: s.localId,
                              child: Text(s.name),
                            ),
                        ],
                        onChanged: controller.setServerFilter,
                      ),
                    Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        ChurrSwitch(
                          key: const Key('show-read-threads'),
                          value: controller.showRead,
                          onChanged: controller.setShowRead,
                        ),
                        const SizedBox(width: 6),
                        Text(t.showReadThreads),
                      ],
                    ),
                  ],
                ),
              ),
              Container(height: 1.5, color: c.line),
              Expanded(
                child: items.isEmpty
                    ? Center(
                        child: Text(
                          t.noThreads,
                          style: TextStyle(color: c.dim),
                        ),
                      )
                    : ListView(
                        key: const Key('threads-list'),
                        children: [
                          for (final i in items)
                            _ThreadTile(
                              key: ValueKey(i.key),
                              item: i,
                              controller: controller,
                              onOpened: widget.onOpened,
                            ),
                        ],
                      ),
              ),
            ],
          ),
        );
      },
    );
  }
}

class _ThreadTile extends StatefulWidget {
  const _ThreadTile({
    super.key,
    required this.item,
    required this.controller,
    this.onOpened,
  });
  final ThreadItem item;
  final ThreadsController controller;
  final VoidCallback? onOpened;

  @override
  State<_ThreadTile> createState() => _ThreadTileState();
}

class _ThreadTileState extends State<_ThreadTile> {
  bool replying = false;
  final text = TextEditingController();

  @override
  void dispose() {
    text.dispose();
    super.dispose();
  }

  Future<void> _send() async {
    if (text.text.trim().isEmpty) return;
    await widget.controller.reply(widget.item, text.text);
    text.clear();
    if (mounted) setState(() => replying = false);
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final i = widget.item;
    final s = i.server;
    final conv = s.conversation(i.conversationId);
    final where = conv == null
        ? ''
        : conversationName(conv, s.accounts, s.me?.id);
    final parent = i.row.parent;
    final author = parent == null
        ? ''
        : (s.accounts[parent.authorId]?.name ?? t.someone);
    return Container(
      decoration: BoxDecoration(
      ),
      padding: const EdgeInsets.fromLTRB(16, 10, 8, 10),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              if (widget.controller.app.servers.length > 1)
                Padding(
                  padding: const EdgeInsets.only(right: 8),
                  child: ServerMark(server: s),
                ),
              Expanded(
                child: Text(
                  '$where · $author',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(fontWeight: FontWeight.w800),
                ),
              ),
              if (i.unread > 0)
                CountBadge(i.unread, label: t.unreadReplies(i.unread)),
            ],
          ),
          const SizedBox(height: 4),
          Text(
            parent == null ? t.messageNotLoaded : previewOf(parent, t),
            maxLines: 3,
            overflow: TextOverflow.ellipsis,
          ),
          if (i.unread > 0)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text(
                t.unreadReplies(i.unread),
                style: monoStyle(context, size: 10, color: c.lavender),
              ),
            ),
          Wrap(
            spacing: 4,
            children: [
              TextButton(
                onPressed: () => setState(() => replying = !replying),
                child: Text(t.replyInPlace),
              ),
              if (i.unread > 0)
                TextButton(
                  onPressed: () => widget.controller.markRead(i),
                  child: Text(t.markRead),
                ),
              TextButton(
                onPressed: () => widget.controller.stopFollowing(i),
                child: Text(t.stopFollowing),
              ),
              TextButton(
                key: Key('open-thread-${i.key}'),
                onPressed: () {
                  widget.controller.open(i);
                  widget.onOpened?.call();
                },
                child: Text(t.openThread),
              ),
            ],
          ),
          if (replying)
            Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: text,
                    autofocus: true,
                    minLines: 1,
                    maxLines: 4,
                    decoration: InputDecoration(hintText: t.replyHint),
                    onSubmitted: (_) => _send(),
                  ),
                ),
                IconButton(
                  tooltip: t.send,
                  icon: const Glyph(ChurrIcons.send),
                  onPressed: _send,
                ),
              ],
            ),
        ],
      ),
    );
  }
}
