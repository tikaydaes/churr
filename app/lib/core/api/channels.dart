import '../models/account.dart';
import '../models/conversation.dart';
import '../models/json.dart';
import '../models/message.dart';
import 'client.dart';
import 'operations.dart';

/// An account in a conversation (`Member`).
/// An account banned from one channel (schema ChannelBan, ruling R72).
class ChannelBan {
  ChannelBan(this.raw)
    : accountId = raw['account_id'] as String? ?? '',
      conversationId = raw['conversation_id'] as String? ?? '',
      reason = raw['reason'] as String? ?? '',
      createdById = raw['created_by_id'] as String? ?? '',
      createdAt = parseTime(raw['created_at']),
      expiresAt = parseTime(raw['expires_at']);
  final Json raw;
  final String accountId;
  final String conversationId;
  final String reason;
  final String createdById;
  final DateTime? createdAt;

  /// Null for a ban that lasts until it is lifted.
  final DateTime? expiresAt;
}

class Member {
  Member(this.raw)
    : accountId = raw['account_id'] as String? ?? '',
      level = raw['level'] as String? ?? 'member',
      directLevel = raw['direct_level'] as String?,
      viaGroup = raw['via_group'] == true,
      mayPost = raw['may_post'] == true,
      invited = raw['invited'] == true,
      joinedAt = parseTime(raw['joined_at']);
  final Json raw;
  final String accountId;

  /// The highest of the direct level and the group levels.
  final String level;

  /// Absent for someone here only through groups.
  final String? directLevel;
  final bool viaGroup;

  /// Allowed by a manager to post while the conversation is an
  /// announcement channel (ruling R66).
  final bool mayPost;

  /// Invited and not yet joined (ruling R103): listed after the members,
  /// not one of them. [level] is the level accepting brings, and
  /// [joinedAt] when they were invited.
  final bool invited;
  final DateTime? joinedAt;
}

/// A group added to a channel at a level (`ChannelGroup`).
class ChannelGroup {
  ChannelGroup(this.raw)
    : groupId = raw['group_id'] as String? ?? '',
      level = raw['level'] as String? ?? 'member';
  final Json raw;
  final String groupId;
  final String level;
}

/// A pinned message (`Pin`).
class Pin {
  Pin(this.raw)
    : messageId = raw['message_id'] as String? ?? '',
      pinnedById = raw['pinned_by_id'] as String? ?? '',
      pinnedAt = parseTime(raw['pinned_at']),
      outsideHistory = raw['outside_history'] == true,
      message = object(raw['message']) == null
          ? null
          : Message.fromJson(object(raw['message'])!);
  final Json raw;
  final String messageId;
  final String pinnedById;
  final DateTime? pinnedAt;

  /// Posted before the reader first joined this private channel: the pin
  /// shows it, and it is not in their history (ruling R87).
  final bool outsideHistory;
  final Message? message;
}

/// Creating, browsing and managing channels.
extension ChannelsApi on ApiClient {
  Conversation _conv(Json j) =>
      Conversation.fromJson(object(j['conversation']) ?? const {});

  /// Public channels and the caller's own; [q] matches name or topic.
  Future<List<Conversation>> browseChannels({
    String? q,
    String? parentId,
    bool archived = false,
  }) => allPages(
    Ops.browseChannels,
    'conversations',
    Conversation.fromJson,
    query: {
      if (q != null && q.isNotEmpty) 'q': q,
      'parent_id': ?parentId,
      if (archived) 'archived': true,
    },
  );

  Future<Conversation> createChannel({
    required String name,
    required bool private,
    String? topic,
    String? sectionId,
    String? parentId,
    String? defaultLevel,
    bool announcement = false,
    List<String> accountIds = const [],
  }) async => _conv(
    await call(
      Ops.createChannel,
      body: {
        'name': name,
        'private': private,
        if (topic != null && topic.isNotEmpty) 'topic': topic,
        'section_id': ?sectionId,
        'parent_id': ?parentId,
        'default_level': ?defaultLevel,
        if (announcement) 'announcement': true,
        if (accountIds.isNotEmpty) 'account_ids': accountIds,
      },
    ),
  );

  /// Finds or makes the ad-hoc channel with exactly these people.
  Future<Conversation> openChat(List<String> accountIds) async =>
      _conv(await call(Ops.openChat, body: {'account_ids': accountIds}));

