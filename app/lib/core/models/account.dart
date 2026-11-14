import 'json.dart';

/// The status a person or bot sets (`Status` in the contract).
class Status {
  Status({this.emoji, this.text, this.until, this.appearAway = false});

  factory Status.fromJson(Json j) => Status(
    emoji: j['emoji'] as String?,
    text: j['text'] as String?,
    until: parseTime(j['until']),
    appearAway: j['appear_away'] == true,
  );

  final String? emoji;
  final String? text;
  final DateTime? until;
  final bool appearAway;
}

/// A person, a bot, or the local shadow of someone on another server, as
/// other accounts see it (`Account` in the contract).
class Account {
  Account({
    required this.id,
    required this.username,
    required this.displayName,
    required this.role,
    required this.presence,
    this.serverName,
    this.avatarUrl,
    this.title,
    this.pronouns,
    this.status,
    this.dnd = false,
    this.inCall = false,
    this.groupIds = const [],
    this.createdAt,
    this.removedAt,
    this.raw = const {},
  });

  factory Account.fromJson(Json j) => Account(
    id: j['id'] as String? ?? '',
    username: j['username'] as String? ?? '',
    displayName: j['display_name'] as String? ?? '',
    role: j['role'] as String? ?? 'member',
    presence: j['presence'] as String? ?? 'offline',
    serverName: j['server_name'] as String?,
    avatarUrl: j['avatar_url'] as String?,
    title: j['title'] as String?,
    pronouns: j['pronouns'] as String?,
    status: object(j['status']) == null
        ? null
        : Status.fromJson(object(j['status'])!),
    dnd: j['dnd'] == true,
    inCall: j['in_call'] == true,
    groupIds: strings(j['group_ids']),
    createdAt: parseTime(j['created_at']),
    removedAt: parseTime(j['removed_at']),
    raw: j,
  );

  final String id;
  final String username;
  final String displayName;

  /// `owner`, `admin`, `member`, `guest`, `bot` or `remote`. Kept as the
  /// server's word so a role this build does not know still round-trips.
  final String role;

  /// `online`, `away` or `offline`.
  final String presence;

  /// Set for a person from another server, shown as `username@server`.
  final String? serverName;

  /// A path on the server, fetched with the same credential as the API.
  final String? avatarUrl;
  final String? title;
  final String? pronouns;
  final Status? status;
  final bool dnd;
  final bool inCall;
  final List<String> groupIds;
  final DateTime? createdAt;
  final DateTime? removedAt;

  /// The object as the server sent it, which is what the database keeps.
  final Json raw;

  bool get isBot => role == 'bot';

  /// The name to draw: the display name, or the username if it is empty.
  String get name => displayName.isEmpty ? username : displayName;

  Json toJson() => raw;
}

/// The caller's own account: the public shape plus what only they see
/// (`Me` in the contract).
class Me extends Account {
  Me._(Account a, this.threadView, this.allowNotifyAnyway, this.dndOn)
    : super(
        id: a.id,
        username: a.username,
        displayName: a.displayName,
        role: a.role,
        presence: a.presence,
        serverName: a.serverName,
        avatarUrl: a.avatarUrl,
        title: a.title,
        pronouns: a.pronouns,
        status: a.status,
        dnd: a.dnd,
        inCall: a.inCall,
        groupIds: a.groupIds,
        createdAt: a.createdAt,
        removedAt: a.removedAt,
        raw: a.raw,
      );

  factory Me.fromJson(Json j) {
    final prefs = object(j['prefs']) ?? const {};
    return Me._(
      Account.fromJson(j),
      prefs['thread_view'] as String? ?? 'threaded',
      prefs['allow_notify_anyway'] != false,
      object(j['dnd_state'])?['on'] == true,
    );
  }

  /// The person's default thread view on this server: `threaded` or `flat`.
  final String threadView;
  final bool allowNotifyAnyway;

  /// Whether do not disturb is on for any reason.
  final bool dndOn;
}
