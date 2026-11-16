import 'client.dart';
import 'operations.dart';

/// The lock (docs/AUTH.md section 10.6): this app's session locked on the
/// server until a passkey or the account's access key unlocks it.
extension LockApi on ApiClient {
  /// Locks this app's session. Locking a locked session changes nothing.
  Future<void> lockSession() async {
    await call(Ops.lockSession, body: const <String, Object?>{});
  }

  /// Starts unlocking: the address of the server's page that runs the
  /// passkey assertion or takes the access key, which the app opens in the
  /// system browser, because a passkey works only at the server's own web
  /// address (ruling R43). The access key is typed on that page too, so
  /// the app never handles it.
  Future<Uri> beginSessionUnlock() async {
    final j = await call(
      Ops.beginSessionUnlock,
      body: const <String, Object?>{},
    );
    return Uri.parse(j['unlock_url'] as String? ?? '');
  }
}
