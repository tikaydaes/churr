// Events (ruling R73): two instants joined by a range, drawn as one range
// in the reader's zone and locale, as a control offering "Add to
// calendar", which makes a calendar file on this device, and the time
// converter for the start.

import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../../core/instant/format.dart';
import '../../core/instant/zone.dart';
import '../../core/models/message.dart';
import '../../core/objects/ics.dart';
import '../../l10n/app_localizations.dart';
import '../../platform/media_files.dart';
import '../theme/churr_icons.dart';
import '../theme/tokens.dart';
import 'instant_view.dart';

/// "Thu 24 Dec, 6:00 pm – 8:00 pm": the end's day only when it is another.
String eventRange(
  int start,
  int end,
  String zone,
  String locale,
  InstantWords words, {
  String precision = 'minute',
}) {
  final a = compactInstant(start, zone, locale, words, precision: precision);
  final sameDay =
      dayWord(start, zone, locale, words) == dayWord(end, zone, locale, words);
  final b = sameDay
      ? timeOfDay(end, zone, locale, precision: precision)
      : compactInstant(end, zone, locale, words, precision: precision);
  return '$a – $b';
}

/// What "Add to calendar" does: asks where to save the file. Tests replace
/// it.
Future<void> Function(String name, Uint8List bytes) saveEventFile = (
  name,
  bytes,
) => saveDownload(name, bytes, 'text/calendar');

/// The event control.
class EventChip extends StatelessWidget {
  const EventChip({
    super.key,
    required this.entity,
    required this.typed,
    required this.style,
    this.onRemind,
    this.file,
  });
  final Entity entity;
  final String typed;
  final TextStyle style;
  final Future<void> Function(DateTime at)? onRemind;

  /// The message's first line, a link to it and its id, for the file.
  final ({String title, String link, String id})? file;

  Future<void> _addToCalendar() async {
    final f = file;
    final text = eventIcs(
      uid: '${f?.id ?? entity.time}-${entity.at}@churr',
      start: entity.time!,
      end: '${entity.raw['end']}',
      title: f?.title ?? 'Event',
      link: f?.link ?? '',
      now: DateTime.now().toUtc().toIso8601String(),
    );
    await saveEventFile('event.ics', Uint8List.fromList(utf8.encode(text)));
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final t = L10n.of(context);
    final words = instantWords(context);
    final locale = Localizations.maybeLocaleOf(context)?.toString() ?? 'en';
    final zone = InstantEnv.viewerZone;
    final precision = entity.precision ?? 'minute';
    final a = DateTime.parse(entity.time!).millisecondsSinceEpoch;
    final b = DateTime.parse('${entity.raw['end']}').millisecondsSinceEpoch;
    final label = t.eventLabel(
      fullInstant(a, zone, locale, precision: precision),
      fullInstant(b, zone, locale, precision: precision),
      zoneName(zone, a),
      typed,
    );
    return Semantics(
      container: true,
      button: true,
      label: label,
      excludeSemantics: true,
      child: Tooltip(
        message: typed,
        child: PopupMenuButton<String>(
          key: Key('event-${entity.at}'),
          tooltip: '',
          onSelected: (v) {
            if (v == 'calendar') {
              _addToCalendar();
            } else {
              showInstantConverter(context, entity, typed, onRemind: onRemind);
            }
          },
          itemBuilder: (_) => [
            PopupMenuItem(
              key: const Key('event-add-to-calendar'),
              value: 'calendar',
              child: Text(t.eventAddToCalendar),
            ),
            PopupMenuItem(
              value: 'converter',
              child: Text(t.eventOpenConverter),
            ),
          ],
          child: Container(
            color: c.raised,
            padding: const EdgeInsets.symmetric(horizontal: 4),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Glyph(
                  ChurrIcons.calendar,
                  size: (style.fontSize ?? 14),
                  color: c.cyan,
                ),
                const SizedBox(width: 3),
                Flexible(
                  child: Text(
                    eventRange(a, b, zone, locale, words, precision: precision),
                    style: style.copyWith(
                      fontWeight: FontWeight.w600,
                      decoration: TextDecoration.underline,
                      decorationStyle: TextDecorationStyle.dotted,
                      decorationColor: c.cyan,
                    ),
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
