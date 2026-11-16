import 'dart:async';
import 'dart:io' show Platform;

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

import '../core/api/client.dart';
import '../core/api/errors.dart';
import '../core/api/lock.dart';
import '../core/api/me.dart';
import '../core/live.dart';
import 'app_state.dart';
import 'call_service.dart';
import 'sealed_state.dart';
import 'server_state.dart';

/// The lock (ruling R75, docs/CLIENTS.md section 9.27, docs/AUTH.md section
/// 10.6): the whole app locked on this device until a passkey unlocks it,
/// without signing out, which would throw away the device's sealed-chat
/// set-up.
///
/// Locking ends this device's part in a call, makes every server's
/// sealed-chat device a locked one, locks every server's session on the
/// server (each session locks on its own; the person's other devices are
/// untouched), stops the connections and lets go of what the servers held
/// in memory. The app draws only the lock screen while [locked], and stays
/// locked across a restart. Unlocking opens each server's unlock page in
/// the system browser (a passkey works only at the server's own web
/// address, ruling R43) and waits until the server says the session is
/// unlocked; then everything starts again and catches up.
class AppLock extends ChangeNotifier {
  AppLock(this.app);
  final AppState app;

  /// Opens a server's unlock page in the system browser. main.dart sets
  /// the platform's; tests give their own.
  static Future<void> Function(Uri url) openUnlockPage = (_) async {};

  /// Keeps the window out of screenshots, screen recording and the app
  /// switcher's picture where the platform allows (FLAG_SECURE on
  /// Android, sharing type none on macOS). Set by main.dart.
  static Future<void> Function(bool on) secureWindow = (_) async {};

  /// How long to wait between asking a server whether its session is
  /// unlocked yet, and for how long in all.
  static Duration pollEvery = const Duration(seconds: 2);
  static Duration pollFor = const Duration(minutes: 5);

  /// The choices for the automatic lock, in minutes; 0 is off.
  static const autoChoices = [0, 1, 5, 15, 60];

  static const _lockedKey = 'lock.locked';
  static const _autoKey = 'lock.auto_minutes';
  static const _backgroundKey = 'lock.in_background';

  /// Whether the app is locked now. Kept in `app.db`, so a restart stays
  /// locked.
  bool get locked => app.prefs.raw(_lockedKey) == '1';

  /// The automatic lock after a time without use, in minutes; 0 is off
  /// (the default). A setting of this device.
  int get autoMinutes {
    final n = int.tryParse(app.prefs.raw(_autoKey) ?? '') ?? 0;
    return autoChoices.contains(n) ? n : 0;
  }

  set autoMinutes(int n) {
    unawaited(app.prefs.putRaw(_autoKey, '$n'));
    notifyListeners();
  }

  /// Lock when the app goes to the background (phones; off by default).
  bool get lockInBackground => app.prefs.raw(_backgroundKey) == '1';

  set lockInBackground(bool on) {
    unawaited(app.prefs.putRaw(_backgroundKey, on ? '1' : '0'));
    unawaited(_secure());
    notifyListeners();
  }

  /// True while an unlock is waiting for the browser.
  bool unlocking = false;

  /// What went wrong with the last unlock, in words to show.
  String? problem;

  bool _locking = false;
  bool _cancel = false;
  DateTime _lastUse = DateTime.now();
  Timer? _timer;
  AppLifecycleListener? _life;

  Future<void> _secure() =>
      secureWindow(locked || (isPhone && lockInBackground));

  /// Whether this is a phone, where "lock in the background" is offered.
  static bool get isPhone => !kIsWeb && (Platform.isAndroid || Platform.isIOS);

  /// Starts the automatic lock and the background lock. main.dart calls
  /// it once the app state is loaded.
  void start() {
    _timer ??= Timer.periodic(const Duration(seconds: 10), (_) => checkIdle());
    _life ??= AppLifecycleListener(
      onHide: () {
        if (isPhone && lockInBackground) unawaited(lock());
      },
    );
    for (final s in app.servers) {
      s.addListener(() => _watchServer(s));
    }
    HardwareKeyboard.instance.addHandler(_key);
    unawaited(_secure());
  }

