import 'package:flutter/material.dart';

import '../../core/ids.dart';
import '../../core/live.dart';
import '../../state/app_prefs.dart';
import '../../state/app_state.dart';
import '../../state/hybrid_order.dart';
import '../../state/server_state.dart';
import '../../state/sidebar_model.dart';
import '../scopes.dart';
import '../../l10n/app_localizations.dart';
import '../screens/account/sign_in_screens.dart';
import '../screens/channels/browse_screen.dart';
import '../screens/channels/new_channel_dialog.dart';
import '../screens/search/quick_switcher_dialog.dart';
import '../screens/search/search_screen.dart';
import '../theme/tokens.dart';
import '../theme/churr_icons.dart';
import '../theme/shapes.dart';
import '../widgets/controls.dart';
import '../widgets/reconnect_countdown.dart';
import 'account_entries.dart';
import 'conversation_row.dart';
import '../widgets/avatar.dart';

/// The sidebar: where conversations are listed, in whichever of the
/// three layouts the person chose (docs/CLIENTS.md 9.1). The layouts
/// differ only in grouping and order; which rows exist and how each is
/// drawn comes from `sidebar_model.dart` and is the same in all three.
class Sidebar extends StatelessWidget {
  const Sidebar({
    super.key,
    required this.onAddServer,
    required this.onOpenSettings,
  });
  final VoidCallback onAddServer;
  final VoidCallback onOpenSettings;

