import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../../core/conversation/emoji_data.dart';
import '../../../core/models/server.dart';
import '../../../state/server_state.dart';
import '../../message/server_image.dart';
import '../../theme/shapes.dart';
import '../../theme/tokens.dart';
import '../../theme/churr_icons.dart';

Future<EmojiData>? _emoji;

/// The standard emoji, loaded once from `assets/emoji.json`.
Future<EmojiData> loadEmojiData() => _emoji ??= rootBundle
    .loadString('assets/emoji.json')
    .then(EmojiData.parse, onError: (Object _) => EmojiData.empty);

/// Draws a reaction or picker value: a standard emoji is its characters,
/// a custom one (`:name:`) the server's image.
Widget emojiGlyph(ServerState server, String value, {double size = 20}) {
  if (value.length > 2 && value.startsWith(':') && value.endsWith(':')) {
    final name = value.substring(1, value.length - 1);
    for (final e in server.emoji.values) {
      if (e.name == name) {
        return ServerImage(
          server: server,
          path: e.url,
          width: size,
          height: size,
          label: value,
        );
      }
    }
    return Text(value, style: TextStyle(fontSize: size * 0.6));
  }
  return Text(value, style: TextStyle(fontSize: size * 0.8, height: 1.1));
}

/// The emoji picker (docs/CLIENTS.md 9.5): search, the ones used lately,
/// the server's custom set, then the standard groups. Gives back a
/// standard emoji's characters or `:name:` for a custom one.
Future<String?> showEmojiPicker(
  BuildContext context,
  ServerState server, {
  List<String> recent = const [],
}) => showDialog<String>(
  animationStyle: AnimationStyle.noAnimation,
  context: context,
  builder: (_) => Dialog(
    child: SizedBox(
      width: 420,
      height: 460,
      child: EmojiPicker(server: server, recent: recent),
    ),
  ),
);

class EmojiPicker extends StatefulWidget {
  const EmojiPicker({super.key, required this.server, this.recent = const []});
  final ServerState server;
  final List<String> recent;

  @override
  State<EmojiPicker> createState() => _EmojiPickerState();
}

class _EmojiPickerState extends State<EmojiPicker> {
  final search = TextEditingController();
  EmojiData data = EmojiData.empty;

  @override
  void initState() {
    super.initState();
    loadEmojiData().then((d) {
      if (mounted) setState(() => data = d);
    });
    search.addListener(() => setState(() {}));
  }

  @override
  void dispose() {
    search.dispose();
    super.dispose();
  }

  List<CustomEmoji> get _custom =>
      [for (final e in widget.server.emoji.values) e]
        ..sort((a, b) => a.name.compareTo(b.name));

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final q = search.text.trim().toLowerCase().replaceAll(':', '');
    final sections = <(String, List<(String value, String label)>)>[];
    if (q.isNotEmpty) {
      sections.add((
        'Custom',
        [
          for (final e in _custom)
            if (e.name.contains(q)) (':${e.name}:', ':${e.name}:'),
        ],
      ));
      sections.add((
        'Found',
        [for (final e in data.search(q, limit: 200)) (e.char, ':${e.code}:')],
      ));
    } else {
      sections.add((
        'Used lately',
        [
          for (final r in widget.recent)
            (r, data.codeOf(r) == null ? r : ':${data.codeOf(r)}:'),
        ],
      ));
      sections.add((
        'Custom',
        [for (final e in _custom) (':${e.name}:', ':${e.name}:')],
      ));
      for (final g in data.groups) {
        sections.add((
          g.name,
          [for (final e in g.emoji) (e.char, ':${e.code}:')],
        ));
      }
    }
    final visible = [
      for (final s in sections)
        if (s.$2.isNotEmpty) s,
    ];
    final first = visible.isEmpty || visible.first.$2.isEmpty
        ? null
        : visible.first.$2.first.$1;
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.all(8),
          child: TextField(
            key: const Key('emoji-search'),
            controller: search,
            autofocus: true,
            decoration: const InputDecoration(
              hintText: 'Search emoji',
              prefixIcon: Glyph(ChurrIcons.search),
            ),
            onSubmitted: (_) {
              if (first != null) Navigator.of(context).pop(first);
            },
          ),
        ),
        Expanded(
          child: visible.isEmpty
              ? Center(
                  child: Text(
                    'No emoji by that name.',
                    style: TextStyle(color: c.dim),
                  ),
                )
              : CustomScrollView(
                  slivers: [
                    for (final (name, cells) in visible) ...[
                      SliverToBoxAdapter(
                        child: Padding(
                          padding: const EdgeInsets.fromLTRB(10, 8, 10, 4),
                          child: Semantics(
                            header: true,
                            child: Text(
                              name.toUpperCase(),
                              style: ChurrType.label(ChurrColors.of(context).dim, size: 11),
                            ),
                          ),
                        ),
                      ),
                      SliverPadding(
                        padding: const EdgeInsets.symmetric(horizontal: 6),
                        sliver: SliverGrid.builder(
                          gridDelegate:
                              const SliverGridDelegateWithMaxCrossAxisExtent(
                                maxCrossAxisExtent: 44,
                              ),
                          itemCount: cells.length,
                          itemBuilder: (context, i) => Tooltip(
                            message: cells[i].$2,
                            child: InkWell(
                              onTap: () =>
                                  Navigator.of(context).pop(cells[i].$1),
                              child: Semantics(
                                button: true,
                                label: cells[i].$2,
                                excludeSemantics: true,
                                child: Center(
                                  child: emojiGlyph(
                                    widget.server,
                                    cells[i].$1,
                                    size: 26,
                                  ),
                                ),
                              ),
                            ),
                          ),
                        ),
                      ),
                    ],
                  ],
                ),
        ),
      ],
    );
  }
}

