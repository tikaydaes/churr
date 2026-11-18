import 'json.dart';

/// Something the server resolved in a message's text (`Entity`,
/// docs/MESSAGE-FORMAT.md section 3). [at] and [len] count UTF-16 code
/// units, which is how Dart indexes strings.
class Entity {
  Entity({
    required this.type,
    required this.at,
    required this.len,
    this.accountId,
    this.groupId,
    this.scope,
    this.conversationId,
    this.emojiId,
    this.url,
    this.itemId,
    this.time,
    this.precision,
    this.zone,
    this.patternId,
    this.value,
    this.messageId,
    this.raw = const {},
  });

  factory Entity.fromJson(Json j) => Entity(
    type: j['type'] as String? ?? '',
    at: asInt(j['at']),
    len: asInt(j['len']),
    accountId: j['account_id'] as String?,
    groupId: j['group_id'] as String?,
    scope: j['scope'] as String?,
    conversationId: j['conversation_id'] as String?,
    emojiId: j['emoji_id'] as String?,
    url: j['url'] as String?,
    itemId: j['item_id'] as String?,
    time: j['time'] as String?,
    precision: j['precision'] as String?,
    zone: j['zone'] as String?,
    patternId: j['pattern_id'] as String?,
    value: j['value'] as String?,
    messageId: j['message_id'] as String?,
    raw: j,
  );

  /// `account`, `group`, `broadcast`, `conversation`, `emoji`, `link` or
  /// `task`. A type this build does not know is drawn as the plain text
  /// it covers (MESSAGE-FORMAT.md section 10), so it is kept as a string.
  final String type;
  final int at;
  final int len;
  final String? accountId;
  final String? groupId;
  final String? scope;
  final String? conversationId;
  final String? emojiId;
  final String? url;
  final String? itemId;

  /// An `instant`'s moment in UTC (RFC 3339), the precision it was typed
  /// with, and its zone as written or the author's (ruling R69).
  final String? time;
  final String? precision;
  final String? zone;

  /// A `reference`'s pattern and the value matched (ruling R73); its
  /// address, as the pattern gave it when the text was written, is [url].
  final String? patternId;
  final String? value;

  /// A `message_link`'s message; its conversation is [conversationId].
  final String? messageId;

  /// The entity as it came, for the fields of the later objects of ruling
  /// R73 (a measurement's unit, a colour's value, …).
  final Json raw;
}

/// The state of one checklist item (`Task`).
class TaskState {
  TaskState({
    required this.id,
    required this.checked,
    this.checkedById,
    this.checkedAt,
  });

  factory TaskState.fromJson(Json j) => TaskState(
    id: j['id'] as String? ?? '',
    checked: j['checked'] == true,
    checkedById: j['checked_by_id'] as String?,
    checkedAt: parseTime(j['checked_at']),
  );

  final String id;
  final bool checked;
  final String? checkedById;
  final DateTime? checkedAt;

  Json toJson() => {
    'id': id,
    'checked': checked,
    if (checkedById != null) 'checked_by_id': checkedById,
    if (checkedAt != null) 'checked_at': formatTime(checkedAt!),
  };
}

/// How a file is drawn (`File.kind`). A kind this build does not know is
/// drawn as [file] (MESSAGE-FORMAT.md section 10).
enum FileKind {
  image,
  video,
  audio,
  text,
  file;

  static FileKind parse(Object? v) =>
      values.firstWhere((k) => k.name == v, orElse: () => file);
}

/// An uploaded file (`File`, MESSAGE-FORMAT.md section 5).
class FileRef {
  FileRef({
    required this.id,
    required this.name,
    required this.size,
    required this.mediaType,
    required this.kind,
    this.width,
    this.height,
    this.durationSeconds,
    this.caption,
    this.previewText,
    this.url,
    this.thumbUrl,
    this.posterUrl,
    this.state,
  });

  factory FileRef.fromJson(Json j) => FileRef(
    id: j['id'] as String? ?? '',
    name: j['name'] as String? ?? '',
    size: asInt(j['size']),
    mediaType: j['media_type'] as String? ?? 'application/octet-stream',
    kind: FileKind.parse(j['kind']),
    width: j['width'] == null ? null : asInt(j['width']),
    height: j['height'] == null ? null : asInt(j['height']),
    durationSeconds: j['duration_seconds'] == null
        ? null
        : asInt(j['duration_seconds']),
    caption: j['caption'] as String?,
    previewText: j['preview_text'] as String?,
    url: j['url'] as String?,
    thumbUrl: j['thumb_url'] as String?,
    posterUrl: j['poster_url'] as String?,
    state: j['state'] as String?,
  );

