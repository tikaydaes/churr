import '../models/chat_request.dart';
import '../models/conversation.dart';
import '../models/json.dart';
import 'client.dart';
import 'operations.dart';

export '../models/chat_request.dart';

extension ChatRequestsApi on ApiClient {
  /// The requests waiting for the caller, oldest first.
  Future<List<ChatRequest>> listChatRequests() async {
    final j = await call(Ops.listChatRequests);
    return listOf(j['requests'], ChatRequest.fromJson);
  }

  /// Accepts a request: the chat opens for both.
  Future<Conversation> acceptChatRequest(String id) async {
    final j = await call(Ops.acceptChatRequest, path: {'id': id});
    return Conversation.fromJson(object(j['conversation'])!);
  }

  Future<void> declineChatRequest(String id) =>
      call(Ops.declineChatRequest, path: {'id': id});

  Future<void> dismissChatRequest(String id) =>
      call(Ops.dismissChatRequest, path: {'id': id});

  Future<void> blockChatRequester(String id) =>
      call(Ops.blockChatRequester, path: {'id': id});

  /// Closes a direct or sealed chat for both people.
  Future<Conversation> closeChat(String conversationId) async {
    final j = await call(Ops.closeChat, path: {'id': conversationId});
    return Conversation.fromJson(object(j['conversation'])!);
  }

  /// Withdraws a request of the caller's that still waits; its chat goes.
  Future<void> withdrawChatRequest(String conversationId) =>
      call(Ops.withdrawChatRequest, path: {'id': conversationId});
}
