import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../core/markdown/nodes.dart';
import '../../core/markdown/parser.dart';
import '../../core/models/account.dart';
import '../../core/models/conversation.dart';
import '../../core/models/message.dart';
import '../../core/models/server.dart';
import 'channel_card.dart';
import '../theme/tokens.dart';
import '../theme/churr_icons.dart';
import 'highlight.dart';

/// What drawing a message's text needs to know about the server it is
/// on: the current names behind its entities, and what happens when a
/// link, a channel or a tick-box is pressed.
///
/// Names come from here at draw time and never from the text, so a
/// renamed account or channel shows its new name in old messages.
class MarkdownContext {
  const MarkdownContext({
    this.accounts = const {},
    this.groups = const {},
    this.emoji = const {},
    this.conversation,
    this.myId,
    this.onOpenLink,
    this.onOpenChannel,
    this.onToggleTask,
    this.emojiImage,
    this.taskDetail,
  });

  final Map<String, Account> accounts;
  final Map<String, Group> groups;
  final Map<String, CustomEmoji> emoji;

  /// The conversation with this id, if the reader can see it.
  final Conversation? Function(String id)? conversation;
  final String? myId;

  /// Links always open outside the app.
  final void Function(String url)? onOpenLink;
  final void Function(String conversationId)? onOpenChannel;

  /// Null where the reader may not tick (a reader-level member, a card).
  final void Function(String itemId, bool checked)? onToggleTask;

  /// Draws a custom emoji's image. Null in tests and wherever images
  /// cannot be fetched; the emoji is then drawn as its `:name:`.
  final Widget Function(CustomEmoji emoji, double size)? emojiImage;

  /// Drawn under a checklist item in place of the plain "who ticked it":
  /// who and when, and the way to the item's history. Set by the
  /// checklist code (lib/ui/message/checklist.dart).
  final Widget Function(String itemId, TaskState? state)? taskDetail;
}

/// Draws Churr Markdown (docs/MESSAGE-FORMAT.md section 2) as widgets,
/// from the text and the entities the server sent.
///
/// The parser makes the tree from the text alone; candidates in it
/// (mentions, channel links, emoji, bare links) are drawn as what they
/// are only where the server stored an entity at the same position.
/// One that has no entity is drawn as the text it covers, except a
/// channel link, which reads "unknown channel". The client never
/// resolves a name itself.
class MarkdownView extends StatelessWidget {
  const MarkdownView({
    super.key,
    required this.text,
    this.entities = const [],
    this.tasks = const [],
    this.context_ = const MarkdownContext(),
    this.inlineOnly = false,
    this.style,
    this.lead,
  });

  final String text;
  final List<Entity> entities;
  final List<TaskState> tasks;
  final MarkdownContext context_;

  /// For a card field's value: inline constructs only, as one paragraph.
  final bool inlineOnly;
  final TextStyle? style;

  /// Drawn before the text, with [inlineOnly]: the author's name of a /me
  /// message (ruling R67).
  final InlineSpan? lead;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final base = (style ?? DefaultTextStyle.of(context).style).copyWith(
      height: 1.4,
    );
    final r = _Renderer(this, c, base);
    if (inlineOnly) {
      return Text.rich(
        TextSpan(children: [?lead, ...r.spans(parseInline(text), base)]),
      );
    }
    final blocks = parseMarkdown(text).blocks;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: r.blocks(blocks),
    );
  }
}

class _Renderer {
  _Renderer(this.view, this.c, this.base) {
    for (final e in view.entities) {
      if (e.type == 'task') {
        taskAt[e.at] = e;
      } else {
        entityAt[e.at] = e;
      }
    }
  }

  final MarkdownView view;
  final ChurrColors c;
  final TextStyle base;
  final Map<int, Entity> entityAt = {};
  final Map<int, Entity> taskAt = {};

  MarkdownContext get mc => view.context_;

  List<Widget> blocks(List<Block> blocks) {
    final out = <Widget>[];
    for (var i = 0; i < blocks.length; i++) {
      if (i > 0) out.add(const SizedBox(height: 6));
      out.add(block(blocks[i]));
    }
    return out;
  }

