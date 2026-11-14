import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';

import '../core/account_sync.dart';
import '../core/api/account.dart';
import '../core/api/errors.dart';
import '../core/api/me.dart';
import '../core/db/account_tables.dart';
import '../core/live.dart';
import '../core/models/account.dart';
import '../core/models/json.dart';
import '../core/models/personal.dart';
import '../core/oauth_metadata.dart';
import 'account_prefs.dart';
import 'app_prefs.dart';
import 'app_state.dart';
import 'server_state.dart';

/// A change the app makes once for every server taking part
/// (docs/CLIENTS.md 8.6): what it is and what it carries, so a server
/// that cannot be reached now can be sent it when it next connects.
class SharedChange {
  const SharedChange(this.kind, this.payload);

  /// `status`, `dnd`, `availability` or `away`. A later change of the same
  /// kind replaces a waiting one: "unless the person has changed it again
  /// since".
  final String kind;

  /// `{"clear": true}` to clear; otherwise the request's fields.
  final Json payload;

  bool get clears => payload['clear'] == true;

  Json toJson() => {'kind': kind, 'payload': payload};

  /// Sends it to one server and returns the account as the server now
  /// has it (or null where the answer does not carry it).
  Future<Me?> applyTo(ServerState s) async {
    final api = s.api;
    switch (kind) {
      case 'status':
        if (clears) {
          await api.clearStatus();
          return null;
        }
        return api.setStatus(status: FullStatus.fromJson(payload));
      case 'dnd':
        if (clears) {
          await api.clearDnd();
        } else {
          await api.setDnd(until: parseTime(payload['until']));
        }
        return null;
      case 'availability':
        if (clears) {
          await api.clearAvailability();
          return null;
        }
        return api.setAvailability(
          payload['value'] as String,
          until: parseTime(payload['until']),
        );
      case 'away':
        if (clears) {
          await api.clearAway();
          return null;
        }
        return api.setAway(until: parseTime(payload['until']));
    }
    return null;
  }
}

/// What one shared change did on each server: the servers that refused
/// it (by name, with the reason), and those it is waiting for.
class SharedResult {
  final refused = <String, String>{};
  final waiting = <String>[];
  bool get allDone => refused.isEmpty && waiting.isEmpty;
}

/// What the app does across servers for the person's own state
/// (docs/CLIENTS.md 8.1, 8.6 to 8.9; ruling R48): status, do not
/// disturb, availability and appear away set once; muting a whole
/// server; profile changes on chosen servers; and the housekeeping that
/// follows each connection (the personal lists, the notifications list
/// filled in, the time zone kept current, the cache trimmed).
///
/// There is one per [AppState], made on first use with [of].
class AccountHub extends ChangeNotifier {
  AccountHub(this.app, {DateTime Function()? now, this.deviceZone})
    : _now = now ?? DateTime.now;

  final AppState app;
  final DateTime Function() _now;

  /// The device's IANA time zone; [deviceZoneSource] when not given.
  final Future<String?> Function()? deviceZone;

  /// Set by `main` to the platform's answer (`lib/platform/device.dart`).
  static Future<String?> Function()? deviceZoneSource;

  static final _hubs = Expando<AccountHub>();

  /// The hub of [app], started on first use.
  static AccountHub of(AppState app) => _hubs[app] ??= AccountHub(app)..start();

  /// Installs a hub made by a test.
  static void install(AppState app, AccountHub hub) =>
      _hubs[app] = hub..start();

  AppPrefs get prefs => app.prefs;
  DateTime get now => _now();

  final _liveSubs = <String, StreamSubscription<LiveState>>{};
  final _trimmed = <String>{};
  Timer? _poll;
  bool _started = false;

  /// Whether the app is on a phone, where the rail layout connects only
  /// the selected server (docs/CLIENTS.md 8.1). Set by `main`.
  static bool phone = false;

