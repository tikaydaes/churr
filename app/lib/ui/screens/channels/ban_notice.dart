import 'dart:async';

import 'package:flutter/material.dart';

import '../../../core/api/channels.dart';
import '../../../core/models/json.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/app_state.dart';
import '../../../state/server_state.dart';
import '../../message/checklist.dart' show shortWhen;
import '../../theme/churr_icons.dart';
import 'common.dart';

/// Tells the person, once and plainly, that they were banned from a channel
/// (ruling R72, `channel_ban.created`). The channel itself is dropped from
/// the cache by sync as the event arrives.
class BanNoticeHost extends StatefulWidget {
  const BanNoticeHost({super.key, required this.app, required this.child});
  final AppState app;
  final Widget child;

  @override
  State<BanNoticeHost> createState() => _BanNoticeHostState();
}

class _BanNoticeHostState extends State<BanNoticeHost> {
  final _subs = <String, StreamSubscription<Json>>{};
  final _told = <String>{};

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
      _subs[s.localId] ??= s.sync.localEvents.listen((e) => _onEvent(s, e));
    }
  }

  void _onEvent(ServerState server, Json e) {
    if (e['type'] != 'channel_ban.created') return;
    final ban = object(e['ban']);
    if (ban == null || ban['account_id'] != server.me?.id) return;
    final id = ban['id'] as String? ?? '';
    if (!_told.add(id) || !mounted) return;
    final open = widget.app.open;
    if (open != null &&
        open.serverId == server.localId &&
        open.conversationId == ban['conversation_id']) {
      widget.app.openConversation(null);
    }
    say(context, banNotice(L10n.of(context), e));
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

/// What the person banned is told: where, until when, and why.
String banNotice(L10n t, Json e) {
  final ban = object(e['ban']) ?? const <String, dynamic>{};
  final name = e['conversation_name'] as String? ?? '';
  final where = name.isEmpty ? t.banNoticeAChat : '#$name';
  final until = parseTime(ban['expires_at']);
  final reason = ban['reason'] as String? ?? '';
  final head = until == null
      ? t.banNotice(where)
      : t.banNoticeUntil(where, shortWhen(until));
  return reason.isEmpty ? head : t.banNoticeReason(head, reason);
}

/// Asks why and for how long, then bans the account from the channel. True
/// when it was done.
Future<bool> showBanDialog(
  BuildContext context,
  ServerState server,
  String conversationId,
  String accountId,
) async {
  final t = L10n.of(context);
  final who = server.accounts[accountId]?.name ?? t.them;
  final reason = TextEditingController();
  var seconds = 0;
  final ok = await showDialog<bool>(
    animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (ctx) => StatefulBuilder(
      builder: (ctx, set) => AlertDialog(
        title: Text(t.banTitle(who)),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(t.banBody),
            const SizedBox(height: 12),
            TextField(
              key: const Key('ban-reason'),
              controller: reason,
              maxLength: 1000,
              decoration: InputDecoration(labelText: t.banReason),
            ),
            DropdownButton<int>(
              key: const Key('ban-for'),
              elevation: 0,
              icon: const Glyph(ChurrIcons.chevronDown, size: 16),
              isExpanded: true,
              value: seconds,
              items: [
                for (final e in {
                  0: t.banUntilLifted,
                  3600: t.hour1,
                  86400: t.day1,
                  604800: t.days7,
                  2592000: t.days30,
                }.entries)
                  DropdownMenuItem(value: e.key, child: Text(e.value)),
              ],
              onChanged: (v) => set(() => seconds = v ?? 0),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: Text(t.cancel),
          ),
          FilledButton(
            key: const Key('confirm-ban'),
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(t.banYes),
          ),
        ],
      ),
    ),
  );
  if (ok != true) return false;
  try {
    await server.api.banFromChannel(
      conversationId,
      accountId,
      reason: reason.text.trim().isEmpty ? null : reason.text.trim(),
      seconds: seconds == 0 ? null : seconds,
    );
  } on Exception catch (e) {
    if (context.mounted) say(context, refusal(e));
    return false;
  }
  if (context.mounted) say(context, t.cmdBanned(who));
  return true;
}
