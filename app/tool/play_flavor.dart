// Turns the checked-in tree (no Google library: the F-Droid build) into
// the Play Store build, which is woken through Firebase Cloud Messaging
// (docs/CLIENTS.md sections 3 and 14, docs/PUSH.md section 7), and back.
//
//   dart run tool/play_flavor.dart          add Firebase
//   dart run tool/play_flavor.dart --undo   take it out again
//
// It adds `firebase_core` and `firebase_messaging` to pubspec.yaml and
// puts tool/play/fcm.dart.in in place of the stub lib/platform/fcm.dart.
// Firebase is configured from --dart-define values at build time (see
// tool/play/fcm.dart.in), so no google-services.json is needed. Never
// commit the tree in this state.

import 'dart:io';

const _packages = ['firebase_core:^5.1.0', 'firebase_messaging:^17.0.1'];

Future<void> main(List<String> args) async {
  final undo = args.contains('--undo');
  final root = File.fromUri(Platform.script).parent.parent.path;
  final target = File('$root/lib/platform/fcm.dart');
  final source = File(
    '$root/tool/play/${undo ? 'fcm_stub.dart.in' : 'fcm.dart.in'}',
  );
  final pub = await Process.run(
    'flutter',
    [
      'pub',
      undo ? 'remove' : 'add',
      for (final p in _packages) undo ? p.split(':').first : p,
    ],
    workingDirectory: root,
    runInShell: true,
  );
  stdout.write(pub.stdout);
  stderr.write(pub.stderr);
  if (pub.exitCode != 0) exit(pub.exitCode);
  await source.copy(target.path);
  stdout.writeln(
    undo ? 'Firebase taken out: this is the F-Droid tree again.' : 'Firebase added: build the Play app with the FCM_* --dart-define values.',
  );
}
