import 'package:flutter/material.dart';

import '../../core/api/forms.dart';
import '../../core/api/reminders.dart';
import '../../core/models/message.dart';
import '../../state/form_actions.dart';
import '../../state/server_state.dart';
import '../theme/tokens.dart';
import '../widgets/controls.dart';
import '../theme/churr_icons.dart';
import '../../core/commands/chance_live.dart';
import '../../core/types/catalogue.dart';
import '../../core/objects/ics.dart';
import '../screens/conversation/message_menu.dart' show messageLink;
import 'markdown_view.dart';

const _months = [
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'May',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Oct',
  'Nov',
  'Dec',
];

/// `14 Nov 09:30`, in the device's time zone.
String shortWhen(DateTime t) {
  final l = t.toLocal();
  String two(int n) => n.toString().padLeft(2, '0');
  return '${l.day} ${_months[l.month - 1]} ${two(l.hour)}:${two(l.minute)}';
}

/// Whether the reader may tick this message's checklist: a member of the
/// conversation at level `member` or above, on a message not deleted
/// (docs/MESSAGE-FORMAT.md section 4). Readers, and people reading a
/// public channel they have not joined, see the list and cannot change
/// it.
bool canTick(ServerState server, Message m) {
  final level = server.conversation(m.conversationId)?.membership?.level;
  return !m.isDeleted && level != null && level != 'reader';
}

/// The message widget's Markdown context with the checklist rules
/// applied: ticking only for those who may, and under each item who
/// ticked it and when, with the way to its history.
MarkdownContext checklistMarkdown(
  BuildContext context,
  ServerState server,
  Message m,
  MarkdownContext base,
) {
  // "Remind me" in an instant's converter (ruling R69): not in a sealed
  // chat, where nothing can be scheduled.
  final hasInstant = m.entities.any(
    (e) => e.type == 'instant' || e.type == 'event',
  );
  final sealed = server.conversation(m.conversationId)?.isSealed ?? false;
  final remind = hasInstant && !sealed
      ? (DateTime at) async {
          await server.api.remindMeAbout(m.id, at);
        }
      : null;
  // An event's calendar file names the message (ruling R73).
  final eventFile = m.entities.any((e) => e.type == 'event')
      ? (
          title: eventTitle(m.text),
          link: sealed ? '' : messageLink(server, m),
          id: m.id,
        )
      : null;
  // Its values in the text that move (inline dice) spin once when the
  // message arrived live.
  final liveObject = m.entities.any(objectSpins)
      ? (int at) => ChanceLive.spinAt(ChanceLive.objectId(m.id, at))
      : null;
  if (m.tasks.isEmpty && !m.entities.any((e) => e.type == 'task')) {
    return remind == null && eventFile == null && liveObject == null
        ? base
        : base.withRemind(
            remind ?? base.onRemindAt,
            eventFile: eventFile,
            liveObject: liveObject,
          );
  }
  return MarkdownContext(
    accounts: base.accounts,
    groups: base.groups,
    emoji: base.emoji,
    conversation: base.conversation,
    myId: base.myId,
    onOpenLink: base.onOpenLink,
    onOpenChannel: base.onOpenChannel,
    emojiImage: base.emojiImage,
    onToggleTask: canTick(server, m) ? base.onToggleTask : null,
    taskDetail: (itemId, state) =>
        _TaskDetail(server: server, message: m, itemId: itemId, state: state),
    onRemindAt: remind ?? base.onRemindAt,
    messageLink: base.messageLink,
    eventFile: eventFile ?? base.eventFile,
    liveObject: liveObject ?? base.liveObject,
  );
}

