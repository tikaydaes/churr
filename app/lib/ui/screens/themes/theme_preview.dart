import 'package:flutter/material.dart';

import '../../../core/themes/theme.dart';
import '../../../l10n/app_localizations.dart';
import '../../message/form_view.dart' show pollBar;
import '../../message/markdown_view.dart';
import '../../theme/churr_icons.dart';
import '../../theme/shapes.dart';
import '../../theme/theme.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import 'theme_widgets.dart';

/// The editor's live preview: a real conversation drawn with the theme
/// being edited, through the same Material theme and components as the
/// app (lib/ui/theme): sidebar rows in every state, messages with a
/// mention, a link and code, a poll, a thread marker, buttons, tags and
/// the composer with its block cursor.
class ThemePreview extends StatelessWidget {
  const ThemePreview({super.key, required this.theme});
  final ChurrTheme theme;

  @override
  Widget build(BuildContext context) => InTheme(
    theme: theme,
    child: Builder(builder: (context) => const _Preview()),
  );
}

class _Preview extends StatelessWidget {
  const _Preview();

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Material(
      key: const Key('theme-preview'),
      color: c.bg,
      child: LayoutBuilder(
        builder: (context, box) {
          final wide = box.maxWidth >= 520;
          final sidebar = ConstrainedBox(
            constraints: BoxConstraints(
              minWidth: wide ? 200 : box.maxWidth,
              maxWidth: wide ? 200 : box.maxWidth,
              minHeight: wide ? 600 : 0,
            ),
            child: const _Sidebar(),
          );
          const conversation = _Conversation();
          // The preview scrolls with the editor, so its height is its
          // content's: the sidebar is given the conversation's usual
          // height rather than stretched.
          return wide
              ? Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    sidebar,
                    const Expanded(child: conversation),
                  ],
                )
              : Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [sidebar, conversation],
                );
        },
      ),
    );
  }
}

class _Sidebar extends StatelessWidget {
  const _Sidebar();

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    return Container(
      color: c.panel,
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        mainAxisSize: MainAxisSize.min,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(12, 4, 12, 8),
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    t.previewCommunity.toUpperCase(),
                    style: archivo(
                      size: 15,
                      weight: 900,
                      width: 78,
                      color: c.ink,
                    ),
                  ),
                ),
                const PresenceLight('online'),
                const SizedBox(width: 4),
                Text(
                  t.previewConnected,
                  style: monoStyle(context, size: 9.5, color: c.faint),
                ),
              ],
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(12, 6, 12, 4),
            child: Row(
              children: [
                ChevronMark(open: true, color: c.faint),
                const SizedBox(width: 4),
                Text(
                  t.previewSection.toUpperCase(),
                  style: ChurrType.label(c.faint),
                ),
                const SizedBox(width: 6),
                Text('5', style: monoStyle(context, size: 10, color: c.faint)),
              ],
            ),
          ),
          _Row(icon: ChurrIcons.hash, name: 'general', state: _RowState.normal),
          _Row(
            icon: ChurrIcons.hash,
            name: 'design',
            state: _RowState.unread,
            count: 3,
          ),
          _Row(
            icon: ChurrIcons.private,
            name: 'planning',
            state: _RowState.open,
          ),
          _Row(icon: ChurrIcons.hash, name: 'random', state: _RowState.quiet),
          _Row(
            icon: ChurrIcons.hash,
            name: 'archive-2025',
            state: _RowState.hidden,
            tag: t.previewHiddenTag,
          ),
          _Row(icon: ChurrIcons.hash, name: 'releases', state: _RowState.hover),
        ],
      ),
    );
  }
}

enum _RowState { normal, unread, open, quiet, hidden, hover }

class _Row extends StatelessWidget {
  const _Row({
    required this.icon,
    required this.name,
    required this.state,
    this.count,
    this.tag,
  });
  final ChurrGlyph icon;
  final String name;
  final _RowState state;
  final int? count;
  final String? tag;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final raised = state == _RowState.open || state == _RowState.hover;
    final color = switch (state) {
      _RowState.unread || _RowState.open || _RowState.hover => c.ink,
      _RowState.quiet => c.faint,
      _ => c.dim,
    };
    return Container(
      margin: const EdgeInsets.only(bottom: 2),
      decoration: BoxDecoration(
        color: raised ? c.raised : null,
        border: state == _RowState.open
            ? Border(left: BorderSide(color: c.cyan, width: 4))
            : null,
      ),
      padding: EdgeInsets.fromLTRB(state == _RowState.open ? 18 : 22, 5, 10, 5),
      child: Row(
        children: [
          Glyph(icon, size: 15, color: color),
          const SizedBox(width: 6),
          Expanded(
            child: Text(
              name,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                color: color,
                fontSize: 14,
                fontWeight: state == _RowState.unread
                    ? FontWeight.w800
                    : FontWeight.w500,
                fontStyle: state == _RowState.hidden ? FontStyle.italic : null,
              ),
            ),
          ),
          if (state == _RowState.quiet)
            Glyph(ChurrIcons.notifyOff, size: 13, color: c.faint),
          if (tag != null) Tag(tag!, fill: c.lavender, degrees: 2),
          if (count != null) PriceTag(count!, size: 11),
        ],
      ),
    );
  }
}

