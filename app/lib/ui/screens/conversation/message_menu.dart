import '../../../core/conversation/large.dart';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../../core/api/errors.dart';
import '../../../core/api/reminders.dart';
import '../../../core/api/watches.dart';
import '../../../core/conversation/composer_text.dart';
import '../../../core/db/conversation_queries.dart';
import '../../../core/models/account.dart';
import '../../../core/models/conversation.dart';
import '../../../core/models/message.dart';
import '../../../core/models/personal.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/message_actions.dart';
import '../../../state/server_state.dart';
import '../../layout/breakpoints.dart';
import '../../theme/theme.dart';
import '../../theme/tokens.dart';
import '../../theme/churr_icons.dart';
import '../../widgets/avatar.dart';
import 'history_view.dart';
import 'pickers.dart';
import 'schedule_sheet.dart';

/// What the conversation screen and the thread pane do when an action
/// needs them: replying, editing in a composer, opening a thread,
/// bringing a message into view, opening a channel.
abstract interface class MessageHost {
  ServerState get server;
  Conversation? get conversation;
  ServerLimits get limits;
  bool get flat;

  /// Whether the host is the thread pane (Reply then quotes in place).
  bool get inThread;
  ThreadState? threadState(String parentId);
  bool isBookmarked(String messageId);

  void reply(Message m, {int? start, int? end});
  void edit(Message m);
  void openThread(String parentId);
  void jumpTo(String messageId);
  void openChannel(String conversationId);
}

/// The actions of docs/CLIENTS.md 9.6.
enum MsgAction {
  react('React', ChurrIcons.addReaction),
  reply('Reply', ChurrIcons.reply),
  openThread('Open thread', ChurrIcons.thread),
  copyText('Copy text', ChurrIcons.copy),
  copyLink('Copy link', ChurrIcons.link),
  bookmark('Bookmark', ChurrIcons.bookmark),
  removeBookmark('Remove bookmark', ChurrIcons.bookmarkOn),
  pin('Pin', ChurrIcons.pin),
  unpin('Unpin', ChurrIcons.pinOn),
  edit('Edit', ChurrIcons.edit),
  history('History', ChurrIcons.history),
  followThread('Follow thread', ChurrIcons.notify),
  stopFollowing('Stop following', ChurrIcons.notifyOff),
  ignoreThread('Ignore thread', ChurrIcons.eyeOff),
  moveThread('Move to a channel', ChurrIcons.moveTo),
  markUnread('Mark unread from here', ChurrIcons.chatUnread),
  remindMe('Remind me about this', ChurrIcons.clock),

  /// "Tell me when they are around": a watch for the author (ruling R76).
  watchAuthor('Tell me when they are around', ChurrIcons.notify),
  whoReacted('Who reacted', ChurrIcons.people),
  report('Report', ChurrIcons.flag),
  purge('Ask for purge', ChurrIcons.delete),
  delete('Delete', ChurrIcons.delete),

  /// A manager or moderator of a hub-owned channel deletes another
  /// instance's or person's message on every instance (LINK-PROTOCOL.md
  /// section 23a).
  deleteForEveryone('Delete for everyone', ChurrIcons.delete),

  /// A local manager who is neither deletes it on this instance only.
  deleteHere('Delete here', ChurrIcons.delete);

  const MsgAction(this.label, this.icon);
  final String label;
  final ChurrGlyph icon;
}

/// An action's words: the two hub deletes through lib/l10n.
String actionLabel(L10n t, MsgAction a) => switch (a) {
  MsgAction.deleteForEveryone => t.deleteForEveryone,
  MsgAction.deleteHere => t.deleteHere,
  MsgAction.watchAuthor => t.watchAuthor,
  _ => a.label,
};

