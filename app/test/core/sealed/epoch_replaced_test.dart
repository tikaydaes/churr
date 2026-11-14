// `sealed.epoch_started` with `"replaced": true` (docs/EVENTS.md section
// 12): in a sealed chat across two servers, both devices made a record for
// the same key epoch at the same moment. The other server's stood, so this
// device takes it in place of its own and sends again, under it, what it
// had sent under the one that gave way.

import 'dart:convert';
import 'dart:typed_data';

import 'package:churr/core/sealed/sealed.dart';
import 'package:flutter_test/flutter_test.dart';

import 'negative_test.dart' show World, StreamRandom, convId, bobId;

Row rowOf(Outgoing o, int seq) => Row(
  seq: seq,
  author: Header.decode(o.ciphertext).accountId,
  keyEpoch: o.keyEpoch,
  quiet: o.quiet,
  ciphertext: o.ciphertext,
);

/// Ann (on one server) and Bob (on another) each make a record for key
/// epoch 2. Each server files its own first; Ann sends under hers.
Future<(World, Uint8List annRecord, Uint8List bobRecord, Outgoing sent)>
race() async {
  final w = await World.make();
  final (annRecord, k1) = await w.annChat.makeEpoch();
  final (bobRecord, k2) = await w.bobChat.makeEpoch();
  expect((k1, k2), (2, 2));
  w.seq++;
  await w.annChat.acceptEpoch(2, w.seq, annRecord);
  await w.bobChat.acceptEpoch(2, w.seq, bobRecord);
  final sent = await w.annChat.seal(
    messageContent('hello under my own record'),
  );
  w.seq++;
  return (w, annRecord, bobRecord, sent);
}

void main() {
  test('the replacing record is taken and what was sent under the other is sent again', () async {
    final (w, _, bobRecord, sent) = await race();
    // Bob's device cannot read what Ann sent under the record that gave way.
    await expectLater(
      w.bobChat.receive(rowOf(sent, w.seq)),
      throwsA(isA<SealedError>()),
    );
    expect(w.annChat.sentUnder(2).length, 1);

    // Ann's server sends the event again for key epoch 2, replaced.
    w.seq++;
    final event = {
      'type': 'sealed.epoch_started',
      'conversation_id': '$convId',
      'seq': w.seq,
      'replaced': true,
      'epoch': {
        'conversation_id': '$convId',
        'key_epoch': 2,
        'seq': w.seq,
        'record': base64.encode(bobRecord),
      },
    };
    final again = await w.annChat.epochStarted(event);

    // Ann now holds Bob's record for key epoch 2.
    expect(w.annChat.current!.stored, bobRecord);
    expect(w.annChat.current!.accepted, isTrue);
    expect(w.annChat.needsEpoch(), isNull);

    // The message is sent again under it: same reference, a new counter.
    expect(again, hasLength(1));
    final resent = again.single;
    expect(resent.clientId, sent.clientId);
    expect(resent.keyEpoch, 2);
    expect(resent.counter, greaterThan(sent.counter));
    w.seq++;
    final got = await w.bobChat.receive(rowOf(resent, w.seq));
    expect(got.content['text'], 'hello under my own record');
    expect(
      w.bobChat.view().map((m) => m.text),
      contains('hello under my own record'),
    );

    // Ann still shows it once.
    w.seq++;
    await w.annChat.receive(rowOf(resent, w.seq - 1));
    expect(
      w.annChat.view().where((m) => m.reference == sent.clientId),
      hasLength(1),
    );
    // What is kept is now the copy sent under the record that stood.
    expect(w.annChat.sentUnder(2).map((x) => x.$1), [sent.clientId]);
  });

  test(
    'what was sent survives a restart before the replacement arrives',
    () async {
      final (w, _, bobRecord, sent) = await race();
      final store = MemoryStore();
      w.ann.store = store;
      // seal() writes the chat; write once more to be sure of the device.
      await w.annChat.seal(messageContent('a second one'));
      final loaded = (await loadDevice(
        store,
        random: StreamRandom('ann-again'),
        now: () => w.now,
      ))!;
      final chat = loaded.chat(convId, bobId);
      expect(chat.sentUnder(2).map((x) => x.$1), contains(sent.clientId));
      w.seq++;
      final again = await chat.replaceEpoch(2, w.seq, bobRecord);
      expect(again.map((o) => o.clientId).first, sent.clientId);
      expect(again, hasLength(2));
    },
  );

  test(
    'the same record again, or a first record, sends nothing again',
    () async {
      final (w, annRecord, _, _) = await race();
      expect(await w.annChat.replaceEpoch(2, w.seq, annRecord), isEmpty);
      expect(w.annChat.sentUnder(2), hasLength(1));
    },
  );

  test('a record that is not the newest cannot be replaced', () async {
    final (w, _, bobRecord, _) = await race();
    final (next, k) = await w.annChat.makeEpoch();
    w.seq++;
    await w.annChat.acceptEpoch(k, w.seq, next);
    await expectLater(
      w.annChat.replaceEpoch(2, w.seq + 1, bobRecord),
      throwsA(isA<ChainGapError>()),
    );
  });

  test('a refused message is not sent again', () async {
    final (w, _, bobRecord, sent) = await race();
    w.annChat.unsend(sent);
    w.seq++;
    expect(await w.annChat.replaceEpoch(2, w.seq, bobRecord), isEmpty);
  });
}
