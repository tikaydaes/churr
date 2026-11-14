import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

import '../core/conversation/composer_text.dart';

/// The composer's text controller: plain text in which each channel
/// link chip is one character (see [ChannelChips]), drawn by
/// [chipBuilder] as a chip. The cursor steps over a chip as one unit and
/// one backspace removes it (docs/CLIENTS.md 9.26).
class ChipTextController extends TextEditingController {
  ChipTextController({super.text, ChannelChips? chips})
    : chips = chips ?? ChannelChips();

  final ChannelChips chips;

  /// Draws the chip for a channel id. Set by the composer widget, which
  /// knows the channel's current name and icon. Null draws nothing
  /// special.
  InlineSpan Function(String conversationId, TextStyle? style)? chipBuilder;

  /// The text as it is sent: chips written `<#conversation-id>`.
  String get wireText => chips.decode(text);

  /// Replaces the text with [wire], turning each `<#id>` of a channel
  /// [visible] says the person can see into a chip, and puts the cursor
  /// at the end.
  void setWire(String wire, bool Function(String id) visible) {
    final t = chips.encode(wire, visible);
    value = TextEditingValue(
      text: t,
      selection: TextSelection.collapsed(offset: t.length),
    );
  }

  /// Replaces [start]..[end] with [replacement] and puts the cursor after
  /// it.
  void replaceRange(int start, int end, String replacement) {
    final t = text.replaceRange(start, end, replacement);
    value = TextEditingValue(
      text: t,
      selection: TextSelection.collapsed(offset: start + replacement.length),
    );
  }

  /// Replaces [start]..[end] with a chip for [conversationId] and a
  /// space after it.
  void insertChip(int start, int end, String conversationId) =>
      replaceRange(start, end, '${chips.chipFor(conversationId)} ');

  @override
  TextSpan buildTextSpan({
    required BuildContext context,
    TextStyle? style,
    required bool withComposing,
  }) {
    final builder = chipBuilder;
    if (builder == null || !text.codeUnits.any(ChannelChips.isChip)) {
      return super.buildTextSpan(
        context: context,
        style: style,
        withComposing: withComposing,
      );
    }
    final children = <InlineSpan>[];
    final run = StringBuffer();
    void flush() {
      if (run.isEmpty) return;
      children.add(TextSpan(text: run.toString()));
      run.clear();
    }

    for (final unit in text.codeUnits) {
      final id = ChannelChips.isChip(unit) ? chips.idOf(unit) : null;
      if (id == null) {
        run.writeCharCode(unit);
      } else {
        flush();
        children.add(builder(id, style));
      }
    }
    flush();
    return TextSpan(style: style, children: children);
  }
}

/// Turns pasted `<#conversation-id>` text naming a channel the person
/// can see into chips; anything else is pasted as it is (9.26).
class ChipPasteFormatter extends TextInputFormatter {
  ChipPasteFormatter(this.controller, this.visible);
  final ChipTextController controller;
  final bool Function(String id) visible;

  @override
  TextEditingValue formatEditUpdate(
    TextEditingValue oldValue,
    TextEditingValue newValue,
  ) {
    if (!channelLinkPattern.hasMatch(newValue.text)) return newValue;
    final encoded = controller.chips.encode(newValue.text, visible);
    if (encoded == newValue.text) return newValue;
    final delta = newValue.text.length - encoded.length;
    final at = (newValue.selection.baseOffset - delta).clamp(0, encoded.length);
    return TextEditingValue(
      text: encoded,
      selection: TextSelection.collapsed(offset: at),
    );
  }
}
