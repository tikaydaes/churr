// The composer's row for each reading of an exact token (ruling R73;
// docs/CLIENTS.md, "Objects in the composer"), for the kinds after
// references and instants. One case per kind.

import '../../core/models/json.dart';
import '../../core/objects/measure.dart';
import '../../l10n/app_localizations.dart';
import '../theme/churr_icons.dart';

/// The words of the row offering [e], typed as [typed].
/// [units] is the author's own system for measurements; [range] an
/// event's range as the author reads it.
String objectRowTitle(
  L10n t,
  String kind,
  Json e,
  String typed, {
  String units = 'off',
  String? range,
}) => switch (kind) {
  'message_link' => t.objectMessageLinkRow,
  'measure' => switch (convertMeasure('${e['value']}', '${e['unit']}', units)) {
    final alt? => t.objectMeasureRowAlt(typed, alt),
    null => t.objectMeasureRow(typed),
  },
  'colour' => t.objectColourRow('${e['value']}'),
  'event' => t.objectEventRow(range ?? typed),
  'phone' => t.objectPhoneRow(typed),
  'email' => t.objectEmailRow(typed),
  _ => typed,
};

/// The row's glyph.
ChurrGlyph objectRowIcon(String kind) => switch (kind) {
  'instant' => ChurrIcons.clock,
  'event' => ChurrIcons.calendar,
  'phone' => ChurrIcons.phone,
  'email' => ChurrIcons.send,
  'measure' => ChurrIcons.rows,
  'colour' => ChurrIcons.image,
  _ => ChurrIcons.link,
};
