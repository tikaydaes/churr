import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../core/models/conversation.dart';
import '../../state/app_state.dart';
import '../../state/personal_sidebar.dart';
import '../../state/server_state.dart';
import '../../state/sidebar_model.dart';
import '../../l10n/app_localizations.dart';
import '../screens/account/conversation_settings.dart';
import '../scopes.dart';
import '../theme/tokens.dart';
import '../theme/churr_icons.dart';
import '../theme/shapes.dart';
import '../widgets/controls.dart';
import 'sidebar_arrange.dart';

/// One conversation's row in the sidebar, drawn the same in every
/// layout (docs/CLIENTS.md 9.2, "How a row is drawn"), as the guide's
/// channel list draws it: a fold chevron in the left gutter for parents,
/// one column of marks (`#`, padlock, presence square, sealed shield),
/// the name, then "N more", the hidden tag and the yellow count.
class ConversationRowTile extends StatefulWidget {
  const ConversationRowTile({
    super.key,
    required this.server,
    required this.row,
    this.onToggleCollapsed,
    this.onMoveBy,
    this.reordering = false,
    this.dropTarget = false,
  });

  final ServerState server;
  final SidebarRow row;
  final VoidCallback? onToggleCollapsed;

  /// Moves the row one place up (-1) or down (1) under its heading:
  /// Alt+Up and Alt+Down, and the arrows in reordering mode. Null for a
  /// sub-channel, which sits under its parent by name.
  final void Function(int by)? onMoveBy;

  /// Reordering mode: a handle to drag by, and arrows.
  final bool reordering;

  /// A dragged row would land here, before this one: a line above it.
  final bool dropTarget;

  @override
  State<ConversationRowTile> createState() => _ConversationRowTileState();
}

/// The left edge of a top-level row's mark column; a sub-channel's sits
/// one step further in. Shared with the sidebar's own entries so every
/// mark lines up.
const rowGutter = 26.0;
const rowIndentStep = 22.0;

class _ConversationRowTileState extends State<ConversationRowTile> {
  bool focused = false;

  SidebarRow get row => widget.row;
  ServerState get server => widget.server;

  @override
  Widget build(BuildContext context) {
    final app = AppScope.of(context);
    final c = ChurrColors.of(context);
    final target = OpenTarget(server.localId, row.id);
    final selected = app.open == target;
    // Quiet rows (notify nothing, or muted) are dimmed. The guide halves
    // their opacity; faint is used instead so the name still reaches
    // 4.5:1 on the panel.
    final color = selected || row.bold ? c.ink : (row.dimmed ? c.faint : c.dim);
    final label = [
      if (row.parentName != null) '${row.parentName} ›',
      row.name,
      ?row.detail,
    ].join(' ');
    final indent = rowGutter + rowIndentStep * row.indent;
    return Semantics(
      button: true,
      selected: selected,
      label:
          '$label'
          '${row.bold ? ', unread' : ''}'
          '${row.badge > 0 ? ', ${row.badge} notifications' : ''}'
          '${row.dimmed ? ', notifies nothing' : ''}'
          '${row.hiddenMark ? ', hidden' : ''}'
          '${row.conversation.chatState != null ? ', ${row.conversation.chatState}' : ''}'
          '${row.moreCount > 0 ? ', ${row.moreCount} more sub-channels' : ''}',
      excludeSemantics: true,
      child: Tooltip(
        // A name too long for its row is shown whole here, with the
        // detail line that tells same-named channels apart.
        message: row.conversation.isNamed
            ? '$label\n${detailLine(row.conversation, parentName: row.parentName)}'
            : label,
        waitDuration: const Duration(milliseconds: 700),
        child: GestureDetector(
          onSecondaryTapUp: (d) => _menu(context, d.globalPosition),
          onLongPressStart: (d) => _menu(context, d.globalPosition),
          child: _keys(InkWell(
            key: Key('row-${row.id}'),
            onTap: () => app.openConversation(target),
            onFocusChange: (v) => setState(() => focused = v),
            focusColor: Colors.transparent,
            child: FocusOutline(
              focused: focused,
              inset: true,
              child: Container(
                constraints: const BoxConstraints(minHeight: 32),
                decoration: BoxDecoration(
                  color: selected ? c.raised : null,
                  // The selected row: raised, with a cyan bar on its
                  // left edge so it does not rest on the fill alone. A
                  // row about to take a dropped one has a line on top.
                  border: Border(
                    left: selected
                        ? BorderSide(color: c.cyan, width: 4)
                        : BorderSide.none,
                    top: widget.dropTarget
                        ? BorderSide(color: c.cyan, width: 3)
                        : BorderSide.none,
                  ),
                ),
                child: Stack(
                  children: [
                    Padding(
                      padding: EdgeInsets.fromLTRB(
                        indent - (selected ? 4 : 0),
                        5,
                        12,
                        5,
                      ),
                      child: Row(
                        children: [
                          SizedBox(width: 14, child: Center(child: _mark(c))),
                          const SizedBox(width: 8),
                          Expanded(child: _name(c, color, selected)),
                          if (row.moreCount > 0)
                            Padding(
                              padding: const EdgeInsets.only(left: 6),
                              child: _More(
                                count: row.moreCount,
                                onTap: widget.onToggleCollapsed,
                              ),
                            ),
                          if (row.hiddenMark)
                            const Padding(
                              padding: EdgeInsets.only(left: 6),
                              child: Tag('hidden'),
                            ),
                          // A chat that waits, was declined or was closed
                          // (rulings R78, R79).
                          if (row.conversation.chatState != null)
                            Padding(
                              padding: const EdgeInsets.only(left: 6),
                              child: Tag(
                                switch (row.conversation.chatState) {
                                  'waiting' =>
                                    L10n.of(context).chatStateWaiting,
                                  'declined' =>
                                    L10n.of(context).chatStateDeclined,
                                  _ => L10n.of(context).chatStateClosed,
                                },
                              ),
                            ),
                          if (row.badge > 0)
                            Padding(
                              padding: const EdgeInsets.only(left: 6),
                              child: PriceTag(
                                row.badge,
                                label: '${row.badge} notifications',
                                size: 12,
                              ),
                            ),
                          if (widget.reordering && widget.onMoveBy != null) ..._steps(c),
                        ],
                      ),
                    ),
                    if (row.hasChildren)
                      Positioned(
                        left: rowIndentStep * row.indent - (selected ? 4 : 0),
                        top: 0,
                        bottom: 0,
                        child: FoldChevron(
                          open: !row.collapsed,
                          width: rowGutter,
                          onTap: widget.onToggleCollapsed,
                          label: row.collapsed
                              ? 'Show sub-channels of ${row.name}'
                              : 'Hide sub-channels of ${row.name}',
                        ),
                      ),
                  ],
                ),
              ),
            ),
          )),
        ),
      ),
    );
  }

