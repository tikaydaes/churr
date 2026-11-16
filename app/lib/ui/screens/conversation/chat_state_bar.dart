import 'package:flutter/material.dart';

import '../../../core/api/errors.dart';
import '../../../core/models/conversation.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/app_state.dart';
import '../../../state/chat_requests.dart';
import '../../../state/server_state.dart';
import '../../scopes.dart';
import '../../theme/churr_icons.dart';
import '../../theme/tokens.dart';
import 'message_menu.dart' show confirm;

/// What stands in for the composer of a direct or sealed chat that waits
/// for its request, was declined, or was closed (rulings R78, R79;
/// docs/CLIENTS.md 9.28). It never says who closed a chat.
class ChatStateBar extends StatefulWidget {
  const ChatStateBar({
    super.key,
    required this.server,
    required this.conversation,
  });
  final ServerState server;
  final Conversation conversation;

  @override
  State<ChatStateBar> createState() => _ChatStateBarState();
}

class _ChatStateBarState extends State<ChatStateBar> {
  bool busy = false;
  String? error;

  Future<void> _run(Future<void> Function() fn) async {
    setState(() {
      busy = true;
      error = null;
    });
    try {
      await fn();
    } on ApiException catch (e) {
      if (mounted) setState(() => error = e.message);
    } on Object catch (e) {
      if (mounted) setState(() => error = '$e');
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final conv = widget.conversation;
    final actions = ChatRequestActions(widget.server);
    final other =
        widget.server.accounts[actions.other(conv) ?? '']?.name ?? '…';
    final app = AppScope.of(context);
    late final String text;
    Widget? action;
    switch (conv.chatState) {
      case 'waiting':
        text = t.chatWaiting(other);
        action = OutlinedButton(
          key: const Key('withdraw-request'),
          onPressed: busy
              ? null
              : () => _run(() => withdrawChat(context, widget.server, conv)),
          child: Text(t.withdrawRequest),
        );
      case 'declined':
        text = t.chatDeclined(other);
      default:
        text = t.chatClosedNote;
        action = FilledButton(
          key: const Key('ask-again'),
          onPressed: busy
              ? null
              : () => _run(() async {
                  final next = await actions.askAgain(conv);
                  if (next != null) {
                    app.openConversation(
                      OpenTarget(widget.server.localId, next.id),
                    );
                  }
                }),
          child: Text(t.askToChatAgain),
        );
    }
    return Semantics(
      liveRegion: true,
      child: Container(
        key: const Key('chat-state-bar'),
        color: c.panel,
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          mainAxisSize: MainAxisSize.min,
          children: [
            Row(
              children: [
                Glyph(
                  conv.chatState == 'waiting'
                      ? ChurrIcons.clock
                      : ChurrIcons.locked,
                  size: 16,
                  color: c.dim,
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(text, style: TextStyle(color: c.ink)),
                ),
                if (action != null) ...[const SizedBox(width: 10), action],
              ],
            ),
            if (error != null)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Text(error!, style: TextStyle(color: c.pink)),
              ),
          ],
        ),
      ),
    );
  }
}

/// Closes a direct or sealed chat after asking; a chat whose request still
/// waits is withdrawn instead. True when it was done.
Future<bool> closeChat(
  BuildContext context,
  ServerState server,
  Conversation conv,
) async {
  if (conv.chatState == 'waiting') return withdrawChat(context, server, conv);
  final t = L10n.of(context);
  final actions = ChatRequestActions(server);
  final other = server.accounts[actions.other(conv) ?? '']?.name ?? '…';
  final ok = await confirm(
    context,
    t.closeChatTitle,
    t.closeChatBody(other),
    yes: t.closeChat,
    danger: true,
  );
  if (!ok) return false;
  await actions.close(conv.id);
  return true;
}

/// Withdraws a request that waits, after asking.
Future<bool> withdrawChat(
  BuildContext context,
  ServerState server,
  Conversation conv,
) async {
  final t = L10n.of(context);
  final actions = ChatRequestActions(server);
  final other = server.accounts[actions.other(conv) ?? '']?.name ?? '…';
  final ok = await confirm(
    context,
    t.withdrawRequestTitle,
    t.withdrawRequestBody(other),
    yes: t.withdrawRequest,
    danger: true,
  );
  if (!ok) return false;
  await actions.withdraw(conv.id);
  return true;
}
