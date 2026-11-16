import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import '../core/api/client.dart';
import '../core/api/me.dart';
import '../core/db/app_db.dart';
import '../core/db/account_tables.dart';
import '../core/db/cache_db.dart';
import '../core/ids.dart';
import '../core/live.dart';
import '../core/oauth.dart';
import '../core/session_store.dart';
import 'app_lock.dart';
import 'app_prefs.dart';
import 'sealed_state.dart';
import 'server_state.dart';
import 'themes_state.dart';

/// What the app needs from the platform to keep a server: where its
/// database lives and how to forget it. The app gives real folders
/// (`lib/platform/paths.dart`); tests give memory.
abstract class ServerStorage {
  /// Opens (creating if need be) the `cache.db` of a server.
  Future<CacheDb> openCache(String localId);

  /// The folder for a server's downloaded files, or null for none.
  Future<String?> filesDirectory(String localId);

  /// Deletes everything kept for a server. Signing out calls this.
  Future<void> delete(String localId);
}

/// The conversation on screen: which server, which conversation.
@immutable
class OpenTarget {
  const OpenTarget(this.serverId, this.conversationId);
  final String serverId;
  final String conversationId;

  @override
  bool operator ==(Object other) =>
      other is OpenTarget &&
      other.serverId == serverId &&
      other.conversationId == conversationId;
  @override
  int get hashCode => Object.hash(serverId, conversationId);
}

/// The top of the app's state (docs/CLIENTS.md section 4): the app
/// preferences and one [ServerState] per signed-in server, added and
/// removed at runtime. Servers stay independent; what joins them is the
/// app.
///
/// It also holds the two things about navigation that are not in any
/// database: which server the rail shows and which conversation is open.
class AppState extends ChangeNotifier {
  AppState({
    required this.appDb,
    required this.storage,
    required SecretStore secrets,
    this.httpClientFactory,
    this.connector,
    this.connectOnStart = true,
  }) : sessions = SessionStore(secrets),
       prefs = AppPrefs(appDb);

  final AppDb appDb;
  final ServerStorage storage;
  final SessionStore sessions;
  final AppPrefs prefs;

  /// The theme in use and the person's saved themes on every server.
  late final ThemesState themes = ThemesState(this);

  /// The lock (lib/state/app_lock.dart): while it is locked the app draws
  /// only the lock screen and the servers hold nothing in memory.
  late final AppLock lock = AppLock(this);

  /// Replaced in tests to keep requests off the network.
  final http.Client Function()? httpClientFactory;
  final LiveConnector? connector;
  final bool connectOnStart;

  /// Signed-in servers, in the person's order.
  final List<ServerState> servers = [];

  bool loaded = false;
  String? _selectedServerId;
  OpenTarget? _open;

  /// The server the rail layout shows.
  ServerState? get selectedServer {
    for (final s in servers) {
      if (s.localId == _selectedServerId) return s;
    }
    return servers.isEmpty ? null : servers.first;
  }

  OpenTarget? get open => _open;

  ServerState? server(String localId) {
    for (final s in servers) {
      if (s.localId == localId) return s;
    }
    return null;
  }

  /// Opens `app.db`'s servers and their databases. The app is usable as
  /// soon as this returns, before any request finishes.
  Future<void> load() async {
    await prefs.load();
    themes; // made now, so it hears every server from the start
    for (final entry in await appDb.servers()) {
      final tokens = await sessions.load(entry.localId);
      servers.add(await _openServer(entry, tokens));
    }
    loaded = true;
    notifyListeners();
  }

  Future<ServerState> _openServer(ServerEntry entry, Tokens? tokens) async {
    final api = ApiClient(
      base: Uri.parse(entry.address),
      httpClient: httpClientFactory?.call(),
      tokens: tokens,
      onTokens: (t) => sessions.save(entry.localId, t),
    );
    final s = ServerState(
      entry: entry,
      db: await storage.openCache(entry.localId),
      api: api,
      connector: connector,
      filesDirectory: await storage.filesDirectory(entry.localId),
    );
    // A server's change (a badge, a new conversation) redraws the shell.
    s.addListener(notifyListeners);
    // A locked app reads nothing of the account until it is unlocked
    // (unlockedAgain).
    if (lock.locked) return s;
    await s.start(connect: connectOnStart && tokens != null);
    // Sealed chats: this app becomes a device of the account there.
    if (connectOnStart && tokens != null) SealedHub.instance.attach(s);
    return s;
  }