  @override
  Widget build(BuildContext context) {
    final app = AppScope.of(context);
    final c = ChurrColors.of(context);
    final layout = app.prefs.layout;
    final selected = app.selectedServer;
    return ListenableBuilder(
      listenable: app.prefs,
      builder: (context, _) => Container(
        color: c.panel,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            _Top(
              title: layout == SidebarLayout.rail
                  ? (selected?.name ?? 'Churr')
                  : 'Churr',
              server: layout == SidebarLayout.rail ? selected : null,
            ),
            Expanded(
              child: ListView(
                key: const Key('sidebar-list'),
                padding: const EdgeInsets.only(bottom: 8),
                children: switch (layout) {
                  SidebarLayout.rail =>
                    selected == null
                        ? const []
                        : _serverGroups(context, selected),
                  SidebarLayout.merged => [
                    for (final s in app.servers) ..._mergedServer(context, s),
                  ],
                  SidebarLayout.hybrid => _hybrid(context),
                },
              ),
            ),
            _Bottom(onAddServer: onAddServer, onOpenSettings: onOpenSettings),
          ],
        ),
      ),
    );
  }

  SidebarInput _input(AppState app, ServerState s) => SidebarInput(
    conversations: [for (final r in s.conversations) r.conversation],
    sections: s.sections,
    outsideParents: s.outsideParents,
    accounts: s.accounts,
    myId: s.me?.id,
    defaultShow: app.prefs.defaultShow,
    showHidden: app.prefs.showHidden,
    openConversationId: app.open?.serverId == s.localId
        ? app.open!.conversationId
        : null,
    collapsedParents: {
      for (final k in app.prefs.collapsed)
        if (k.startsWith('${s.localId}/')) k.substring(s.localId.length + 1),
    },
  );

  /// One server's groups: Starred, the sections, Channels, Chats, Sealed.
  List<Widget> _serverGroups(
    BuildContext context,
    ServerState s, {
    double inset = 0,
  }) {
    final app = AppScope.of(context);
    final out = <Widget>[];
    for (final g in buildServerGroups(_input(app, s))) {
      final key = '${s.localId}/${g.key}';
      final folded = app.prefs.isCollapsed(key);
      out.add(
        _GroupHeading(
          title: g.title,
          folded: folded,
          inset: inset,
          count: g.kind == GroupKind.starred ? null : g.total,
          onTap: () => app.prefs.setCollapsed(key, !folded),
        ),
      );
      // A folded group still draws its unread and selected rows; the
      // model has already left out the rest.
      for (final r in g.rows) {
        out.add(_row(context, s, r));
      }
    }
    return out;
  }

  Widget _row(
    BuildContext context,
    ServerState s,
    SidebarRow r, {
    bool mark = false,
  }) {
    final app = AppScope.read(context);
    final key = '${s.localId}/${r.id}';
    final tile = ConversationRowTile(
      server: s,
      row: r,
      showServerMark: mark,
      onToggleCollapsed: () =>
          app.prefs.setCollapsed(key, !app.prefs.isCollapsed(key)),
    );
    if (app.prefs.layout != SidebarLayout.hybrid) return tile;
    // Hybrid: a row can be picked up and dropped on a group's heading.
    return LongPressDraggable<(String, String)>(
      data: (s.entry.address, r.id),
      feedback: _DragFeedback(r.name),
      child: tile,
    );
  }

  /// Merged tree: each server is a heading that folds, with its groups
  /// under it.
  List<Widget> _mergedServer(BuildContext context, ServerState s) {
    final app = AppScope.of(context);
    final key = '${s.localId}/server';
    final folded = app.prefs.isCollapsed(key);
    return [
      // Servers are in the person's order, changed by dragging a heading;
      // its menu is the server's menu.
      ServerReorder(
        server: s,
        child: _ServerHeading(
          server: s,
          folded: folded,
          onTap: () => app.prefs.setCollapsed(key, !folded),
        ),
      ),
      if (!folded) ..._serverGroups(context, s, inset: 8),
    ];
  }

  /// Hybrid: the person's own groups, holding conversations from any
  /// server, then "Everything else" with what they have not placed,
  /// listed by server as in the rail. The admin's sections are not drawn
  /// inside the person's groups: the groups take their place.
  List<Widget> _hybrid(BuildContext context) {
    final app = AppScope.of(context);
    final prefs = app.prefs;
    final placed = {
      for (final p in prefs.hybridPlacements)
        '${p.serverAddress}|${p.conversationId}': p.groupId,
    };
    final rowsByServer = {
      for (final s in app.servers)
        s: [for (final g in buildServerGroups(_input(app, s))) ...g.rows],
    };
    final out = <Widget>[];
    for (final g in prefs.hybridGroups) {
      // In the person's order within the group.
      final rows =
          [
            for (final e in rowsByServer.entries)
              for (final r in e.value)
                if (placed['${e.key.entry.address}|${r.id}'] == g.id)
                  (e.key, r),
          ]..sort(
            (a, b) => hybridPosition(
              prefs,
              a.$1.entry.address,
              a.$2.id,
            ).compareTo(hybridPosition(prefs, b.$1.entry.address, b.$2.id)),
          );
      // A group with nothing to show has no heading, except while "Show
      // hidden" is on, so an empty group can still be dragged into.
      if (rows.isEmpty && !prefs.showHidden) continue;
      final key = 'hybrid/${g.id}';
      final folded = prefs.isCollapsed(key);
      final groupIndex = prefs.hybridGroups.indexOf(g);
      out.add(
        // A row dropped on the heading goes to the top of the group; a
        // heading dropped here moves that group to this place.
        DragTarget<Object>(
          onWillAcceptWithDetails: (d) =>
              d.data is (String, String) ||
              (d.data is GroupDrag && (d.data as GroupDrag).groupId != g.id),
          onAcceptWithDetails: (d) {
            final data = d.data;
            if (data is (String, String)) moveInHybrid(app, data.$1, data.$2, g.id, 0);
            if (data is GroupDrag) moveHybridGroup(app, data.groupId, groupIndex);
          },
          builder: (context, candidates, _) => LongPressDraggable<GroupDrag>(
            data: GroupDrag(g.id),
            feedback: _DragFeedback(g.name),
            child: _GroupHeading(
              title: g.name,
              count: rows.length,
              folded: folded,
              highlighted: candidates.isNotEmpty,
              onTap: () => prefs.setCollapsed(key, !folded),
              onDelete: () => prefs.deleteHybridGroup(g.id),
              // Keyboard and screen readers move groups from the menu.
              onMoveUp: groupIndex > 0
                  ? () => moveHybridGroup(app, g.id, groupIndex - 1)
                  : null,
              onMoveDown: groupIndex < prefs.hybridGroups.length - 1
                  ? () => moveHybridGroup(app, g.id, groupIndex + 1)
                  : null,
            ),
          ),
        ),
      );
      final open = app.open;
      for (var i = 0; i < rows.length; i++) {
        final (s, r) = rows[i];
        // Folded: only unread and selected rows stay.
        if (folded && !r.keepsWhenFolded(open?.serverId == s.localId ? open!.conversationId : null,)) continue;
        // A row dropped on a row takes its place in the group.
        out.add(
          DragTarget<(String, String)>(
            onWillAcceptWithDetails: (d) => d.data != (s.entry.address, r.id),
            onAcceptWithDetails: (d) =>
                moveInHybrid(app, d.data.$1, d.data.$2, g.id, i),
            // A drop target is provisional: dashed.
            builder: (context, candidates, _) => candidates.isEmpty
                ? _row(context, s, _flat(s, r), mark: true)
                : DashedBox(
                    color: ChurrColors.of(context).lavender,
                    child: _row(context, s, _flat(s, r), mark: true),
                  ),
          ),
        );
      }
    }
    out.add(
      DragTarget<(String, String)>(
        onAcceptWithDetails: (d) => prefs.place(d.data.$1, d.data.$2, null),
        builder: (context, candidates, _) => _GroupHeading(
          title: L10n.of(context).everythingElse,
          folded: prefs.isCollapsed('hybrid/else'),
          highlighted: candidates.isNotEmpty,
          onTap: () => prefs.setCollapsed(
            'hybrid/else',
            !prefs.isCollapsed('hybrid/else'),
          ),
        ),
      ),
    );
    {
      final elseFolded = prefs.isCollapsed('hybrid/else');
      for (final e in rowsByServer.entries) {
        var rest = [
          for (final r in e.value)
            if (!placed.containsKey('${e.key.entry.address}|${r.id}')) r,
        ];
        if (elseFolded) {
          final open = app.open;
          rest = foldRows(
            rest,
            openId: open?.serverId == e.key.localId
                ? open!.conversationId
                : null,
          );
        }
        if (rest.isEmpty) continue;
        if (app.servers.length > 1) {
          out.add(_GroupHeading(title: e.key.name, folded: false, inset: 8));
        }
        for (final r in rest) {
          out.add(_row(context, e.key, r, mark: true));
        }
      }
    }
    out.add(
      Padding(
        padding: const EdgeInsets.fromLTRB(rowGutter, 12, 12, 0),
        child: OutlinedButton.icon(
          key: const Key('new-group'),
          icon: const Glyph(ChurrIcons.add, size: 16),
          label: Text(L10n.of(context).newGroup),
          onPressed: () => _newGroup(context),
        ),
      ),
    );
    return out;
  }

  /// In a person's group a sub-channel has no tree to sit in, so it is
  /// drawn at the top level and names its parent: `parent › name`.
  SidebarRow _flat(ServerState s, SidebarRow r) {
    if (r.indent == 0) return r;
    final parentId = r.conversation.parentId;
    final parent = parentId == null ? null : s.conversation(parentId)?.name;
    return r.copyWith(indent: 0, parentName: parent);
  }

  Future<void> _newGroup(BuildContext context) async {
    final prefs = AppScope.read(context).prefs;
    final name = TextEditingController();
    final ok = await showDialog<bool>(
  animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('New group'),
        content: TextField(
          controller: name,
          autofocus: true,
          decoration: const InputDecoration(labelText: 'Name'),
          onSubmitted: (_) => Navigator.pop(context, true),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Create'),
          ),
        ],
      ),
    );
    if (ok == true && name.text.trim().isNotEmpty) {
      await prefs.addHybridGroup(newUuid(), name.text.trim());
    }
  }
}

