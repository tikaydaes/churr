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

  /// Accepts a request: a direct or sealed chat opens for both; for a
  /// group chat or a channel the caller becomes a member.
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

  /// The channels offered to the caller (ruling R103), oldest first.
  Future<List<ChannelOffer>> listChannelOffers() async {
    final j = await call(Ops.listChannelOffers);
    return listOf(j['offers'], ChannelOffer.fromJson);
  }

  /// Accepts an offer: the caller becomes a member at the level it brings.
  Future<Conversation> acceptChannelOffer(String conversationId) async {
    final j = await call(Ops.acceptChannelOffer, path: {'id': conversationId});
    return Conversation.fromJson(object(j['conversation'])!);
  }

  /// Declines an offer; the channel may still be joined from Browse.
  Future<void> declineChannelOffer(String conversationId) =>
      call(Ops.declineChannelOffer, path: {'id': conversationId});

  /// Closes a direct or sealed chat for both people.
  Future<Conversation> closeChat(String conversationId) async {
    final j = await call(Ops.closeChat, path: {'id': conversationId});
    return Conversation.fromJson(object(j['conversation'])!);
  }

  /// Withdraws a request of the caller's that still waits; its chat goes.
  Future<void> withdrawChatRequest(String conversationId) =>
      call(Ops.withdrawChatRequest, path: {'id': conversationId});
}
