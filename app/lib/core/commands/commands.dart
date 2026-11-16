// The built-in slash commands (docs/FEATURES.md sections 12.3 and 12.4,
// rulings R67 and R68): the table every client holds and the one grammar
// they read typed text by. testdata/commands/commands.json is the table and
// testdata/commands/parse.json the cases this file is held to
// (test/core/commands_test.dart). The web chat's copy is
// internal/web/static/shared/commands.js.

import 'decks.dart';

/// The server-wide ban's two commands (ruling R72). Their names are the
/// owner's to change; everything else reaches them by these two constants.
const serverBanCommand = 'exile';
const serverUnbanCommand = 'unexile';

/// name: (args, offered in a sealed chat, offered to admins only). The order
/// is the menu's.
const builtinCommands = <String, (String, bool, bool)>{
  'poll': ('form', false, false),
  'rsvp': ('form', false, false),
  'signup': ('form', false, false),
  'approve': ('form', false, false),
  'form': ('none', false, false),
  'remind': ('raw', true, false),
  'watch': ('person', true, false),
  'unwatch': ('person', true, false),
  'me': ('text', true, false),
  'shrug': ('text_optional', true, false),
  'tableflip': ('text_optional', true, false),
  'unflip': ('text_optional', true, false),
  'spoiler': ('text', true, false),
  'roll': ('dice', false, false),
  'flip': ('none', false, false),
  'pick': ('options', false, false),
  'shuffle': ('options', false, false),
  'draw': ('draw', false, false),
  'tarot': ('spread', false, false),
  'mahjongg': ('count', false, false),
  'mahjong': ('count', false, false),
  'join': ('channel', true, false),
  'leave': ('channel_optional', false, false),
  'part': ('channel_optional', false, false),
  'msg': ('person_text', true, false),
  'topic': ('text_optional', false, false),
  'invite': ('person_channel', false, false),
  'kick': ('person', false, false),
  'ban': ('person_text', false, false),
  'unban': ('person', false, false),
  serverBanCommand: ('person_text', false, true),
  serverUnbanCommand: ('person', false, true),
  'mute': ('person_duration', false, false),
  'op': ('person', false, false),
  'deop': ('person', false, false),
  'names': ('none', false, false),
  'list': ('none', true, false),
  'whois': ('person', true, false),
  'ignore': ('person', true, false),
  'unignore': ('person', true, false),
  'nick': ('text', true, false),
  'away': ('duration_optional', true, false),
  'back': ('none', true, false),
  'dnd': ('duration_off', true, false),
  'quiet': ('duration_off', true, false),
};

/// Other names a command answers to that the menu does not show
/// (commands.json, aliases).
const hiddenCommandAliases = {
  'kline': serverBanCommand,
  'unkline': serverUnbanCommand,
  'who': 'names',
};

/// Other names for a command.
const commandAliases = {
  'part': 'leave',
  'mahjong': 'mahjongg',
  ...hiddenCommandAliases,
};

/// Whether a name is a built-in command or one of its hidden aliases.
bool isBuiltinCommand(String name) =>
    builtinCommands.containsKey(name) || hiddenCommandAliases.containsKey(name);

/// The built-ins a community's command may replace: the fun and games
/// (commands.json, replaceable). Every other built-in is protected, a new one
/// included: the server refuses its name and aliases to saved replies, custom
/// commands and bot commands, and ignores any made before.
const replaceableCommands = {
  'shrug',
  'tableflip',
  'unflip',
  'roll',
  'flip',
  'pick',
  'shuffle',
  'draw',
  'tarot',
  'mahjongg',
  'mahjong',
};

/// Whether a name is a protected built-in or one of its aliases.
bool isProtectedCommand(String name) =>
    isBuiltinCommand(name) &&
    !replaceableCommands.contains(hiddenCommandAliases[name] ?? name);

/// The one rule of which built-ins are not commands for this reader here
/// (ruling R72; testdata/commands/parse.json, hidden_cases). [listed] is the
/// server's command list for the conversation as (name, kind): a saved
/// reply, custom command or bot command in it hides the built-in of its
/// name, each name on its own, unless the built-in is protected. The
/// admin-only built-ins and their aliases
/// are hidden from anyone else. What is hidden is left out of the menu, is
/// not read by [parseCommand] and is not carried out.
Set<String> hiddenBuiltins(
  Iterable<(String, String)> listed, {
  required bool admin,
}) => {
  for (final (name, kind) in listed)
    if (kind != 'builtin' &&
        isBuiltinCommand(name) &&
        !isProtectedCommand(name))
      name,
  if (!admin) ...[
    for (final e in builtinCommands.entries)
      if (e.value.$3) e.key,
    for (final e in hiddenCommandAliases.entries)
      if (builtinCommands[e.value]!.$3) e.key,
  ],
};

