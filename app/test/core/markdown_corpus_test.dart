import 'dart:convert';
import 'dart:io';

import 'package:churr/core/markdown/objects.dart';
import 'package:churr/core/markdown/parser.dart';
import 'package:flutter_test/flutter_test.dart';

/// Runs the shared corpus in testdata/markdown (see its README): every
/// case's text must parse to the case's tree. The Go and JavaScript
/// parsers run the same files.
void main() {
  final dir = Directory('../testdata/markdown');
  final files =
      dir
          .listSync()
          .whereType<File>()
          .where((f) => f.path.endsWith('.json'))
          .where((f) => !f.path.endsWith('resolver.json'))
          .toList()
        ..sort((a, b) => a.path.compareTo(b.path));

  var total = 0;
  for (final f in files) {
    final cases = (jsonDecode(f.readAsStringSync()) as List)
        .cast<Map<String, dynamic>>();
    total += cases.length;
    group(f.uri.pathSegments.last, () {
      for (final c in cases) {
        test(c['name'] as String, () {
          final got = _norm(parseMarkdown(c['text'] as String).toTree());
          expect(jsonEncode(got), jsonEncode(_norm(c['tree'])));
          // The objects of ruling R73 the app finds itself (sealed chats,
          // the composer) are the case's entities of those types.
          final r = (c['resolver'] as Map?) ?? const {};
          final objs = findObjects(
            c['text'] as String,
            patterns: [
              for (final p in (r['patterns'] as List?) ?? const [])
                (p as Map).cast<String, dynamic>(),
            ],
            hints: [
              for (final h in (r['object_hints'] as List?) ?? const [])
                (h as Map).cast<String, dynamic>(),
            ],
            origin: '${r['origin'] ?? ''}',
          );
          // Inline dice are the server's value of kind dice_roll over the
          // span; the app finds them as `dice` and is held to the range.
          final want = [
            for (final e in (c['entities'] as List?) ?? const [])
              if (e is Map &&
                  e['type'] == 'object' &&
                  (e['object'] as Map?)?['kind'] == 'dice_roll')
                {'type': 'dice', 'at': e['at'], 'len': e['len']}
              else if (objectKinds.contains((e as Map)['type']))
                e,
          ];
          final found = [
            for (final o in objs)
              if (o['type'] == 'dice')
                {'type': 'dice', 'at': o['at'], 'len': o['len']}
              else
                o,
          ];
          expect(jsonEncode(_norm(found)), jsonEncode(_norm(want)));
        });
      }
    });
  }

  test('the corpus was found', () {
    // A wrong path must not pass by finding nothing.
    expect(total, greaterThanOrEqualTo(60));
  });
}

/// Absent and empty mean the same in the compact tree, and key order does
/// not matter, so both sides are reduced to one form before comparing.
Object? _norm(Object? v) {
  if (v is Map) {
    final keys = v.keys.cast<String>().toList()..sort();
    return {
      for (final k in keys)
        if (!_empty(v[k])) k: _norm(v[k]),
    };
  }
  if (v is List) return [for (final e in v) _norm(e)];
  return v;
}

bool _empty(Object? v) => v == null || v == '' || (v is List && v.isEmpty);
