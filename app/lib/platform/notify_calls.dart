import 'dart:async';

import '../state/app_state.dart';
import '../state/call_service.dart';
import '../state/notify_center.dart';

/// Incoming calls while the app is not in front (docs/PUSH.md section
/// 13, "Incoming calls"), joined to the calls code's interface
/// (`CallService.showIncomingCall` and `CallService.incoming`; see
/// app/docs/NOTIFY-SHARED.md):
///
/// - a tap on a ring notification, or a phone woken for a ring that is
///   still ringing, opens the calls code's incoming-call prompt;
/// - a ring the calls code shows while the window is not in front also
///   gets a system notification on the calls channel with the ring
///   sound, replaced by "Missed call from …" or removed when it stops.
void joinCallsToNotifications(AppState app, NotifyCenter center) {
  final calls = CallService.of(app);
  IncomingCallHook.openPrompt = (serverId, conversationId, callId) =>
      calls.showIncomingCall(
        serverId: serverId,
        conversationId: conversationId,
        callId: callId,
      );
  IncomingCall? shown;
  var answered = false;
  calls.addListener(() {
    final now = calls.incoming;
    if (now != null && now.callId != shown?.callId) {
      shown = now;
      answered = false;
      if (!center.windowInFront && !now.silent) {
        unawaited(
          center.showRing(
            serverLocalId: now.server.localId,
            conversationId: now.conversationId,
            callId: now.callId,
            callerName: _name(now),
          ),
        );
      }
    } else if (now == null && shown != null) {
      final was = shown!;
      shown = null;
      // Joined here, or the call is the one this device is now in.
      answered = calls.active?.conversationId == was.conversationId;
      unawaited(
        center.stopRing(
          serverLocalId: was.server.localId,
          conversationId: was.conversationId,
          callerName: _name(was),
          answered: answered,
        ),
      );
    }
  });
}

String _name(IncomingCall i) {
  final id = i.fromAccountId;
  final n = id == null ? null : i.server.accounts[id]?.displayName;
  return n == null || n.isEmpty ? '…' : n;
}
