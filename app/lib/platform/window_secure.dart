import 'package:flutter/services.dart';

/// Keeps the window out of screenshots, screen recording and the picture
/// the app switcher keeps, while the app is locked (the lock,
/// docs/CLIENTS.md section 9.27): FLAG_SECURE on Android, a sharing type of
/// none on macOS. iOS has no such switch for an app's window, and Windows
/// and Linux are not asked; there it does nothing.
abstract final class WindowSecure {
  static const _channel = MethodChannel('churr/window');

  static Future<void> set(bool on) async {
    try {
      await _channel.invokeMethod<void>('secure', on);
    } on MissingPluginException {
      // A platform without it.
    } on PlatformException {
      // Not fatal: the lock screen still hides everything.
    }
  }
}
