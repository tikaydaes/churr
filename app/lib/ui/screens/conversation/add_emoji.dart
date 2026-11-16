import 'package:flutter/material.dart';

import '../../../core/api/errors.dart';
import '../../../core/api/me.dart';
import '../../../core/models/server.dart';
import '../../../l10n/app_localizations.dart';
import '../../../platform/device.dart';
import '../../../state/server_state.dart';

/// Whether the person may add a custom emoji on [server]
/// (docs/PERMISSIONS.md note 7): admins and the owner always, members when
/// the server's `members_may_add_emoji` is on, guests and bots never.
/// Where it is false "Add emoji" is not shown at all.
bool mayAddEmoji(ServerState server) => switch (server.me?.role) {
  'owner' || 'admin' => true,
  'member' => server.info?.features['members_may_add_emoji'] ?? false,
  _ => false,
};

/// Picks a picture; the device's file chooser unless a test gives another.
typedef ImageChooser = Future<(String, List<int>)?> Function();

/// Asks for a name and a picture and adds the emoji. Gives back the new
/// emoji, or null when the person backed out.
Future<CustomEmoji?> showAddEmoji(
  BuildContext context,
  ServerState server, {
  ImageChooser choose = pickImage,
}) => showDialog<CustomEmoji>(
  animationStyle: AnimationStyle.noAnimation,
  context: context,
  builder: (_) => _AddEmojiDialog(server: server, choose: choose),
);

class _AddEmojiDialog extends StatefulWidget {
  const _AddEmojiDialog({required this.server, required this.choose});
  final ServerState server;
  final ImageChooser choose;

  @override
  State<_AddEmojiDialog> createState() => _AddEmojiDialogState();
}

class _AddEmojiDialogState extends State<_AddEmojiDialog> {
  final name = TextEditingController();
  (String, List<int>)? picture;
  String? error;
  bool busy = false;

  @override
  void dispose() {
    name.dispose();
    super.dispose();
  }

  Future<void> _add() async {
    final n = name.text.trim().replaceAll(':', '');
    final p = picture;
    if (n.isEmpty || p == null) return;
    setState(() => busy = true);
    try {
      final s = widget.server;
      final e = await s.api.createEmoji(n, p.$1, p.$2);
      // The emoji.added event brings it too; the list is asked for now so
      // the picker has it at once.
      try {
        await s.db.replaceEmoji(await s.api.listEmoji());
      } on Exception {
        // The event fills it in.
      }
      if (mounted) Navigator.pop(context, e);
    } on ApiException catch (e) {
      if (mounted) {
        setState(() {
          error = e.message;
          busy = false;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    return AlertDialog(
      scrollable: true,
      title: Text(t.addEmojiTitle),
      content: SizedBox(
        width: 400,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            TextField(
              key: const Key('emoji-name'),
              controller: name,
              autofocus: true,
              maxLength: 32,
              decoration: InputDecoration(
                labelText: t.addEmojiName,
                helperText: t.addEmojiNameHint,
                helperMaxLines: 2,
              ),
            ),
            const SizedBox(height: 8),
            Row(
              children: [
                Expanded(child: Text(picture?.$1 ?? t.addEmojiPictureHint)),
                TextButton(
                  key: const Key('emoji-choose'),
                  onPressed: () async {
                    final p = await widget.choose();
                    if (p != null && mounted) setState(() => picture = p);
                  },
                  child: Text(t.addEmojiChoose),
                ),
              ],
            ),
            if (error != null)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(error!),
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
          key: const Key('emoji-add'),
          onPressed: busy ? null : _add,
          child: Text(t.addEmojiAdd),
        ),
      ],
    );
  }
}
