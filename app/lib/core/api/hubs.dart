import '../models/conversation.dart';
import '../models/json.dart';
import 'client.dart';
import 'operations.dart';

/// The listing modes of a channel a hub owns (ruling R56).
enum Listing {
  /// Instances join only by a manager's invitation. A private channel is
  /// unlisted for good.
  unlisted,

  /// Every server on the hub sees it and its owner may ask to join.
  request,

  /// Every server on the hub sees it and its owner may join.
  open;

  static Listing parse(Object? v) =>
      values.firstWhere((l) => l.name == v, orElse: () => unlisted);
}

/// A manager or moderator of a channel a hub owns (`HubAccount`): an
/// account, and the instance it belongs to.
class HubAccount {
  HubAccount({
    required this.accountId,
    required this.server,
    required this.username,
  });

  factory HubAccount.fromJson(Json j) => HubAccount(
    accountId: j['account_id'] as String? ?? '',
    server: j['server'] as String? ?? '',
    username: j['username'] as String? ?? '',
  );

  final String accountId;
  final String server;
  final String username;

  /// `name@server`, as managers and moderators are written.
  String get handle => '$username@$server';
}

/// A hub's record of a channel it owns (`HubChannel`), as last signed.
class HubChannel {
  HubChannel(this.raw)
    : streamId = raw['stream_id'] as String? ?? '',
      hub = raw['hub'] as String? ?? '',
      version = asInt(raw['version']),
      name = raw['name'] as String? ?? '',
      description = raw['description'] as String? ?? '',
      listing = Listing.parse(raw['listing']),
      servers = strings(raw['servers']),
      managers = listOf(raw['managers'], HubAccount.fromJson),
      moderators = listOf(raw['moderators'], HubAccount.fromJson),
      invited = strings(raw['invited']),
      requests = strings(raw['requests']),
      closed = raw['closed'] == true,
      private = raw['private'] == true,
      updatedAt = parseTime(raw['updated_at']);

  final Json raw;
  final String streamId;

  /// The hub's name.
  final String hub;

  /// Grows by one with every change the hub signs.
  final int version;
  final String name;
  final String description;
  final Listing listing;

  /// Member servers, longest-standing first.
  final List<String> servers;

  /// Who holds the channel as an owner would: its instances, its name
  /// and description, its managers and moderators, closing it.
  final List<HubAccount> managers;

  /// Who keeps the peace: admits and removes people and removes
  /// messages, on every instance. A manager may do all a moderator may.
  final List<HubAccount> moderators;

  /// Seen only by managers, moderators and the owner; empty for everyone
  /// else.
  final List<String> invited;
  final List<String> requests;
  final bool closed;

  /// Private for good: never listed, and its people on every instance
  /// admitted by its moderators and managers.
  final bool private;
  final DateTime? updatedAt;

  static bool _on(
    List<HubAccount> list,
    String? accountId,
    String? serverName,
  ) =>
      accountId != null &&
      list.any(
        (m) =>
            m.accountId == accountId &&
            (serverName == null ||
                serverName.isEmpty ||
                m.server == serverName),
      );

  /// Whether the account [accountId] of the instance called [serverName]
  /// is on the manager list (EVENTS.md, `stream.channel_changed`).
  bool isManager(String? accountId, String? serverName) =>
      _on(managers, accountId, serverName);

  /// Whether that account is on the moderator list.
  bool isModerator(String? accountId, String? serverName) =>
      _on(moderators, accountId, serverName);

  /// Whether that account keeps the peace here: a manager or a moderator,
  /// who deletes messages for everyone and admits and removes people.
  bool keepsPeace(String? accountId, String? serverName) =>
      isManager(accountId, serverName) || isModerator(accountId, serverName);
}

/// Someone of an instance in a private channel on a hub (`HubPerson`):
/// one who asks to join, or one a moderator or manager may admit.
class HubPerson {
  HubPerson(this.raw)
    : accountId = raw['account_id'] as String? ?? '',
      username = raw['username'] as String? ?? '',
      displayName = raw['display_name'] as String? ?? '',
      server = raw['server'] as String? ?? '',
      requestedAt = parseTime(raw['requested_at']);

  final Json raw;
  final String accountId;
  final String username;
  final String displayName;

  /// Their home instance.
  final String server;

  /// When they asked to join; null for someone who has not.
  final DateTime? requestedAt;

  /// `name@server`.
  String get handle => '$username@$server';

  /// The display name, or the username when there is none.
  String get name => displayName.isEmpty ? username : displayName;
}

/// One channel in a hub's directory (`HubDirectoryEntry`).
class HubDirectoryEntry {
  HubDirectoryEntry(this.raw)
    : streamId = raw['stream_id'] as String? ?? '',
      name = raw['name'] as String? ?? '',
      description = raw['description'] as String? ?? '',
      listing = Listing.parse(raw['listing']),
      managers = listOf(raw['managers'], HubAccount.fromJson),
      servers = asInt(raw['servers']),
      state = raw['state'] as String? ?? 'none';

  final Json raw;
  final String streamId;
  final String name;
  final String description;
  final Listing listing;
  final List<HubAccount> managers;

