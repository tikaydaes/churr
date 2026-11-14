import 'package:flutter/material.dart';

import '../../../l10n/app_localizations.dart';
import '../../../state/sidebar_model.dart';
import '../../../state/threads_bookmarks.dart';
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../../theme/churr_icons.dart';
import 'common.dart';
import 'notifications_screen.dart' show previewOf;

/// Bookmarks (docs/CLIENTS.md 9.10): the person's bookmarked messages,
/// newest bookmark first, with their conversation. Open in place, or
/// remove the bookmark.
class BookmarksScreen extends StatefulWidget {
  const BookmarksScreen({super.key, this.onOpened});
  final VoidCallback? onOpened;

  @override
  State<BookmarksScreen> createState() => _BookmarksScreenState();
}

class _BookmarksScreenState extends State<BookmarksScreen> {
  late final BookmarksController controller;

  @override
  void initState() {
    super.initState();
    controller = BookmarksController(AppScope.read(context))..start();
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
    return ListenableBuilder(
      listenable: controller,
      builder: (context, _) {
        final items = controller.items;
        if (items.isEmpty) {
          return Container(
            color: c.bg,
            alignment: Alignment.center,
            padding: const EdgeInsets.all(32),
            child: Text(
              t.noBookmarks,
              textAlign: TextAlign.center,
              style: TextStyle(color: c.dim),
            ),
          );
        }
        return Container(
          color: c.bg,
          child: ListView(
            key: const Key('bookmarks-list'),
            children: [
              for (final b in items)
                _BookmarkTile(
                  key: ValueKey(b.key),
                  item: b,
                  controller: controller,
                  onOpened: widget.onOpened,
                ),
            ],
          ),
        );
      },
    );
  }
}

class _BookmarkTile extends StatelessWidget {
  const _BookmarkTile({
    super.key,
    required this.item,
    required this.controller,
    this.onOpened,
  });
  final BookmarkItem item;
  final BookmarksController controller;
  final VoidCallback? onOpened;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final s = item.server;
    final m = item.row.message;
    final conv = s.conversation(item.row.conversationId);
    final where = conv == null
        ? ''
        : conversationName(conv, s.accounts, s.me?.id);
    final who = m == null ? '' : (s.accounts[m.authorId]?.name ?? t.someone);
    return InkWell(
      key: Key('bookmark-${item.key}'),
      onTap: () {
        controller.open(item);
        onOpened?.call();
      },
      child: Container(
        decoration: BoxDecoration(
        ),
        padding: const EdgeInsets.fromLTRB(16, 10, 4, 10),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (controller.app.servers.length > 1)
              Padding(
                padding: const EdgeInsets.only(top: 2, right: 10),
                child: ServerMark(server: s),
              ),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    [where, who].where((x) => x.isNotEmpty).join(' · '),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(fontWeight: FontWeight.w800),
                  ),
                  Text(
                    t.bookmarkedAt(formatWhen(context, item.row.createdAt)),
                    style: monoStyle(context, size: 10),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    m == null ? t.messageNotLoaded : previewOf(m, t),
                    maxLines: 3,
                    overflow: TextOverflow.ellipsis,
                  ),
                ],
              ),
            ),
            IconButton(
              tooltip: t.removeBookmark,
              icon: const Glyph(ChurrIcons.bookmarkOn, size: 18),
              onPressed: () => controller.remove(item),
            ),
          ],
        ),
      ),
    );
  }
}