  /// A server's connection was closed because its session is locked (by
  /// this app, or before a restart that lost the mark): the whole app
  /// locks.
  void _watchServer(ServerState s) {
    if (!locked && s.liveState == LiveState.locked) unawaited(lock());
  }

  /// The person did something: a pointer or a key in the app's window.
  void noteUse() => _lastUse = DateTime.now();

  bool _key(KeyEvent e) {
    noteUse();
    return false;
  }

  /// Locks if the automatic lock is on and its time has passed without
  /// use. It fires during a call too, and ends it (the owner's decision).
  @visibleForTesting
  void checkIdle([DateTime? now]) {
    final n = autoMinutes;
    if (n == 0 || locked) return;
    if ((now ?? DateTime.now()).difference(_lastUse) >= Duration(minutes: n)) {
      unawaited(lock());
    }
  }

  /// Locks the app. Nothing is asked: the command and the menu ask about
  /// a call before they call this.
  Future<void> lock() async {
    if (locked || _locking) return;
    _locking = true;
    try {
      // The mark first, so the screen is replaced at once and a restart
      // from here on stays locked.
      await app.prefs.putRaw(_lockedKey, '1');
      problem = null;
      notifyListeners();
      unawaited(_secure());
      try {
        await CallService.of(app).leave();
      } on Object {
        // The server drops a locked session from the call by itself.
      }
      await SealedHub.instance.lockAll();
      for (final s in List.of(app.servers)) {
        try {
          await s.api.lockSession();
        } on Object {
          // Unreachable: the unlock locks it first, below.
        }
        await s.forgetForLock();
      }
      app.lockedOut();
    } finally {
      _locking = false;
    }
  }

  /// Stops waiting for the browser.
  void cancelUnlock() => _cancel = true;

  /// Unlocks: for each server, its unlock page in the browser, then a
  /// wait until the server says the session is unlocked. A server whose
  /// session ended while locked (signed out elsewhere, revoked) is left
  /// to show its sign-in, as any signed-out server does.
  Future<void> unlock() async {
    if (!locked || unlocking) return;
    unlocking = true;
    _cancel = false;
    problem = null;
    notifyListeners();
    try {
      for (final s in List.of(app.servers)) {
        if (!await _unlockOne(s.api)) return;
      }
      await app.prefs.putRaw(_lockedKey, '0');
      _lastUse = DateTime.now();
      await app.unlockedAgain();
      unawaited(_secure());
    } finally {
      unlocking = false;
      notifyListeners();
    }
  }

  /// One server. False to stop (cancelled, unreachable, too slow).
  Future<bool> _unlockOne(ApiClient api) async {
    Uri url;
    try {
      // A session the lock did not reach (no network then) is locked now.
      await api.lockSession();
      url = await api.beginSessionUnlock();
    } on SignedOutException {
      return true;
    } on ApiException catch (e) {
      if (e.code == 'unauthenticated') return true;
      if (e.code == 'conflict') return true;
      problem = e.code == 'network'
          ? 'The server could not be reached. Try again.'
          : e.message;
      return false;
    }
    await openUnlockPage(url);
    final end = DateTime.now().add(pollFor);
    while (DateTime.now().isBefore(end)) {
      if (_cancel) return false;
      await Future<void>.delayed(pollEvery);
      if (_cancel) return false;
      try {
        await api.getMe();
        return true;
      } on SignedOutException {
        return true;
      } on ApiException catch (e) {
        if (e.code == 'unauthenticated') return true;
        // session_locked: not yet. Anything else: ask again.
      }
    }
    problem = 'That took too long. Press Unlock to try again.';
    return false;
  }

  @override
  void dispose() {
    _timer?.cancel();
    _life?.dispose();
    HardwareKeyboard.instance.removeHandler(_key);
    super.dispose();
  }
}