/// Which actions apply to [m] for this reader, in menu order. A sealed
/// chat's messages have React, Reply, Copy, Edit and Delete only.
List<MsgAction> availableActions({
  required Message m,
  required Conversation? c,
  required String? myId,
  required MessageActions actions,
  required ServerLimits limits,
  required ThreadState? threadState,
  required bool bookmarked,
  required bool inThread,
  required DateTime now,
  bool hubOwned = false,
  bool hubKeepsPeace = false,
  bool authorIsPerson = false,
}) {
  if (m.isSystem) return const [];
  final sealed = c?.isSealed ?? false;
  final member = c?.membership != null;
  final mine = m.authorId == myId;
  final manager = actions.isManager(c);
  // Replying: in an announcement channel only for those who post there,
  // unless the channel lets its members reply in threads.
  final canPost =
      c != null && cannotPost(c, manager: manager, reply: true) == null;
  final parentId =
      m.parentId ??
      (m.thread != null && m.thread!.replyCount > 0 ? m.id : null);
  final out = <MsgAction>[
    if (member && !m.isDeleted) MsgAction.react,
    if (canPost) MsgAction.reply,
    if (!inThread && parentId != null) MsgAction.openThread,
    if (m.text.isNotEmpty) MsgAction.copyText,
    if (!sealed) MsgAction.copyLink,
    if (!sealed) bookmarked ? MsgAction.removeBookmark : MsgAction.bookmark,
    if (!sealed && manager && !m.isDeleted)
      m.pinned ? MsgAction.unpin : MsgAction.pin,
    if (actions.canEdit(m, c, limits, now)) MsgAction.edit,
    if (!sealed && m.hasHistory) MsgAction.history,
    if (!sealed &&
        member &&
        parentId != null &&
        threadState?.state != 'following')
      MsgAction.followThread,
    if (!sealed &&
        member &&
        parentId != null &&
        threadState?.state == 'following')
      MsgAction.stopFollowing,
    if (!sealed &&
        member &&
        parentId != null &&
        threadState?.state != 'ignored')
      MsgAction.ignoreThread,
    if (!sealed &&
        !(c?.shared ?? true) &&
        !(c?.isArchived ?? true) &&
        m.parentId == null &&
        (m.thread?.replyCount ?? 0) > 0 &&
        (mine || manager))
      MsgAction.moveThread,
    if (!sealed && member) MsgAction.markUnread,
    if (!sealed && member && !m.isDeleted) MsgAction.remindMe,
    if (!mine && authorIsPerson) MsgAction.watchAuthor,
    if (m.reactions.isNotEmpty) MsgAction.whoReacted,
    if (!sealed && !mine) MsgAction.report,
    if (!sealed && mine && (m.hasHistory || m.isDeleted)) MsgAction.purge,
    // In a channel a hub owns, deleting someone else's message is either
    // for everyone (a manager or moderator on the hub's lists) or for this
    // instance only (a local manager who is neither). One's own message is deleted as anywhere.
    if (hubOwned && !mine && !m.isDeleted && !m.isSystem)
      if (hubKeepsPeace)
        MsgAction.deleteForEveryone
      else if (actions.canDelete(m, c))
        MsgAction.deleteHere
      else
        ...[]
    else if (actions.canDelete(m, c))
      MsgAction.delete,
  ];
  if (sealed) {
    const allowed = {
      MsgAction.react,
      MsgAction.reply,
      MsgAction.copyText,
      MsgAction.edit,
      MsgAction.delete,
    };
    out.removeWhere((a) => !allowed.contains(a));
  }
  return out;
}

/// People this message of the reader's mentions (or, in a small chat,
/// messages) who are in do not disturb: "Notify anyway" is offered for
/// each (FEATURES.md 3.8).
List<Account> notifyAnywayTargets(
  Message m,
  Conversation? c,
  ServerState server,
) {
  final me = server.me?.id;
  if (m.authorId != me ||
      m.via != null ||
      m.isDeleted ||
      (c?.isSealed ?? false)) {
    return const [];
  }
  final ids = <String>{
    for (final e in m.entities)
      if (e.type == 'account' && e.accountId != null) e.accountId!,
    if (c?.isAdHoc ?? false) ...c!.memberIds,
  }..remove(me);
  return [
    for (final id in ids)
      if (server.accounts[id] case final a? when a.dnd && !a.isBot) a,
  ];
}

/// A link to a message, as the web chat writes it.
String messageLink(ServerState server, Message m) {
  final base = server.api.base;
  // A reply's link opens its thread and names the reply.
  final path = m.parentId != null
      ? '/chat/c/${m.conversationId}/t/${m.parentId}/m/${m.id}'
      : '/chat/c/${m.conversationId}/m/${m.id}';
  return base.resolve(path).toString();
}

