import '../models/message.dart';

/// The rules for drawing threads in a conversation (docs/FEATURES.md
/// section 1, docs/CLIENTS.md 9.3 and 9.4). Plain Dart, so they are
/// tested without widgets.

/// One line of a conversation's body.
sealed class BodyEntry {
  const BodyEntry();

  /// Where the entry is shown: entries are sorted by it, compared as
  /// plain text (the messages' `order`).
  String get order;
}

/// A message. In the flat view a reply carries [reply], what its header
/// says.
class MessageEntry extends BodyEntry {
  const MessageEntry(this.message, {this.reply});
  final Message message;
  final ReplyHeader? reply;

  @override
  String get order => message.order;
}

/// The moving marker of a thread in the threaded view (FEATURES.md 1.3):
/// drawn where the parent's latest reply would sit.
class MarkerEntry extends BodyEntry {
  const MarkerEntry({
    required this.parent,
    required this.unread,
    required this.title,
    required this.order,
  });
  final Message parent;

  /// Replies the reader has not seen. Zero reads "Replies to: …".
  final int unread;

  /// The first line of the parent's text, cut to 80 characters.
  final String title;

  /// The `order` of the thread's latest cached reply, or of the parent
  /// when none is cached: the marker sits just after it.
  @override
  final String order;

  /// "3 new replies to: Deploy plan", or "Replies to: Deploy plan".
  String get label => unread == 0
      ? 'Replies to: $title'
      : '$unread new ${unread == 1 ? 'reply' : 'replies'} to: $title';
}

/// The header of a reply in the flat view: what it answers (a link up to
/// it), the top message when that is a different one, and "open thread".
class ReplyHeader {
  const ReplyHeader({
    required this.parentId,
    required this.answeredId,
    this.answered,
    this.parent,
  });

  /// The thread's top message.
  final String parentId;

  /// The message the reply answers: the parent, or the reply it quotes.
  final String answeredId;

  /// The answered message, when it is cached.
  final Message? answered;
  final Message? parent;

  /// Whether a separate link to the top message is drawn.
  bool get showsParentLink => answeredId != parentId;
}

/// The reader's position in a thread (FEATURES.md 2.1): the thread
/// state's `read_seq` when the account has a row for it, otherwise the
/// conversation's read position.
int threadPosition(ThreadState? state, int conversationReadSeq) =>
    state?.readSeq ?? conversationReadSeq;

/// Whether a reply counts as unread for the reader (FEATURES.md 2.1):
/// not deleted, not a system notice, not the reader's own (unless it
/// is a reminder), and after the thread position.
bool countsAsUnread(Message reply, int position, String? myId) =>
    !reply.isDeleted &&
    !reply.isSystem &&
    (reply.authorId != myId || reply.via?.kind == 'reminder') &&
    reply.seq > position;

/// The title of a marker: the first line of the parent's text, cut to 80
/// characters.
String markerTitle(Message parent) {
  if (parent.isDeleted) return 'a deleted message';
  var line = parent.text
      .split('\n')
      .firstWhere((l) => l.trim().isNotEmpty, orElse: () => '');
  line = line.trim();
  if (line.isEmpty && parent.files.isNotEmpty) line = parent.files.first.name;
  if (line.isEmpty) line = 'a message';
  return line.length <= 80 ? line : '${line.substring(0, 79)}…';
}

