import 'package:flutter/material.dart';

import '../../../core/api/me.dart';
import '../../../core/models/server.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/server_state.dart';
import '../../theme/churr_icons.dart';
import '../../theme/tokens.dart';
import 'common.dart';

/// Opens the groups of one server, from "Browse channels".
Future<void> openGroups(BuildContext context, ServerState server) =>
    showScreen<void>(
      context,
      title: L10n.of(context).groupsTitle,
      child: GroupsScreen(server: server),
    );

/// The server's groups (docs/CLIENTS.md 9.15): each with its handle, name
/// and size, and Join or Leave on an open one. Joining an open group adds
/// the person to every channel it is in. The two built-in groups are not
/// listed; a closed group or one filled from a provider is shown without
/// a button, since admins decide who is in it.
class GroupsScreen extends StatefulWidget {
  const GroupsScreen({super.key, required this.server});
  final ServerState server;

  @override
  State<GroupsScreen> createState() => _GroupsScreenState();
}

class _GroupsScreenState extends State<GroupsScreen> {
  final busy = <String>{};

  ServerState get server => widget.server;

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  /// The cached list is filled at sign-in; it is asked again here so the
  /// sizes and who is in what are current.
  Future<void> _refresh() async {
    try {
      await server.db.replaceGroups(await server.api.listGroups());
    } on Exception {
      // The cached list stays.
    }
  }

  Future<void> _toggle(Group g) async {
    setState(() => busy.add(g.id));
    try {
      if (g.isMember) {
        await server.api.leaveGroup(g.id);
      } else {
        await server.api.joinGroup(g.id);
      }
      await _refresh();
      if (mounted) {
        final t = L10n.of(context);
        say(
          context,
          g.isMember ? t.leftGroup(g.handle) : t.joinedGroup(g.handle),
        );
      }
    } on Exception catch (e) {
      if (mounted) say(context, refusal(e));
    } finally {
      if (mounted) setState(() => busy.remove(g.id));
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    return ListenableBuilder(
      listenable: server,
      builder: (context, _) {
        final role = server.me?.role;
        final groups = [
          for (final g in server.groups.values)
            if (g.builtin == null) g,
        ]..sort((a, b) => a.handle.compareTo(b.handle));
        if (groups.isEmpty) {
          return Center(
            child: Text(t.groupsNone, style: TextStyle(color: c.dim)),
          );
        }
        return ListView(
          key: const Key('groups-list'),
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 8),
          children: [
            for (final g in groups)
              ListTile(
                key: Key('group-${g.handle}'),
                leading: Glyph(ChurrIcons.people, size: 18, color: c.dim),
                title: Text(
                  '@${g.handle}',
                  style: monoStyle(context, size: 14),
                ),
                subtitle: Text(
                  [
                    if (g.name.isNotEmpty) g.name,
                    t.groupMembers(g.memberCount),
                    if (g.isMember) t.groupYoureIn,
                    if (!g.open) t.groupClosed,
                    if (g.description.isNotEmpty) g.description,
                  ].join(' · '),
                ),
                trailing: !g.selfServe(role)
                    ? null
                    : g.isMember
                    ? TextButton(
                        key: Key('leave-group-${g.handle}'),
                        onPressed: busy.contains(g.id)
                            ? null
                            : () => _toggle(g),
                        child: Text(t.leaveGroup),
                      )
                    : FilledButton(
                        key: Key('join-group-${g.handle}'),
                        onPressed: busy.contains(g.id)
                            ? null
                            : () => _toggle(g),
                        child: Text(t.join),
                      ),
              ),
          ],
        );
      },
    );
  }
}