  /// A client for a server the app is not signed in to yet, for "Add a
  /// server": it can read the server's description and run the sign-in.
  ApiClient probe(Uri address) =>
      ApiClient(base: address, httpClient: httpClientFactory?.call());

  /// Adds a server whose sign-in has just finished: [tokens] are stored
  /// in the platform's key store, the server gets its own folder, and
  /// its first sync starts.
  Future<ServerState> addServer(Uri address, Tokens tokens) async {
    final api = probe(address);
    String name;
    try {
      final info = await api.getServer();
      name = info.communityName.isEmpty ? address.host : info.communityName;
    } finally {
      api.close();
    }
    final entry = ServerEntry(
      localId: newUuid(),
      address: address.toString(),
      name: name,
      position: servers.length,
    );
    await sessions.save(entry.localId, tokens);
    await appDb.addServer(entry);
    final s = await _openServer(entry, tokens);
    servers.add(s);
    _selectedServerId = s.localId;
    notifyListeners();
    return s;
  }

  /// Run while signing out of a server, before its session is revoked
  /// (the notifications code removes the device there).
  final List<Future<void> Function(ServerState)> beforeRemove = [];

  /// Signs out of a server: revokes the session, then deletes its
  /// tokens, its folder and its entry (docs/CLIENTS.md section 6).
  Future<void> removeServer(String localId) async {
    final s = server(localId);
    if (s == null) return;
    servers.remove(s);
    if (_open?.serverId == localId) _open = null;
    if (_selectedServerId == localId) _selectedServerId = null;
    notifyListeners();
    s.removeListener(notifyListeners);
    for (final f in List.of(beforeRemove)) {
      try {
        await f(s);
      } on Object {
        // Signing out goes ahead whatever a listener met.
      }
    }
    // Its last act on the server: leave the account's sealed-chat device
    // list, then wipe the sealed keys and store (docs/SEALED.md 7.3).
    await SealedHub.instance.signOut(s);
    await s.api.revoke();
    await s.shutDown();
    await sessions.clear(localId);
    await storage.delete(localId);
    await appDb.removeServer(localId);
  }

  /// The lock has let go of everything: nothing is open.
  void lockedOut() {
    _open = null;
    clearFocus();
    notifyListeners();
  }

  /// Unlocked: every server reads its database again, connects and
  /// catches up, and sealed chats start (opening the copy of the key the
  /// lock left for this device).
  Future<void> unlockedAgain() async {
    for (final s in servers) {
      final signedIn = s.api.tokens != null;
      await s.start(connect: connectOnStart && signedIn);
      if (connectOnStart && signedIn) SealedHub.instance.attach(s);
    }
    notifyListeners();
  }

  void selectServer(String localId) {
    _selectedServerId = localId;
    notifyListeners();
  }

  /// Opens a conversation, or with null goes back to the sidebar.
  /// Opening a sub-channel expands its parent (9.25).
  void openConversation(OpenTarget? target) {
    _open = target;
    if (target != null) {
      _selectedServerId = target.serverId;
      // What a conversation keeps depends on when it was last opened.
      unawaited(
        server(target.serverId)?.db
            .markOpened(target.conversationId, DateTime.now()),
      );
      final parent = server(target.serverId)
          ?.conversation(target.conversationId)
          ?.parentId;
      if (parent != null) {
        prefs.setCollapsed('${target.serverId}/$parent', false);
      }
    }
    notifyListeners();
  }

  /// The message to bring into view when a conversation opens from the
  /// notifications list, Threads or Bookmarks ("open in place"), and the
  /// thread it is in, if it is a reply. The conversation screen reads it
  /// once and calls [clearFocus].
  String? focusMessageId;
  String? focusThreadId;

  /// Opens the conversation a message is in with that message in view;
  /// a reply opens its thread.
  void openMessage(OpenTarget target, String messageId, {String? parentId}) {
    focusMessageId = messageId;
    focusThreadId = parentId;
    openConversation(target);
  }

  void clearFocus() {
    focusMessageId = null;
    focusThreadId = null;
  }

  /// Writes the person's order of servers.
  Future<void> reorderServers(int from, int to) async {
    final s = servers.removeAt(from);
    servers.insert(to > from ? to - 1 : to, s);
    notifyListeners();
    await appDb.orderServers([for (final x in servers) x.localId]);
  }

  /// The sum of the servers' badges: the icon badge, and the count on
  /// the Notifications entry. Totals ignore `show`.
  int get totalBadge => servers.fold(0, (n, s) => n + s.summary.badge);
}
