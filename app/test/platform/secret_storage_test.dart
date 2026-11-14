import 'dart:io';

import 'package:churr/platform/secret_storage.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('which secret store', () {
    test('debug builds on desktop use the file', () {
      for (final os in ['macos', 'windows', 'linux']) {
        expect(usesFileSecrets(debug: true, os: os), isTrue, reason: os);
      }
    });
    test('phones always use secure storage', () {
      for (final os in ['ios', 'android']) {
        expect(usesFileSecrets(debug: true, os: os), isFalse, reason: os);
      }
    });
    test('release and profile builds never use the file', () {
      for (final os in ['macos', 'windows', 'linux', 'ios', 'android']) {
        expect(usesFileSecrets(debug: false, os: os), isFalse, reason: os);
      }
    });
  });

  group('the development file store', () {
    late Directory dir;
    setUp(() => dir = Directory.systemTemp.createTempSync('secrets'));
    tearDown(() => dir.deleteSync(recursive: true));

    test(
      'keeps, replaces and deletes values, and survives a restart',
      () async {
        final f = File('${dir.path}/dev-secrets.json');
        final a = DevFileSecretStore(f);
        expect(await a.read('churr.tokens.x'), isNull);
        await Future.wait([
          a.write('one', '1'),
          a.write('two', '2'),
          a.write('three', '3'),
        ]);
        await a.write('two', 'II');
        await a.delete('three');
        final b = DevFileSecretStore(f);
        expect(await b.read('one'), '1');
        expect(await b.read('two'), 'II');
        expect(await b.read('three'), isNull);
        if (!Platform.isWindows) {
          final mode = f.statSync().mode & 0x1ff;
          expect(mode, 0x180, reason: 'readable by this user only (600)');
        }
      },
    );

    test('a damaged file reads as empty', () async {
      final f = File('${dir.path}/dev-secrets.json')
        ..writeAsStringSync('{not json');
      expect(await DevFileSecretStore(f).read('one'), isNull);
    });
  });
}