  final String id;
  final String name;
  final int size;
  final String mediaType;
  final FileKind kind;
  final int? width;
  final int? height;
  final int? durationSeconds;

  /// What a screen reader reads for an image.
  final String? caption;

  /// The first lines of a text file.
  final String? previewText;

  /// Paths on the server, fetched with the same credential as the API.
  /// Only a ready file has a [url].
  final String? url;
  final String? thumbUrl;
  final String? posterUrl;

  /// In a conversation shared with other servers: `ready`, `arriving`,
  /// `unavailable` or `too_large`. Null means ready.
  final String? state;
}

/// A label and value on a card.
class CardField {
  CardField({required this.label, required this.value, this.short = false});

  factory CardField.fromJson(Json j) => CardField(
    label: j['label'] as String? ?? '',
    value: j['value'] as String? ?? '',
    short: j['short'] == true,
  );

  final String label;

  /// Inline Churr Markdown.
  final String value;

  /// Short fields sit two to a row on wide screens.
  final bool short;
}

/// A fixed layout for structured information, posted by bots, webhooks
/// and the server (`Card`, MESSAGE-FORMAT.md section 6). Unknown fields
/// are ignored.
class MessageCard {
  MessageCard({
    this.color,
    this.authorName,
    this.authorIconFileId,
    this.title,
    this.url,
    this.text,
    this.fields = const [],
    this.imageFileId,
    this.footer,
    this.at,
    this.previewUrl,
    this.theme,
  });

  factory MessageCard.fromJson(Json j) => MessageCard(
    color: j['color'] as String?,
    authorName: object(j['author'])?['name'] as String?,
    authorIconFileId: object(j['author'])?['icon_file_id'] as String?,
    title: j['title'] as String?,
    url: j['url'] as String?,
    text: j['text'] as String?,
    fields: listOf(j['fields'], CardField.fromJson),
    imageFileId: j['image_file_id'] as String?,
    footer: j['footer'] as String?,
    at: parseTime(j['at']),
    previewUrl: j['preview_url'] as String?,
    theme: object(j['theme']),
  );

  /// One of `lavender`, `cyan`, `pink`, `yellow`, `green`, `red`, `grey`.
  final String? color;
  final String? authorName;
  final String? authorIconFileId;
  final String? title;

  /// Makes the title a link.
  final String? url;

  /// Churr Markdown, inline constructs and lists only.
  final String? text;
  final List<CardField> fields;

  /// A file on this server. A card never loads anything from another site.
  final String? imageFileId;
  final String? footer;
  final DateTime? at;

  /// Set on a link preview the server added.
  final String? previewUrl;

  /// Set by the server on a theme card (`CardTheme`): a link to one of
  /// its own themes. Drawn with swatches, "Try" and "Add".
  final Json? theme;
}

/// One emoji on a message, with the accounts behind it (`Reaction`).
class Reaction {
  Reaction({
    required this.emoji,
    required this.count,
    required this.me,
    this.accountIds = const [],
  });

  factory Reaction.fromJson(Json j) => Reaction(
    emoji: j['emoji'] as String? ?? '',
    count: asInt(j['count']),
    me: j['me'] == true,
    accountIds: strings(j['account_ids']),
  );

  /// A standard emoji sequence, or `:name:` for a custom one.
  final String emoji;
  final int count;

  /// Whether the reader is one of them.
  final bool me;

  /// At most the first ten.
  final List<String> accountIds;

  Json toJson() => {
    'emoji': emoji,
    'count': count,
    'me': me,
    'account_ids': accountIds,
  };
}

/// Thread information on a top-level message that has replies
/// (`ThreadSummary`).
class ThreadSummary {
  ThreadSummary({
    required this.replyCount,
    required this.lastReplySeq,
    this.lastReplyAt,
    this.participantIds = const [],
  });

  factory ThreadSummary.fromJson(Json j) => ThreadSummary(
    replyCount: asInt(j['reply_count']),
    lastReplySeq: asInt(j['last_reply_seq']),
    lastReplyAt: parseTime(j['last_reply_at']),
    participantIds: strings(j['participant_ids']),
  );

