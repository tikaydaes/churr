import 'dart:async';

import '../../../l10n/app_localizations.dart';

import 'package:flutter/material.dart';

import '../conversation/command_dialogs.dart' show knockOn;
import '../hubs/hub_screens.dart' show openAskToJoinHubChannel;

import '../../../core/api/channels.dart';
import '../../../core/models/conversation.dart';
import '../../../state/app_state.dart';
import '../../../state/channel_actions.dart';
import '../../../state/channel_detail.dart';
import '../../../state/server_state.dart';
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import '../../theme/churr_icons.dart';
import 'common.dart';
import 'groups_screen.dart';
import 'subchannel_offer.dart';

/// Opens "Browse channels" for one server, asking which first when
/// several are signed in.
Future<void> openBrowse(BuildContext context, {ServerState? server}) async {
  final app = AppScope.read(context);
  final s = await pickServer(context, app, preferred: server);
  if (s == null || !context.mounted) return;
  await showScreen<void>(
    context,
    title: L10n.of(context).browseTitle(s.name),
    child: BrowseScreen(app: app, server: s),
  );
}

/// Orders channels as the browser lists them (docs/CLIENTS.md 9.26):
/// a tree of parents with their sub-channels under them; by name without
/// regard to case; channels of the same name together, those the person
/// is in first, then oldest first.
List<(Conversation, int)> browseTree(List<Conversation> all) {
  int order(Conversation a, Conversation b) {
    final n = foldName(a.name ?? '').compareTo(foldName(b.name ?? ''));
    if (n != 0) return n;
    final inA = a.membership != null, inB = b.membership != null;
    if (inA != inB) return inA ? -1 : 1;
    return (a.createdAt ?? DateTime(0)).compareTo(b.createdAt ?? DateTime(0));
  }

  final ids = {for (final c in all) c.id};
  // A sub-channel whose parent is not in the list is drawn at the top.
  final tops = [
    for (final c in all)
      if (c.parentId == null || !ids.contains(c.parentId)) c,
  ]..sort(order);
  final out = <(Conversation, int)>[];
  for (final t in tops) {
    out.add((t, 0));
    final subs = [
      for (final c in all)
        if (c.parentId == t.id) c,
    ]..sort(order);
    for (final s in subs) {
      out.add((s, 1));
    }
  }
  return out;
}

/// The channel browser: every public channel of the server, the
/// person's own, and the private ones they may join without an
/// invitation (ruling R103), as a tree, with the detail line under each
/// and Join, Ask to join or Open.
class BrowseScreen extends StatefulWidget {
  const BrowseScreen({super.key, required this.app, required this.server});
  final AppState app;
  final ServerState server;

  @override
  State<BrowseScreen> createState() => _BrowseScreenState();
}