/// The body of a conversation.
///
/// [messages] are the cached messages, replies included; they are drawn
/// sorted by `order` ([Message.compareOrder]), whatever order they come
/// in. [states] are the reader's thread states by parent id.
///
/// - **Threaded:** replies are left out; each thread whose parent is
///   cached and that the reader does not ignore gets one marker where
///   its latest reply sits by `order`.
/// - **Flat:** replies are in their place by `order`, each with its
///   header; replies in ignored threads are left out. No markers.
List<BodyEntry> buildBody({
  required List<Message> messages,
  required Map<String, ThreadState> states,
  required bool flat,
  required int conversationReadSeq,
  String? myId,
}) {
  messages = [...messages]..sort(Message.compareOrder);
  final byId = {for (final m in messages) m.id: m};
  if (flat) {
    return [
      for (final m in messages)
        if (!m.isReply)
          MessageEntry(m)
        else if (states[m.parentId]?.state != 'ignored')
          MessageEntry(
            m,
            reply: ReplyHeader(
              parentId: m.parentId!,
              answeredId: m.quote?.messageId ?? m.parentId!,
              answered: byId[m.quote?.messageId ?? m.parentId!],
              parent: byId[m.parentId!],
            ),
          ),
    ];
  }

  final out = <BodyEntry>[
    for (final m in messages)
      if (!m.isReply) MessageEntry(m),
  ];
  final markers = <MarkerEntry>[];
  for (final m in messages) {
    final t = m.thread;
    if (m.isReply || t == null || t.replyCount == 0) continue;
    final state = states[m.id];
    if (state?.state == 'ignored') continue;
    final position = threadPosition(state, conversationReadSeq);
    var unread = 0;
    var order = m.order;
    for (final r in messages) {
      if (r.parentId != m.id) continue;
      if (countsAsUnread(r, position, myId)) unread++;
      // Sorted, so the last reply seen is the latest by `order`.
      order = r.order;
    }
    // Replies older than the cache are not held: the server's count for
    // a thread with a row covers them.
    if (state != null && state.unreadCount > unread) unread = state.unreadCount;
    if (t.lastReplySeq <= position) unread = 0;
    markers.add(
      MarkerEntry(
        parent: m,
        unread: unread,
        title: markerTitle(m),
        order: order,
      ),
    );
  }
  if (markers.isEmpty) return out;
  // Merge by `order`: a marker sits where its latest reply would, so
  // after everything ordered before it (and after its parent, when no
  // reply is cached and it carries the parent's own order).
  markers.sort((a, b) => a.order.compareTo(b.order));
  final merged = <BodyEntry>[];
  var i = 0;
  for (final e in out) {
    while (i < markers.length && markers[i].order.compareTo(e.order) < 0) {
      merged.add(markers[i++]);
    }
    merged.add(e);
  }
  while (i < markers.length) {
    merged.add(markers[i++]);
  }
  return merged;
}

/// The message the "New" line is drawn before: the first, by `order`, of
/// those that arrived after the read position [readSeq], leaving out
/// late messages. A late message is unread too, but it sits up among
/// what was already read, so a line before it would put read messages
/// under "New". No line when that first message is the reader's own.
///
/// [entries] are the messages drawn, in display order.
String? firstNewMessageId(
  Iterable<Message> entries,
  int readSeq, {
  String? myId,
}) {
  for (final m in entries) {
    if (m.seq <= readSeq || m.late) continue;
    return m.authorId == myId ? null : m.id;
  }
  return null;
}

/// The text of a quote (FEATURES.md 1.1): the quoted range of the
/// quoted message's current text, or its first 200 characters when no
/// range is given. Null when the message is deleted or the range no
/// longer exists: the client then draws "original message changed".
String? quoteText(Message? quoted, Quote quote) {
  if (quoted == null || quoted.isDeleted) return null;
  final text = quoted.text;
  final s = quote.start, e = quote.end;
  if (s != null && e != null) {
    if (s < 0 || e > text.length || s >= e) return null;
    return text.substring(s, e);
  }
  if (text.isEmpty && quoted.files.isNotEmpty) return quoted.files.first.name;
  return text.length <= 200 ? text : '${text.substring(0, 200)}…';
}

/// The thread view a conversation is drawn in: the per-conversation
/// override if set, otherwise the person's default (FEATURES.md 1.2).
bool isFlatView({
  String? conversationOverride,
  required String personDefault,
}) => (conversationOverride ?? personDefault) == 'flat';

/// Whether a thread was moved to a channel: its last reply is the
/// pointer, a system reply of kind `thread_moved` (ruling R20). Returns
/// the new channel's id, or null.
String? movedTo(Iterable<Message> replies) {
  for (final r in replies) {
    if (r.system?['kind'] == 'thread_moved') {
      return r.system?['conversation_id'] as String?;
    }
  }
  return null;
}
