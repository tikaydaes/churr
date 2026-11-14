import 'package:flutter/foundation.dart';

import 'app_state.dart';

/// What another screen asks the conversation screen to do when it opens
/// (or at once, if it is open): open a thread, or show a message.
class ConversationIntent {
  const ConversationIntent({this.threadParentId, this.messageId, this.quote});

  /// Open this thread in the thread pane.
  final String? threadParentId;

  /// Bring this message into view and mark it for a moment.
  final String? messageId;

  /// With [threadParentId]: start a reply quoting this
  /// (`{"message_id", "start", "end"}`).
  final Map<String, Object?>? quote;
}

/// The way in to the conversation screen from elsewhere: the Threads
/// view, search, notifications, bookmarks.
///
/// ```dart
/// ConversationIntents.instance.openThread(app, OpenTarget(serverId, conversationId), parentId);
/// ```
///
/// It opens the conversation through [AppState.openConversation] and
/// leaves the request here; the conversation screen takes it when it is
/// built, or at once if it is already showing that conversation.
class ConversationIntents extends ChangeNotifier {
  ConversationIntents._();
  static final instance = ConversationIntents._();

  final _pending = <OpenTarget, ConversationIntent>{};

  void _ask(AppState app, OpenTarget target, ConversationIntent intent) {
    _pending[target] = intent;
    if (app.open != target) app.openConversation(target);
    notifyListeners();
  }

  /// Opens [target] with the thread of [parentId] in the thread pane.
  void openThread(AppState app, OpenTarget target, String parentId) =>
      _ask(app, target, ConversationIntent(threadParentId: parentId));

  /// Opens [target] and shows [messageId].
  void openAt(AppState app, OpenTarget target, String messageId) =>
      _ask(app, target, ConversationIntent(messageId: messageId));

  /// Takes the waiting request for [target], if any.
  ConversationIntent? take(OpenTarget target) => _pending.remove(target);
}
