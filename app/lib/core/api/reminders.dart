import '../models/json.dart';
import 'channels.dart';
import 'client.dart';
import 'operations.dart';

/// "Remind me about this" (docs/FEATURES.md section 9.1, CLIENTS.md 9.6):
/// a `reminder` scheduled into the person's own one-person channel, about
/// a message, which the delivered reminder quotes.
extension RemindersApi on ApiClient {
  /// Schedules the reminder for [runAt]. The one-person channel is found
  /// or made first (`openChat` with nobody else). The text is the web
  /// chat's, "Reminder"; the quote carries the message.
  Future<Json> remindMeAbout(String messageId, DateTime runAt) async {
    final mine = await openChat(const []);
    final res = await call(
      Ops.createScheduled,
      body: {
        'conversation_id': mine.id,
        'kind': 'reminder',
        'content': {'text': 'Reminder'},
        'about_message_id': messageId,
        'run_at': formatTime(runAt.toUtc()),
      },
    );
    return object(res['scheduled']) ?? const {};
  }
}
