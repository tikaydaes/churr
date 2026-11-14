import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:path_provider/path_provider.dart';

import '../core/session_store.dart';

/// Keeps secrets in the platform key store: the Keychain, the Android
/// Keystore, the Windows credential store or libsecret.
///
/// Where no key store is usable (an unsigned macOS build, as during
/// development, or a Linux desktop without a secret service) it falls
/// back to a file only this user can read, so the app still stays signed
/// in. This is Codemail's approach.
class KeychainStore implements SecretStore {
  // On iPhone, items are readable after the first unlock, so the
  // notification extension can read the token and the notification keys
  // while the phone is locked (ios/NotificationService, and
  // app/docs/NOTIFY-SHARED.md). With no `groupId` they go to the first
  // keychain access group of the entitlements, which the app shares with
  // the extension (Runner.entitlements).
  final _storage = const FlutterSecureStorage(
    iOptions: IOSOptions(
      accessibility: KeychainAccessibility.first_unlock_this_device,
    ),
  );
  // Where items written before that setting are found.
  final _before = const FlutterSecureStorage();
  bool _useFile = false;

  Future<File> _file(String key) async {
    final dir = await getApplicationSupportDirectory();
    return File('${dir.path}/$key.secret');
  }

  @override
  Future<String?> read(String key) async {
    if (!_useFile) {
      try {
        final v = await _storage.read(key: key);
        if (v != null) return v;
        if (Platform.isIOS) {
          final old = await _before.read(key: key);
          if (old != null) {
            await _storage.write(key: key, value: old);
            return old;
          }
        }
      } on Object {
        _useFile = true;
      }
    }
    final f = await _file(key);
    return f.existsSync() ? f.readAsString() : null;
  }

  @override
  Future<void> write(String key, String value) async {
    if (!_useFile) {
      try {
        return await _storage.write(key: key, value: value);
      } on Object {
        _useFile = true;
      }
    }
    final f = await _file(key);
    await f.parent.create(recursive: true);
    await f.writeAsString(value, flush: true);
    if (!Platform.isWindows) await Process.run('chmod', ['600', f.path]);
  }

  @override
  Future<void> delete(String key) async {
    try {
      await _storage.delete(key: key);
    } on Object {
      // Nothing was stored there.
    }
    final f = await _file(key);
    if (f.existsSync()) await f.delete();
  }
}
