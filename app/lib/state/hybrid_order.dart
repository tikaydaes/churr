import '../core/db/app_db.dart';
import 'app_prefs.dart';
import 'app_state.dart';

/// Ordering in the hybrid layout (docs/CLIENTS.md 9.1): a conversation is
/// put in a group, and ordered within it, by drag and drop; groups are
/// reordered in place. All of it is the hybrid arrangement, an app
/// preference in `app.db`.

/// A group heading being dragged.
class GroupDrag {
  const GroupDrag(this.groupId);
  final String groupId;
}

/// Puts a conversation in [groupId] at [index] (counted among that
/// group's conversations as they are listed), renumbering the group.
/// With a null [groupId] it goes back to "Everything else".
Future<void> moveInHybrid(
  AppState app,
  String address,
  String conversationId,
  String? groupId,
  int index,
) async {
  final prefs = app.prefs;
  if (groupId == null) {
    await prefs.place(address, conversationId, null);
    return;
  }
  final inGroup = hybridOrderOf(prefs, groupId)
    ..removeWhere(
      (p) => p.serverAddress == address && p.conversationId == conversationId,
    );
  final at = index.clamp(0, inGroup.length);
  inGroup.insert(at, HybridPlacement(address, conversationId, groupId, at));
  await app.appDb.transaction(() async {
    for (var i = 0; i < inGroup.length; i++) {
      await app.appDb.placeConversation(
        inGroup[i].serverAddress,
        inGroup[i].conversationId,
        groupId,
        position: i,
      );
    }
  });
  await prefs.load();
}

/// A group's placements in their order.
List<HybridPlacement> hybridOrderOf(AppPrefs prefs, String groupId) => [
  for (final p in prefs.hybridPlacements)
    if (p.groupId == groupId) p,
]..sort((a, b) => a.position.compareTo(b.position));

/// Where a conversation sits in its group, for sorting rows; a large
/// number for one not placed.
int hybridPosition(AppPrefs prefs, String address, String conversationId) {
  for (final p in prefs.hybridPlacements) {
    if (p.serverAddress == address && p.conversationId == conversationId) return p.position;
  }
  return 1 << 30;
}

/// Moves a group to [index] among the groups.
Future<void> moveHybridGroup(AppState app, String groupId, int index) async {
  final groups = [...app.prefs.hybridGroups]
    ..sort((a, b) => a.position.compareTo(b.position));
  final from = groups.indexWhere((g) => g.id == groupId);
  if (from < 0) return;
  final g = groups.removeAt(from);
  groups.insert(index.clamp(0, groups.length), g);
  await app.appDb.transaction(() async {
    for (var i = 0; i < groups.length; i++) {
      await app.appDb.putHybridGroup(
        HybridGroup(groups[i].id, groups[i].name, i),
      );
    }
  });
  await app.prefs.load();
}

/// Moves a server to [index] in the person's order (rail tiles, merged
/// headings).
Future<void> moveServer(AppState app, String localId, int index) async {
  final from = app.servers.indexWhere((s) => s.localId == localId);
  if (from < 0 || from == index) return;
  // `reorderServers` takes the index before removal, as ReorderableList.
  await app.reorderServers(from, index > from ? index + 1 : index);
}