/// The results the server draws: never in a sealed chat, which the server
/// cannot write into.
const chanceCommands = {
  'roll',
  'flip',
  'pick',
  'shuffle',
  'draw',
  'tarot',
  'mahjongg',
  'mahjong',
};

/// The commands the forms builder and the reminders carry out.
const formCommandNames = {
  'poll',
  'rsvp',
  'signup',
  'approve',
  'form',
  'remind',
};

/// Every error [parseCommand] gives.
const commandErrors = [
  'needs_text',
  'takes_nothing',
  'needs_options',
  'too_many_options',
  'empty_option',
  'bad_draw',
  'unknown_deck',
  'too_many_cards',
  'needs_channel',
  'needs_person',
  'bad_person',
  'bad_duration',
  'unknown_spread',
  'bad_count',
];

const shrugText = r'¯\\\_(ツ)\_/¯';
const _endings = {
  'shrug': shrugText,
  'tableflip': '(╯°□°)╯︵ ┻━┻',
  'unflip': '┬─┬ノ( º _ ºノ)',
};

const _maxSeconds = 366 * 86400;
const _units = {'m': 60, 'h': 3600, 'd': 86400, 'w': 604800};

/// Reads `30m`, `2h`, `1h30m`, `1d`, `2w` or a bare number of minutes as
/// seconds; null if it cannot.
int? parseDuration(String s) {
  final t = s.trim().toLowerCase();
  var secs = 0;
  if (RegExp(r'^\d+$').hasMatch(t)) {
    secs = int.parse(t) * 60;
  } else if (RegExp(r'^(\d+[mhdw])+$').hasMatch(t)) {
    for (final m in RegExp(r'(\d+)([mhdw])').allMatches(t)) {
      secs += int.parse(m.group(1)!) * _units[m.group(2)]!;
    }
  } else {
    return null;
  }
  return secs > 0 && secs <= _maxSeconds ? secs : null;
}

final _person = RegExp(
  r'^@?([a-z0-9._-]+(?:@[a-z0-9.-]+)?)$',
  caseSensitive: false,
);
final _link = RegExp(r'^<#([0-9a-f-]{36})>$');

Map<String, Object?> _channelRef(String s) {
  final m = _link.firstMatch(s);
  if (m != null) return {'id': m.group(1)};
  return {'name': s.replaceFirst(RegExp('^#'), '').trim()};
}

/// The deck a name or alias means, or null.
String? findDeck(String? name) {
  final n = (name == null || name.isEmpty ? 'standard' : name).toLowerCase();
  for (final e in cardDecks.entries) {
    if (e.key == n || e.value.aliases.contains(n)) return e.key;
  }
  return null;
}

/// The tarot spread a name or alias means, or null.
String? findSpread(String? name) {
  final n = (name == null || name.isEmpty ? 'three' : name).toLowerCase();
  for (final e in tarotSpreads.entries) {
    if (e.key == n || e.value.aliases.contains(n)) return e.key;
  }
  return null;
}