/// The wording of "Ask for purge" (FEATURES.md 5.1, DESIGN.md): what
/// happens depends on the kind of channel.
(String title, String body, String yes) purgeWording(
  Message m,
  Conversation? c,
) {
  final what = m.isDeleted
      ? 'its content, leaving only the record that a message was here, who posted it and when'
      : 'its earlier versions, leaving the message as it now reads';
  final private = c == null || c.private || c.isAdHoc;
  final shared = c?.shared ?? false;
  final body = private
      ? 'Purging erases this message\'s history for good: $what. In a private channel this happens at once, and nobody outside the channel sees anything. It cannot be undone.'
      : 'Purging erases this message\'s history for good: $what. In a public channel the history is hidden from everyone except admins as soon as you ask. An admin then chooses to show it again, keep it hidden, or erase it. You are not told which.';
  return (
    'Purge this message\'s history?',
    shared
        ? '$body In a channel shared with other servers, the request goes to the admins of your own server, and an erasure is sent to the linked servers.'
        : body,
    private ? 'Erase' : 'Ask for it',
  );
}

/// Shows the actions: a bottom sheet on touch, a menu at [position]
/// with a pointer.
Future<void> showMessageActions(
  BuildContext context,
  MessageHost host,
  Message m, {
  Offset? position,
  int? selectionStart,
  int? selectionEnd,
}) async {
  final server = host.server;
  final actions = MessageActions(server);
  final parentId = m.parentId ?? m.id;
  final conv = host.conversation;
  // A shared channel: whether a hub owns it, and whether the reader is
  // one of its managers or moderators, from the hub's record (fetched
  // when the conversation opened, and kept current by
  // `stream.channel_changed`).
  final author = server.accounts[m.authorId];
  final list = availableActions(
    authorIsPerson:
        author != null &&
        !author.isBot &&
        author.removedAt == null &&
        !(server.me?.blockedIds.contains(author.id) ?? false),
    hubOwned: conv != null && server.hubChannels.containsKey(conv.id),
    hubKeepsPeace: conv != null && server.keepsHubPeace(conv.id),
    m: m,
    c: host.conversation,
    myId: server.me?.id,
    actions: actions,
    limits: host.limits,
    threadState: host.threadState(parentId),
    bookmarked: host.isBookmarked(m.id),
    inThread: host.inThread,
    now: DateTime.now(),
  );
  final dnd = notifyAnywayTargets(m, host.conversation, server);
  if (list.isEmpty && dnd.isEmpty) return;
  final touch = switch (Theme.of(context).platform) {
    TargetPlatform.android || TargetPlatform.iOS => true,
    _ => false,
  };
  Object? picked;
  if (touch || position == null) {
    picked = await showModalBottomSheet<Object>(
      sheetAnimationStyle: AnimationStyle.noAnimation,
      context: context,
      showDragHandle: true,
      isScrollControlled: true,
      builder: (context) => SafeArea(
        child: ConstrainedBox(
          constraints: BoxConstraints(
            maxHeight: MediaQuery.sizeOf(context).height * 0.8,
          ),
          child: ListView(
            key: const Key('message-actions'),
            shrinkWrap: true,
            children: [
              if (list.contains(MsgAction.react))
                _QuickReactions(
                  server: server,
                  onPick: (e) => Navigator.of(context).pop(e),
                ),
              for (final a in list)
                ListTile(
                  key: Key('action-${a.name}'),
                  leading: Glyph(
                    a.icon,
                    color:
                        a == MsgAction.delete ||
                            a == MsgAction.deleteForEveryone ||
                            a == MsgAction.deleteHere
                        ? ChurrColors.of(context).pink
                        : null,
                  ),
                  title: Text(
                    a == MsgAction.reply && selectionStart != null
                        ? 'Reply quoting the selection'
                        : actionLabel(L10n.of(context), a),
                  ),
                  onTap: () => Navigator.of(context).pop(a),
                ),
              for (final a in dnd)
                ListTile(
                  leading: const Glyph(ChurrIcons.notifyActive),
                  title: Text('Notify ${a.name} anyway'),
                  onTap: () => Navigator.of(context).pop(a),
                ),
            ],
          ),
        ),
      ),
    );
  } else {
    final overlay =
        Overlay.of(context).context.findRenderObject()! as RenderBox;
    picked = await showMenu<Object>(
      popUpAnimationStyle: AnimationStyle.noAnimation,
      context: context,
      position: RelativeRect.fromRect(
        position & const Size(1, 1),
        Offset.zero & overlay.size,
      ),
      items: [
        for (final a in list)
          PopupMenuItem(
            key: Key('action-${a.name}'),
            value: a,
            child: Row(
              children: [
                Glyph(a.icon, size: 18),
                const SizedBox(width: 10),
                Flexible(
                  child: Text(
                    a == MsgAction.reply && selectionStart != null
                        ? 'Reply quoting the selection'
                        : actionLabel(L10n.of(context), a),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ],
            ),
          ),
        for (final a in dnd)
          PopupMenuItem(value: a, child: Text('Notify ${a.name} anyway')),
      ],
    );
  }
  if (picked == null || !context.mounted) return;
  if (picked is String) {
    await _run(context, () => actions.react(m, picked as String));
    return;
  }
  if (picked is Account) {
    await _run(
      context,
      () => actions.notifyAnyway(m, (picked as Account).id),
      done: 'Asked. You are never told whether they were reached.',
    );
    return;
  }
  await runMessageAction(
    context,
    host,
    m,
    picked as MsgAction,
    selectionStart: selectionStart,
    selectionEnd: selectionEnd,
  );
}

Future<void> _run(
  BuildContext context,
  Future<void> Function() f, {
  String? done,
}) async {
  final messenger = ScaffoldMessenger.maybeOf(context);
  try {
    await f();
    if (done != null) messenger?.showSnackBar(SnackBar(content: Text(done)));
  } on ApiException catch (e) {
    messenger?.showSnackBar(SnackBar(content: Text(e.message)));
  } on Exception catch (e) {
    messenger?.showSnackBar(SnackBar(content: Text('$e')));
  }
}

/// Carries out one action, asking first where the design says to.
Future<void> runMessageAction(
  BuildContext context,
  MessageHost host,
  Message m,
  MsgAction a, {
  int? selectionStart,
  int? selectionEnd,
}) async {
  final server = host.server;
  final actions = MessageActions(server);
  final c = host.conversation;
  switch (a) {
    case MsgAction.react:
      final e = await pickReaction(context, server);
      if (e != null && context.mounted) {
        await _run(context, () => actions.react(m, e));
      }
    case MsgAction.reply:
      host.reply(m, start: selectionStart, end: selectionEnd);
    case MsgAction.openThread:
      host.openThread(m.parentId ?? m.id);
    case MsgAction.copyText:
      await Clipboard.setData(
        ClipboardData(text: copyAsText(m, server.conversation)),
      );
      if (context.mounted) _say(context, 'Text copied.');
    case MsgAction.copyLink:
      await Clipboard.setData(ClipboardData(text: messageLink(server, m)));
      if (context.mounted) _say(context, 'Link copied.');
    case MsgAction.bookmark || MsgAction.removeBookmark:
      await _run(
        context,
        () => actions.setBookmarked(m, a == MsgAction.bookmark),
        done: a == MsgAction.bookmark ? 'Bookmarked.' : 'Bookmark removed.',
      );
    case MsgAction.pin || MsgAction.unpin:
      await _run(
        context,
        () => actions.setPinned(m, a == MsgAction.pin),
        done: a == MsgAction.pin ? 'Pinned.' : 'Unpinned.',
      );
    case MsgAction.edit:
      host.edit(m);
    case MsgAction.history:
      await openHistory(context, server, m);
    case MsgAction.followThread ||
        MsgAction.stopFollowing ||
        MsgAction.ignoreThread:
      final parent = m.parentId == null
          ? m
          : await server.db.message(m.parentId!);
      if (parent == null || !context.mounted) return;
      final to = switch (a) {
        MsgAction.followThread => 'following',
        MsgAction.ignoreThread => 'ignored',
        _ => 'none',
      };
      await _run(
        context,
        () => actions.setThreadState(
          parent,
          to,
          readSeq: parent.thread?.lastReplySeq ?? 0,
        ),
      );
    case MsgAction.moveThread:
      final name = await askMoveThreadName(context);
      if (name == null || !context.mounted) return;
      try {
        final moved = await actions.moveThread(m, name);
        host.openChannel(moved.id);
      } on ApiException catch (e) {
        if (context.mounted) _say(context, e.message);
      }
    case MsgAction.markUnread:
      await _run(
        context,
        () => actions.markUnreadFrom(m),
        done: 'Marked unread.',
      );
    case MsgAction.remindMe:
      // Suggested times, or pick one (docs/CLIENTS.md 9.6).
      final at = await showScheduleSheet(context);
      if (at == null || !context.mounted) return;
      await _run(
        context,
        () => server.api.remindMeAbout(m.id, at),
        done: L10n.of(context).reminderSet(scheduleLabel(at)),
      );
    case MsgAction.watchAuthor:
      final t = L10n.of(context);
      final name = server.accounts[m.authorId]?.name ?? '';
      final messenger = ScaffoldMessenger.maybeOf(context);
      try {
        final around = await server.api.watchAccount(m.authorId);
        messenger?.showSnackBar(
          SnackBar(
            content: Text(around ? t.watchIsAround(name) : t.watchSet(name)),
          ),
        );
      } on ApiException catch (e) {
        messenger?.showSnackBar(SnackBar(content: Text(e.message)));
      }
    case MsgAction.whoReacted:
      await showWhoReacted(context, server, m);
    case MsgAction.report:
      final reason = await askReportReason(context, c);
      if (reason != null && context.mounted) {
        await _run(context, () => actions.report(m, reason), done: 'Reported.');
      }
    case MsgAction.purge:
      final (title, body, yes) = purgeWording(m, c);
      if (!await confirm(context, title, body, yes: yes, danger: true) ||
          !context.mounted) {
        return;
      }
      try {
        final state = await actions.purge(m);
        if (context.mounted) {
          _say(
            context,
            state == 'purged'
                ? 'The history is erased.'
                : 'Asked. The history is hidden meanwhile.',
          );
        }
      } on ApiException catch (e) {
        if (context.mounted) {
          _say(
            context,
            e.code == 'conflict'
                ? 'There is nothing to purge, or this was already decided.'
                : e.message,
          );
        }
      }
    case MsgAction.deleteForEveryone || MsgAction.deleteHere:
      final t = L10n.of(context);
      final everyone = a == MsgAction.deleteForEveryone;
      if (!await confirm(
            context,
            everyone ? t.deleteForEveryoneTitle : t.deleteHereTitle,
            everyone ? t.deleteForEveryoneBody : t.deleteHereBody,
            yes: everyone ? t.deleteForEveryone : t.deleteHere,
            danger: true,
          ) ||
          !context.mounted) {
        return;
      }
      await _run(context, () => actions.delete(m));
    case MsgAction.delete:
      final body = c?.isSealed ?? false
          ? 'It is removed for both of you.'
          : 'A marker stays saying who deleted it and when. Its content is kept as history until it is purged.';
      if (!await confirm(
            context,
            'Delete this message?',
            body,
            yes: 'Delete',
            danger: true,
          ) ||
          !context.mounted) {
        return;
      }
      await _run(context, () => actions.delete(m));
  }
}

void _say(BuildContext context, String s) => ScaffoldMessenger.maybeOf(context)
  ?..hideCurrentSnackBar()
  ..showSnackBar(SnackBar(content: Text(s)));

/// Recent emoji first, then the picker (9.6).
Future<String?> pickReaction(BuildContext context, ServerState server) async {
  final recent = await server.db.recentEmoji();
  if (!context.mounted) return null;
  return showEmojiPicker(context, server, recent: recent);
}

class _QuickReactions extends StatelessWidget {
  const _QuickReactions({required this.server, required this.onPick});
  final ServerState server;
  final void Function(String) onPick;

  @override
  Widget build(BuildContext context) => FutureBuilder<List<String>>(
    future: server.db.recentEmoji(),
    builder: (context, snap) {
      final recent = <String>{
        ...?snap.data,
        '👍',
        '❤️',
        '😂',
        '🎉',
        '👀',
        '🙏',
      }.take(8);
      return SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        padding: const EdgeInsets.symmetric(horizontal: 12),
        child: Row(
          children: [
            for (final e in recent)
              IconButton(
                tooltip: 'React with $e',
                icon: emojiGlyph(server, e, size: 26),
                onPressed: () => onPick(e),
              ),
          ],
        ),
      );
    },
  );
}

Future<bool> confirm(
  BuildContext context,
  String title,
  String body, {
  required String yes,
  bool danger = false,
}) async {
  final c = ChurrColors.of(context);
  return await showDialog<bool>(
        animationStyle: AnimationStyle.noAnimation,
        context: context,
        builder: (context) => AlertDialog(
          title: Text(title),
          content: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 420),
            child: Text(body),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.of(context).pop(false),
              child: const Text('Cancel'),
            ),
            FilledButton(
              key: const Key('confirm'),
              style: danger ? dangerButton(c) : null,
              onPressed: () => Navigator.of(context).pop(true),
              child: Text(yes),
            ),
          ],
        ),
      ) ??
      false;
}

