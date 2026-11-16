import '../core/api/chat_requests.dart';
import '../core/models/conversation.dart';
import 'channel_actions.dart';
import 'sealed_state.dart';
import 'server_state.dart';

/// Chat requests and closed chats on one server (rulings R77, R78, R79;
/// docs/FEATURES.md section 20, docs/CLIENTS.md 9.28).
///
/// The person asked answers a request; the database is changed first (the
/// request leaves the list) and the server's chat_request.removed confirms
/// it. Accepting writes the chat the server answered with. Closing and
/// withdrawing are the chat's people's; asking again starts a new chat,
/// a fresh sealed chat for a sealed one.
class ChatRequestActions {
  ChatRequestActions(this.server);
  final ServerState server;

  Future<void> _drop(String id) async {
    await server.db.putChatRequests([
      for (final r in await server.db.chatRequests())
        if (r.id != id) r,
    ]);
  }

  /// Accepts: the chat opens for both, and is returned to be opened.
  Future<Conversation> accept(ChatRequest r) async {
    final c = await server.api.acceptChatRequest(r.id);
    await _drop(r.id);
    return ChannelActions(server).keep(c);
  }

  /// "I don't want to": the person asking is told it was declined.
  Future<void> decline(ChatRequest r) async {
    await _drop(r.id);
    await server.api.declineChatRequest(r.id);
  }

  /// Out of the list; to the person asking it goes on waiting.
  Future<void> dismiss(ChatRequest r) async {
    await _drop(r.id);
    await server.api.dismissChatRequest(r.id);
  }

  /// Blocks the person asking; to them it goes on waiting.
  Future<void> block(ChatRequest r) async {
    await _drop(r.id);
    await server.api.blockChatRequester(r.id);
  }

  /// Closes a direct or sealed chat for both people.
  Future<Conversation> close(String conversationId) async =>
      ChannelActions(server).keep(await server.api.closeChat(conversationId));

  /// Withdraws a request that waits: its chat goes from this device too.
  Future<void> withdraw(String conversationId) async {
    await server.api.withdrawChatRequest(conversationId);
    await server.db.deleteConversation(conversationId);
  }

  /// The other person of a direct or sealed chat.
  String? other(Conversation c) {
    final me = server.me?.id;
    for (final id in c.memberIds) {
      if (id != me) return id;
    }
    return null;
  }

  /// "Ask to chat again" after a close or a decline: a new chat with the
  /// same person, a request unless they approve chats automatically. A
  /// sealed one is a fresh sealed chat with a new epoch record 1, made by
  /// this device (docs/SEALED.md section 6). Null when it cannot be done
  /// here (sealed chats not unlocked on this device).
  Future<Conversation?> askAgain(Conversation c) async {
    final who = other(c);
    if (who == null) return null;
    if (!c.isSealed) return ChannelActions(server).openChat([who]);
    final session = SealedHub.instance.of(server)?.session;
    if (session == null) return null;
    final conv = Conversation.fromJson(await session.startChat(who));
    return ChannelActions(server).keep(conv);
  }
}