class _TaskDetail extends StatelessWidget {
  const _TaskDetail({
    required this.server,
    required this.message,
    required this.itemId,
    this.state,
  });
  final ServerState server;
  final Message message;
  final String itemId;
  final TaskState? state;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final s = state;
    final ticked = s?.checked == true;
    final who = s?.checkedById == null
        ? null
        : server.accounts[s!.checkedById]?.name ?? 'Someone';
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (ticked && who != null)
          Flexible(
            child: Text(
              s?.checkedAt == null ? who : '$who · ${shortWhen(s!.checkedAt!)}',
              style: monoStyle(context, size: 10, color: c.dim),
              overflow: TextOverflow.ellipsis,
            ),
          ),
        Semantics(
          button: true,
          label: 'History of this item',
          excludeSemantics: true,
          child: InkWell(
            key: Key('task-history-$itemId'),
            onTap: () => showChecklistHistory(context, server, message, itemId),
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 1),
              child: Glyph(ChurrIcons.history, size: 12, color: c.dim),
            ),
          ),
        ),
      ],
    );
  }
}

/// The per-item history (docs/CLIENTS.md 9.7): every tick and untick
/// with who and when. Read only.
Future<void> showChecklistHistory(
  BuildContext context,
  ServerState server,
  Message m,
  String itemId,
) {
  final wide = MediaQuery.sizeOf(context).width >= 600;
  final body = _HistoryBody(server: server, message: m, itemId: itemId);
  if (wide) {
    return showDialog<void>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (_) =>
          Dialog(child: SizedBox(width: 420, height: 420, child: body)),
    );
  }
  return showModalBottomSheet<void>(
    sheetAnimationStyle: AnimationStyle.noAnimation,
    context: context,
    isScrollControlled: true,
    builder: (_) =>
        SizedBox(height: MediaQuery.sizeOf(context).height * 0.6, child: body),
  );
}

class _HistoryBody extends StatefulWidget {
  const _HistoryBody({
    required this.server,
    required this.message,
    required this.itemId,
  });
  final ServerState server;
  final Message message;
  final String itemId;

  @override
  State<_HistoryBody> createState() => _HistoryBodyState();
}

class _HistoryBodyState extends State<_HistoryBody> {
  List<TaskChange>? changes;
  String? error;

  @override
  void initState() {
    super.initState();
    FormActions(widget.server)
        .checklistHistory(widget.message, widget.itemId)
        .then((v) {
          if (mounted) setState(() => changes = v.reversed.toList());
        })
        .catchError((Object e) {
          if (mounted) {
            setState(() => error = 'Could not load the history. ($e)');
          }
        });
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 16, 8, 8),
          child: Row(
            children: [
              const Expanded(
                child: Text(
                  'Item history',
                  style: TextStyle(fontSize: 16, fontWeight: FontWeight.w800),
                ),
              ),
              IconButton(
                tooltip: 'Close',
                icon: const Glyph(ChurrIcons.close),
                onPressed: () => Navigator.pop(context),
              ),
            ],
          ),
        ),
        Divider(height: 1.5, thickness: 1.5, color: c.line),
        Expanded(
          child: error != null
              ? Center(
                  child: Text(error!, style: TextStyle(color: c.pink)),
                )
              : changes == null
              ? const Center(child: Busy())
              : changes!.isEmpty
              ? Center(
                  child: Text(
                    'Nobody has changed this item.',
                    style: TextStyle(color: c.dim),
                  ),
                )
              : ListView(
                  children: [
                    for (final ch in changes!)
                      ListTile(
                        dense: true,
                        leading: Glyph(
                          ch.checked ? ChurrIcons.boxChecked : ChurrIcons.box,
                          color: ch.checked ? c.cyan : c.dim,
                          semanticLabel: ch.checked ? 'Ticked' : 'Unticked',
                        ),
                        title: Text(
                          '${widget.server.accounts[ch.accountId]?.name ?? 'Someone'} '
                          '${ch.checked ? 'ticked it' : 'unticked it'}',
                        ),
                        trailing: Text(
                          shortWhen(ch.at),
                          style: monoStyle(context),
                        ),
                      ),
                  ],
                ),
        ),
      ],
    );
  }
}