  void start() {
    if (_started) return;
    _started = true;
    app.addListener(_onApp);
    prefs.addListener(_onApp);
    _onApp();
    // Badges of servers that are not connected stay current from the
    // status summary.
    if (app.connectOnStart) {
      _poll = Timer.periodic(const Duration(minutes: 2), (_) {
        for (final s in app.servers) {
          if (s.liveState != LiveState.ready &&
              s.liveState != LiveState.signedOut) {
            unawaited(s.sync.refreshStatusSummary());
          }
        }
      });
    }
  }

  @override
  void dispose() {
    app.removeListener(_onApp);
    prefs.removeListener(_onApp);
    _poll?.cancel();
    for (final s in _liveSubs.values) {
      s.cancel();
    }
    super.dispose();
  }

  void _onApp() {
    final ids = {for (final s in app.servers) s.localId};
    for (final id in _liveSubs.keys.toList()) {
      if (!ids.contains(id)) _liveSubs.remove(id)?.cancel();
    }
    for (final s in app.servers) {
      _liveSubs[s.localId] ??= s.live.states.listen((st) {
        if (st == LiveState.ready) unawaited(_afterReady(s));
        notifyListeners();
      });
      if (_trimmed.add(s.localId)) {
        unawaited(_trim(s));
        unawaited(_ensureMetadata(s));
      }
    }
    _applyConnectionPolicy();
  }

  /// Phones in the rail layout keep only the selected server connected
  /// and reconnect the others as they are opened. Everywhere else every
  /// server stays connected.
  void _applyConnectionPolicy() {
    if (!app.connectOnStart) return;
    final onlySelected = phone && prefs.layout == SidebarLayout.rail;
    final selected = app.open == null
        ? app.selectedServer
        : app.server(app.open!.serverId);
    for (final s in app.servers) {
      if (s.api.tokens == null || s.api.signedOut) continue;
      final want = !onlySelected || s == selected;
      final running =
          s.liveState != LiveState.offline || _connected.contains(s.localId);
      if (want && !running) {
        _connected.add(s.localId);
        s.live.start();
      } else if (!want && running) {
        _connected.remove(s.localId);
        unawaited(s.live.stop());
      }
    }
  }

  final _connected = <String>{};

  /// What follows each `ready`: anything waiting is sent, the personal
  /// lists are fetched, the notifications list is filled in where this
  /// device missed the events, and the time zone is kept current.
  Future<void> _afterReady(ServerState s) async {
    _connected.add(s.localId);
    await flushPending(s);
    await refreshPersonalLists(s.api, s.db);
    if (await s.db.setting('notifications_filled') == null) {
      try {
        await backfillNotifications(s.api, s.db);
        await s.db.putSetting('notifications_filled', '1');
      } on Object {
        // Tried again after the next `ready`.
      }
    }
    await _keepTimeZone(s);
  }

  Future<void> _keepTimeZone(ServerState s) async {
    final zone = await (deviceZone ?? deviceZoneSource)?.call();
    final me = s.me;
    if (zone == null || zone.isEmpty || me == null || me.timeZone == zone) return;
    try {
      await s.db.putMe(await s.api.updateMe({'time_zone': zone}));
    } on ApiException {
      // Next time.
    }
  }

  /// Token requests go to the endpoints of the server's OAuth metadata:
  /// kept from the last time, or fetched once.
  Future<void> _ensureMetadata(ServerState s) async {
    try {
      await restoreOAuthMetadata(s.db, s.api);
      if (s.api.tokenEndpoint != null || !app.connectOnStart) return;
      await storeOAuthMetadata(s.db, await fetchOAuthMetadata(s.api));
    } on Object {
      // The contract's paths serve until the next start.
    }
  }

  Future<void> _trim(ServerState s) async {
    try {
      await s.db.trim(now);
    } on Object {
      // A closed database in a test, or a busy one: next start.
    }
  }

  // ---- which servers take part ----

  /// Servers that shared status changes go to: not opted out, and signed
  /// in. A muted server is also left out of do not disturb changes.
  List<ServerState> participants({bool forDnd = false}) => [
    for (final s in app.servers)
      if (prefs.takesPartInShared(s.entry.address) &&
          !(forDnd && prefs.isServerMuted(s.entry.address, now)))
        s,
  ];

