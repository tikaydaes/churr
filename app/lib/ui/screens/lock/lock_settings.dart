import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import '../../../l10n/app_localizations.dart';
import '../../../state/app_lock.dart';
import '../../scopes.dart';
import '../../widgets/controls.dart';

/// The lock's settings of this device (docs/CLIENTS.md section 9.27): the
/// automatic lock after a time without use (off by default) and, on
/// phones, locking when the app goes to the background (off by default).
class LockSettingsSection extends StatelessWidget {
  const LockSettingsSection({super.key});

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final lock = AppScope.read(context).lock;
    final mac =
        defaultTargetPlatform == TargetPlatform.macOS ||
        defaultTargetPlatform == TargetPlatform.iOS;
    return ListenableBuilder(
      listenable: lock,
      builder: (context, _) => Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(t.appLockSettingsNote(mac ? '⌘⇧L' : 'Ctrl+Shift+L')),
          const SizedBox(height: 8),
          Text(t.appLockAuto, style: Theme.of(context).textTheme.titleSmall),
          for (final n in AppLock.autoChoices)
            ChoiceTile(
              key: Key('autolock-$n'),
              contentPadding: EdgeInsets.zero,
              selected: lock.autoMinutes == n,
              onTap: () => lock.autoMinutes = n,
              title: Text(n == 0 ? t.appLockAutoOff : t.appLockAutoAfter(n)),
            ),
          Text(t.appLockAutoNote),
          if (AppLock.isPhone)
            ChurrSwitchTile(
              key: const Key('lock-in-background'),
              contentPadding: EdgeInsets.zero,
              title: Text(t.appLockInBackground),
              value: lock.lockInBackground,
              onChanged: (v) => lock.lockInBackground = v,
            ),
        ],
      ),
    );
  }
}
