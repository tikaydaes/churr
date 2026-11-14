import 'dart:async';

import '../../l10n/app_localizations.dart';
import '../../platform/notify_wording.dart';

import 'package:flutter/material.dart';

import '../../core/api/errors.dart';
import '../../core/api/forms.dart';
import '../../core/models/form.dart';
import '../../core/models/json.dart';
import '../../state/app_state.dart';
import '../../state/form_actions.dart';
import '../../state/server_state.dart';
import 'answer_dialog.dart';

/// Shows a bot's dialog to the one person it is for (docs/MESSAGE-FORMAT.md
/// 7, "Dialogs for bots"; EVENTS.md `interaction.responded` with
/// `response.type` `dialog`). It is not a message: nobody else sees it,
/// and it is gone once answered or dismissed.
///
/// Every connection of the person receives the frame; the dialog is shown
/// on the device where they acted: one that started the interaction, or
/// failing that one showing the conversation it came from.
class BotDialogHost extends StatefulWidget {
  const BotDialogHost({super.key, required this.app, required this.child});
  final AppState app;
  final Widget child;

  @override
  State<BotDialogHost> createState() => _BotDialogHostState();
}

class _BotDialogHostState extends State<BotDialogHost> {
  final _subs = <String, StreamSubscription<Json>>{};
  final _shown = <String>{};

  @override
  void initState() {
    super.initState();
    widget.app.addListener(_listen);
    _listen();
  }

  void _listen() {
    final ids = {for (final s in widget.app.servers) s.localId};
    for (final id in _subs.keys.toList()) {
      if (!ids.contains(id)) _subs.remove(id)?.cancel();
    }
    for (final s in widget.app.servers) {
      _subs[s.localId] ??= s.sync.interactions.listen((e) => _onFrame(s, e));
    }
  }

  void _onFrame(ServerState server, Json e) {
    final response = object(e['response']);
    final dialog = object(response?['dialog']);
    if (response?['type'] != 'dialog' || dialog == null) return;
    final interaction = e['interaction_id'] as String?;
    final conversationId =
        (e['conversation_id'] ?? dialog['conversation_id']) as String?;
    final open = widget.app.open;
    final here =
        (interaction != null &&
            FormActions.startedHere.contains(interaction)) ||
        (open != null &&
            open.serverId == server.localId &&
            open.conversationId == conversationId);
    final id = dialog['id'] as String? ?? '';
    if (!here || id.isEmpty || !_shown.add(id) || !mounted) return;
    showBotDialog(context, server, dialog);
  }

  @override
  void dispose() {
    widget.app.removeListener(_listen);
    for (final s in _subs.values) {
      s.cancel();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.child;
}

/// Shows one bot dialog. Submitting sends `submitDialog`; closing it any
/// other way sends `dismissDialog`.
Future<void> showBotDialog(
  BuildContext context,
  ServerState server,
  Json dialog,
) async {
  final id = dialog['id'] as String? ?? '';
  final bot = server.accounts[dialog['bot_id']]?.name;
  final fields = listOf(dialog['fields'], FormFieldDef.fromJson);
  if (!fields.every((f) => knownFieldTypes.contains(f.type))) {
    // A field this build cannot draw: the dialog cannot be answered here.
    await server.api.dismissDialog(id).catchError((_) {});
    if (context.mounted) {
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(
        SnackBar(
          content: Text(
            L10n.of(context).botTooOld(bot ?? L10n.of(context).aBot),
          ),
        ),
      );
    }
    return;
  }
  final sent = await showAnswerDialog(
    context,
    server: server,
    title: dialog['title'] as String? ?? bot ?? L10n.of(context).aBotAsks,
    note: bot == null
        ? L10n.of(context).onlyYouSee
        : L10n.of(context).fromBotOnlyYou(bot),
    fields: fields,
    buttons: listOf(dialog['buttons'], FormButtonDef.fromJson),
    onSubmit: (answers, button) async {
      try {
        final interaction = await server.api.submitDialog(
          id,
          answers,
          buttonId: button,
        );
        if (interaction != null) FormActions.startedHere.add(interaction);
        return null;
      } on ApiException catch (e) {
        return e.code == 'unavailable'
            ? notifyL10n().botNotConnected
            : e.message;
      }
    },
  );
  if (sent != true) {
    try {
      await server.api.dismissDialog(id);
    } on Exception {
      // The bot is told nothing either way.
    }
  }
}
