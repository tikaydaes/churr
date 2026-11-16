import 'dart:async';

import 'package:flutter/foundation.dart';

import '../core/api/client.dart';
import '../core/api/hubs.dart';
import '../core/api/me.dart';
import '../core/api/reference_patterns.dart';
import '../core/db/app_db.dart';
import '../core/db/cache_db.dart';
import '../core/live.dart';
import '../core/models/account.dart';
import '../core/models/conversation.dart';
import '../core/models/json.dart';
import '../core/models/server.dart';
import '../core/sync.dart';

/// One signed-in server: its database, API client, live connection and
/// sync engine, and the latest answers of the queries the shell watches.
///
/// Screens never read the network. They read this object's snapshots (or
/// watch the database themselves through a controller) and rebuild when
/// it notifies; sync is what changes the database underneath.
class ServerState extends ChangeNotifier {
  ServerState({
    required this.entry,
    required this.db,
    required this.api,
    LiveConnector? connector,
    this.filesDirectory,
  }) {
    sync = SyncEngine(db: db, api: api);
    live = LiveConnection(api: api, delegate: sync, connector: connector);
  }

  /// The server as `app.db` lists it.
  ServerEntry entry;
  final CacheDb db;
  final ApiClient api;
  late final SyncEngine sync;
  late final LiveConnection live;

  /// Where downloaded files are cached, or null to keep them in memory
  /// only (tests).
  final String? filesDirectory;

  String get localId => entry.localId;

  /// The community's name, for the tile and the heading.
  String get name => entry.name;

  // The snapshots. Each mirrors one watched query.
  Me? me;
  List<ConversationRow> conversations = const [];
  Map<String, Conversation> outsideParents = const {};
  List<Section> sections = const [];
  Map<String, Account> accounts = const {};
  Map<String, Group> groups = const {};
  Map<String, CustomEmoji> emoji = const {};
  StatusSummary summary = const StatusSummary();
  LiveState liveState = LiveState.offline;

  /// What the server says about itself (`getServer`), fetched on start
  /// and replaced by `server.updated`; null until then. Its
  /// `default_themes` feed the themes (lib/state/themes_state.dart).
  ServerInfo? info;

  /// The hub's records of hub-owned channels this device has seen, by
  /// conversation id: fetched by the channel screens and kept current
  /// from `stream.channel_changed`.
  final Map<String, HubChannel> hubChannels = {};

  /// Whether the reader is a moderator of the hub-owned channel
  /// [conversationId], from the record this device holds.
  bool isHubModerator(String conversationId) =>
      hubChannels[conversationId]?.isModerator(me?.id, info?.name) ?? false;

  /// Keeps a record of a hub-owned channel that a screen fetched.
  void putHubChannel(String conversationId, HubChannel ch) {
    hubChannels[conversationId] = ch;
    if (!_disposed) notifyListeners();
  }

  /// Fetches the hub's record of a shared conversation; null when it is
  /// not a channel a hub owns (or cannot be reached).
  Future<HubChannel?> loadHubChannel(String conversationId) async {
    final c = conversation(conversationId);
    final stream = object(c?.raw['shared'])?['stream_id'] as String?;
    if (stream == null) return null;
    try {
      final (ch, _) = await api.getHubChannel(stream);
      hubChannels[conversationId] = ch;
      if (!_disposed) notifyListeners();
      return ch;
    } on Exception {
      return null;
    }
  }

  /// Reference patterns (ruling R73) by scope: '' for the server's, a
  /// conversation id for a channel's. Like the hub records they are held
  /// here, not stored: fetched when a composer opens and replaced by
  /// `reference_patterns.changed`.
  final Map<String, List<Json>> referencePatterns = {};

  /// The patterns that apply in [conversationId]: the server's, and the
  /// channel's own. A sealed chat has none of its own.
  List<Json> patternsFor(String? conversationId) => [
    ...?referencePatterns[''],
    if (conversationId != null) ...?referencePatterns[conversationId],
  ];

  /// Fetches the patterns of [conversationId] and the server's, once.
  Future<void> loadReferencePatterns(String? conversationId) async {
    final key = conversationId ?? '';
    if (referencePatterns.containsKey(key) &&
        referencePatterns.containsKey('')) {
      return;
    }
    try {
      final all = await api.listReferencePatterns(
        conversationId: conversationId,
      );
      referencePatterns[''] = [
        for (final p in all)
          if ((p['conversation_id'] ?? '') == '') p,
      ];
      if (conversationId != null) {
        referencePatterns[conversationId] = [
          for (final p in all)
            if (p['conversation_id'] == conversationId) p,
        ];
      }
      if (!_disposed) notifyListeners();
    } on Exception {
      // Offline: nothing is offered as a reference until it is fetched.
    }
  }

  /// Fetches the server's description again.
  Future<void> refreshInfo() async {
    try {
      info = await api.getServer();
      if (!_disposed) notifyListeners();
    } on Exception {
      // Offline: the app keeps its local copies.
    }
  }

  String? _shownProfile;

  /// The profile that is open, or null when none is: its presence is one
  /// the app is showing.
  void showingProfile(String? accountId) {
    if (_shownProfile == accountId) return;
    _shownProfile = accountId;
    _watchPresence();
  }

  /// Tells the server whose presence the app shows: the other person of
  /// each chat between two, which the sidebar lists with a presence light,
  /// the open profile, and the account itself. The server sends presence
  /// for those alone (docs/EVENTS.md section 4).
  void _watchPresence() {
    if (_disposed) return;
    final mine = me?.id;
    final ids = <String>{?mine, ?_shownProfile};
    for (final r in conversations) {
      final c = r.conversation;
      if (c.isNamed || c.memberIds.length != 2) continue;
      ids.addAll(c.memberIds);
    }
    live.watchPresence(ids);
  }

