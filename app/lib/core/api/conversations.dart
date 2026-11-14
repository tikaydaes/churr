import '../models/conversation.dart';
import '../models/json.dart';
import 'client.dart';
import 'operations.dart';

/// Conversations and the caller's membership in them.
extension ConversationsApi on ApiClient {
  /// Every conversation the caller is in, with read positions, modes and
  /// counts.
  Future<List<Conversation>> listConversations() =>
      allPages(Ops.listConversations, 'conversations', Conversation.fromJson);

  Future<Conversation> getConversation(String id) async =>
      Conversation.fromJson(
        object(
              (await call(
                Ops.getConversation,
                path: {'id': id},
              ))['conversation'],
            ) ??
            const {},
      );

  Future<List<Section>> listSections() async =>
      listOf((await call(Ops.listSections))['sections'], Section.fromJson);

  /// Moves the caller's read position and returns the membership with
  /// its new counts.
  Future<Membership> setReadPosition(String id, int seq) async =>
      Membership.fromJson(
        object(
              (await call(
                Ops.setReadPosition,
                path: {'id': id},
                body: {'seq': seq},
              ))['membership'],
            ) ??
            const {},
      );

  /// Changes some of `mode`, `muted_until`, `show`, `starred` and
  /// `thread_view` (ruling R46). [changes] holds only the fields to
  /// change; an explicit null for `show` means "the app's default".
  Future<Membership> updateMembership(String id, Json changes) async =>
      Membership.fromJson(
        object(
              (await call(
                Ops.updateMembership,
                path: {'id': id},
                body: changes,
              ))['membership'],
            ) ??
            const {},
      );
}
