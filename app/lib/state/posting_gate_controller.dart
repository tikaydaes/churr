import 'dart:async';

import 'package:flutter/foundation.dart';

import '../core/conversation/posting_gate.dart';
import '../core/models/message.dart';
import 'message_actions.dart';
import 'server_state.dart';

/// Keeps the composer's [Gate] current (docs/CLIENTS.md 9.5): watches the
/// person's own newest post in the conversation for slow mode, and ticks
/// once a second while a countdown or a timeout runs.
class PostingGateController extends ChangeNotifier {
  PostingGateController(
    this.server,
    this.conversationId, {
    DateTime Function()? now,
  }) : _now = now ?? DateTime.now;

  final ServerState server;
  final String conversationId;
  final DateTime Function() _now;
  StreamSubscription<List<Message>>? _sub;
  Timer? _tick;
  DateTime? _lastOwnPost;
  Gate gate = Gate.open;
  bool _disposed = false;

  void start() {
    _sub = server.db.watchMessages(conversationId, limit: 50).listen((ms) {
      final me = server.me?.id;
      DateTime? newest;
      for (final m in ms) {
        if (m.authorId == me &&
            (newest == null || m.createdAt.isAfter(newest))) {
          newest = m.createdAt;
        }
      }
      _lastOwnPost = newest;
      update();
    });
    server.addListener(update);
    update();
  }

  /// Works the gate out again; ticks while it is closed.
  void update() {
    if (_disposed) return;
    final c = server.conversation(conversationId);
    final next = postingGate(
      c,
      now: _now(),
      lastOwnPost: _lastOwnPost,
      exempt: MessageActions(server).isManager(c),
    );
    final changed =
        next.kind != gate.kind ||
        next.left.inSeconds != gate.left.inSeconds ||
        next.until != gate.until;
    gate = next;
    if (gate.kind == GateKind.open) {
      _tick?.cancel();
      _tick = null;
    } else {
      _tick ??= Timer.periodic(const Duration(seconds: 1), (_) => update());
    }
    if (changed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _tick?.cancel();
    _sub?.cancel();
    server.removeListener(update);
    super.dispose();
  }
}
