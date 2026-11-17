import 'dart:async';

import 'package:flutter/material.dart';

import '../../../core/api/notices.dart';
import '../../../core/models/account.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/server_state.dart';
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../account/common.dart';

/// The wording of a notice (the web chat's shared/alerts.js says the same).
/// [accounts] names the people; [convName] a conversation, or null.
String noticeText(
  Notice n,
  Map<String, Account> accounts, {
  String? Function(String id)? convName,
}) {
  final by = n.byId == null ? null : accounts[n.byId]?.name;
  final about = n.subjectId == null ? null : accounts[n.subjectId]?.name;
  switch (n.kind) {
    case 'recovery_link_used':
      return by == null
          ? 'Someone signed in to your account with a recovery link made on the server. If that was not you, tell an owner or an admin.'
          : 'Someone signed in to your account with a recovery link $by made. If that was not you, tell an owner or an admin.';
    case 'owner_made':
      final whom = about ?? 'Someone';
      return by == null || by == about
          ? '$whom was made an owner.'
          : '$whom was made an owner by $by.';
    case 'owner_stepped_down':
      return '${about ?? 'Someone'} stepped down as an owner.';
    case 'owner_promotion_waiting':
      return '${by ?? 'An owner'} made ${about ?? 'someone'} an owner, from when the wait for new owners has run. Any owner can withdraw it until then, in the console.';
    case 'export_started':
      return '${by ?? 'An owner'} started the full export.';
    case 'export_downloaded':
      return '${by ?? 'An owner'} downloaded the full export.';
    case 'override_used':
      final id = n.conversationId;
      final where = id == null ? null : convName?.call(id);
      return '${by ?? 'Someone'} added ${about ?? 'someone'} to ${where == null || where.isEmpty ? 'a private channel' : '#$where'} by override.';
    default:
      return 'Something was done that concerns your account.';
  }
}

/// Notices (docs/FEATURES.md section 21.13, docs/CLIENTS.md 9.30): what
/// was done that concerns the account, kept for it, on every server,
/// newest first. Opening the screen marks them seen; they stay listed.
class NoticesScreen extends StatefulWidget {
  const NoticesScreen({super.key});

  @override
  State<NoticesScreen> createState() => _NoticesScreenState();
}

class _NoticesScreenState extends State<NoticesScreen> {
  bool _started = false;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_started) return;
    _started = true;
    for (final s in AppScope.read(context).servers) {
      unawaited(s.refreshNotices().then((_) => s.seeNotices()));
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
        final shown =
            [
              for (final s in app.servers)
                for (final n in s.notices.notices) (s, n),
            ]..sort(
              (a, b) => (b.$2.createdAt ?? DateTime(0)).compareTo(
                a.$2.createdAt ?? DateTime(0),
              ),
            );
        return Container(
          color: c.bg,
          child: ListView(
            key: const Key('notices-list'),
            padding: const EdgeInsets.fromLTRB(20, 8, 20, 24),
            children: [
              Text(t.noticesIntro, style: TextStyle(color: c.dim)),
              if (shown.isEmpty)
                Padding(
                  padding: const EdgeInsets.all(32),
                  child: Text(
                    t.noticesEmpty,
                    key: const Key('notices-empty'),
                    textAlign: TextAlign.center,
                    style: TextStyle(color: c.dim),
                  ),
                ),
              for (final (s, n) in shown)
                _NoticeTile(
                  key: Key('notice-${n.id}'),
                  server: s,
                  notice: n,
                  showServer: app.servers.length > 1,
                ),
            ],
          ),
        );
      },
    );
  }
}

class _NoticeTile extends StatelessWidget {
  const _NoticeTile({
    super.key,
    required this.server,
    required this.notice,
    required this.showServer,
  });
  final ServerState server;
  final Notice notice;
  final bool showServer;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final n = notice;
    String? convName(String id) {
      for (final r in server.conversations) {
        if (r.conversation.id == id) return r.conversation.name;
      }
      return null;
    }

    final details = [
      if (n.createdAt != null) formatWhen(context, n.createdAt!),
      if (showServer) server.name,
    ].join(' · ');
    return Container(
      margin: const EdgeInsets.only(top: 10),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: c.panel,
        border: n.seenAt == null
            ? Border(left: BorderSide(color: c.pink, width: 3))
            : null,
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            noticeText(n, server.accounts, convName: convName),
            style: TextStyle(color: c.ink),
          ),
          Text(details, style: TextStyle(color: c.dim, fontSize: 12.5)),
        ],
      ),
    );
  }
}
