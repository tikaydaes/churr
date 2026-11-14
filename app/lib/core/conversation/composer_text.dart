import '../models/conversation.dart';
import '../models/message.dart';

/// The text rules of the composer and of copying a message
/// (docs/CLIENTS.md 9.5 and 9.26). Plain Dart, so they are tested
/// without widgets.

/// A channel identifier as written in a link: `<#uuid>`.
final channelLinkPattern = RegExp(
  r'<#([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})>',
);

/// Channel link chips inside the composer's text.
///
/// While being edited, each chip is one character of the Unicode private
/// use area, so the cursor steps over it as one unit and one backspace
/// removes it. Each chip gets its own character, mapped to the channel's
/// identifier here. The text that is sent, stored as a draft or put in
/// an edit has `<#conversation-id>` in its place (ruling R52).
class ChannelChips {
  static const _first = 0xE000;
  static const _last = 0xF8FF;

  final _ids = <int, String>{};
  int _next = _first;

  /// Whether [code] is a chip character.
  static bool isChip(int code) => code >= _first && code <= _last;

  /// The channel a chip character stands for, or null.
  String? idOf(int code) => _ids[code];

  /// A new chip character for [conversationId].
  String chipFor(String conversationId) {
    if (_next > _last) _next = _first;
    final code = _next++;
    _ids[code] = conversationId;
    return String.fromCharCode(code);
  }

  /// The editing form of [wire]: each `<#id>` naming a channel for which
  /// [visible] is true becomes a chip; anything else stays as written.
  String encode(String wire, bool Function(String id) visible) =>
      wire.replaceAllMapped(channelLinkPattern, (m) {
        final id = m.group(1)!.toLowerCase();
        return visible(id) ? chipFor(id) : m.group(0)!;
      });

  /// The form that is sent: each chip becomes `<#id>`. A chip character
  /// this composer did not make (pasted from elsewhere) is dropped.
  String decode(String text) {
    final out = StringBuffer();
    for (final unit in text.codeUnits) {
      if (isChip(unit)) {
        final id = _ids[unit];
        if (id != null) out.write('<#$id>');
      } else {
        out.writeCharCode(unit);
      }
    }
    return out.toString();
  }
}

/// What the person is typing at the cursor that opens a picker.
enum TriggerKind { mention, channel, emoji, command }

class Trigger {
  const Trigger(this.kind, this.start, this.end, this.query);
  final TriggerKind kind;

  /// The range in the text the picker's choice replaces: from the
  /// trigger character up to the cursor.
  final int start;
  final int end;

  /// What was typed after the trigger character.
  final String query;

  @override
  String toString() => 'Trigger($kind, $start, $end, "$query")';
}

bool _isSpace(int c) => c == 0x20 || c == 0x0A || c == 0x09 || c == 0x0D;

/// The trigger the cursor at [cursor] is in, if any.
///
/// - `/name` at the very start of the text: a command.
/// - `@` at the start of a word: a mention. A second `@` is allowed, for
///   `@name@server.name`.
/// - `:` at the start of a word followed by two or more short-code
///   characters: an emoji.
/// - `#` at the start of a word: a channel. Spaces are allowed after it,
///   so `#design rev` is one query; it ends at a line break and after 64
///   characters. Whether a picker opens is decided by whether anything
///   matches (9.26).
Trigger? findTrigger(String text, int cursor) {
  if (cursor < 0 || cursor > text.length) return null;
  // Commands: the text so far is `/word`.
  if (text.startsWith('/')) {
    final word = text.substring(1, cursor.clamp(1, text.length));
    if (cursor >= 1 && RegExp(r'^[a-z0-9_-]*$').hasMatch(word)) {
      return Trigger(TriggerKind.command, 0, cursor, word);
    }
  }
  // Mentions and emoji: back to the start of the word.
  var i = cursor;
  while (i > 0 && !_isSpace(text.codeUnitAt(i - 1))) {
    i--;
  }
  final word = text.substring(i, cursor);
  if (word.startsWith('@')) {
    final q = word.substring(1);
    if (RegExp(r'^[^\s@]*(@[^\s@]*)?$').hasMatch(q)) {
      return Trigger(TriggerKind.mention, i, cursor, q);
    }
  }
  if (word.startsWith(':')) {
    final q = word.substring(1);
    if (q.length >= 2 && RegExp(r'^[a-z0-9_+\-]+$').hasMatch(q)) {
      return Trigger(TriggerKind.emoji, i, cursor, q);
    }
  }
  // Channels: the nearest `#` on this line that starts a word.
  for (var j = cursor - 1; j >= 0 && cursor - j <= 65; j--) {
    final c = text.codeUnitAt(j);
    if (c == 0x0A) break;
    if (c == 0x23 /* # */ && (j == 0 || _isSpace(text.codeUnitAt(j - 1)))) {
      final q = text.substring(j + 1, cursor);
      if (q.isEmpty || _isSpace(q.codeUnitAt(0))) return null;
      return Trigger(TriggerKind.channel, j, cursor, q);
    }
  }
  return null;
}

