import '../models/json.dart';
import 'client.dart';
import 'operations.dart';

/// One scheduled item (`ScheduledItem`): a message to send later, a
/// reminder, or a recurring form. Watches are listed by `watches.dart`.
class ScheduledItem {
  ScheduledItem(this.raw)
    : id = raw['id'] as String? ?? '',
      kind = raw['kind'] as String? ?? 'message',
      conversationId = raw['conversation_id'] as String? ?? '',
      accountId = raw['account_id'] as String? ?? '',
      content = object(raw['content']) ?? const {},
      repeat = object(raw['repeat']),
      nextRunAt = parseTime(raw['next_run_at']),
      failedAt = parseTime(raw['failed_at']),
      failure = raw['failure'] as String? ?? '';
  final Json raw;
  final String id;

  /// `message`, `reminder` or `form`.
  final String kind;
  final String conversationId;
  final String accountId;
  final Json content;

  /// For a recurring form: `every`, `days`, `at`, `time_zone`.
  final Json? repeat;

  /// Absent once it has run, or failed.
  final DateTime? nextRunAt;
  final DateTime? failedAt;
  final String failure;

  String get text => content['text'] as String? ?? '';
  Json? get form => object(content['form']);
}

/// A personal saved reply (`SavedReply`): text posted by typing `/name`.
class SavedReply {
  SavedReply(Json raw)
    : id = raw['id'] as String? ?? '',
      name = raw['name'] as String? ?? '',
      text = raw['text'] as String? ?? '',
      community = raw['community'] == true;
  final String id;
  final String name;
  final String text;

  /// One an admin made for everyone, which the person cannot change.
  final bool community;
}

extension ScheduledApi on ApiClient {
  /// The person's own scheduled items of [kind], or with
  /// [conversationId] that conversation's, recurring forms included for a
  /// manager.
  Future<List<ScheduledItem>> listScheduledItems({
    String? kind,
    String? conversationId,
  }) => allPages(
    Ops.listScheduled,
    'scheduled',
    ScheduledItem.new,
    query: {'kind': kind, 'conversation_id': conversationId},
    limit: 200,
  );

  /// Changes the content, the time or the repeat of one before it runs.
  Future<ScheduledItem> updateScheduled(
    String id, {
    Json? content,
    DateTime? runAt,
    Json? repeat,
  }) async => ScheduledItem(
    object(
          (await call(
            Ops.updateScheduled,
            path: {'id': id},
            body: {
              'content': ?content,
              if (runAt != null) 'run_at': formatTime(runAt.toUtc()),
              'repeat': ?repeat,
            },
          ))['scheduled'],
        ) ??
        const {},
  );

  /// Sets up a recurring form (a check-in) from a whole `ScheduledInput`.
  Future<ScheduledItem> createScheduledItem(Json body) async => ScheduledItem(
    object((await call(Ops.createScheduled, body: body))['scheduled']) ??
        const {},
  );

  /// Sends a scheduled message now, instead of at its time.
  Future<void> sendScheduledNow(String id) =>
      call(Ops.sendScheduledNow, path: {'id': id});
}

extension SavedRepliesApi on ApiClient {
  /// The person's own saved replies; the community's are left out.
  Future<List<SavedReply>> listMySavedReplies() async => [
    for (final r in listOf(
      (await call(Ops.listSavedReplies))['saved_replies'],
      SavedReply.new,
    ))
      if (!r.community) r,
  ];

  Future<void> createSavedReply(String name, String text) =>
      call(Ops.createSavedReply, body: {'name': name, 'text': text});

  Future<void> updateSavedReply(String id, String name, String text) => call(
    Ops.updateSavedReply,
    path: {'id': id},
    body: {'name': name, 'text': text},
  );

  Future<void> deleteSavedReply(String id) =>
      call(Ops.deleteSavedReply, path: {'id': id});
}