class _BrowseScreenState extends State<BrowseScreen> {
  List<Conversation>? channels;
  final Map<String, List<String>> managers = {};
  String? error;
  String q = '';
  bool archived = false;
  Timer? _debounce;
  final busy = <String>{};

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _debounce?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final v = await widget.server.api.browseChannels(
        q: q,
        archived: archived,
      );
      if (!mounted) return;
      setState(() {
        channels = v;
        error = null;
      });
      // The contract gives no managers with a channel; when a server
      // sends `manager_ids` they are shown.
      for (final c in v) {
        final m = managerIdsOf(c);
        if (m.isNotEmpty) managers[c.id] = m;
      }
    } on Exception catch (e) {
      if (mounted) setState(() => error = refusal(e));
    }
  }

  Future<void> _join(Conversation c) async {
    setState(() => busy.add(c.id));
    try {
      final joined = await ChannelActions(widget.server).join(c.id);
      if (!mounted) return;
      await _load();
      if (!mounted) return;
      await offerSubChannels(context, widget.server, joined);
    } on Exception catch (e) {
      if (mounted) say(context, refusal(e));
    } finally {
      if (mounted) setState(() => busy.remove(c.id));
    }
  }

  void _open(Conversation c) {
    Navigator.of(context).maybePop();
    widget.app.openConversation(OpenTarget(widget.server.localId, c.id));
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final list = channels;
    final tree = list == null
        ? const <(Conversation, int)>[]
        : browseTree(list);
    final offered = {
      for (final o in widget.server.channelOffers) o.conversationId,
    };
    final names = <String, int>{};
    for (final (ch, _) in tree) {
      names.update(foldName(ch.name ?? ''), (n) => n + 1, ifAbsent: () => 1);
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
          child: TextField(
            key: const Key('browse-filter'),
            decoration: InputDecoration(
              hintText: L10n.of(context).browseFilterHint,
              prefixIcon: Glyph(ChurrIcons.search),
            ),
            onChanged: (v) {
              q = v;
              _debounce?.cancel();
              _debounce = Timer(const Duration(milliseconds: 300), _load);
            },
          ),
        ),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 8),
          child: Row(
            children: [
              Expanded(
                child: CheckboxListTile(
                  dense: true,
                  controlAffinity: ListTileControlAffinity.leading,
                  title: Text(L10n.of(context).browseIncludeArchived),
                  value: archived,
                  onChanged: (v) {
                    setState(() => archived = v ?? false);
                    _load();
                  },
                ),
              ),
              // Open groups are joined from here too (docs/CLIENTS.md 9.15).
              TextButton.icon(
                key: const Key('browse-groups'),
                icon: const Glyph(ChurrIcons.people, size: 16),
                onPressed: () => openGroups(context, widget.server),
                label: Text(L10n.of(context).groupsTitle),
              ),
            ],
          ),
        ),
        // A private channel on a hub is never listed: someone in it gives
        // its join code, entered here.
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 8),
          child: Align(
            alignment: AlignmentDirectional.centerStart,
            child: TextButton.icon(
              key: const Key('browse-hub-ask'),
              icon: const Glyph(ChurrIcons.globe, size: 16),
              onPressed: () => openAskToJoinHubChannel(context, widget.server),
              label: Text(L10n.of(context).askHubJoin),
            ),
          ),
        ),
        if (error != null)
          Padding(
            padding: const EdgeInsets.all(16),
            child: Text(error!, style: TextStyle(color: c.pink)),
          ),
        Expanded(
          child: list == null && error == null
              ? const Center(child: Busy())
              : tree.isEmpty
              ? Center(
                  child: Text(
                    L10n.of(context).browseNone,
                    style: TextStyle(color: c.dim),
                  ),
                )
              : ListView.builder(
                  key: const Key('browse-list'),
                  itemCount: tree.length,
                  itemBuilder: (context, i) {
                    final (ch, depth) = tree[i];
                    final joined = ch.membership != null;
                    // A channel offered through a group or as a default
                    // channel (R103) is joined, never asked to join; a
                    // private one is listed to a non-member only then.
                    final mayJoin = offered.contains(ch.id) || ch.joinable;
                    final same = (names[foldName(ch.name ?? '')] ?? 0) > 1;
                    final detail = channelDetail(
                      widget.server,
                      ch,
                      showServer: widget.app.servers.length > 1,
                      managerIds: managers[ch.id] ?? const [],
                    );
                    return Padding(
                      padding: EdgeInsets.only(left: 8.0 + depth * 24),
                      child: ListTile(
                        key: Key('browse-${ch.id}'),
                        leading: Glyph(
                          channelIcon(true, ch.private, ch.isLocked),
                          size: 18,
                          color: c.dim,
                        ),
                        title: Row(
                          children: [
                            Flexible(
                              child: Text(
                                ch.name ?? '',
                                overflow: TextOverflow.ellipsis,
                              ),
                            ),
                            if (ch.isArchived) ...[
                              const SizedBox(width: 6),
                              Text(
                                L10n.of(context).archivedTag,
                                style: monoStyle(context),
                              ),
                            ],
                            if (same) ...[
                              const SizedBox(width: 6),
                              Glyph(
                                ChurrIcons.copy,
                                size: 11,
                                color: c.yellowInk,
                                semanticLabel: L10n.of(context)
                                    .sameNameAsAnother,
                              ),
                            ],
                          ],
                        ),
                        subtitle: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            if (ch.topic.isNotEmpty)
                              Text(
                                ch.topic,
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                              ),
                            Text(
                              detail,
                              style: TextStyle(fontSize: 11, color: c.dim),
                            ),
                          ],
                        ),
                        isThreeLine: ch.topic.isNotEmpty,
                        trailing: joined
                            ? TextButton(
                                onPressed: () => _open(ch),
                                child: Text(L10n.of(context).open),
                              )
                            // A knock channel is asked to join (R79).
                            : ch.isKnock && !mayJoin
                            ? FilledButton(
                                key: Key('knock-${ch.id}'),
                                onPressed: busy.contains(ch.id)
                                    ? null
                                    : () => knockOn(context, widget.server, ch),
                                child: Text(L10n.of(context).askToJoin),
                              )
                            : FilledButton(
                                key: Key('join-${ch.id}'),
                                onPressed:
                                    busy.contains(ch.id) ||
                                        ch.isLocked ||
                                        ch.isArchived
                                    ? null
                                    : () => _join(ch),
                                child: Text(L10n.of(context).join),
                              ),
                        onTap: joined ? () => _open(ch) : null,
                      ),
                    );
                  },
                ),
        ),
      ],
    );
  }
}

/// Lists members with the manager level of one channel, for the
/// "managed by" part of the detail line where a screen has a channel's
/// members at hand.
List<String> managersFrom(List<Member> members) => [
  for (final m in members)
    if (m.level == 'manager') m.accountId,
];
