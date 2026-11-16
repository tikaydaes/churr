import 'app_state.dart';

/// Moves a server to [index] in the person's order (rail tiles, merged
/// headings).
Future<void> moveServer(AppState app, String localId, int index) async {
  final from = app.servers.indexWhere((s) => s.localId == localId);
  if (from < 0 || from == index) return;
  // `reorderServers` takes the index before removal, as ReorderableList.
  await app.reorderServers(from, index > from ? index + 1 : index);
}
