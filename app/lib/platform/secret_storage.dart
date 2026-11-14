import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

import '../core/session_store.dart';
import 'keychain_store.dart';

/// Every secret the app keeps (sign-in tokens, the sealed store's key, a
/// device's private keys) goes through one [SecretStore], chosen here:
///
/// - **Release and profile builds, and every phone build:** the
///   platform's secure storage ([KeychainStore]).
/// - **Debug builds on macOS, Windows and Linux:** [DevFileSecretStore],
///   a plain file in the app's own support directory. A rebuilt desktop
///   app is signed afresh each time, so the keychain asks the person
///   for permission on every launch, which blocks unattended runs.
///
/// The file store is for development only and keeps secrets
/// **unprotected**. It cannot be chosen in a release build: the choice
/// is guarded by `kDebugMode`, a constant, so the compiler drops it, and
/// the store refuses to be made outside a debug build. App settings say
/// so while it is in use. Tokens already in the keychain are not moved:
/// in a debug build the person signs in again.
///
/// The iPhone notification extension's keychain group is separate and
/// phone-only (lib/platform/sealed_store_file.dart).
bool usesFileSecrets({required bool debug, required String os}) =>
    debug && (os == 'macos' || os == 'windows' || os == 'linux');

/// Whether this run keeps its secrets in the development file.
bool get secretsInFile =>
    kDebugMode &&
    usesFileSecrets(debug: kDebugMode, os: Platform.operatingSystem);

SecretStore? _shared;

/// The app's one secret store (made once per isolate).
SecretStore platformSecretStore() {
  if (kDebugMode && secretsInFile) {
    return _shared ??= DevFileSecretStore.inSupportDirectory();
  }
  return _shared ??= KeychainStore();
}

/// Development only: the same keys and values as the keychain would
/// hold, in one JSON file only this user can read. Never in a release
/// build (see [usesFileSecrets]).
class DevFileSecretStore implements SecretStore {
  DevFileSecretStore(this._file) {
    if (!kDebugMode) {
      throw StateError('the file secret store exists only in debug builds');
    }
  }

  /// `dev-secrets.json` in the app's support directory.
  DevFileSecretStore.inSupportDirectory() : this(null);

  File? _file;

  Future<File> _target() async {
    final f = _file;
    if (f != null) return f;
    final dir = await getApplicationSupportDirectory();
    return _file = File(p.join(dir.path, 'dev-secrets.json'));
  }

  Future<Map<String, String>> _read() async {
    final f = await _target();
    if (!f.existsSync()) return {};
    try {
      return (jsonDecode(await f.readAsString()) as Map).cast<String, String>();
    } on Object {
      return {};
    }
  }

  Future<void> _write(Map<String, String> all) async {
    final f = await _target();
    await f.parent.create(recursive: true);
    final tmp = File('${f.path}.tmp');
    await tmp.writeAsString(jsonEncode(all), flush: true);
    if (!Platform.isWindows) await Process.run('chmod', ['600', tmp.path]);
    await tmp.rename(f.path);
  }

  // One change at a time, so two writers never lose each other's entry.
  Future<void> _tail = Future.value();
  Future<T> _serial<T>(Future<T> Function() f) {
    final next = _tail.then((_) => f());
    _tail = next.then((_) {}, onError: (_) {});
    return next;
  }

  @override
  Future<String?> read(String key) => _serial(() async => (await _read())[key]);

  @override
  Future<void> write(String key, String value) => _serial(() async {
    final all = await _read();
    all[key] = value;
    await _write(all);
  });

  @override
  Future<void> delete(String key) => _serial(() async {
    final all = await _read();
    if (all.remove(key) != null) await _write(all);
  });
}
