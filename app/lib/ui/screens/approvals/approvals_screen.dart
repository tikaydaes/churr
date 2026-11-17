import 'dart:async';

import 'package:flutter/material.dart';

import '../../../core/api/approvals.dart';
import '../../../core/api/errors.dart';
import '../../../core/models/json.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/server_state.dart';
import '../../scopes.dart';
import '../../theme/theme.dart';
import '../../theme/tokens.dart';
import '../../widgets/avatar.dart';
import '../account/common.dart';

/// The name of a kind of request, as a filter.
String approvalKindName(L10n t, String kind) => switch (kind) {
  'registration' => t.approvalKindRegistration,
  'channel' => t.approvalKindChannel,
  'webhook' => t.approvalKindWebhook,
  _ => kind,
};

/// What one request asks for, in a line.
String approvalAsks(L10n t, Approval a) => switch (a.kind) {
  'registration' => t.approvalAsksRegistration,
  'channel' => t.approvalAsksChannel(a.summary),
  'webhook' => t.approvalAsksWebhook(a.summary),
  _ => a.summary,
};

/// Approvals (docs/FEATURES.md section 31, docs/CLIENTS.md 9.29): the
/// requests that wait for the person on every server where they decide
/// some, oldest first, with the kinds as filters. Approving or declining
/// (with an optional reason the requester is shown) takes a request out
/// of the list; `approval.created` and `approval.decided` keep it current.
class ApprovalsScreen extends StatefulWidget {
  const ApprovalsScreen({super.key});

  @override
  State<ApprovalsScreen> createState() => _ApprovalsScreenState();
}

class _ApprovalsScreenState extends State<ApprovalsScreen> {
  /// The waiting requests, by server; a server not yet read is missing.
  final lists = <ServerState, List<Approval>>{};
  final _subs = <StreamSubscription<Json>>[];
  final busy = <String>{};

  /// The kind shown, or null for all.
  String? kind;
  String? error;
  bool _started = false;