const _accents = {
  'à': 'a',
  'á': 'a',
  'â': 'a',
  'ã': 'a',
  'ä': 'a',
  'å': 'a',
  'ā': 'a',
  'ă': 'a',
  'ą': 'a',
  'ç': 'c',
  'ć': 'c',
  'č': 'c',
  'ď': 'd',
  'đ': 'd',
  'è': 'e',
  'é': 'e',
  'ê': 'e',
  'ë': 'e',
  'ē': 'e',
  'ė': 'e',
  'ę': 'e',
  'ě': 'e',
  'ğ': 'g',
  'ì': 'i',
  'í': 'i',
  'î': 'i',
  'ï': 'i',
  'ī': 'i',
  'į': 'i',
  'ı': 'i',
  'ł': 'l',
  'ñ': 'n',
  'ń': 'n',
  'ň': 'n',
  'ò': 'o',
  'ó': 'o',
  'ô': 'o',
  'õ': 'o',
  'ö': 'o',
  'ø': 'o',
  'ō': 'o',
  'ő': 'o',
  'ř': 'r',
  'ś': 's',
  'š': 's',
  'ş': 's',
  'ß': 'ss',
  'ť': 't',
  'ţ': 't',
  'ù': 'u',
  'ú': 'u',
  'û': 'u',
  'ü': 'u',
  'ū': 'u',
  'ů': 'u',
  'ű': 'u',
  'ų': 'u',
  'ý': 'y',
  'ÿ': 'y',
  'ź': 'z',
  'ż': 'z',
  'ž': 'z',
  'æ': 'ae',
  'œ': 'oe',
};

/// Lower-cases and takes accents off Latin letters, for matching names
/// ("design" finds "Design reviews", "creme" finds "Crème").
String foldForMatch(String s) {
  final lower = s.toLowerCase();
  final out = StringBuffer();
  for (final r in lower.runes) {
    final ch = String.fromCharCode(r);
    out.write(_accents[ch] ?? ch);
  }
  return out.toString();
}

/// Whether [query] matches the start of a word of [name], ignoring case
/// and accents. The query may hold spaces and run across words.
bool matchesWordStart(String name, String query) {
  final n = foldForMatch(name);
  final q = foldForMatch(query);
  if (q.isEmpty) return true;
  for (var i = 0; i < n.length; i++) {
    final starts =
        i == 0 || !RegExp(r'[\p{L}\p{N}]', unicode: true).hasMatch(n[i - 1]);
    if (starts && n.startsWith(q, i)) return true;
  }
  return false;
}

/// The channels the picker offers for [query] (9.26): named channels
/// whose name, or whose parent's name, matches the start of a word;
/// those the person is in first, then by most recent activity.
List<Conversation> matchChannels(
  Iterable<Conversation> visible,
  String query, {
  Conversation? Function(String id)? parentOf,
}) {
  final found = <Conversation>[
    for (final c in visible)
      if (c.isNamed &&
          (matchesWordStart(c.name!, query) ||
              (c.parentId != null &&
                  matchesWordStart(
                    '${parentOf?.call(c.parentId!)?.name ?? ''} ${c.name}',
                    query,
                  ))))
        c,
  ];
  int rank(Conversation c) => c.membership == null ? 1 : 0;
  found.sort((a, b) {
    final r = rank(a).compareTo(rank(b));
    if (r != 0) return r;
    final at = a.lastMessageAt?.millisecondsSinceEpoch ?? 0;
    final bt = b.lastMessageAt?.millisecondsSinceEpoch ?? 0;
    return bt.compareTo(at);
  });
  return found;
}

