import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../core/models/personal.dart';
import '../../l10n/app_localizations.dart';
import '../../state/account_hub.dart';
import '../../state/hybrid_order.dart';
import '../../state/server_state.dart';
import '../screens/account/bookmarks_screen.dart';
import '../screens/account/common.dart';
import '../screens/account/dnd_screen.dart';
import '../screens/account/my_profile_screen.dart';
import '../screens/account/notifications_screen.dart';
import '../screens/account/server_settings_screen.dart';
import '../screens/account/sign_in_screens.dart';
import '../screens/account/status_screen.dart';
import '../screens/account/threads_screen.dart';
import '../scopes.dart';
import '../theme/tokens.dart';
import '../widgets/controls.dart';
import '../theme/churr_icons.dart';
import '../widgets/avatar.dart';

/// The account area's entries in the sidebar (docs/CLIENTS.md 9.1): the
/// person's own status and do not disturb at the top, Notifications,
/// Threads and Bookmarks, each server's menu, and the banners for
/// servers that signed this device out.

/// Opens a list screen; opening an item in it closes it, so the
/// conversation is in view.
Future<void> _openList(
  BuildContext context,
  String title,
  Widget Function(VoidCallback onOpened) build,
) {
  final nav = Navigator.of(context);
  return showAccountScreen(
    context,
    title: title,
    width: 640,
    child: Builder(builder: (_) => build(() => nav.pop())),
  );
}

Future<void> openNotifications(BuildContext context) => _openList(
  context,
  L10n.of(context).notifications,
  (done) => NotificationsScreen(onOpened: done),
);

Future<void> openThreads(BuildContext context, {String? serverId}) => _openList(
  context,
  L10n.of(context).threads,
  (done) => ThreadsScreen(onOpened: done, serverFilter: serverId),
);

Future<void> openBookmarks(BuildContext context) => _openList(
  context,
  L10n.of(context).bookmarks,
  (done) => BookmarksScreen(onOpened: done),
);

Future<void> openStatus(BuildContext context) => showAccountScreen(
  context,
  title: L10n.of(context).yourStatus,
  child: const StatusScreen(),
);

Future<void> openDnd(BuildContext context) => showAccountScreen(
  context,
  title: L10n.of(context).dndTitle,
  child: const DndScreen(),
);

Future<void> openMyProfile(BuildContext context, {ServerState? server}) =>
    showAccountScreen(
      context,
      title: L10n.of(context).myProfile,
      child: MyProfileScreen(initialServer: server),
    );

Future<void> openSetupCode(BuildContext context) => showAccountScreen(
  context,
  title: L10n.of(context).setUpAnotherDevice,
  child: const SetupCodeScreen(),
);

/// The top of the sidebar: who the person is, their status, and do not
/// disturb one tap away.
class AccountStrip extends StatelessWidget {
  const AccountStrip({super.key, this.server});

