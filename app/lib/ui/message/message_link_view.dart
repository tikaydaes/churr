// A link to a message of this server (ruling R73, MESSAGE-FORMAT.md 3.2),
// drawn as a compact quote of that message: who, when, where and its first
// lines. The entity holds nothing of the message; it is fetched with
// getMessage, which answers only a reader who may see it, so a reader who
// may not sees "A message you can't see". Fetched again after a minute, so
// an edit or a deletion shows.

import 'package:flutter/material.dart';

import '../../core/api/conversation_ops.dart';
import '../../core/instant/format.dart';
import '../../core/instant/zone.dart';
import '../../core/models/conversation.dart';
import '../../core/models/message.dart';
import '../../l10n/app_localizations.dart';
import '../../state/server_state.dart';
import '../theme/tokens.dart';
import 'instant_view.dart';

/// What a message link names, as the reader may see it: the message, or
/// null for one the reader cannot see.
class LinkedMessages {
  LinkedMessages(this.fetch);

  /// Fetches one message; throws when the reader may not see it.
  final Future<Message> Function(String id) fetch;

  final _seen = <String, (DateTime, Future<Message?>)>{};

  /// How long an answer is kept.
  static const keep = Duration(minutes: 1);

  Future<Message?> get(String id) {
    final had = _seen[id];
    if (had != null && DateTime.now().difference(had.$1) < keep) return had.$2;
    final f = fetch(id).then<Message?>((m) => m).catchError((_) => null);
    _seen[id] = (DateTime.now(), f);
    return f;
  }

  static final _byServer = Expando<LinkedMessages>();

  /// The one for a server.
  static LinkedMessages of(ServerState server) =>
      _byServer[server] ??= LinkedMessages((id) => server.api.getMessage(id));
}

/// The preview of a message link.
class MessageLinkView extends StatefulWidget {
  const MessageLinkView({
    super.key,
    required this.entity,
    required this.typed,
    required this.linked,
    required this.nameOf,
    required this.conversationOf,
    required this.onOpen,
  });

  final Entity entity;
  final String typed;

  /// Where the message comes from: [LinkedMessages.of] the server.
  final LinkedMessages linked;

  /// An account's name, and a conversation, as the reader knows them.
  final String? Function(String accountId) nameOf;
  final Conversation? Function(String id) conversationOf;

  /// Goes to the message: its conversation, and its thread for a reply.
  final void Function(String conversationId, String messageId, String? parent)
  onOpen;

  @override
  State<MessageLinkView> createState() => _MessageLinkViewState();
}

class _MessageLinkViewState extends State<MessageLinkView> {
  late Future<Message?> _m;

  @override
  void initState() {
    super.initState();
    _m = widget.linked.get(widget.entity.messageId ?? '');
  }

  /// The first three lines of a message's text.
  static String firstLines(Message m) {
    final lines = m.text
        .split('\n')
        .where((l) => l.trim().isNotEmpty)
        .take(3)
        .join('\n');
    return lines.length > 280 ? '${lines.substring(0, 279)}…' : lines;
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final t = L10n.of(context);
    return FutureBuilder<Message?>(
      future: _m,
      builder: (context, snap) {
        final m = snap.data;
        final loading = snap.connectionState != ConnectionState.done;
        String? head;
        String body;
        var dim = false;
        if (loading) {
          body = t.messageLinkLoading;
          dim = true;
        } else if (m == null || m.sealed != null) {
          body = t.messageLinkHidden;
          dim = true;
        } else {
          final who = widget.nameOf(m.authorId) ?? t.someone;
          final conv = widget.conversationOf(m.conversationId);
          final where = conv?.name == null
              ? ''
              : conv!.kind == 'channel'
              ? ' · #${conv.name}'
              : ' · ${conv.name}';
          final when = compactInstant(
            m.createdAt.millisecondsSinceEpoch,
            InstantEnv.viewerZone,
            Localizations.localeOf(context).toString(),
            instantWords(context),
          );
          head = '$who · $when$where';
          body = m.isDeleted ? t.messageLinkDeleted : firstLines(m);
          dim = m.isDeleted;
        }
        final e = widget.entity;
        return Semantics(
          container: true,
          link: true,
          label: t.messageLinkLabel(widget.typed, [?head, body].join(': ')),
          child: ExcludeSemantics(
            child: InkWell(
              onTap: () => widget.onOpen(
                m?.conversationId ?? e.conversationId ?? '',
                m?.id ?? e.messageId ?? '',
                m?.parentId,
              ),
              child: Container(
                constraints: const BoxConstraints(maxWidth: 480),
                margin: const EdgeInsets.symmetric(vertical: 2),
                padding: const EdgeInsets.fromLTRB(10, 4, 10, 4),
                decoration: BoxDecoration(
                  color: c.raised,
                  border: Border(left: BorderSide(color: c.cyan, width: 4)),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    if (head != null)
                      Text(
                        head,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          fontSize: 13,
                          fontWeight: FontWeight.w600,
                          color: c.ink,
                        ),
                      ),
                    Text(
                      body,
                      maxLines: 3,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: 13,
                        color: dim ? c.dim : c.ink,
                        fontStyle: dim ? FontStyle.italic : null,
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        );
      },
    );
  }
}
