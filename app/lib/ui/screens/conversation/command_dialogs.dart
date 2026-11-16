import 'dart:async';

import 'package:flutter/material.dart';

import '../../../core/api/channels.dart';
import '../../../core/api/conversation_ops.dart';
import '../../../core/api/operations.dart';
import '../../../core/commands/commands.dart';
import '../../../core/db/conversation_queries.dart';
import '../../../core/models/json.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/server_state.dart';
import '../../message/checklist.dart' show shortWhen;
import '../../theme/tokens.dart';
import '../../widgets/avatar.dart';
import '../channels/common.dart';

// The dialogs of the commands of ruling R74: /help, /banlist, and the
// requests to join a channel that its managers answer (/knock).

/// The name of a group of commands in /help.
String commandGroupName(L10n t, String group) => switch (group) {
  'post' => t.cmdGroupPost,
  'conversations' => t.cmdGroupConversations,
  'members' => t.cmdGroupMembers,
  'people' => t.cmdGroupPeople,
  'status' => t.cmdGroupStatus,
  'server' => t.cmdGroupServer,
  'info' => t.cmdGroupInfo,
  'forms' => t.cmdGroupForms,
  'reminders' => t.cmdGroupReminders,
  'chance' => t.cmdGroupChance,
  'saved' => t.cmdGroupSavedReplies,
  _ => t.cmdGroupBots,
};

/// The group a listed command is shown under in /help.
String helpGroupOf(SlashCommand c) => switch (c.kind) {
  'builtin' => commandGroupOf[c.name] ?? 'info',
  'saved_reply' => 'saved',
  _ => 'bots',
};

/// The commands /help shows: what can be run here for this person, grouped
/// in the table's order, with the saved replies and bots' commands after.
/// [listed] is the server's command list for the conversation, which leaves
/// out what is hidden for the person; [query] narrows it by name, alias or
/// description.
List<(String, List<SlashCommand>)> helpGroups(
  List<SlashCommand> listed,
  String query,
) {
  final q = query.trim().toLowerCase().replaceFirst(RegExp('^/'), '');
  bool hit(SlashCommand c) =>
      q.isEmpty ||
      c.name.contains(q) ||
      c.description.toLowerCase().contains(q) ||
      (c.kind == 'builtin' &&
          commandAliasesOf(c.name).any((a) => a.contains(q)));
  final by = <String, List<SlashCommand>>{};
  for (final c in listed) {
    // An alias the table lists as a row (/part, /mahjong, /info) is shown
    // with the command it stands for.
    if (c.kind == 'builtin' && commandAliases.containsKey(c.name)) continue;
    if (hit(c)) (by[helpGroupOf(c)] ??= []).add(c);
  }
  return [
    for (final g in [...commandGroups, 'saved', 'bots'])
      if (by[g] != null) (g, by[g]!),
  ];
}

/// Shows every command the person can use here, grouped and searchable.
/// Returns the name of the one chosen, to put in the message box, or null.
Future<String?> showHelpDialog(
  BuildContext context,
  ServerState server,
  String conversationId, {
  String query = '',
}) async {
  var listed = await server.db.commands(conversationId);
  if (listed.isEmpty) {
    try {
      listed = await server.api.listCommands(conversationId);
    } on Exception {
      // Offline: the table alone, without what only the server knows.
    }
  }
  if (listed.isEmpty) {
    final admin = server.me?.role == 'owner' || server.me?.role == 'admin';
    final hidden = hiddenBuiltins(const [], admin: admin);
    listed = [
      for (final n in builtinCommands.keys)
        if (!hidden.contains(n))
          SlashCommand({'name': n, 'kind': 'builtin', 'description': ''}),
    ];
  }
  if (!context.mounted) return null;
  return showScreen<String>(
    context,
    title: L10n.of(context).helpTitle,
    child: HelpList(commands: listed, query: query),
  );
}

/// The searchable list of /help.
class HelpList extends StatefulWidget {
  const HelpList({super.key, required this.commands, this.query = ''});
  final List<SlashCommand> commands;
  final String query;

  @override
  State<HelpList> createState() => _HelpListState();
}

class _HelpListState extends State<HelpList> {
  late final search = TextEditingController(text: widget.query);

