import 'dart:async';
import 'dart:io';

import '../state/account_hub.dart';
import '../state/account_prefs.dart';
import '../state/app_state.dart';
import 'device.dart';
import 'file_cache.dart';
import 'sounds.dart';

/// What the account area does once at launch, behind the first frame:
/// the start-up sound (an app setting, on out of the box), the
/// cross-server hub with the device's facts, and the file cache trimmed
/// to its cap.
void startAccountArea(AppState state) {
  AccountHub.phone = Platform.isAndroid || Platform.isIOS;
  AccountHub.deviceZoneSource = deviceTimeZone;
  AccountHub.of(state);
  if (state.prefs.startupSound) unawaited(Sounds.playStartup());
  unawaited(() async {
    final dirs = [for (final s in state.servers) ?s.filesDirectory];
    await trimFileCache(dirs, state.prefs.fileCacheMb * 1024 * 1024);
  }());
}
