/// The person's own state on one server, and the lists that belong to
/// them alone: status in full, availability, do not disturb, quiet hours,
/// presets, sessions, devices, bookmarks and followed threads
/// (docs/FEATURES.md section 3, `Me`, `Dnd`, `QuietHours`,
/// `StatusPreset`, `Session`, `Device`, `Bookmark`, `ThreadEntry` in
/// docs/openapi.yaml).
library;

import 'account.dart';
import 'json.dart';
import 'message.dart';

/// A status with both switches (`Status`). [Status] in `account.dart`
/// carries only what other screens draw; this reads the rest from the
/// same JSON.
class FullStatus {
  const FullStatus({
    this.emoji,
    this.text,
    this.until,
    this.pauseNotifications = false,
    this.appearAway = false,
  });

  factory FullStatus.fromJson(Json j) => FullStatus(
    emoji: _nonEmpty(j['emoji']),
    text: _nonEmpty(j['text']),
    until: parseTime(j['until']),
    pauseNotifications: j['pause_notifications'] == true,
    appearAway: j['appear_away'] == true,
  );

  final String? emoji;
  final String? text;
  final DateTime? until;
  final bool pauseNotifications;
  final bool appearAway;

  /// A status exists while it has an emoji or text and has not run out
  /// (docs/FEATURES.md 3.3).
  bool existsAt(DateTime now) =>
      (emoji != null || text != null) && (until == null || until!.isAfter(now));

  /// What two servers must agree on for the status to count as shared.
  /// The end time is compared to the minute: two calls a moment apart
  /// give two slightly different times.
  String get signature =>
      '${emoji ?? ''}|${text ?? ''}|${until == null ? '' : until!.millisecondsSinceEpoch ~/ 60000}'
      '|$pauseNotifications|$appearAway';

  Json toRequest() => {
    'emoji': ?emoji,
    'text': ?text,
    if (until != null) 'until': formatTime(until!),
    'pause_notifications': pauseNotifications,
    'appear_away': appearAway,
  };
}

String? _nonEmpty(Object? v) => v is String && v.isNotEmpty ? v : null;

/// In a meeting or out of office, with an optional end (`Availability`).
class Availability {
  const Availability(this.value, {this.until, this.setBy});

  factory Availability.fromJson(Json j) => Availability(
    j['value'] as String? ?? 'meeting',
    until: parseTime(j['until']),
    setBy: j['set_by'] as String?,
  );

  /// `meeting` or `out_of_office`.
  final String value;
  final DateTime? until;

  /// `self` or `integration`.
  final String? setBy;

  bool get outOfOffice => value == 'out_of_office';
  bool activeAt(DateTime now) => until == null || until!.isAfter(now);
}

/// Whether the account is in do not disturb, and why (`Dnd`).
class DndState {
  const DndState({
    this.on = false,
    this.sources = const [],
    this.manualUntil,
    this.until,
  });

  factory DndState.fromJson(Json j) => DndState(
    on: j['on'] == true,
    sources: strings(j['sources']),
    manualUntil: parseTime(j['manual_until']),
    until: parseTime(j['until']),
  );

  final bool on;

  /// `manual`, `status`, `meeting`, `quiet`.
  final List<String> sources;
  final DateTime? manualUntil;
  final DateTime? until;

  bool get manual => sources.contains('manual');
}

/// One server's quiet hours (`QuietHours`). Days are seven characters,
/// Monday first, `1` for a quiet day.
class QuietHours {
  const QuietHours({
    required this.start,
    required this.end,
    required this.days,
    this.timeZone = '',
    this.skipUntil,
  });

  factory QuietHours.fromJson(Json j) => QuietHours(
    start: j['start'] as String? ?? '22:00',
    end: j['end'] as String? ?? '07:00',
    days: j['days'] as String? ?? '1111111',
    timeZone: j['time_zone'] as String? ?? '',
    skipUntil: parseTime(j['skip_until']),
  );

  final String start;
  final String end;
  final String days;
  final String timeZone;
  final DateTime? skipUntil;

  Json toRequest() => {'start': start, 'end': end, 'days': days};

  Json toJson() => {
    ...toRequest(),
    'time_zone': timeZone,
    if (skipUntil != null) 'skip_until': formatTime(skipUntil!),
  };

  bool sameScheduleAs(QuietHours o) =>
      start == o.start && end == o.end && days == o.days;
}

/// A saved status (`StatusPreset`): a community one made by admins, or
/// one of the person's own.
class StatusPreset {
  StatusPreset({
    required this.id,
    required this.community,
    required this.text,
    this.emoji,
    this.durationSeconds,
    this.pauseNotifications = false,
    this.appearAway = false,
    this.availability,
    this.position = 0,
    this.raw = const {},
  });

