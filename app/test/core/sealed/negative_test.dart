// Refusals: every refusal the Go reference's own tests make (ref_test.go),
// and those the state machine must make: a wrong signature, a wrong chain
// hash, a wrong device set, a replayed counter, a stale epoch, tampered
// ciphertext, a truncated or reordered file, and the rest.

import 'dart:convert';
import 'dart:typed_data';

import 'package:churr/core/sealed/sealed.dart';
import 'package:flutter_test/flutter_test.dart';

/// Any number of deterministic values, whatever they are for.
class StreamRandom implements SealedRandom {
  final String seed;
  int _i = 0;
  StreamRandom(this.seed);
  @override
  Uint8List take(String what, int n) {
    final out = BytesBuilder();
    while (out.length < n) {
      out.add(sha256(utf8.encode('$seed/${_i++}/$what')));
    }
    return out.takeBytes().sublist(0, n);
  }
}

Uint8List fixed(String s, int n) => StreamRandom(s).take('x', n);

final annId = Uuid.parse('0193a000-0000-7000-8000-00000000000a');
final bobId = Uuid.parse('0193a000-0000-7000-8000-00000000000b');
final convId = Uuid.parse('0193a0e0-0000-7000-8000-0000000000c0');

/// Two accounts with one device each, epoch 1 accepted by both, and the
/// stand-in server's order of items.
class World {
  int now = 1800000000000;
  late Device ann, bob;
  final Map<Uuid, List<SignedList>> lists = {};
  int seq = 0;

  Chat get annChat => ann.chat(convId, bobId);
  Chat get bobChat => bob.chat(convId, annId);

  static Future<World> make() async {
    final w = World();
    w.ann = await Device.create(
      account: annId,
      server: 'chat.example.org',
      platform: platformAndroid,
      random: StreamRandom('ann'),
      now: () => w.now,
      id: Uuid.parse('0193aa00-0000-7000-8000-0000000000a1'),
    );
    w.bob = await Device.create(
      account: bobId,
      server: 'chat.example.org',
      platform: platformIOS,
      random: StreamRandom('bob'),
      now: () => w.now,
      id: Uuid.parse('0193aa00-0000-7000-8000-0000000000b1'),
    );
    for (final d in [w.ann, w.bob]) {
      final b = await d.createBackup();
      await d.adopt(b);
      w.lists[d.account] = [b.list];
    }
    await w.ann.learnLists(bobId, w.lists[bobId]!);
    await w.bob.learnLists(annId, w.lists[annId]!);
    final (stored, k) = await w.annChat.makeEpoch();
    await w.post(stored, k);
    return w;
  }

  Future<void> post(Uint8List stored, int keyEpoch) async {
    seq++;
    for (final c in [annChat, bobChat]) {
      await c.acceptEpoch(keyEpoch, seq, stored);
    }
  }

  Future<Row> send(Chat c, String t) async {
    final o = await c.seal(messageContent(t));
    seq++;
    return Row(
      seq: seq,
      author: Header.decode(o.ciphertext).accountId,
      keyEpoch: o.keyEpoch,
      quiet: o.quiet,
      ciphertext: o.ciphertext,
    );
  }
}

Future<void> expectRefused(Future<Object?> Function() f, Matcher type) async {
  Object? err;
  try {
    await f();
  } catch (e) {
    err = e;
  }
  expect(err, type);
}