/// Asks for the new channel's name, saying the move cannot be undone
/// (9.4).
Future<String?> askMoveThreadName(BuildContext context) {
  final name = TextEditingController();
  return showDialog<String>(
    animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (context) => AlertDialog(
      title: const Text('Move this thread to a channel'),
      content: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 420),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'The replies move to a new channel under this one, which starts with a copy of this message. '
              'A line here points to it, and this thread takes no more replies.',
            ),
            const SizedBox(height: 8),
            const Text(
              'This cannot be undone.',
              style: TextStyle(fontWeight: FontWeight.w800),
            ),
            const SizedBox(height: 12),
            TextField(
              key: const Key('move-name'),
              controller: name,
              autofocus: true,
              maxLength: 64,
              decoration: const InputDecoration(
                labelText: 'Name of the new channel',
              ),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        FilledButton(
          key: const Key('confirm'),
          onPressed: () {
            final n = name.text.trim().replaceAll(RegExp(r'\s+'), ' ');
            if (n.isNotEmpty) Navigator.of(context).pop(n);
          },
          child: const Text('Move it'),
        ),
      ],
    ),
  );
}

/// Asks why, and says who the report goes to.
/// What the report dialog says about who is shown a report of a message
/// in [c] (docs/PERMISSIONS.md section 8.4). A chat has no moderators, so
/// its reports go to the admins, who are shown that one message.
String reportGoesTo(Conversation? c) {
  const shown =
      'They are shown this message (its text, author, time and file names) and nothing else of it.';
  if (c != null && c.isAdHoc) {
    return 'The report goes to this instance\'s admins, since a chat has no moderators. $shown They learn that you share this chat. The author is not told.';
  }
  if (c != null && c.private) {
    return 'The report goes to this channel\'s moderators and managers. If it is about one of them, it goes to the instance\'s admins too. $shown The author is not told.';
  }
  return 'The report goes to this channel\'s moderators and managers, and to the admins. The author is not told.';
}

