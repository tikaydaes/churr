import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../../core/conversation/threads.dart';
import '../../../core/models/message.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/message_actions.dart';
import '../../message/markdown_view.dart';
import '../../message/message_tile.dart';
import '../../theme/shapes.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import '../../theme/churr_icons.dart';
import 'keep_focus.dart';
import 'message_menu.dart';

/// One message in the conversation or the thread pane: the message
/// itself (`MessageTile`) with, around it, what the conversation adds —
/// the flat view's reply header, the quote of a reply, the hover
/// actions on desktop, the long-press sheet on touch, a right-click
/// menu, and keyboard access to all of them.
class ConversationMessage extends StatefulWidget {
  const ConversationMessage({
    super.key,
    required this.host,
    required this.message,
    required this.markdown,
    this.showAuthor = true,
    this.replyHeader,
    this.quoted,
    this.highlighted = false,
    this.showThreadLine = true,
  });

  final MessageHost host;
  final Message message;
  final MarkdownContext markdown;
  final bool showAuthor;

  /// Set for a reply in the flat view.
  final ReplyHeader? replyHeader;

  /// The message a reply quotes, when it is cached.
  final Message? quoted;

  /// Marked for a moment after a jump to it.
  final bool highlighted;

  /// False in the thread pane, whose header already says it all.
  final bool showThreadLine;

  @override
  State<ConversationMessage> createState() => _ConversationMessageState();
}

class _ConversationMessageState extends State<ConversationMessage> {
  bool hovered = false;
  bool focused = false;
  String selected = '';

  Message get m => widget.message;

  @override
  void dispose() {
    KeepFocus.noteSelection(this, false);
    super.dispose();
  }

  bool get _touch => switch (Theme.of(context).platform) {
    TargetPlatform.android || TargetPlatform.iOS => true,
    _ => false,
  };

  /// The selection as a range of the message's text: the first place
  /// the selected characters occur. A selection across formatting that
  /// is not in the text as written quotes the whole message.
  (int?, int?) get _range {
    final s = selected.trim();
    if (s.isEmpty) return (null, null);
    final at = m.text.indexOf(s);
    if (at < 0) return (null, null);
    return (at, at + s.length);
  }

