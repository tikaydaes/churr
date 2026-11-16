import 'package:flutter/material.dart';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import '../../core/live.dart';
import '../../state/app_prefs.dart';
import '../../state/personal_sidebar.dart';
import '../../state/server_state.dart';
import '../../core/models/personal_layout.dart';
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
import 'sidebar_arrange.dart';
import '../widgets/avatar.dart';

/// The sidebar: where conversations are listed, in whichever of the two
/// layouts the person chose (docs/CLIENTS.md 9.1). Under each server the
/// headings and their order are the person's own, kept by that server
/// (ruling R83); which rows exist and how each is drawn comes from
/// `sidebar_model.dart` and is the same in both.
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
      listenable: Listenable.merge([app.prefs, reorderingServer]),
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
                },
              ),
            ),
            _Bottom(onAddServer: onAddServer, onOpenSettings: onOpenSettings),
          ],
        ),
      ),
    );
  }

  /// One server's headings in the person's order, each with its rows.
  /// Rows and headings are moved by dragging (a mouse drags at once; on a
  /// touch screen in reordering mode, by the handle), by "Move…" in a
  /// row's menu, or with Alt+Up and Alt+Down on the focused one.
  List<Widget> _serverGroups(
    BuildContext context,
    ServerState s, {
    double inset = 0,
  }) {
    final app = AppScope.of(context);
    final reordering = reorderingServer.value == s.localId;
    final out = <Widget>[];
    if (reordering) out.add(_ReorderBar(server: s));
    final groups = buildServerGroups(sidebarInput(app, s));
    final hereOf = {
      for (final g in groups)
        for (final id in g.tops) id: g.key,
    };
    for (final g in groups) {
      final key = '${s.localId}/${g.key}';
      final folded = app.prefs.isCollapsed(key);
      Widget heading(bool highlighted) => _GroupHeading(
        title: g.title,
        folded: folded,
        inset: inset,
        count: g.total,
        sort: g.sort,
        highlighted: highlighted,
        onTap: () => app.prefs.setCollapsed(key, !folded),
        onMenu: (at) => showHeadingMenu(context, s, g, at),
        onMoveBy: g.movable ? (by) => moveHeadingBy(context, s, g.key, by) : null,
      );
      out.add(
        // A row dropped on a heading goes in at its end; a heading dropped
        // on a heading takes its place.
        DragTarget<Object>(
          onWillAcceptWithDetails: (d) => switch (d.data) {
            RowDrag(:final serverId, :final conversationId) =>
              serverId == s.localId &&
                  hereOf[conversationId] != null &&
                  _takes(s, g, conversationId, hereOf, groups),
            HeadingDrag(:final serverId, key: final k) =>
              serverId == s.localId && g.movable && k != g.key,
            _ => false,
          },
          onAcceptWithDetails: (d) => switch (d.data) {
            RowDrag(:final conversationId) => _dropRow(context, s, groups, conversationId, g, g.tops.length),
            HeadingDrag(key: final k) => _dropHeading(context, s, k, g.key),
            _ => null,
          },
          builder: (context, candidates, _) => g.movable && (reordering || _pointerDrags)
              ? Draggable<HeadingDrag>(
                  data: HeadingDrag(s.localId, g.key),
                  feedback: _DragFeedback(g.title),
                  child: heading(candidates.isNotEmpty),
                )
              : heading(candidates.isNotEmpty),
        ),
      );
      if (g.rows.isEmpty && g.own) {
        out.add(
          Padding(
            padding: EdgeInsets.fromLTRB(inset + rowGutter, 0, 12, 6),
            child: Text(
              'Empty. Drag conversations here, or use Move… in a row’s menu.',
              style: TextStyle(fontSize: 12, color: ChurrColors.of(context).faint),
            ),
          ),
        );
      }
      // A folded group still draws its unread and selected rows; the
      // model has already left out the rest.
      for (final r in g.rows) {
        out.add(_row(context, s, r, g, groups, hereOf, reordering));
      }
    }
    return out;
  }

  bool _takes(
    ServerState s,
    SidebarGroup g,
    String id,
    Map<String, String> hereOf,
    List<SidebarGroup> groups,
  ) {
    final from = groups.where((x) => x.key == hereOf[id]).firstOrNull;
    final r = from?.rows.where((x) => x.id == id).firstOrNull;
    return r != null && takesRow(s, g, r, hereOf[id]);
  }

  Future<void> _dropRow(
    BuildContext context,
    ServerState s,
    List<SidebarGroup> groups,
    String id,
    SidebarGroup target,
    int index,
  ) async {
    final from = groups.where((g) => g.tops.contains(id)).firstOrNull;
    final name = from?.rows.where((r) => r.id == id).firstOrNull?.name ?? '';
    if (from?.key == target.key) {
      final at = target.tops.indexOf(id);
      final to = index > at ? index - 1 : index;
      if (to == at) return;
      await sendLayout(context, s, reorderOps(target, id, to, s.layout),
          () => placeText(context, s, id, name));
    } else {
      await sendLayout(context, s, moveOps(id, target, target.key, index, s.layout),
          () => placeText(context, s, id, name));
    }
  }

  Future<void> _dropHeading(BuildContext context, ServerState s, String key, String before) async {
    final keys = [for (final h in channelHeadings(s)) h.$1]..remove(key);
    final index = keys.indexOf(before).clamp(0, keys.length);
    await sendLayout(context, s, [
      {'op': 'move_section', 'key': key, 'index': index},
    ], () => 'Heading moved to ${index + 1} of ${keys.length + 1}');
  }

  /// A mouse or trackpad drags a row at once; a touch screen scrolls
  /// instead, and drags by the handle in reordering mode.
  static bool get _pointerDrags =>
      kIsWeb ||
      defaultTargetPlatform == TargetPlatform.macOS ||
      defaultTargetPlatform == TargetPlatform.windows ||
      defaultTargetPlatform == TargetPlatform.linux;

  Widget _row(
    BuildContext context,
    ServerState s,
    SidebarRow r,
    SidebarGroup g,
    List<SidebarGroup> groups,
    Map<String, String> hereOf,
    bool reordering,
  ) {
    final app = AppScope.read(context);
    final key = '${s.localId}/${r.id}';
    final top = r.indent == 0 && g.tops.contains(r.id);
    Widget tile(bool target) {
      final t = ConversationRowTile(
        server: s,
        row: r,
        dropTarget: target,
        reordering: reordering && top,
        onMoveBy: top ? (by) => moveRowBy(context, s, r.id, r.name, by) : null,
        onToggleCollapsed: () =>
            app.prefs.setCollapsed(key, !app.prefs.isCollapsed(key)),
      );
      return t;
    }

    if (!top) return tile(false);
    final drop = DragTarget<RowDrag>(
      onWillAcceptWithDetails: (d) =>
          d.data.serverId == s.localId &&
          d.data.conversationId != r.id &&
          _takes(s, g, d.data.conversationId, hereOf, groups),
      onAcceptWithDetails: (d) => _dropRow(
        context,
        s,
        groups,
        d.data.conversationId,
        g,
        g.tops.indexOf(r.id),
      ),
      builder: (context, candidates, _) => tile(candidates.isNotEmpty),
    );
    if (reordering || !_pointerDrags) {
      // In reordering mode the handle on the row drags it (see
      // ConversationRowTile); otherwise a touch screen keeps its scroll.
      return reordering
          ? DragHandleScope(data: RowDrag(s.localId, r.id), label: r.name, child: drop)
          : drop;
    }
    return Draggable<RowDrag>(
      data: RowDrag(s.localId, r.id),
      feedback: _DragFeedback(r.name),
      child: drop,
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
    // The server's own count (`presence.counts`), kept for a few seconds.
    // Counting the accounts this device holds went stale when presence
    // came to be sent only for the accounts on screen.
    final online = shown?.onlineCount ?? 0;
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
              // Chat requests waiting for an answer (ruling R78).
              entry(
                ChurrIcons.personAdd,
                L10n.of(context).requests,
                () => openRequests(context),
                count: app.totalRequests,
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

/// What follows the pointer while a row or heading is dragged: a floating
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
/// in the narrow label cut, how many conversations it holds, and its menu
/// (sort, rename, delete, move, reset). Alt+Up and Alt+Down move it.
class _GroupHeading extends StatelessWidget {
  const _GroupHeading({
    required this.title,
    required this.folded,
    this.onTap,
    this.onMenu,
    this.onMoveBy,
    this.inset = 0,
    this.highlighted = false,
    this.count,
    this.sort,
  });
  final String title;
  final bool folded;
  final VoidCallback? onTap;
  final void Function(Offset at)? onMenu;
  final void Function(int by)? onMoveBy;
  final double inset;

  /// A row or heading is being dragged over it: a dashed drop target.
  final bool highlighted;
  final int? count;
  final SortMode? sort;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final heading = Semantics(
      header: true,
      expanded: onTap == null ? null : !folded,
      hint: sort == null ? null : 'Sorted ${sortNames[sort]!.toLowerCase()}',
      child: InkWell(
        key: Key('heading-$title'),
        onTap: onTap,
        child: Padding(
          padding: EdgeInsets.fromLTRB(inset, 12, 4, 4),
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
              if (onMenu != null)
                Builder(
                  builder: (context) => IconButton(
                    key: Key('heading-menu-$title'),
                    tooltip: 'Menu for the heading $title',
                    visualDensity: VisualDensity.compact,
                    icon: Glyph(ChurrIcons.moreH, size: 14, color: c.dim),
                    onPressed: () {
                      final box = context.findRenderObject() as RenderBox?;
                      onMenu!(box?.localToGlobal(Offset(0, box.size.height)) ?? Offset.zero);
                    },
                  ),
                ),
            ],
          ),
        ),
      ),
    );
    final framed = highlighted ? DashedBox(color: c.lavender, child: heading) : heading;
    if (onMoveBy == null) return framed;
    return CallbackShortcuts(
      bindings: {
        const SingleActivator(LogicalKeyboardKey.arrowUp, alt: true): () => onMoveBy!(-1),
        const SingleActivator(LogicalKeyboardKey.arrowDown, alt: true): () => onMoveBy!(1),
      },
      child: framed,
    );
  }
}

/// The bar at the top of a server's list while it is being reordered.
class _ReorderBar extends StatelessWidget {
  const _ReorderBar({required this.server});
  final ServerState server;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Container(
      color: c.raised,
      padding: const EdgeInsets.fromLTRB(14, 6, 8, 6),
      child: Row(
        children: [
          const Expanded(
            child: Text(
              'Reordering: drag a row by its handle, or use the arrows.',
              style: TextStyle(fontSize: 13),
            ),
          ),
          FilledButton(
            key: const Key('reorder-done'),
            onPressed: () => reorderingServer.value = null,
            child: const Text('Done'),
          ),
        ],
      ),
    );
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