  /// How many people are online on the server, as `presence.counts` last
  /// said; null when the server has not said (an older server, a guest).
  int? onlineCount;

  /// The server broadcast showing (ruling R74): from `server.broadcast`
  /// and the status summary fetched at connect; null when none is. It
  /// carries `until`, after which it is not shown.
  Json? broadcast;

  /// The broadcasts dismissed on this device while the app runs.
  final dismissedBroadcasts = <String>{};

  /// The broadcast to show now: current, and not dismissed.
  Json? get broadcastShowing {
    final b = broadcast;
    if (b == null || dismissedBroadcasts.contains(b['id'])) return null;
    final until = parseTime(b['until']);
    if (until == null || !until.isAfter(DateTime.now())) return null;
    return b;
  }

  /// Hides the broadcast showing on this device.
  void dismissBroadcast() {
    final id = broadcast?['id'] as String?;
    if (id == null) return;
    dismissedBroadcasts.add(id);
    if (!_disposed) notifyListeners();
  }

  /// How many members are online in each large conversation that is on
  /// screen, by conversation id (`presence.counts`).
  Map<String, int> onlineIn = const {};

  // The conversations on screen, each with how many screens show it.
  final Map<String, int> _shown = {};

  /// A screen now shows [conversationId]: the server is told, with where
  /// the cache of it ends, and sends what came after and then its events
  /// as they happen (lazy delivery, docs/EVENTS.md section 4.1).
  void showConversation(String conversationId) {
    _shown[conversationId] = (_shown[conversationId] ?? 0) + 1;
    unawaited(_syncOpen());
  }

  /// The screen that showed [conversationId] has gone.
  void hideConversation(String conversationId) {
    final n = (_shown[conversationId] ?? 0) - 1;
    if (n > 0) {
      _shown[conversationId] = n;
    } else {
      _shown.remove(conversationId);
    }
    unawaited(_syncOpen());
  }

  Future<void> _syncOpen() async {
    if (_disposed) return;
    final Map<String, int> held;
    try {
      held = await db.positions();
    } on Object {
      return; // The database is closing.
    }
    if (_disposed) return;
    live.openConversations({
      for (final id in _shown.keys)
        if (held.containsKey(id)) id: held[id]!,
    });
  }

  void _onLocal(Json e) {
    switch (e['type']) {
      case 'presence.counts':
        final n = e['online_count'];
        onlineCount = n is num ? n.round() : null;
        final by = object(e['conversations']) ?? const <String, dynamic>{};
        onlineIn = {
          for (final entry in by.entries)
            if (entry.value is num) entry.key: (entry.value as num).round(),
        };
      case 'server.updated':
        final s = object(e['server']);
        if (s != null) info = ServerInfo.fromJson(s);
      case 'server.broadcast':
        broadcast = object(e['broadcast']);
      case 'reference_patterns.changed':
        final list = [
          for (final p in (e['patterns'] as List?) ?? const [])
            if (p is Map) p.cast<String, dynamic>(),
        ];
        referencePatterns[e['conversation_id'] as String? ?? ''] = list;
      case 'stream.channel_changed':
        final id = e['conversation_id'] as String?;
        final ch = object(e['channel']);
        if (id == null || ch == null) return;
        // As every member is shown it: `invited` and `requests` are
        // empty, and a moderator's screen fetches them again.
        hubChannels[id] = HubChannel(ch);
      default:
        return;
    }
    if (!_disposed) notifyListeners();
  }

  final _subs = <StreamSubscription<Object?>>[];
  bool _disposed = false;

  void _watch<T>(Stream<T> stream, void Function(T) set) {
    _subs.add(
      stream.listen((v) {
        set(v);
        if (!_disposed) notifyListeners();
      }),
    );
  }

  /// Opens the watches and draws from the database at once; then, unless
  /// [connect] is false (tests), sets badges early and opens the socket
  /// (docs/CLIENTS.md section 7, "On start").
  Future<void> start({bool connect = true}) async {
    me = await db.me();
    conversations = await db.conversations();
    sections = await db.sections();
    accounts = await db.accounts();
    summary = await db.statusSummary();
    _watch(db.watchMe(), (v) {
      me = v;
      _watchPresence();
    });
    _watch(db.watchConversations(), (v) {
      conversations = v;
      _watchPresence();
    });
    _watch(db.watchOutsideParents(), (v) => outsideParents = v);
    _watch(db.watchSections(), (v) => sections = v);
    _watch(db.watchAccounts(), (v) => accounts = v);
    _watch(db.watchGroups(), (v) => groups = v);
    _watch(db.watchEmoji(), (v) => emoji = v);
    _watch(db.watchStatusSummary(), (v) => summary = v);
    _watch(live.states, (v) => liveState = v);
    _subs.add(sync.localEvents.listen(_onLocal));
    notifyListeners();
    _watchPresence();
    if (connect) {
      unawaited(refreshInfo());
      unawaited(sync.refreshStatusSummary());
      live.start();
    }
  }

  /// The conversation with this id, if the account is in it.
  Conversation? conversation(String id) {
    for (final r in conversations) {
      if (r.conversation.id == id) return r.conversation;
    }
    return outsideParents[id];
  }

  /// Stops the connection and closes the database.
  Future<void> shutDown() async {
    _disposed = true;
    for (final s in _subs) {
      await s.cancel();
    }
    await live.dispose();
    api.close();
    await db.close();
  }
}
