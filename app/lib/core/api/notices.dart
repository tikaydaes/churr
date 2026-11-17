import '../models/json.dart';
import 'client.dart';
import 'operations.dart';

/// One notice kept for the caller (`Notice`, docs/FEATURES.md section
/// 21.13): something done that concerns their account.
class Notice {
  Notice(this.raw)
    : id = raw['id'] as String? ?? '',
      kind = raw['kind'] as String? ?? '',
      byId = raw['by_id'] as String?,
      subjectId = raw['subject_id'] as String?,
      conversationId = raw['conversation_id'] as String?,
      createdAt = parseTime(raw['created_at']),
      seenAt = parseTime(raw['seen_at']);

  final Json raw;
  final String id;

  /// `recovery_link_used`, or for Owners `owner_made`,
  /// `owner_stepped_down`, `export_started`, `export_downloaded`,
  /// `override_used`.
  final String kind;

  /// Who did it, when known.
  final String? byId;

  /// The account it is about, when not the caller.
  final String? subjectId;

  /// The conversation it is about, if any.
  final String? conversationId;
  final DateTime? createdAt;
  final DateTime? seenAt;
}

/// The newest notices and how many are unseen.
class NoticeList {
  const NoticeList({this.notices = const [], this.unseen = 0});
  final List<Notice> notices;
  final int unseen;
}

extension NoticesApi on ApiClient {
  /// The notices kept for the caller, newest first.
  Future<NoticeList> listNotices() async {
    final j = await call(Ops.listNotices);
    return NoticeList(
      notices: listOf(j['notices'], Notice.new),
      unseen: (j['unseen'] as num?)?.toInt() ?? 0,
    );
  }

  /// Marks every notice of the caller seen.
  Future<void> markNoticesSeen() async {
    await call(Ops.markNoticesSeen);
  }
}
