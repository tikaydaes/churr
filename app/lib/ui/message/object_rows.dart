// The composer's row for each reading of an exact token (ruling R73;
// docs/CLIENTS.md, "Objects in the composer"), for the kinds after
// references and instants. One case per kind.

import '../../core/models/json.dart';
import '../../l10n/app_localizations.dart';
import '../theme/churr_icons.dart';

/// The words of the row offering [e], typed as [typed].
String objectRowTitle(L10n t, String kind, Json e, String typed) =>
    switch (kind) {
      'message_link' => t.objectMessageLinkRow,
      _ => typed,
    };

/// The row's glyph.
ChurrGlyph objectRowIcon(String kind) => switch (kind) {
  'instant' => ChurrIcons.clock,
  _ => ChurrIcons.link,
};