  void _menu([Offset? at]) {
    final (start, end) = _range;
    showMessageActions(
      context,
      widget.host,
      m,
      position: at,
      selectionStart: start,
      selectionEnd: end,
    );
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final host = widget.host;
    final tile = MessageTile(
      server: host.server,
      message: m,
      markdown: widget.markdown,
      showAuthor: widget.showAuthor || widget.replyHeader != null,
      onReact: host.conversation?.membership == null
          ? null
          : (e) => runReact(e),
      onOpenThread: widget.showThreadLine ? () => host.openThread(m.id) : null,
      above: m.quote == null
          ? null
          : QuoteBlock(
              quote: m.quote!,
              quoted: widget.quoted,
              onTap: () => host.jumpTo(m.quote!.messageId),
            ),
    );
    final selectable = _touch || m.isSystem
        ? tile
        : SelectionArea(
            onSelectionChanged: (s) {
              selected = s?.plainText ?? '';
              // While text is selected a click does not take focus away
              // from it (keep_focus.dart).
              KeepFocus.noteSelection(this, selected.isNotEmpty);
            },
            // A right-click opens the message's actions instead (below).
            contextMenuBuilder: (_, _) => const SizedBox.shrink(),
            child: tile,
          );
    final body = Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      mainAxisSize: MainAxisSize.min,
      children: [
        if (widget.replyHeader != null)
          ReplyHeaderLine(header: widget.replyHeader!, host: host),
        if (m.late) const LateTag(),
        // Not yet passed on to the other servers of a shared
        // conversation: provisional, so dashed. It is a message like any
        // other in every other way.
        if (m.unsent)
          Semantics(
            key: const Key('unsent'),
            label: L10n.of(context).notSentToOtherServers,
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
              child: DashedBox(color: c.edge, child: selectable),
            ),
          )
        else
          selectable,
      ],
    );
    if (m.isSystem) return body;
    final showBar = !_touch && (hovered || focused);
    return FocusableActionDetector(
      onShowFocusHighlight: (v) => setState(() => focused = v),
      shortcuts: const {
        SingleActivator(LogicalKeyboardKey.enter): ActivateIntent(),
        SingleActivator(LogicalKeyboardKey.contextMenu): ActivateIntent(),
        SingleActivator(LogicalKeyboardKey.f10, shift: true): ActivateIntent(),
      },
      actions: {
        ActivateIntent: CallbackAction<ActivateIntent>(
          onInvoke: (_) => _menu(),
        ),
      },
      child: MouseRegion(
        onEnter: (_) => setState(() => hovered = true),
        onExit: (_) => setState(() => hovered = false),
        child: Listener(
          onPointerDown: (e) {
            if (!_touch && e.buttons == kSecondaryMouseButton) {
              _menu(e.position);
            }
          },
          child: GestureDetector(
            behavior: HitTestBehavior.translucent,
            onLongPress: _touch ? _menu : null,
            child: Container(
              decoration: BoxDecoration(
                color: widget.highlighted
                    ? c.raised
                    : hovered && !_touch
                    ? c.panel
                    : null,
                border: focused ? Border.all(color: c.focus, width: 3) : null,
              ),
              child: Stack(
                children: [
                  body,
                  if (showBar)
                    Positioned(
                      top: 0,
                      right: 12,
                      child: _HoverBar(
                        canReact:
                            host.conversation?.membership != null &&
                            !m.isDeleted,
                        canReply: host.conversation?.membership != null,
                        onReact: () async {
                          final e = await pickReaction(context, host.server);
                          if (e != null) runReact(e);
                        },
                        onReply: () {
                          final (start, end) = _range;
                          host.reply(m, start: start, end: end);
                        },
                        onMore: (at) => _menu(at),
                      ),
                    ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  Future<void> runReact(String emoji) async {
    final messenger = ScaffoldMessenger.maybeOf(context);
    try {
      await MessageActions(widget.host.server).react(m, emoji);
    } on Exception catch (e) {
      messenger?.showSnackBar(SnackBar(content: Text('$e')));
    }
  }
}

/// The tag on a message that arrived late (docs/EVENTS.md section 8): it
/// sits at its place by `order`, up among messages that came in before
/// it, and this says why it was not there before.
class LateTag extends StatelessWidget {
  const LateTag({super.key});

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    return Padding(
      key: const Key('late-tag'),
      padding: const EdgeInsets.fromLTRB(62, 6, 16, 0),
      child: Align(
        alignment: AlignmentDirectional.centerStart,
        child: Tooltip(
          message: t.arrivedLateExplained,
          excludeFromSemantics: true,
          child: Semantics(
            label: t.arrivedLateExplained,
            excludeSemantics: true,
            child: Tag(t.arrivedLate, degrees: -2),
          ),
        ),
      ),
    );
  }
}

class _HoverBar extends StatelessWidget {
  const _HoverBar({
    required this.canReact,
    required this.canReply,
    required this.onReact,
    required this.onReply,
    required this.onMore,
  });
  final bool canReact;
  final bool canReply;
  final VoidCallback onReact;
  final VoidCallback onReply;
  final void Function(Offset) onMore;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Container(
      key: const Key('hover-actions'),
      // It floats over the message: the hard offset block.
      decoration: floatingBox(c),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (canReact)
            IconButton(
              visualDensity: VisualDensity.compact,
              tooltip: 'React',
              icon: const Glyph(ChurrIcons.addReaction, size: 18),
              onPressed: onReact,
            ),
          if (canReply)
            IconButton(
              visualDensity: VisualDensity.compact,
              tooltip: 'Reply',
              icon: const Glyph(ChurrIcons.reply, size: 18),
              onPressed: onReply,
            ),
          Builder(
            builder: (context) => IconButton(
              key: const Key('more-actions'),
              visualDensity: VisualDensity.compact,
              tooltip: 'More actions',
              icon: const Glyph(ChurrIcons.moreH, size: 18),
              onPressed: () {
                final box = context.findRenderObject()! as RenderBox;
                onMore(box.localToGlobal(box.size.bottomLeft(Offset.zero)));
              },
            ),
          ),
        ],
      ),
    );
  }
}

/// The quote above a reply: the quoted range of the quoted message's
/// current text, or "original message changed" (FEATURES.md 1.1).
class QuoteBlock extends StatelessWidget {
  const QuoteBlock({
    super.key,
    required this.quote,
    required this.quoted,
    this.onTap,
  });
  final Quote quote;
  final Message? quoted;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final text = quoteText(quoted, quote);
    return Padding(
      padding: const EdgeInsets.only(bottom: 4),
      child: InkWell(
        onTap: onTap,
        child: Container(
          key: const Key('quote'),
          decoration: BoxDecoration(
            color: c.raised,
            border: Border(left: BorderSide(color: c.edge, width: 3)),
          ),
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
          child: Text(
            text ?? 'original message changed',
            maxLines: 4,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(
              fontSize: 13,
              color: c.dim,
              fontStyle: text == null ? FontStyle.italic : null,
            ),
          ),
        ),
      ),
    );
  }
}

/// The header of a reply in the flat view (9.3): who it answers and the
/// start of that message, as a link up to it; a link to the top message
/// when that is a different one; and "Open thread".
class ReplyHeaderLine extends StatelessWidget {
  const ReplyHeaderLine({super.key, required this.header, required this.host});
  final ReplyHeader header;
  final MessageHost host;

  String _start(Message? m) {
    if (m == null) return 'an earlier message';
    if (m.isDeleted) return 'a deleted message';
    final t = m.text.replaceAll('\n', ' ').trim();
    return t.length <= 60 ? t : '${t.substring(0, 59)}…';
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final who = header.answered == null
        ? null
        : host.server.accounts[header.answered!.authorId]?.name;
    final small = TextStyle(fontSize: 12, color: c.dim);
    final link = TextStyle(fontSize: 12, color: c.link);
    return Padding(
      key: const Key('reply-header'),
      padding: const EdgeInsets.fromLTRB(62, 6, 16, 0),
      child: Wrap(
        crossAxisAlignment: WrapCrossAlignment.center,
        spacing: 6,
        children: [
          Glyph(ChurrIcons.subArrow, size: 14, color: c.dim),
          InkWell(
            onTap: () => host.jumpTo(header.answeredId),
            child: Text.rich(
              TextSpan(
                children: [
                  TextSpan(
                    text: 'Replying to ${who ?? 'a message'}: ',
                    style: small,
                  ),
                  TextSpan(text: _start(header.answered), style: link),
                ],
              ),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          if (header.showsParentLink)
            InkWell(
              onTap: () => host.jumpTo(header.parentId),
              child: Text(
                'in the thread of: ${_start(header.parent)}',
                style: link,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
              ),
            ),
          InkWell(
            onTap: () => host.openThread(header.parentId),
            child: Text(
              'Open thread',
              style: link.copyWith(fontWeight: FontWeight.w700),
            ),
          ),
        ],
      ),
    );
  }
}

/// The moving marker (FEATURES.md 1.3): one line where a thread's latest
/// reply would sit. Selecting it opens the thread; its menu has Follow
/// and Ignore.
class MarkerLine extends StatelessWidget {
  const MarkerLine({
    super.key,
    required this.marker,
    required this.host,
    required this.following,
  });
  final MarkerEntry marker;
  final MessageHost host;
  final bool following;

  Future<void> _set(BuildContext context, String to) async {
    final messenger = ScaffoldMessenger.maybeOf(context);
    try {
      await MessageActions(host.server).setThreadState(
        marker.parent,
        to,
        readSeq: marker.parent.thread?.lastReplySeq ?? 0,
      );
    } on Exception catch (e) {
      messenger?.showSnackBar(SnackBar(content: Text('$e')));
    }
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final unread = marker.unread > 0;
    return Semantics(
      button: true,
      label: '${marker.label}. Opens the thread.',
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 4, 8, 4),
        child: Row(
          children: [
            Expanded(
              // The guide's moving marker: raised, a yellow bar at its
              // left edge, the count of new replies in yellow-ink.
              child: MovingMarker(
                key: Key('marker-${marker.parent.id}'),
                onTap: () => host.openThread(marker.parent.id),
                count: unread
                    ? '${marker.unread} new ${marker.unread == 1 ? 'reply' : 'replies'}'
                    : 'Replies',
                excerpt: 'to: ${marker.title}',
              ),
            ),
            PopupMenuButton<String>(
              popUpAnimationStyle: AnimationStyle.noAnimation,
              tooltip: 'Thread options',
              icon: Glyph(ChurrIcons.moreV, size: 18, color: c.dim),
              onSelected: (v) => _set(context, v),
              itemBuilder: (_) => [
                following
                    ? const PopupMenuItem(
                        value: 'none',
                        child: Text('Stop following'),
                      )
                    : const PopupMenuItem(
                        value: 'following',
                        child: Text('Follow thread'),
                      ),
                const PopupMenuItem(
                  value: 'ignored',
                  child: Text('Ignore thread'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
