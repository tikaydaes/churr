import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:intl/intl.dart';

import '../../core/api/unicode.dart';
import '../../core/models/message.dart';
import '../../core/objects/codepoint.dart';
import '../../core/objects/measure.dart';
import '../../l10n/app_localizations.dart';
import '../layout/breakpoints.dart';
import '../scopes.dart';
import '../theme/tokens.dart';

/// Numbers in another base and code points (MESSAGE-FORMAT.md section
/// 3.2): the controls drawn in a message, and the converters they open,
/// which work like the time converter of an instant.

String _grouped(BuildContext context, String s) {
  final locale = Localizations.maybeLocaleOf(context)?.toString() ?? 'en';
  try {
    final sym = NumberFormat.decimalPattern(locale).symbols;
    return measureSeparators(s, sym.GROUP_SEP, sym.DECIMAL_SEP);
  } on Object {
    return s;
  }
}

/// The server's lookupCodePoints for the server on screen, or null.
Future<CodePointName?> Function(String)? _lookup(BuildContext context) {
  final api = context
      .getInheritedWidgetOfExactType<AppScope>()
      ?.notifier
      ?.selectedServer
      ?.api;
  return api?.lookupCodePoint;
}

/// A control in the text: [children] as a button that opens something.
class _Chip extends StatelessWidget {
  const _Chip({
    super.key,
    required this.label,
    required this.onTap,
    required this.style,
    required this.children,
  });
  final String label;
  final VoidCallback onTap;
  final TextStyle style;
  final List<InlineSpan> children;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Semantics(
      button: true,
      label: label,
      excludeSemantics: true,
      child: InkWell(
        onTap: onTap,
        child: Container(
          color: c.raised,
          padding: const EdgeInsets.symmetric(horizontal: 2),
          child: Text.rich(
            TextSpan(
              style: style.copyWith(
                decoration: TextDecoration.underline,
                decorationStyle: TextDecorationStyle.dotted,
                decorationColor: c.cyan,
              ),
              children: children,
            ),
          ),
        ),
      ),
    );
  }
}

/// A number in another base, as typed, opening the number converter.
class NumberChip extends StatelessWidget {
  const NumberChip({
    super.key,
    required this.entity,
    required this.typed,
    required this.style,
  });
  final Entity entity;
  final String typed;
  final TextStyle style;

  @override
  Widget build(BuildContext context) {
    final r = numberRows('${entity.raw['value']}');
    return _Chip(
      key: Key('number-${entity.at}'),
      label: L10n.of(context)
          .numberChipLabel(typed, _grouped(context, r.decimal)),
      onTap: () => showNumberConverter(context, entity, typed),
      style: style,
      children: [TextSpan(text: typed)],
    );
  }
}

/// A code point as typed, the character beside it in muted text, opening
/// what it is.
class CodePointChip extends StatelessWidget {
  const CodePointChip({
    super.key,
    required this.entity,
    required this.typed,
    required this.style,
  });
  final Entity entity;
  final String typed;
  final TextStyle style;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final value = '${entity.raw['value']}';
    final f = codePointFacts(value);
    return _Chip(
      key: Key('codepoint-${entity.at}'),
      label: L10n.of(context)
          .codePointChipLabel(typed, f.shownKind == 'char' ? f.char : f.shown),
      onTap: () => showCodePoint(context, value, typed),
      style: style,
      children: [
        TextSpan(text: '$typed '),
        WidgetSpan(
          alignment: PlaceholderAlignment.middle,
          child: _Shown(
            facts: f,
            style: style.copyWith(color: c.dim),
          ),
        ),
      ],
    );
  }
}

/// What stands for a code point: the character, a dotted circle and the
/// mark, or a dotted box with a short name for what cannot be seen.
class _Shown extends StatelessWidget {
  const _Shown({required this.facts, required this.style});
  final CodePointFacts facts;
  final TextStyle style;