  /// How many instances are in it.
  final int servers;

  /// This server's own place: `none`, `invited`, `requested`, `member`.
  final String state;
}

/// A server reachable through a hub (`KnownServer`).
class KnownServer {
  KnownServer(this.raw)
    : id = raw['id'] as String? ?? '',
      name = raw['name'] as String? ?? '',
      fingerprint = raw['fingerprint'] as String? ?? '';
  final Json raw;
  final String id;
  final String name;
  final String fingerprint;
}

/// What a time reference measured of a server's clock (`PeerClock`).
class PeerClock {
  PeerClock(Json j)
    : offsetMs = asInt(j['offset_ms']),
      latencyMs = asInt(j['latency_ms']),
      jitterMs = asInt(j['jitter_ms']),
      uncertaintyMs = asInt(j['uncertainty_ms']),
      forwardMs = asInt(j['forward_ms']),
      backMs = asInt(j['back_ms']),
      asymmetryMs = asInt(j['asymmetry_ms']),
      synced = j['synced'] == true,
      samples = asInt(j['samples']),
      measuredAt = parseTime(j['measured_at']);

  final int offsetMs;
  final int latencyMs;
  final int jitterMs;
  final int uncertaintyMs;
  final int forwardMs;
  final int backMs;
  final int asymmetryMs;
  final bool synced;
  final int samples;
  final DateTime? measuredAt;
}

/// A link to another server or a hub (`Peer`).
class Peer {
  Peer(this.raw)
    : id = raw['id'] as String? ?? '',
      kind = raw['kind'] as String? ?? 'server',
      name = raw['name'] as String? ?? '',
      state = raw['state'] as String? ?? '',
      connectedAt = parseTime(raw['connected_at']),
      servers = listOf(raw['servers'], KnownServer.new),
      toldClock = object(raw['told_clock']) == null
          ? null
          : PeerClock(object(raw['told_clock'])!),
      measuredClock = object(raw['measured_clock']) == null
          ? null
          : PeerClock(object(raw['measured_clock'])!);

  final Json raw;
  final String id;

  /// `server` or `hub`.
  final String kind;
  final String name;

  /// `invited`, `active`, `paused`, `removed`.
  final String state;
  final DateTime? connectedAt;

  /// For a hub: the servers reachable through it.
  final List<KnownServer> servers;

  /// What the peer, as time reference, measured of this server's clock.
  final PeerClock? toldClock;

  /// On a direct link: what this server measured of the other's.
  final PeerClock? measuredClock;

  bool get isHub => kind == 'hub';
  bool get active => state == 'active';
}

/// One shared conversation as the owner sees it (`Stream`), as far as the
/// hub screens need it.
class StreamInfo {
  StreamInfo(this.raw)
    : id = raw['id'] as String? ?? '',
      peerId = raw['peer_id'] as String? ?? '',
      conversationId = raw['conversation_id'] as String?,
      ownerServer = raw['owner_server'] as String?,
      reference = raw['reference'] as String? ?? '',
      state = raw['state'] as String? ?? '',
      offeredName = raw['offered_name'] as String?;
  final Json raw;
  final String id;
  final String peerId;
  final String? conversationId;
  final String? ownerServer;

  /// `self`, `owner` or `hub`.
  final String reference;

  /// `offered`, `active`, `left`, `removed`.
  final String state;
  final String? offeredName;
}

/// A move of a direct-link channel onto a hub (`StreamMove`).
class StreamMove {
  StreamMove(this.raw)
    : id = raw['id'] as String? ?? '',
      streamId = raw['stream_id'] as String? ?? '',
      direction = raw['direction'] as String? ?? '',
      otherServer = raw['other_server'] as String? ?? '',
      hubName = raw['hub_name'] as String? ?? '',
      hubPeerId = raw['hub_peer_id'] as String?,
      state = raw['state'] as String? ?? '',
      reason = raw['reason'] as String?;
  final Json raw;
  final String id;
  final String streamId;

  /// `outgoing` (this server proposed it) or `incoming`.
  final String direction;
  final String otherServer;
  final String hubName;
  final String? hubPeerId;

  /// `proposed`, `accepted`, `committed`, `done`, `declined`, `abandoned`.
  final String state;
  final String? reason;
}

/// The manager actions of `moderateHubChannel`.
enum ModAction {
  invite,
  approve,
  decline,
  removeServer('remove_server'),
  addManager('add_manager'),
  removeManager('remove_manager'),
  addModerator('add_moderator'),
  removeModerator('remove_moderator'),
  describe,
  setListing('set_listing'),
  close;

  const ModAction([this.wire]);
  final String? wire;
  String get code => wire ?? name;
}

/// What `moderateHubChannelMember` does to one person.
enum MemberAction {
  /// Lets them into a private channel.
  admit,

  /// Answers their request to join one.
  decline,

  /// Takes them out on every instance, in any channel on a hub.
  remove,
}

/// What `getHubChannel` answers: the record, and the caller's place.
typedef HubChannelView = ({HubChannel channel, bool manager, bool moderator});

