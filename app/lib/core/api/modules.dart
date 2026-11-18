// The built-in modules (docs/DESIGN-MODULES.md section 6.5) as they stand
// in a conversation: the server's switch for each, and the channel's own,
// which its managers set.

import '../models/json.dart';
import 'client.dart';
import 'operations.dart';

extension ModulesApi on ApiClient {
  /// The modules, each with `name`, `title`, `on` (here), `server_on`,
  /// `commands` and `claims`.
  Future<List<Json>> listConversationModules(String conversationId) async {
    final out = await call(
      Ops.listConversationModules,
      path: {'id': conversationId},
    );
    return [
      for (final m in (out['modules'] as List?) ?? const [])
        if (m is Map) m.cast<String, dynamic>(),
    ];
  }

  /// Switches a module off in a channel, or on again. The server refuses
  /// to switch on what it has off itself.
  Future<Json> setConversationModule(
    String conversationId,
    String name, {
    required bool on,
  }) async =>
      ((await call(
                Ops.setConversationModule,
                path: {'id': conversationId, 'name': name},
                body: {'on': on},
              ))['module']
              as Map)
          .cast<String, dynamic>();
}