/// The top of the sidebar, the same in every layout: the server's name in
/// the narrow heavy cut with the connection's state under it, search and
/// the quick switcher beside it, then Notifications, Threads and
/// Bookmarks as rows whose marks line up with the conversations'.
class _Top extends StatelessWidget {
  const _Top({required this.title, this.server});
  final String title;

  /// The one server shown (rail layout), or null for all of them.
  final ServerState? server;

  @override
  Widget build(BuildContext context) {
    final app = AppScope.of(context);
    final c = ChurrColors.of(context);
    final states = server == null
        ? [for (final s in app.servers) s.liveState]
        : [server!.liveState];
    final catchingUp = states.any(
      (s) => s == LiveState.catchingUp || s == LiveState.connecting,
    );
    final offline = states.any((s) => s == LiveState.offline);
    final signedOut = states.any((s) => s == LiveState.signedOut);
    final status = signedOut
        ? 'Signed out'
        : catchingUp
        ? 'Catching up'
        : offline
        ? 'Not connected'
        : null;
    final shown = server ?? app.selectedServer;
    final online = shown == null
        ? 0
        : shown.accounts.values.where((a) => a.presence == 'online').length;
    Widget statusLine(String? status) => Text(
      status?.toLowerCase() ??
          'connected${online > 0 ? ' · $online online' : ''}',
      key: const Key('connection-status'),
      maxLines: 1,
      overflow: TextOverflow.ellipsis,
      style: monoStyle(
        context,
        size: 10.5,
        color: status == null ? c.dim : c.yellowInk,
      ),
    );
    Widget entry(
      ChurrGlyph icon,
      String label,
      VoidCallback onTap, {
      int count = 0,
    }) => InkWell(
      key: Key('entry-$label'),
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(rowGutter, 5, 12, 5),
        child: Row(
          children: [
            SizedBox(width: 14, child: Glyph(icon, size: 14, color: c.faint)),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                label,
                style: TextStyle(
                  fontSize: 14,
                  height: 1.3,
                  color: count > 0 ? c.ink : c.dim,
                  fontWeight: count > 0 ? FontWeight.w700 : FontWeight.w500,
                ),
              ),
            ),
            if (count > 0) PriceTag(count, label: '$count unread', size: 12),
          ],
        ),
      ),
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        // Servers that signed this device out, each with "Sign in again".
        const SignedOutBanners(),
        Padding(
          padding: const EdgeInsets.fromLTRB(14, 14, 4, 6),
          child: Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      title.toUpperCase(),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: archivo(
                        size: 20,
                        weight: 900,
                        width: 78,
                        color: c.ink,
                        height: 1,
                      ),
                    ),
                    const SizedBox(height: 5),
                    Row(
                      children: [
                        PresenceLight(
                          status == null
                              ? 'online'
                              : (signedOut || offline ? 'offline' : 'away'),
                        ),
                        const SizedBox(width: 8),
                        Expanded(
                          // While waiting to try again the line counts
                          // down the wait the connection chose; with
                          // several servers, the soonest.
                          child: status != null && !signedOut && !catchingUp
                              ? ReconnectCountdown(
                                  retryAt: () => ReconnectCountdown.soonest(
                                    server == null
                                        ? app.servers.map((s) => s.live)
                                        : [server!.live],
                                  ),
                                  builder: (context, seconds) => statusLine(
                                    seconds == null
                                        ? status
                                        : L10n.of(
                                            context,
                                          ).reconnectingIn(seconds),
                                  ),
                                )
                              : statusLine(status),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
              IconButton(
                tooltip: 'Search',
                icon: const Glyph(ChurrIcons.search),
                onPressed: () => openSearch(context, server: server),
              ),
              IconButton(
                tooltip: 'Quick switcher',
                icon: const Glyph(ChurrIcons.bolt),
                onPressed: () => openQuickSwitcher(context),
              ),
            ],
          ),
        ),
        Padding(
          padding: const EdgeInsets.only(top: 2, bottom: 4),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              // The count is the sum of the servers' badges, whether or
              // not the conversations behind it have rows.
              entry(
                ChurrIcons.notify,
                L10n.of(context).notifications,
                () => openNotifications(context),
                count: app.totalBadge,
              ),
              entry(
                ChurrIcons.thread,
                L10n.of(context).threads,
                () => openThreads(context, serverId: server?.localId),
              ),
              entry(
                ChurrIcons.bookmark,
                L10n.of(context).bookmarks,
                () => openBookmarks(context),
              ),
            ],
          ),
        ),
      ],
    );
  }
}

