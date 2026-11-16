import 'dart:convert';
import 'dart:io';

import 'package:churr/core/commands/commands.dart';
import 'package:churr/core/commands/decks.dart';
import 'package:flutter_test/flutter_test.dart';

/// The built-in commands (rulings R67, R68, R74) against the shared files in
/// testdata/commands: the table, the grammar, the decks and the tarot.
Object? _read(String name) =>
    jsonDecode(File('../testdata/commands/$name').readAsStringSync());

/// JSON with its keys in order, so two maps compare as text.
String _canon(Object? v) {
  Object? sort(Object? v) => v is Map
      ? {for (final k in (v.keys.toList()..sort())) k: sort(v[k])}
      : v is List
      ? [for (final x in v) sort(x)]
      : v;
  return jsonEncode(sort(v));
}

void main() {
  test('the table is the shared one', () {
    final table = (_read('commands.json')! as Map)['commands'] as List;
    expect(builtinCommands.keys.toList(), [
      for (final c in table) (c as Map)['name'],
    ]);
    for (final c in table.cast<Map>()) {
      expect(builtinCommands[c['name']], (
        c['args'],
        c['sealed'],
        c['admin'],
      ), reason: '${c['name']}');
      expect(commandGroupOf[c['name']], c['group'], reason: '${c['name']}');
      expect(commandGroups, contains(c['group']));
    }
    expect(
      hiddenCommandAliases,
      ((_read('commands.json')! as Map)['aliases'] as Map)
          .cast<String, String>(),
    );
    expect(replaceableCommands, {
      for (final c in table.cast<Map>())
        if (c['replaceable'] == true) c['name'],
    });
    for (final e in hiddenCommandAliases.entries) {
      expect(isProtectedCommand(e.key), isProtectedCommand(e.value));
    }
  });

  test('the one rule of hidden built-ins (ruling R72)', () {
    final v = _read('parse.json')! as Map;
    for (final c in (v['hidden_cases'] as List).cast<Map>()) {
      final listed = [
        for (final l in (c['listed'] as List).cast<Map>())
          (l['name'] as String, l['kind'] as String),
      ];
      expect(
        hiddenBuiltins(listed, admin: c['admin'] as bool).toList()..sort(),
        c['hidden'],
        reason: '$listed',
      );
    }
  });

  test('every case of the grammar', () {
    final v = _read('parse.json')! as Map;
    expect(commandErrors.toSet(), (v['errors'] as List).toSet());
    expect(muteMaxSeconds, v['mute_max_seconds']);
    for (final c in (v['cases'] as List).cast<Map>()) {
      expect(
        _canon(
          parseCommand(
            c['text'] as String,
            hidden: {...(c['hidden'] as List? ?? const []).cast<String>()},
          ),
        ),
        _canon(c['want']),
        reason: c['text'] as String,
      );
    }
  });

  test('the literal slash (ruling R74)', () {
    final v = _read('parse.json')! as Map;
    for (final c in (v['literal_cases'] as List).cast<Map>()) {
      expect(
        literalSlash(c['text'] as String),
        c['literal'],
        reason: jsonEncode(c['text']),
      );
    }
  });

  test('the decks are the shared ones', () {
    final v = _read('decks.json')! as Map;
    final decks = (v['decks'] as List).cast<Map>();
    expect(cardDecks.keys.toList(), [for (final d in decks) d['name']]);
    for (final d in decks) {
      final mine = cardDecks[d['name']]!;
      expect(mine.cards, d['cards'], reason: '${d['name']}');
      expect(mine.cards.length, d['size']);
      expect(mine.aliases, d['aliases']);
      expect(mine.orientation, d['orientation']);
      expect(mine.naming, d['naming']);
      for (final a in [
        d['name'] as String,
        ...(d['aliases'] as List).cast<String>(),
      ]) {
        expect(findDeck(a.toUpperCase()), d['name']);
      }
    }
    expect(cardGlyphs, v['glyphs']);
    expect(cardExtras, (v['extras'] as Map).keys.toSet());
  });

  test('the tarot: spreads and the overall energy', () {
    final v = _read('tarot.json')! as Map;
    for (final s in (v['spreads'] as List).cast<Map>()) {
      expect(tarotSpreads[s['name']]!.positions, s['positions']);
      expect(findSpread(s['name'] as String), s['name']);
    }
    for (final c in (v['summary_cases'] as List).cast<Map>()) {
      expect(
        tarotSummaryKeys(
          (c['cards'] as List).cast<String>(),
          (c['reversed'] as List).cast<bool>(),
        ),
        c['keys'],
      );
    }
  });

  test('durations', () {
    expect(parseDuration('1h30m'), 5400);
    expect(parseDuration('45'), 2700);
    expect(parseDuration('0m'), isNull);
    expect(parseDuration('soon'), isNull);
  });
}
