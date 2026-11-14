import '../calls/call_media.dart';
import '../calls/call_model.dart';
import '../models/json.dart';
import 'client.dart';
import 'operations.dart';

/// The three call operations (docs/openapi.yaml, tag Calls).
extension CallsApi on ApiClient {
  /// The call in progress in a conversation, or null.
  Future<CallInfo?> getCall(String conversationId) async => CallInfo.fromJson(
    object((await call(Ops.getCall, path: {'id': conversationId}))['call']),
  );

  /// Asks to join (starting the call when there is none) and returns the
  /// call and a ticket for `call.join`.
  Future<(CallInfo, String)> joinCall(String conversationId) async {
    final j = await call(
      Ops.joinCall,
      path: {'id': conversationId},
      body: const <String, Object>{},
    );
    final c = CallInfo.fromJson(object(j['call']));
    final ticket = j['ticket'];
    if (c == null || ticket is! String) {
      throw const FormatException('joinCall answered without a call');
    }
    return (c, ticket);
  }

  Future<void> leaveCall(String conversationId) async {
    await call(Ops.leaveCall, path: {'id': conversationId});
  }
}

/// [CallApi] over an [ApiClient].
class ClientCallApi implements CallApi {
  ClientCallApi(this.client);
  final ApiClient client;

  @override
  Future<(CallInfo, String)> joinCall(String conversationId) =>
      client.joinCall(conversationId);

  @override
  Future<CallInfo?> getCall(String conversationId) =>
      client.getCall(conversationId);

  @override
  Future<void> leaveCall(String conversationId) =>
      client.leaveCall(conversationId);
}
