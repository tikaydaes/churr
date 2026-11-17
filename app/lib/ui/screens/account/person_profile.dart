import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../../../core/conversation/large.dart';
import '../../../core/api/account.dart';
import '../../../core/api/channels.dart';
import '../../../core/api/conversation_ops.dart';
import '../../../core/api/errors.dart';
import '../../../core/api/watches.dart';
import '../../../core/models/account.dart';
import '../../../core/models/personal.dart';
import '../../../l10n/app_localizations.dart';
import '../../../platform/device.dart' as device;
import '../../../state/account_hub.dart';
import '../../../state/app_state.dart';
import '../../../state/server_state.dart';
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import '../../theme/churr_icons.dart';
import '../../widgets/avatar.dart';
import 'common.dart';

/// Opens someone else's profile (docs/CLIENTS.md 9.11).
Future<void> showPersonProfile(
  BuildContext context,
  ServerState server,
  String accountId,
) {
  final a = server.accounts[accountId];
  return showAccountScreen(
    context,
    title: a?.name ?? L10n.of(context).profile,
    width: 480,
    child: PersonProfile(server: server, accountId: accountId),
  );
}

/// Someone else's profile: avatar, names, title, pronouns, local time,
/// presence, status, availability (with the out-of-office notice), group
/// labels and role; message them, or block them.
class PersonProfile extends StatefulWidget {
  const PersonProfile({
    super.key,
    required this.server,
    required this.accountId,
    this.timeIn,
  });
  final ServerState server;
  final String accountId;

  /// The wall-clock time in a zone; replaced in tests.
  final DateTime? Function(String zone, {DateTime? now})? timeIn;

  @override
  State<PersonProfile> createState() => _PersonProfileState();
}

class _PersonProfileState extends State<PersonProfile> {
  String? error;

  /// Whether the person has a watch waiting for this account (ruling R76);
  /// null until asked.
  bool? watching;

  @override
  void initState() {
    super.initState();
    // The profile is drawn from the cache at once and fetched fresh.
    widget.server.api
        .getAccount(widget.accountId)
        .then((a) => a.id.isEmpty ? null : widget.server.db.upsertAccounts([a]))
        .catchError((_) {});
    // While the profile is open the server keeps its presence current.
    widget.server.showingProfile(widget.accountId);
    if (widget.accountId != widget.server.me?.id) {
      widget.server.api
          .listWatches()
          .then((w) {
            if (mounted) {
              setState(
                () => watching = w.any((x) => x.accountId == widget.accountId),
              );
            }
          })
          .catchError((_) {
            if (mounted) setState(() => watching = false);
          });
    }
  }

  /// "Tell me when Ada is around", or stopping it.
  Future<void> _watch(Account a) async {
    final t = L10n.of(context);
    final messenger = ScaffoldMessenger.maybeOf(context);
    try {
      final String said;
      if (watching == true) {
        await widget.server.api.unwatchAccount(a.id);
        said = t.watchStopped(a.name);
        if (mounted) setState(() => watching = false);
      } else {
        final around = await widget.server.api.watchAccount(a.id);
        said = around ? t.watchIsAround(a.name) : t.watchSet(a.name);
        if (mounted) setState(() => watching = !around);
      }
      messenger?.showSnackBar(SnackBar(content: Text(said)));
    } on ApiException catch (e) {
      if (mounted) setState(() => error = e.message);
    }
  }

  @override
  void dispose() {
    widget.server.showingProfile(null);
    super.dispose();
  }

  Future<void> _message() async {
    final app = AppScope.read(context);
    final nav = Navigator.of(context);
    try {
      final c = await widget.server.api.openChat([widget.accountId]);
      await widget.server.db.upsertConversation(c, syncedSeq: c.lastSeq);
      app.openConversation(OpenTarget(widget.server.localId, c.id));
      if (nav.canPop()) nav.pop();
    } on Exception catch (e) {
      if (mounted) setState(() => error = L10n.of(context).didNotWork('$e'));
    }
  }

