import 'package:flutter/material.dart';

import '../../../l10n/app_localizations.dart';
import '../../../state/call_service.dart';
import '../../scopes.dart';
import '../conversation/message_menu.dart' show confirm;

/// Locks the app from the account menu, /lock or Mod+Shift+L (the lock,
/// docs/CLIENTS.md section 9.27). Locking ends this device's part in a
/// call, so the person is asked first when there is one.
Future<void> lockAsking(BuildContext context) async {
  final app = AppScope.read(context);
  if (CallService.of(app).active != null) {
    final t = L10n.of(context);
    final ok = await confirm(
      context,
      t.appLockCallTitle,
      t.appLockCallBody,
      yes: t.appLockCallConfirm,
    );
    if (!ok) return;
  }
  await app.lock.lock();
}
