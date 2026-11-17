import '../core/api/channels.dart';
import '../core/db/cache_db.dart';
import '../core/models/conversation.dart';
import '../core/models/json.dart';
import 'server_state.dart';

/// Creating, joining, leaving and changing channels on one server.
///
/// Every change is a request whose answer is the conversation as it now
/// stands; that answer is written to the cache at once, and the
/// `conversation.updated` (or membership) event that follows writes the
/// same again. Lists that live only on the server (members, groups in a
/// channel, pins, the channel browser) are fetched by the screens'
/// controllers and are not cached.
class ChannelActions {
  ChannelActions(this.server);
  final ServerState server;

  CacheDb get _db => server.db;

  /// Writes a conversation the server answered with. One the cache does
  /// not hold yet is taken in at its newest event, like any new
  /// conversation (README, "New conversations are taken in at their
  /// last_seq").
  Future<Conversation> keep(Conversation c) async {
    final held = await _db.conversation(c.id);
    if (c.membership == null && held == null) return c;
    await _db.upsertConversation(c, syncedSeq: held == null ? c.lastSeq : null);
    return c;
  }

  Future<Conversation> create({
    required String name,
    required bool private,
    String? topic,
    String? sectionId,
    String? parentId,
    bool announcement = false,
  }) async => keep(
    await server.api.createChannel(
      name: name,
      private: private,
      topic: topic,
      sectionId: parentId == null ? sectionId : null,
      parentId: parentId,
      // An announcement channel (ruling R66): its managers and those
      // they allow post; nobody's level is lowered for it.
      announcement: announcement,
    ),
  );

  /// Finds or makes the ad-hoc channel with exactly these people.
  Future<Conversation> openChat(List<String> accountIds) async =>
      keep(await server.api.openChat(accountIds));

  Future<Conversation> update(String id, Json changes) async =>
      keep(await server.api.updateConversation(id, changes));

  Future<Conversation> setParent(String id, String? parentId) =>
      update(id, {'parent_id': parentId});

  Future<Conversation> makePrivate(String id) => update(id, {'private': true});

  /// Changes a channel's kind of access (ruling R79): `public`, `knock` or
  /// `private`.
  Future<Conversation> setAccess(String id, String access) =>
      update(id, {'access': access});

  Future<Conversation> lock(String id) async =>
      keep(await server.api.lockConversation(id));

  Future<Conversation> archive(String id) async =>
      keep(await server.api.archiveConversation(id));

  Future<Conversation> unarchive(String id) async =>
      keep(await server.api.unarchiveConversation(id));

  Future<Conversation> join(String id) async =>
      keep(await server.api.joinConversation(id));

  /// Leaves, and forgets the conversation on this device. The server's
  /// `member.left` would do the same.
  Future<void> leave(String id) async {
    await server.api.leaveConversation(id);
    await _db.deleteConversation(id);
  }

  /// The public sub-channels of [parentId] the person is not in, offered
  /// after joining the parent (docs/CLIENTS.md 9.25). Private ones are
  /// never listed.
  Future<List<Conversation>> publicSubChannels(String parentId) async => [
    for (final c in await server.api.browseChannels(parentId: parentId))
      if (!c.private && c.membership == null && c.parentId == parentId) c,
  ];

  Future<List<Member>> members(String id) => server.api.listMembers(id);
  Future<List<ChannelGroup>> groups(String id) =>
      server.api.listChannelGroups(id);
  Future<List<Pin>> pins(String id) => server.api.listPins(id);

  Future<void> addMembers(
    String id,
    List<String> accountIds, {
    String? level,
  }) => server.api.addMembers(id, accountIds, level: level);
  Future<void> setLevel(String id, String accountId, String level) =>
      server.api.setMemberLevel(id, accountId, level);
  Future<void> setMayPost(String id, String accountId, bool may) =>
      server.api.setMemberMayPost(id, accountId, may);
  Future<void> removeMember(String id, String accountId) =>
      server.api.removeMember(id, accountId);
  Future<void> addGroup(String id, String groupId, String level) =>
      server.api.addChannelGroup(id, groupId, level);
  Future<void> removeGroup(String id, String groupId) =>
      server.api.removeChannelGroup(id, groupId);
  Future<void> requestWebhook(String id, String name, String purpose) =>
      server.api.requestWebhook(id, name: name, purpose: purpose);

  // ---- who may do what (FEATURES.md 21, PERMISSIONS) ----

  /// Acting as manager: the channel's level for the reader is manager.
  /// The account's role (owner, admin) gives nothing in a channel
  /// (ruling R99); a sealed chat has no manager.
  bool actsAsManager(Conversation c) =>
      !c.isSealed && c.membership?.level == 'manager';

  /// Keeping the peace (ruling R99): a moderator or a manager of the
  /// channel, who answers knocks, adds people at the default level,
  /// removes and bans, deletes others' messages, times people out, sets
  /// slow mode and locks.
  bool keepsPeace(Conversation c) =>
      !c.isSealed &&
      (c.membership?.level == 'manager' || c.membership?.level == 'moderator');

  /// The parent may be set by an account acting as manager that is also
  /// a member of the parent; never on ad-hoc channels or sealed chats.
  /// (An admin sets it from the admin console in the browser.)
  bool canSetParent(Conversation c) => c.isNamed && actsAsManager(c);

  /// The channels that may be chosen as a parent for [c]: top-level
  /// named channels the person is in, not [c] itself. Empty when [c] has
  /// sub-channels of its own.
  List<Conversation> parentChoices(Conversation c) {
    final all = [for (final r in server.conversations) r.conversation];
    if (all.any((x) => x.parentId == c.id)) return const [];
    return [
      for (final x in all)
        if (x.id != c.id &&
            x.isNamed &&
            x.parentId == null &&
            !x.isSealed &&
            x.membership != null)
          x,
    ]..sort((a, b) => a.name!.toLowerCase().compareTo(b.name!.toLowerCase()));
  }

  /// Why [c] cannot be moved under a parent, or null.
  String? parentRefusal(Conversation c) {
    if (!c.isNamed) return 'Only named channels can sit under another channel.';
    if (c.isArchived) return 'An archived channel cannot be moved.';
    if (server.conversations.any((r) => r.conversation.parentId == c.id)) {
      return 'This channel has sub-channels of its own, so it cannot sit under another.';
    }
    return null;
  }
}

/// Checks a channel name as the server will (CONVENTIONS.md, R51): 1 to
/// 64 characters of any script, no control characters, no leading or
/// trailing space. Runs of spaces are collapsed by the server. Returns a
/// sentence, or null.
String? checkChannelName(String name) {
  if (name.isEmpty) return 'A name is needed.';
  if (name.runes.length > 64) return 'At most 64 characters.';
  if (name != name.trim()) return 'No space at the start or end.';
  if (RegExp(r'[\x00-\x1f\x7f]').hasMatch(name)) return 'No control characters.';
  return null;
}