/// The foot of the sidebar: "Show hidden" with its switch, the tools
/// (Browse channels, New…, Add a server, App settings), and the person's
/// own block with their status and do not disturb.
class _Bottom extends StatelessWidget {
  const _Bottom({required this.onAddServer, required this.onOpenSettings});
  final VoidCallback onAddServer;
  final VoidCallback onOpenSettings;

  @override
  Widget build(BuildContext context) {
    final app = AppScope.of(context);
    final c = ChurrColors.of(context);
    final rail = app.prefs.layout == SidebarLayout.rail;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        InkWell(
          key: const Key('show-hidden'),
          onTap: () => app.prefs.showHidden = !app.prefs.showHidden,
          child: Semantics(
            toggled: app.prefs.showHidden,
            label: 'Show hidden',
            excludeSemantics: true,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(14, 0, 8, 0),
              child: Row(
                children: [
                  ExcludeFocus(
                    child: ChurrSwitch(value: app.prefs.showHidden, onChanged: (v) => app.prefs.showHidden = v),
                  ),
                  const SizedBox(width: 8),
                  Text('Show hidden', style: TextStyle(fontSize: 13, color: c.dim)),
                ],
              ),
            ),
          ),
        ),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 6),
          child: Row(
            children: [
              IconButton(
                tooltip: 'Browse channels',
                icon: const Glyph(ChurrIcons.compass),
                onPressed: () => openBrowse(context, server: rail ? app.selectedServer : null),
              ),
              IconButton(
                tooltip: 'New…',
                icon: const Glyph(ChurrIcons.newChat),
                onPressed: () => openNewMenu(context, server: rail ? app.selectedServer : null),
              ),
              const Spacer(),
              if (!rail)
                IconButton(
                  tooltip: 'Add a server',
                  icon: const Glyph(ChurrIcons.add),
                  onPressed: onAddServer,
                ),
              IconButton(
                key: const Key('open-settings'),
                tooltip: 'App settings',
                icon: const Glyph(ChurrIcons.settings),
                onPressed: onOpenSettings,
              ),
            ],
          ),
        ),
        // The person's own status, and do not disturb one tap away.
        AccountStrip(server: rail ? app.selectedServer : null),
      ],
    );
  }
}

