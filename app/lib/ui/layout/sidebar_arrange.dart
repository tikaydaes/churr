import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/semantics.dart';

import '../../core/models/json.dart';
import '../../core/models/personal_layout.dart';
import '../../state/app_state.dart';
import '../../state/personal_sidebar.dart';
import '../../state/server_state.dart';
import '../../state/sidebar_model.dart';
import '../scopes.dart';
import '../theme/churr_icons.dart';
import '../theme/tokens.dart';

/// Arranging a person's own sidebar on one server (ruling R83,
/// docs/CLIENTS.md 9.1): the heading menu, "Move…", moving by one place,
/// new sections, and what is said for each move. Dragging is in
/// sidebar.dart; everything here is also what a keyboard or a screen
/// reader uses.

/// What the sidebar rules are given for one server on this device.
SidebarInput sidebarInput(AppState app, ServerState s) => SidebarInput(
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
  layout: s.layout,
);

/// A row being dragged.
class RowDrag {
  const RowDrag(this.serverId, this.conversationId);
  final String serverId;
  final String conversationId;
}

/// A heading being dragged.
class HeadingDrag {
  const HeadingDrag(this.serverId, this.key);
  final String serverId;
  final String key;
}

const sortNames = {
  SortMode.alphabetical: 'Alphabetical',
  SortMode.activity: 'Last activity',
  SortMode.custom: 'Custom order',
};

void announce(BuildContext context, String text) {
  final view = View.maybeOf(context);
  if (view == null) return;
  unawaited(SemanticsService.sendAnnouncement(view, text, TextDirection.ltr));
}

/// Sends changes and says what happened; [said] is worked out after, from
/// the layout as it now stands.
Future<void> sendLayout(
  BuildContext context,
  ServerState s,
  List<Json> ops,
  String Function() said,
) async {
  if (ops.isEmpty) return;
  final messenger = ScaffoldMessenger.maybeOf(context);
  try {
    await changeLayout(s, ops);
    if (context.mounted) announce(context, said());
  } on Exception catch (e) {
    messenger?.showSnackBar(SnackBar(content: Text('That did not work. ($e)')));
  }
}

List<SidebarGroup> groupsOf(BuildContext context, ServerState s) =>
    buildServerGroups(sidebarInput(AppScope.read(context), s));

/// "town is in Focus, 2 of 5".
String placeText(BuildContext context, ServerState s, String id, String name) {
  for (final g in groupsOf(context, s)) {
    final i = g.tops.indexOf(id);
    if (i >= 0) return '$name is in ${g.title}, ${i + 1} of ${g.tops.length}';
  }
  return '$name moved';
}

/// Moves a row one place up or down under its heading. A starred row
/// moves among the stars; the others below them.
Future<void> moveRowBy(
  BuildContext context,
  ServerState s,
  String id,
  String name,
  int by,
) async {
  final g = groupsOf(context, s).where((g) => g.tops.contains(id)).firstOrNull;
  if (g == null) return;
  final i = g.tops.indexOf(id);
  final to = i + by;
  final starred = i < g.starredCount;
  if (to < 0 || to >= g.tops.length) {
    announce(context, by < 0 ? 'Already first' : 'Already last');
    return;
  }
  if (starred != (to < g.starredCount)) {
    announce(
      context,
      starred ? 'Starred rows stay above the rest' : 'Already first after the starred rows',
    );
    return;
  }
  await sendLayout(
    context,
    s,
    reorderOps(g, id, to, s.layout),
    () => placeText(context, s, id, name),
  );
}

/// The channel headings in the person's order.
List<(String, String)> channelHeadings(ServerState s) => [
  for (final (key, title, _) in headingsOf(s.layout, s.sections))
    if (key != 'chats' && key != 'sealed') (key, title),
];

Future<void> moveHeadingBy(
  BuildContext context,
  ServerState s,
  String key,
  int by,
) async {
  final keys = channelHeadings(s);
  final i = keys.indexWhere((h) => h.$1 == key);
  final to = i + by;
  if (i < 0 || to < 0 || to >= keys.length) {
    announce(context, by < 0 ? 'Already first' : 'Already last');
    return;
  }
  await sendLayout(
    context,
    s,
    [
      {'op': 'move_section', 'key': key, 'index': to},
    ],
    () => '${keys[i].$2} moved to ${to + 1} of ${keys.length}',
  );
}