  final int replyCount;
  final int lastReplySeq;
  final DateTime? lastReplyAt;
  final List<String> participantIds;

  Json toJson() => {
    'reply_count': replyCount,
    'last_reply_seq': lastReplySeq,
    if (lastReplyAt != null) 'last_reply_at': formatTime(lastReplyAt!),
    'participant_ids': participantIds,
  };
}

/// What a reply quotes. No range means the whole message.
class Quote {
  Quote({required this.messageId, this.start, this.end, this.text});

  factory Quote.fromJson(Json j) => Quote(
    messageId: j['message_id'] as String? ?? '',
    start: j['start'] == null ? null : asInt(j['start']),
    end: j['end'] == null ? null : asInt(j['end']),
    text: j['text'] as String?,
  );

  final String messageId;
  final int? start;
  final int? end;

  /// What the quote shows, as the server filled it in: how someone who
  /// may not open the quoted message still reads the quote.
  final String? text;

  /// The quote carries its text but the quoted message cannot be had:
  /// a message this reader cannot see (ruling R87).
  bool unseen(Message? quoted) => quoted == null && (text ?? '').isNotEmpty;
}

/// How a message was posted, if not typed by its author (`Via`).
class Via {
  Via({required this.kind, this.name, this.accountId, this.iconFileId});

  factory Via.fromJson(Json j) => Via(
    kind: j['kind'] as String? ?? '',
    name: j['name'] as String?,
    accountId: j['account_id'] as String?,
    iconFileId: j['icon_file_id'] as String?,
  );

  /// `webhook`, `reminder`, `scheduled`, `command`, `import` or `system`.
  final String kind;
  final String? name;
  final String? accountId;
  final String? iconFileId;
}

/// The caller's state for one thread (`ThreadState`). No row means
/// "neither".
class ThreadState {
  ThreadState({
    required this.parentId,
    required this.conversationId,
    required this.state,
    required this.readSeq,
    required this.unreadCount,
  });

  factory ThreadState.fromJson(Json j) => ThreadState(
    parentId: j['parent_id'] as String? ?? '',
    conversationId: j['conversation_id'] as String? ?? '',
    state: j['state'] as String? ?? 'following',
    readSeq: asInt(j['read_seq']),
    unreadCount: asInt(j['unread_count']),
  );

  final String parentId;
  final String conversationId;

  /// `following` or `ignored`.
  final String state;
  final int readSeq;
  final int unreadCount;

  Json toJson() => {
    'parent_id': parentId,
    'conversation_id': conversationId,
    'state': state,
    'read_seq': readSeq,
    'unread_count': unreadCount,
  };
}

/// One post in a conversation, in the shape MESSAGE-FORMAT.md section 1
/// defines.
///
/// The object is immutable. Things beside the content that change without
/// an edit (reactions, ticks, the thread summary, the form's tally) are
/// replaced with [copyWith], which also rewrites [raw] so the stored JSON
/// stays in the API's shape.
class Message {
  Message._(this.raw)
    : id = raw['id'] as String? ?? '',
      conversationId = raw['conversation_id'] as String? ?? '',
      seq = asInt(raw['seq']),
      order = raw['order'] as String? ?? '',
      late = raw['late'] == true,
      unsent = raw['unsent'] == true,
      authorId = raw['author_id'] as String? ?? '',
      clientId = raw['client_id'] as String?,
      parentId = raw['parent_id'] as String?,
      quote = object(raw['quote']) == null
          ? null
          : Quote.fromJson(object(raw['quote'])!),
      text = raw['text'] as String? ?? '',
      entities = listOf(raw['entities'], Entity.fromJson),
      tasks = listOf(raw['tasks'], TaskState.fromJson),
      files = listOf(raw['files'], FileRef.fromJson),
      cards = listOf(raw['cards'], MessageCard.fromJson),
      form = object(raw['form']),
      fallback = raw['fallback'] as String? ?? '',
      reactions = listOf(raw['reactions'], Reaction.fromJson),
      thread = object(raw['thread']) == null
          ? null
          : ThreadSummary.fromJson(object(raw['thread'])!),
      via = object(raw['via']) == null
          ? null
          : Via.fromJson(object(raw['via'])!),
      system = object(raw['system']),
      sealed = object(raw['sealed']),
      pinned = raw['pinned'] == true,
      notifiedAnyway = raw['notified_anyway'] == true,
      hasHistory = raw['has_history'] == true,
      createdAt =
          parseTime(raw['created_at']) ??
          DateTime.fromMillisecondsSinceEpoch(0, isUtc: true),
      editedAt = parseTime(raw['edited_at']),
      deletedAt = parseTime(raw['deleted_at']),
      deletedById = raw['deleted_by_id'] as String?;