  /// Changes some settings. [changes] holds only what changes; a null
  /// `parent_id` makes the channel top-level.
  Future<Conversation> updateConversation(String id, Json changes) async =>
      _conv(
        await call(Ops.updateConversation, path: {'id': id}, body: changes),
      );

  Future<Conversation> joinConversation(String id) async =>
      _conv(await call(Ops.joinConversation, path: {'id': id}));

  Future<void> leaveConversation(String id) =>
      call(Ops.leaveConversation, path: {'id': id});

  Future<Conversation> archiveConversation(String id) async =>
      _conv(await call(Ops.archiveConversation, path: {'id': id}));

  Future<Conversation> unarchiveConversation(String id) async =>
      _conv(await call(Ops.unarchiveConversation, path: {'id': id}));

  Future<Conversation> lockConversation(String id) async =>
      _conv(await call(Ops.lockConversation, path: {'id': id}));

  Future<List<Member>> listMembers(String id, {String? level}) => allPages(
    Ops.listMembers,
    'members',
    Member.new,
    path: {'id': id},
    query: {'level': ?level},
  );

  Future<List<Member>> addMembers(
    String id,
    List<String> accountIds, {
    String? level,
  }) async => listOf(
    (await call(
      Ops.addMembers,
      path: {'id': id},
      body: {'account_ids': accountIds, 'level': ?level},
    ))['members'],
    Member.new,
  );

  /// Allows a member to post in an announcement channel, or stops
  /// allowing it. Managers only.
  Future<void> setMemberMayPost(String id, String accountId, bool may) => call(
    Ops.setMemberMayPost,
    path: {'id': id, 'account_id': accountId},
    body: {'may_post': may},
  );

  Future<void> setMemberLevel(String id, String accountId, String level) =>
      call(
        Ops.setMemberLevel,
        path: {'id': id, 'account_id': accountId},
        body: {'level': level},
      );

  Future<void> removeMember(String id, String accountId) =>
      call(Ops.removeMember, path: {'id': id, 'account_id': accountId});

  /// The bans in force in a channel (ruling R72). Managers only.
  Future<List<ChannelBan>> listChannelBans(String id) async => listOf(
    (await call(Ops.listChannelBans, path: {'id': id}))['bans'],
    ChannelBan.new,
  );

  /// Bans an account from a channel: out, and kept out until lifted or,
  /// with [seconds], until then.
  Future<void> banFromChannel(
    String id,
    String accountId, {
    String? reason,
    int? seconds,
  }) => call(
    Ops.banFromChannel,
    path: {'id': id},
    body: {'account_id': accountId, 'reason': ?reason, 'seconds': ?seconds},
  );

  Future<void> liftChannelBan(String id, String accountId) =>
      call(Ops.liftChannelBan, path: {'id': id, 'account_id': accountId});

  Future<List<ChannelGroup>> listChannelGroups(String id) async => listOf(
    (await call(Ops.listChannelGroups, path: {'id': id}))['groups'],
    ChannelGroup.new,
  );

  Future<void> addChannelGroup(String id, String groupId, String level) => call(
    Ops.addChannelGroup,
    path: {'id': id, 'group_id': groupId},
    body: {'level': level},
  );

  Future<void> removeChannelGroup(String id, String groupId) =>
      call(Ops.removeChannelGroup, path: {'id': id, 'group_id': groupId});

  Future<List<Pin>> listPins(String id) async =>
      listOf((await call(Ops.listPins, path: {'id': id}))['pins'], Pin.new);

  Future<void> requestWebhook(
    String id, {
    required String name,
    String? purpose,
  }) => call(
    Ops.requestWebhook,
    path: {'id': id},
    body: {
      'name': name,
      if (purpose != null && purpose.isNotEmpty) 'purpose': purpose,
    },
  );

  /// Accounts matching [q] by username or display name, for finding
  /// people this device has not seen yet (docs/CLIENTS.md 8.5).
  Future<List<Account>> findAccounts(String q, {int limit = 20}) async =>
      listOf(
        (await call(
          Ops.listAccounts,
          query: {'q': q, 'limit': limit},
        ))['accounts'],
        Account.fromJson,
      );
}
