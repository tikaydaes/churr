import 'dart:convert';
import 'dart:io';

import 'package:churr/core/api/conversation_ops.dart';
import 'package:churr/core/commands/commands.dart';
import 'package:churr/core/commands/decks.dart';
import 'package:churr/core/commands/help.dart';
import 'package:churr/core/commands/help_text.dart';
import 'package:churr/core/markdown/objects.dart';
import 'package:churr/core/objects/units.dart';
import 'package:flutter_test/flutter_test.dart';

/// What the app's /help says (rulings R67 and R74) against the one source,
/// testdata/commands/help.json: its copy is the file as it is, every
/// command of the table has a full entry, a name after /help finds the
/// right command and never one hidden from the reader, and "Things you
/// can type" covers every object the composer knows, the units, the decks
/// and the reference patterns that apply.
void main() {
  final source = File('../testdata/commands/help.json').readAsStringSync();
  final table =
      ((jsonDecode(
                File('../testdata/commands/commands.json').readAsStringSync(),
              ) as Map)['commands']
              as List)
          .cast<Map>();
  List<SlashCommand> listedFor({required bool admin}) {
    final hidden = hiddenBuiltins(const [], admin: admin);
    return [
      for (final c in table)
        if (!hidden.contains(c['name']))
          SlashCommand({
            'name': c['name'],
            'kind': 'builtin',
            'description': c['description'],
            'hint': c['hint'],
          }),
    ];
  }

  final admin = listedFor(admin: true);
  final member = listedFor(admin: false);
  SlashCommand named(List<SlashCommand> l, String n) =>
      l.firstWhere((c) => c.name == n);

  test('the app holds help.json as it is', () {
    expect(helpSource, source.trimRight());
  });

  test('every command of the table has a full entry', () {
    for (final c in admin) {
      final e = helpEntry(c, admin);
      final why = '/${c.name}';
      expect(e.summary, isNotEmpty, reason: why);
      expect(e.description.length, greaterThan(40), reason: why);
      expect(e.usage, startsWith('/${e.name}'), reason: why);
      expect(e.examples.length, greaterThanOrEqualTo(2), reason: why);
      for (final x in e.examples) {
        expect(x.type, isNotEmpty, reason: why);
        expect(x.then, isNotEmpty, reason: why);
      }
      final row = builtinCommands[e.name]!;
      expect(
        e.where,
        endsWith(helpWords(row.$2 ? 'sealedYes' : 'sealedNo')),
        reason: why,
      );
      if (row.$3) expect(e.who, contains(helpWords('adminOnly')), reason: why);
      if ((c.hint ?? '').isNotEmpty) expect(e.args, isNotEmpty, reason: why);
    }
    expect(helpEntry(named(admin, 'part'), admin).name, 'leave');
    expect(helpEntry(named(admin, 'leave'), admin).aliases, contains('/part'));
    expect(
      helpEntry(named(admin, 'ban'), admin).aliases,
      containsAll(['/kickban', '/kb']),
    );
    expect(helpEntry(named(admin, 'flip'), admin).notes, [
      helpWords('replaceable'),
    ]);
  });

  test('a module\'s command has the help the server gives with it', () {
    final roll = SlashCommand({
      'name': 'roll',
      'kind': 'module',
      'module': 'dice',
      'description': 'Rolls dice',
      'hint': '[dice]',
      'help': {
        'description': 'Rolls dice, rolled by the server.',
        'args': [
          {'name': 'dice', 'about': 'Such as 2d6.'},
        ],
        'examples': [
          {'type': '/roll', 'then': 'Rolls one d20.'},
          {'type': '/roll 2d6', 'then': 'Rolls two.'},
        ],
        'who': 'People.',
      },
    });
    final e = helpEntry(roll, [roll]);
    expect(e.description, 'Rolls dice, rolled by the server.');
    expect(e.usage, '/roll [dice]');
    expect(e.args.single.name, 'dice');
    expect(e.examples.length, 2);
    expect(e.who, 'People.');
  });

  test('a name after /help finds its command, never a hidden one', () {
    expect(helpFind(member, 'flip')?.name, 'flip');
    expect(helpFind(member, '/flip')?.name, 'flip');
    expect(helpFind(member, 'kb')?.name, 'ban');
    expect(helpFind(member, 'part')?.name, 'leave');
    expect(helpFind(member, 'trout'), isNull);
    expect(helpFind(member, 'flip coin'), isNull);
    for (final n in ['exile', 'kline', 'broadcast', 'amsg']) {
      expect(helpFind(member, n), isNull, reason: n);
      expect(helpFind(admin, n), isNotNull, reason: n);
    }
    for (final c in member) {
      for (final r in helpEntry(c, member).related) {
        expect(member.map((m) => m.name), contains(r), reason: '/${c.name}');
      }
    }
    expect(helpEntry(named(admin, 'ban'), admin).related, contains('exile'));
    expect(
      helpEntry(named(member, 'ban'), member).related,
      isNot(contains('exile')),
    );
  });

  test('a community command shows what its admin said of it', () {
    final custom = SlashCommand({
      'name': 'deploy',
      'kind': 'saved_reply',
      'description': 'Ships the build',
    });
    expect(helpEntry(custom, [custom]).summary, 'Ships the build');
    final fish = SlashCommand({
      'name': 'slap',
      'kind': 'saved_reply',
      'description': '',
    });
    expect(helpEntry(fish, [fish]).notes.single, contains('/slap'));
    final bot = SlashCommand({'name': 'ship', 'kind': 'bot'});
    expect(helpEntry(bot, [bot]).summary, helpWords('noDescription'));
  });

  test('the decks, spreads and set come from the registries', () {
    final decks = helpMore('decks');
    expect([for (final r in decks) r.name], cardDecks.keys.toList());
    for (final r in decks) {
      expect(r.count, contains('${cardDecks[r.name]!.cards.length}'));
      expect(r.aliases, cardDecks[r.name]!.aliases);
      expect(r.about, isNotEmpty, reason: r.name);
    }
    expect([
      for (final r in helpMore('spreads')) r.name,
    ], tarotSpreads.keys.toList());
    expect(helpMore('mahjong').single.count, contains('144'));
  });

  test('things you can type cover every object and unit', () {
    final covered = {for (final t in helpThings()) ...t.covers};
    // The composer's inline dice become an `object` entity of kind
    // dice_roll.
    String entityOf(String k) => k == 'dice' ? 'object' : k;
    for (final k in objectKinds) {
      expect(covered, contains('entity:${entityOf(k)}'));
    }
    final codes = {for (final u in measureUnits) u.$2};
    final sorted = {
      for (final k in helpUnits())
        for (final u in k.units) u.code,
    };
    expect(sorted, codes);
    expect(
      [
        for (final k in helpUnits())
          for (final u in k.units) ...u.typed,
      ].length,
      measureUnits.length,
    );
    expect(helpPopupRules, hasLength(2));
    expect(helpMarkdown, isNotEmpty);
  });

  test('the reference patterns that apply, each with an example', () {
    for (final c in (helpData['pattern_examples'] as List).cast<Map>()) {
      expect(
        patternExample(c['expression'] as String),
        c['want'],
        reason: '${c['expression']}',
      );
    }
    final server = [
      {
        'name': 'Tickets',
        'prefix': 'PROJ-',
        'url_template': 'https://issues.example.com/browse/PROJ-{value}',
      },
    ];
    final here = [
      {
        'name': 'Builds',
        'prefix': 'BUILD',
        'expression': r'[A-Z]{2}\d{3}',
        'url_template': 'https://ci.example.com/{value}',
      },
    ];
    final p = helpPatterns(server: server, here: here);
    expect(p.server.single.example, 'PROJ-123');
    expect(p.server.single.host, 'issues.example.com');
    expect(p.here.single.example, 'BUILDAB123');
    expect(p.none, isFalse);
    final sealed = helpPatterns(server: server, here: here, sealed: true);
    expect(sealed.here, isEmpty);
    expect(sealed.sealed, isTrue);
    expect(helpPatterns().none, isTrue);
    final refs = helpThings(server: server)
        .firstWhere((t) => t.patterns != null);
    expect(refs.patterns!.server, hasLength(1));
    expect(
      [
        for (final t in helpThings())
          if (t.matches('[[')) t.id,
      ],
      ['dice'],
    );
  });
}
