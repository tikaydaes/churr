import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../core/instant/format.dart';
import '../../core/instant/zone.dart';
import '../../core/models/message.dart';
import '../../l10n/app_localizations.dart';
import '../layout/breakpoints.dart';
import '../scopes.dart';
import '../theme/churr_icons.dart';
import '../theme/tokens.dart';

/// Instants (ruling R69): the control drawn in a message for an `instant`
/// entity, in the reader's zone and locale, and the time converter it
/// opens.

/// The words formatting needs, from l10n.
InstantWords instantWords(BuildContext context) {
  final t = L10n.of(context);
  return InstantWords(
    today: t.instantToday,
    tomorrow: t.instantTomorrow,
    yesterday: t.instantYesterday,
    relative: (n, unit, future) {
      final amount = switch (unit) {
        'second' => t.instantSeconds(n),
        'minute' => t.instantMinutes(n),
        'hour' => t.instantHours(n),
        'day' => t.instantDays(n),
        'month' => t.instantMonths(n),
        _ => t.instantYears(n),
      };
      return future ? t.instantIn(amount) : t.instantAgo(amount);
    },
  );
}

String _locale(BuildContext context) =>
    Localizations.maybeLocaleOf(context)?.toString() ?? 'en';

/// The instant control: the moment, compact, in the reader's zone, as a
/// button that opens the converter. [typed] is the text it covers.
class InstantChip extends StatelessWidget {
  const InstantChip({
    super.key,
    required this.entity,
    required this.typed,
    required this.style,
    this.onRemind,
  });
  final Entity entity;
  final String typed;
  final TextStyle style;