/// What follows the pointer while a row or group is dragged: a floating
/// thing, so it wears the hard offset block.
class _DragFeedback extends StatelessWidget {
  const _DragFeedback(this.text);
  final String text;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Material(
      type: MaterialType.transparency,
      child: Container(
        decoration: floatingBox(c),
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
        child: Text(
          text,
          style: TextStyle(
            color: c.ink,
            fontSize: 14,
            fontWeight: FontWeight.w600,
          ),
        ),
      ),
    );
  }
}

/// A group's heading: the whole width folds it. A drawn chevron, the name
/// in the narrow label cut, and how many conversations it holds.
class _GroupHeading extends StatelessWidget {
  const _GroupHeading({
    required this.title,
    required this.folded,
    this.onTap,
    this.onDelete,
    this.inset = 0,
    this.highlighted = false,
    this.onMoveUp,
    this.onMoveDown,
    this.count,
  });
  final String title;
  final bool folded;
  final VoidCallback? onTap;
  final VoidCallback? onDelete;

  /// Moving a hybrid group without dragging (keyboard, screen reader).
  final VoidCallback? onMoveUp;
  final VoidCallback? onMoveDown;
  final double inset;

  /// A row or group is being dragged over it: a dashed drop target.
  final bool highlighted;
  final int? count;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    Widget small(ChurrGlyph g, String label, VoidCallback onTap) => InkWell(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.all(6),
        child: Glyph(g, size: 14, color: c.dim, semanticLabel: label),
      ),
    );
    final heading = Semantics(
      header: true,
      expanded: onTap == null ? null : !folded,
      child: InkWell(
        onTap: onTap,
        child: Padding(
          padding: EdgeInsets.fromLTRB(inset, 12, 8, 4),
          child: Row(
            children: [
              SizedBox(
                width: rowGutter,
                height: 24,
                child: onTap == null
                    ? null
                    : Center(
                        child: ChevronMark(open: !folded, color: c.dim),
                      ),
              ),
              Expanded(
                child: Text(
                  title.toUpperCase(),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: ChurrType.label(c.faint),
                ),
              ),
              if (count != null && count! > 0)
                Padding(
                  padding: const EdgeInsets.only(left: 6, right: 4),
                  child: Text(
                    '$count',
                    style: monoStyle(context, size: 10, color: c.faint),
                  ),
                ),
              if (onMoveUp != null)
                small(
                  ChurrIcons.arrowUp,
                  L10n.of(context).moveGroupUp,
                  onMoveUp!,
                ),
              if (onMoveDown != null)
                small(
                  ChurrIcons.arrowDown,
                  L10n.of(context).moveGroupDown,
                  onMoveDown!,
                ),
              if (onDelete != null)
                small(
                  ChurrIcons.close,
                  L10n.of(context).deleteGroup(title),
                  onDelete!,
                ),
            ],
          ),
        ),
      ),
    );
    return highlighted ? DashedBox(color: c.lavender, child: heading) : heading;
  }
}

