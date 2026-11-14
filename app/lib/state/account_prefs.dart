import 'dart:convert';

import '../core/db/app_db.dart';
import 'app_prefs.dart';

/// The account area's app preferences (docs/CLIENTS.md 8.2, 9.12 "For
/// this device", 9.23), kept in `app.db` beside the others. Servers are
/// named by their address, never by a local id, so the same choices mean
/// the same thing after "Set up another device".
extension AccountPrefs on AppPrefs {
  /// Servers opted out of shared status and do not disturb (8.6).
  Set<String> get sharedOptOut =>
      AppDb.decodeList(raw('shared_opt_out')).toSet();

  bool takesPartInShared(String address) => !sharedOptOut.contains(address);

  void setTakesPartInShared(String address, bool value) {
    final next = sharedOptOut;
    value ? next.remove(address) : next.add(address);
    putRaw('shared_opt_out', jsonEncode(next.toList()));
  }

  /// Servers muted from the app (8.8): address to the end of the mute, in
  /// milliseconds since the epoch.
  Map<String, int> get mutedServers {
    try {
      return (jsonDecode(raw('muted_servers') ?? '{}') as Map).map(
        (k, v) => MapEntry(k as String, (v as num).toInt()),
      );
    } on Object {
      return {};
    }
  }

  /// Whether a server is muted from the app at [now].
  bool isServerMuted(String address, DateTime now) {
    final until = mutedServers[address];
    return until != null && until > now.millisecondsSinceEpoch;
  }

  DateTime? serverMutedUntil(String address) {
    final until = mutedServers[address];
    return until == null ? null : DateTime.fromMillisecondsSinceEpoch(until);
  }

  void setServerMuted(String address, DateTime? until) {
    final next = mutedServers;
    until == null
        ? next.remove(address)
        : next[address] = until.millisecondsSinceEpoch;
    putRaw('muted_servers', jsonEncode(next));
  }

  /// The start-up sound, played once at launch. On out of the box.
  bool get startupSound => raw('startup_sound') != '0';
  set startupSound(bool v) => putRaw('startup_sound', v ? '1' : '0');

  /// Whether notifications are shown at all, their sounds, and whether a
  /// notification shows the message text or only the sender.
  bool get notificationsOn => raw('notifications_on') != '0';
  set notificationsOn(bool v) => putRaw('notifications_on', v ? '1' : '0');
  bool get notificationSounds => raw('notification_sounds') != '0';
  set notificationSounds(bool v) =>
      putRaw('notification_sounds', v ? '1' : '0');
  bool get notificationText => raw('notification_text') != '0';
  set notificationText(bool v) => putRaw('notification_text', v ? '1' : '0');

  /// The file cache's cap, in megabytes. 1 GB out of the box.
  int get fileCacheMb => int.tryParse(raw('file_cache_mb') ?? '') ?? 1024;
  set fileCacheMb(int v) => putRaw('file_cache_mb', '$v');

  /// The language, as a locale tag, or null to follow the system.
  String? get language {
    final v = raw('language');
    return v == null || v.isEmpty ? null : v;
  }

  set language(String? v) => putRaw('language', v ?? '');

  /// The status presets saved from the app, by their signature
  /// (`StatusPreset.signature`): the shared ones listed first (8.6).
  Set<String> get sharedPresets =>
      AppDb.decodeList(raw('shared_presets')).toSet();

  void addSharedPreset(String signature) => putRaw(
    'shared_presets',
    jsonEncode({...sharedPresets, signature}.toList()),
  );

  void removeSharedPreset(String signature) => putRaw(
    'shared_presets',
    jsonEncode((sharedPresets..remove(signature)).toList()),
  );

  /// Quiet-hours schedules switched off, kept to offer again: address to
  /// `start|end|days`.
  Map<String, String> get oldQuietHours {
    try {
      return (jsonDecode(raw('old_quiet_hours') ?? '{}') as Map)
          .cast<String, String>();
    } on Object {
      return {};
    }
  }

  void keepOldQuietHours(String address, String schedule) => putRaw(
    'old_quiet_hours',
    jsonEncode({...oldQuietHours, address: schedule}),
  );
}