  Widget block(Block b) {
    switch (b.kind) {
      case BlockKind.paragraph:
        return Text.rich(TextSpan(children: spans(b.inlines, base)));
      case BlockKind.heading:
        final size = const [0.0, 22.0, 18.0, 16.0][b.level];
        final s = base.copyWith(
          fontSize: size,
          fontWeight: FontWeight.w800,
          height: 1.25,
        );
        return Padding(
          padding: const EdgeInsets.only(top: 2),
          child: Text.rich(TextSpan(children: spans(b.inlines, s)), style: s),
        );
      case BlockKind.quote:
        return Container(
          decoration: BoxDecoration(
            color: c.raised,
            border: Border(left: BorderSide(color: c.edge, width: 3)),
          ),
          padding: const EdgeInsets.fromLTRB(10, 6, 10, 6),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: blocks(b.blocks),
          ),
        );
      case BlockKind.list:
        return list(b);
      case BlockKind.codeBlock:
        return _CodeBlock(code: b.code, language: b.lang);
      case BlockKind.table:
        return table(b);
      case BlockKind.rule:
        return Divider(color: c.line, height: 13);
    }
  }

  Widget list(Block b) {
    final rows = <Widget>[];
    for (var i = 0; i < b.items.length; i++) {
      final it = b.items[i];
      final entity = it.task ? taskAt[it.textAt] : null;
      final state = entity?.itemId == null ? null : _task(entity!.itemId!);
      // After posting, the state lives in `tasks`, not in the text.
      final checked = state?.checked ?? it.checked;
      Widget marker;
      if (it.task) {
        final toggle = mc.onToggleTask;
        marker = SizedBox(
          width: 22,
          height: 22,
          child: Checkbox(
            value: checked,
            materialTapTargetSize: MaterialTapTargetSize.shrinkWrap,
            visualDensity: VisualDensity.compact,
            onChanged: entity?.itemId == null || toggle == null
                ? null
                : (v) => toggle(entity!.itemId!, v ?? false),
          ),
        );
      } else {
        marker = SizedBox(
          width: 22,
          child: Text(
            b.ordered ? '${b.start + i}.' : '•',
            style: base.copyWith(color: c.dim),
          ),
        );
      }
      final textStyle = it.task && checked
          ? base.copyWith(decoration: TextDecoration.lineThrough, color: c.dim)
          : base;
      final detail = mc.taskDetail == null || entity?.itemId == null
          ? null
          : mc.taskDetail!(entity!.itemId!, state);
      final who =
          detail == null && state?.checked == true && state?.checkedById != null
          ? mc.accounts[state!.checkedById]?.name
          : null;
      rows.add(
        Padding(
          padding: const EdgeInsets.symmetric(vertical: 1),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              marker,
              const SizedBox(width: 4),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text.rich(
                      TextSpan(
                        children: [
                          ...spans(it.inlines, textStyle),
                          // A ticked item is followed by who ticked it.
                          if (who != null)
                            TextSpan(
                              text: '  $who',
                              style: TextStyle(
                                fontFamily: fontMono,
                                fontSize: 10,
                                color: c.dim,
                                decoration: TextDecoration.none,
                              ),
                            ),
                        ],
                      ),
                      style: textStyle,
                    ),
                    ?detail,
                    for (final sub in it.lists) list(sub),
                  ],
                ),
              ),
            ],
          ),
        ),
      );
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: rows,
    );
  }

  TaskState? _task(String id) {
    for (final t in view.tasks) {
      if (t.id == id) return t;
    }
    return null;
  }

  Widget table(Block b) {
    TextAlign align(int i) => switch (b.align[i]) {
      CellAlign.center => TextAlign.center,
      CellAlign.right => TextAlign.right,
      _ => TextAlign.left,
    };
    TableRow row(List<Cell> cells, {required bool head}) => TableRow(
      decoration: head ? BoxDecoration(color: c.raised) : null,
      children: [
        for (var i = 0; i < cells.length; i++)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
            child: Text.rich(
              TextSpan(
                children: spans(
                  cells[i].inlines,
                  head ? base.copyWith(fontWeight: FontWeight.w700) : base,
                ),
              ),
              textAlign: align(i),
            ),
          ),
      ],
    );
    // On a narrow screen a table scrolls sideways inside the message.
    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      child: Table(
        defaultColumnWidth: const IntrinsicColumnWidth(),
        border: TableBorder.all(color: c.line, width: 1.5),
        children: [
          row(b.head, head: true),
          for (final r in b.rows) row(r, head: false),
        ],
      ),
    );
  }

  List<InlineSpan> spans(List<Inline> inlines, TextStyle style) => [
    for (final s in inlines) span(s, style),
  ];

  String _source(Inline s) => view.text.substring(s.at, s.end);

  InlineSpan span(Inline s, TextStyle style) {
    switch (s.kind) {
      case InlineKind.text:
        return TextSpan(text: s.text, style: style);
      case InlineKind.escape:
        return TextSpan(text: s.text, style: style);
      case InlineKind.lineBreak:
        return const TextSpan(text: '\n');
      case InlineKind.bold:
        return _nested(s, style.copyWith(fontWeight: FontWeight.w700));
      case InlineKind.italic:
        return _nested(s, style.copyWith(fontStyle: FontStyle.italic));
      case InlineKind.strike:
        return _nested(
          s,
          style.copyWith(decoration: TextDecoration.lineThrough),
        );
      case InlineKind.spoiler:
        return WidgetSpan(
          alignment: PlaceholderAlignment.baseline,
          baseline: TextBaseline.alphabetic,
          child: _Spoiler(child: Text.rich(_nested(s, style))),
        );
      case InlineKind.code:
        return TextSpan(
          text: s.text,
          style: style.copyWith(
            fontFamily: fontMono,
            fontSize: (style.fontSize ?? 14) * 0.9,
            backgroundColor: c.raised,
          ),
        );
      case InlineKind.link:
        return _link(
          s.url,
          spans(s.children, _linkStyle(style)),
          style,
          shown: _plain(s.children),
        );
      case InlineKind.image:
        // Images are not supported: an image is a link to its address.
        return _link(s.url, [TextSpan(text: s.url)], style);
      case InlineKind.noLink:
        // A link with a scheme the format does not allow is its text.
        return TextSpan(children: spans(s.children, style));
      case InlineKind.bareLink:
        final e = entityAt[s.at];
        if (e?.type != 'link') return TextSpan(text: _source(s), style: style);
        return _link(e!.url ?? s.url, [TextSpan(text: _source(s))], style);
      case InlineKind.mention:
        return _mention(s, style);
      case InlineKind.channel:
        return _channel(s, style);
      case InlineKind.emoji:
        final e = entityAt[s.at];
        final custom = e?.type == 'emoji' ? mc.emoji[e!.emojiId] : null;
        final image = mc.emojiImage;
        if (custom == null || image == null) {
          // A standard short code needs the emoji list (assets/emoji.json),
          // which comes with the emoji picker; until then it is as typed.
          return TextSpan(text: _source(s), style: style);
        }
        final size = (style.fontSize ?? 14) * 1.4;
        return WidgetSpan(
          alignment: PlaceholderAlignment.middle,
          child: Tooltip(
            message: ':${custom.name}:',
            child: image(custom, size),
          ),
        );
    }
  }

  TextSpan _nested(Inline s, TextStyle style) =>
      TextSpan(children: spans(s.children, style), style: style);

  TextStyle _linkStyle(TextStyle style) => style.copyWith(
    color: c.link,
    decoration: TextDecoration.underline,
    decorationColor: c.link,
  );

  String _plain(List<Inline> spans) => [
    for (final s in spans)
      s.kind == InlineKind.text ? s.text : _plain(s.children),
  ].join();

  /// A link. When the visible text looks like an address on a different
  /// host from the real one, the real host is shown after it, so a link
  /// cannot pass itself off as another site.
  InlineSpan _link(
    String url,
    List<InlineSpan> children,
    TextStyle style, {
    String? shown,
  }) {
    final open = mc.onOpenLink;
    final host = Uri.tryParse(url)?.host ?? '';
    final shownHost = shown == null
        ? null
        : Uri.tryParse(shown.contains('://') ? shown : 'https://$shown')?.host;
    final looksLikeAddress =
        shown != null &&
        RegExp(r'^[\w.-]+\.[a-z]{2,}(/|$)|://').hasMatch(shown);
    final misleading = looksLikeAddress && host.isNotEmpty && shownHost != host;
    return TextSpan(
      style: _linkStyle(style),
      recognizer: open == null
          ? null
          : (TapGestureRecognizer()..onTap = () => open(url)),
      mouseCursor: SystemMouseCursors.click,
      children: [
        ...children,
        if (misleading)
          TextSpan(
            text: ' ($host)',
            style: TextStyle(color: c.pink, decoration: TextDecoration.none),
          ),
      ],
    );
  }

  InlineSpan _mention(Inline s, TextStyle style) {
    final e = entityAt[s.at];
    String? label;
    var mine = false;
    switch (e?.type) {
      case 'account':
        final a = mc.accounts[e!.accountId];
        // The account's current display name; the name as written when
        // the account is gone or not known on this device.
        label = '@${a?.name ?? s.name}';
        mine = e.accountId != null && e.accountId == mc.myId;
      case 'group':
        final g = mc.groups[e!.groupId];
        label = '@${g?.handle ?? s.name}';
        mine = g?.isMember ?? false;
      case 'broadcast':
        label = '@${e!.scope ?? s.name}';
        mine = true; // Highlighted for everyone.
    }
    if (label == null) return TextSpan(text: _source(s), style: style);
    return TextSpan(
      text: label,
      style: mine
          ? style.copyWith(
              color: c.onLavender,
              backgroundColor: c.lavender,
              fontWeight: FontWeight.w700,
            )
          : style.copyWith(color: c.lavender, fontWeight: FontWeight.w700),
    );
  }

  InlineSpan _channel(Inline s, TextStyle style) {
    final e = entityAt[s.at];
    if (e?.type != 'conversation') {
      // A `<#…>` that names no channel has no entity.
      return TextSpan(
        text: 'unknown channel',
        style: style.copyWith(color: c.dim, fontStyle: FontStyle.italic),
      );
    }
    final conv = mc.conversation?.call(e!.conversationId ?? s.id);
    if (conv == null || conv.name == null) {
      // The author could see it; this reader cannot. No name, no card.
      return TextSpan(
        text: 'private channel',
        style: style.copyWith(color: c.dim, fontStyle: FontStyle.italic),
      );
    }
    final open = mc.onOpenChannel;
    final icon = conv.isLocked
        ? ChurrIcons.locked
        : conv.private
        ? ChurrIcons.private
        : ChurrIcons.hash;
    final parent = conv.parentId == null
        ? null
        : mc.conversation?.call(conv.parentId!)?.name;
    return WidgetSpan(
      alignment: PlaceholderAlignment.baseline,
      baseline: TextBaseline.alphabetic,
      // A card on hover or long press (lib/ui/message/channel_card.dart).
      child: ChannelLinkCard(
        conversation: conv,
        parentName: parent,
        onOpen: open == null ? null : () => open(conv.id),
        child: MouseRegion(
          cursor: SystemMouseCursors.click,
          child: GestureDetector(
            onTap: open == null ? null : () => open(conv.id),
            child: Container(
              color: c.raised,
              padding: const EdgeInsets.symmetric(horizontal: 4),
              child: Text.rich(
                TextSpan(
                  children: [
                    WidgetSpan(
                      alignment: PlaceholderAlignment.middle,
                      child: Glyph(
                        icon,
                        size: (style.fontSize ?? 14),
                        color: c.lavender,
                      ),
                    ),
                    // The channel's current name, so a rename shows
                    // everywhere at once.
                    TextSpan(text: ' ${conv.name}'),
                  ],
                ),
                style: style.copyWith(
                  color: c.lavender,
                  fontWeight: FontWeight.w700,
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// A spoiler: hidden until the reader selects it.
class _Spoiler extends StatefulWidget {
  const _Spoiler({required this.child});
  final Widget child;

  @override
  State<_Spoiler> createState() => _SpoilerState();
}

class _SpoilerState extends State<_Spoiler> {
  bool shown = false;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Semantics(
      label: shown ? null : 'Spoiler, hidden',
      button: !shown,
      child: GestureDetector(
        onTap: () => setState(() => shown = !shown),
        child: Container(
          color: shown ? c.raised : c.edge,
          child: Opacity(opacity: shown ? 1 : 0, child: widget.child),
        ),
      ),
    );
  }
}

/// A code block: monospace, highlighted when its language is on the
/// list, scrolling sideways instead of wrapping, with a copy button.
class _CodeBlock extends StatelessWidget {
  const _CodeBlock({required this.code, required this.language});
  final String code;
  final String language;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Container(
      width: double.infinity,
      // The guide's code block: raised, a cyan bar down its left edge.
      decoration: BoxDecoration(
        color: c.raised,
        border: Border(left: BorderSide(color: c.cyan, width: 3)),
      ),
      child: Stack(
        children: [
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            padding: const EdgeInsets.fromLTRB(12, 10, 44, 10),
            child: Text.rich(
              TextSpan(children: highlightCode(code, language, c)),
              style: TextStyle(
                fontFamily: fontMono,
                fontSize: 12.5,
                height: 1.5,
                color: c.ink,
              ),
              softWrap: false,
            ),
          ),
          Positioned(
            top: 0,
            right: 0,
            child: IconButton(
              tooltip: 'Copy code',
              iconSize: 16,
              icon: const Glyph(ChurrIcons.copy),
              onPressed: () => Clipboard.setData(ClipboardData(text: code)),
            ),
          ),
          if (language.isNotEmpty)
            Positioned(
              bottom: 4,
              right: 8,
              child: Text(language, style: monoStyle(context, size: 9)),
            ),
        ],
      ),
    );
  }
}