  /// Alt+Up and Alt+Down move the focused row (docs/CLIENTS.md 9.1).
  Widget _keys(Widget child) => widget.onMoveBy == null
      ? child
      : CallbackShortcuts(
          bindings: {
            const SingleActivator(LogicalKeyboardKey.arrowUp, alt: true): () => widget.onMoveBy!(-1),
            const SingleActivator(LogicalKeyboardKey.arrowDown, alt: true): () => widget.onMoveBy!(1),
          },
          child: child,
        );

  /// Reordering mode: up, down, and a handle that drags the row.
  List<Widget> _steps(ChurrColors c) {
    final drag = DragHandleScope.maybeOf(context);
    Widget step(ChurrGlyph g, String label, int by) => IconButton(
      tooltip: label,
      visualDensity: VisualDensity.compact,
      icon: Glyph(g, size: 16, color: c.ink, semanticLabel: label),
      onPressed: () => widget.onMoveBy!(by),
    );
    return [
      step(ChurrIcons.arrowUp, 'Move ${row.name} up', -1),
      step(ChurrIcons.arrowDown, 'Move ${row.name} down', 1),
      if (drag != null)
        Draggable<RowDrag>(
          data: drag.data,
          feedback: Material(
            type: MaterialType.transparency,
            child: Container(
              decoration: floatingBox(c),
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
              child: Text(drag.label, style: TextStyle(color: c.ink, fontSize: 14, fontWeight: FontWeight.w600)),
            ),
          ),
          child: Semantics(
            label: 'Drag ${row.name}',
            child: Padding(
              padding: const EdgeInsets.all(10),
              child: Glyph(ChurrIcons.grip, size: 16, color: c.dim),
            ),
          ),
        ),
    ];
  }

  /// The one column of marks: what kind of conversation, and for a
  /// two-person chat the other person's presence as a shape.
  Widget _mark(ChurrColors c) => switch (row.icon) {
    RowIcon.public => Text(
      '#',
      style: monoStyle(
        context,
        size: 12,
        color: c.faint,
        weight: FontWeight.w600,
      ),
    ),
    RowIcon.private => Glyph(
      ChurrIcons.private,
      size: 14,
      color: c.faint,
      semanticLabel: 'private',
    ),
    RowIcon.locked => Glyph(
      ChurrIcons.locked,
      size: 14,
      color: c.faint,
      semanticLabel: 'locked',
    ),
    RowIcon.sealed => SealedShield(row.presence ?? 'offline'),
    RowIcon.chat =>
      row.presence != null
          ? PresenceLight(row.presence!)
          : Glyph(ChurrIcons.chat, size: 14, color: c.faint),
  };

  Widget _name(ChurrColors c, Color color, bool selected) => Text.rich(
    TextSpan(
      children: [
        // A sub-channel drawn without its parent names it first.
        if (row.parentName != null)
          TextSpan(
            text: '${row.parentName} › ',
            style: TextStyle(
              color: row.dimmed && !selected ? c.faint : c.dim,
              fontWeight: FontWeight.w500,
            ),
          ),
        TextSpan(text: row.name),
        if (row.detail != null)
          TextSpan(
            text: '  ${row.detail}',
            style: TextStyle(
              fontSize: 11,
              color: c.faint,
              fontWeight: FontWeight.w400,
            ),
          ),
      ],
    ),
    maxLines: 1,
    overflow: TextOverflow.ellipsis,
    style: TextStyle(
      fontSize: 14,
      height: 1.3,
      color: color,
      fontStyle: row.hiddenMark ? FontStyle.italic : null,
      fontWeight: row.bold || selected ? FontWeight.w700 : FontWeight.w500,
    ),
  );

