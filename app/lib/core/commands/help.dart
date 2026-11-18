// What /help shows (rulings R67 and R74), worked out from the one source,
// testdata/commands/help.json (here as help_text.dart, written by
// tools/help-gen/gen.py), the command table and the registries: the full
// entry of a command, and the section on the things people can type.
// Plain Dart, tested without Flutter. The web chat's copy of this logic is
// internal/web/static/shared/help.js, held to the same source.

import 'dart:convert';

import '../api/conversation_ops.dart' show SlashCommand;
import '../objects/units.dart' show measureUnits;
import '../models/json.dart';
import 'commands.dart';
import 'decks.dart';
import 'help_text.dart';

/// help.json, read once.
final Json helpData = jsonDecode(helpSource) as Json;

Json get _commands => helpData['commands'] as Json;

/// A label of help.json, with {name} and the like filled in.
String helpWords(String key, [Map<String, Object> vars = const {}]) =>
    ((helpData['labels'] as Json)[key] as String? ?? '').replaceAllMapped(
      RegExp(r'\{(\w+)\}'),
      (m) => '${vars[m[1]] ?? ''}',
    );

/// The command a name stands for: itself, or the command it is another
/// name of (/part is /leave, /kb is /ban).
String helpCanonical(String name) {
  final n = name.toLowerCase().replaceFirst(RegExp('^/'), '');
  final hidden = hiddenCommandAliases[n];
  if (hidden != null) return hidden;
  final e = _commands[n] as Json?;
  return e?['same_as'] as String? ?? n;
}

/// What a typed word names among the commands listed here for this person
/// (the server's list leaves out what is hidden for them): a command, or
/// a built-in by another of its names. Null if none, and the word is a
/// search.
SlashCommand? helpFind(List<SlashCommand> listed, String word) {
  final n = word.trim().toLowerCase().replaceFirst(RegExp('^/'), '');
  if (n.isEmpty || RegExp(r'\s').hasMatch(n)) return null;
  for (final c in listed) {
    final e = _commands[n] as Json?;
    if (c.name == n &&
        (c.kind != 'builtin' || e == null || e['same_as'] == null)) {
      return c;
    }
  }
  if (!isBuiltinCommand(n) && !_commands.containsKey(n)) return null;
  final to = helpCanonical(n);
  for (final c in listed) {
    if (c.kind == 'builtin' && c.name == to) return c;
  }
  return null;
}

/// An argument of a command, explained.
typedef HelpArg = ({String name, String about});

/// An example: what is typed, and what happens.
typedef HelpExample = ({String type, String then});

/// A row of a list drawn from a registry: a deck, a spread, the mahjong
/// set. A spread's [about] is filled in by the screen, in its words.
typedef HelpMoreRow = ({
  String name,
  List<String> aliases,
  String count,
  String about,
  List<String> positions,
});

/// One command's full entry.
class HelpEntry {
  HelpEntry({
    required this.name,
    required this.kind,
    required this.summary,
    required this.usage,
    required this.args,
    required this.description,
    required this.examples,
    required this.aliases,
    required this.where,
    required this.who,
    required this.notes,
    required this.related,
    required this.moreKind,
    required this.more,
  });

  final String name, kind, summary, usage, description, where, who;
  final List<HelpArg> args;
  final List<HelpExample> examples;
  final List<String> aliases, notes, related;

  /// decks, spreads or mahjong, or null.
  final String? moreKind;
  final List<HelpMoreRow> more;
}