  factory StatusPreset.fromJson(Json j) => StatusPreset(
    id: j['id'] as String? ?? '',
    community: j['community'] == true,
    text: j['text'] as String? ?? '',
    emoji: _nonEmpty(j['emoji']),
    durationSeconds: j['duration_seconds'] == null
        ? null
        : asInt(j['duration_seconds']),
    pauseNotifications: j['pause_notifications'] == true,
    appearAway: j['appear_away'] == true,
    availability: j['availability'] as String?,
    position: asInt(j['position']),
    raw: j,
  );

  final String id;
  final bool community;
  final String text;
  final String? emoji;
  final int? durationSeconds;
  final bool pauseNotifications;
  final bool appearAway;
  final String? availability;
  final int position;
  final Json raw;

  /// What makes two servers' presets "the same preset" for the app's
  /// shared list (docs/CLIENTS.md 8.6).
  String get signature =>
      '${emoji ?? ''}|$text|${durationSeconds ?? ''}|$pauseNotifications|$appearAway|${availability ?? ''}';

  Json toInput() => {
    'emoji': ?emoji,
    'text': text,
    'duration_seconds': ?durationSeconds,
    'pause_notifications': pauseNotifications,
    'appear_away': appearAway,
    'availability': ?availability,
  };
}

/// A signed-in session (`Session`). Token hashes are never returned.
class SessionInfo {
  SessionInfo(this.raw);
  final Json raw;
  String get id => raw['id'] as String? ?? '';

  /// `web`, `app`, `personal` or `bot`.
  String get kind => raw['kind'] as String? ?? 'web';
  String get label => raw['label'] as String? ?? '';
  String? get signedInBy => raw['signed_in_by'] as String?;
  bool get current => raw['current'] == true;
  DateTime? get createdAt => parseTime(raw['created_at']);
  DateTime? get lastSeenAt => parseTime(raw['last_seen_at']);
  DateTime? get expiresAt => parseTime(raw['expires_at']);
}

/// A device (`Device`): an app or browser that holds keys or a push
/// address.
class DeviceInfo {
  DeviceInfo(this.raw);
  final Json raw;
  String get id => raw['id'] as String? ?? '';
  String get name => raw['name'] as String? ?? '';
  String get platform => raw['platform'] as String? ?? '';
  bool get current => raw['current'] == true;
  DateTime? get retiredAt => parseTime(raw['retired_at']);
  DateTime? get createdAt => parseTime(raw['created_at']);
  DateTime? get lastSeenAt => parseTime(raw['last_seen_at']);
}

/// A bookmarked message (`Bookmark`).
class Bookmark {
  Bookmark(this.messageId, this.createdAt, this.message);

  factory Bookmark.fromJson(Json j) => Bookmark(
    j['message_id'] as String? ?? '',
    parseTime(j['created_at']) ?? DateTime.now().toUtc(),
    object(j['message']) == null
        ? null
        : Message.fromJson(object(j['message'])!),
  );

  final String messageId;
  final DateTime createdAt;
  final Message? message;
}

/// A followed thread from `listThreads` (`ThreadEntry`).
class ThreadEntry {
  ThreadEntry(this.state, this.parent);

  factory ThreadEntry.fromJson(Json j) => ThreadEntry(
    ThreadState.fromJson(object(j['thread_state']) ?? const {}),
    Message.fromJson(object(j['parent']) ?? const {}),
  );

  final ThreadState state;
  final Message parent;
}

/// The fields of `Account` and `Me` that [Account] does not parse.
extension AccountMore on Account {
  FullStatus? get fullStatus => object(raw['status']) == null
      ? null
      : FullStatus.fromJson(object(raw['status'])!);
  Availability? get availability => object(raw['availability']) == null
      ? null
      : Availability.fromJson(object(raw['availability'])!);
  String? get timeZone => _nonEmpty(raw['time_zone']);
}

/// The parts of `Me` only its owner sees that [Me] does not parse.
extension MeMore on Me {
  DndState get dndState =>
      DndState.fromJson(object(raw['dnd_state']) ?? const {});
  QuietHours? get quietHours => object(raw['quiet_hours']) == null
      ? null
      : QuietHours.fromJson(object(raw['quiet_hours'])!);
  bool get meetingPauses => object(raw['prefs'])?['meeting_pauses'] == true;

  /// Appear away: null for off, an end time, or [appearAwayForever].
  bool get appearAwayOn => object(raw['appear_away'])?['on'] == true;
  DateTime? get appearAwayUntil =>
      parseTime(object(raw['appear_away'])?['until']);
  List<String> get blockedIds => strings(raw['blocked_ids']);
}
