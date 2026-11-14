import 'package:flutter/material.dart';

import '../../../core/account_sync.dart';
import '../../../core/models/message.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/notifications_controller.dart';
import '../../../state/sealed_state.dart';
import '../../../state/sidebar_model.dart';
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../../theme/churr_icons.dart';
import '../../widgets/badge.dart';
import 'common.dart';
import 'conversation_settings.dart';

/// The first lines of a message, as plain text, for lists.
String previewOf(Message? m, L10n t) {
  if (m == null) return '';
  // A sealed message is decrypted on the device (docs/CLIENTS.md 9.24).
  if (m.sealed != null) return SealedHub.instance.decryptedText(m) ?? t.sealedMessage;
  final s = (m.text.isNotEmpty ? m.text : m.fallback).trim();
  return s.length > 280 ? '${s.substring(0, 280)}…' : s;
}

/// The Notifications screen (docs/CLIENTS.md 9.24): one list across every
/// signed-in server, newest first, each item with its server's mark.
class NotificationsScreen extends StatefulWidget {
  const NotificationsScreen({super.key, this.onOpened});

  /// Called after an item is opened, so a page or dialog can close.
  final VoidCallback? onOpened;

  @override
  State<NotificationsScreen> createState() => _NotificationsScreenState();
}

class _NotificationsScreenState extends State<NotificationsScreen> {
  late final NotificationsController controller;

  @override
  void initState() {
    super.initState();
    controller = NotificationsController(AppScope.read(context))..start();
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
        final unread = controller.unread;
        final earlier = controller.earlier;
        return Container(
          color: c.bg,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Container(
                color: c.panel,
                padding: const EdgeInsets.fromLTRB(12, 8, 12, 8),
                child: Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  children: [
                    if (app.servers.length > 1)
                      DropdownButton<String?>(
                        elevation: 0,
                        icon: const Glyph(ChurrIcons.chevronDown, size: 16),
                        key: const Key('notifications-server-filter'),
                        value: controller.serverFilter,
                        hint: Text(t.allServers),
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
                    SegmentedButton<NotificationKinds>(
                      key: const Key('notifications-kind-filter'),
                      showSelectedIcon: false,
                      segments: [
                        ButtonSegment(
                          value: NotificationKinds.all,
                          label: Text(t.allKinds),
                        ),
                        ButtonSegment(
                          value: NotificationKinds.mentions,
                          label: Text(t.mentionsOnly),
                        ),
                        ButtonSegment(
                          value: NotificationKinds.threads,
                          label: Text(t.threadsOnly),
                        ),
                      ],
                      selected: {controller.kinds},
                      onSelectionChanged: (s) => controller.setKinds(s.first),
                    ),
                    TextButton.icon(
                      key: const Key('mark-all-read'),
                      onPressed: unread.isEmpty ? null : controller.markAllRead,
                      icon: const Glyph(ChurrIcons.checkAll, size: 16),
                      label: Text(t.markAllRead),
                    ),
                  ],
                ),
              ),
              Container(height: 1.5, color: c.line),
              Expanded(
                child: unread.isEmpty && earlier.isEmpty
                    ? Center(
                        child: Padding(
                          padding: const EdgeInsets.all(32),
                          child: Text(
                            t.nothingNew,
                            textAlign: TextAlign.center,
                            style: TextStyle(color: c.dim),
                          ),
                        ),
                      )
                    : ListView(
                        key: const Key('notifications-list'),
                        children: [
                          for (final e in unread)
                            _Item(
                              entry: e,
                              controller: controller,
                              onOpened: widget.onOpened,
                            ),
                          if (earlier.isNotEmpty) ...[
                            Padding(
                              padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
                              child: SectionHead(t.earlier),
                            ),
                            for (final e in earlier)
                              _Item(
                                entry: e,
                                controller: controller,
                                onOpened: widget.onOpened,
                              ),
                          ],
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

String reasonText(L10n t, NotificationReason r) => switch (r) {
  NotificationReason.mentionedYou => t.reasonMentionedYou,
  NotificationReason.mentionedGroup => t.reasonMentionedGroup,
  NotificationReason.mentionedChannel => t.reasonMentionedChannel,
  NotificationReason.threadReply => t.reasonThreadReply,
  NotificationReason.newMessage => t.reasonNewMessage,
  NotificationReason.missedCall => t.reasonMissedCall,
};

class _Item extends StatelessWidget {
  const _Item({required this.entry, required this.controller, this.onOpened});
  final NotificationEntry entry;
  final NotificationsController controller;
  final VoidCallback? onOpened;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final s = entry.server;
    final conv = entry.conversation;
    final where = conv == null
        ? ''
        : conversationName(conv, s.accounts, s.me?.id);
    final m = entry.message;
    final who = m == null ? '' : (s.accounts[m.authorId]?.name ?? t.someone);
    final why = reasonText(t, entry.reason);
    final when = formatWhen(context, entry.newest.at);
    final multi = controller.app.servers.length > 1;
    return Semantics(
      button: true,
      label: t.itemLabel(who, where, why, when),
      child: InkWell(
        key: Key('notification-${entry.key}'),
        onTap: () async {
          await controller.open(entry);
          onOpened?.call();
        },
        child: Container(
          decoration: BoxDecoration(
          ),
          padding: const EdgeInsets.fromLTRB(16, 10, 4, 10),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (multi)
                Padding(
                  padding: const EdgeInsets.only(top: 2, right: 10),
                  child: ServerMark(server: s),
                ),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text.rich(
                      TextSpan(
                        children: [
                          TextSpan(
                            text: where,
                            style: const TextStyle(fontWeight: FontWeight.w800),
                          ),
                          if (entry.parentId != null)
                            TextSpan(
                              text: ' · ${t.inThread}',
                              style: TextStyle(color: c.dim),
                            ),
                        ],
                      ),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                    const SizedBox(height: 2),
                    Text(
                      '$who · $why · $when',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: monoStyle(
                        context,
                        size: 10,
                        color: entry.cleared ? c.dim : c.lavender,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      previewOf(m, t),
                      maxLines: 3,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(color: entry.cleared ? c.dim : c.ink),
                    ),
                    if (entry.count > 1)
                      Padding(
                        padding: const EdgeInsets.only(top: 4),
                        child: Text(
                          t.moreCount(entry.count - 1),
                          style: monoStyle(context, size: 10),
                        ),
                      ),
                  ],
                ),
              ),
              if (!entry.cleared && entry.count > 1) CountBadge(entry.count),
              if (!entry.cleared)
                IconButton(
                  key: Key('mark-read-${entry.key}'),
                  tooltip: t.markRead,
                  icon: const Glyph(ChurrIcons.check, size: 18),
                  onPressed: () => controller.markRead(entry),
                ),
              PopupMenuButton<String>(
  popUpAnimationStyle: AnimationStyle.noAnimation,
                tooltip: t.conversationSettings,
                icon: const Glyph(ChurrIcons.moreV, size: 18),
                onSelected: (v) async {
                  switch (v) {
                    case 'settings':
                      if (conv != null) await showConversationSettings(context, s, conv.id);
                    case 'follow':
                      await controller.setThreadState(entry, 'following');
                    case 'ignore':
                      await controller.setThreadState(entry, 'ignored');
                  }
                },
                itemBuilder: (_) => [
                  PopupMenuItem(
                    value: 'settings',
                    child: Text(t.conversationSettings),
                  ),
                  if (entry.parentId != null) ...[
                    PopupMenuItem(value: 'follow', child: Text(t.followThread)),
                    PopupMenuItem(value: 'ignore', child: Text(t.ignoreThread)),
                  ],
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}
