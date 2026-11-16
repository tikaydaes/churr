// Reference patterns (ruling R73): text such as PROJ-123 that becomes a
// link. The server's, which admins define, and a channel's, which its
// managers define.

import '../models/json.dart';
import 'client.dart';
import 'operations.dart';

extension ReferencePatternsApi on ApiClient {
  /// The server's patterns and, with [conversationId], that channel's.
  Future<List<Json>> listReferencePatterns({String? conversationId}) async {
    final out = await call(
      Ops.listReferencePatterns,
      query: {'conversation_id': ?conversationId},
    );
    return [
      for (final p in (out['patterns'] as List?) ?? const [])
        if (p is Map) p.cast<String, dynamic>(),
    ];
  }

  /// Defines a pattern: the server's with no [conversationId].
  Future<Json> createReferencePattern(
    Json fields, {
    String? conversationId,
  }) async =>
      ((await call(
                Ops.createReferencePattern,
                body: {...fields, 'conversation_id': ?conversationId},
              ))['pattern']
              as Map)
          .cast<String, dynamic>();

  /// Changes the fields given.
  Future<Json> updateReferencePattern(String id, Json fields) async =>
      ((await call(
                Ops.updateReferencePattern,
                path: {'id': id},
                body: fields,
              ))['pattern']
              as Map)
          .cast<String, dynamic>();

  Future<void> deleteReferencePattern(String id) async {
    await call(Ops.deleteReferencePattern, path: {'id': id});
  }
}
