import '../core/api/chat_requests.dart';
import '../core/api/conversations.dart';
import '../core/api/errors.dart';
import '../core/models/conversation.dart';
import 'channel_actions.dart';
import 'sealed_state.dart';
import 'server_state.dart';

/// Chat requests, invitations and closed chats on one server (rulings
/// R77, R78, R79, R103; docs/FEATURES.md section 20, docs/CLIENTS.md
/// 9.28).
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

  /// Accepts: the chat opens for both, or the caller joins the group chat
  /// or channel; it is returned to be opened.
  Future<Conversation> accept(ChatRequest r) async {
    final c = await server.api.acceptChatRequest(r.id);
    await _drop(r.id);
    return ChannelActions(server).keep(c);
  }

  /// "I don't want to": the person asking is told it was declined.
  Future<void> decline(ChatRequest r) async {
    await _drop(r.id);
    await server.api.declineChatRequest(r.id);
    await _forget(r);
  }

  /// Out of the list; to the person asking it goes on waiting.
  Future<void> dismiss(ChatRequest r) async {
    await _drop(r.id);
    await server.api.dismissChatRequest(r.id);
    await _forget(r);
  }

  /// Blocks the person asking; to them it goes on waiting.
  Future<void> block(ChatRequest r) async {
    await _drop(r.id);
    await server.api.blockChatRequester(r.id);
    await _forget(r);
  }

  Future<void> _forget(ChatRequest r) async {
    final id = r.conversationId;
    if (id != null) await forgetPreview(server, id);
  }

  /// An invitation to a public channel, opened to read before answering.
  Future<Conversation?> preview(ChatRequest r) async {
    if (!r.previewable || r.conversationId == null) return null;
    return previewChannel(server, r.conversationId!, r.channel);
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

/// The channels offered to the account on one server (ruling R103,
/// docs/CLIENTS.md 9.28, "Offers"): Accept joins at the level the offer
/// brings, Decline takes it out of the sidebar. The database is changed
/// first and the server's channel_offer.removed confirms it.
class ChannelOfferActions {
  ChannelOfferActions(this.server);
  final ServerState server;

  Future<void> _drop(String id) async {
    await server.db.putChannelOffers([
      for (final o in await server.db.channelOffers())
        if (o.conversationId != id) o,
    ]);
  }

  /// Accepts: the channel is joined, and returned to be opened.
  Future<Conversation> accept(ChannelOffer o) async {
    final c = await server.api.acceptChannelOffer(o.conversationId);
    await _drop(o.conversationId);
    return ChannelActions(server).keep(c);
  }

  /// Declines: out of the sidebar; Browse still offers Join.
  Future<void> decline(ChannelOffer o) async {
    await _drop(o.conversationId);
    await server.api.declineChannelOffer(o.conversationId);
    await forgetPreview(server, o.conversationId);
  }

  /// A public one, opened to read before answering.
  Future<Conversation?> preview(ChannelOffer o) async => o.previewable
      ? previewChannel(server, o.conversationId, o.channel)
      : null;
}

/// Opens a public channel the account is not in, to read without joining
/// (docs/CLIENTS.md 9.3): it is written to the cache without a
/// membership, so it has no row in the sidebar except while open, and the
/// next refresh of the account's state drops it unless it was joined.
/// [seen] stands in when the channel cannot be fetched.
Future<Conversation> previewChannel(
  ServerState server,
  String id,
  Conversation? seen,
) async {
  Conversation c;
  try {
    c = await server.api.getConversation(id);
  } on ApiException {
    if (seen == null) rethrow;
    c = seen;
  }
  if (await server.db.conversation(id) == null) {
    await server.db.upsertConversation(c, syncedSeq: c.lastSeq);
  }
  return c;
}

/// Drops a channel opened only to read before answering, once answered
/// with anything but Accept.
Future<void> forgetPreview(ServerState server, String id) async {
  final row = await server.db.conversation(id);
  if (row != null && row.conversation.membership == null) {
    await server.db.deleteConversation(id);
  }
}
