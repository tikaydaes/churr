import 'dart:convert';

/// One standard emoji: its characters and its short code.
class StandardEmoji {
  const StandardEmoji(this.char, this.code);
  final String char;
  final String code;
}

/// A group of standard emoji, as Unicode groups them.
class EmojiGroup {
  const EmojiGroup(this.name, this.emoji);
  final String name;
  final List<StandardEmoji> emoji;
}

/// The standard emoji and their short codes: `assets/emoji.json`, the
/// same file the web chat uses, made from Unicode's emoji-test.txt.
/// Shape: `{"groups": [{"name", "emoji": [[char, code], …]}], "aliases":
/// {code: char}}`; aliases are the familiar short names (`+1`,
/// `thumbsup`, `joy`).
class EmojiData {
  EmojiData(this.groups, this.aliases)
    : _byCode = {
        for (final g in groups)
          for (final e in g.emoji) e.code: e.char,
        ...aliases,
      },
      _codeOf = {
        for (final g in groups)
          for (final e in g.emoji) e.char: e.code,
      };

  factory EmojiData.parse(String json) {
    final j = jsonDecode(json) as Map<String, dynamic>;
    return EmojiData(
      [
        for (final g in j['groups'] as List? ?? const [])
          EmojiGroup((g as Map)['name'] as String? ?? '', [
            for (final e in g['emoji'] as List? ?? const [])
              if (e is List && e.length >= 2)
                StandardEmoji(e[0] as String, e[1] as String),
          ]),
      ],
      {
        for (final e in (j['aliases'] as Map? ?? const {}).entries)
          e.key as String: e.value as String,
      },
    );
  }

  static final empty = EmojiData(const [], const {});

  final List<EmojiGroup> groups;
  final Map<String, String> aliases;
  final Map<String, String> _byCode;
  final Map<String, String> _codeOf;

  /// The characters for a short code, or null.
  String? byCode(String code) => _byCode[code];

  /// The short code of an emoji's characters, or null.
  String? codeOf(String char) => _codeOf[char];

  /// Emoji whose short code contains [query]: aliases and codes that
  /// start with it first. At most [limit].
  List<StandardEmoji> search(String query, {int limit = 60}) {
    final q = query
        .toLowerCase()
        .replaceAll(RegExp(r'^:|:$'), '')
        .replaceAll(RegExp(r'\s+'), '_');
    if (q.isEmpty) return const [];
    final starts = <StandardEmoji>[], contains = <StandardEmoji>[];
    final seen = <String>{};
    void consider(String code, String char) {
      if (seen.contains(char)) return;
      if (code.startsWith(q)) {
        seen.add(char);
        starts.add(StandardEmoji(char, code));
      } else if (code.contains(q)) {
        seen.add(char);
        contains.add(StandardEmoji(char, code));
      }
    }

    aliases.forEach(consider);
    for (final g in groups) {
      for (final e in g.emoji) {
        consider(e.code, e.char);
      }
    }
    return [...starts, ...contains].take(limit).toList();
  }
}