  // ---- shared changes ----

  /// Sends [change] to every server taking part. A server that cannot be
  /// reached keeps it and is sent it when it next connects; one that
  /// refuses it is named in the result.
  Future<SharedResult> applyShared(
    SharedChange change, {
    List<ServerState>? to,
  }) async {
    final result = SharedResult();
    final targets = to ?? participants(forDnd: change.kind == 'dnd');
    await Future.wait([
      for (final s in targets)
        () async {
          try {
            await sendTo(s, change);
            await s.db.putSetting('pending_${change.kind}', '');
          } on ApiException catch (e) {
            if (e.isNetwork || e.isTransient) {
              await s.db.putSetting(
                'pending_${change.kind}',
                jsonEncode(change.toJson()),
              );
              result.waiting.add(s.name);
            } else {
              result.refused[s.name] = e.message;
            }
          } on SignedOutException {
            await s.db.putSetting(
              'pending_${change.kind}',
              jsonEncode(change.toJson()),
            );
            result.waiting.add(s.name);
          }
        }(),
    ]);
    notifyListeners();
    return result;
  }

  /// Sends one change to one server and writes what came back. The
  /// server's events confirm the rest.
  Future<void> sendTo(ServerState s, SharedChange change) async {
    final me = await change.applyTo(s);
    if (me != null) {
      await s.db.putMe(me);
    } else {
      await s.db.putMe(await s.api.getMe());
    }
  }

  /// Sends what is waiting for [s], oldest kind first.
  Future<void> flushPending(ServerState s) async {
    for (final kind in const ['status', 'dnd', 'availability', 'away']) {
      final raw = await s.db.setting('pending_$kind');
      if (raw == null || raw.isEmpty) continue;
      try {
        final j = jsonDecode(raw) as Map<String, dynamic>;
        await sendTo(
          s,
          SharedChange(j['kind'] as String, (j['payload'] as Map).cast()),
        );
        await s.db.putSetting('pending_$kind', '');
      } on ApiException catch (e) {
        if (!(e.isNetwork || e.isTransient)) await s.db.putSetting('pending_$kind', '');
      } on Object {
        // Kept for the next connection.
      }
    }
  }

  /// Whether a change is waiting to be sent to [s].
  Future<bool> hasPending(ServerState s) async {
    for (final kind in const ['status', 'dnd', 'availability', 'away']) {
      final raw = await s.db.setting('pending_$kind');
      if (raw != null && raw.isNotEmpty) return true;
    }
    return false;
  }

  Future<SharedResult> setStatus(FullStatus status) =>
      applyShared(SharedChange('status', status.toRequest()));
  Future<SharedResult> clearStatus() =>
      applyShared(const SharedChange('status', {'clear': true}));

  /// Do not disturb until [until], or with null until turned off.
  Future<SharedResult> setDnd(DateTime? until) => applyShared(
    SharedChange('dnd', {if (until != null) 'until': formatTime(until)}),
  );

  /// Makes the person available now on every server taking part (R37).
  Future<SharedResult> clearDnd() =>
      applyShared(const SharedChange('dnd', {'clear': true}));

  Future<SharedResult> setAvailability(String value, DateTime? until) =>
      applyShared(
        SharedChange('availability', {
          'value': value,
          if (until != null) 'until': formatTime(until),
        }),
      );
  Future<SharedResult> clearAvailability() =>
      applyShared(const SharedChange('availability', {'clear': true}));

  Future<SharedResult> setAway(DateTime? until) => applyShared(
    SharedChange('away', {if (until != null) 'until': formatTime(until)}),
  );
  Future<SharedResult> clearAway() =>
      applyShared(const SharedChange('away', {'clear': true}));

  // ---- "differs on <server>" ----

  String _statusKey(Me? me) {
    final s = me?.fullStatus;
    return s == null || !s.existsAt(now) ? '' : s.signature;
  }

