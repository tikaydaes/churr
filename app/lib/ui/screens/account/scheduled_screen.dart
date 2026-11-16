import 'package:flutter/material.dart';

import '../../../core/api/errors.dart';
import '../../../core/api/scheduled.dart';
import '../../../core/api/watches.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/channel_detail.dart';
import '../../../state/server_state.dart';
import '../../theme/churr_icons.dart';
import '../../theme/theme.dart';
import '../../theme/tokens.dart';
import '../conversation/message_menu.dart' show confirm;
import 'common.dart';

/// What waits to be sent on one server (docs/CLIENTS.md 9.16): scheduled
/// messages, with edit, reschedule, send now and cancel, and reminders,
/// with edit and cancel. Not kept in the
/// cache: asked fresh each time, as the watches are.
class ScheduledScreen extends StatefulWidget {
  const ScheduledScreen({super.key, required this.server});
  final ServerState server;

  @override
  State<ScheduledScreen> createState() => _ScheduledScreenState();
}

class _ScheduledScreenState extends State<ScheduledScreen> {
  List<ScheduledItem>? items;
  String? error;

  ServerState get server => widget.server;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final all = await server.api.listScheduledItems();
      if (mounted) {
        setState(() {
          items = [
            for (final s in all)
              if (s.kind != 'watch') s,
          ];
          error = null;
        });
      }
    } on ApiException catch (e) {
      if (mounted) setState(() => error = e.message);
    }
  }

  Future<void> _run(Future<void> Function() action, [String? said]) async {
    try {
      await action();
      if (said != null && mounted) {
        ScaffoldMessenger.maybeOf(context)
          ?..hideCurrentSnackBar()
          ..showSnackBar(SnackBar(content: Text(said)));
      }
      await _load();
    } on ApiException catch (e) {
      if (mounted) setState(() => error = e.message);
    }
  }

  Future<void> _edit(ScheduledItem s) async {
    final out = await showDialog<(String, DateTime)>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (_) => _EditScheduledDialog(item: s),
    );
    if (out == null) return;
    await _run(
      () => server.api.updateScheduled(
        s.id,
        content: {...s.content, 'text': out.$1},
        runAt: out.$2,
      ),
    );
  }

  Future<void> _cancel(ScheduledItem s) async {
    final t = L10n.of(context);
    if (!await confirm(
      context,
      t.cancelScheduledTitle,
      t.cancelScheduledBody,
      yes: t.cancelScheduled,
      danger: true,
    )) {
      return;
    }
    await _run(() => server.api.cancelScheduled(s.id), t.scheduledCancelled);
  }

  String _where(ScheduledItem s) {
    // A reminder is posted to the person's own one-person channel, so it
    // is named by what it is about rather than where it goes.
    if (s.kind == 'reminder') return L10n.of(context).reminderWhere;
    final c = server.conversation(s.conversationId);
    return c == null ? L10n.of(context).aConversation : channelTitle(server, c);
  }

  Widget _tile(ScheduledItem s) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final state = s.failedAt != null
        ? t.scheduledFailed
        : s.nextRunAt != null
        ? formatUntil(context, s.nextRunAt!)
        : t.scheduledDone;
    final text = s.text.isNotEmpty
        ? s.text
        : (s.form?['title'] as String? ?? t.scheduledNoText);
    return Padding(
      key: Key('scheduled-${s.id}'),
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Glyph(
                s.kind == 'reminder' ? ChurrIcons.notify : ChurrIcons.clock,
                size: 16,
                color: c.dim,
              ),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  s.kind == 'reminder' ? _where(s) : t.scheduledFor(_where(s)),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              Text(
                state,
                style: TextStyle(color: s.failedAt != null ? c.pink : c.dim),
              ),
            ],
          ),
          Padding(
            padding: const EdgeInsets.only(left: 24, top: 2),
            child: Text(text, maxLines: 4, overflow: TextOverflow.ellipsis),
          ),
          if (s.failure.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(left: 24),
              child: Text(s.failure, style: TextStyle(color: c.pink)),
            ),
          Wrap(
            spacing: 4,
            children: [
              if (s.kind != 'form')
                TextButton(
                  key: Key('edit-${s.id}'),
                  onPressed: () => _edit(s),
                  child: Text(t.editScheduled),
                ),
              if (s.kind == 'message')
                TextButton(
                  key: Key('send-${s.id}'),
                  onPressed: () => _run(
                    () => server.api.sendScheduledNow(s.id),
                    t.scheduledSent,
                  ),
                  child: Text(t.sendNow),
                ),
              TextButton(
                key: Key('cancel-${s.id}'),
                style: TextButton.styleFrom(foregroundColor: c.pink),
                onPressed: () => _cancel(s),
                child: Text(t.cancelScheduled),
              ),
            ],
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final messages = [
      for (final s in items ?? const <ScheduledItem>[])
        if (s.kind == 'message' || s.kind == 'form') s,
    ];
    final reminders = [
      for (final s in items ?? const <ScheduledItem>[])
        if (s.kind == 'reminder') s,
    ];
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 8, 20, 24),
      children: [
        if (error != null) ErrorLine(error!),
        SectionHead(t.scheduledMessagesHead),
        if (items == null) const LinearProgressIndicator(minHeight: 2),
        if (items != null && messages.isEmpty) Text(t.scheduledNone),
        for (final s in messages) _tile(s),
        SectionHead(t.remindersHead),
        if (items != null && reminders.isEmpty) Text(t.remindersNone),
        for (final s in reminders) _tile(s),
      ],
    );
  }
}

/// Edits the text and the time of a scheduled message or reminder.
class _EditScheduledDialog extends StatefulWidget {
  const _EditScheduledDialog({required this.item});
  final ScheduledItem item;

  @override
  State<_EditScheduledDialog> createState() => _EditScheduledDialogState();
}

class _EditScheduledDialogState extends State<_EditScheduledDialog> {
  late final text = TextEditingController(text: widget.item.text);
  late DateTime at =
      widget.item.nextRunAt?.toLocal() ??
      DateTime.now().add(const Duration(hours: 1));

  @override
  void dispose() {
    text.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    return AlertDialog(
      title: Text(t.editScheduled),
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            TextField(
              key: const Key('scheduled-text'),
              controller: text,
              autofocus: true,
              minLines: 2,
              maxLines: 6,
              decoration: InputDecoration(labelText: t.scheduledTextLabel),
            ),
            const SizedBox(height: 12),
            Row(
              children: [
                Expanded(
                  child: Text(t.scheduledWhen(formatUntil(context, at))),
                ),
                TextButton(
                  key: const Key('scheduled-reschedule'),
                  onPressed: () async {
                    final picked = await pickDateTime(context, initial: at);
                    if (picked != null) setState(() => at = picked);
                  },
                  child: Text(t.reschedule),
                ),
              ],
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: Text(t.cancel),
        ),
        FilledButton(
          key: const Key('scheduled-save'),
          style: primaryButton(c),
          onPressed: () {
            if (text.text.trim().isEmpty) return;
            Navigator.pop(context, (text.text.trim(), at));
          },
          child: Text(t.save),
        ),
      ],
    );
  }
}