/// A server's heading in the merged tree: on ground, a fill step down
/// from the panel, with its tile, name, badge, and a mark if it is
/// disconnected or muted.
class _ServerHeading extends StatelessWidget {
  const _ServerHeading({
    required this.server,
    required this.folded,
    required this.onTap,
  });
  final ServerState server;
  final bool folded;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final muted = serverMuted(context, server);
    return InkWell(
      key: Key('server-heading-${server.localId}'),
      onTap: onTap,
      child: Container(
        color: c.bg,
        padding: const EdgeInsets.fromLTRB(0, 6, 4, 6),
        child: Row(
          children: [
            SizedBox(
              width: rowGutter,
              child: Center(
                child: ChevronMark(open: !folded, color: c.dim),
              ),
            ),
            InitialsTile(
              name: server.name,
              id: server.localId,
              size: 24,
              dimmed: muted,
            ),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                server.name.toUpperCase(),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                // A muted server is drawn dimmed in every layout.
                style: archivo(
                  size: 15,
                  weight: 900,
                  width: 78,
                  color: muted ? c.faint : c.ink,
                ),
              ),
            ),
            if (server.liveState != LiveState.ready)
              Padding(
                padding: const EdgeInsets.only(right: 6),
                child: Glyph(
                  ChurrIcons.unplugged,
                  size: 14,
                  color: c.dim,
                  semanticLabel: L10n.of(context).notConnected,
                ),
              ),
            if (muted)
              Padding(
                padding: const EdgeInsets.only(right: 6),
                child: Glyph(
                  ChurrIcons.notifyOff,
                  size: 14,
                  color: c.dim,
                  semanticLabel: L10n.of(context).muted,
                ),
              ),
            if (server.summary.badge > 0)
              PriceTag(
                server.summary.badge,
                label: '${server.summary.badge} notifications',
                size: 12,
              ),
            IconButton(
              tooltip: L10n.of(context).serverSettings,
              icon: Glyph(ChurrIcons.moreH, size: 16, color: c.dim),
              onPressed: () {
                final box = context.findRenderObject() as RenderBox?;
                showServerMenu(
                  context,
                  server,
                  box?.localToGlobal(Offset(box.size.width, 0)) ?? Offset.zero,
                );
              },
            ),
          ],
        ),
      ),
    );
  }
}