class _Conversation extends StatelessWidget {
  const _Conversation();

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final mc = MarkdownContext(onOpenLink: (_) {});
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      mainAxisSize: MainAxisSize.min,
      children: [
        Container(
          color: c.panel,
          padding: const EdgeInsets.fromLTRB(14, 10, 14, 10),
          child: Row(
            children: [
              Glyph(ChurrIcons.private, size: 17, color: c.dim),
              const SizedBox(width: 6),
              Text('planning', style: ChurrType.title(c.ink, size: 17)),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  t.previewTopic,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(color: c.dim, fontSize: 13),
                ),
              ),
            ],
          ),
        ),
        _Message(
          name: 'Sam Okafor',
          time: '09:12',
          fill: c.cyan,
          child: MarkdownView(text: t.previewMessage1, context_: mc),
        ),
        _Message(
          name: 'Kai Tanaka',
          time: '09:14',
          fill: c.lavender,
          mine: true,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text.rich(
                TextSpan(
                  children: [
                    WidgetSpan(
                      alignment: PlaceholderAlignment.middle,
                      child: Container(
                        color: c.lavender,
                        padding: const EdgeInsets.symmetric(horizontal: 3),
                        child: Text(
                          '@Sam',
                          style: TextStyle(
                            color: c.onLavender,
                            fontWeight: FontWeight.w700,
                            fontSize: 15,
                          ),
                        ),
                      ),
                    ),
                    TextSpan(text: ' ${t.previewMessage2}'),
                  ],
                ),
                style: ChurrType.body(c.ink, size: 15),
              ),
              const SizedBox(height: 6),
              ThreadSummaryChip(text: t.previewThread, unread: true),
            ],
          ),
        ),
        _Message(
          name: 'Robin',
          time: '09:20',
          fill: c.pink,
          child: _Poll(
            question: t.previewPoll,
            a: t.previewPollA,
            b: t.previewPollB,
          ),
        ),
        const NewMarker(),
        Padding(
          padding: const EdgeInsets.fromLTRB(14, 8, 14, 8),
          child: Wrap(
            spacing: 8,
            runSpacing: 8,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              FilledButton(onPressed: () {}, child: Text(t.previewJoin)),
              OutlinedButton(onPressed: () {}, child: Text(t.previewSecondary)),
              FilledButton(
                style: dangerButton(c),
                onPressed: () {},
                child: Text(t.previewDanger),
              ),
              Tag(t.previewTagPrivate, fill: c.lavender, degrees: -2),
              Tag(t.previewTagOn, fill: c.cyan, degrees: 2),
              Tag(t.previewTagFailed, fill: c.pink, degrees: -3),
              Tag(t.previewTagNew, fill: c.yellow, degrees: 3),
            ],
          ),
        ),
        Container(
          margin: const EdgeInsets.fromLTRB(14, 4, 14, 12),
          decoration: BoxDecoration(
            color: c.panel,
            border: Border(bottom: BorderSide(color: c.edge, width: 2)),
          ),
          padding: const EdgeInsets.fromLTRB(10, 10, 10, 10),
          child: Row(
            children: [
              const BlockCursor(),
              const SizedBox(width: 6),
              Expanded(
                child: Text(
                  t.previewComposer,
                  style: TextStyle(color: c.faint, fontSize: 15),
                ),
              ),
              Glyph(ChurrIcons.attach, size: 18, color: c.dim),
              const SizedBox(width: 10),
              Glyph(ChurrIcons.send, size: 18, color: c.cyan),
            ],
          ),
        ),
      ],
    );
  }
}

class _Message extends StatelessWidget {
  const _Message({
    required this.name,
    required this.time,
    required this.fill,
    required this.child,
    this.mine = false,
  });
  final String name;
  final String time;
  final Color fill;
  final Widget child;
  final bool mine;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Container(
      decoration: mine
          ? BoxDecoration(
              border: Border(left: BorderSide(color: c.lavender, width: 4)),
            )
          : null,
      padding: EdgeInsets.fromLTRB(mine ? 10 : 14, 8, 14, 4),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          ClipPath(
            clipper: const CutCornerClipper(6),
            child: Container(
              width: 30,
              height: 30,
              color: fill,
              alignment: Alignment.center,
              child: Text(
                name.characters.first,
                style: archivo(size: 14, weight: 900, color: c.on(fill)),
              ),
            ),
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Text(
                      name,
                      style: TextStyle(
                        fontWeight: FontWeight.w700,
                        fontSize: 14,
                        color: c.ink,
                      ),
                    ),
                    const SizedBox(width: 8),
                    Text(
                      time,
                      style: monoStyle(context, size: 10, color: c.faint),
                    ),
                  ],
                ),
                const SizedBox(height: 2),
                child,
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _Poll extends StatelessWidget {
  const _Poll({required this.question, required this.a, required this.b});
  final String question;
  final String a;
  final String b;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    Widget option(String label, double share, bool mine) => Container(
      margin: const EdgeInsets.only(top: 4),
      height: 30,
      color: c.raised,
      child: Stack(
        children: [
          FractionallySizedBox(
            widthFactor: share,
            child: Container(
              decoration: BoxDecoration(
                color: pollBar(c, mine: mine),
                border: mine
                    ? Border(left: BorderSide(color: c.cyan, width: 4))
                    : null,
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 10),
            child: Row(
              children: [
                if (mine) ...[
                  Glyph(ChurrIcons.check, size: 14, color: c.ink),
                  const SizedBox(width: 4),
                ],
                Expanded(
                  child: Text(
                    label,
                    style: TextStyle(
                      color: c.ink,
                      fontWeight: mine ? FontWeight.w800 : FontWeight.w500,
                    ),
                  ),
                ),
                Text(
                  '${(share * 100).round()}%',
                  style: monoStyle(context, color: c.ink),
                ),
              ],
            ),
          ),
        ],
      ),
    );
    return Container(
      constraints: const BoxConstraints(maxWidth: 360),
      color: c.panel,
      padding: const EdgeInsets.all(10),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            question,
            style: TextStyle(color: c.ink, fontWeight: FontWeight.w700),
          ),
          option(a, 0.6, true),
          option(b, 0.4, false),
        ],
      ),
    );
  }
}