  Future<void> _block(Account a, bool blocked) async {
    final t = L10n.of(context);
    if (blocked) {
      final ok = await showDialog<bool>(
        animationStyle: AnimationStyle.noAnimation,
        context: context,
        builder: (context) => AlertDialog(
          content: Text(t.blockConfirm(a.name)),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: Text(t.cancel),
            ),
            FilledButton(
              key: const Key('confirm-block'),
              onPressed: () => Navigator.pop(context, true),
              child: Text(t.block),
            ),
          ],
        ),
      );
      if (ok != true) return;
    }
    if (!mounted) return;
    try {
      await AccountHub.of(AppScope.read(context))
          .setBlocked(widget.server, a.id, blocked);
    } on Exception catch (e) {
      if (mounted) setState(() => error = t.didNotWork('$e'));
    }
  }

  /// Reports this person to the instance's admins.
  Future<void> _report(Account a) async {
    final t = L10n.of(context);
    final messenger = ScaffoldMessenger.maybeOf(context);
    final reason = TextEditingController();
    final send = await showDialog<bool>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (context) => AlertDialog(
        title: Text(t.reportPersonTitle(a.name)),
        content: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 420),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(t.reportPersonBody),
              const SizedBox(height: 12),
              TextField(
                key: const Key('report-reason'),
                controller: reason,
                maxLines: 4,
                maxLength: 500,
                decoration: InputDecoration(labelText: t.reportReason),
              ),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: Text(t.cancel),
          ),
          FilledButton(
            key: const Key('confirm-report'),
            onPressed: () => Navigator.pop(context, true),
            child: Text(t.reportSend),
          ),
        ],
      ),
    );
    if (send != true || !mounted) return;
    try {
      await widget.server.api.reportAccount(a.id, reason.text.trim());
      messenger?.showSnackBar(SnackBar(content: Text(t.reported)));
    } on ApiException catch (e) {
      if (mounted) setState(() => error = e.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final s = widget.server;
    return ListenableBuilder(
      listenable: s,
      builder: (context, _) {
        final a = s.accounts[widget.accountId];
        if (a == null) return const Center(child: Busy());
        final now = DateTime.now();
        final locale = Localizations.localeOf(context).toString();
        final status = a.fullStatus;
        final availability = a.availability;
        final zone = a.timeZone;
        final local = zone == null
            ? null
            : (widget.timeIn ?? device.timeIn)(zone, now: now);
        final blocked = s.me?.blockedIds.contains(a.id) ?? false;
        final groups = [
          for (final id in a.groupIds)
            if (s.groups[id] != null) '@${s.groups[id]!.handle}',
        ];
        final presence = switch (a.presence) {
          'online' => t.presenceOnline,
          'away' => t.presenceAway,
          _ => t.presenceOffline,
        };
        return ListView(
          padding: const EdgeInsets.all(20),
          children: [
            Row(
              children: [
                Avatar(account: a, server: s, size: 72),
                const SizedBox(width: 16),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        a.name,
                        style: const TextStyle(
                          fontSize: 20,
                          fontWeight: FontWeight.w800,
                        ),
                      ),
                      SelectableText(
                        '@${a.username}${a.serverName == null ? '' : '@${a.serverName}'}',
                        style: monoStyle(context, size: 12),
                      ),
                      if ((a.title ?? '').isNotEmpty) Text(a.title!),
                      if ((a.pronouns ?? '').isNotEmpty)
                        Text(a.pronouns!, style: TextStyle(color: c.dim)),
                    ],
                  ),
                ),
              ],
            ),
            const SizedBox(height: 16),
            Wrap(
              spacing: 12,
              runSpacing: 6,
              children: [
                // Presence has a shape as well as a colour.
                _Fact(
                  icon: a.presence == 'online'
                      ? ChurrIcons.boxDot
                      : (a.presence == 'away'
                            ? ChurrIcons.moon
                            : ChurrIcons.box),
                  color: a.presence == 'online' ? c.cyan : c.faint,
                  text: presence,
                ),
                // Roughly when they were last here; the server never
                // says the time. Nothing for someone online, whose
                // presence says so, or who hides it.
                if (a.presence != 'online')
                  switch (LastSeen.parse(a.raw['last_seen'])) {
                    LastSeen.recently => _Fact(
                      icon: ChurrIcons.moon,
                      color: c.faint,
                      text: t.lastSeenRecently,
                    ),
                    LastSeen.withinWeek => _Fact(
                      icon: ChurrIcons.moon,
                      color: c.faint,
                      text: t.lastSeenWithinWeek,
                    ),
                    LastSeen.longAgo => _Fact(
                      icon: ChurrIcons.moon,
                      color: c.faint,
                      text: t.lastSeenLongAgo,
                    ),
                    _ => const SizedBox.shrink(),
                  },
                if (a.inCall)
                  _Fact(
                    icon: ChurrIcons.call,
                    color: c.lavender,
                    text: t.inACall,
                  ),
                if (a.dnd)
                  _Fact(
                    icon: ChurrIcons.dnd,
                    color: c.pink,
                    text: t.doNotDisturbMark,
                  ),
                if (local != null)
                  _Fact(
                    icon: ChurrIcons.clock,
                    color: c.dim,
                    text: t.localTime(DateFormat.jm(locale).format(local)),
                  ),
              ],
            ),
            if (status != null && status.existsAt(now))
              Padding(
                padding: const EdgeInsets.only(top: 12),
                child: Text(
                  status.until == null
                      ? [?status.emoji, ?status.text].join(' ')
                      : t.statusUntil(
                          [?status.emoji, ?status.text].join(' '),
                          formatUntil(context, status.until!),
                        ),
                  key: const Key('person-status'),
                ),
              ),
            if (availability != null && availability.activeAt(now))
              availability.outOfOffice
                  ? NoteLine(
                      availability.until == null
                          ? t.outOfOfficeNote(a.name)
                          : t.outOfOfficeUntil(
                              DateFormat.yMMMEd(locale)
                                  .format(availability.until!.toLocal()),
                            ),
                      icon: ChurrIcons.plane,
                    )
                  : Padding(
                      padding: const EdgeInsets.only(top: 8),
                      child: Text(
                        availability.until == null
                            ? t.inMeeting
                            : t.inMeetingUntil(
                                formatUntil(context, availability.until!),
                              ),
                      ),
                    ),
            if (groups.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(t.groupsLabel(groups.join(', '))),
              ),
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text(t.roleLabel(a.role), style: TextStyle(color: c.dim)),
            ),
            const SizedBox(height: 16),
            if (error != null) ErrorLine(error!),
            if (a.id != s.me?.id && !a.isBot)
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  FilledButton.icon(
                    onPressed: blocked ? null : _message,
                    icon: const Glyph(ChurrIcons.chat, size: 16),
                    label: Text(t.message),
                  ),
                  if (!blocked && a.removedAt == null)
                    OutlinedButton.icon(
                      key: const Key('watch-toggle'),
                      onPressed: watching == null ? null : () => _watch(a),
                      icon: const Glyph(ChurrIcons.notify, size: 16),
                      label: Text(
                        watching == true
                            ? t.stopWatchingFor(a.name)
                            : t.watchFor(a.name),
                      ),
                    ),
                  OutlinedButton(
                    key: const Key('block-toggle'),
                    onPressed: () => _block(a, !blocked),
                    child: Text(blocked ? t.unblock : t.block),
                  ),
                  if (a.removedAt == null)
                    OutlinedButton(
                      key: const Key('report-person'),
                      onPressed: () => _report(a),
                      child: Text(t.reportPerson),
                    ),
                ],
              ),
          ],
        );
      },
    );
  }
}

class _Fact extends StatelessWidget {
  const _Fact({required this.icon, required this.color, required this.text});
  final ChurrGlyph icon;
  final Color color;
  final String text;

  @override
  Widget build(BuildContext context) => Row(
    mainAxisSize: MainAxisSize.min,
    children: [
      Glyph(icon, size: 12, color: color),
      const SizedBox(width: 4),
      Text(text),
    ],
  );
}
