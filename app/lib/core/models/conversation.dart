import 'json.dart';

/// What notifies in a conversation (`membership.mode`, ruling R46).
enum NotifyMode {
  any,
  mentions,
  nothing;

  /// A mode this build does not know is read as `mentions`, the default
  /// of named channels, so nothing is silently dropped or made loud.
  static NotifyMode parse(Object? v) => switch (v) {
    'any' => any,
    'nothing' => nothing,
    _ => mentions,
  };
}

/// Whether a conversation has a row in the sidebar (`membership.show`,
/// ruling R46). A null `show` on a membership means "the app's default",
/// which is an app preference and is always one of the first three.
enum ShowSetting {
  always,
  activity,
  notification,
  hidden;

  static ShowSetting? parse(Object? v) => switch (v) {
    'always' => always,
    'activity' => activity,
    'notification' => notification,
    'hidden' => hidden,
    _ => null,
  };
}

/// The caller's own place in a conversation, with the settings only they
/// see (`Membership` in the contract).
class Membership {
  Membership({
    required this.level,
    required this.readSeq,
    required this.mode,
    this.mutedUntil,
    this.show,
    this.starred = false,
    this.threadView,
    this.unreadCount = 0,
    this.mentionCount = 0,
    this.badgeCount = 0,
    this.joinedAt,
    this.timedOutUntil,
    this.ignoreBroadMentions = false,
  });

  factory Membership.fromJson(Json j) => Membership(
    level: j['level'] as String? ?? 'member',
    readSeq: asInt(j['read_seq']),
    mode: NotifyMode.parse(j['mode']),
    mutedUntil: parseTime(j['muted_until']),
    show: ShowSetting.parse(j['show']),
    starred: j['starred'] == true,
    threadView: j['thread_view'] as String?,
    unreadCount: asInt(j['unread_count']),
    mentionCount: asInt(j['mention_count']),
    badgeCount: asInt(j['badge_count']),
    joinedAt: parseTime(j['joined_at']),
    timedOutUntil: parseTime(j['timed_out_until']),
    ignoreBroadMentions: j['ignore_broad_mentions'] == true,
  );

  /// `reader`, `member` or `manager`.
  final String level;
  final int readSeq;
  final NotifyMode mode;

  /// While set and in the future the conversation behaves as `nothing`.
  final DateTime? mutedUntil;

  /// Null means the app's default (an app preference on the device).
  final ShowSetting? show;
  final bool starred;

  /// `threaded`, `flat`, or null for the person's default.
  final String? threadView;

  /// Unread messages, whether or not they notify.
  final int unreadCount;
  final int mentionCount;

  /// The count the mode notifies: what the badge shows.
  final int badgeCount;
  final DateTime? joinedAt;

  /// Set while the person is timed out in the conversation: they may
  /// read but not post until then (docs/FEATURES.md section 15).
  final DateTime? timedOutUntil;

  /// `@channel`, `@here` and `@everyone` do not notify the person here
  /// (ruling R64). A mention by name or of another group still does.
  final bool ignoreBroadMentions;

  /// Whether the mute is still running at [now].
  bool mutedAt(DateTime now) => mutedUntil != null && mutedUntil!.isAfter(now);

  Membership copyWith({
    int? readSeq,
    int? unreadCount,
    int? mentionCount,
    int? badgeCount,
  }) => Membership(
    level: level,
    readSeq: readSeq ?? this.readSeq,
    mode: mode,
    mutedUntil: mutedUntil,
    show: show,
    starred: starred,
    threadView: threadView,
    unreadCount: unreadCount ?? this.unreadCount,
    mentionCount: mentionCount ?? this.mentionCount,
    badgeCount: badgeCount ?? this.badgeCount,
    joinedAt: joinedAt,
    timedOutUntil: timedOutUntil,
    ignoreBroadMentions: ignoreBroadMentions,
  );

  Json toJson() => {
    'level': level,
    'read_seq': readSeq,
    'mode': mode.name,
    if (mutedUntil != null) 'muted_until': formatTime(mutedUntil!),
    if (show != null) 'show': show!.name,
    'starred': starred,
    if (threadView != null) 'thread_view': threadView,
    'unread_count': unreadCount,
    'mention_count': mentionCount,
    'badge_count': badgeCount,
    if (joinedAt != null) 'joined_at': formatTime(joinedAt!),
    if (timedOutUntil != null) 'timed_out_until': formatTime(timedOutUntil!),
    'ignore_broad_mentions': ignoreBroadMentions,
  };
}

