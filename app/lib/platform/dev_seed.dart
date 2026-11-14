import 'dart:convert';

import '../core/session_store.dart';
import '../state/app_state.dart';

/// Development only. `tools/devseed` prints JSON with a server's address
/// and app tokens; passed to a debug build as base64 in the
/// `CHURR_DEV_SEED` define, it signs the app in as the first account
/// listed, so a trial needs no typing:
///
///     flutter run -d macos --dart-define=CHURR_DEV_SEED=$(base64 < seed.json)
///
/// `main` calls this only under `kDebugMode`, so release builds contain
/// neither the define's value nor this code.
Future<void> addServerFromDevSeed(AppState state) async {
  const seed = String.fromEnvironment('CHURR_DEV_SEED');
  if (seed.isEmpty) return;
  final j =
      jsonDecode(utf8.decode(base64.decode(seed))) as Map<String, dynamic>;
  final origin = j['origin'] as String;
  // Once is enough: the next start finds the server in `app.db`.
  if (state.servers.any((s) => s.entry.address == origin)) return;
  final account = (j['accounts'] as List).first as Map<String, dynamic>;
  await state.addServer(
    Uri.parse(origin),
    Tokens(
      access: account['access_token'] as String,
      refresh: account['refresh_token'] as String,
      expiresAt: DateTime.now().add(const Duration(minutes: 50)),
    ),
  );
}
