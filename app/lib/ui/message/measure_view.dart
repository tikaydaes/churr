import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:intl/intl.dart';

import '../../core/models/message.dart';
import '../../core/objects/measure.dart';
import '../../core/objects/units.dart';
import '../../l10n/app_localizations.dart';
import '../layout/breakpoints.dart';
import '../theme/tokens.dart';
import 'unit_names.dart';

/// Measurements (ruling R73): the control drawn in a message for a
/// `measure` entity, and the unit converter it opens, which works like the
/// time converter of an instant.

/// The control: what was typed and, in muted text, the equivalent in the
/// reader's [system], as a button that opens the converter.
class MeasureChip extends StatelessWidget {
  const MeasureChip({
    super.key,
    required this.entity,
    required this.typed,
    required this.style,
    required this.system,
  });
  final Entity entity;
  final String typed;
  final TextStyle style;
  final String system;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final value = '${entity.raw['value'] ?? ''}';
    final unit = '${entity.raw['unit'] ?? ''}';
    final alt = convertMeasure(value, unit, system);
    return Semantics(
      button: true,
      label: L10n.of(context)
          .measureChipLabel(typed, alt == null ? '' : ' ($alt)'),
      excludeSemantics: true,
      child: InkWell(
        key: Key('measure-${entity.at}'),
        onTap: () => showUnitConverter(context, entity, typed, system),
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
              children: [
                TextSpan(text: typed),
                if (alt != null)
                  TextSpan(
                    text: ' ($alt)',
                    style: TextStyle(color: c.dim),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// Opens the converter: a sheet on a phone, a dialog elsewhere.
Future<void> showUnitConverter(
  BuildContext context,
  Entity entity,
  String typed,
  String system,
) {
  final compact =
      WidthClass.of(MediaQuery.sizeOf(context).width) == WidthClass.compact;
  Widget body(BuildContext context) =>
      UnitConverter(entity: entity, typed: typed, system: system);
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

/// The unit converter: the text as typed, then the value in the other
/// units of its kind, the reader's system first; each row copies its value.
class UnitConverter extends StatelessWidget {
  const UnitConverter({
    super.key,
    required this.entity,
    required this.typed,
    required this.system,
  });
  final Entity entity;
  final String typed;
  final String system;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final locale = Localizations.maybeLocaleOf(context)?.toString() ?? 'en';
    var group = ',', point = '.';
    try {
      final symbols = NumberFormat.decimalPattern(locale).symbols;
      group = symbols.GROUP_SEP;
      point = symbols.DECIMAL_SEP;
    } on Object {
      // The written marks.
    }
    final rows = measureRows(
      '${entity.raw['value'] ?? ''}',
      '${entity.raw['unit'] ?? ''}',
      system,
    );
    return SingleChildScrollView(
      padding: const EdgeInsets.all(20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(
            t.unitConverterTitle.toUpperCase(),
            style: TextStyle(
              fontSize: 20,
              fontWeight: FontWeight.w800,
              color: c.ink,
            ),
          ),
          const SizedBox(height: 8),
          Text(t.instantTypedAs(typed), style: TextStyle(color: c.dim)),
          const SizedBox(height: 12),
          Container(
            color: c.bg,
            child: Column(
              children: [
                for (final r in rows)
                  _row(
                    context,
                    t,
                    c,
                    r.unit,
                    measureSeparators(r.text, group, point),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _row(
    BuildContext context,
    L10n t,
    ChurrColors c,
    String unit,
    String text,
  ) {
    final name = unitName(t, unit);
    final mine = measureUnitTable[unit]!.sys == system;
    return Semantics(
      button: true,
      label: t.unitCopyRow(text, name),
      excludeSemantics: true,
      child: Material(
        color: c.panel,
        child: InkWell(
          key: Key('measure-row-$unit'),
          onTap: () async {
            await Clipboard.setData(ClipboardData(text: text));
            if (context.mounted) {
              ScaffoldMessenger.maybeOf(context)
                  ?.showSnackBar(SnackBar(content: Text(t.unitCopied(text))));
            }
          },
          child: Container(
            margin: const EdgeInsets.only(bottom: 2),
            decoration: BoxDecoration(
              border: mine
                  ? Border(left: BorderSide(color: c.lavender, width: 4))
                  : null,
            ),
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
            child: Row(
              children: [
                Text(
                  text,
                  style: TextStyle(
                    color: c.ink,
                    fontWeight: FontWeight.w600,
                    fontFeatures: const [FontFeature.tabularFigures()],
                  ),
                ),
                const Spacer(),
                Text(name, style: TextStyle(color: c.dim, fontSize: 13)),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