/// One entry of a composer picker (people, channels, emoji, commands).
class Suggestion {
  const Suggestion({
    required this.title,
    this.detail,
    this.leading,
    this.notice,
    this.key,
  });
  final String title;
  final String? detail;
  final Widget? leading;

  /// A warning drawn under the entry (not notified, do not disturb).
  final String? notice;
  final Key? key;
}

/// The list a composer picker shows above the box. The highlighted entry
/// moves with the arrow keys (the composer handles them) and is chosen
/// with Enter or Tab.
class SuggestionList extends StatelessWidget {
  const SuggestionList({
    super.key,
    required this.title,
    required this.items,
    required this.selected,
    required this.onPick,
    this.onDismiss,
  });

  final String title;
  final List<Suggestion> items;
  final int selected;
  final void Function(int index) onPick;
  final VoidCallback? onDismiss;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Container(
      key: const Key('suggestions'),
      constraints: const BoxConstraints(maxHeight: 280),
      // It floats over the conversation: the hard offset block.
      decoration: floatingBox(c),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(10, 2, 2, 2),
            child: Row(
              children: [
                Expanded(
                  child: Text(title, style: ChurrType.label(c.dim, size: 11)),
                ),
                if (onDismiss != null)
                  IconButton(
                    tooltip: 'Dismiss',
                    visualDensity: VisualDensity.compact,
                    icon: const Glyph(ChurrIcons.close, size: 16),
                    onPressed: onDismiss,
                  ),
              ],
            ),
          ),
          Flexible(
            child: ListView.builder(
              shrinkWrap: true,
              itemCount: items.length,
              itemBuilder: (context, i) {
                final s = items[i];
                final on = i == selected;
                return Material(
                  color: on ? c.raised : c.panel,
                  child: InkWell(
                    key: s.key,
                    onTap: () => onPick(i),
                    child: Container(
                      decoration: BoxDecoration(
                        border: Border(
                          left: BorderSide(
                            color: on ? c.cyan : c.panel,
                            width: 3,
                          ),
                        ),
                      ),
                      constraints: const BoxConstraints(minHeight: 44),
                      padding: const EdgeInsets.symmetric(
                        horizontal: 10,
                        vertical: 4,
                      ),
                      child: Row(
                        children: [
                          if (s.leading != null) ...[
                            s.leading!,
                            const SizedBox(width: 10),
                          ],
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                Text(
                                  s.title,
                                  overflow: TextOverflow.ellipsis,
                                  style: const TextStyle(
                                    fontWeight: FontWeight.w700,
                                  ),
                                ),
                                if (s.detail != null)
                                  Text(
                                    s.detail!,
                                    maxLines: 2,
                                    overflow: TextOverflow.ellipsis,
                                    style: TextStyle(
                                      fontSize: 12,
                                      color: c.dim,
                                    ),
                                  ),
                                if (s.notice != null)
                                  Row(
                                    children: [
                                      Glyph(
                                        ChurrIcons.info,
                                        size: 12,
                                        color: c.yellowInk,
                                      ),
                                      const SizedBox(width: 4),
                                      Flexible(
                                        child: Text(
                                          s.notice!,
                                          style: TextStyle(
                                            fontSize: 12,
                                            color: c.yellowInk,
                                          ),
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
                );
              },
            ),
          ),
        ],
      ),
    );
  }
}
