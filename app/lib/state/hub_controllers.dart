import 'dart:async';

import 'package:flutter/foundation.dart';

import '../core/api/hubs.dart';
import '../core/models/json.dart';
import 'server_state.dart';

/// The hub's record of one hub-owned channel, for its details screen and
/// its moderators' tools (rulings R56, R59). Fetched with
/// `getHubChannel` and kept current from `stream.channel_changed`
/// (through [ServerState.hubChannels]); a moderator's change answers
/// with the record as the hub signed it afterwards.
class HubChannelController extends ChangeNotifier {
  HubChannelController(this.server, this.conversationId);

  final ServerState server;
  final String conversationId;

  HubChannel? channel;

  /// What `getHubChannel` said of the caller; the record's moderator
  /// list is the authority once an event has changed it.
  bool moderatorFlag = false;

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

  /// Whether the reader is a moderator: on the record's list as an
  /// account of this server.
  bool get moderator =>
      channel?.isModerator(server.me?.id, server.info?.name) ?? moderatorFlag;

  bool get owner => server.me?.role == 'owner';

  void _changed() {
    if (!_disposed) notifyListeners();
  }

  Future<void> start() async {
    server.addListener(_onServer);
    await load();
  }

  void _onServer() {
    final next = server.hubChannels[conversationId];
    if (next == null || next.version == channel?.version) return;
    // A newer record from `stream.channel_changed`. Members see it as
    // it is; a moderator fetches it again for `invited` and `requests`.
    final had = channel;
    channel = next;
    if (moderator || owner) {
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
      final (ch, mod) = await server.api.getHubChannel(id);
      channel = ch;
      moderatorFlag = mod;
      server.putHubChannel(conversationId, ch);
      none = false;
      error = null;
    } on Exception {
      // Not a hub-owned channel (a direct link, an ad-hoc channel), or
      // the hub's record cannot be read now.
      none = channel == null;
    }
    loading = false;
    _changed();
  }

  /// Runs a moderator action; returns the refusal, or null.
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

  /// Channels this server asked to join that a moderator has not
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
