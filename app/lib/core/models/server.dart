import 'json.dart';

/// What a server says about itself before and after sign-in (`Server`,
/// from `getServer`).
class ServerInfo {
  ServerInfo({
    required this.name,
    required this.communityName,
    required this.version,
    required this.minAppVersion,
    required this.setupNeeded,
    this.timeZone = 'UTC',
    this.iconUrl,
    this.fileSizeLimit = 0,
    this.messageTextLimit = 16000,
    this.features = const {},
    this.defaultThemes,
    this.pushKinds = const [],
    this.welcome,
  });

  factory ServerInfo.fromJson(Json j) {
    final limits = object(j['limits']) ?? const {};
    return ServerInfo(
      name: j['name'] as String? ?? '',
      communityName: j['community_name'] as String? ?? '',
      version: j['version'] as String? ?? '',
      minAppVersion: j['min_app_version'] as String? ?? '',
      setupNeeded: j['setup_needed'] == true,
      timeZone: j['time_zone'] as String? ?? 'UTC',
      iconUrl: j['icon_url'] as String?,
      fileSizeLimit: asInt(limits['file_size']),
      messageTextLimit: asInt(limits['message_text'], 16000),
      features: {
        for (final e in (object(j['features']) ?? const {}).entries)
          if (e.value is bool) e.key: e.value as bool,
      },
      defaultThemes: object(j['default_themes']),
      pushKinds: strings(j['push_kinds']),
      welcome: j['welcome'] as String?,
    );
  }

  /// The ways this server can wake a phone (`apns`, `fcm`,
  /// `unifiedpush`); empty when the relay and UnifiedPush are both off
  /// (docs/PUSH.md section 3).
  final List<String> pushKinds;

  /// The server welcome text, Churr Markdown, sent to a signed-in caller
  /// (`/motd`, ruling R74). Null or empty when there is none.
  final String? welcome;

  /// The server's host name.
  final String name;

  /// What the community calls itself: the name the apps show.
  final String communityName;

  /// The server's time zone (`server.time_zone`): typed times are read in
  /// it for an account with no time zone of its own.
  final String timeZone;
  final String version;
  final String minAppVersion;
  final bool setupNeeded;
  final String? iconUrl;
  final int fileSizeLimit;
  final int messageTextLimit;

  /// Which features are switched on (`sealed_chats`, `calls`, …).
  final Map<String, bool> features;

  /// `default_themes`: the server's default dark and light themes, as
  /// sent (read with `DefaultThemes.fromJson`). Null when none is set.
  final Json? defaultThemes;
}

/// Totals for the icon badge (`StatusSummary`, from `getStatusSummary`
/// and carried as `total` on badge and read events).
class StatusSummary {
  const StatusSummary({
    this.badge = 0,
    this.mentions = 0,
    this.unreadConversations = 0,
    this.unreadThreads = 0,
    this.newest,
    this.ringingCallId,
    this.dnd = false,
    this.dndUntil,
    this.broadcast,
  });

  factory StatusSummary.fromJson(Json j) => StatusSummary(
    badge: asInt(j['badge']),
    mentions: asInt(j['mentions']),
    unreadConversations: asInt(j['unread_conversations']),
    unreadThreads: asInt(j['unread_threads']),
    newest: j['newest'] as String?,
    ringingCallId: j['ringing_call_id'] as String?,
    dnd: j['dnd'] == true,
    dndUntil: j['dnd_until'] as String?,
    broadcast: object(j['broadcast']),
  );

  final int badge;
  final int mentions;
  final int unreadConversations;
  final int unreadThreads;

  /// Opaque; changes whenever something newer exists (docs/PUSH.md 11).
  final String? newest;

  /// A call ringing for this account right now.
  final String? ringingCallId;

  /// In do not disturb from any cause, and when that ends if known
  /// (an RFC 3339 time, kept as the server wrote it).
  final bool dnd;
  final String? dndUntil;

  /// The server broadcast showing when this was fetched (ruling R74).
  final Json? broadcast;

  Json toJson() => {
    'badge': badge,
    'mentions': mentions,
    'unread_conversations': unreadConversations,
    'unread_threads': unreadThreads,
    'newest': ?newest,
    'ringing_call_id': ?ringingCallId,
    'dnd': dnd,
    'dnd_until': ?dndUntil,
    'broadcast': ?broadcast,
  };
}

/// A custom emoji (`Emoji`).
class CustomEmoji {
  CustomEmoji({required this.id, required this.name, required this.url});

  factory CustomEmoji.fromJson(Json j) => CustomEmoji(
    id: j['id'] as String? ?? '',
    name: j['name'] as String? ?? '',
    url: j['url'] as String? ?? '',
  );

  final String id;
  final String name;

  /// A path on the server.
  final String url;

  Json toJson() => {'id': id, 'name': name, 'url': url};
}

/// A named set of accounts with a handle (`Group`): what drawing a group
/// mention needs, and what joining or leaving an open group needs.
class Group {
  Group({
    required this.id,
    required this.handle,
    this.isMember = false,
    this.name = '',
    this.description = '',
    this.open = false,
    this.builtin,
    this.fromProvider = false,
    this.memberCount = 0,
  });

  factory Group.fromJson(Json j) => Group(
    id: j['id'] as String? ?? '',
    handle: j['handle'] as String? ?? '',
    isMember: j['is_member'] == true,
    name: j['name'] as String? ?? '',
    description: j['description'] as String? ?? '',
    open: j['open'] == true,
    builtin: j['builtin'] as String?,
    fromProvider: j['provider_id'] != null || j['from_provider'] == true,
    memberCount: (j['member_count'] as num?)?.toInt() ?? 0,
  );

  final String id;
  final String handle;

  /// Whether the reader is in it: a mention of it is then highlighted.
  final bool isMember;
  final String name;
  final String description;

  /// Members may join and leave it themselves.
  final bool open;

  /// `everyone` or `admins` for the two built-in groups.
  final String? builtin;

  /// Filled from a sign-in provider, so nobody joins it by hand.
  final bool fromProvider;
  final int memberCount;

  /// Whether a person with [role] may join or leave it themselves: an
  /// open group, not built in, not filled from a provider, and not for
  /// guests or bots, whom admins put in groups.
  bool selfServe(String? role) =>
      open &&
      builtin == null &&
      !fromProvider &&
      (role == 'owner' || role == 'admin' || role == 'member');

  Json toJson() => {
    'id': id,
    'handle': handle,
    'is_member': isMember,
    'name': name,
    'description': description,
    'open': open,
    'builtin': ?builtin,
    if (fromProvider) 'from_provider': true,
    'member_count': memberCount,
  };
}