  @override
  void dispose() {
    search.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final groups = helpGroups(widget.commands, search.text);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 8),
          child: TextField(
            key: const Key('help-search'),
            controller: search,
            autofocus: true,
            decoration: InputDecoration(labelText: t.helpSearch),
            onChanged: (_) => setState(() {}),
          ),
        ),
        Expanded(
          child: groups.isEmpty
              ? Center(child: Text(t.helpNone))
              : ListView(
                  children: [
                    for (final (g, cmds) in groups) ...[
                      Padding(
                        padding: const EdgeInsets.fromLTRB(16, 14, 16, 4),
                        child: Semantics(
                          header: true,
                          child: Text(
                            commandGroupName(t, g).toUpperCase(),
                            style: TextStyle(
                              fontSize: 11,
                              fontWeight: FontWeight.w700,
                              letterSpacing: 1,
                              color: c.faint,
                            ),
                          ),
                        ),
                      ),
                      for (final cmd in cmds) _row(context, t, c, cmd),
                    ],
                  ],
                ),
        ),
      ],
    );
  }

  Widget _row(BuildContext context, L10n t, ChurrColors c, SlashCommand cmd) {
    final aliases = cmd.kind == 'builtin'
        ? [for (final a in commandAliasesOf(cmd.name)) '/$a']
        : const <String>[];
    final detail = [
      if (cmd.description.isNotEmpty) cmd.description,
      if (aliases.isNotEmpty) t.helpAlso(aliases.join(', ')),
    ].join(' · ');
    return ListTile(
      key: Key('help-${cmd.name}'),
      dense: true,
      title: Text.rich(
        TextSpan(
          children: [
            TextSpan(
              text: '/${cmd.name}',
              style: const TextStyle(fontWeight: FontWeight.w700),
            ),
            if ((cmd.hint ?? '').isNotEmpty)
              TextSpan(
                text: ' ${cmd.hint}',
                style: TextStyle(color: c.dim),
              ),
          ],
        ),
      ),
      subtitle: detail.isEmpty ? null : Text(detail),
      onTap: () => Navigator.of(context).pop(cmd.name),
      trailing: Tooltip(
        message: t.helpInsert(cmd.name),
        child: ExcludeSemantics(
          child: Text('↵', style: TextStyle(color: c.faint)),
        ),
      ),
    );
  }
}

/// Lists who is banned from a channel; those who may ban lift a ban from
/// it (`/banlist`, ruling R74). The list is the server's, so someone who
/// may not see it is told so in the server's words.
Future<void> showBanListDialog(
  BuildContext context,
  ServerState server,
  String conversationId,
) async {
  final List<ChannelBan> bans;
  try {
    bans = await server.api.listChannelBans(conversationId);
  } on Exception catch (e) {
    if (context.mounted) say(context, refusal(e));
    return;
  }
  if (!context.mounted) return;
  final name = server.conversation(conversationId)?.name ?? '';
  await showScreen<void>(
    context,
    title: L10n.of(context).banListTitle(name.isEmpty ? '' : '#$name'),
    height: 480,
    child: BanList(server: server, conversationId: conversationId, bans: bans),
  );
}

/// The list of /banlist.
class BanList extends StatefulWidget {
  const BanList({
    super.key,
    required this.server,
    required this.conversationId,
    required this.bans,
  });
  final ServerState server;
  final String conversationId;
  final List<ChannelBan> bans;

  @override
  State<BanList> createState() => _BanListState();
}

class _BanListState extends State<BanList> {
  late final bans = [...widget.bans];

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final s = widget.server;
    if (bans.isEmpty) return Center(child: Text(t.cmdNoBans));
    return ListView(
      children: [
        for (final b in bans)
          ListTile(
            key: Key('banlist-${b.accountId}'),
            leading: Avatar(
              account: s.accounts[b.accountId],
              server: s,
              size: 28,
            ),
            title: Text(s.accounts[b.accountId]?.name ?? t.someone),
            subtitle: Text(
              [
                t.bannedBy(
                  s.accounts[b.createdById]?.name ?? t.someoneLower,
                  b.createdAt == null ? '' : shortWhen(b.createdAt!),
                ),
                if (b.expiresAt != null) t.bannedUntil(shortWhen(b.expiresAt!)),
                if (b.reason.isNotEmpty) '“${b.reason}”',
              ].join(' · '),
            ),
            trailing: TextButton(
              key: Key('banlist-lift-${b.accountId}'),
              onPressed: () async {
                try {
                  await s.api.liftChannelBan(
                    widget.conversationId,
                    b.accountId,
                  );
                  if (!context.mounted) return;
                  setState(() => bans.remove(b));
                  say(context, t.banLifted);
                } on Exception catch (e) {
                  if (context.mounted) say(context, refusal(e));
                }
              },
              child: Text(t.liftBan),
            ),
          ),
      ],
    );
  }
}

/// The requests waiting to join a channel, for those who answer them.
Future<List<Json>> loadKnocks(ServerState server, String conversationId) async {
  final j = await server.api.call(Ops.listKnocks, path: {'id': conversationId});
  return [
    for (final k in (j['knocks'] as List? ?? const []))
      if (k is Map) k.cast<String, dynamic>(),
  ];
}

