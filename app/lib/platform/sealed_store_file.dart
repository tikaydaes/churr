import 'dart:io';

import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

import '../core/api/client.dart';
import '../core/sealed_client/notify_keys.dart';
import '../core/sealed_client/session.dart';
import '../core/sealed_client/sqlite_store.dart';
import '../core/session_store.dart';
import 'browser_sign_in.dart' as browser;
import 'secret_storage.dart';

/// Where each sealed secret lives on a real device:
///
/// - `servers/<local id>/sealed.db` in the support directory: the sealed
///   store, a SQLite file encrypted at rest (SQLite3MultipleCiphers),
///   holding the backup key, account key, pins, verified marks, epochs and
///   history secrets, counters and the decrypted messages;
/// - the app's secret store (lib/platform/secret_storage.dart: the
///   Keychain, Keystore, credential store or libsecret; a plain file in
///   desktop debug builds only): `churr.sealed.<local id>.dbkey`, the
///   store's 32-byte key, and `churr.sealed.<local id>.device`, the
///   device's Ed25519 seed and X25519 private key;
/// - on iPhone only, the keychain group the app shares with the
///   notification extension (`$(AppIdentifierPrefix)example.notfinal.churr.shared`,
///   ios/Runner/Runner.entitlements): one item per readable key epoch with
///   its content key and the senders' public keys, as
///   app/docs/NOTIFY-SHARED.md section 1 sets out.
///
/// Signing out, or this device being removed, deletes the file, both
/// secrets and the extension's items.
Future<SealedSession> openSealedSession({
  required String serverLocalId,
  required ApiClient api,
  SecretStore? secrets,
}) async {
  final s = secrets ?? platformSecretStore();
  final dir = await getApplicationSupportDirectory();
  final folder = Directory(p.join(dir.path, 'servers', serverLocalId))
    ..createSync(recursive: true);
  final store = await SqliteSealedStore.open(
    path: p.join(folder.path, 'sealed.db'),
    secrets: s,
    scope: serverLocalId,
  );
  return SealedSession(
    api: api,
    store: store,
    platformName: Platform.operatingSystem,
    deviceName: browser.deviceName,
    nse: Platform.isIOS
        ? SharedNotificationKeys(s, serverLocalId)
        : const NoNotificationKeys(),
  );
}
