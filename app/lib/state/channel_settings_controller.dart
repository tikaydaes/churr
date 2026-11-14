import 'dart:async';

import 'package:flutter/foundation.dart';

import '../core/api/channels.dart';
import '../core/db/cache_db.dart';
import '../core/models/conversation.dart';
import 'channel_actions.dart';
import 'server_state.dart';

/// The members-and-settings screen's state (docs/CLIENTS.md 9.15).
///
/// The conversation itself is watched in the local database, so a change
/// made here or anywhere shows as soon as sync writes it. The member
/// list, the groups in the channel and the pins live on the server only
/// and are fetched when the screen opens and after each change.
class ChannelSettingsController extends ChangeNotifier {
  ChannelSettingsController(this.server, this.conversationId)
    : actions = ChannelActions(server);

  final ServerState server;
  final String conversationId;
  final ChannelActions actions;

  Conversation? conversation;
  List<Member>? members;
  List<ChannelGroup>? groups;
  List<Pin>? pins;
  String? error;
  bool _disposed = false;
  StreamSubscription<ConversationRow?>? _sub;

  void _changed() {
    if (!_disposed) notifyListeners();
  }

  Future<void> start() async {
    conversation = server.conversation(conversationId);
    _sub = server.db.watchConversation(conversationId).listen((r) {
      if (r != null) conversation = r.conversation;
      _changed();
    });
    await Future.wait([loadMembers(), loadPins()]);
  }

  Future<void> loadMembers() async {
    try {
      final c = conversation;
      final m = await actions.members(conversationId);
      final g = c != null && c.isNamed
          ? await actions.groups(conversationId)
          : const <ChannelGroup>[];
      members = m;
      groups = g;
      error = null;
    } on Exception catch (e) {
      error = 'Could not load the members. ($e)';
    }
    _changed();
  }

  Future<void> loadPins() async {
    try {
      pins = await actions.pins(conversationId);
    } on Exception {
      pins = const [];
    }
    _changed();
  }

  /// The reader's own member row, when the list is loaded.
  Member? get mine {
    for (final m in members ?? const <Member>[]) {
      if (m.accountId == server.me?.id) return m;
    }
    return null;
  }

  bool get manager =>
      conversation != null && actions.actsAsManager(conversation!);

  /// In through a group only: such a person cannot leave on their own.
  bool get inThroughGroupOnly =>
      mine != null && mine!.viaGroup && mine!.directLevel == null;

  /// Runs a change and reloads the lists; returns the refusal, or null.
  Future<Exception?> run(
    Future<void> Function() f, {
    bool reloadMembers = true,
  }) async {
    try {
      await f();
      if (reloadMembers) await loadMembers();
      return null;
    } on Exception catch (e) {
      return e;
    }
  }

  @override
  void dispose() {
    _disposed = true;
    _sub?.cancel();
    super.dispose();
  }
}
