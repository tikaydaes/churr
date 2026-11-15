// Broad mentions: `@channel`, `@here` and `@everyone` (ruling R64;
// docs/FEATURES.md section 2.5, docs/CLIENTS.md 9.5).
//
// In a large channel only its managers and the admins may use them.
// Anyone else's is posted as plain text and notifies nobody, and the
// composer says so before sending. Those who may are asked to confirm when
// a great many people would be notified. Whether someone may, and how many
// people a mention reaches, is the server's to say: this file only finds
// the words and decides what to ask. Plain Dart: the composer words it.

import '../markdown/nodes.dart';
import '../markdown/parser.dart';
import '../models/conversation.dart';
import '../models/json.dart';

const _broad = ['channel', 'here', 'everyone'];

/// The broad mentions written in [text], each once, in the order they
/// first appear. A word inside code is not a mention, and neither is
/// `@here@server.name`.
List<String> broadMentions(String text) {
  if (!text.contains('@')) return const [];
  final found = <String>[];
  void inlines(List<Inline> spans) {
    for (final s in spans) {
      if (s.kind == InlineKind.mention &&
          s.server.isEmpty &&
          _broad.contains(s.name) &&
          !found.contains(s.name)) {
        found.add(s.name);
      }
      inlines(s.children);
    }
  }

  void blocks(List<Block> list) {
    for (final b in list) {
      inlines(b.inlines);
      blocks(b.blocks);
      for (final item in b.items) {
        inlines(item.inlines);
        blocks(item.lists);
      }
      for (final cell in b.head) {
        inlines(cell.inlines);
      }
      for (final row in b.rows) {
        for (final cell in row) {
          inlines(cell.inlines);
        }
      }
    }
  }

  blocks(parseMarkdown(text).blocks);
  return found;
}

/// Whether the composer warns that [text] will go as plain text: it holds
/// a broad mention and [c] says its reader may not use one. A conversation
/// that does not say (it has only just been joined, or an event replaced
/// it) gives no warning: the server answers when asked. The server cannot
/// read a sealed chat, so there is nothing to say there.
bool broadGoesPlain(String text, Conversation? c) =>
    c != null &&
    !c.isSealed &&
    c.broadMentionsAllowed == false &&
    broadMentions(text).isNotEmpty;

/// What the server says the broad mentions of a text would do
/// (`checkBroadMentions`).
class BroadCheck {
  const BroadCheck({
    required this.broad,
    required this.allowed,
    required this.recipients,
    required this.confirm,
  });

  factory BroadCheck.fromJson(Json j) => BroadCheck(
    broad: j['broad'] == true,
    allowed: j['allowed'] == true,
    recipients: asInt(j['recipients']),
    confirm: j['confirm'] == true,
  );

  /// The text holds a broad mention, outside code.
  final bool broad;

  /// The author may use it here.
  final bool allowed;

  /// How many people it would notify, and whether that is enough to ask
  /// the author first.
  final int recipients;
  final bool confirm;
}

/// Whether a text may go as it is. [ask] asks the server what the text's
/// broad mentions would do and [confirm] puts "This will notify N people"
/// to the person, answering true to send. The answer is true to send and
/// false when the person chose not to.
///
/// Nothing is asked for a text with no broad mention, in a sealed chat, or
/// where the conversation already says no: that text goes as plain text
/// and the warning has said so. If the server cannot be asked (the device
/// is offline) the message is sent all the same, since the post decides.
Future<bool> broadBeforeSend(
  String text,
  Conversation? c, {
  required Future<BroadCheck> Function(String text) ask,
  required Future<bool?> Function(int recipients) confirm,
}) async {
  if (c == null || c.isSealed) return true;
  if (broadMentions(text).isEmpty) return true;
  if (c.broadMentionsAllowed == false) return true;
  final BroadCheck out;
  try {
    out = await ask(text);
  } on Exception {
    return true;
  }
  if (!out.broad || !out.allowed || !out.confirm) return true;
  return await confirm(out.recipients) == true;
}

/// A conversation as an event sent to everyone carries it, with what only
/// this reader is told kept from the copy [held]: the membership, and
/// whether they may use broad mentions. That last answer is dropped when
/// the channel's own rule about them has changed, so that it is asked for
/// again and not shown out of date.
Json keepOwn(Json fresh, Conversation? held) {
  if (held == null) return fresh;
  final out = {...fresh};
  if (!out.containsKey('membership') && held.raw['membership'] != null) {
    out['membership'] = held.toJson()['membership'];
  }
  if (!out.containsKey('broad_mentions_allowed') &&
      held.raw.containsKey('broad_mentions_allowed') &&
      out['broad_mentions'] == held.raw['broad_mentions']) {
    out['broad_mentions_allowed'] = held.raw['broad_mentions_allowed'];
  }
  return out;
}
