import '../models/json.dart';
import 'client.dart';
import 'operations.dart';

/// A watch: "tell me when this person is around" (ruling R76,
/// docs/FEATURES.md section 9.7). It is a scheduled item of kind `watch`,
/// waiting until [expiresAt] for [accountId] to be around.
class Watch {
  const Watch({required this.id, required this.accountId, this.expiresAt});
  final String id;
  final String accountId;

  /// When it expires unfired; null once it has fired.
  final DateTime? expiresAt;

  bool get waiting => expiresAt != null;

  static Watch fromJson(Json j) => Watch(
    id: j['id'] as String? ?? '',
    accountId: j['watch_account_id'] as String? ?? '',
    expiresAt: DateTime.tryParse(j['next_run_at'] as String? ?? ''),
  );
}

extension WatchesApi on ApiClient {
  /// Asks to be told once when [accountId] is next around. True when they
  /// are around now, and nothing was stored.
  Future<bool> watchAccount(String accountId) async {
    final j = await call(Ops.watchAccount, path: {'account_id': accountId});
    return j['around'] == true;
  }

  /// Stops watching for [accountId].
  Future<void> unwatchAccount(String accountId) =>
      call(Ops.unwatchAccount, path: {'account_id': accountId});

  /// The person's waiting watches.
  Future<List<Watch>> listWatches() async {
    final j = await call(
      Ops.listScheduled,
      query: {'kind': 'watch', 'limit': '200'},
    );
    return [
      for (final w in listOf(j['scheduled'], Watch.fromJson))
        if (w.waiting) w,
    ];
  }

  /// Cancels one scheduled item, a watch among them.
  Future<void> cancelScheduled(String id) =>
      call(Ops.cancelScheduled, path: {'id': id});
}