/// Puts a conversation under [key]: one of the person's own sections, or
/// back where the server lists it.
Future<void> moveRowTo(
  BuildContext context,
  ServerState s,
  String id,
  String name,
  String key, {
  int? index,
}) async {
  final groups = groupsOf(context, s);
  final target = groups.where((g) => g.key == key).firstOrNull;
  final title =
      target?.title ??
      headingsOf(s.layout, s.sections).where((h) => h.$1 == key).firstOrNull?.$2 ??
      key;
  await sendLayout(
    context,
    s,
    moveOps(id, target, key, index, s.layout),
    () => '$name moved to $title',
  );
}

/// Asks for a section's name.
Future<String?> askSectionName(
  BuildContext context, {
  String title = 'New section',
  String initial = '',
  String action = 'Create',
}) async {
  final name = TextEditingController(text: initial);
  final ok = await showDialog<bool>(
    animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (context) => AlertDialog(
      title: Text(title),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          TextField(
            key: const Key('section-name'),
            controller: name,
            autofocus: true,
            maxLength: 64,
            decoration: const InputDecoration(labelText: 'Name'),
            onSubmitted: (_) => Navigator.pop(context, true),
          ),
          const Text(
            'Only you see your sections. This server keeps them for all '
            'your devices and the web chat.',
          ),
        ],
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context, false),
          child: const Text('Cancel'),
        ),
        FilledButton(
          onPressed: () => Navigator.pop(context, true),
          child: Text(action),
        ),
      ],
    ),
  );
  final v = name.text.trim();
  return ok == true && v.isNotEmpty ? v : null;
}

/// Makes a section of the person's own, at the end of their sections,
/// and returns its key.
Future<String?> newSection(BuildContext context, ServerState s) async {
  final name = await askSectionName(context);
  if (name == null || !context.mounted) return null;
  await sendLayout(
    context,
    s,
    [
      {'op': 'create_section', 'name': name},
    ],
    () => 'Section $name made',
  );
  final made = s.layout.sections.where((x) => x.name == name).lastOrNull;
  return made?.key;
}

Future<bool> _confirm(BuildContext context, String title, String body, String yes) async =>
    await showDialog<bool>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (context) => AlertDialog(
        title: Text(title),
        content: Text(body),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: Text(yes),
          ),
        ],
      ),
    ) ==
    true;

PopupMenuItem<String> _item(String value, String label, {bool on = false, String? hint}) =>
    PopupMenuItem(
      value: value,
      height: 34,
      child: Row(
        children: [
          SizedBox(
            width: 22,
            child: on ? const Glyph(ChurrIcons.check, size: 14) : null,
          ),
          Flexible(child: Text(label, overflow: TextOverflow.ellipsis)),
          if (hint != null) ...[
            const SizedBox(width: 8),
            Text(hint, style: const TextStyle(fontSize: 11)),
          ],
        ],
      ),
    );

PopupMenuItem<String> _head(BuildContext context, String label) => PopupMenuItem(
  enabled: false,
  height: 26,
  child: Text(
    label.toUpperCase(),
    style: ChurrType.label(ChurrColors.of(context).dim, size: 10.5),
  ),
);