  /// The servers taking part whose status is not the first one's.
  List<ServerState> statusDiffers() => _differs(_statusKey);

  /// The servers taking part whose do not disturb is not the first one's.
  List<ServerState> dndDiffers() =>
      _differs((me) => '${me?.dndOn ?? false}', forDnd: true);

  List<ServerState> availabilityDiffers() => _differs((me) {
    final a = me?.availability;
    return a == null || !a.activeAt(now) ? '' : a.value;
  });

  List<ServerState> _differs(String Function(Me?) key, {bool forDnd = false}) {
    final p = participants(forDnd: forDnd);
    if (p.length < 2) return const [];
    final first = key(p.first.me);
    return [
      for (final s in p.skip(1))
        if (key(s.me) != first) s,
    ];
  }

  /// "Make them the same": the first server's status goes to the rest.
  Future<SharedResult> alignStatus() {
    final p = participants();
    final s = p.isEmpty ? null : p.first.me?.fullStatus;
    if (s == null || !s.existsAt(now)) return applyShared(const SharedChange('status', {'clear': true}), to: p.skip(1).toList(),);
    return applyShared(
      SharedChange('status', s.toRequest()),
      to: p.skip(1).toList(),
    );
  }

  Future<SharedResult> alignDnd() {
    final p = participants(forDnd: true);
    final me = p.isEmpty ? null : p.first.me;
    final on = me?.dndOn ?? false;
    final change = on
        ? SharedChange('dnd', {
            if (me!.dndState.manualUntil != null)
              'until': formatTime(me.dndState.manualUntil!),
          })
        : const SharedChange('dnd', {'clear': true});
    return applyShared(change, to: p.skip(1).toList());
  }

  // ---- muting a whole server (8.8) ----

  /// Turns do not disturb on for [s] alone until [until]. The person is
  /// told beforehand that people there will see the mark.
  Future<void> muteServer(ServerState s, DateTime until) async {
    prefs.setServerMuted(s.entry.address, until);
    await s.api.setDnd(until: until);
    await s.db.putMe(await s.api.getMe());
    notifyListeners();
  }

  Future<void> unmuteServer(ServerState s) async {
    prefs.setServerMuted(s.entry.address, null);
    await s.api.clearDnd();
    await s.db.putMe(await s.api.getMe());
    notifyListeners();
  }

  bool isMuted(ServerState s) => prefs.isServerMuted(s.entry.address, now);

  // ---- profile on chosen servers (8.9) ----

  /// Applies profile [changes] (and a new avatar) to each of [to]. A
  /// server that refuses a value is reported by name; the others still
  /// change. Usernames are never part of this.
  Future<Map<String, String>> applyProfile(
    Json changes, {
    required List<ServerState> to,
    String? avatarName,
    List<int>? avatarBytes,
    bool removeAvatar = false,
  }) async {
    assert(!changes.containsKey('username'));
    final refused = <String, String>{};
    await Future.wait([
      for (final s in to)
        () async {
          try {
            var me = changes.isEmpty ? null : await s.api.updateMe(changes);
            if (avatarBytes != null) {
              me = await s.api.setMyAvatar(
                avatarName ?? 'avatar.png',
                avatarBytes,
              );
            } else if (removeAvatar) {
              await s.api.removeMyAvatar();
              me = await s.api.getMe();
            }
            if (me != null) await s.db.putMe(me);
          } on ApiException catch (e) {
            refused[s.name] = e.message;
          } on SignedOutException {
            refused[s.name] = 'Signed out';
          }
        }(),
    ]);
    notifyListeners();
    return refused;
  }

  // ---- blocking ----

  /// Blocks or unblocks [accountId] on [s]: the database first, then the
  /// server; `block.added` confirms.
  Future<void> setBlocked(ServerState s, String accountId, bool blocked) async {
    final me = s.me;
    if (me != null) {
      final ids = {...me.blockedIds};
      blocked ? ids.add(accountId) : ids.remove(accountId);
      await s.db.putMe(Me.fromJson({...me.raw, 'blocked_ids': ids.toList()}));
    }
    try {
      blocked
          ? await s.api.blockAccount(accountId)
          : await s.api.unblockAccount(accountId);
    } on ApiException {
      if (me != null) await s.db.putMe(me);
      rethrow;
    }
  }