/// The full entry for a listed command. Related commands are only those
/// listed here, so a command hidden from this person is never named to
/// them.
HelpEntry helpEntry(SlashCommand c, List<SlashCommand> listed) {
  final here = {for (final x in listed) x.name};
  if (c.kind == 'module') {
    // A module's command: its help comes from the module, through the
    // server, with the names commands have here written in.
    final e = c.raw['help'] as Json? ?? const {};
    return HelpEntry(
      name: c.name,
      kind: c.kind,
      summary: c.description.isEmpty
          ? helpWords('noDescription')
          : c.description,
      usage: '/${c.name}${(c.hint ?? '').isEmpty ? '' : ' ${c.hint}'}',
      args: [
        for (final a in (e['args'] as List? ?? const []).cast<Json>())
          (name: a['name'] as String, about: a['about'] as String),
      ],
      description: e['description'] as String? ?? '',
      examples: [
        for (final x in (e['examples'] as List? ?? const []).cast<Json>())
          (type: x['type'] as String, then: x['then'] as String),
      ],
      aliases: const [],
      where: e['where'] as String? ?? '',
      who: e['who'] as String? ?? '',
      notes: const [],
      related: const [],
      moreKind: null,
      more: const [],
    );
  }
  if (c.kind != 'builtin') {
    return HelpEntry(
      name: c.name,
      kind: c.kind,
      summary: c.description.isEmpty
          ? helpWords('noDescription')
          : c.description,
      usage: '/${c.name}${(c.hint ?? '').isEmpty ? '' : ' ${c.hint}'}',
      args: const [],
      description: helpWords(c.kind == 'bot' ? 'botCommand' : 'savedReply'),
      examples: const [],
      aliases: const [],
      where: '',
      who: '',
      notes: [
        if (isBuiltinCommand(c.name))
          helpWords('replacesBuiltin', {'name': c.name}),
      ],
      related: const [],
      moreKind: null,
      more: const [],
    );
  }
  final name = helpCanonical(c.name);
  final e = _commands[name] as Json? ?? const {};
  final row = builtinCommands[name];
  String join(List<String> parts) => parts.where((p) => p.isNotEmpty).join(' ');
  final moreKind = e['more'] as String?;
  return HelpEntry(
    name: name,
    kind: 'builtin',
    summary: c.description,
    usage: '/$name${(c.hint ?? '').isEmpty ? '' : ' ${c.hint}'}',
    args: [
      for (final a in (e['args'] as List? ?? const []).cast<Json>())
        (name: a['name'] as String, about: a['about'] as String),
    ],
    description: e['description'] as String? ?? '',
    examples: [
      for (final x in (e['examples'] as List? ?? const []).cast<Json>())
        (type: x['type'] as String, then: x['then'] as String),
    ],
    aliases: [for (final a in commandAliasesOf(name)) '/$a'],
    where: join([
      e['where'] as String? ?? '',
      helpWords((row?.$2 ?? false) ? 'sealedYes' : 'sealedNo'),
    ]),
    who: join([
      e['who'] as String? ?? '',
      if (row?.$3 ?? false) helpWords('adminOnly'),
    ]),
    notes: [if (replaceableCommands.contains(name)) helpWords('replaceable')],
    related: [
      for (final r in (e['related'] as List? ?? const []).cast<String>())
        if (here.contains(r)) r,
    ],
    moreKind: moreKind,
    more: helpMore(moreKind),
  );
}

/// The list a command's entry draws from a registry.
List<HelpMoreRow> helpMore(String? kind) {
  final decks = helpData['decks'] as Json;
  HelpMoreRow deck(String k) => (
    name: k,
    aliases: cardDecks[k]!.aliases,
    count: helpWords(k == 'mahjong' ? 'deckTiles' : 'deckCards', {
      'n': cardDecks[k]!.cards.length,
    }),
    about: decks[k] as String? ?? '',
    positions: const [],
  );
  return switch (kind) {
    'decks' => [for (final k in cardDecks.keys) deck(k)],
    'mahjong' => [deck('mahjong')],
    'spreads' => [
      for (final e in tarotSpreads.entries)
        (
          name: e.key,
          aliases: e.value.aliases,
          count: helpWords('spreadCards', {'n': e.value.positions.length}),
          about: '',
          positions: e.value.positions,
        ),
    ],
    _ => const [],
  };
}

/// A row of "Things you can type", with what only this conversation knows
/// filled in where help.json says live.
class HelpThing {
  HelpThing(this.raw, {this.patterns, this.units});

  final Json raw;
  String get id => raw['id'] as String;
  String get title => raw['title'] as String? ?? '';
  String get type => raw['type'] as String;
  String get becomes => raw['becomes'] as String;
  String get popup => raw['popup'] as String? ?? '';
  String get sealed => raw['sealed'] as String? ?? '';
  HelpExample? get example {
    final x = raw['example'] as Json?;
    return x == null
        ? null
        : (type: x['type'] as String, then: x['then'] as String);
  }

  List<String> get covers => (raw['covers'] as List).cast<String>();

  final HelpPatterns? patterns;
  final List<HelpUnitKind>? units;

  /// Whether it answers a search.
  bool matches(String query) {
    final q = query.trim().toLowerCase();
    return q.isEmpty ||
        [title, type, becomes].any((s) => s.toLowerCase().contains(q));
  }
}

/// A reference pattern as /help shows it.
typedef HelpPattern = ({
  String name,
  String prefix,
  String example,
  String host,
});

/// The reference patterns that apply here.
typedef HelpPatterns = ({
  List<HelpPattern> here,
  List<HelpPattern> server,
  bool none,
  bool sealed,
});

/// A kind of measurement and its units, each with every way of typing it.
typedef HelpUnitKind = ({
  String title,
  List<({String code, List<String> typed})> units,
});

