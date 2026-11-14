import '../models/conversation.dart';
import '../models/json.dart';
import '../models/message.dart';
import 'client.dart';
import 'operations.dart';

/// A message's thread as `getThread` returns it.
class ThreadPage {
  ThreadPage(this.parent, this.replies, this.state, this.hasMore);
  final Message parent;
  final List<Message> replies;
  final ThreadState? state;
  final bool hasMore;
}

/// An earlier version of a message, or the last content of a deleted one
/// (`MessageVersion`).
class MessageVersion {
  MessageVersion(this.raw)
    : version = asInt(raw['version']),
      text = raw['text'] as String? ?? '',
      entities = listOf(raw['entities'], Entity.fromJson),
      files = listOf(raw['files'], FileRef.fromJson),
      cards = listOf(raw['cards'], MessageCard.fromJson),
      replacedAt = parseTime(raw['replaced_at']),
      replacedById = raw['replaced_by_id'] as String? ?? '',
      reason = raw['reason'] as String? ?? 'edit';

  final Json raw;
  final int version;
  final String text;
  final List<Entity> entities;
  final List<FileRef> files;
  final List<MessageCard> cards;
  final DateTime? replacedAt;
  final String replacedById;

  /// `edit` or `delete`.
  final String reason;
}

/// Something that can be typed as `/name` (`Command`).
class SlashCommand {
  SlashCommand(this.raw)
    : name = raw['name'] as String? ?? '',
      kind = raw['kind'] as String? ?? 'builtin',
      description = raw['description'] as String? ?? '',
      hint = raw['hint'] as String?;

  final Json raw;
  final String name;

  /// `builtin`, `saved_reply` or `bot`.
  final String kind;
  final String description;
  final String? hint;
}

/// The operations of the conversation screen: threads, edits, deletes,
/// history, purge, reactions, pins, bookmarks, reports, notify anyway,
/// files, typing, commands and send later.
extension ConversationOpsApi on ApiClient {
  Future<Message> _message(
    Op op,
    Map<String, String> path, {
    Object? body,
    Map<String, Object?>? query,
  }) async => Message.fromJson(
    object((await call(op, path: path, body: body, query: query))['message']) ??
        const {},
  );

  Future<Message> getMessage(String id) => _message(Ops.getMessage, {'id': id});

  /// Changes a message's text and files. Fields left null stay.
  Future<Message> editMessage(
    String id, {
    String? text,
    List<String>? fileIds,
  }) => _message(
    Ops.editMessage,
    {'id': id},
    body: {'text': ?text, 'file_ids': ?fileIds},
  );

  Future<Message> deleteMessage(String id) =>
      _message(Ops.deleteMessage, {'id': id});

  /// The parent and its replies, oldest first, paged by `seq`.
  Future<ThreadPage> getThread(String id, {int? after, int limit = 200}) async {
    final j = await call(
      Ops.getThread,
      path: {'id': id},
      query: {'after': after, 'limit': limit},
    );
    final s = object(j['thread_state']);
    return ThreadPage(
      Message.fromJson(object(j['parent']) ?? const {}),
      listOf(j['messages'], Message.fromJson),
      s == null ? null : ThreadState.fromJson(s),
      j['has_more'] == true,
    );
  }

  Future<List<MessageVersion>> getMessageHistory(String id) async {
    final j = await call(Ops.getMessageHistory, path: {'id': id});
    return [
      for (final v in j['versions'] as List? ?? const [])
        if (v is Map) MessageVersion(v.cast<String, dynamic>()),
    ];
  }

  /// Purges or asks for a purge. Returns `purged` or `requested` and the
  /// message as it now is.
  Future<(String, Message)> purgeMessage(String id) async {
    final j = await call(Ops.purgeMessage, path: {'id': id});
    return (
      j['state'] as String? ?? 'requested',
      Message.fromJson(object(j['message']) ?? const {}),
    );
  }

  Future<List<Reaction>> listReactions(String id) async => listOf(
    (await call(Ops.listReactions, path: {'id': id}))['reactions'],
    Reaction.fromJson,
  );

