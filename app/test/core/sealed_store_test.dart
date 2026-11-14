import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:churr/core/sealed/sealed.dart';
import 'package:churr/core/sealed_client/sqlite_store.dart';
import 'package:churr/core/session_store.dart';
import 'package:flutter_test/flutter_test.dart';

import '../sealed_support.dart';

/// The encrypted sealed store against what the library's in-memory store
/// does, and what only a real store has to do: survive a restart, keep
/// the device's private keys out of the file, be unreadable without its
/// key, and be gone after a wipe.
void main() {
  late Directory dir;
  setUp(() => dir = Directory.systemTemp.createTempSync('sealed-store'));
  tearDown(() => dir.deleteSync(recursive: true));

  Future<SqliteSealedStore> open(SecretStore secrets, [String name = 'sealed.db']) =>
      SqliteSealedStore.open(path: '${dir.path}/$name', secrets: secrets, scope: 's1');

  Uint8List bytes(String s) => Uint8List.fromList(utf8.encode(s));

  group('the same behaviours as MemoryStore', () {
    for (final kind in ['memory', 'sqlite']) {
      test('$kind: write, read, replace, delete, keys', () async {
        final SealedStore s = kind == 'memory' ? MemoryStore() : await open(MemorySecretStore());
        expect(await s.read('sealed/chat/x'), isNull);
        expect(await s.keys(), isEmpty);
        await s.write('sealed/chat/x', bytes('one'));
        await s.write('sealed/chat/y', bytes('two'));
        await s.write('sealed/chat/x', bytes('three'));
        expect(utf8.decode((await s.read('sealed/chat/x'))!), 'three');
        expect((await s.keys()).toSet(), {'sealed/chat/x', 'sealed/chat/y'});
        await s.delete('sealed/chat/y');
        await s.delete('sealed/chat/none');
        expect(await s.keys(), ['sealed/chat/x']);
        // A value written is a copy: changing the caller's bytes later
        // changes nothing stored.
        final v = bytes('four');
        await s.write('sealed/chat/z', v);
        v[0] = 0;
        expect(utf8.decode((await s.read('sealed/chat/z'))!), 'four');
      });
    }

    test('a whole device and its chat round-trip as they do in memory', () async {
      final (a, b) = await twoDevices();
      await chatBetween(a, b, messages: [(b, messageContent('Hi kai'))]);
      final mem = MemoryStore();
      final real = await open(MemorySecretStore());
      for (final s in [mem, real]) {
        a.store = s;
        await a.save();
      }
      final fromMem = (await loadDevice(mem, random: SystemRandom(), now: testNow))!;
      final fromReal = (await loadDevice(real, random: SystemRandom(), now: testNow))!;
      for (final d in [fromMem, fromReal]) {
        expect(d.id, a.id);
        expect(d.unlocked, isTrue);
        expect(d.signPublic, a.signPublic);
        expect(d.chats.values.single.view().single.text, 'Hi kai');
      }
    });
  });

  test('persists across a restart, with the private keys in secure storage only', () async {
    final secrets = MemorySecretStore();
    final (a, b) = await twoDevices();
    await chatBetween(a, b, messages: [(b, messageContent('Remember this'))]);
    var s = await open(secrets);
    a.store = s;
    await a.save();
    s.putRow(SealedRowMeta(conversationId: sealedConv, seq: 2, messageId: 'm', reference: 'r', createdAt: DateTime.utc(2026)));
    s.close();

    // The file: encrypted, no SQLite header, no text, no private key.
    final raw = File('${dir.path}/sealed.db').readAsBytesSync();
    final text = latin1.decode(raw);
    expect(text.startsWith('SQLite format 3'), isFalse);
    expect(text.contains('Remember this'), isFalse);
    expect(text.contains(b64(a.sign.seed)), isFalse);
    expect(secrets.values.keys, containsAll(['churr.sealed.s1.dbkey', 'churr.sealed.s1.device']));
    expect(secrets.values['churr.sealed.s1.device'], contains(b64(a.sign.seed)));

    s = await open(secrets);
    final back = (await loadDevice(s, random: SystemRandom(), now: testNow))!;
    expect(back.chats.values.single.view().single.text, 'Remember this');
    expect(back.box, a.box);
    expect(s.rows(sealedConv).single.messageId, 'm');
    s.close();
  });

  test('without its key the file is not read; a lost key starts it empty', () async {
    final secrets = MemorySecretStore();
    final s = await open(secrets);
    await s.write('sealed/chat/x', bytes('secret'));
    s.close();
    // Another key: the store refuses the file, deletes it, starts again.
    final other = MemorySecretStore()..values['churr.sealed.s1.dbkey'] = 'ab' * 32;
    final again = await open(other);
    expect(await again.keys(), isEmpty);
    again.close();
  });

  test('the device record without its keys in secure storage reads as no device', () async {
    final secrets = MemorySecretStore();
    final (a, _) = await twoDevices();
    final s = await open(secrets);
    a.store = s;
    await a.save();
    secrets.values.remove('churr.sealed.s1.device');
    expect(await loadDevice(s, random: SystemRandom(), now: testNow), isNull);
    s.close();
  });

  test('signing out wipes the file, its key and the device keys', () async {
    final secrets = MemorySecretStore();
    final (a, _) = await twoDevices();
    final s = await open(secrets);
    a.store = s;
    await a.save();
    await s.wipe();
    expect(File('${dir.path}/sealed.db').existsSync(), isFalse);
    expect(secrets.values.keys.where((k) => k.startsWith('churr.sealed.s1.')), isEmpty);
    final fresh = await open(secrets);
    expect(await loadDevice(fresh, random: SystemRandom(), now: testNow), isNull);
    fresh.close();
  });

  test('clearAll empties the file but keeps it and its key', () async {
    final secrets = MemorySecretStore();
    final (a, _) = await twoDevices();
    final s = await open(secrets);
    a.store = s;
    await a.save();
    await s.clearAll();
    expect(await s.keys(), isEmpty);
    expect(secrets.values.containsKey('churr.sealed.s1.dbkey'), isTrue);
    expect(secrets.values.containsKey('churr.sealed.s1.device'), isFalse);
    s.close();
  });
}
