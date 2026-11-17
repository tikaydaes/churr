import 'package:flutter/material.dart';

import '../../../core/api/errors.dart';
import '../../../core/models/chat_request.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/app_state.dart';
import '../../../state/chat_requests.dart';
import '../../../state/server_state.dart';
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../../widgets/avatar.dart';
import '../account/common.dart';

/// Requests (rulings R78 and R103, docs/CLIENTS.md 9.28): who is asking to
/// start a chat, has included the person in a group chat or invites them
/// to a channel, on every server, with the four answers. It shows only
/// what the server sends: who is asking, what kind of request, the names
/// in a group chat, and a channel as someone outside it sees it. A public
/// channel can be previewed before answering.
class RequestsScreen extends StatefulWidget {
  const RequestsScreen({super.key, this.onOpened});

  /// Called when an accepted chat has been opened, to close the list.
  final VoidCallback? onOpened;

  @override
  State<RequestsScreen> createState() => _RequestsScreenState();
}

class _RequestsScreenState extends State<RequestsScreen> {
  String? error;
  final busy = <String>{};

  Future<void> _answer(
    AppState app,
    ServerState s,
    ChatRequest r,
    String how,
  ) async {
    setState(() {
      busy.add(r.id);
      error = null;
    });
    final actions = ChatRequestActions(s);
    try {
      switch (how) {
        case 'accept':
          final c = await actions.accept(r);
          app.openConversation(OpenTarget(s.localId, c.id));
          widget.onOpened?.call();
        case 'decline':
          await actions.decline(r);
        case 'dismiss':
          await actions.dismiss(r);
        case 'block':
          await actions.block(r);
        case 'preview':
          final c = await actions.preview(r);
          if (c != null) {
            app.openConversation(OpenTarget(s.localId, c.id));
            widget.onOpened?.call();
          }
      }
    } on ApiException catch (e) {
      if (mounted) setState(() => error = e.message);
    } finally {
      if (mounted) setState(() => busy.remove(r.id));
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final app = AppScope.of(context);
    return ListenableBuilder(
      listenable: Listenable.merge(app.servers),
      builder: (context, _) {
        final items = [
          for (final s in app.servers)
            for (final r in s.chatRequests) (s, r),
        ];
        if (items.isEmpty) {
          return Container(
            color: c.bg,
            alignment: Alignment.center,
            padding: const EdgeInsets.all(32),
            child: Text(
              t.requestsEmpty,
              key: const Key('requests-empty'),
              textAlign: TextAlign.center,
              style: TextStyle(color: c.dim),
            ),
          );
        }
        return Container(
          color: c.bg,
          child: ListView(
            key: const Key('requests-list'),
            padding: const EdgeInsets.fromLTRB(20, 8, 20, 24),
            children: [
              if (error != null) ErrorLine(error!),
              Text(
                t.requestAnswersNote,
                style: TextStyle(color: c.dim, fontSize: 12.5),
              ),
              for (final (s, r) in items)
                _RequestTile(
                  key: Key('request-${r.id}'),
                  server: s,
                  request: r,
                  showServer: app.servers.length > 1,
                  busy: busy.contains(r.id),
                  onAnswer: (how) => _answer(app, s, r, how),
                ),
            ],
          ),
        );
      },
    );
  }
}

class _RequestTile extends StatelessWidget {
  const _RequestTile({
    super.key,
    required this.server,
    required this.request,
    required this.showServer,
    required this.busy,
    required this.onAnswer,
  });
  final ServerState server;
  final ChatRequest request;
  final bool showServer;
  final bool busy;
  final void Function(String how) onAnswer;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final a = server.accounts[request.account.id] ?? request.account;
    final name = a.displayName.isEmpty ? a.username : a.displayName;
    final what = switch (request.kind) {
      'sealed' => t.requestAsksSealed,
      'group' => t.requestIncludesGroup,
      'channel' => t.requestInvitesChannel,
      _ => t.requestAsksDirect,
    };
    final when = request.createdAt == null
        ? ''
        : ' · ${formatWhen(context, request.createdAt!)}';
    final small = TextStyle(color: c.dim, fontSize: 12.5);
    return Container(
      margin: const EdgeInsets.only(top: 10),
      padding: const EdgeInsets.all(12),
      color: c.panel,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Avatar(account: a, server: server, size: 40),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text.rich(
                      TextSpan(
                        children: [
                          TextSpan(
                            text: name,
                            style: TextStyle(
                              fontWeight: FontWeight.w700,
                              color: c.ink,
                            ),
                          ),
                          TextSpan(
                            text: '  @${a.username}',
                            style: TextStyle(color: c.dim),
                          ),
                        ],
                      ),
                    ),
                    Text(
                      '$what$when${showServer ? ' · ${server.name}' : ''}',
                      style: small,
                    ),
                  ],
                ),
              ),
            ],
          ),
          if (request.isGroup && request.people.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text(
                t.requestGroupWith(request.people.join(', ')),
                key: const Key('request-people'),
                style: TextStyle(color: c.ink, fontSize: 14),
              ),
            ),
          if (request.isChannel && request.channel != null)
            _InvitedChannel(request: request),
          const SizedBox(height: 10),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              FilledButton(
                key: const Key('request-accept'),
                onPressed: busy ? null : () => onAnswer('accept'),
                child: Text(t.requestAccept),
              ),
              OutlinedButton(
                key: const Key('request-decline'),
                onPressed: busy ? null : () => onAnswer('decline'),
                child: Text(t.requestDecline),
              ),
              OutlinedButton(
                key: const Key('request-dismiss'),
                onPressed: busy ? null : () => onAnswer('dismiss'),
                child: Text(t.requestDismiss),
              ),
              OutlinedButton(
                key: const Key('request-block'),
                onPressed: busy ? null : () => onAnswer('block'),
                child: Text(t.requestBlock),
              ),
              // Only a public channel can be read before accepting.
              if (request.previewable)
                TextButton(
                  key: const Key('request-preview'),
                  onPressed: busy ? null : () => onAnswer('preview'),
                  child: Text(t.requestPreview),
                ),
            ],
          ),
        ],
      ),
    );
  }
}

