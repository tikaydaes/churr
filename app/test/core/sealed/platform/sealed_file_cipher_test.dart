// lib/platform/sealed_file_cipher.dart under `flutter test`, where no
// platform plugin is registered: it must say so and the app must end up
// with pure Dart. Its real run is platform/harness_main.dart.

import 'package:churr/core/sealed/sealed.dart';
import 'package:churr/platform/sealed_file_cipher.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('without the plugin the platform cipher fails its self-check and pure Dart is installed', () async {
    final platform = PlatformFileChunkCipher();
    expect(platform.claimsSupport, isFalse);
    final logs = <String>[];
    expect(await fileChunkCipherSelfCheck(platform, log: logs.add), isFalse);
    expect(logs.single, contains('no platform AES-256-GCM here'));
    try {
      expect(
        await installSealedFileCipher(log: logs.add),
        same(dartFileChunkCipher),
      );
      expect(fileChunkCipher, same(dartFileChunkCipher));
      expect(logs.last, 'sealed files: pure Dart AES-256-GCM');
    } finally {
      fileChunkCipher = dartFileChunkCipher;
    }
  });
}
