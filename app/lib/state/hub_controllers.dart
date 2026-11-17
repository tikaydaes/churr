import 'dart:async';

import 'package:flutter/foundation.dart';

import '../core/api/hubs.dart';
import '../core/models/json.dart';
import 'server_state.dart';

/// The hub's record of one hub-owned channel, for its details screen and
/// its managers' and moderators' tools (rulings R56, R59). Fetched with
/// `getHubChannel` and kept current from `stream.channel_changed`
/// (through [ServerState.hubChannels]); a manager's change answers with
/// the record as the hub signed it afterwards. In a private channel the
/// requests to join are fetched for its managers and moderators.
class HubChannelController extends ChangeNotifier {
  HubChannelController(this.server, this.conversationId);

  final ServerState server;
  final String conversationId;

  HubChannel? channel;

  /// What `getHubChannel` said of the caller; the record's manager and
  /// moderator lists are the authority once an event has changed them.
  bool managerFlag = false;
  bool moderatorFlag = false;

  /// People asking to join a private channel, oldest first; null until
  /// fetched (managers and moderators only).
  List<HubPerson>? requests;

  /// Whether the conversation turned out not to be a hub-owned channel
  /// (or the hub's record could not be read).
  bool none = false;
  bool loading = true;
  String? error;
  bool busy = false;
  bool _disposed = false;

  String? get streamId =>
      object(server.conversation(conversationId)?.raw['shared'])?['stream_id']
          as String?;

  /// Whether the reader is a manager: on the record's manager list as an
  /// account of this instance.
  bool get manager =>
      channel?.isManager(server.me?.id, server.info?.name) ?? managerFlag;

  /// Whether the reader keeps the peace: a manager or on the moderator
  /// list.
  bool get keepsPeace =>
      manager ||
      (channel?.isModerator(server.me?.id, server.info?.name) ?? moderatorFlag);

  bool get owner => server.me?.role == 'owner';

  void _changed() {
    if (!_disposed) notifyListeners();
  }

  StreamSubscription<Json>? _knocks;

  Future<void> start() async {
    server.addListener(_onServer);
    // A request to join that arrives from another instance is told to
    // this instance's moderators and managers on the lists live
    // (`knock.created`): the list is read again.
    _knocks = server.sync.localEvents.listen((e) {
      if (e['type'] != 'knock.created' && e['type'] != 'knock.answered') {
        return;
      }
      if (object(e['knock'])?['conversation_id'] != conversationId) return;
      if ((channel?.private ?? false) && keepsPeace) unawaited(loadRequests());
    });
    await load();
  }

  void _onServer() {
    final next = server.hubChannels[conversationId];
    if (next == null || next.version == channel?.version) return;
    // A newer record from `stream.channel_changed`. Members see it as
    // it is; a manager or moderator fetches it again for `invited` and
    // `requests`.
    final had = channel;
    channel = next;
    if (keepsPeace || owner) {
      if (had != null && next.version > had.version) unawaited(load());
    }
    _changed();
  }

  Future<void> load() async {
    final id = streamId;
    if (id == null) {
      none = true;
      loading = false;
      _changed();
      return;
    }
    try {
      final v = await server.api.getHubChannel(id);
      channel = v.channel;
      managerFlag = v.manager;
      moderatorFlag = v.moderator;
      server.putHubChannel(conversationId, v.channel);
      none = false;
      error = null;
      if (v.channel.private && keepsPeace) await loadRequests();
    } on Exception {
      // Not a hub-owned channel (a direct link, an ad-hoc channel), or
      // the hub's record cannot be read now.
      none = channel == null;
    }
    loading = false;
    _changed();
  }

  /// Fetches the requests to join a private channel.
  Future<void> loadRequests() async {
    final id = streamId;
    if (id == null) return;
    try {
      requests = await server.api.listHubChannelRequests(id);
    } on Exception {
      requests ??= const [];
    }
    _changed();
  }

  /// Admits, declines or removes [accountId]; returns the refusal, or
  /// null. An answered request leaves the list.
  Future<Exception?> member(MemberAction action, String accountId) async {
    final id = streamId;
    if (id == null) return null;
    busy = true;
    _changed();
    try {
      await server.api.moderateHubChannelMember(id, action, accountId);
      if (action != MemberAction.remove) {
        requests = [
          for (final r in requests ?? const <HubPerson>[])
            if (r.accountId != accountId) r,
        ];
      }
      return null;
    } on Exception catch (e) {
      return e;
    } finally {
      busy = false;
      _changed();
    }
  }

  /// Runs a manager action; returns the refusal, or null.
  Future<Exception?> act(
    ModAction action, {
    String? serverName,
    String? accountId,
    String? name,
    String? description,
    Listing? listing,
  }) async {
    final id = streamId;
    if (id == null) return null;
    busy = true;
    _changed();
    try {
      final ch = await server.api.moderateHubChannel(
        id,
        action,
        server: serverName,
        accountId: accountId,
        name: name,
        description: description,
        listing: listing,
      );
      channel = ch;
      server.putHubChannel(conversationId, ch);
      return null;
    } on Exception catch (e) {
      return e;
    } finally {
      busy = false;
      _changed();
    }
  }

  @override
  void dispose() {
    _disposed = true;
    server.removeListener(_onServer);
    _knocks?.cancel();
    super.dispose();
  }
}

/// A hub's directory for a server's owner: its listed channels, and this
/// server's pending and declined requests.
class HubDirectoryController extends ChangeNotifier {
  HubDirectoryController(this.server, this.hub);

  final ServerState server;
  final Peer hub;

  List<HubDirectoryEntry>? channels;

  /// Channels this instance asked to join that a manager has not
  /// answered yet (directory state `requested`).
  List<HubDirectoryEntry> get pending => [
    for (final c in channels ?? const <HubDirectoryEntry>[])
      if (c.state == 'requested') c,
  ];

  /// Streams over this hub whose request was declined: `removed`, the
  /// local channel kept as an archived local channel (`listStreams`).
  List<StreamInfo> declined = const [];
  String? error;
  bool _disposed = false;

  void _changed() {
    if (!_disposed) notifyListeners();
  }

  Future<void> load() async {
    try {
      final results = await Future.wait([
        server.api.listHubDirectory(hub.id),
        server.api.listStreams(),
      ]);
      channels = results[0] as List<HubDirectoryEntry>;
      final streams = [
        for (final s in results[1] as List<StreamInfo>)
          if (s.peerId == hub.id) s,
      ];
      declined = [
        for (final s in streams)
          if (s.state == 'removed') s,
      ];
      error = null;
    } on Exception catch (e) {
      error = '$e';
    }
    _changed();
  }

  /// Joins an open channel or asks to join one by request; returns the
  /// refusal, or null.
  Future<Exception?> join(
    HubDirectoryEntry entry, {
    required String name,
    required bool private,
    String? sectionId,
  }) async {
    try {
      await server.api.joinHubChannel(
        hub.id,
        streamId: entry.streamId,
        name: name,
        private: private,
        sectionId: sectionId,
      );
      await load();
      return null;
    } on Exception catch (e) {
      return e;
    }
  }

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }
}