  /// Sets a reminder at the instant; null hides "Remind me".
  final Future<void> Function(DateTime at)? onRemind;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final ms = DateTime.parse(entity.time!).millisecondsSinceEpoch;
    final zone = InstantEnv.viewerZone;
    final locale = _locale(context);
    final precision = entity.precision ?? 'minute';
    final shown = compactInstant(
      ms,
      zone,
      locale,
      instantWords(context),
      precision: precision,
    );
    final label = L10n.of(context).instantChipLabel(
      fullInstant(ms, zone, locale, precision: precision),
      zoneName(zone, ms),
      typed,
    );
    return Semantics(
      button: true,
      label: label,
      excludeSemantics: true,
      child: Tooltip(
        message: typed,
        child: InkWell(
          key: Key('instant-${entity.at}'),
          onTap: () =>
              showInstantConverter(context, entity, typed, onRemind: onRemind),
          child: Container(
            color: c.raised,
            padding: const EdgeInsets.symmetric(horizontal: 4),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Glyph(
                  ChurrIcons.clock,
                  size: (style.fontSize ?? 14),
                  color: c.cyan,
                ),
                const SizedBox(width: 3),
                Text(
                  shown,
                  style: style.copyWith(
                    fontWeight: FontWeight.w600,
                    decoration: TextDecoration.underline,
                    decorationStyle: TextDecorationStyle.dotted,
                    decorationColor: c.cyan,
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// Where the converter keeps the zones the reader added: the app
/// preferences when the app is running, else for the session only.
List<String> _sessionZones = [];
List<String> _savedZones(BuildContext context) {
  final prefs = context
      .getInheritedWidgetOfExactType<AppScope>()
      ?.notifier
      ?.prefs;
  final list = prefs == null ? _sessionZones : prefs.instantZones;
  return [
    for (final z in list)
      if (validZone(z)) z,
  ];
}

void _saveZones(BuildContext context, List<String> zones) {
  final prefs = context
      .getInheritedWidgetOfExactType<AppScope>()
      ?.notifier
      ?.prefs;
  if (prefs == null) {
    _sessionZones = zones;
  } else {
    prefs.instantZones = zones;
  }
}

/// Opens the converter: a sheet on a phone, a dialog elsewhere.
Future<void> showInstantConverter(
  BuildContext context,
  Entity entity,
  String typed, {
  Future<void> Function(DateTime at)? onRemind,
}) {
  final compact =
      WidthClass.of(MediaQuery.sizeOf(context).width) == WidthClass.compact;
  Widget body(BuildContext context) =>
      InstantConverter(entity: entity, typed: typed, onRemind: onRemind);
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

/// The time converter (ruling R69): the instant in the reader's zone, in
/// the zone as written, in UTC and in zones the reader added; how long
/// until or since, live; copy as ISO; and "Remind me".
class InstantConverter extends StatefulWidget {
  const InstantConverter({
    super.key,
    required this.entity,
    required this.typed,
    this.onRemind,
  });
  final Entity entity;
  final String typed;
  final Future<void> Function(DateTime at)? onRemind;

  @override
  State<InstantConverter> createState() => _InstantConverterState();
}

class _InstantConverterState extends State<InstantConverter> {
  final search = TextEditingController();
  Timer? _tick;

  @override
  void initState() {
    super.initState();
    _tick = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) setState(() {});
    });
    search.addListener(() => setState(() {}));
  }

  @override
  void dispose() {
    _tick?.cancel();
    search.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final e = widget.entity;
    final ms = DateTime.parse(e.time!).millisecondsSinceEpoch;
    final here = InstantEnv.viewerZone;
    final locale = _locale(context);
    final precision = e.precision ?? 'minute';
    final saved = _savedZones(context);
    final label = TextStyle(
      fontSize: 11,
      fontWeight: FontWeight.w700,
      letterSpacing: 0.9,
      color: c.dim,
    );

    Widget row(
      String title,
      String zone, {
      bool removable = false,
      bool mine = false,
    }) => Container(
      key: Key('instant-row-$zone'),
      margin: const EdgeInsets.only(bottom: 2),
      decoration: BoxDecoration(
        color: c.panel,
        border: mine
            ? Border(left: BorderSide(color: c.lavender, width: 4))
            : null,
      ),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      child: Row(
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title.toUpperCase(), style: label),
                Text(
                  fullInstant(ms, zone, locale, precision: precision),
                  style: TextStyle(color: c.ink, fontWeight: FontWeight.w600),
                ),
              ],
            ),
          ),
          if (removable)
            IconButton(
              tooltip: t.instantRemoveZone(zone.replaceAll('_', ' ')),
              icon: Glyph(ChurrIcons.close, size: 18, color: c.dim),
              onPressed: () => setState(
                () => _saveZones(context, [
                  for (final z in saved)
                    if (z != zone) z,
                ]),
              ),
            ),
        ],
      ),
    );

    final q = search.text.trim().toLowerCase().replaceAll(RegExp(r'\s+'), '_');
    final found = q.isEmpty
        ? const <String>[]
        : allZones().where((z) => z.toLowerCase().contains(q)).take(8).toList();
    final future = ms > InstantEnv.now().millisecondsSinceEpoch;

    return SingleChildScrollView(
      padding: const EdgeInsets.all(20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(
            t.instantConverterTitle.toUpperCase(),
            style: TextStyle(
              fontSize: 20,
              fontWeight: FontWeight.w800,
              color: c.ink,
            ),
          ),
          const SizedBox(height: 8),
          Text(t.instantTypedAs(widget.typed), style: TextStyle(color: c.dim)),
          const SizedBox(height: 12),
          Container(
            color: c.bg,
            child: Column(
              children: [
                row(
                  '${t.instantYourTime} · ${zoneName(here, ms)}',
                  here,
                  mine: true,
                ),
                if (e.zone != null && e.zone != here)
                  row(
                    '${t.instantAsWritten} · ${zoneName(e.zone!, ms)}',
                    e.zone!,
                  ),
                if (here != 'UTC' && e.zone != 'Z' && e.zone != 'UTC')
                  row(t.instantUtc, 'UTC'),
                for (final z in saved)
                  if (z != here && z != e.zone)
                    row(zoneName(z, ms), z, removable: true),
              ],
            ),
          ),
          const SizedBox(height: 8),
          Text(
            relativeInstant(ms, instantWords(context)),
            key: const Key('instant-relative'),
            style: TextStyle(fontFamily: fontMono, fontSize: 13, color: c.ink),
          ),
          const SizedBox(height: 16),
          Text(t.instantAddZone.toUpperCase(), style: label),
          const SizedBox(height: 4),
          TextField(
            key: const Key('instant-search'),
            controller: search,
            decoration: InputDecoration(hintText: t.instantSearchZones),
            onSubmitted: (_) {
              if (found.isNotEmpty) {
                setState(() {
                  _saveZones(context, [
                    ...saved,
                    if (!saved.contains(found.first)) found.first,
                  ]);
                  search.clear();
                });
              }
            },
          ),
          if (q.isNotEmpty && found.isEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Text(t.instantNoZones, style: TextStyle(color: c.dim)),
            ),
          for (final z in found)
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton(
                key: Key('instant-zone-$z'),
                onPressed: () => setState(() {
                  _saveZones(context, [...saved, if (!saved.contains(z)) z]);
                  search.clear();
                }),
                child: Text(zoneName(z, ms)),
              ),
            ),
          const SizedBox(height: 16),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            alignment: WrapAlignment.end,
            children: [
              OutlinedButton(
                onPressed: () async {
                  await Clipboard.setData(ClipboardData(text: e.time!));
                  if (context.mounted) {
                    ScaffoldMessenger.maybeOf(
                      context,
                    )?.showSnackBar(SnackBar(content: Text(t.instantCopied)));
                  }
                },
                child: Text(t.instantCopyIso),
              ),
              if (widget.onRemind != null && future)
                FilledButton(
                  onPressed: () async {
                    final at = DateTime.parse(e.time!);
                    final nav = Navigator.of(context);
                    final messenger = ScaffoldMessenger.maybeOf(context);
                    final when = compactInstant(
                      ms,
                      here,
                      locale,
                      instantWords(context),
                      precision: precision,
                    );
                    try {
                      await widget.onRemind!(at);
                      messenger?.showSnackBar(
                        SnackBar(content: Text(t.reminderSet(when))),
                      );
                      nav.maybePop();
                    } on Object catch (err) {
                      messenger?.showSnackBar(SnackBar(content: Text('$err')));
                    }
                  },
                  child: Text(t.instantRemindMe),
                ),
            ],
          ),
        ],
      ),
    );
  }
}
