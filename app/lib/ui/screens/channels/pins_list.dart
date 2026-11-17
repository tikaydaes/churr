import 'package:flutter/material.dart';

import '../../../core/api/channels.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/app_state.dart';
import '../../../state/conversation_intents.dart';
import '../../../state/server_state.dart';
import '../../message/checklist.dart' show shortWhen;
import '../../message/markdown_view.dart';
import '../../message/message_tile.dart';
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import 'common.dart';

/// Opens the pinned messages of a conversation from its header
/// (docs/CLIENTS.md 9.15): the list, each opening the message in place.
Future<void> openPins(
  BuildContext context,
  ServerState server,
  String conversationId,
) => showScreen<void>(
  context,
  title: L10n.of(context).tabPins,
  child: PinsScreen(server: server, conversationId: conversationId),
);

/// The pins of one conversation, asked for when opened.
class PinsScreen extends StatefulWidget {
  const PinsScreen({
    super.key,
    required this.server,
    required this.conversationId,
  });
  final ServerState server;
  final String conversationId;

  @override
  State<PinsScreen> createState() => _PinsScreenState();
}

class _PinsScreenState extends State<PinsScreen> {
  List<Pin>? pins;

  @override
  void initState() {
    super.initState();
    widget.server.api
        .listPins(widget.conversationId)
        .then(
          (v) {
            if (mounted) setState(() => pins = v);
          },
          onError: (Object _) {
            if (mounted) setState(() => pins = const []);
          },
        );
  }

  @override
  Widget build(BuildContext context) => PinsList(
    server: widget.server,
    conversationId: widget.conversationId,
    pins: pins,
  );
}

/// The pinned messages: who wrote each, its text, who pinned it and when.
/// Tapping one opens it in the conversation. Null [pins] is loading.
class PinsList extends StatelessWidget {
  const PinsList({
    super.key,
    required this.server,
    required this.conversationId,
    required this.pins,
  });
  final ServerState server;
  final String conversationId;
  final List<Pin>? pins;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final pins = this.pins;
    final s = server;
    if (pins == null) return const Center(child: Busy());
    if (pins.isEmpty) {
      return Center(
        child: Text(
          L10n.of(context).nothingPinned,
          style: TextStyle(color: c.dim),
        ),
      );
    }
    return ListView.separated(
      itemCount: pins.length,
      separatorBuilder: (_, _) => const SizedBox(height: 2),
      itemBuilder: (context, i) {
        final p = pins[i];
        final m = p.message;
        final text = m == null ? '' : (m.text.isNotEmpty ? m.text : m.fallback);
        return ListTile(
          key: Key('pin-${p.messageId}'),
          title: Text(
            m == null
                ? L10n.of(context).aMessage
                : s.accounts[m.authorId]?.name ?? L10n.of(context).someone,
          ),
          subtitle: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(text, maxLines: 3, overflow: TextOverflow.ellipsis),
              Text(
                p.pinnedAt == null
                    ? L10n.of(context).pinnedBy(
                        s.accounts[p.pinnedById]?.name ??
                            L10n.of(context).someoneLower,
                      )
                    : L10n.of(context).pinnedByAt(
                        s.accounts[p.pinnedById]?.name ??
                            L10n.of(context).someoneLower,
                        shortWhen(p.pinnedAt!),
                      ),
                style: monoStyle(context),
              ),
            ],
          ),
          onTap: () {
            if (p.outsideHistory && m != null) {
              // Not in this reader's history: opened on its own, in
              // place, with its files (ruling R87).
              showScreen<void>(
                context,
                title: L10n.of(context).tabPins,
                child: SingleChildScrollView(
                  key: const Key('pin-alone'),
                  padding: const EdgeInsets.all(8),
                  child: MessageTile(
                    server: s,
                    message: m,
                    markdown: MarkdownContext(accounts: s.accounts),
                  ),
                ),
              );
              return;
            }
            final app = AppScope.read(context);
            Navigator.of(context).maybePop();
            ConversationIntents.instance.openAt(
              app,
              OpenTarget(s.localId, conversationId),
              p.messageId,
            );
          },
        );
      },
    );
  }
}