  @override
  Widget build(BuildContext context) {
    if (facts.shownKind != 'box') {
      return Text(
        facts.shown,
        style: style.copyWith(decoration: TextDecoration.none),
      );
    }
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 3),
      decoration: BoxDecoration(
        border: Border.all(color: style.color ?? Colors.grey),
      ),
      child: Text(
        facts.shown,
        style: style.copyWith(
          fontFamily: fontMono,
          fontSize: (style.fontSize ?? 14) * 0.75,
          fontWeight: FontWeight.w600,
          decoration: TextDecoration.none,
        ),
      ),
    );
  }
}

Future<void> _open(BuildContext context, WidgetBuilder body) {
  final compact =
      WidthClass.of(MediaQuery.sizeOf(context).width) == WidthClass.compact;
  if (compact) {
    return showModalBottomSheet<void>(
      sheetAnimationStyle: AnimationStyle.noAnimation,
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: body,
    );
  }
  return showDialog<void>(
    animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (context) =>
        Dialog(child: SizedBox(width: 440, child: body(context))),
  );
}

/// A row of facts: its value large and what it is; a press copies [copy].
Widget _fact(
  BuildContext context,
  String what,
  String shown,
  String copy, {
  bool mono = false,
  bool mine = false,
  Key? key,
}) {
  final t = L10n.of(context);
  final c = ChurrColors.of(context);
  return Semantics(
    button: true,
    label: t.copyFact(what, copy),
    excludeSemantics: true,
    child: Material(
      color: c.panel,
      child: InkWell(
        key: key,
        onTap: () async {
          await Clipboard.setData(ClipboardData(text: copy));
          if (context.mounted) {
            ScaffoldMessenger.maybeOf(context)
                ?.showSnackBar(SnackBar(content: Text(t.unitCopied(copy))));
          }
        },
        child: _rowBox(c, mine, shown, what, mono: mono),
      ),
    ),
  );
}

Widget _rowBox(
  ChurrColors c,
  bool mine,
  String shown,
  String what, {
  bool mono = false,
}) => Container(
  margin: const EdgeInsets.only(bottom: 2),
  decoration: BoxDecoration(
    border: mine ? Border(left: BorderSide(color: c.lavender, width: 4)) : null,
  ),
  padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
  child: Row(
    crossAxisAlignment: CrossAxisAlignment.baseline,
    textBaseline: TextBaseline.alphabetic,
    children: [
      Expanded(
        child: Text(
          shown,
          style: TextStyle(
            color: c.ink,
            fontWeight: mono ? FontWeight.w500 : FontWeight.w600,
            fontFamily: mono ? fontMono : null,
          ),
        ),
      ),
      const SizedBox(width: 8),
      Text(what, style: TextStyle(color: c.dim, fontSize: 13)),
    ],
  ),
);

Widget _frame(
  BuildContext context,
  String title,
  String typed,
  List<Widget> children,
) {
  final t = L10n.of(context);
  final c = ChurrColors.of(context);
  return SingleChildScrollView(
    padding: const EdgeInsets.all(20),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(
          title.toUpperCase(),
          style: TextStyle(
            fontSize: 20,
            fontWeight: FontWeight.w800,
            color: c.ink,
          ),
        ),
        const SizedBox(height: 8),
        Text(t.instantTypedAs(typed), style: TextStyle(color: c.dim)),
        const SizedBox(height: 12),
        ...children,
      ],
    ),
  );
}

/// Opens the number converter: decimal, hexadecimal, octal and binary,
/// and the code point it is as a character where it is one.
Future<void> showNumberConverter(
  BuildContext context,
  Entity entity,
  String typed,
) {
  return _open(context, (context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final r = numberRows('${entity.raw['value']}');
    final base = entity.raw['base'];
    return _frame(context, t.numberConverterTitle, typed, [
      Container(
        color: c.bg,
        child: Column(
          children: [
            _fact(
              context,
              t.numberDecimal,
              _grouped(context, r.decimal),
              r.copy['decimal']!,
              key: const Key('number-row-decimal'),
            ),
            _fact(
              context,
              t.numberHex,
              r.hex,
              r.copy['hex']!,
              mono: true,
              mine: base == 16,
              key: const Key('number-row-hex'),
            ),
            _fact(
              context,
              t.numberOctal,
              r.octal,
              r.copy['octal']!,
              mono: true,
              mine: base == 8,
              key: const Key('number-row-octal'),
            ),
            _fact(
              context,
              t.numberBinary,
              r.binary,
              r.copy['binary']!,
              mono: true,
              mine: base == 2,
              key: const Key('number-row-binary'),
            ),
          ],
        ),
      ),
      if (r.codepoint != null)
        Align(
          alignment: Alignment.centerLeft,
          child: TextButton(
            key: const Key('number-as-char'),
            onPressed: () {
              final nav = Navigator.of(context);
              final outer = nav.context;
              nav.pop();
              showCodePoint(outer, r.codepoint!, r.codepoint!);
            },
            child: Text(
              t.numberAsChar(codePointFacts(r.codepoint!).shown, r.codepoint!),
            ),
          ),
        ),
    ]);
  });
}