/// The channel an invitation is for, as someone outside it sees it: its
/// name, access, member count and topic, the level accepting brings when
/// it is not the usual one, and the owner's override when it is one.
class _InvitedChannel extends StatelessWidget {
  const _InvitedChannel({required this.request});
  final ChatRequest request;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final ch = request.channel!;
    final access = switch (ch.access) {
      'knock' => t.kindKnock,
      'private' => t.channelCardPrivate,
      _ => t.channelCardPublic,
    };
    final level = switch (request.level) {
      'reader' => t.levelReader,
      'moderator' => t.levelModerator,
      'manager' => t.levelManager,
      _ => null,
    };
    return Padding(
      key: const Key('request-channel'),
      padding: const EdgeInsets.only(top: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            '#${ch.name ?? ''}',
            style: TextStyle(fontWeight: FontWeight.w700, color: c.ink),
          ),
          Text(
            '$access · ${t.channelCardMembers(ch.memberCount)}',
            style: TextStyle(color: c.dim, fontSize: 12.5),
          ),
          if (ch.topic.isNotEmpty)
            Text(
              ch.topic,
              maxLines: 3,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(color: c.ink, fontSize: 14),
            ),
          if (level != null)
            Text(
              t.requestJoinsAs(level.toLowerCase()),
              style: TextStyle(color: c.dim, fontSize: 12.5),
            ),
          if (request.override)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text(
                t.requestOverrideNote,
                key: const Key('request-override'),
                style: TextStyle(color: c.yellowInk, fontSize: 12.5),
              ),
            ),
        ],
      ),
    );
  }
}
