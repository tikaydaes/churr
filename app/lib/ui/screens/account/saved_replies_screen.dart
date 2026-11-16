import 'package:flutter/material.dart';

import '../../../core/api/errors.dart';
import '../../../core/api/scheduled.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/server_state.dart';
import '../../theme/tokens.dart';
import 'common.dart';

/// The person's saved replies on one server (docs/CLIENTS.md 9.16): text
/// posted by typing `/name`, with add, edit and delete. The community's
/// commands, which admins make, are not listed. Asked fresh each time.
class SavedRepliesScreen extends StatefulWidget {
  const SavedRepliesScreen({super.key, required this.server});
  final ServerState server;

  @override
  State<SavedRepliesScreen> createState() => _SavedRepliesScreenState();
}

class _SavedRepliesScreenState extends State<SavedRepliesScreen> {
  List<SavedReply>? replies;
  String? error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final r = await widget.server.api.listMySavedReplies();
      if (mounted) {
        setState(() {
          replies = r;
          error = null;
        });
      }
    } on ApiException catch (e) {
      if (mounted) setState(() => error = e.message);
    }
  }

  Future<void> _run(Future<void> Function() action) async {
    try {
      await action();
      await _load();
    } on ApiException catch (e) {
      if (mounted) setState(() => error = e.message);
    }
  }

  Future<void> _edit(SavedReply? r) async {
    final out = await showDialog<(String, String)>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (_) => _SavedReplyDialog(reply: r),
    );
    if (out == null) return;
    final api = widget.server.api;
    await _run(
      () => r == null
          ? api.createSavedReply(out.$1, out.$2)
          : api.updateSavedReply(r.id, out.$1, out.$2),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 8, 20, 24),
      children: [
        if (error != null) ErrorLine(error!),
        Align(
          alignment: Alignment.centerLeft,
          child: FilledButton(
            key: const Key('new-saved-reply'),
            onPressed: () => _edit(null),
            child: Text(t.newSavedReply),
          ),
        ),
        SectionHead(t.savedRepliesTitle),
        if (replies == null) const LinearProgressIndicator(minHeight: 2),
        if (replies != null && replies!.isEmpty) Text(t.savedRepliesNone),
        for (final r in replies ?? const <SavedReply>[])
          ListTile(
            key: Key('saved-${r.id}'),
            contentPadding: EdgeInsets.zero,
            title: Text('/${r.name}', style: monoStyle(context, size: 14)),
            subtitle: Text(
              r.text,
              maxLines: 3,
              overflow: TextOverflow.ellipsis,
            ),
            trailing: Wrap(
              children: [
                TextButton(
                  key: Key('edit-saved-${r.id}'),
                  onPressed: () => _edit(r),
                  child: Text(t.editScheduled),
                ),
                TextButton(
                  key: Key('delete-saved-${r.id}'),
                  style: TextButton.styleFrom(foregroundColor: c.pink),
                  onPressed: () =>
                      _run(() => widget.server.api.deleteSavedReply(r.id)),
                  child: Text(t.deleteSavedReply),
                ),
              ],
            ),
          ),
      ],
    );
  }
}

class _SavedReplyDialog extends StatefulWidget {
  const _SavedReplyDialog({this.reply});
  final SavedReply? reply;

  @override
  State<_SavedReplyDialog> createState() => _SavedReplyDialogState();
}

class _SavedReplyDialogState extends State<_SavedReplyDialog> {
  late final name = TextEditingController(text: widget.reply?.name ?? '');
  late final text = TextEditingController(text: widget.reply?.text ?? '');

  @override
  void dispose() {
    name.dispose();
    text.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final r = widget.reply;
    return AlertDialog(
      title: Text(r == null ? t.newSavedReply : '/${r.name}'),
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              key: const Key('saved-name'),
              controller: name,
              autofocus: true,
              maxLength: 32,
              decoration: InputDecoration(
                labelText: t.savedReplyName,
                helperText: t.savedReplyNameHint,
                helperMaxLines: 3,
              ),
            ),
            TextField(
              key: const Key('saved-text'),
              controller: text,
              minLines: 3,
              maxLines: 8,
              decoration: InputDecoration(labelText: t.savedReplyText),
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
          key: const Key('saved-save'),
          onPressed: () {
            final n = name.text.trim().replaceFirst(RegExp('^/'), '');
            if (n.isEmpty || text.text.trim().isEmpty) return;
            Navigator.pop(context, (n, text.text));
          },
          child: Text(r == null ? t.create : t.save),
        ),
      ],
    );
  }
}