/// A channel (named or ad-hoc) or a sealed chat (`Conversation` in the
/// contract).
class Conversation {
  Conversation({
    required this.id,
    required this.kind,
    required this.private,
    this.name,
    this.topic = '',
    this.sectionId,
    this.parentId,
    this.lockedAt,
    this.archivedAt,
    this.createdAt,
    this.lastSeq = 0,
    this.lastMessageAt,
    this.memberCount = 0,
    this.memberIds = const [],
    this.shared = false,
    this.membership,
    this.raw = const {},
  });

  factory Conversation.fromJson(Json j) => Conversation(
    id: j['id'] as String? ?? '',
    kind: j['kind'] as String? ?? 'channel',
    private: j['private'] == true,
    name: j['name'] as String?,
    topic: j['topic'] as String? ?? '',
    sectionId: j['section_id'] as String?,
    parentId: j['parent_id'] as String?,
    lockedAt: parseTime(j['locked_at']),
    archivedAt: parseTime(j['archived_at']),
    createdAt: parseTime(j['created_at']),
    lastSeq: asInt(j['last_seq']),
    lastMessageAt: parseTime(j['last_message_at']),
    memberCount: asInt(j['member_count']),
    memberIds: strings(j['member_ids']),
    shared: j['shared'] is Map,
    membership: object(j['membership']) == null
        ? null
        : Membership.fromJson(object(j['membership'])!),
    raw: j,
  );

  final String id;

  /// `channel` or `sealed`.
  final String kind;
  final bool private;

  /// Free-form, 1 to 64 characters of any script, and not unique (R50,
  /// R51): a channel is identified by [id] everywhere. Null for an ad-hoc
  /// channel and a sealed chat, which are drawn from [memberIds].
  final String? name;
  final String topic;

  /// The admin's section. Ignored for display while [parentId] is set.
  final String? sectionId;

  /// The parent channel of a sub-channel, one level deep (R49). Absent
  /// when the caller cannot see the parent.
  final String? parentId;
  final DateTime? lockedAt;
  final DateTime? archivedAt;
  final DateTime? createdAt;

  /// The conversation's newest sequence number.
  final int lastSeq;
  final DateTime? lastMessageAt;
  final int memberCount;

  /// The members of an ad-hoc channel or sealed chat.
  final List<String> memberIds;

  /// Shared with other servers.
  final bool shared;

  /// The caller's own membership, or null for a public channel the caller
  /// reads without having joined.
  final Membership? membership;

  /// The object as the server sent it.
  final Json raw;

  bool get isSealed => kind == 'sealed';

  /// A named channel, as opposed to an ad-hoc one.
  bool get isNamed => kind == 'channel' && name != null && name!.isNotEmpty;
  bool get isAdHoc => kind == 'channel' && !isNamed;
  bool get isLocked => lockedAt != null;
  bool get isArchived => archivedAt != null;

  /// Who may use `@channel`, `@here` and `@everyone` here: `server` (the
  /// server's settings decide), `anyone` or `managers` (ruling R64).
  String get broadMentions => raw['broad_mentions'] as String? ?? 'server';

  /// Whether this reader may use them here now, as the server last said.
  /// Null when it has not said: the server is asked before sending.
  bool? get broadMentionsAllowed => raw['broad_mentions_allowed'] as bool?;

  /// The API shape, with the membership as it now stands.
  Json toJson() => {
    ...raw,
    if (membership != null) 'membership': membership!.toJson(),
  };

  Conversation withMembership(Membership? m) {
    final j = {...raw}..remove('membership');
    if (m != null) j['membership'] = m.toJson();
    return Conversation.fromJson(j);
  }
}

/// A heading the admins define to group channels in the list.
class Section {
  Section({required this.id, required this.name, required this.position});

  factory Section.fromJson(Json j) => Section(
    id: j['id'] as String? ?? '',
    name: j['name'] as String? ?? '',
    position: asInt(j['position']),
  );

  final String id;
  final String name;
  final int position;

  Json toJson() => {'id': id, 'name': name, 'position': position};
}