  /// The server shown (rail layout); otherwise the first taking part.
  final ServerState? server;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final app = AppScope.of(context);
    final hub = AccountHub.of(app);
    return ListenableBuilder(
      listenable: hub,
      builder: (context, _) {
        final s =
            server ?? hub.participants().firstOrNull ?? app.selectedServer;
        final me = s?.me;
        if (s == null || me == null) return const SizedBox.shrink();
        final now = DateTime.now();
        final st = me.fullStatus;
        final status = st != null && st.existsAt(now)
            ? [?st.emoji, ?st.text].join(' ')
            : '';
        final dnd =
            hub.participants(forDnd: true).any((x) => x.me?.dndOn ?? false) ||
            me.dndOn;
        // The guide's "me" block: raised, at the foot of the sidebar,
        // with the avatar, the name and a presence light.
        final presence = dnd
            ? 'dnd'
            : (me.presence == 'offline' ? 'online' : me.presence);
        return Container(
          color: c.raised,
          padding: const EdgeInsets.fromLTRB(10, 8, 4, 8),
          child: Row(
            children: [
              Expanded(
                child: InkWell(
                  key: const Key('account-strip'),
                  onTap: () => _menu(context, s),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(
                      vertical: 2,
                      horizontal: 4,
                    ),
                    child: Row(
                      children: [
                        Avatar(account: me, server: s, size: 34),
                        const SizedBox(width: 10),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              Text(
                                me.name,
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: TextStyle(
                                  fontSize: 14,
                                  fontWeight: FontWeight.w700,
                                  color: c.ink,
                                  height: 1.2,
                                ),
                              ),
                              const SizedBox(height: 3),
                              Row(
                                children: [
                                  PresenceLight(presence),
                                  const SizedBox(width: 8),
                                  Expanded(
                                    child: Text(
                                      status.isEmpty
                                          ? presenceWords(presence)
                                          : status,
                                      maxLines: 1,
                                      overflow: TextOverflow.ellipsis,
                                      style: monoStyle(context, size: 10.5),
                                    ),
                                  ),
                                ],
                              ),
                            ],
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
              IconButton(
                key: const Key('dnd-toggle'),
                tooltip: dnd ? t.dndToggleOff : t.dndToggleOn,
                isSelected: dnd,
                icon: Glyph(ChurrIcons.dndOff, color: c.dim),
                selectedIcon: Glyph(ChurrIcons.dnd, color: c.pink),
                onPressed: () async {
                  if (dnd) {
                    await hub.clearDnd();
                  } else {
                    final v = await pickUntil(context, title: t.dndTurnOnTitle);
                    if (v != null) await hub.setDnd(v == untilForever ? null : v);
                  }
                },
              ),
            ],
          ),
        );
      },
    );
  }

  Future<void> _menu(BuildContext context, ServerState s) async {
    final t = L10n.of(context);
    final box = context.findRenderObject() as RenderBox?;
    final at = box?.localToGlobal(Offset.zero) ?? Offset.zero;
    final v = await showMenu<String>(
  popUpAnimationStyle: AnimationStyle.noAnimation,
      context: context,
      position: RelativeRect.fromLTRB(at.dx, at.dy + 30, at.dx + 200, at.dy),
      items: [
        PopupMenuItem(value: 'status', child: Text(t.yourStatus)),
        PopupMenuItem(value: 'dnd', child: Text(t.dndTitle)),
        PopupMenuItem(value: 'profile', child: Text(t.myProfile)),
      ],
    );
    if (!context.mounted) return;
    switch (v) {
      case 'status':
        await openStatus(context);
      case 'dnd':
        await openDnd(context);
      case 'profile':
        await openMyProfile(context, server: s);
    }
  }
}

/// A server's menu (merged heading, rail tile, hybrid "Servers"): its
/// settings, mute, server settings in the browser, moving it in the
/// person's order, and sign out.
Future<void> showServerMenu(
  BuildContext context,
  ServerState s,
  Offset at,
) async {
  final t = L10n.of(context);
  final app = AppScope.read(context);
  final hub = AccountHub.of(app);
  final i = app.servers.indexOf(s);
  final v = await showMenu<String>(
  popUpAnimationStyle: AnimationStyle.noAnimation,
    context: context,
    position: RelativeRect.fromLTRB(at.dx, at.dy, at.dx, at.dy),
    items: [
      PopupMenuItem(value: 'settings', child: Text(t.serverSettings)),
      PopupMenuItem(
        value: 'mute',
        child: Text(hub.isMuted(s) ? t.unmuteServer : t.muteServer),
      ),
      PopupMenuItem(value: 'admin', child: Text(t.serverAdminInBrowser)),
      if (i > 0) PopupMenuItem(value: 'up', child: Text(t.moveUp)),
      if (i < app.servers.length - 1)
        PopupMenuItem(value: 'down', child: Text(t.moveDown)),
      PopupMenuItem(value: 'signout', child: Text(t.signOut)),
    ],
  );
  if (!context.mounted) return;
  switch (v) {
    case 'settings':
      await showServerSettings(context, s);
    case 'mute':
      hub.isMuted(s)
          ? await hub.unmuteServer(s)
          : await confirmMuteServer(context, s);
    case 'admin':
      await launchUrl(
        Uri.parse(s.entry.address).replace(path: '/admin'),
        mode: LaunchMode.externalApplication,
      );
    case 'up':
      await moveServer(app, s.localId, i - 1);
    case 'down':
      await moveServer(app, s.localId, i + 1);
    case 'signout':
      await confirmSignOut(context, s);
  }
}

/// Makes [child] (a server's tile or heading) draggable onto another
/// server's, to change the person's order; right-click or long press
/// with no drag opens the server's menu.
class ServerReorder extends StatelessWidget {
  const ServerReorder({
    super.key,
    required this.server,
    required this.child,
    this.axis,
  });
  final ServerState server;
  final Widget child;

  /// The rail's direction, or null for a heading.
  final Axis? axis;

  @override
  Widget build(BuildContext context) {
    final app = AppScope.of(context);
    final c = ChurrColors.of(context);
    return DragTarget<ServerState>(
      onWillAcceptWithDetails: (d) => d.data != server,
      onAcceptWithDetails: (d) =>
          moveServer(app, d.data.localId, app.servers.indexOf(server)),
      builder: (context, candidates, _) => GestureDetector(
        onSecondaryTapUp: (d) =>
            showServerMenu(context, server, d.globalPosition),
        child: Draggable<ServerState>(
          data: server,
          axis: axis,
          feedback: Material(
            color: c.raised,
            child: Padding(
              padding: const EdgeInsets.all(8),
              child: Text(server.name),
            ),
          ),
          child: Container(
            decoration: candidates.isEmpty
                ? null
                : BoxDecoration(
                    border: Border.all(color: c.lavender, width: 2),
                  ),
            child: child,
          ),
        ),
      ),
    );
  }
}

/// Whether a server is muted from the app, for marks in every layout.
bool serverMuted(BuildContext context, ServerState s) =>
    AccountHub.of(AppScope.read(context)).isMuted(s);
