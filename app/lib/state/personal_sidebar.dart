import 'package:flutter/foundation.dart';

import '../core/api/errors.dart';
import '../core/api/me.dart';
import '../core/models/json.dart';
import '../core/models/personal_layout.dart';
import 'server_state.dart';

/// Changes to a person's own sidebar on one server (ruling R83,
/// docs/FEATURES.md 21.4). The server keeps it, so every device and the
/// web chat show the same; `sidebar.changed` brings other devices' changes.

/// Sends [ops], made against the layout this device has. If another
/// device changed it meanwhile the server refuses; the layout is read
/// again and the same changes sent once more, since each says where
/// things go and not how far they move.
Future<PersonalLayout> changeLayout(ServerState s, List<Json> ops) async {
  for (var tries = 0; ; tries++) {
    try {
      final l = await s.api.changeSidebar(s.layout.version, ops);
      await _keep(s, l);
      return l;
    } on ApiException catch (e) {
      if (e.code != 'conflict' || tries > 0) rethrow;
      await _keep(s, await s.api.getSidebar());
    }
  }
}

Future<void> _keep(ServerState s, PersonalLayout l) async {
  // Drawn at once; the database follows and its watch agrees.
  if (l.version >= s.layout.version) s.layout = l;
  await s.db.putSidebarLayout(l);
}

/// Which server's sidebar is in reordering mode, if any: rows carry a
/// handle to drag and buttons to move up and down. Entered from a row's
/// menu (a long press on a phone).
final reorderingServer = ValueNotifier<String?>(null);
