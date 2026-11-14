import 'package:flutter/material.dart';

import 'account_entries.dart';

import '../../core/live.dart';
import '../../state/server_state.dart';
import '../scopes.dart';
import '../theme/tokens.dart';
import '../theme/churr_icons.dart';
import '../widgets/avatar.dart';
import '../widgets/badge.dart';

/// One notched tile for a server: its initials on a fill picked from its
/// id, its badge, and a mark when it is not connected.
class ServerTile extends StatelessWidget {
  const ServerTile({
    super.key,
    required this.server,
    this.selected = false,
    this.size = 44,
    this.onTap,
  });

  final ServerState server;
  final bool selected;
  final double size;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final offline = server.liveState != LiveState.ready;
    final badge = server.summary.badge;
    return Semantics(
      button: true,
      selected: selected,
      label:
          '${server.name}${badge > 0 ? ', $badge notifications' : ''}'
          '${offline ? ', not connected' : ''}',
      excludeSemantics: true,
      child: Tooltip(
        message: server.name,
        child: InkWell(
          onTap: onTap,
          child: SizedBox(
            width: size + 12,
            height: size + 8,
            child: Stack(
              alignment: Alignment.center,
              children: [
                // The selected server: a cyan bar on the left edge, as
                // the selected row has.
                if (selected)
                  Positioned(
                    left: 0,
                    top: 4,
                    bottom: 4,
                    child: Container(width: 4, color: c.cyan),
                  ),
                // A muted server is drawn dimmed in every layout.
                InitialsTile(
                  name: server.name,
                  id: server.localId,
                  size: size,
                  dimmed: serverMuted(context, server),
                ),
                if (badge > 0)
                  Positioned(top: 0, right: 0, child: CountBadge(badge)),
                if (offline)
                  Positioned(
                    bottom: 2,
                    left: 4,
                    child: Container(
                      color: c.bg,
                      padding: const EdgeInsets.all(1),
                      // Signed out by the server has its own mark.
                      child: Glyph(
                        server.liveState == LiveState.signedOut
                            ? ChurrIcons.clock
                            : ChurrIcons.unplugged,
                        size: 12,
                        color: server.liveState == LiveState.signedOut
                            ? c.yellowInk
                            : c.dim,
                      ),
                    ),
                  ),
                if (serverMuted(context, server))
                  Positioned(
                    bottom: 2,
                    right: 4,
                    child: Container(
                      color: c.bg,
                      padding: const EdgeInsets.all(1),
                      child: Glyph(
                        ChurrIcons.notifyOff,
                        size: 12,
                        color: c.dim,
                      ),
                    ),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// The rail of the server-rail layout: one tile per server and a "+"
/// tile. Vertical beside the list on medium widths and up; a strip
/// across the top of the first screen on a phone.
class ServerRail extends StatelessWidget {
  const ServerRail({super.key, required this.axis, required this.onAddServer});
  final Axis axis;
  final VoidCallback onAddServer;

  @override
  Widget build(BuildContext context) {
    final app = AppScope.of(context);
    final c = ChurrColors.of(context);
    final tiles = <Widget>[
      for (final s in app.servers)
        // Tiles reorder by dragging; right-click opens the server's menu.
        ServerReorder(
          server: s,
          axis: axis,
          child: GestureDetector(
            onLongPressStart: (d) =>
                showServerMenu(context, s, d.globalPosition),
            child: ServerTile(
              key: Key('server-tile-${s.localId}'),
              server: s,
              selected: s == app.selectedServer,
              onTap: () => app.selectServer(s.localId),
            ),
          ),
        ),
      Tooltip(
        message: 'Add a server',
        child: InkWell(
          key: const Key('add-server'),
          onTap: onAddServer,
          child: Container(
            width: 44,
            height: 44,
            margin: const EdgeInsets.symmetric(horizontal: 6, vertical: 4),
            color: c.raised,
            child: Glyph(
              ChurrIcons.add,
              color: c.ink,
              semanticLabel: 'Add a server',
            ),
          ),
        ),
      ),
    ];
    return Container(
      width: axis == Axis.vertical ? 64 : null,
      height: axis == Axis.horizontal ? 60 : null,
      // Ground beside the panel of the list: a fill step, not a line.
      color: c.bg,
      // Tiles start at the top (or the leading edge on a phone), in the
      // person's order, and scroll when there are many.
      child: Align(
        alignment: axis == Axis.vertical
            ? Alignment.topCenter
            : AlignmentDirectional.centerStart,
        child: SingleChildScrollView(
          scrollDirection: axis,
          padding: const EdgeInsets.symmetric(vertical: 4, horizontal: 2),
          child: Flex(
            direction: axis,
            mainAxisSize: MainAxisSize.min,
            children: tiles,
          ),
        ),
      ),
    );
  }
}
