// Firebase Cloud Messaging, for the Play Store build only.
//
// The repository as checked in has no Google library (docs/CLIENTS.md
// sections 3 and 14): this file is the stand-in that says "no FCM here".
// `dart run tool/play_flavor.dart` replaces it with tool/play/fcm.dart,
// which uses `firebase_messaging`, and adds that package to pubspec.yaml.
// Keep the two files' public names the same.

/// Whether this build can be woken through FCM.
const bool fcmAvailable = false;

/// Starts FCM and returns the registration token, or null. [onToken] is
/// told each new token; [onWake] each data message received while the
/// app runs (`e`, `c`, `b` of docs/PUSH.md section 7).
Future<String?> startFcm({
  required void Function(String token) onToken,
  required void Function(Map<String, String> data) onWake,
}) async => null;
