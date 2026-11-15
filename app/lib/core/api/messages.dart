import '../conversation/broad_mentions.dart';
import '../models/json.dart';
import '../ids.dart';
import '../models/message.dart';
import 'client.dart';
import 'operations.dart';

/// One page of messages, paged by sequence number (docs/API.md section
/// 4). Thread states arrive beside the messages, not inside them.
class MessagePage {
  MessagePage(this.messages, this.threadStates, this.hasMore);
  final List<Message> messages;
  final List<ThreadState> threadStates;
  final bool hasMore;
}

/// One page of a conversation's stored events (`listEvents`).
class EventPage {
  EventPage(this.events, this.hasMore, this.reload);
  final List<Json> events;
  final bool hasMore;

  /// The position asked for is older than the log: reload instead.
  final bool reload;
}

/// Messages, reactions, checklists and catch-up.
extension MessagesApi on ApiClient {
  /// Asks the server to draw a result (/roll, /flip, /pick, /shuffle,
  /// /draw, /mahjongg, /tarot; rulings R67 and R68) and posts it as a
  /// message of its own. [chance] is a `ChanceInput`.
  Future<Message> postMessageChance(
    String conversationId,
    Json chance, {
    String? parentId,
  }) async {
    final j = await call(
      Ops.postMessage,
      path: {'id': conversationId},
      body: {'client_id': newUuid(), 'chance': chance, 'parent_id': ?parentId},
    );
    return Message.fromJson(object(j['message']) ?? const {});
  }

  /// The newest [limit] messages, or those before or after a `seq`.
  /// Replies come among top-level messages.
  Future<MessagePage> listMessages(
    String conversationId, {
    int? before,
    int? after,
    int limit = 50,
  }) async {
    final j = await call(
      Ops.listMessages,
      path: {'id': conversationId},
      query: {'before': before, 'after': after, 'limit': limit},
    );
    return MessagePage(
      listOf(j['messages'], Message.fromJson),
      listOf(j['thread_states'], ThreadState.fromJson),
      j['has_more'] == true,
    );
  }

  /// Posts a message. [clientId] makes a retry safe: sending the same one
  /// again within 24 hours answers with the message already created
  /// (docs/API.md section 5), so every post carries one.
  Future<Message> postMessage(
    String conversationId, {
    required String clientId,
    required String text,
    String? parentId,
    List<String> fileIds = const [],
    Json? quote,
    bool action = false,
    Json? chance,
    void Function(List<String> cannotReadIds)? onCannotRead,
    void Function()? onBroadRefused,
  }) async {
    final j = await call(
      Ops.postMessage,
      path: {'id': conversationId},
      body: {
        'client_id': clientId,
        // A chance request stands alone: the server draws the result.
        if (chance == null) 'text': text,
        'parent_id': ?parentId,
        'quote': ?quote,
        if (fileIds.isNotEmpty) 'file_ids': fileIds,
        if (action) 'action': true,
        'chance': ?chance,
      },
    );
    onCannotRead?.call(strings(j['cannot_read_ids']));
    // The text held @channel, @here or @everyone that the author may not
    // use there: it was stored as plain text (ruling R64).
    if (j['broad_mentions_refused'] == true) onBroadRefused?.call();
    return Message.fromJson(object(j['message']) ?? const {});
  }

  /// Asks what the broad mentions of [text] would do if it were posted:
  /// whether they are allowed, and how many people they would notify.
  Future<BroadCheck> checkBroadMentions(
    String conversationId,
    String text,
  ) async => BroadCheck.fromJson(
    await call(
      Ops.checkBroadMentions,
      path: {'id': conversationId},
      body: {'text': text},
    ),
  );

  Future<Reaction> addReaction(String messageId, String emoji) async =>
      Reaction.fromJson(
        object(
              (await call(
                Ops.addReaction,
                path: {'id': messageId, 'emoji': emoji},
              ))['reaction'],
            ) ??
            const {},
      );

  Future<void> removeReaction(String messageId, String emoji) =>
      call(Ops.removeReaction, path: {'id': messageId, 'emoji': emoji});

  Future<TaskState> setChecklistItem(
    String messageId,
    String itemId,
    bool checked,
  ) async => TaskState.fromJson(
    object(
          (await call(
            Ops.setChecklistItem,
            path: {'id': messageId, 'item': itemId},
            body: {'checked': checked},
          ))['task'],
        ) ??
        const {},
  );

  /// Stored events after a `seq`, oldest first; [until] fetches exactly a
  /// gap.
  Future<EventPage> listEvents(
    String conversationId, {
    required int after,
    int? until,
    int limit = 200,
  }) async {
    final j = await call(
      Ops.listEvents,
      path: {'id': conversationId},
      query: {'after': after, 'until': until, 'limit': limit},
    );
    return EventPage(
      [
        if (j['events'] is List)
          for (final e in j['events'] as List)
            if (e is Map) e.cast<String, dynamic>(),
      ],
      j['has_more'] == true,
      j['reload'] == true,
    );
  }
}
