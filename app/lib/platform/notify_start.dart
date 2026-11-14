import 'dart:async';
import 'dart:io';

import 'package:flutter/gestures.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

import '../core/notify/registration.dart';
import '../state/app_state.dart';
import '../state/notify_center.dart';
import '../state/notify_prefs.dart';
import 'background_service_control.dart';
import 'desktop.dart';
import 'notifications.dart';
import 'notify_calls.dart';
import 'push.dart';

/// Starts the notifications area behind the first frame: the platform's
/// notifications, the notify center, the desktop tray or the phone's
/// wake-ups, and activity reporting (docs/PUSH.md sections 2, 12, 13).
/// [args] are `main`'s: `--hidden` when the login item started the app.
Future<void> startNotifications(AppState app, List<String> args) async {
  late final NotifyCenter center;
  final notifier = await PlatformNotifier.start(
    onTap: (p) {
      center.openTapped(p);
      if (isDesktop) unawaited(_desktop?.showWindow());
    },
  );
  center = NotifyCenter(app, notifier: notifier ?? SilentNotifier())
    ..deferWhenAway = () => _stays(app);
  NotifyCenter.install(center);
  _Watcher(app, center).attach();
  if (isDesktop) {
    _desktop = await DesktopShell.start(
      app,
      center,
      startHidden: args.contains(hiddenArgument),
    );
  }
  joinCallsToNotifications(app, center);
  final push = await PushService.start(app, center);
  app.beforeRemove.add((s) async => push?.removeFrom(s));
}

DesktopShell? _desktop;

bool _stays(AppState app) =>
    Platform.isAndroid && app.prefs.androidPush == AndroidPush.stayConnected;

/// Whether the app is in front, and the person's activity in its window.
///
/// Activity: keyboard and pointer use in the app's own window, and
/// nothing wider (docs/PUSH.md section 13), at most once a minute per
/// server (`LiveConnection.noteActivity`). It is what keeps the server
/// from waking the person's phone while they are here.
class _Watcher with WidgetsBindingObserver {
  _Watcher(this.app, this.center);
  final AppState app;
  final NotifyCenter center;

  void attach() {
    WidgetsBinding.instance.addObserver(this);
    GestureBinding.instance.pointerRouter.addGlobalRoute(_pointer);
    HardwareKeyboard.instance.addHandler(_key);
    app.addListener(_tellService);
  }

  void _activity() {
    for (final s in app.servers) {
      s.live.noteActivity();
    }
  }

  void _pointer(PointerEvent e) {
    if (e is PointerDownEvent ||
        e is PointerScrollEvent ||
        e is PointerHoverEvent) {
      _activity();
    }
  }

  bool _key(KeyEvent e) {
    _activity();
    return false;
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    final front = state == AppLifecycleState.resumed;
    // On desktop, focus comes from the window manager (DesktopShell).
    if (!isDesktop || !front) center.windowInFront = front;
    if (front) {
      PushService.current?.resumed();
      _activity();
    }
    _tellService();
  }

  void _tellService() {
    if (!_stays(app)) return;
    final o = app.open;
    tellService(
      front: center.windowInFront,
      open: o == null ? null : '${o.serverId}/${o.conversationId}',
    );
  }
}