  // ---- presets (docs/FEATURES.md 3.4, docs/CLIENTS.md 8.6) ----

  /// Applies a preset on every server taking part: its emoji, text and
  /// switches, an end time from its duration, and its availability.
  Future<SharedResult> applyPreset(StatusPreset p) async {
    final until = p.durationSeconds == null
        ? null
        : now.add(Duration(seconds: p.durationSeconds!));
    final r = await setStatus(
      FullStatus(
        emoji: p.emoji,
        text: p.text.isEmpty ? null : p.text,
        until: until,
        pauseNotifications: p.pauseNotifications,
        appearAway: p.appearAway,
      ),
    );
    if (p.availability != null) {
      final a = await setAvailability(p.availability!, until);
      r.refused.addAll(a.refused);
      r.waiting.addAll(a.waiting.where((w) => !r.waiting.contains(w)));
    }
    return r;
  }

  /// "Save as preset" from the app: created on each server taking part,
  /// and listed first in the picker as a shared preset.
  Future<SharedResult> savePreset(StatusPreset p) async {
    final result = SharedResult();
    for (final s in participants()) {
      try {
        await s.api.createStatusPreset(p);
        await s.db.replaceStatusPresets(await s.api.listStatusPresets());
      } on ApiException catch (e) {
        result.refused[s.name] = e.message;
      }
    }
    prefs.addSharedPreset(p.signature);
    notifyListeners();
    return result;
  }

  /// Deletes a preset of the person's own on each server that has it.
  Future<void> deletePreset(StatusPreset p) async {
    for (final s in app.servers) {
      for (final q in await s.db.statusPresets()) {
        if (!q.community && q.signature == p.signature) {
          try {
            await s.api.deleteStatusPreset(q.id);
          } on ApiException {
            continue;
          }
        }
      }
      try {
        await s.db.replaceStatusPresets(await s.api.listStatusPresets());
      } on ApiException {
        // Next time the picker opens.
      }
    }
    prefs.removeSharedPreset(p.signature);
    notifyListeners();
  }

  /// The personal switch for whether a meeting pauses notifications, on
  /// every server taking part.
  Future<SharedResult> setMeetingPauses(bool on) async {
    final result = SharedResult();
    for (final s in participants()) {
      try {
        await s.api.updateMyPrefs({'meeting_pauses': on});
        await s.db.putMe(await s.api.getMe());
      } on ApiException catch (e) {
        result.refused[s.name] = e.message;
      }
    }
    notifyListeners();
    return result;
  }

  // ---- quiet hours, per server (8.7) ----

  Future<void> setQuietHours(ServerState s, QuietHours q) async {
    await s.api.setQuietHours(q);
    await s.db.putMe(await s.api.getMe());
  }

  /// Switches the schedule off; the old times are kept to offer again.
  Future<void> removeQuietHours(ServerState s) async {
    final q = s.me?.quietHours;
    if (q != null) prefs.keepOldQuietHours(s.entry.address, '${q.start}|${q.end}|${q.days}');
    await s.api.removeQuietHours();
    await s.db.putMe(await s.api.getMe());
  }

  Future<void> skipQuietHours(ServerState s, {bool skip = true}) async {
    skip ? await s.api.skipQuietHours() : await s.api.unskipQuietHours();
    await s.db.putMe(await s.api.getMe());
  }

  /// "Copy to…": one server's schedule on others.
  Future<Map<String, String>> copyQuietHours(
    QuietHours q,
    List<ServerState> to,
  ) async {
    final refused = <String, String>{};
    for (final s in to) {
      try {
        await setQuietHours(s, q);
      } on ApiException catch (e) {
        refused[s.name] = e.message;
      }
    }
    return refused;
  }
}