const _months = [
  'January',
  'February',
  'March',
  'April',
  'May',
  'June',
  'July',
  'August',
  'September',
  'October',
  'November',
  'December',
];

/// The detail line that tells same-named channels apart (9.26): the
/// server when more than one is signed in, the parent, the section, the
/// member count and whether the person is in it, up to two managers,
/// and when it was made.
String channelDetailLine(
  Conversation c, {
  String? serverName,
  String? parentName,
  String? sectionName,
  List<String> managerNames = const [],
}) {
  final parts = <String>[
    ?serverName,
    if (parentName != null) '$parentName ›',
    if (sectionName != null && c.parentId == null) sectionName,
    '${c.memberCount} ${c.memberCount == 1 ? 'member' : 'members'}'
        '${c.membership != null ? ", you're in it" : ''}',
    if (managerNames.isNotEmpty)
      'managed by ${managerNames.take(2).join(', ')}',
    if (c.createdAt != null)
      'since ${_months[c.createdAt!.toLocal().month - 1]} ${c.createdAt!.toLocal().year}',
  ];
  return parts.join(' · ');
}

/// A message's text as plain text for the clipboard (9.26): each channel
/// link becomes the channel's current name, or "private channel" or
/// "unknown channel"; identifiers never reach the clipboard this way.
String copyAsText(Message m, Conversation? Function(String id) conversation) {
  final text = m.text;
  final byAt = {
    for (final e in m.entities)
      if (e.type == 'conversation') e.at: e,
  };
  return text.replaceAllMapped(channelLinkPattern, (match) {
    final e = byAt[match.start];
    if (e == null) return 'unknown channel';
    final c = conversation(e.conversationId ?? match.group(1)!);
    if (c == null || c.name == null) return 'private channel';
    return '#${c.name}';
  });
}

/// What a formatting button or shortcut does to the text.
enum Format {
  bold,
  italic,
  strike,
  code,
  link,
  bullets,
  numbers,
  checklist,
  quote,
  codeBlock,
}

/// Applies [f] to [text] with the selection [start]..[end]. Returns the
/// new text and selection.
(String, int, int) applyFormat(String text, int start, int end, Format f) {
  if (start < 0 || end < 0) start = end = text.length;
  if (start > end) (start, end) = (end, start);
  final sel = text.substring(start, end);
  (String, int, int) wrap(String l, String r, [String empty = '']) {
    final inner = sel.isEmpty ? empty : sel;
    final out = text.replaceRange(start, end, '$l$inner$r');
    return (out, start + l.length, start + l.length + inner.length);
  }

  switch (f) {
    case Format.bold:
      return wrap('**', '**');
    case Format.italic:
      return wrap('_', '_');
    case Format.strike:
      return wrap('~~', '~~');
    case Format.code:
      return wrap('`', '`');
    case Format.link:
      final label = sel.isEmpty ? 'text' : sel;
      final out = text.replaceRange(start, end, '[$label](https://)');
      final at = start + label.length + 3;
      return (out, at, at + 8);
    case Format.codeBlock:
      final before = start == 0 || text[start - 1] == '\n' ? '' : '\n';
      return wrap('$before```\n', '\n```');
    case Format.bullets || Format.numbers || Format.checklist || Format.quote:
      // Each selected line gets the prefix.
      final lineStart = text.lastIndexOf('\n', start == 0 ? 0 : start - 1) + 1;
      final lineStartFixed = start == 0 ? 0 : lineStart;
      var lineEnd = text.indexOf('\n', end);
      if (lineEnd < 0) lineEnd = text.length;
      final lines = text.substring(lineStartFixed, lineEnd).split('\n');
      var n = 1;
      final prefixed = [
        for (final l in lines)
          switch (f) {
            Format.bullets => '- $l',
            Format.numbers => '${n++}. $l',
            Format.checklist => '- [ ] $l',
            _ => '> $l',
          },
      ].join('\n');
      final out = text.replaceRange(lineStartFixed, lineEnd, prefixed);
      return (
        out,
        lineStartFixed + prefixed.length,
        lineStartFixed + prefixed.length,
      );
  }
}
