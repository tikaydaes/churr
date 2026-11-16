import 'dart:async';

import 'package:flutter/foundation.dart';

import '../core/live.dart';
import '../core/models/message.dart';
import '../core/sealed_client/pending.dart';
import '../core/sealed_client/session.dart';
import 'server_state.dart';

/// Opens the sealed session of a server: the encrypted file and secure
/// storage in the app (lib/platform/sealed_store_file.dart), memory in
/// tests.
typedef SealedOpener = Future<SealedSession> Function(ServerState server);

/// The sealed-chat side of every signed-in server: one
/// [SealedController] each, made when the server is opened
/// (`AppState._openServer`) and wiped when it is signed out of.
class SealedHub {
  SealedHub._();
  static final instance = SealedHub._();

  /// Set by main.dart to the platform's; tests set their own.
  SealedOpener? opener;

  final Map<String, SealedController> _byServer = {};

  /// The controller of [server], if sealed chats run there.
  SealedController? of(ServerState server) => _byServer[server.localId];

  /// Starts sealed chats for a server that has just been opened.
  SealedController? attach(ServerState server) {
    final open = opener;
    if (open == null) return null;
    final c = _byServer[server.localId] ??= SealedController(server, open);
    unawaited(c.start());
    return c;
  }

  /// Signing out of [server]: the device leaves its list and everything
  /// sealed is wiped (docs/SEALED.md section 7.3).
  Future<void> signOut(ServerState server) async {
    final c = _byServer.remove(server.localId);
    if (c == null) return;
    try {
      await c.session?.signOut();
    } on Object {
      // Wiped as far as it could; the server's folder goes next anyway.
    }
    c.dispose();
  }

  /// Locking the app (docs/AUTH.md section 10.6): every server's device
  /// becomes a locked device and every session is let go, with what it
  /// had decrypted. Unlocking opens them again ([attach]).
  Future<void> lockAll() async {
    final all = List.of(_byServer.values);
    _byServer.clear();
    for (final c in all) {
      try {
        await c.session?.lockForSessionLock();
      } on Object {
        // The keys stay in the store; nothing sealed is lost.
      }
      c.dispose();
    }
  }

  /// The decrypted text of a sealed row on any server, for lists; null
  /// when no device here can read it.
  String? decryptedText(Message m) {
    for (final c in _byServer.values) {
      final t = c.session?.textOfRow(m.id);
      if (t != null) return t;
    }
    return null;
  }

  @visibleForTesting
  void reset() {
    for (final c in _byServer.values) {
      c.dispose();
    }
    _byServer.clear();
  }
}

/// One server's sealed chats, for the screens: it feeds the session the
/// sealed events the sync engine passes on and the sealed rows that reach
/// the local database, keeps it told of the account's sealed
/// conversations, and catches up after each `ready`.
class SealedController extends ChangeNotifier {
  SealedController(this.server, this._open);
  final ServerState server;
  final SealedOpener _open;

  SealedSession? session;
  String? error;
  bool _disposed = false;
  final _subs = <StreamSubscription<Object?>>[];
  final _rowSubs = <String, StreamSubscription<List<Message>>>{};
  Map<String, String> _known = const {};
  LiveState? _lastLive;

  SealedStatus get status => session?.status ?? SealedStatus.starting;

  void _changed() {
    if (!_disposed) notifyListeners();
  }

  Future<void> start() async {
    try {
      final me = server.me?.id ?? (await server.db.me())?.id;
      if (me == null || _disposed) return;
      final s = session = await _open(server);
      _subs.add(s.changes.listen((_) => _changed()));
      _usernames();
      await s.start(
        myId: me,
        signIn: takeSignIn(Uri.parse(server.entry.address)),
      );
      _subs.add(server.sync.localEvents.listen(_onEvent));
      server.addListener(_onServer);
      _onServer();
    } on Object catch (e) {
      error = '$e';
    }
    _changed();
  }

  void _usernames() {
    final s = session;
    if (s == null) return;
    final me = server.me;
    s.patterns = () => server.patternsFor(null);
    s.usernames = {
      for (final a in server.accounts.values) a.username.toLowerCase(): a.id,
      if (me != null) me.username.toLowerCase(): me.id,
    };
  }

  static const _sealedEvents = {
    'sealed.epoch_started',
    'device_list.changed',
    'unlock_request.created',
    'unlock_request.answered',
    'backup.changed',
    'device.retired',
  };

  void _onEvent(Map<String, dynamic> e) {
    if (!_sealedEvents.contains(e['type'])) return;
    unawaited(session?.handleEvent(e));
  }

  /// The server's snapshots changed: sealed conversations, accounts, the
  /// connection.
  void _onServer() {
    final s = session;
    if (s == null || _disposed) return;
    _usernames();
    final me = server.me?.id;
    final now = <String, String>{};
    for (final r in server.conversations) {
      final c = r.conversation;
      if (!c.isSealed) continue;
      final other = c.memberIds.firstWhere((m) => m != me, orElse: () => '');
      if (other.isNotEmpty) now[c.id] = other;
    }
    if (!mapEquals(now, _known)) {
      _known = now;
      unawaited(s.setConversations(now));
      for (final id in _rowSubs.keys.toList()) {
        if (!now.containsKey(id)) _rowSubs.remove(id)?.cancel();
      }
      for (final e in now.entries) {
        _rowSubs[e.key] ??= server.db
            .watchMessages(e.key, limit: 1000)
            .listen((rows) => session?.takeRows(e.key, e.value, rows));
      }
    }
    if (server.liveState == LiveState.ready && _lastLive != LiveState.ready) {
      unawaited(s.refresh());
    }
    _lastLive = server.liveState;
  }

  /// After this device was removed (and wiped): starts again as a new,
  /// locked device.
  Future<void> restart() async {
    for (final s in _subs) {
      await s.cancel();
    }
    _subs.clear();
    server.removeListener(_onServer);
    for (final s in _rowSubs.values) {
      await s.cancel();
    }
    _rowSubs.clear();
    _known = const {};
    session?.dispose();
    session = null;
    await start();
  }

  // ---- what the message menu does in a sealed chat ----
  // (MessageActions hands these over; a decrypted message's id is its
  // reference.)

  Future<void> edit(Message m, String text) =>
      session!.edit(m.conversationId, otherOf(m.conversationId)!, m.id, text);

  Future<void> delete(Message m) =>
      session!.delete(m.conversationId, otherOf(m.conversationId)!, m.id);

  Future<void> react(Message m, String emoji) {
    final mine = m.reactions.any((r) => r.emoji == emoji && r.me);
    return session!.react(
      m.conversationId,
      otherOf(m.conversationId)!,
      m.id,
      emoji,
      !mine,
    );
  }

  Future<void> tick(Message m, String item, bool checked) => session!.tick(
    m.conversationId,
    otherOf(m.conversationId)!,
    m.id,
    item,
    checked,
  );

  /// The other person in [conversationId].
  String? otherOf(String conversationId) => _known[conversationId];

  @override
  void dispose() {
    _disposed = true;
    server.removeListener(_onServer);
    for (final s in _subs) {
      s.cancel();
    }
    for (final s in _rowSubs.values) {
      s.cancel();
    }
    super.dispose();
  }
}
