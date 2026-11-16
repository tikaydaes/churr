// The desktop apps' tray, window and start at login (docs/PUSH.md section
// 13, docs/CLIENTS.md 9.23).
//
// Packages, checked on pub.dev 2026-11-14:
// - `tray_manager` 0.8.3 (2026-11-05): the tray icon and its menu on macOS,
//   Windows and Linux, the package CLIENTS.md names. Since 0.7 it is built
//   on `nativeapi`, one C++ core for the three desktops; 0.9 needs Flutter
//   3.50 and this tree is on 3.47.
// - `window_manager` 0.5.2: hiding the window to the tray, showing it
//   again, and focus. 0.6 and 0.7 exist, but `unifiedpush` (through
//   `unifiedpush_linux`) holds it to 0.5.
// - Start at login through `nativeapi`'s `LaunchAtLogin` (0.5.10,
//   2026-11), already in the build under `tray_manager`. `launch_at_startup`
//   (named in CLIENTS.md, marked † there) has had no release since
//   2025-04 and cannot be resolved beside `flutter_secure_storage` 11
//   (its `win32_registry` 2 wants `win32` 5; the key store wants 6).

import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:nativeapi/nativeapi.dart' as native;
import 'package:tray_manager/tray_manager.dart' as tray;
import 'package:window_manager/window_manager.dart';

import '../core/notify/tray_state.dart';
import '../l10n/app_localizations.dart';
import '../state/account_hub.dart';
import '../state/app_state.dart';
import '../state/notify_center.dart';
import '../state/notify_prefs.dart';
import 'notify_wording.dart';

/// Whether this is one of the three desktop targets.
bool get isDesktop =>
    Platform.isMacOS || Platform.isWindows || Platform.isLinux;

/// The argument the login item starts the app with: the window stays
/// hidden.
const hiddenArgument = '--hidden';

/// The tray icon, its menu, the window's close button and start at login.
class DesktopShell with WindowListener {
  DesktopShell._(this.app, this.center);

  final AppState app;
  final NotifyCenter center;
  tray.TrayIcon? _icon;
  String? _shownIcon;
  String? _shownMenu;
  final _keep = <Object>[];

  /// Starts it. [startHidden] when the login item started the app.
  static Future<DesktopShell?> start(
    AppState app,
    NotifyCenter center, {
    bool startHidden = false,
  }) async {
    if (!isDesktop) return null;
    final d = DesktopShell._(app, center);
    try {
      await windowManager.ensureInitialized();
      windowManager.addListener(d);
      await windowManager.setPreventClose(app.prefs.closeToTray);
      if (startHidden) await windowManager.hide();
    } on Object catch (e) {
      debugPrint('Window manager not available: $e');
    }
    d._tray();
    center.addListener(d._tray);
    app.lock.addListener(d._tray);
    app.prefs.addListener(d._prefs);
    applyStartAtLogin(app.prefs.startAtLogin);
    return d;
  }

  bool _lastClose = true;

  void _prefs() {
    final close = app.prefs.closeToTray;
    if (close != _lastClose) {
      _lastClose = close;
      unawaited(windowManager.setPreventClose(close));
    }
    _tray();
  }

  // ---- the tray ----

  void _tray() {
    try {
      var icon = _icon;
      if (icon == null) {
        icon = _icon = tray.TrayIcon.create();
        if (icon == null) return;
        // A left click opens the window where the menu is on the right
        // button (Windows; Linux panels keep clicks to themselves).
        if (!Platform.isMacOS) {
          icon.addListener((e) {
            if (e is tray.TrayIconClickedEvent) showWindow();
          });
        }
      }
      // Locked, the tray shows nothing of the account: the plain icon, and
      // a menu of Open and Quit (the lock).
      final locked = app.lock.locked;
      final state = locked
          ? const TrayState(TrayIcon.normal, 0, [])
          : center.tray;
      final t = notifyL10n();
      final template = Platform.isMacOS;
      final name =
          'assets/tray/${template ? 'template' : 'color'}/${trayIconName(state)}.png';
      if (name != _shownIcon) {
        final img = tray.ImageAsset.fromAsset(name);
        if (img != null) {
          icon.icon = img;
          icon.isIconTemplate = template;
          if (template) {
            icon.iconSize = const native.Size(width: 18, height: 18);
          }
          _shownIcon = name;
        }
      }
      icon.setTooltip(switch (state.icon) {
        TrayIcon.disconnected => t.trayTooltipDisconnected,
        TrayIcon.dnd => t.trayTooltipDnd,
        _ when state.count > 0 => t.trayTooltipUnread(state.count),
        _ => t.trayTooltip,
      });
      _menu(icon, state, t);
      icon.setVisible(true);
    } on Object catch (e) {
      debugPrint('Tray not available: $e');
    }
  }