  factory Message.fromJson(Json j) => Message._(j);

  /// The object as the server sent it. Unknown fields survive storage
  /// because this, not the typed fields, is what the database keeps.
  final Json raw;

  final String id;
  final String conversationId;

  /// The message's arrival number on this server. Read positions, unread
  /// counts and paging go by it; where the message is shown does not.
  final int seq;

  /// Where the message is shown (docs/EVENTS.md section 8): a
  /// conversation and a thread are sorted by it, compared as plain text.
  /// Opaque: nothing else is read out of it. See [compareOrder].
  final String order;

  /// In a conversation shared with other servers: it arrived after
  /// messages ordered later than it had already been delivered here. It
  /// is shown at its place by [order] all the same.
  final bool late;

  /// In a conversation shared with other servers, on a message of this
  /// server's own: the link has not acknowledged it yet. It is stored and
  /// can be edited, deleted and reacted to; only its look says so.
  final bool unsent;
  final String authorId;

  /// Present only for the author's own connections and in a sealed chat.
  final String? clientId;

  /// The top-level message this replies to. A reply never has replies.
  final String? parentId;
  final Quote? quote;

  /// Churr Markdown, exactly as the author sent it.
  final String text;
  final List<Entity> entities;
  final List<TaskState> tasks;
  final List<FileRef> files;
  final List<MessageCard> cards;

  /// The form, as raw JSON. Typed form models belong with the form
  /// screens; until a build can draw a form it shows [fallback]
  /// (MESSAGE-FORMAT.md section 10).
  final Json? form;

  /// Plain text standing in for the cards and form.
  final String fallback;
  final List<Reaction> reactions;

  /// Set on a top-level message with replies.
  final ThreadSummary? thread;
  final Via? via;

  /// In place of text on a message the server posted itself: `kind` and
  /// fields that depend on it. Kept raw so an unknown kind is drawn as
  /// "Something happened here".
  final Json? system;

  /// A sealed chat's `key_epoch` and `ciphertext`. Decrypting is the
  /// sealed-chat code's work; this layer only carries it.
  final Json? sealed;
  final bool pinned;
  final bool notifiedAnyway;
  final bool hasHistory;
  final DateTime createdAt;
  final DateTime? editedAt;
  final DateTime? deletedAt;
  final String? deletedById;

  /// The text says what the author is doing, typed with /me (ruling R67):
  /// drawn after the author's name.
  bool get action => raw['action'] == true;

  bool get isReply => parentId != null;
  bool get isDeleted => deletedAt != null;
  bool get isSystem => system != null;

  /// Display order: by [order] as plain text, then by [seq] so that two
  /// equal orders (which the server does not give) still sort the same
  /// way every time.
  static int compareOrder(Message a, Message b) {
    final o = a.order.compareTo(b.order);
    return o != 0 ? o : a.seq.compareTo(b.seq);
  }

  /// The state of the checklist item [itemId], or null.
  TaskState? task(String itemId) {
    for (final t in tasks) {
      if (t.id == itemId) return t;
    }
    return null;
  }

  /// A copy with some of the parts that change without an edit replaced.
  /// Pass [clearThread] to drop the thread summary, and `unsent: false`
  /// when the link has acknowledged it (`message.sent`).
  Message copyWith({
    List<Reaction>? reactions,
    List<TaskState>? tasks,
    ThreadSummary? thread,
    bool clearThread = false,
    Json? form,
    bool? pinned,
    bool? unsent,
  }) {
    final j = {...raw};
    if (reactions != null) {
      j['reactions'] = [for (final r in reactions) r.toJson()];
    }
    if (tasks != null) j['tasks'] = [for (final t in tasks) t.toJson()];
    if (thread != null) j['thread'] = thread.toJson();
    if (clearThread) j.remove('thread');
    if (form != null) j['form'] = form;
    if (pinned != null) j['pinned'] = pinned;
    if (unsent == false) j.remove('unsent');
    if (unsent == true) j['unsent'] = true;
    return Message._(j);
  }

  Json toJson() => raw;
}