/// A heading's menu: how it sorts; for the person's own sections rename
/// and delete; moving it; reset; a new section; "Reset my sidebar".
Future<void> showHeadingMenu(
  BuildContext context,
  ServerState s,
  SidebarGroup g,
  Offset at,
) async {
  final choice = await showMenu<String>(
    popUpAnimationStyle: AnimationStyle.noAnimation,
    context: context,
    position: RelativeRect.fromLTRB(at.dx, at.dy, at.dx, at.dy),
    items: [
      _head(context, 'Sort by'),
      for (final m in SortMode.values)
        _item('sort:${m.name}', sortNames[m]!, on: g.sort == m),
      const PopupMenuDivider(),
      if (g.own) _item('rename', 'Rename…'),
      if (g.own) _item('delete', 'Delete section…'),
      if (g.movable) _item('up', 'Move up', hint: 'Alt+Up'),
      if (g.movable) _item('down', 'Move down', hint: 'Alt+Down'),
      _item(
        'reset',
        g.movable ? 'Reset to alphabetical' : 'Reset to last activity',
      ),
      const PopupMenuDivider(),
      _item('new', 'New section…'),
      _item('reset-all', 'Reset my sidebar…'),
    ],
  );
  if (choice == null || !context.mounted) return;
  switch (choice) {
    case final c when c.startsWith('sort:'):
      final m = SortMode.values.byName(c.substring(5));
      await sendLayout(
        context,
        s,
        [
          {'op': 'set_sort', 'key': g.key, 'sort': m.name},
        ],
        () => '${g.title} sorted by ${sortNames[m]!.toLowerCase()}',
      );
    case 'rename':
      final name = await askSectionName(
        context,
        title: 'Rename section',
        initial: g.title,
        action: 'Rename',
      );
      if (name != null && context.mounted) {
        await sendLayout(
          context,
          s,
          [
            {'op': 'rename_section', 'key': g.key, 'name': name},
          ],
          () => 'Renamed to $name',
        );
      }
    case 'delete':
      if (await _confirm(
            context,
            'Delete ${g.title}?',
            'Its conversations go back to where the server lists them. '
                'Nothing else is deleted.',
            'Delete',
          ) &&
          context.mounted) {
        await sendLayout(
          context,
          s,
          [
            {'op': 'delete_section', 'key': g.key},
          ],
          () => '${g.title} deleted',
        );
      }
    case 'up' || 'down':
      await moveHeadingBy(context, s, g.key, choice == 'up' ? -1 : 1);
    case 'reset':
      await sendLayout(
        context,
        s,
        [
          {'op': 'reset_section', 'key': g.key},
        ],
        () => '${g.title} reset',
      );
    case 'new':
      await newSection(context, s);
    case 'reset-all':
      if (await _confirm(
            context,
            'Reset your sidebar?',
            'Your own sections go, and every heading goes back to the '
                'server’s default order and sort. Stars stay.',
            'Reset',
          ) &&
          context.mounted) {
        await sendLayout(
          context,
          s,
          [
            {'op': 'reset_all'},
          ],
          () => 'Sidebar reset to the server’s default',
        );
      }
  }
}

/// "Move…": the person's own sections, back where the server lists it,
/// a new section, one place up or down, and reordering mode.
Future<void> showMoveMenu(
  BuildContext context,
  ServerState s,
  SidebarRow r,
  Offset at,
) async {
  final groups = groupsOf(context, s);
  final here = groups.where((g) => g.tops.contains(r.id)).firstOrNull?.key;
  final home = _homeKey(s, r);
  final homeTitle =
      headingsOf(s.layout, s.sections).where((h) => h.$1 == home).firstOrNull?.$2 ?? home;
  final choice = await showMenu<String>(
    popUpAnimationStyle: AnimationStyle.noAnimation,
    context: context,
    position: RelativeRect.fromLTRB(at.dx, at.dy, at.dx, at.dy),
    items: [
      _head(context, 'Move ${r.name} to'),
      for (final x in s.layout.sections)
        _item('to:${x.key}', x.name, on: here == x.key),
      _item('to:$home', '$homeTitle (where the server lists it)', on: here == home),
      const PopupMenuDivider(),
      _item('new', 'New section…'),
      _item('up', 'Move up', hint: 'Alt+Up'),
      _item('down', 'Move down', hint: 'Alt+Down'),
      _item('reorder', 'Reorder…'),
    ],
  );
  if (choice == null || !context.mounted) return;
  switch (choice) {
    case final c when c.startsWith('to:'):
      await moveRowTo(context, s, r.id, r.name, c.substring(3));
    case 'new':
      final key = await newSection(context, s);
      if (key != null && context.mounted) {
        await moveRowTo(context, s, r.id, r.name, key);
      }
    case 'up' || 'down':
      await moveRowBy(context, s, r.id, r.name, choice == 'up' ? -1 : 1);
    case 'reorder':
      reorderingServer.value = s.localId;
      announce(context, 'Reordering: each row has move up and move down');
  }
}

/// Where the server lists a conversation, whatever the person did.
String _homeKey(ServerState s, SidebarRow r) {
  final c = r.conversation;
  if (c.isSealed) return 'sealed';
  if (!c.isNamed) return 'chats';
  final p = c.parentId == null
      ? null
      : s.conversation(c.parentId!) ?? s.outsideParents[c.parentId];
  final section = (p ?? c).sectionId;
  return s.sections.any((x) => x.id == section) ? 's:$section' : 's:';
}

/// Whether a heading takes a dragged row: one of the person's own
/// sections, the heading the row is under, or the one the server lists
/// it under.
bool takesRow(ServerState s, SidebarGroup g, SidebarRow r, String? here) =>
    g.own || g.key == here || g.key == _homeKey(s, r);
