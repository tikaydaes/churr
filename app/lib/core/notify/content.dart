// What a notification says (docs/PUSH.md section 8, step 5): who, where,
// and the message as plain text up to 300 characters, or a line for what
// it holds instead of text. Plain Dart with no wording: the platform layer
// (lib/platform/notify_wording.dart) turns these values into sentences in
// the reader's language, so the background entry points can use them too.

import '../markdown/nodes.dart';
import '../markdown/parser.dart';
import '../models/message.dart';

/// The longest body a notification carries.
const maxBodyLength = 300;

/// What a notification's body stands for.
enum BodyKind {
  /// The message's text, in [NotificationText.text].
  text,

  /// No text, and a file or several.
  file,

  /// No text, and a poll or another form.
  form,

  /// No text, and a card from a bot.
  card,

  /// A sealed message this device could not open, or previews are off:
  /// the placeholder "New message".
  placeholder,

  /// A test notification from settings.
  test,

  /// The app is locked here: "You have a new notification", and nothing
  /// about whom or where (the lock, docs/PUSH.md section 8).
  locked,
}

/// The values a notification is worded from.
class NotificationText {
  const NotificationText({
    required this.sender,
    required this.kind,
    this.text = '',
    this.channel,
    this.chatTitle,
    this.server,
    this.more = 0,
  });

  /// The author's display name.
  final String sender;

  /// A named channel's name: the title reads "Sam in #ops".
  final String? channel;

  /// An ad-hoc channel's or sealed chat's title, when it is not just the
  /// sender (a group of three or more).
  final String? chatTitle;

  /// The server's name, when more than one server is signed in
  /// (docs/CLIENTS.md section 8.1).
  final String? server;

  final BodyKind kind;
  final String text;

  /// How many more messages are waiting beyond this one (a wake-up).
  final int more;
}

/// The placeholder "New message", titled with the app's name: what a
/// wake-up shows when the real message cannot be fetched or opened.
/// What a wake-up shows while the app is locked: the product name and
/// "You have a new notification", nothing else.
const lockedText = NotificationText(sender: 'Churr', kind: BodyKind.locked);

const placeholderText = NotificationText(
  sender: 'Churr',
  kind: BodyKind.placeholder,
);

/// Builds the values for [m]. [showText] false is the "only the sender"
/// setting (docs/CLIENTS.md 9.12): the body becomes the placeholder.
NotificationText describeMessage(
  Message m, {
  required String sender,
  String? channel,
  String? chatTitle,
  String? server,
  bool showText = true,
  int more = 0,
}) {
  final title = chatTitle == null || chatTitle == sender ? null : chatTitle;
  NotificationText out(BodyKind k, [String text = '']) => NotificationText(
    sender: sender,
    channel: channel,
    chatTitle: title,
    server: server,
    kind: k,
    text: text,
    more: more,
  );
  if (!showText || m.sealed != null) return out(BodyKind.placeholder);
  final plain = plainText(m.text);
  if (plain.isNotEmpty) return out(BodyKind.text, plain);
  if (m.files.isNotEmpty) return out(BodyKind.file);
  if (m.form != null) return out(BodyKind.form);
  if (m.cards.isNotEmpty) {
    final f = m.fallback.trim();
    return f.isEmpty ? out(BodyKind.card) : out(BodyKind.text, _cut(f));
  }
  return out(BodyKind.placeholder);
}

/// Churr Markdown as one line of plain text, up to [maxBodyLength]
/// characters: marks dropped, mentions as `@name`, channels as `#…`,
/// spoilers hidden, blocks joined by spaces.
String plainText(String markdown) {
  if (markdown.trim().isEmpty) return '';
  final b = StringBuffer();
  void inl(List<Inline> xs) {
    for (final x in xs) {
      switch (x.kind) {
        case InlineKind.text || InlineKind.escape || InlineKind.code:
          b.write(x.text);
        case InlineKind.spoiler:
          b.write('•••');
        case InlineKind.mention:
          b.write(x.name.isEmpty ? x.text : '@${x.name}');
        case InlineKind.emoji:
          b.write(x.name.isEmpty ? x.text : ':${x.name}:');
        case InlineKind.lineBreak:
          b.write(' ');
        case InlineKind.bareLink:
          b.write(x.url.isEmpty ? x.text : x.url);
        case InlineKind.image:
          if (x.children.isNotEmpty) {
            inl(x.children);
          } else {
            b.write(x.text);
          }
        default:
          if (x.children.isNotEmpty) {
            inl(x.children);
          } else {
            b.write(x.text);
          }
      }
    }
  }

  void blocks(List<Block> bs) {
    for (final bl in bs) {
      if (b.isNotEmpty) b.write(' ');
      switch (bl.kind) {
        case BlockKind.paragraph || BlockKind.heading:
          inl(bl.inlines);
        case BlockKind.quote:
          blocks(bl.blocks);
        case BlockKind.list:
          for (final it in bl.items) {
            if (b.isNotEmpty) b.write(' ');
            inl(it.inlines);
            blocks(it.lists);
          }
        case BlockKind.codeBlock:
          b.write(bl.code);
        case BlockKind.table:
          for (final c in bl.head) {
            if (b.isNotEmpty) b.write(' ');
            inl(c.inlines);
          }
        case BlockKind.rule:
          break;
      }
    }
  }

  try {
    blocks(parseMarkdown(markdown).blocks);
  } on Object {
    return _cut(markdown);
  }
  return _cut(b.toString());
}

String _cut(String s) {
  final one = s.replaceAll(RegExp(r'\s+'), ' ').trim();
  if (one.runes.length <= maxBodyLength) return one;
  return '${String.fromCharCodes(one.runes.take(maxBodyLength - 1))}…';
}
