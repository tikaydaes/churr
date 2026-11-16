import 'dart:io';
import 'dart:typed_data';

import 'package:churr/core/sealed/sealed.dart';
import 'package:churr/core/sealed_client/sqlite_store.dart';
import 'package:churr/core/session_store.dart';
import 'package:flutter_test/flutter_test.dart';

import '../sealed_support.dart';

/// The lock and sealed chats (docs/AUTH.md section 10.6): locking leaves a
/// copy of the backup key for the device's own box key and forgets the key
/// in memory and in the store; unlocking opens the copy with the box key.
/// Nothing sealed is lost: earlier messages are still read, and new ones
/// are sent and read by the other side.
void main() {
  late Directory dir;
  setUp(() => dir = Directory.systemTemp.createTempSync('sealed-lock'));
  tearDown(() => dir.deleteSync(recursive: true));

  test('lock, restart, unlock with the device copy: earlier messages read, new ones sent', () async {
    final secrets = MemorySecretStore();
    Future<SqliteSealedStore> open() => SqliteSealedStore.open(
      path: '${dir.path}/sealed.db',
      secrets: secrets,
      scope: 's1',
    );

    final me = await Device.create(
      account: Uuid.parse(meId),
      server: 'chat.example.org',
      platform: 3,
      random: SystemRandom(),
      now: testNow,
      id: Uuid.parse('0193aa00-0000-7000-8000-0000000000a1'),
    );
    final bundle = await me.createBackup();
    await me.adopt(bundle);
    final sam = await Device.create(
      account: Uuid.parse(samId),
      server: 'chat.example.org',
      platform: 3,
      random: SystemRandom(),
      now: testNow,
      id: Uuid.parse('0193aa00-0000-7000-8000-0000000000b1'),
    );
    await sam.adopt(await sam.createBackup());
    await me.learnLists(sam.account, [
      sam.pins[sam.account]!.newestList!.signed,
    ]);
    await sam.learnLists(me.account, [me.pins[me.account]!.newestList!.signed]);
    await chatBetween(
      me,
      sam,
      messages: [(sam, messageContent('Before the lock'))],
    );
    final key = Uint8List.fromList(me.backupKey!);

    var store = await open();
    me.store = store;
    await me.save();

    // Locking: the copy for this device's own box key, then forget.
    final copy = await me.deviceCopyFor(me.id, me.boxPublic);
    me.lockDevice();
    expect(me.unlocked, isFalse);
    await me.save();
    store.close();

    // A restart while locked: the store holds no backup key.
    store = await open();
    final back = (await loadDevice(
      store,
      random: SystemRandom(),
      now: testNow,
    ))!;
    expect(back.unlocked, isFalse);
    expect(back.accountKey, isNull);
    expect(back.chats.values.single.view().single.text, 'Before the lock');

    // Unlocking: the server gives the copy back to the unlocked session,
    // and the box key opens it.
    await back.unlockWithDeviceCopy(
      copy,
      StoredBackup(bundle.keyVersion, bundle.revision, bundle.accountRecord),
    );
    expect(back.unlocked, isTrue);
    expect(back.backupKey, key);

    // A new message, read on the other side.
    final mine = back.chats.values.single;
    final out = await mine.seal(messageContent('After the lock'));
    final row = Row(
      seq: 3,
      author: back.account,
      keyEpoch: out.keyEpoch,
      quiet: out.quiet,
      ciphertext: out.ciphertext,
    );
    await mine.receive(row);
    await sam.chats.values.single.receive(row);
    expect(
      sam.chats.values.single.view().map((s) => s.text),
      contains('After the lock'),
    );
    store.close();
  });
}
