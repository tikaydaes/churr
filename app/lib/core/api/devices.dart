import '../models/json.dart';
import '../notify/wake.dart';
import 'client.dart';
import 'operations.dart';

/// This device's registration for wake-ups, and the wake-up fetch
/// (docs/PUSH.md sections 3 and 9).
extension DevicesApi on ApiClient {
  /// Registers (or updates) the device of this session. Fields left null
  /// are left as they are on the server, so the sealed-chat code can set
  /// its keys through the same operation without touching the push
  /// fields, and the other way round. Returns the `Device` object, which
  /// carries `id` and `wake_route`.
  Future<Json> registerDevice({
    String? name,
    String? platform,
    String? pushKind,
    String? pushAddress,
    String? pushPublicKey,
    String? pushAuth,
  }) async {
    final res = await call(
      Ops.registerDevice,
      body: {
        'name': ?name,
        'platform': ?platform,
        'push_kind': ?pushKind,
        'push_address': ?pushAddress,
        'push_public_key': ?pushPublicKey,
        'push_auth': ?pushAuth,
      },
    );
    return object(res['device']) ?? const {};
  }

  /// What a wake-up points at. `not_found` when the server cannot read
  /// the identifier, it is older than seven days, it names another
  /// session's device, or the conversation can no longer be read.
  Future<WakeUp> fetchWakeUp(String eventId) async {
    final res = await call(Ops.fetchWakeUp, path: {'event_id': eventId});
    return WakeUp.fromJson(object(res['wake_up']) ?? const {});
  }
}
