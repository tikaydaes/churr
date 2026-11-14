import 'dart:io';

import 'package:churr/core/api/operations.dart';
import 'package:flutter_test/flutter_test.dart';

/// Holds the Dart client to docs/openapi.yaml, so the two cannot drift:
/// every operation the app calls must exist in the contract with the
/// same identifier, method and path, and no file other than
/// `operations.dart` may spell an API path.
void main() {
  // The file is generated with a fixed layout: paths at two spaces,
  // methods at four, `operationId` at six. Reading it line by line needs
  // no YAML package.
  final contract = <String, (String method, String path)>{};
  String? path, method;
  var inPaths = false;
  for (final line in File('../docs/openapi.yaml').readAsLinesSync()) {
    if (line == 'paths:') inPaths = true;
    if (line == 'components:') inPaths = false;
    if (!inPaths) continue;
    final p = RegExp(r'^  (/\S*):$').firstMatch(line);
    final m = RegExp(r'^    (get|post|put|patch|delete):$').firstMatch(line);
    final o = RegExp(r'^      operationId: (\S+)$').firstMatch(line);
    if (p != null) path = p.group(1);
    if (m != null) method = m.group(1)!.toUpperCase();
    if (o != null) contract[o.group(1)!] = (method!, path!);
  }

  test('the contract was read', () {
    expect(contract.length, greaterThan(300));
    expect(contract['postMessage'], (
      'POST',
      '/api/v1/conversations/{id}/messages',
    ));
  });

  for (final op in Ops.all) {
    test('${op.id} is ${op.method} ${op.path} in the contract', () {
      expect(
        contract.containsKey(op.id),
        isTrue,
        reason: 'no such operationId in docs/openapi.yaml',
      );
      expect((op.method, op.path), contract[op.id]);
    });
  }

  test('operation identifiers are listed once', () {
    expect({for (final op in Ops.all) op.id}.length, Ops.all.length);
  });

  test('no other file under lib/ spells an API path', () {
    final offenders = <String>[];
    for (final f in Directory(
      'lib',
    ).listSync(recursive: true).whereType<File>()) {
      if (!f.path.endsWith('.dart') || f.path.endsWith('core/api/operations.dart')) continue;
      final lines = f.readAsLinesSync();
      for (var i = 0; i < lines.length; i++) {
        final line = lines[i];
        // Doc comments may name a path; code may not.
        if (line.trimLeft().startsWith('//')) continue;
        if (RegExp('''['"][^'"]*(/api/v1|/oauth/|/\\.well-known)''')
            .hasMatch(line)) {
          offenders.add('${f.path}:${i + 1}: ${line.trim()}');
        }
      }
    }
    expect(offenders, isEmpty);
  });

  test('a filled path encodes its parameters', () {
    expect(
      Ops.addReaction.fill({'id': 'm1', 'emoji': '👍'}),
      '/api/v1/messages/m1/reactions/%F0%9F%91%8D',
    );
    expect(
      Ops.addReaction.fill({'id': 'm1', 'emoji': ':party:'}),
      '/api/v1/messages/m1/reactions/%3Aparty%3A',
    );
    expect(thumbnailPath('f1'), '/api/v1/files/f1/thumb');
  });
}