  List<ServerState> get _servers => [
    for (final s in AppScope.read(context).servers)
      if (s.approvals.kinds.isNotEmpty) s,
  ];

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_started) return;
    _started = true;
    for (final s in AppScope.read(context).servers) {
      _subs.add(
        s.sync.localEvents.listen((e) {
          final type = e['type'];
          if (type == 'approval.created' ||
              type == 'approval.decided' ||
              type == 'approvals.changed') {
            unawaited(_load(s));
          }
        }),
      );
    }
    for (final s in _servers) {
      unawaited(_load(s));
    }
  }

  @override
  void dispose() {
    for (final sub in _subs) {
      sub.cancel();
    }
    super.dispose();
  }

  Future<void> _load(ServerState s) async {
    try {
      final got = await s.api.listApprovals();
      if (mounted) setState(() => lists[s] = got);
    } on ApiException catch (e) {
      // One who decides nothing any more is refused: their list is empty.
      if (mounted) {
        setState(() {
          lists[s] = const [];
          if (e.code != 'forbidden') error = e.message;
        });
      }
    }
  }

  Future<void> _decide(ServerState s, Approval a, bool approve) async {
    final t = L10n.of(context);
    String? reason;
    if (!approve) {
      final r = await _askReason(t, a);
      if (r == null) return;
      reason = r;
    }
    setState(() {
      busy.add(a.id);
      error = null;
    });
    try {
      await s.api.decideApproval(a, approve: approve, reason: reason);
      if (!mounted) return;
      setState(() => lists[s] = [...?lists[s]]..remove(a));
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(
        SnackBar(
          content: Text(approve ? t.approvalApproved : t.approvalDeclined),
        ),
      );
      unawaited(s.refreshApprovals());
    } on ApiException catch (e) {
      if (mounted) setState(() => error = e.message);
      // Answered by someone else, or lapsed: the list is read again.
      if (e.code == 'not_found') unawaited(_load(s));
    } finally {
      if (mounted) setState(() => busy.remove(a.id));
    }
  }

  /// Asks for the reason of a decline; null when the person thinks better
  /// of it, '' for no reason.
  Future<String?> _askReason(L10n t, Approval a) async {
    final box = TextEditingController();
    final ok = await showDialog<bool>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t.approvalDeclineTitle),
        content: SizedBox(
          width: 420,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(approvalAsks(t, a)),
              TextField(
                key: const Key('approval-reason'),
                controller: box,
                autofocus: true,
                maxLength: 500,
                minLines: 1,
                maxLines: 4,
                decoration: InputDecoration(labelText: t.approvalReasonLabel),
              ),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: Text(t.cancel),
          ),
          FilledButton(
            key: const Key('approval-decline-go'),
            style: dangerButton(ChurrColors.of(ctx)),
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(t.approvalDecline),
          ),
        ],
      ),
    );
    // Not disposed here: the dialog may still be animating out.
    return ok == true ? box.text.trim() : null;
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final app = AppScope.of(context);
    return ListenableBuilder(
      listenable: Listenable.merge(app.servers),
      builder: (context, _) {
        final servers = _servers;
        final kinds = <String>{for (final s in servers) ...s.approvals.kinds};
        final shown = [
          for (final s in servers)
            for (final a in lists[s] ?? const <Approval>[])
              if (kind == null || a.kind == kind) (s, a),
        ];
        final loading = servers.any((s) => !lists.containsKey(s));
        return Container(
          color: c.bg,
          child: ListView(
            key: const Key('approvals-list'),
            padding: const EdgeInsets.fromLTRB(20, 8, 20, 24),
            children: [
              if (kinds.length > 1)
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    ChoiceChip(
                      key: const Key('approvals-kind-all'),
                      label: Text(t.approvalsAll),
                      selected: kind == null,
                      onSelected: (_) => setState(() => kind = null),
                    ),
                    for (final k in approvalKinds)
                      if (kinds.contains(k))
                        ChoiceChip(
                          key: Key('approvals-kind-$k'),
                          label: Text(approvalKindName(t, k)),
                          selected: kind == k,
                          onSelected: (_) => setState(() => kind = k),
                        ),
                  ],
                ),
              if (error != null) ErrorLine(error!),
              if (shown.isEmpty && !loading)
                Padding(
                  padding: const EdgeInsets.all(32),
                  child: Text(
                    t.approvalsEmpty,
                    key: const Key('approvals-empty'),
                    textAlign: TextAlign.center,
                    style: TextStyle(color: c.dim),
                  ),
                ),
              for (final (s, a) in shown)
                _ApprovalTile(
                  key: Key('approval-${a.id}'),
                  server: s,
                  approval: a,
                  showServer: app.servers.length > 1,
                  busy: busy.contains(a.id),
                  onDecide: (approve) => _decide(s, a, approve),
                ),
            ],
          ),
        );
      },
    );
  }
}

class _ApprovalTile extends StatelessWidget {
  const _ApprovalTile({
    super.key,
    required this.server,
    required this.approval,
    required this.showServer,
    required this.busy,
    required this.onDecide,
  });
  final ServerState server;
  final Approval approval;
  final bool showServer;
  final bool busy;
  final void Function(bool approve) onDecide;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final a = approval;
    final who = server.accounts[a.requesterId] ?? a.requester;
    final name = who?.name ?? t.someone;
    final details = [
      approvalKindName(t, a.kind),
      if (a.createdAt != null) formatWhen(context, a.createdAt!),
      if (a.expiresAt != null)
        t.approvalLapses(formatWhen(context, a.expiresAt!)),
      if (showServer) server.name,
    ].join(' · ');
    return Container(
      margin: const EdgeInsets.only(top: 10),
      padding: const EdgeInsets.all(12),
      color: c.panel,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Avatar(account: who, server: server, size: 40),
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
                          if (who != null)
                            TextSpan(
                              text: '  @${who.username}',
                              style: TextStyle(color: c.dim),
                            ),
                        ],
                      ),
                    ),
                    Text(approvalAsks(t, a)),
                    Text(
                      details,
                      style: TextStyle(color: c.dim, fontSize: 12.5),
                    ),
                  ],
                ),
              ),
            ],
          ),
          if (a.note.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text(
                '“${a.note}”',
                key: Key('approval-note-${a.id}'),
                style: TextStyle(color: c.ink),
              ),
            ),
          const SizedBox(height: 10),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              FilledButton(
                key: Key('approval-approve-${a.id}'),
                onPressed: busy ? null : () => onDecide(true),
                child: Text(t.approvalApprove),
              ),
              OutlinedButton(
                key: Key('approval-decline-${a.id}'),
                onPressed: busy ? null : () => onDecide(false),
                child: Text(t.approvalDecline),
              ),
            ],
          ),
        ],
      ),
    );
  }
}