Future<String?> askReportReason(BuildContext context, Conversation? c) {
  final reason = TextEditingController();
  return showDialog<String>(
    animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (context) => AlertDialog(
      title: const Text('Report this message'),
      content: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 420),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(reportGoesTo(c)),
            const SizedBox(height: 12),
            TextField(
              key: const Key('report-reason'),
              controller: reason,
              autofocus: true,
              maxLines: 4,
              maxLength: 500,
              decoration: const InputDecoration(labelText: 'Why'),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        FilledButton(
          key: const Key('confirm'),
          onPressed: () => Navigator.of(context).pop(reason.text.trim()),
          child: const Text('Report'),
        ),
      ],
    ),
  );
}

/// Every reaction with the people behind it (`listReactions`: the
/// message itself names at most ten per emoji).
Future<void> showWhoReacted(
  BuildContext context,
  ServerState server,
  Message m,
) {
  final compact =
      WidthClass.of(MediaQuery.sizeOf(context).width) == WidthClass.compact;
  final body = FutureBuilder(
    future: MessageActions(server).reactions(m),
    builder: (context, snap) {
      final list = snap.data ?? m.reactions;
      return ListView(
        key: const Key('who-reacted'),
        shrinkWrap: true,
        padding: const EdgeInsets.all(12),
        children: [
          for (final r in list) ...[
            Padding(
              padding: const EdgeInsets.only(top: 8, bottom: 4),
              child: Row(
                children: [
                  emojiGlyph(server, r.emoji, size: 22),
                  const SizedBox(width: 8),
                  Text('${r.count}', style: monoStyle(context)),
                ],
              ),
            ),
            for (final id in r.accountIds)
              ListTile(
                dense: true,
                leading: Avatar(
                  account: server.accounts[id],
                  server: server,
                  size: 24,
                ),
                title: Text(server.accounts[id]?.name ?? 'Someone'),
              ),
            if (r.count > r.accountIds.length &&
                snap.connectionState != ConnectionState.waiting)
              Padding(
                padding: const EdgeInsets.only(left: 16),
                child: Text(
                  'and ${r.count - r.accountIds.length} more',
                  style: monoStyle(context),
                ),
              ),
          ],
        ],
      );
    },
  );
  if (compact) {
    return showModalBottomSheet<void>(
      sheetAnimationStyle: AnimationStyle.noAnimation,
      context: context,
      showDragHandle: true,
      builder: (_) => SafeArea(child: body),
    );
  }
  return showDialog<void>(
    animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (context) =>
        Dialog(child: SizedBox(width: 360, height: 420, child: body)),
  );
}