void main() {
  group('primitives and constructions', () {
    test('an all-zero X25519 result is refused', () {
      expect(
        () => x25519(fixed('a', 32), Uint8List(32)),
        throwsA(isA<ZeroSharedError>()),
      );
    });

    test('a seal with any byte changed, another label, context or key, or a zero one-time key does not open', () {
      final priv = fixed('recipient', 32);
      final sealed = seal(
        x25519Public(priv),
        'churr seal test v1',
        utf8.encode('context'),
        utf8.encode('thirty-two bytes of plaintext!!!'),
        fixed('one-time', 32),
      );
      expect(sealed.length, 80);
      expect(
        utf8.decode(
          openSealed(
            priv,
            'churr seal test v1',
            utf8.encode('context'),
            sealed,
          ),
        ),
        'thirty-two bytes of plaintext!!!',
      );
      expect(
        () => openSealed(
          priv,
          'churr seal other v1',
          utf8.encode('context'),
          sealed,
        ),
        throwsA(isA<OpenError>()),
      );
      expect(
        () => openSealed(
          priv,
          'churr seal test v1',
          utf8.encode('contexu'),
          sealed,
        ),
        throwsA(isA<OpenError>()),
      );
      expect(
        () => openSealed(
          fixed('someone else', 32),
          'churr seal test v1',
          utf8.encode('context'),
          sealed,
        ),
        throwsA(isA<OpenError>()),
      );
      expect(
        () => openSealed(
          priv,
          'churr seal test v1',
          utf8.encode('context'),
          cat([Uint8List(32), sealed.sublist(32)]),
        ),
        throwsA(isA<OpenError>()),
      );
      for (var i = 0; i < sealed.length; i++) {
        final changed = Uint8List.fromList(sealed)..[i] ^= 1;
        expect(
          () => openSealed(
            priv,
            'churr seal test v1',
            utf8.encode('context'),
            changed,
          ),
          throwsA(isA<OpenError>()),
          reason: 'byte $i',
        );
      }
    });

    test('a lock with any byte changed or another label does not open', () {
      final key = fixed('lock key', 32);
      final locked = lock(
        key,
        'churr lock test v1',
        utf8.encode('ctx'),
        utf8.encode('hello'),
        fixed('salt', 16),
      );
      expect(
        utf8.decode(
          unlockLocked(key, 'churr lock test v1', utf8.encode('ctx'), locked),
        ),
        'hello',
      );
      expect(
        () =>
            unlockLocked(key, 'churr lock test v2', utf8.encode('ctx'), locked),
        throwsA(isA<OpenError>()),
      );
      for (var i = 0; i < locked.length; i++) {
        final changed = Uint8List.fromList(locked)..[i] ^= 1;
        expect(
          () => unlockLocked(
            key,
            'churr lock test v1',
            utf8.encode('ctx'),
            changed,
          ),
          throwsA(isA<OpenError>()),
        );
      }
    });

    test('a signature under one label is refused under every other, and a wrong signature is refused', () async {
      final key = await SigningKey.fromSeed(fixed('label test', 32));
      final record = utf8.encode('a record');
      for (final made in signatureLabels) {
        final sig = await sign(key, made, record);
        for (final checked in signatureLabels) {
          expect(
            await verify(key.public, checked, record, sig),
            made == checked,
            reason: '$made checked under $checked',
          );
        }
        expect(
          await verify(
            key.public,
            made,
            record,
            Uint8List.fromList(sig)..[10] ^= 1,
          ),
          isFalse,
        );
        expect(
          await verify(key.public, made, utf8.encode('a recore'), sig),
          isFalse,
        );
      }
    });

    test('a file cut short, reordered, without its last-chunk mark or changed is refused, whole and as a stream', () async {
      final key = fixed('file key', 32);
      Uint8List pattern(int n) =>
          Uint8List.fromList([for (var i = 0; i < n; i++) i % 251]);
      Future<void> refusedBoth(Uint8List blob, String why) async {
        expect(
          () => decryptFile(key, blob),
          throwsA(isA<FileError>()),
          reason: why,
        );
        await expectRefused(
          () => decryptFileStream(key, Stream.value(blob)).toList(),
          isA<FileError>(),
        );
      }

      for (final n in [0, 1, 65535, 65536, 65537, 200000]) {
        final plain = pattern(n);
        final blob = encryptFile(key, plain);
        expect(decryptFile(key, blob), plain);
        if (blob.length > sealedChunkSize) {
          await refusedBoth(
            blob.sublist(0, sealedChunkSize),
            'cut short at a chunk boundary ($n)',
          );
          await refusedBoth(
            blob.sublist(0, blob.length - 1),
            'last byte missing ($n)',
          );
          if (blob.length >= 2 * sealedChunkSize) {
            await refusedBoth(
              cat([
                blob.sublist(sealedChunkSize, 2 * sealedChunkSize),
                blob.sublist(0, sealedChunkSize),
                blob.sublist(2 * sealedChunkSize),
              ]),
              'reordered ($n)',
            );
          }
        }
        await refusedBoth(
          Uint8List.fromList(blob)..[blob.length ~/ 2] ^= 1,
          'changed ($n)',
        );
      }
      await refusedBoth(
        encryptChunk(key, 0, false, utf8.encode('x')),
        'no last-chunk mark',
      );
      await refusedBoth(Uint8List(0), 'empty');
      // A file of exactly one chunk plus a few bytes, cut to leave a tail
      // shorter than a tag.
      await refusedBoth(
        encryptFile(key, pattern(65536 + 3)).sublist(0, sealedChunkSize + 10),
        'a tail shorter than a tag',
      );
    });

    test('a phrase with a bad checksum, too few words or an unknown word is refused; no two words share four letters', () {
      for (final bad in [
        'abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon',
        'abandon abandon abandon',
        'abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon churr',
      ]) {
        expect(() => readPhrase(bad), throwsA(isA<PhraseError>()), reason: bad);
      }
      final seen = <String>{};
      for (final w in bip39English) {
        expect(
          seen.add(w.length > 4 ? w.substring(0, 4) : w),
          isTrue,
          reason: w,
        );
      }
    });

    test('approval codes: a different request gives a different code; look-alike letters are read as digits', () {
      final code = approvalCode(utf8.encode('request'));
      expect(code.length, 23);
      expect(
        normalizeApprovalCode(code.toLowerCase().replaceAll('-', ' ')),
        code,
      );
      expect(
        normalizeApprovalCode('OIL00-00000-00000-00000'),
        '01100-00000-00000-00000',
      );
      expect(approvalCode(utf8.encode('requesu')), isNot(code));
    });

    test('records: bytes left over, cut short, out of order, too long', () {
      final l = DeviceList(
        accountId: annId,
        server: 's',
        version: 1,
        keyVersion: 1,
        accountKey: Uint8List(32),
        devices: [],
        at: 0,
      );
      final b = l.encode();
      expect(
        () => DeviceList.decode(
          cat([
            b,
            [0],
          ]),
        ),
        throwsA(RecordError.trailing),
      );
      expect(
        () => DeviceList.decode(b.sublist(0, b.length - 1)),
        throwsA(RecordError.short),
      );
      expect(
        () => DeviceList.decode(Uint8List(maxRecord + 1)),
        throwsA(RecordError.tooLong),
      );
      expect(
        () => BackupRecord.decode(
          cat([
            [2],
            Uint8List(64),
          ]),
        ),
        throwsA(RecordError.choice),
      );
    });
  });

  group('the state machine', () {
    test('a message is drawn; a replayed counter at a later sequence number is dropped', () async {
      final w = await World.make();
      final row = await w.send(w.annChat, 'hello');
      final got = await w.bobChat.receive(row);
      expect(got.dropped, isFalse);
      expect(w.bobChat.view().single.text, 'hello');
      w.seq++;
      final replay = Row(
        seq: w.seq,
        author: row.author,
        keyEpoch: row.keyEpoch,
        quiet: row.quiet,
        ciphertext: row.ciphertext,
      );
      expect((await w.bobChat.receive(replay)).dropped, isTrue);
      expect(w.bobChat.view().length, 1);
      // The very same row again changes nothing.
      expect((await w.bobChat.receive(row)).already, isTrue);
    });

    test('a replay seen first is replaced by the original, and the message is drawn once', () async {
      final w = await World.make();
      final row = await w.send(w.annChat, 'hello');
      final later = Row(
        seq: row.seq + 5,
        author: row.author,
        keyEpoch: row.keyEpoch,
        quiet: row.quiet,
        ciphertext: row.ciphertext,
      );
      await w.bobChat.receive(later);
      final got = await w.bobChat.receive(row);
      expect(got.dropped, isFalse);
      final view = w.bobChat.view();
      expect(view.length, 1, reason: 'the reference would draw it twice here');
      expect(view.single.seq, row.seq);
    });

    test('tampered ciphertext, a wrong signature and a changed quiet flag are refused and counted', () async {
      final w = await World.make();
      final row = await w.send(w.annChat, 'hello');
      Row withBytes(Uint8List b, {bool? quiet}) => Row(
        seq: row.seq,
        author: row.author,
        keyEpoch: row.keyEpoch,
        quiet: quiet ?? row.quiet,
        ciphertext: b,
      );
      final ct = Uint8List.fromList(row.ciphertext)
        ..[headerSize + saltSize + 1] ^= 1;
      await expectRefused(
        () => w.bobChat.receive(withBytes(ct)),
        isA<MessageError>(),
      );
      final sig = Uint8List.fromList(row.ciphertext)
        ..[row.ciphertext.length - 1] ^= 1;
      await expectRefused(
        () => w.bobChat.receive(withBytes(sig)),
        isA<MessageError>(),
      );
      await expectRefused(
        () => w.bobChat.receive(withBytes(row.ciphertext, quiet: true)),
        isA<MessageError>(),
      );
      final flag = Uint8List.fromList(row.ciphertext)..[1] = 1;
      await expectRefused(
        () => w.bobChat.receive(withBytes(flag, quiet: true)),
        isA<MessageError>(),
      );
      expect(w.bobChat.unchecked, 4);
      expect(w.bobChat.view(), isEmpty);
    });

    test('a message in a stale epoch, after a newer epoch began, is refused; a sender must make a new epoch for age', () async {
      final w = await World.make();
      final old = await w.send(w.annChat, 'in epoch 1');
      final (stored, k) = await w.bobChat.makeEpoch();
      await w.post(stored, k);
      final late = Row(
        seq: w.seq + 1,
        author: old.author,
        keyEpoch: 1,
        quiet: false,
        ciphertext: old.ciphertext,
      );
      await expectRefused(() => w.bobChat.receive(late), isA<MessageError>());
      // Eight days on, sending needs a new epoch first.
      w.now += 8 * 24 * 60 * 60 * 1000;
      expect(w.annChat.needsEpoch(), 'age');
      await expectRefused(
        () => w.annChat.seal(messageContent('x')),
        isA<NeedsEpochError>(),
      );
    });

    test('an epoch record with a wrong chain hash, a wrong device set or a wrong signature is refused and kept in the chain', () async {
      final w = await World.make();
      final cur = w.annChat.current!;
      final record = EpochRecord.decode(splitStoredEpoch(cur.stored).$1);
      Future<Uint8List> resign(EpochRecord r, SigningKey k) async {
        final raw = r.encode();
        return cat([raw, await sign(k, labelEpoch, raw)]);
      }

      // Each is offered as epoch 2 to a fresh copy of Bob's chat state.
      Future<int> ruleOf(Uint8List stored) async {
        final w2 = await World.make();
        try {
          await w2.bobChat.acceptEpoch(2, 5, stored);
        } on EpochError catch (e) {
          expect(w2.bobChat.current!.accepted, isFalse);
          expect(w2.bobChat.needsEpoch(), 'refused');
          expect(w2.bob.notices.last.kind, noticeOutOfStep);
          return e.rule;
        }
        return 0;
      }

      final (good, _) = await w.annChat.makeEpoch();
      final next = EpochRecord.decode(splitStoredEpoch(good).$1);
      next.prev = Uint8List(32);
      expect(
        await ruleOf(await resign(next, w.ann.sign)),
        2,
        reason: 'wrong chain hash',
      );
      final dropped = EpochRecord.decode(splitStoredEpoch(good).$1)
        ..recipients.removeLast();
      expect(
        await ruleOf(await resign(dropped, w.ann.sign)),
        5,
        reason: 'wrong device set',
      );
      final other = await SigningKey.fromSeed(fixed('intruder', 32));
      expect(
        await ruleOf(
          await resign(EpochRecord.decode(splitStoredEpoch(good).$1), other),
        ),
        6,
        reason: 'wrong signature',
      );
      expect(record.keyEpoch, 1);
    });

    test(
      'a second record for a key epoch and a record after a gap are refused',
      () async {
        final w = await World.make();
        final (a, _) = await w.annChat.makeEpoch();
        final (b, _) = await w.bobChat.makeEpoch();
        await w.post(a, 2);
        await expectRefused(
          () => w.bobChat.acceptEpoch(2, 9, b),
          isA<SecondRecordError>(),
        );
        await expectRefused(
          () => w.bobChat.acceptEpoch(4, 10, b),
          isA<ChainGapError>(),
        );
      },
    );

    test('a list that does not check out is refused, the pin kept and a notice shown', () async {
      final w = await World.make();
      final forged = await signList(
        DeviceList(
          accountId: bobId,
          server: 'chat.example.org',
          version: 2,
          prev: listHash(w.lists[bobId]!.first.list),
          keyVersion: 1,
          accountKey: (await SigningKey.fromSeed(fixed('k', 32))).public,
          devices: [],
          at: 0,
        ).encode(),
        await SigningKey.fromSeed(fixed('k', 32)),
      );
      await expectRefused(
        () => w.ann.learnLists(bobId, [forged]),
        isA<ListError>(),
      );
      expect(w.ann.pins[bobId]!.newest, 1);
      expect(w.ann.notices.last.kind, noticeListRefused);
    });

    test('a reset after codes were compared removes the mark and holds sending until acknowledged', () async {
      final w = await World.make();
      w.ann.markVerified(bobId);
      final bob2 = await Device.create(
        account: bobId,
        server: 'chat.example.org',
        platform: platformIOS,
        random: StreamRandom('bob2'),
        now: () => w.now,
        id: Uuid.parse('0193aa00-0000-7000-8000-0000000000b9'),
      );
      await bob2.learnLists(bobId, w.lists[bobId]!);
      final reset = await bob2.resetBackup();
      await w.ann.learnLists(bobId, [reset.list]);
      expect(w.ann.notices.last.kind, noticeReset);
      expect(w.ann.pins[bobId]!.verified, isFalse);
      await expectRefused(
        () => w.annChat.seal(messageContent('x')),
        isA<HeldError>(),
      );
      w.ann.acknowledge(bobId);
      await expectRefused(
        () => w.annChat.seal(messageContent('x')),
        isA<NeedsEpochError>(),
      );
    });

    test('unlocking refuses a copy for another way, an older account record, a copy not signed by the account key, and a wrong code', () async {
      final w = await World.make();
      final newcomer = await Device.create(
        account: annId,
        server: 'chat.example.org',
        platform: platformWeb,
        random: StreamRandom('new'),
        now: () => w.now,
        id: Uuid.parse('0193aa00-0000-7000-8000-0000000000a7'),
      );
      await newcomer.learnLists(annId, w.lists[annId]!);
      final (request, code) = await newcomer.newUnlockRequest();
      await expectRefused(
        () => w.ann.answerUnlockRequest(request, 'AAAAA-AAAAA-AAAAA-AAAAA'),
        isA<CodeError>(),
      );
      final answer = await w.ann.answerUnlockRequest(
        request,
        code.toLowerCase(),
      );
      final rec = w.ann.record!;
      final sb = StoredBackup(
        rec.keyVersion,
        rec.revision,
        lockAccountRecord(w.ann.backupKey!, rec, fixed('salt', 16)),
      );
      // Opened as a copy for the phrase way: refused.
      await expectRefused(
        () => newcomer.unlockWithCopy(
          newcomer.box,
          wayPhrase,
          Uuid.zero,
          answer,
          sb,
        ),
        isA<UnlockError>(),
      );
      // A copy sealed by someone without the account key.
      final forged = await makeLockedCopy(
        newcomer.boxPublic,
        annId,
        rec.keyVersion,
        wayDevice,
        newcomer.id,
        w.ann.backupKey!,
        await SigningKey.fromSeed(fixed('server', 32)),
        fixed('ot', 32),
      );
      await expectRefused(
        () => newcomer.unlockWithDeviceCopy(forged, sb),
        isA<UnlockError>(),
      );
      expect(newcomer.unlocked, isFalse);
      await newcomer.unlockWithDeviceCopy(answer, sb);
      expect(newcomer.unlocked, isTrue);
      // An account record older than one already seen.
      newcomer.record!.revision = 5;
      newcomer.lockDevice();
      newcomer.record = rec.copy()..revision = 5;
      await expectRefused(
        () => newcomer.unlockWithDeviceCopy(answer, sb),
        isA<UnlockError>(),
      );
    });

    test(
      'a backup record for another epoch, or with a made-up secret, is refused',
      () async {
        final w = await World.make();
        final recs = w.annChat.takeNewSecrets();
        expect(recs.length, 1);
        final r = recs.single;
        expect(
          () => w.annChat.readBackupRecord(
            StoredRecord(convId, 2, r.keyVersion, r.locked),
          ),
          throwsA(isA<BackupRecordError>()),
        );
        final madeUp = lockBackupRecord(
          w.ann.backupKey!,
          annId,
          w.ann.keyVersion,
          BackupRecord(
            conversationId: convId,
            keyEpoch: 1,
            secret: fixed('made up', 32),
            at: 0,
          ),
          fixed('s', 16),
        );
        expect(
          () => w.annChat.readBackupRecord(
            StoredRecord(convId, 1, r.keyVersion, madeUp),
          ),
          throwsA(isA<BackupRecordError>()),
        );
        w.annChat.readBackupRecord(r);
      },
    );

    test(
      'a locked device cannot make epochs, send copies or hand over',
      () async {
        final w = await World.make();
        w.ann.lockDevice();
        await expectRefused(() => w.annChat.makeEpoch(), isA<LockedError>());
        await expectRefused(
          () => w.ann.deviceCopyFor(w.bob.id, w.bob.boxPublic),
          isA<LockedError>(),
        );
        expect(
          () => w.ann.handoverFor(w.bob.boxPublic, 'c'),
          throwsA(isA<LockedError>()),
        );
      },
    );

    test('a device saved and loaded keeps its counter, so the next message does not reuse it', () async {
      final w = await World.make();
      final store = MemoryStore();
      w.ann.store = store;
      final row = await w.send(w.annChat, 'one');
      final loaded = (await loadDevice(
        store,
        random: StreamRandom('ann-after'),
        now: () => w.now,
      ))!;
      final o = await loaded.chat(convId, bobId).seal(messageContent('two'));
      expect(
        Header.decode(o.ciphertext).counter,
        Header.decode(row.ciphertext).counter + 1,
      );
    });
  });
}