/// Shows who asks to join a channel, each with Add and Decline
/// (ruling R74).
Future<void> showKnocksDialog(
  BuildContext context,
  ServerState server,
  String conversationId,
) async {
  final List<Json> knocks;
  try {
    knocks = await loadKnocks(server, conversationId);
  } on Exception catch (e) {
    if (context.mounted) say(context, refusal(e));
    return;
  }
  if (!context.mounted) return;
  final name = server.conversation(conversationId)?.name ?? '';
  await showScreen<void>(
    context,
    title: L10n.of(context).knocksTitle('#$name'),
    height: 480,
    child: KnockList(
      server: server,
      conversationId: conversationId,
      knocks: knocks,
    ),
  );
}

/// The list of requests to join, each answered in place.
class KnockList extends StatefulWidget {
  const KnockList({
    super.key,
    required this.server,
    required this.conversationId,
    required this.knocks,
  });
  final ServerState server;
  final String conversationId;
  final List<Json> knocks;

  @override
  State<KnockList> createState() => _KnockListState();
}

class _KnockListState extends State<KnockList> {
  late final knocks = [...widget.knocks];

  Future<void> _answer(Json k, bool accept) async {
    final t = L10n.of(context);
    final s = widget.server;
    final who = s.accounts[k['account_id']]?.name ?? t.someone;
    try {
      await s.api.call(
        Ops.answerKnock,
        path: {'id': widget.conversationId, 'knock_id': k['id'] as String},
        body: {'accept': accept},
      );
      if (!mounted) return;
      setState(() => knocks.remove(k));
      say(context, accept ? t.knockAdded(who) : t.knockTurnedAway(who));
    } on Exception catch (e) {
      if (mounted) say(context, refusal(e));
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final s = widget.server;
    if (knocks.isEmpty) return Center(child: Text(t.knocksNone));
    return ListView(
      children: [
        for (final k in knocks)
          ListTile(
            key: Key('knock-${k['account_id']}'),
            leading: Avatar(
              account: s.accounts[k['account_id']],
              server: s,
              size: 28,
            ),
            title: Text(s.accounts[k['account_id']]?.name ?? t.someone),
            subtitle: parseTime(k['created_at']) == null
                ? null
                : Text(shortWhen(parseTime(k['created_at'])!)),
            trailing: Wrap(
              spacing: 8,
              children: [
                TextButton(
                  key: Key('knock-decline-${k['account_id']}'),
                  onPressed: () => _answer(k, false),
                  child: Text(t.knockDecline),
                ),
                FilledButton(
                  key: Key('knock-accept-${k['account_id']}'),
                  onPressed: () => _answer(k, true),
                  child: Text(t.knockAccept),
                ),
              ],
            ),
          ),
      ],
    );
  }
}

/// Keeps count of the requests waiting to join a channel for someone who
/// answers them, from the list and `knock.created` / `knock.answered`.
class KnocksWatcher extends ChangeNotifier {
  KnocksWatcher(this.server, this.conversationId) {
    _sub = server.sync.localEvents.listen((e) {
      if (e['type'] != 'knock.created' && e['type'] != 'knock.answered') {
        return;
      }
      if (object(e['knock'])?['conversation_id'] == conversationId) {
        unawaited(refresh());
      }
    });
    unawaited(refresh());
  }

  final ServerState server;
  final String conversationId;
  StreamSubscription<Json>? _sub;
  bool _disposed = false;

  /// How many wait; 0 also when the person may not see them.
  int count = 0;

  Future<void> refresh() async {
    final conv = server.conversation(conversationId);
    if (conv == null || !conv.isNamed || !conv.private) return;
    try {
      final n = (await loadKnocks(server, conversationId)).length;
      if (_disposed || n == count) return;
      count = n;
      notifyListeners();
    } on Exception {
      // Not a manager, or offline: nothing to show.
    }
  }

  @override
  void dispose() {
    _disposed = true;
    _sub?.cancel();
    super.dispose();
  }
}

/// The bar a channel's managers see while people ask to join it.
class KnocksBar extends StatelessWidget {
  const KnocksBar({super.key, required this.watcher});
  final KnocksWatcher watcher;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    return ListenableBuilder(
      listenable: watcher,
      builder: (context, _) {
        if (watcher.count == 0) return const SizedBox.shrink();
        return Container(
          key: const Key('knocks-bar'),
          color: c.raised,
          padding: const EdgeInsets.fromLTRB(16, 4, 8, 4),
          child: Row(
            children: [
              Expanded(child: Text(t.knocksWaiting(watcher.count))),
              TextButton(
                key: const Key('knocks-review'),
                onPressed: () async {
                  await showKnocksDialog(
                    context,
                    watcher.server,
                    watcher.conversationId,
                  );
                  await watcher.refresh();
                },
                child: Text(t.knocksReview),
              ),
            ],
          ),
        );
      },
    );
  }
}
