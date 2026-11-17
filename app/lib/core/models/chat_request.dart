import 'account.dart';
import 'conversation.dart';
import 'json.dart';

/// A request as the person asked sees it (rulings R78 and R103,
/// docs/FEATURES.md section 20.2): who is asking and for what, nothing
/// more. For a group chat also the names of who is in it; for a named
/// channel, the channel as someone outside it sees it.
class ChatRequest {
  ChatRequest({
    required this.id,
    required this.kind,
    required this.account,
    this.createdAt,
    this.conversationId,
    this.people = const [],
    this.channel,
    this.level,
    this.override = false,
    this.raw = const {},
  });

  factory ChatRequest.fromJson(Json j) => ChatRequest(
    id: j['id'] as String? ?? '',
    kind: j['kind'] as String? ?? 'direct',
    account: Account.fromJson(object(j['account']) ?? const {}),
    createdAt: parseTime(j['created_at']),
    conversationId: j['conversation_id'] as String?,
    people: strings(j['people']),
    channel: object(j['channel']) == null
        ? null
        : Conversation.fromJson(object(j['channel'])!),
    level: j['level'] as String?,
    override: j['override'] == true,
    raw: j,
  );

  final String id;

  /// `direct`, `sealed`, `group` (being included in a group chat) or
  /// `channel` (an invitation to a named channel).
  final String kind;

  /// Who is asking, as the caller is shown them.
  final Account account;
  final DateTime? createdAt;

  /// For `group` and `channel`: the conversation.
  final String? conversationId;

  /// For `group`: the names of who is in the chat now.
  final List<String> people;

  /// For `channel`: its name, topic, access and member count.
  final Conversation? channel;

  /// For `channel`: the level accepting brings.
  final String? level;

  /// For `channel`: sent by an owner from the admin console; joining
  /// shows every member the `owner_override` notice.
  final bool override;
  final Json raw;

  bool get sealed => kind == 'sealed';
  bool get isGroup => kind == 'group';
  bool get isChannel => kind == 'channel';

  /// Only a public channel can be read before accepting.
  bool get previewable => isChannel && channel?.access == 'public';
}

/// A named channel offered to the caller (ruling R103): a group they are
/// in was added to it, or it became a default channel after they signed
/// up. Shown in the sidebar with Accept and Decline.
class ChannelOffer {
  ChannelOffer({
    required this.conversationId,
    required this.source,
    required this.level,
    required this.channel,
    this.createdAt,
    this.raw = const {},
  });

  factory ChannelOffer.fromJson(Json j) {
    final id = j['conversation_id'] as String? ?? '';
    return ChannelOffer(
      conversationId: id,
      source: j['source'] as String? ?? 'group',
      level: j['level'] as String? ?? 'member',
      createdAt: parseTime(j['created_at']),
      channel: Conversation.fromJson(object(j['channel']) ?? {'id': id}),
      raw: j,
    );
  }

  final String conversationId;

  /// `group` or `default`.
  final String source;

  /// The level accepting brings.
  final String level;
  final DateTime? createdAt;
  final Conversation channel;
  final Json raw;

  bool get viaDefault => source == 'default';

  /// Only a public channel can be read before accepting.
  bool get previewable => channel.access == 'public';
}
