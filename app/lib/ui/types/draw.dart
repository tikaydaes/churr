// Drawing the catalogue's values (docs/DESIGN-MODULES.md sections 3.2 and
// 4.1; the web chat's shared/types/draw.js): a message's view and the
// objects in its text, each through its kind's own drawing, or through the
// three-step fallback when this client cannot ([readValue]): the kind,
// else its generic rendition, else the text. The text is always there to
// fall back to, and it is drawn as text: nothing here reads a string as
// markup.

import 'package:flutter/material.dart';

import '../../core/models/message.dart';
import '../../core/types/catalogue.dart';
import '../../l10n/app_localizations.dart';
import '../theme/churr_icons.dart';
import '../theme/tokens.dart';
import 'dice_roll/dice_roll_view.dart';

/// Draws an `object` entity: the value its [typed] characters stand for,
/// in [style]. [live] says how far into its one live spin it is
/// ([ChanceLive]), or is null. A value this client cannot draw is its
/// typed text; one that needs a newer app says so on hover and to a
/// screen reader.
InlineSpan drawObject(
  Entity e,
  String typed,
  TextStyle style, {
  Duration? Function()? live,
}) {
  final r = readValue(valueOf(e));
  switch (r.kind) {
    case 'dice_roll':
      return WidgetSpan(
        alignment: PlaceholderAlignment.middle,
        child: DiceRollChip(
          data: r.data,
          typed: typed,
          style: style,
          at: e.at,
          removed: r.removed,
          live: live,
        ),
      );
  }
  if (r.newer) {
    return WidgetSpan(
      alignment: PlaceholderAlignment.baseline,
      baseline: TextBaseline.alphabetic,
      child: _Newer(typed: typed, style: style, at: e.at),
    );
  }
  return TextSpan(text: typed, style: style);
}

/// What a message's view draws: its kind's [card] in place of the text,
/// or no card, where the message's text stands, and [newer] says to add
/// the note that a newer app shows more ([NewerNote]).
({Widget? card, bool newer}) drawView(Message m) {
  final view = viewOf(m);
  if (view == null) return (card: null, newer: false);
  final r = readValue(view);
  if (r.drawn && !r.removed) {
    switch (r.kind) {
      case 'dice_roll':
        return (
          card: DiceRollCard(messageId: m.id, data: r.data!),
          newer: false,
        );
    }
  }
  return (card: null, newer: r.newer);
}

/// Typed text standing for a value this client cannot draw: the text,
/// marked, and on hover that a newer app would show more.
class _Newer extends StatelessWidget {
  const _Newer({required this.typed, required this.style, required this.at});
  final String typed;
  final TextStyle style;
  final int at;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final tip = L10n.of(context).valueNewerTip;
    return Semantics(
      container: true,
      label: '$typed. $tip',
      excludeSemantics: true,
      child: Tooltip(
        key: Key('object-newer-$at'),
        message: tip,
        child: Text(
          typed,
          style: style.copyWith(
            decoration: TextDecoration.underline,
            decorationStyle: TextDecorationStyle.dotted,
            decorationColor: c.dim,
          ),
        ),
      ),
    );
  }
}

/// The built-in state "this needs a newer app" of a card (day-one rule 4),
/// under the text that stands for it.
class NewerNote extends StatelessWidget {
  const NewerNote({super.key});

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Padding(
      key: const Key('needs-newer'),
      padding: const EdgeInsets.only(top: 2),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Glyph(ChurrIcons.info, size: 12, color: c.dim),
          const SizedBox(width: 4),
          Flexible(
            child: Text(
              L10n.of(context).valueNewerNote,
              style: TextStyle(color: c.dim, fontSize: 12),
            ),
          ),
        ],
      ),
    );
  }
}
