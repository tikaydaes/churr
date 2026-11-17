import '../models/account.dart';
import '../models/json.dart';
import 'client.dart';
import 'operations.dart';

/// The kinds of request that wait for approval, in the order they are
/// offered as filters (docs/FEATURES.md section 31).
const approvalKinds = ['registration', 'channel', 'webhook'];

/// What the caller may approve and how many wait (`getApprovalSummary`).
class ApprovalSummary {
  const ApprovalSummary({
    this.mayApprove = false,
    this.kinds = const [],
    this.counts = const {},
  });

  factory ApprovalSummary.fromJson(Json j) => ApprovalSummary(
    mayApprove: j['may_approve'] == true,
    kinds: [for (final k in (j['kinds'] as List? ?? const [])) '$k'],
    counts: {
      for (final e in (object(j['counts']) ?? const {}).entries)
        if (e.value is num) e.key: (e.value as num).toInt(),
    },
  );

  /// Whether the caller may approve requests as well as their role lets
  /// them (the account's "may approve requests" flag).
  final bool mayApprove;

  /// The kinds the caller decides; empty for someone who decides none.
  final List<String> kinds;

  /// How many wait, by kind.
  final Map<String, int> counts;

  /// How many wait in all: the count on the Approvals entry.
  int get total => counts.values.fold(0, (n, c) => n + c);
}

/// One request that waits for approval (`Approval`).
class Approval {
  Approval(this.raw)
    : id = raw['id'] as String? ?? '',
      kind = raw['kind'] as String? ?? '',
      requesterId = raw['requester_id'] as String? ?? '',
      summary = raw['summary'] as String? ?? '',
      note = raw['note'] as String? ?? '',
      state = raw['state'] as String? ?? 'waiting',
      reason = raw['reason'] as String? ?? '',
      createdAt = parseTime(raw['created_at']),
      expiresAt = parseTime(raw['expires_at']),
      requester = object(raw['requester']) == null
          ? null
          : Account.fromJson(object(raw['requester'])!);

  final Json raw;
  final String id;

  /// `registration`, `channel` or `webhook`.
  final String kind;
  final String requesterId;

  /// What is asked for, in the server's words (a channel's name, an
  /// applicant's username).
  final String summary;

  /// The requester's note, for whoever decides.
  final String note;

  /// `waiting`, `approved`, `declined` or `lapsed`.
  final String state;

  /// Why it was declined, once decided.
  final String reason;
  final DateTime? createdAt;
  final DateTime? expiresAt;
  final Account? requester;
}

extension ApprovalsApi on ApiClient {
  Future<ApprovalSummary> getApprovalSummary() async {
    final j = await call(Ops.getApprovalSummary);
    return ApprovalSummary.fromJson(object(j['summary']) ?? const {});
  }

  /// The requests that wait for the caller, oldest first; [kind] narrows
  /// them to one kind.
  Future<List<Approval>> listApprovals({String? kind}) async {
    final j = await call(Ops.listApprovals, query: {'kind': ?kind});
    return listOf(j['approvals'], Approval.new);
  }

  /// Approves or declines; [reason] (at most 500 characters) is shown to
  /// the requester of a declined request.
  Future<Approval> decideApproval(
    Approval a, {
    required bool approve,
    String? reason,
  }) async {
    final j = await call(
      Ops.decideApproval,
      path: {'kind': a.kind, 'id': a.id},
      body: {
        'approve': approve,
        if (reason != null && reason.isNotEmpty) 'reason': reason,
      },
    );
    return Approval(object(j['approval']) ?? const {});
  }

  /// Asks for a channel while new channels wait for approval: what
  /// `createChannel` takes, and a note for whoever decides.
  Future<Approval> requestChannel(Json channel, {String? note}) async {
    final j = await call(
      Ops.requestChannel,
      body: {...channel, if (note != null && note.isNotEmpty) 'note': note},
    );
    return Approval(object(j['approval']) ?? const {});
  }
}