  void _menu(tray.TrayIcon icon, TrayState state, L10n t) {
    // Rebuilt only when what it says changes.
    final sig = [
      for (final s in state.servers)
        '${s.localId}:${s.name}:${s.badge}:${s.connected}:${s.muted}',
      state.icon.name,
      app.lock.locked,
    ].join('|');
    if (sig == _shownMenu) return;
    _shownMenu = sig;
    _keep.clear();
    final menu = tray.Menu.create()!;
    _keep.add(menu);

    tray.MenuItem item(String label, void Function()? onClick) {
      final i = tray.MenuItem.createWithLabelAndType(
        label,
        tray.MenuItemType.normal,
      )!;
      if (onClick == null) {
        i.isEnabled = false;
      } else {
        i.addListener((e) {
          if (e is tray.MenuItemClickedEvent) onClick();
        });
      }
      _keep.add(i);
      return i;
    }

    tray.MenuItem sub(String label, List<tray.MenuItem?> items) {
      final m = tray.Menu.create()!;
      for (final x in items) {
        x == null ? m.addSeparator() : m.addItem(x);
      }
      final i = tray.MenuItem.createWithLabelAndType(
        label,
        tray.MenuItemType.submenu,
      )!;
      i.submenu = m;
      _keep
        ..add(m)
        ..add(i);
      return i;
    }

    menu.addItem(item(t.trayOpen, showWindow));
    menu.addSeparator();
    if (app.lock.locked) {
      menu.addItem(item(t.trayQuit, quit));
      icon.setContextMenu(menu);
      icon.setContextMenuTrigger(
        Platform.isMacOS
            ? tray.ContextMenuTrigger.clicked
            : tray.ContextMenuTrigger.rightClicked,
      );
      return;
    }
    for (final s in state.servers) {
      menu.addItem(
        item(
          s.connected
              ? t.trayServerCount(s.name, s.badge)
              : t.trayServerOffline(s.name),
          () {
            app.selectServer(s.localId);
            showWindow();
          },
        ),
      );
    }
    if (state.servers.isNotEmpty) menu.addSeparator();
    final hub = AccountHub.of(app);
    DateTime now() => DateTime.now();
    menu.addItem(
      sub(t.trayDnd, [
        item(
          t.trayDndFor30,
          () => hub.setDnd(now().add(const Duration(minutes: 30))),
        ),
        item(
          t.trayDndForHour,
          () => hub.setDnd(now().add(const Duration(hours: 1))),
        ),
        item(t.trayDndTomorrow, () {
          final n = now();
          hub.setDnd(DateTime(n.year, n.month, n.day + 1, 8));
        }),
        item(t.trayDndUntilOff, () => hub.setDnd(null)),
        null,
        item(t.trayDndTurnOff, hub.clearDnd),
      ]),
    );
    menu.addItem(
      sub(t.trayMute, [
        for (final s in app.servers)
          sub(s.name, [
            for (final (label, d) in [
              (t.trayMuteFor1h, const Duration(hours: 1)),
              (t.trayMuteFor8h, const Duration(hours: 8)),
              (t.trayMuteForDay, const Duration(days: 1)),
              (t.trayMuteForWeek, const Duration(days: 7)),
            ])
              item(label, () => hub.muteServer(s, now().add(d))),
            null,
            item(t.trayUnmute, () => hub.unmuteServer(s)),
          ]),
      ]),
    );
    menu.addSeparator();
    menu.addItem(item(t.trayQuit, quit));
    icon.setContextMenu(menu);
    icon.setContextMenuTrigger(
      Platform.isMacOS
          ? tray.ContextMenuTrigger.clicked
          : tray.ContextMenuTrigger.rightClicked,
    );
  }

  // ---- the window ----

  Future<void> showWindow() async {
    try {
      await windowManager.show();
      await windowManager.focus();
    } on Object {
      // No window manager: nothing to show.
    }
  }

  /// Quits for real: the tray's and the application menu's Quit.
  Future<void> quit() async {
    try {
      await windowManager.setPreventClose(false);
      await windowManager.destroy();
    } on Object {
      exit(0);
    }
  }

  @override
  void onWindowClose() {
    // With close-to-tray on, closing hides; the connection stays open.
    if (app.prefs.closeToTray) {
      unawaited(windowManager.hide());
      center.windowInFront = false;
    } else {
      unawaited(quit());
    }
  }

  @override
  void onWindowFocus() => center.windowInFront = true;

  @override
  void onWindowBlur() => center.windowInFront = false;

  @override
  void onWindowMinimize() => center.windowInFront = false;
}

/// Turns the login item on or off. The item starts the app with
/// [hiddenArgument] where the platform passes arguments (Windows, Linux);
/// on macOS the window opens hidden when the app was opened as a login
/// item (`MainFlutterWindow.swift`).
bool applyStartAtLogin(bool on) {
  try {
    if (!native.LaunchAtLogin.isSupported()) return false;
    final l = native.LaunchAtLogin.createWithIdAndDisplayName(
      'example.notfinal.churr',
      'Churr',
    );
    if (l == null) return false;
    if (!Platform.isMacOS) {
      l.setProgram(Platform.resolvedExecutable, [hiddenArgument]);
    }
    final ok = on ? l.enable() : (l.isEnabled ? l.disable() : true);
    l.dispose();
    return ok;
  } on Object catch (e) {
    debugPrint('Start at login not available: $e');
    return false;
  }
}
