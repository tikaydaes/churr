import 'account.dart';
import 'json.dart';

/// A chat request as the person asked sees it (ruling R78,
/// docs/FEATURES.md section 20.2): who is asking and for what kind of chat,
/// nothing more.
class ChatRequest {
  ChatRequest({
    required this.id,
    required this.kind,
    required this.account,
    this.createdAt,
    this.raw = const {},
  });

  factory ChatRequest.fromJson(Json j) => ChatRequest(
    id: j['id'] as String? ?? '',
    kind: j['kind'] as String? ?? 'direct',
    account: Account.fromJson(object(j['account']) ?? const {}),
    createdAt: parseTime(j['created_at']),
    raw: j,
  );

  final String id;

  /// `direct` or `sealed`.
  final String kind;

  /// Who is asking, as the caller is shown them.
  final Account account;
  final DateTime? createdAt;
  final Json raw;

  bool get sealed => kind == 'sealed';
}