  /// The row's menu: the two independent settings of 9.12, star, mark
  /// read, "Move…" (the person's own sections, one place up or down,
  /// reordering mode) and "Reorder…", which a long press on a phone leads
  /// to.
  Future<void> _menu(BuildContext context, Offset at) async {
    final app = AppScope.read(context);
    final m = row.conversation.membership;
    if (m == null) return;
    final messenger = ScaffoldMessenger.of(context);
    PopupMenuItem<String> item(String value, String label, {bool on = false}) =>
        PopupMenuItem(
          value: value,
          height: 34,
          child: Row(
            children: [
              SizedBox(
                width: 22,
                child: on ? const Glyph(ChurrIcons.check, size: 14) : null,
              ),
              Text(label),
            ],
          ),
        );
    PopupMenuItem<String> head(String label) => PopupMenuItem(
      enabled: false,
      height: 26,
      child: Text(label.toUpperCase(), style: ChurrType.label(ChurrColors.of(context).dim, size: 10.5)),
    );
    final choice = await showMenu<String>(
  popUpAnimationStyle: AnimationStyle.noAnimation,
      context: context,
      position: RelativeRect.fromLTRB(at.dx, at.dy, at.dx, at.dy),
      items: [
        head('What notifies'),
        item('mode:any', 'Any activity', on: m.mode == NotifyMode.any),
        if (!row.conversation.isSealed)
          item('mode:mentions', 'Mentions', on: m.mode == NotifyMode.mentions),
        item('mode:nothing', 'Nothing', on: m.mode == NotifyMode.nothing),
        const PopupMenuDivider(),
        head('Show in sidebar'),
        item(
          'show:',
          'App default (${_showName(app.prefs.defaultShow)})',
          on: m.show == null,
        ),
        for (final s in ShowSetting.values)
          item('show:${s.name}', _showName(s), on: m.show == s),
        const PopupMenuDivider(),
        // Mute for a while, the thread view and the summary line.
        item('settings', L10n.of(context).conversationSettings),
        item('star', m.starred ? 'Remove star' : 'Star'),
        item('read', 'Mark read'),
        const PopupMenuDivider(),
        item('move', 'Move…'),
        item('reorder', 'Reorder…'),
      ],
    );
    if (choice == null) return;
    try {
      if (choice.startsWith('mode:')) {
        await server.sync.updateMembership(row.id, {
          'mode': choice.substring(5),
        });
      } else if (choice.startsWith('show:')) {
        final v = choice.substring(5);
        await server.sync.updateMembership(row.id, {
          'show': v.isEmpty ? null : v,
        });
      } else if (choice == 'settings') {
        if (context.mounted) await showConversationSettings(context, server, row.id);
      } else if (choice == 'star') {
        await server.sync.updateMembership(row.id, {'starred': !m.starred});
      } else if (choice == 'read') {
        await server.sync.markRead(row.id, row.conversation.lastSeq);
      } else if (choice == 'move') {
        if (context.mounted) await showMoveMenu(context, server, row, at);
      } else if (choice == 'reorder') {
        reorderingServer.value = server.localId;
        if (context.mounted) {
          announce(context, 'Reordering: each row has move up and move down');
        }
      }
    } on Exception catch (e) {
      messenger.showSnackBar(
        SnackBar(content: Text('That did not work. ($e)')),
      );
    }
  }
}

String _showName(ShowSetting s) => switch (s) {
  ShowSetting.always => 'Always',
  ShowSetting.activity => 'When there is activity',
  ShowSetting.notification => 'When it notifies',
  ShowSetting.hidden => 'Hidden',
};

/// The words for a `show` setting, for menus and settings.
String showSettingName(ShowSetting s) => _showName(s);

/// "N more": the sub-channels a folded parent is not drawing. Pressing it
/// unfolds the parent.
class _More extends StatelessWidget {
  const _More({required this.count, this.onTap});
  final int count;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Semantics(
      button: true,
      label: '$count more sub-channels, show them',
      excludeSemantics: true,
      child: InkWell(
        onTap: onTap,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 2),
          decoration: BoxDecoration(
            border: Border.all(color: c.edge, width: 1.5),
          ),
          child: Text(
            '$count more',
            style: monoStyle(
              context,
              size: 10,
              color: c.dim,
              weight: FontWeight.w600,
            ),
          ),
        ),
      ),
    );
  }
}

/// Hands a row's drag data to its handle in reordering mode.
class DragHandleScope extends InheritedWidget {
  const DragHandleScope({
    super.key,
    required this.data,
    required this.label,
    required super.child,
  });
  final RowDrag data;
  final String label;

  static DragHandleScope? maybeOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<DragHandleScope>();

  @override
  bool updateShouldNotify(DragHandleScope old) => old.data.conversationId != data.conversationId;
}