/// Opens what a code point is: the character large, its encodings and
/// escapes, and its name and category, asked of the server now (so the
/// server learns which code point was looked at), once a session.
Future<void> showCodePoint(BuildContext context, String value, String typed) {
  final lookup = _lookup(context);
  return _open(
    context,
    (context) => CodePointSheet(value: value, typed: typed, lookup: lookup),
  );
}

class CodePointSheet extends StatefulWidget {
  const CodePointSheet({
    super.key,
    required this.value,
    required this.typed,
    this.lookup,
  });
  final String value;
  final String typed;
  final Future<CodePointName?> Function(String value)? lookup;

  @override
  State<CodePointSheet> createState() => _CodePointSheetState();
}

class _CodePointSheetState extends State<CodePointSheet> {
  late final Future<CodePointName?> _name = codePointName(
    codePointFacts(widget.value).value,
    widget.lookup,
  );

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final f = codePointFacts(widget.value);
    return _frame(context, t.codePointTitle, widget.typed, [
      Center(
        child: Semantics(
          button: true,
          label: t.copyFact(t.codePointTitle, f.char),
          excludeSemantics: true,
          child: InkWell(
            key: const Key('codepoint-big'),
            onTap: () => Clipboard.setData(ClipboardData(text: f.char)),
            child: Container(
              color: c.raised,
              padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 8),
              child: _Shown(
                facts: f,
                style: TextStyle(fontSize: 56, color: c.ink),
              ),
            ),
          ),
        ),
      ),
      const SizedBox(height: 8),
      Container(
        color: c.bg,
        child: Column(
          children: [
            FutureBuilder<CodePointName?>(
              future: _name,
              builder: (context, snap) {
                final done = snap.connectionState == ConnectionState.done;
                final r = snap.data;
                final name = !done
                    ? t.codePointNameLoading
                    : r == null
                    ? t.codePointNameFailed
                    : (r.name.isEmpty ? t.codePointNameNone : r.name);
                return Semantics(
                  liveRegion: true,
                  child: Column(
                    children: [
                      ColoredBox(
                        color: c.panel,
                        child: _rowBox(c, false, name, t.codePointName),
                      ),
                      if (r != null)
                        ColoredBox(
                          color: c.panel,
                          child: _rowBox(
                            c,
                            false,
                            '${r.categoryName} (${r.category})',
                            t.codePointCategory,
                          ),
                        ),
                    ],
                  ),
                );
              },
            ),
            _fact(
              context,
              t.codePointValue,
              f.value,
              f.value,
              mono: true,
              mine: true,
              key: const Key('cp-row-value'),
            ),
            _fact(
              context,
              t.codePointDecimal,
              f.decimal,
              f.decimal,
              mono: true,
            ),
            _fact(
              context,
              t.codePointUtf8,
              f.utf8,
              f.utf8,
              mono: true,
              key: const Key('cp-row-utf8'),
            ),
            _fact(context, t.codePointUtf16, f.utf16, f.utf16, mono: true),
            _fact(context, t.codePointHtml, f.html, f.html, mono: true),
            _fact(context, t.codePointJson, f.json, f.json, mono: true),
            _fact(context, t.codePointJs, f.js, f.js, mono: true),
            _fact(context, t.codePointPython, f.python, f.python, mono: true),
          ],
        ),
      ),
    ]);
  }
}
