// The composer's row for each reading of an exact token (ruling R73;
// docs/CLIENTS.md, "Objects in the composer"), for the kinds after
// references and instants. One case per kind.

import '../../core/models/json.dart';
import '../../core/objects/codepoint.dart';
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
  'number' => t.objectNumberRow(typed, numberRows('${e['value']}').decimal),
  'codepoint' => t.objectCodePointRow(
    '${e['value']}',
    codePointFacts('${e['value']}').shown,
  ),
  'event' => t.objectEventRow(range ?? typed),
  'phone' => t.objectPhoneRow(typed),
  'email' => t.objectEmailRow(typed),
  'geo' => t.objectGeoRow('${e['lat']}', '${e['lon']}'),
  'dice' => t.objectDiceRow('${e['value']}'),
  _ => typed,
};

/// The row's glyph.
ChurrGlyph objectRowIcon(String kind) => switch (kind) {
  'instant' => ChurrIcons.clock,
  'event' => ChurrIcons.calendar,
  'phone' => ChurrIcons.phone,
  'email' => ChurrIcons.send,
  'geo' => ChurrIcons.compass,
  'dice' => ChurrIcons.box,
  'measure' => ChurrIcons.rows,
  'colour' => ChurrIcons.image,
  'number' => ChurrIcons.rows,
  'codepoint' => ChurrIcons.rows,
  _ => ChurrIcons.link,
};