/// The section on what people can type. [server] and [here] are the
/// reference patterns of the server and of this channel.
List<HelpThing> helpThings({
  bool sealed = false,
  List<Json> server = const [],
  List<Json> here = const [],
}) => [
  for (final t in (helpData['things'] as List).cast<Json>())
    switch (t['live']) {
      'patterns' => HelpThing(
        t,
        patterns: helpPatterns(server: server, here: here, sealed: sealed),
      ),
      'units' => HelpThing(t, units: helpUnits()),
      _ => HelpThing(t),
    },
];

/// The formatting rows.
List<HelpThing> get helpMarkdown => [
  for (final t in (helpData['markdown'] as List).cast<Json>()) HelpThing(t),
];

/// The list's two rules.
List<String> get helpPopupRules =>
    (helpData['popup_rules'] as List).cast<String>();

/// What the references row says about the patterns that apply here.
HelpPatterns helpPatterns({
  List<Json> server = const [],
  List<Json> here = const [],
  bool sealed = false,
}) {
  HelpPattern show(Json p) {
    final value = patternExample(p['expression'] as String? ?? '');
    final url = (p['url_template'] as String? ?? '').replaceAll(
      '{value}',
      value,
    );
    final host = Uri.tryParse(url)?.host ?? '';
    return (
      name: p['name'] as String? ?? '',
      prefix: p['prefix'] as String? ?? '',
      example: '${p['prefix'] ?? ''}$value',
      host: host.isEmpty ? url : host,
    );
  }

  final s = [for (final p in server) show(p)];
  final h = sealed ? <HelpPattern>[] : [for (final p in here) show(p)];
  return (here: h, server: s, none: s.isEmpty && h.isEmpty, sealed: sealed);
}

/// The measurement table's units sorted into kinds.
List<HelpUnitKind> helpUnits() => [
  for (final k in (helpData['measure_kinds'] as List).cast<Json>())
    (
      title: k['title'] as String,
      units: [
        for (final code in (k['units'] as List).cast<String>())
          (
            code: code,
            typed: [
              for (final u in measureUnits)
                if (u.$2 == code) u.$1,
            ],
          ),
      ],
    ),
];

/// A value a reference pattern's expression matches, for its example
/// (docs/MESSAGE-FORMAT.md 3.2, the restricted form): each part once, or
/// three times for + and *, or its count; digits counting from 1, letters
/// from a, a class from its first member (help.json, pattern_examples).
String patternExample(String expression) {
  if (expression.isEmpty) return '123';
  final out = StringBuffer();
  var i = 0;
  final repeat = RegExp(r'^(?:([?+*])|\{(\d+)(?:,(\d+))?\})');
  while (i < expression.length) {
    String pool;
    if (expression[i] == r'\') {
      final c = i + 1 < expression.length ? expression[i + 1] : '';
      pool = switch (c) {
        'd' => '1234567890',
        'w' => 'abcdefghij',
        _ => c,
      };
      i += 2;
    } else if (expression[i] == '[') {
      final end = expression.indexOf(']', i);
      pool = _classMembers(expression.substring(i + 1, end));
      i = end + 1;
    } else {
      pool = expression[i];
      i += 1;
    }
    var n = 1;
    final r = repeat.firstMatch(expression.substring(i));
    if (r != null) {
      if (r[1] != null) {
        n = r[1] == '?' ? 1 : 3;
      } else {
        n = int.parse(r[2]!);
        if (n < 1) n = 1;
      }
      i += r[0]!.length;
    }
    for (var k = 0; k < n; k++) {
      out.write(pool[k % pool.length]);
    }
  }
  return out.toString();
}

/// The characters a class stands for, in order: a-z for a range, \- for
/// a hyphen itself.
String _classMembers(String body) {
  final toks = <(String, bool)>[];
  for (var i = 0; i < body.length; i++) {
    if (body[i] == r'\' && i + 1 < body.length) {
      toks.add((body[i + 1], true));
      i++;
    } else {
      toks.add((body[i], false));
    }
  }
  final out = StringBuffer();
  for (var i = 0; i < toks.length; i++) {
    final dash = i + 1 < toks.length ? toks[i + 1] : null;
    if (dash != null && dash.$1 == '-' && !dash.$2 && i + 2 < toks.length) {
      for (
        var c = toks[i].$1.codeUnitAt(0);
        c <= toks[i + 2].$1.codeUnitAt(0);
        c++
      ) {
        out.writeCharCode(c);
      }
      i += 2;
    } else {
      out.write(toks[i].$1);
    }
  }
  return out.toString();
}
