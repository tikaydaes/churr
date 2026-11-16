import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:url_launcher/url_launcher.dart';

import 'platform/dev_seed.dart';
import 'platform/account_start.dart';
import 'platform/secret_storage.dart';
import 'platform/notify_start.dart';
import 'push_background.dart';
import 'platform/paths.dart';
import 'platform/sealed_file_cipher.dart';
import 'platform/sealed_store_file.dart';
import 'platform/window_secure.dart';
import 'state/app_lock.dart';
import 'state/app_state.dart';
import 'state/sealed_state.dart';
import 'ui/app.dart';

/// Starts the app: opens `app.db` and each server's database, draws
/// from them at once, and lets every server connect behind the first
/// frame (docs/CLIENTS.md section 7, "On start").
Future<void> main(List<String> args) async {
  WidgetsFlutterBinding.ensureInitialized();
  // Android: a UnifiedPush distributor started the app to hand it a
  // wake-up. No window (lib/push_background.dart).
  if (args.contains('--unifiedpush-bg')) {
    await startUnifiedPushInBackground();
    return;
  }
  // The platform's AES-256-GCM for sealed-chat files (pure Dart if none).
  await installSealedFileCipher();
  // Sealed chats: each signed-in server's encrypted store and device keys.
  SealedHub.instance.opener = (server) =>
      openSealedSession(serverLocalId: server.localId, api: server.api);
  // The lock: its page opens in the system browser (ruling R43), and the
  // window is kept out of screenshots while locked.
  AppLock.openUnlockPage = (url) async {
    await launchUrl(url, mode: LaunchMode.externalApplication);
  };
  AppLock.secureWindow = WindowSecure.set;
  final state = await openAppState();
  runApp(ChurrApp(state: state));
  state.lock.start();
  startAccountArea(state);
  // Notifications, the desktop tray and phone wake-ups (docs/PUSH.md).
  unawaited(startNotifications(state, args));
  if (kDebugMode) {
    try {
      await addServerFromDevSeed(state);
    } on Exception catch (e) {
      debugPrint('CHURR_DEV_SEED was not usable: $e');
    }
  }
}

/// The app state on real storage: the support directory and the
/// platform's key store. Integration tests call this too, so they run
/// the app as it ships.
Future<AppState> openAppState() async {
  final storage = await DiskStorage.open();
  final state = AppState(
    appDb: storage.openAppDb(),
    storage: storage,
    secrets: platformSecretStore(),
  );
  await state.load();
  return state;
}
