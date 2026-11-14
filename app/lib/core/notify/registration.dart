// Registering this device for wake-ups on each signed-in server
// (docs/PUSH.md section 3): straight after sign-in, on every start, and
// whenever the operating system hands the app a new push address.

import '../api/client.dart';
import '../api/devices.dart';
import '../api/errors.dart';

/// How this device is woken (`push_kind`).
enum PushKind { apns, fcm, unifiedpush, none }

/// How an Android phone chooses to be told (Settings → Notifications,
/// docs/PUSH.md section 10). Ignored on other platforms.
enum AndroidPush {
  /// Firebase where the build has it, else UnifiedPush where a
  /// distributor is installed, else the status endpoint only.
  automatic,
  unifiedPush,

  /// The foreground service keeps the connection open (`none`).
  stayConnected,

  /// Nothing in the background: the status endpoint when opened.
  off,
}

/// A push address the platform gave, with its kind.
class PushAddress {
  const PushAddress(this.kind, this.address, {this.publicKey, this.auth});
  final PushKind kind;
  final String address;

  /// Web Push keys for a UnifiedPush distributor that wants them
  /// (base64url).
  final String? publicKey;
  final String? auth;
}

/// Picks what to register on one server, from the addresses the device
/// has (best first) and the kinds the server accepts (`push_kinds` of
/// `getServer`). With nothing that fits, `none`.
PushAddress choosePush(List<PushAddress> available, List<String> accepted) {
  for (final a in available) {
    if (a.kind == PushKind.none) break;
    if (accepted.contains(a.kind.name)) return a;
  }
  return const PushAddress(PushKind.none, '');
}

/// The outcome of registering on one server.
class Registration {
  const Registration({
    required this.kind,
    this.deviceId,
    this.wakeRoute,
    this.refused = false,
  });
  final PushKind kind;
  final String? deviceId;
  final String? wakeRoute;

  /// The server refused the push kind (`feature_off`) and the device was
  /// registered as `none` instead: Android offers the foreground service.
  final bool refused;
}

/// Registers the device of [api]'s session. A kind the server refuses
/// with `feature_off` falls back to `none` (the status endpoint).
Future<Registration> registerThisDevice(
  ApiClient api, {
  required String name,
  required String platform,
  required PushAddress push,
}) async {
  Future<Registration> send(PushAddress p, {bool refused = false}) async {
    final d = await api.registerDevice(
      name: name,
      platform: platform,
      pushKind: p.kind.name,
      // `none` clears the address: the server keeps what it had otherwise.
      pushAddress: p.kind == PushKind.none ? null : p.address,
      pushPublicKey: p.kind == PushKind.unifiedpush ? p.publicKey : null,
      pushAuth: p.kind == PushKind.unifiedpush ? p.auth : null,
    );
    return Registration(
      kind: p.kind,
      deviceId: d['id'] as String?,
      wakeRoute: d['wake_route'] as String?,
      refused: refused,
    );
  }

  try {
    return await send(push);
  } on ApiException catch (e) {
    if (e.code != 'feature_off' || push.kind == PushKind.none) rethrow;
    return send(const PushAddress(PushKind.none, ''), refused: true);
  }
}

/// A device name the server accepts (1 to 60 characters).
String deviceName(String raw) {
  final t = raw.trim();
  if (t.isEmpty) return 'Churr';
  return t.runes.length <= 60 ? t : String.fromCharCodes(t.runes.take(60));
}