/// Hubs: the owner's screens and the managers' and moderators' tools.
extension HubsApi on ApiClient {
  Future<List<Peer>> listPeers() async =>
      listOf((await call(Ops.listPeers))['peers'], Peer.new);

  /// Shares a channel over [peerId]; on a hub, with its listing mode and
  /// the servers to invite. The hub owns the channel from then on.
  Future<StreamInfo> shareChannel({
    required String conversationId,
    required String peerId,
    Listing listing = Listing.unlisted,
    List<String> serverIds = const [],
  }) async => StreamInfo(
    object(
          (await call(
            Ops.shareChannel,
            body: {
              'conversation_id': conversationId,
              'peer_id': peerId,
              'listing': listing.name,
              if (serverIds.isNotEmpty) 'server_ids': serverIds,
            },
          ))['stream'],
        ) ??
        const {},
  );

  Future<List<HubDirectoryEntry>> listHubDirectory(String peerId) async =>
      listOf(
        (await call(Ops.listHubDirectory, path: {'id': peerId}))['channels'],
        HubDirectoryEntry.new,
      );

  /// Joins an `open` channel, or asks to join a `request` one.
  Future<(StreamInfo, Conversation)> joinHubChannel(
    String peerId, {
    required String streamId,
    required String name,
    bool private = false,
    String? sectionId,
  }) async {
    final j = await call(
      Ops.joinHubChannel,
      path: {'id': peerId},
      body: {
        'stream_id': streamId,
        'name': name,
        'private': private,
        'section_id': ?sectionId,
      },
    );
    return (
      StreamInfo(object(j['stream']) ?? const {}),
      Conversation.fromJson(object(j['conversation']) ?? const {}),
    );
  }

  Future<List<StreamInfo>> listStreams({String? state}) async => listOf(
    (await call(Ops.listStreams, query: {'state': ?state}))['streams'],
    StreamInfo.new,
  );

  /// The hub's record, and whether the caller is a manager or a
  /// moderator of the channel.
  Future<HubChannelView> getHubChannel(String streamId) async {
    final j = await call(Ops.getHubChannel, path: {'id': streamId});
    return (
      channel: HubChannel(object(j['channel']) ?? const {}),
      manager: j['manager'] == true,
      moderator: j['moderator'] == true,
    );
  }

  Future<HubChannel> moderateHubChannel(
    String streamId,
    ModAction action, {
    String? server,
    String? accountId,
    String? name,
    String? description,
    Listing? listing,
  }) async => HubChannel(
    object(
          (await call(
            Ops.moderateHubChannel,
            path: {'id': streamId},
            body: {
              'action': action.code,
              'server': ?server,
              'account_id': ?accountId,
              'name': ?name,
              'description': ?description,
              'listing': ?listing?.name,
            },
          ))['channel'],
        ) ??
        const {},
  );

  /// Asks to be admitted to a private channel on a hub; [streamId] is
  /// the join code a moderator or manager gave.
  Future<HubPerson> askToJoinHubChannel(String streamId) async => HubPerson(
    object(
          (await call(
            Ops.askToJoinHubChannel,
            path: {'id': streamId},
          ))['request'],
        ) ??
        const {},
  );

  /// The requests to join a private channel not yet answered, oldest
  /// first.
  Future<List<HubPerson>> listHubChannelRequests(String streamId) async =>
      listOf(
        (await call(
          Ops.listHubChannelRequests,
          path: {'id': streamId},
        ))['requests'],
        HubPerson.new,
      );

  /// The people a moderator or manager can admit to a private channel.
  Future<List<HubPerson>> listHubChannelCandidates(
    String streamId, {
    String? q,
  }) async => listOf(
    (await call(
      Ops.listHubChannelCandidates,
      path: {'id': streamId},
      query: {if (q != null && q.isNotEmpty) 'q': q},
    ))['people'],
    HubPerson.new,
  );

  /// Admits, declines or removes someone in a channel on a hub.
  Future<void> moderateHubChannelMember(
    String streamId,
    MemberAction action,
    String accountId,
  ) => call(
    Ops.moderateHubChannelMember,
    path: {'id': streamId},
    body: {'action': action.name, 'account_id': accountId},
  );

  Future<StreamMove> proposeStreamMove(String streamId, String peerId) async =>
      StreamMove(
        object(
              (await call(
                Ops.proposeStreamMove,
                path: {'id': streamId},
                body: {'peer_id': peerId},
              ))['move'],
            ) ??
            const {},
      );

  Future<List<StreamMove>> listStreamMoves() async =>
      listOf((await call(Ops.listStreamMoves))['moves'], StreamMove.new);

  Future<StreamMove> _move(Op op, String id) async => StreamMove(
    object((await call(op, path: {'id': id}))['move']) ?? const {},
  );

  Future<StreamMove> acceptStreamMove(String id) =>
      _move(Ops.acceptStreamMove, id);
  Future<StreamMove> declineStreamMove(String id) =>
      _move(Ops.declineStreamMove, id);
  Future<StreamMove> cancelStreamMove(String id) =>
      _move(Ops.cancelStreamMove, id);
}
