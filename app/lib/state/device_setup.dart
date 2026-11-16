import 'dart:convert';

import '../core/db/app_db.dart';
import '../core/setup_code.dart';
import 'app_state.dart';

/// The app's side of "Set up another device" (docs/CLIENTS.md 8.2).

/// What this device would hand to a new one.
Future<SetupPayload> setupPayloadOf(AppState app) async {
  final all = await app.appDb.prefs();
  return SetupPayload(
    prefs: {
      for (final e in all.entries)
        if (SetupPayload.carriedPrefs.contains(e.key)) e.key: e.value,
    },
    servers: [for (final s in app.servers) s.entry.address],
  );
}

/// Takes a set-up code on the new device: the layout, the default for
/// `show` and the rest of the carried
/// preferences. The servers are then signed in to one by one; those not
/// signed in yet are kept in [pendingSetupServers]. Entries for a server
/// the person skips stay and fall into place if it is added later.
Future<void> applySetupPayload(AppState app, SetupPayload p) async {
  for (final e in p.prefs.entries) {
    if (SetupPayload.carriedPrefs.contains(e.key)) await app.appDb.putPref(e.key, e.value);
  }
  final known = {for (final s in app.servers) s.entry.address};
  await app.appDb.putPref(
    'setup_pending',
    jsonEncode([
      for (final a in p.servers)
        if (!known.contains(a)) a,
    ]),
  );
  await app.prefs.load();
}

/// Server addresses from a set-up code not signed in to yet.
List<String> pendingSetupServers(AppState app) {
  final known = {for (final s in app.servers) s.entry.address};
  return [
    for (final a in AppDb.decodeList(app.prefs.raw('setup_pending')))
      if (!known.contains(a)) a,
  ];
}

/// Drops an address from the walk-through (signed in, or skipped).
Future<void> donePendingServer(AppState app, String address) =>
    app.prefs.putRaw(
      'setup_pending',
      jsonEncode([
        for (final a in AppDb.decodeList(app.prefs.raw('setup_pending')))
          if (a != address) a,
      ]),
    );
