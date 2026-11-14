import 'package:flutter/material.dart';

import '../../../core/api/conversation_ops.dart';
import '../../../core/api/errors.dart';
import '../../../core/conversation/diff.dart';
import '../../../core/models/message.dart';
import '../../../state/message_actions.dart';
import '../../../state/server_state.dart';
import '../../layout/breakpoints.dart';
import '../../message/message_tile.dart' show clockTime;
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import '../../theme/churr_icons.dart';

/// Opens a message's history (docs/CLIENTS.md 9.7): a page on phones, a
/// dialog elsewhere.
Future<void> openHistory(BuildContext context, ServerState server, Message m) {
  final compact =
      WidthClass.of(MediaQuery.sizeOf(context).width) == WidthClass.compact;
  final view = HistoryView(server: server, message: m);
  if (compact) {
    return Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => Scaffold(
          appBar: AppBar(title: const Text('History')),
          body: view,
        ),
      ),
    );
  }
  return showDialog<void>(
  animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (context) => Dialog(
      child: SizedBox(
        width: 620,
        height: 560,
        child: Column(
          children: [
            ListTile(
              title: const Text(
                'History',
                style: TextStyle(fontWeight: FontWeight.w800),
              ),
              trailing: IconButton(
                tooltip: 'Close',
                icon: const Glyph(ChurrIcons.close),
                onPressed: () => Navigator.of(context).pop(),
              ),
            ),
            Expanded(child: view),
          ],
        ),
      ),
    ),
  );
}

/// A message's earlier versions, newest first, each with when it was
/// replaced and by whom and its differences from the version after it.
/// For a deleted message, its last content and who deleted it. Read
/// only. History is not kept on the device, so it is fetched each time.
class HistoryView extends StatefulWidget {
  const HistoryView({super.key, required this.server, required this.message});
  final ServerState server;
  final Message message;

  @override
  State<HistoryView> createState() => _HistoryViewState();
}

class _HistoryViewState extends State<HistoryView> {
  late final Future<List<MessageVersion>> versions = MessageActions(
    widget.server,
  ).history(widget.message);

  String _who(String id) => widget.server.accounts[id]?.name ?? 'Someone';

  String _when(DateTime? t) {
    if (t == null) return '';
    final l = t.toLocal();
    return '${l.day}/${l.month}/${l.year} ${clockTime(t)}';
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return FutureBuilder<List<MessageVersion>>(
      future: versions,
      builder: (context, snap) {
        if (snap.hasError) {
          final e = snap.error;
          final hidden =
              e is ApiException &&
              (e.code == 'not_found' || e.code == 'forbidden');
          return _line(
            context,
            hidden
                ? 'The history of this message is hidden or has been erased.'
                : 'Could not load the history. ($e)',
          );
        }
        if (!snap.hasData) return const Center(child: Busy());
        final list = [...snap.data!]
          ..sort((a, b) => b.version.compareTo(a.version));
        if (list.isEmpty) return _line(context, 'There are no earlier versions. A purged history shows nothing.',);
        // Each version is compared with the one that replaced it: the
        // next newer version, or the message as it now reads.
        return ListView.separated(
          key: const Key('history'),
          padding: const EdgeInsets.all(16),
          itemCount: list.length,
          separatorBuilder: (_, _) => Divider(color: c.line, height: 24),
          itemBuilder: (context, i) {
            final v = list[i];
            final newer = i == 0
                ? (widget.message.isDeleted ? null : widget.message.text)
                : list[i - 1].text;
            final deleted = v.reason == 'delete';
            return Semantics(
              container: true,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    deleted
                        ? 'Last content. Deleted by ${_who(v.replacedById)}, ${_when(v.replacedAt)}'
                        : 'Version ${v.version}. Replaced by ${_who(v.replacedById)}, ${_when(v.replacedAt)}',
                    style: monoStyle(context),
                  ),
                  const SizedBox(height: 6),
                  if (newer == null || deleted)
                    SelectableText(v.text.isEmpty ? '(no text)' : v.text)
                  else
                    _Diff(parts: wordDiff(v.text, newer)),
                  for (final f in v.files)
                    Padding(
                      padding: const EdgeInsets.only(top: 4),
                      child: Row(
                        children: [
                          Glyph(ChurrIcons.attach, size: 14, color: c.dim),
                          const SizedBox(width: 4),
                          Flexible(
                            child: Text(
                              f.name,
                              style: TextStyle(color: c.dim, fontSize: 12),
                            ),
                          ),
                        ],
                      ),
                    ),
                ],
              ),
            );
          },
        );
      },
    );
  }

  Widget _line(BuildContext context, String s) => Center(
    child: Padding(
      padding: const EdgeInsets.all(24),
      child: Text(
        s,
        textAlign: TextAlign.center,
        style: TextStyle(color: ChurrColors.of(context).dim),
      ),
    ),
  );
}

/// The text of a version with what the next version removed struck
/// through in red, and what it added underlined in green. Not by colour
/// alone: the marks are also struck and underlined, and read aloud.
class _Diff extends StatelessWidget {
  const _Diff({required this.parts});
  final List<DiffPart> parts;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Semantics(
      label: [
        for (final p in parts)
          switch (p.op) {
            DiffOp.same => p.text,
            DiffOp.removed => '(removed: ${p.text.trim()}) ',
            DiffOp.added => '(added: ${p.text.trim()}) ',
          },
      ].join(),
      excludeSemantics: true,
      child: SelectableText.rich(
        TextSpan(
          children: [
            for (final p in parts)
              TextSpan(
                text: p.text,
                style: switch (p.op) {
                  DiffOp.same => null,
                  DiffOp.removed => TextStyle(
                    color: c.pink,
                    decoration: TextDecoration.lineThrough,
                  ),
                  DiffOp.added => TextStyle(
                    color: c.cyan,
                    decoration: TextDecoration.underline,
                  ),
                },
              ),
          ],
        ),
      ),
    );
  }
}