  Future<Message> notifyAnyway(String id, String accountId) =>
      _message(Ops.notifyAnyway, {'id': id}, body: {'account_id': accountId});

  Future<void> reportMessage(String id, String reason) =>
      call(Ops.reportMessage, path: {'id': id}, body: {'reason': reason});

  Future<void> addBookmark(String id) =>
      call(Ops.addBookmark, path: {'id': id});
  Future<void> removeBookmark(String id) =>
      call(Ops.removeBookmark, path: {'id': id});

  Future<void> pinMessage(String conversationId, String messageId) => call(
    Ops.pinMessage,
    path: {'id': conversationId, 'message_id': messageId},
  );
  Future<void> unpinMessage(String conversationId, String messageId) => call(
    Ops.unpinMessage,
    path: {'id': conversationId, 'message_id': messageId},
  );

  Future<Conversation> moveThreadToChannel(
    String parentId,
    String name,
  ) async => Conversation.fromJson(
    object(
          (await call(
            Ops.moveThreadToChannel,
            path: {'id': parentId},
            body: {'name': name},
          ))['conversation'],
        ) ??
        const {},
  );

  /// `following`, `ignored` or `none`.
  Future<void> setThreadState(String parentId, String state) =>
      call(Ops.setThreadState, path: {'id': parentId}, body: {'state': state});

  Future<void> setThreadReadPosition(String parentId, int seq) => call(
    Ops.setThreadReadPosition,
    path: {'id': parentId},
    body: {'seq': seq},
  );

  Future<void> sendTyping(String conversationId, {String? parentId}) => call(
    Ops.sendTyping,
    path: {'id': conversationId},
    body: {'parent_id': ?parentId},
  );

  /// Uploads one file to a conversation, with an optional caption and,
  /// for a video, a poster image.
  Future<FileRef> uploadFile(
    String conversationId, {
    required String name,
    required List<int> bytes,
    String? caption,
    List<int>? poster,
    void Function(int sent, int total)? onProgress,
  }) async {
    final j = await upload(
      Ops.uploadFile,
      path: {'id': conversationId},
      fields: {if (caption != null && caption.isNotEmpty) 'caption': caption},
      parts: [
        UploadPart('file', name, bytes),
        if (poster != null) UploadPart('poster', 'poster.jpg', poster),
      ],
      onProgress: onProgress,
    );
    return FileRef.fromJson(object(j['file']) ?? const {});
  }

  Future<List<SlashCommand>> listCommands(String conversationId) async => [
    for (final c
        in (await call(
                  Ops.listCommands,
                  query: {'conversation_id': conversationId},
                ))['commands']
                as List? ??
            const [])
      if (c is Map) SlashCommand(c.cast<String, dynamic>()),
  ];

  /// Runs a saved reply or a bot command. Returns the posted message for
  /// a saved reply, and the interaction id for a bot command (which
  /// answers later).
  Future<(Message?, String?)> runCommand(
    String conversationId,
    String name, {
    String? text,
    String? parentId,
  }) async {
    final j = await call(
      Ops.runCommand,
      path: {'id': conversationId},
      body: {
        'name': name,
        if (text != null && text.isNotEmpty) 'text': text,
        'parent_id': ?parentId,
      },
    );
    final m = object(j['message']);
    return (
      m == null ? null : Message.fromJson(m),
      j['interaction_id'] as String?,
    );
  }

  /// Schedules a message to be posted once at [runAt].
  Future<Json> scheduleMessage(
    String conversationId,
    Json content,
    DateTime runAt,
  ) async =>
      object(
        (await call(
          Ops.createScheduled,
          body: {
            'conversation_id': conversationId,
            'kind': 'message',
            'content': content,
            'run_at': formatTime(runAt.toUtc()),
          },
        ))['scheduled'],
      ) ??
      const {};

  /// Sets the person's default thread view: `threaded` or `flat`.
  Future<void> setDefaultThreadView(String view) =>
      call(Ops.updatePrefs, body: {'thread_view': view});
}