/// Reads text typed in a composer. Null when the text is not a built-in
/// command; otherwise `{name, …}` with what was read, or `{name, error}`.
/// Channel links in the text are written `<#conversation-id>`. [hidden] is
/// what [hiddenBuiltins] gives for the reader: those are not built-ins here.
Map<String, Object?>? parseCommand(
  String text, {
  Set<String> hidden = const {},
}) {
  final m = RegExp(r'^/([a-z0-9_+-]+)(?:\s+([\s\S]*))?$').firstMatch(text);
  if (m == null ||
      !isBuiltinCommand(m.group(1)!) ||
      hidden.contains(m.group(1))) {
    return null;
  }
  final typed = m.group(1)!;
  final name = commandAliases[typed] ?? typed;
  final rest = (m.group(2) ?? '').trim();
  final out = <String, Object?>{'name': name};
  Map<String, Object?> fail(String e) => {'name': name, 'error': e};
  final words = rest.isEmpty ? <String>[] : rest.split(RegExp(r'\s+'));
  Map<String, Object?>? person() {
    if (words.isEmpty) return fail('needs_person');
    final p = _person.firstMatch(words[0]);
    if (p == null) return fail('bad_person');
    out['person'] = p.group(1)!.toLowerCase();
    return null;
  }

  String after() => rest.substring(words[0].length).trim();
  final args = builtinCommands[name]!.$1;
  switch (args) {
    case 'form' || 'raw':
      out['args'] = rest;
      return out;
    case 'none':
      return rest.isNotEmpty ? fail('takes_nothing') : out;
    case 'text':
      if (rest.isEmpty) return fail('needs_text');
      if (name == 'me') return {'name': name, 'post': rest, 'action': true};
      if (name == 'spoiler') return {'name': name, 'post': '||$rest||'};
      out['text'] = rest;
      return out;
    case 'text_optional':
      final end = _endings[name];
      if (end != null) {
        return {'name': name, 'post': rest.isEmpty ? end : '$rest $end'};
      }
      if (rest.isNotEmpty) out['text'] = rest;
      return out;
    case 'dice':
      out['dice'] = rest;
      return out;
    case 'options':
      final opts = rest.isEmpty
          ? <String>[]
          : [for (final o in rest.split('|')) o.trim()];
      if (opts.length < 2) return fail('needs_options');
      if (opts.length > 20) return fail('too_many_options');
      if (opts.any((o) => o.isEmpty)) return fail('empty_option');
      out['options'] = opts;
      return out;
    case 'draw':
      var count = 1;
      var deck = 'standard';
      if (words.length > 2) return fail('bad_draw');
      if (words.isNotEmpty) {
        if (RegExp(r'^\d+$').hasMatch(words[0])) {
          count = int.parse(words[0]);
          if (words.length > 1) deck = words[1];
        } else if (words.length == 1) {
          deck = words[0];
        } else {
          return fail('bad_draw');
        }
      }
      if (count < 1) return fail('bad_draw');
      final key = findDeck(deck);
      if (key == null) return fail('unknown_deck');
      if (count > cardDecks[key]!.cards.length) return fail('too_many_cards');
      return {'name': name, 'count': count, 'deck': key};
    case 'spread':
      final key = words.length > 1 ? null : findSpread(rest);
      return key == null
          ? fail('unknown_spread')
          : {'name': name, 'spread': key};
    case 'count':
      final n = rest.isEmpty
          ? 1
          : (RegExp(r'^\d+$').hasMatch(rest) ? int.parse(rest) : 0);
      if (n < 1 || n > cardDecks['mahjong']!.cards.length) {
        return fail('bad_count');
      }
      return {'name': name, 'count': n};
    case 'channel':
      if (rest.isEmpty) return fail('needs_channel');
      out['channel'] = _channelRef(rest);
      return out;
    case 'channel_optional':
      if (rest.isNotEmpty) out['channel'] = _channelRef(rest);
      return out;
    case 'person':
      return person() ?? (words.length > 1 ? fail('bad_person') : out);
    case 'person_text':
      final bad = person();
      if (bad != null) return bad;
      final t = after();
      if (t.isNotEmpty) out['text'] = t;
      return out;
    case 'person_channel':
      final bad = person();
      if (bad != null) return bad;
      final t = after();
      if (t.isNotEmpty) out['channel'] = _channelRef(t);
      return out;
    case 'person_duration':
      final bad = person();
      if (bad != null) return bad;
      final t = after();
      final secs = t.isEmpty ? 3600 : parseDuration(t);
      if (secs == null) return fail('bad_duration');
      out['seconds'] = secs;
      return out;
    case 'duration_optional' || 'duration_off':
      if (rest.isEmpty) {
        if (name == 'quiet') out['seconds'] = 3600;
        return out;
      }
      if (args == 'duration_off' && rest.toLowerCase() == 'off') {
        return {'name': name, 'off': true};
      }
      final secs = parseDuration(rest);
      if (secs == null) return fail('bad_duration');
      out['seconds'] = secs;
      return out;
  }
  return null;
}

/// The chance request a parsed command asks the server for, or null.
Map<String, Object?>? chanceRequest(Map<String, Object?> c) =>
    switch (c['name']) {
      'roll' => {
        'kind': 'roll',
        if ((c['dice'] as String? ?? '').isNotEmpty) 'dice': c['dice'],
      },
      'flip' => {'kind': 'flip'},
      'pick' || 'shuffle' => {'kind': c['name'], 'options': c['options']},
      'draw' => {'kind': 'draw', 'count': c['count'], 'deck': c['deck']},
      'mahjongg' => {'kind': 'draw', 'count': c['count'], 'deck': 'mahjong'},
      'tarot' => {'kind': 'tarot', 'spread': c['spread']},
      _ => null,
    };

/// A tarot reading's overall energy, by key (testdata/commands/tarot.json,
/// summary_cases): the major and minor balance, how many are reversed, and
/// a suit that stands out (at least two cards and above a quarter).
List<String> tarotSummaryKeys(List<String> cards, List<bool> reversed) {
  final n = cards.length;
  final major = cards.where((c) => c.startsWith('major-')).length;
  final out = [
    major * 100 > 60 * n
        ? 'major_high'
        : major * 100 < 30 * n
        ? 'major_low'
        : 'major_balanced',
  ];
  final r = reversed.where((x) => x).length;
  if (r * 100 > 60 * n) {
    out.add('reversed_many');
  } else if (r * 100 < 20 * n) {
    out.add('reversed_few');
  }
  for (final s in ['cups', 'pentacles', 'wands', 'swords']) {
    final k = cards.where((c) => c.startsWith('$s-')).length;
    if (k >= 2 && k * 4 > n) out.add('suit_$s');
  }
  return out;
}
