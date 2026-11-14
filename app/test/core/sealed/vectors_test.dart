// One test per vector file of testdata/sealed (all but the transcript,
// which transcript_test.dart replays). Each runs every case in its file
// and compares bytes; signatures, which Ed25519 here makes
// deterministically, are compared too and also verified.

import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:churr/core/sealed/sealed.dart';
import 'package:flutter_test/flutter_test.dart';

import 'vectors_helper.dart';

Uint8List filePattern(int n) =>
    Uint8List.fromList([for (var i = 0; i < n; i++) i % 251]);

Stream<List<int>> pieces(List<int> b, int size) async* {
  for (var i = 0; i < b.length; i += size) {
    yield b.sublist(i, i + size > b.length ? b.length : i + size);
  }
}

Future<Uint8List> collect(Stream<List<int>> s) async {
  final out = BytesBuilder(copy: false);
  await for (final c in s) {
    out.add(c);
  }
  return out.takeBytes();
}

void main() {
  test('primitives.json', () async {
    final v = loadVector('primitives.json');
    final c = Cases('primitives.json');
    for (final x in v['x25519'] as List) {
      await c.check('x25519 ${x['source']}', () async {
        if (x['refused'] == true) {
          await refused(
            () async => x25519(hx(x['private']), hx(x['public'])),
            'all-zero X25519 result',
          );
        } else {
          sameBytes(
            x25519(hx(x['private']), hx(x['public'])),
            hx(x['shared']),
            'shared',
          );
        }
      });
    }
    for (final x in v['x25519_public'] as List) {
      await c.check(
        'x25519_public ${x['source']}',
        () async => sameBytes(
          x25519Public(hx(x['private'])),
          hx(x['public']),
          'public',
        ),
      );
    }
    for (final x in v['hkdf_sha256'] as List) {
      await c.check(
        'hkdf ${x['source']}',
        () async => sameBytes(
          hkdf(hx(x['ikm']), hx(x['salt']), hx(x['info']), x['length'] as int),
          hx(x['okm']),
          'okm',
        ),
      );
    }
    for (final x in v['aes_256_gcm'] as List) {
      await c.check('gcm ${x['source']}', () async {
        final ct = gcmSeal(
          hx(x['key']),
          hx(x['nonce']),
          hx(x['plaintext']),
          hx(x['associated_data']),
        );
        sameBytes(ct, hx(x['ciphertext']), 'ciphertext');
        sameBytes(
          gcmOpen(hx(x['key']), hx(x['nonce']), ct, hx(x['associated_data'])),
          hx(x['plaintext']),
          'plaintext',
        );
      });
    }
    for (final x in v['sha256'] as List) {
      await c.check(
        'sha256 ${x['source']}',
        () async =>
            sameBytes(sha256(hx(x['message'])), hx(x['digest']), 'digest'),
      );
    }
    for (final x in v['ed25519'] as List) {
      await c.check('ed25519 ${x['source']}', () async {
        eq(
          await verifyRaw(
            hx(x['public']),
            hx(x['message']),
            hx(x['signature']),
          ),
          x['valid'],
          'valid',
        );
        if (x['seed'] != null) {
          final k = await SigningKey.fromSeed(hx(x['seed']));
          sameBytes(k.public, hx(x['public']), 'public from seed');
          if (x['valid'] == true) sameBytes(await k.signRaw(hx(x['message'])), hx(x['signature']), 'signature',);
        }
      });
    }
    c.finish();
  });

  test('encoding.json', () async {
    final v = loadVector('encoding.json');
    final c = Cases('encoding.json');
    for (final t in v['types'] as List) {
      await c.check('type ${t['type']}', () async {
        final w = Writer();
        final value = t['value'];
        final r = Reader(hx(t['bytes']));
        switch (t['type']) {
          case 'u8':
            w.u8(int.parse(value));
            eq(r.u8(), int.parse(value), 'read');
          case 'u32':
            w.u32(int.parse(value));
            eq(r.u32(), int.parse(value), 'read');
          case 'u64':
            w.u64(int.parse(value));
            eq(r.u64(), int.parse(value), 'read');
          case 'time':
            w.time(int.parse(value));
            eq(r.u64(), int.parse(value), 'read');
          case 'uuid':
            w.uuid(id(value));
            eq(r.uuid(), id(value), 'read');
          case 'blob':
            w.blob(hx(value));
            sameBytes(r.blob(), hx(value), 'read');
          case 'text':
            w.text(value as String);
            eq(r.text(), value, 'read');
          case 'list of uuid':
            final l = (value as List).map(id).toList();
            w.u32(l.length);
            for (final u in l) {
              w.uuid(u);
            }
            final n = r.count(16);
            eq(
              [for (var i = 0; i < n; i++) r.uuid()].join(','),
              l.join(','),
              'read',
            );
          default:
            throw Mismatch('unknown type ${t['type']}');
        }
        r.done();
        sameBytes(w.bytes(), hx(t['bytes']), 'bytes');
      });
    }
    Uint8List reencode(String record, Uint8List b) => switch (record) {
      'unlock_request' => UnlockRequest.decode(b).encode(),
      'device_list' => DeviceList.decode(b).encode(),
      _ => throw Mismatch('unknown record $record'),
    };
    for (final a in v['accepted'] as List) {
      await c.check(
        'accepted ${a['record']}',
        () async => sameBytes(
          reencode(a['record'], hx(a['bytes'])),
          hx(a['bytes']),
          'round trip',
        ),
      );
    }
    for (final x in v['refusals'] as List) {
      await c.check('refused ${x['record']}: ${x['why']}', () async {
        try {
          reencode(x['record'], hx(x['bytes']));
        } on RecordError {
          return;
        }
        throw Mismatch('accepted');
      });
    }
    c.finish();
  });

  test('signatures.json', () async {
    final v = loadVector('signatures.json');
    final c = Cases('signatures.json');
    final key = await SigningKey.fromSeed(hx(v['seed']));
    for (final s in v['signatures'] as List) {
      await c.check('signature ${s['label']}', () async {
        final label = s['label'] as String;
        sameBytes(key.public, hx(s['public']), 'public');
        sameBytes(
          labelled(label, [hx(s['record'])]),
          hx(s['signed_bytes']),
          'signed bytes',
        );
        yes(
          await verify(
            hx(s['public']),
            label,
            hx(s['record']),
            hx(s['signature']),
          ),
          'verifies',
        );
        sameBytes(
          await sign(key, label, hx(s['record'])),
          hx(s['signature']),
          'signature',
        );
        for (final other in s['refused_under'] as List) {
          yes(
            !await verify(
              hx(s['public']),
              other as String,
              hx(s['record']),
              hx(s['signature']),
            ),
            'refused under $other',
          );
        }
      });
    }
    for (final h in v['hashes'] as List) {
      await c.check(
        'hash ${h['label']}',
        () async => sameBytes(
          hashOf(h['label'], hx(h['record'])),
          hx(h['hash']),
          'hash',
        ),
      );
    }
    c.finish();
  });

  test('seal.json', () async {
    final v = loadVector('seal.json');
    final c = Cases('seal.json');
    for (final x in v['vectors'] as List) {
      await c.check('seal ${x['label']}', () async {
        final pub = x25519Public(hx(x['recipient_private']));
        sameBytes(pub, hx(x['recipient_public']), 'recipient public');
        sameBytes(
          x25519Public(hx(x['one_time_private'])),
          hx(x['one_time_public']),
          'one-time public',
        );
        final sealed = seal(
          pub,
          x['label'],
          hx(x['context']),
          hx(x['plaintext']),
          hx(x['one_time_private']),
        );
        sameBytes(sealed, hx(x['sealed']), 'sealed');
        sameBytes(
          openSealed(
            hx(x['recipient_private']),
            x['label'],
            hx(x['context']),
            sealed,
          ),
          hx(x['plaintext']),
          'opened',
        );
      });
    }
    for (final x in v['refusals'] as List) {
      await c.check('refused: ${x['why']}', () async {
        await refused(
          () async => openSealed(
            hx(x['recipient_private']),
            x['label'],
            hx(x['context']),
            hx(x['sealed']),
          ),
          x['why'],
        );
      });
    }
    c.finish();
  });

  test('lock.json', () async {
    final v = loadVector('lock.json');
    final c = Cases('lock.json');
    for (final x in v['vectors'] as List) {
      await c.check('lock ${x['label']}', () async {
        final locked = lock(
          hx(x['key']),
          x['label'],
          hx(x['context']),
          hx(x['plaintext']),
          hx(x['salt']),
        );
        sameBytes(locked, hx(x['locked']), 'locked');
        sameBytes(
          unlockLocked(hx(x['key']), x['label'], hx(x['context']), locked),
          hx(x['plaintext']),
          'unlocked',
        );
      });
    }
    for (final x in v['refusals'] as List) {
      await c.check('refused: ${x['why']}', () async {
        await refused(
          () async => unlockLocked(
            hx(x['key']),
            x['label'],
            hx(x['context']),
            hx(x['locked']),
          ),
          x['why'],
        );
      });
    }
    c.finish();
  });

  test('device_list.json', () async {
    final v = loadVector('device_list.json');
    final c = Cases('device_list.json');
    final chain = v['chain'] as List;
    for (var i = 0; i < chain.length; i++) {
      final x = chain[i];
      await c.check('chain version ${i + 1}', () async {
        final f = x['fields'];
        final l = DeviceList(
          format: f['format'],
          accountId: id(f['account_id']),
          server: f['server'],
          version: f['version'],
          prev: hx(f['prev']),
          reset: f['reset'],
          keyVersion: f['key_version'],
          accountKey: hx(f['account_key']),
          at: f['at'],
          devices: [
            for (final d in f['devices'] as List)
              ListDevice(
                deviceId: id(d['device_id']),
                signKey: hx(d['sign_key']),
                boxKey: hx(d['box_key']),
                platform: d['platform'],
                addedAt: d['added_at'],
                proof: hx(d['proof']),
              ),
          ],
        );
        sameBytes(l.encode(), hx(x['list']), 'list record');
        sameBytes(listHash(hx(x['list'])), hx(x['hash']), 'hash');
        final key = await SigningKey.fromSeed(hx(x['account_key_seed']));
        sameBytes(key.public, l.accountKey, 'account key from seed');
        final previous = x['prev_sig'] == null
            ? null
            : await SigningKey.fromSeed(hx(chain[i - 1]['account_key_seed']));
        final signed = await signList(hx(x['list']), key, previous);
        sameBytes(signed.sig, hx(x['sig']), 'sig');
        if (x['prev_sig'] != null) sameBytes(signed.prevSig!, hx(x['prev_sig']), 'prev_sig');
        sameBytes(signed.encode(), hx(x['signed_list']), 'signed_list');
        final given = SignedList.decode(hx(x['signed_list']));
        final String step;
        if (i == 0) {
          await checkFirstList(given);
          step = 'first';
        } else {
          step = (await checkListStep(hx(chain[i - 1]['list']), given)).$2;
        }
        eq(step, x['step'], 'step');
      });
    }
    for (final p in v['proofs'] as List) {
      await c.check('proof ${p['device']['device_id']}', () async {
        final d = p['device'];
        final k = await SigningKey.fromSeed(hx(d['sign_seed']));
        sameBytes(k.public, hx(d['sign_key']), 'sign key');
        sameBytes(
          x25519Public(hx(d['box_private'])),
          hx(d['box_key']),
          'box key',
        );
        sameBytes(
          proofBytes(
            id(d['account_id']),
            id(d['device_id']),
            k.public,
            hx(d['box_key']),
          ),
          hx(p['signed_record']),
          'signed record',
        );
        sameBytes(
          await deviceProof(
            k,
            id(d['account_id']),
            id(d['device_id']),
            hx(d['box_key']),
          ),
          hx(p['proof']),
          'proof',
        );
        yes(
          await verify(
            k.public,
            p['label'],
            hx(p['signed_record']),
            hx(p['proof']),
          ),
          'proof verifies',
        );
      });
    }
    for (final x in v['refusals'] as List) {
      await c.check('refused (rule ${x['rule']}): ${x['why']}', () async {
        try {
          await checkListStep(
            hx(x['pinned_list']),
            SignedList.decode(hx(x['signed_list'])),
          );
        } on ListError catch (e) {
          eq(e.rule, x['rule'], 'rule');
          return;
        }
        throw Mismatch('accepted');
      });
    }
    for (final x in v['first_contact'] as List) {
      await c.check('first contact: ${x['why']}', () async {
        var ok = true;
        try {
          await checkFirstList(SignedList.decode(hx(x['signed_list'])));
        } on ListError {
          ok = false;
        }
        eq(ok, x['accepted'], 'accepted');
      });
    }
    c.finish();
  });

  test('epoch_record.json', () async {
    final v = loadVector('epoch_record.json');
    final c = Cases('epoch_record.json');
    final conv = id(v['conversation_id']);
    final accounts = (v['accounts'] as List).map(id).toList();
    final devices = {
      for (final d in v['devices'] as List) id(d['device_id']): d,
    };
    final lists = {
      for (final l in v['lists_checked'] as List)
        '${l['account_id']}/${l['version']}': hx(l['list']),
    };
    Uint8List? listOf(Uuid a, int version) => lists['$a/$version'];
    Future<AcceptedEpoch> check(
      Uint8List stored,
      Uint8List? previous,
      Uuid device,
    ) => checkEpochRecord(
      stored: stored,
      conversationId: conv,
      accounts: accounts,
      previous: previous,
      listOf: listOf,
      deviceId: device,
      box: hx(devices[device]['box_private']),
    );
    final e1 = v['epoch_1'];
    final in1 = e1['inputs'];
    await c.check('epoch_1 from its inputs', () async {
      final maker = in1['maker'];
      final stored = await makeEpoch(
        conversationId: id(in1['conversation_id']),
        keyEpoch: in1['key_epoch'],
        makerAccountId: id(maker['account_id']),
        makerDeviceId: id(maker['device_id']),
        makerSign: await SigningKey.fromSeed(hx(maker['sign_seed'])),
        lists: [
          for (final l in in1['lists'] as List)
            EpochList(id(l['account_id']), hx(l['list'])),
        ],
        secret: hx(in1['epoch_secret']),
        oneTime: (d) => hx(in1['one_time_private']['$d']),
        at: in1['at'],
      );
      sameBytes(
        stored.sublist(0, stored.length - 64),
        hx(e1['record']),
        'record',
      );
      yes(
        await verify(
          hx(maker['sign_key']),
          labelEpoch,
          hx(e1['record']),
          hx(e1['signature']),
        ),
        "the vector's signature verifies",
      );
      sameBytes(stored, hx(e1['stored']), 'stored (record and signature)');
      sameBytes(epochHash(stored), hx(e1['hash']), 'hash');
    });
    Uint8List fromFields(Map f) => EpochRecord(
      format: f['format'],
      conversationId: id(f['conversation_id']),
      keyEpoch: f['key_epoch'],
      prev: hx(f['prev']),
      makerAccountId: id(f['maker_account_id']),
      makerDeviceId: id(f['maker_device_id']),
      accounts: [
        for (final a in f['accounts'] as List)
          AccountRef(
            id(a['account_id']),
            a['list_version'],
            hx(a['list_hash']),
          ),
      ],
      check: hx(f['check']),
      recipients: [
        for (final r in f['recipients'] as List)
          Recipient(id(r['account_id']), id(r['device_id']), hx(r['sealed'])),
      ],
      at: f['at'],
    ).encode();
    await c.check(
      'epoch_1 fields',
      () async => sameBytes(
        fromFields(e1['fields']),
        hx(e1['record']),
        'record from fields',
      ),
    );
    await c.check('epoch_1 derived', () async {
      final d = e1['derived'];
      final secret = hx(in1['epoch_secret']);
      sameBytes(epochSalt(conv, 1), hx(d['salt']), 'salt');
      final history = historySecret(secret, conv, 1);
      sameBytes(history, hx(d['history_secret']), 'history secret');
      final content = contentKey(history, conv, 1);
      sameBytes(content, hx(d['content_key']), 'content key');
      sameBytes(epochCheck(history, conv, 1), hx(d['check']), 'check');
      (d['sender_keys'] as Map).forEach(
        (dev, k) => sameBytes(
          senderKey(content, conv, 1, id(dev)),
          hx(k),
          'sender key $dev',
        ),
      );
      (d['seal_context'] as Map).forEach(
        (dev, k) => sameBytes(
          epochSealContext(conv, 1, id(dev)),
          hx(k),
          'seal context $dev',
        ),
      );
    });
    for (final r in e1['fields']['recipients'] as List) {
      await c.check('epoch_1 accepted by ${r['device_id']}', () async {
        final ok = await check(hx(e1['stored']), null, id(r['device_id']));
        sameBytes(ok.secret!, hx(in1['epoch_secret']), 'opened secret');
      });
    }
    final e2 = v['epoch_2'];
    await c.check('epoch_2', () async {
      sameBytes(
        fromFields(e2['fields']),
        hx(e2['record']),
        'record from fields',
      );
      sameBytes(
        cat([hx(e2['record']), hx(e2['signature'])]),
        hx(e2['stored']),
        'stored',
      );
      sameBytes(epochHash(hx(e2['stored'])), hx(e2['hash']), 'hash');
      sameBytes(
        hx(e2['fields']['prev']),
        hx(e1['hash']),
        'prev is the hash of epoch 1',
      );
      final ok = await check(
        hx(e2['stored']),
        hx(e1['stored']),
        id('0193aa00-0000-7000-8000-0000000000a2'),
      );
      yes(ok.secret != null, 'opened');
    });
    for (final x in v['refusals'] as List) {
      await c.check('refused (rule ${x['rule']}): ${x['why']}', () async {
        try {
          await check(
            hx(x['stored']),
            hx(x['previous_stored']),
            id(x['checking_device']),
          );
        } on EpochError catch (e) {
          eq(e.rule, x['rule'], 'rule');
          return;
        }
        throw Mismatch('accepted');
      });
    }
    final outsider = v['accepted_by_a_device_that_is_not_a_recipient'];
    await c.check('accepted by a device that is not a recipient', () async {
      final ok = await check(
        hx(outsider['stored']),
        null,
        id(outsider['checking_device']),
      );
      yes(ok.secret == null, 'no secret for a non-recipient');
    });
    c.finish();
  });

  test('message.json', () async {
    final v = loadVector('message.json');
    final ev = loadVector('epoch_record.json');
    final dl = loadVector('device_list.json');
    final c = Cases('message.json');
    final conv = id(v['conversation_id']);
    final devices = {
      for (final d in ev['devices'] as List) id(d['device_id']): d,
    };
    final content = contentKey(hx(v['history_secret']), conv, 1);
    await c.check(
      'content key',
      () async => sameBytes(content, hx(v['content_key']), 'content key'),
    );
    for (final m in v['messages'] as List) {
      await c.check('message ${m['kind']}', () async {
        final hf = m['header'];
        final h = Header(
          format: hf['format'],
          flags: hf['flags'],
          conversationId: id(hf['conversation_id']),
          keyEpoch: hf['key_epoch'],
          accountId: id(hf['account_id']),
          deviceId: id(hf['device_id']),
          counter: hf['counter'],
          reference: id(hf['reference']),
        );
        sameBytes(h.encode(), hx(hf['bytes']), 'header');
        eq(h.quiet, m['quiet'], 'quiet');
        sameBytes(
          utf8.encode(m['content']),
          hx(m['content_bytes']),
          'content bytes',
        );
        final sk = senderKey(content, conv, 1, h.deviceId);
        sameBytes(sk, hx(m['sender_key']), 'sender key');
        final signer = await SigningKey.fromSeed(
          hx(devices[h.deviceId]['sign_seed']),
        );
        sameBytes(signer.public, hx(m['sign_key']), 'sign key');
        final sealed = await sealMessage(
          h,
          sk,
          hx(m['salt']),
          hx(m['content_bytes']),
          signer,
        );
        final want = hx(m['sealed']);
        sameBytes(
          sealed.sublist(0, sealed.length - 64),
          want.sublist(0, want.length - 64),
          'sealed, without the signature',
        );
        sameBytes(sealed, want, 'sealed');
        final (_, opened) = await openMessage(want, sk, hx(m['sign_key']));
        sameBytes(opened, hx(m['content_bytes']), 'opened content');
      });
    }
    // The receiving device: Bob's phone, holding epoch 1 at seq 1.
    final bobId = id(v['receiving_device']);
    final bob = devices[bobId];
    final receiver = await Device.create(
      account: id(bob['account_id']),
      server: 'chat.example.org',
      platform: bob['platform'],
      id: bobId,
      random: GivenRandom([
        ('device_sign_seed', hx(bob['sign_seed'])),
        ('device_box_private', hx(bob['box_private'])),
      ]),
      now: () => 1800000006000,
    );
    final ann = id('0193a000-0000-7000-8000-00000000000a');
    await receiver.learnLists(ann, [
      SignedList.decode(hx((dl['chain'] as List)[1]['signed_list'])),
    ]);
    // Bob's version 1 is given as its record only; it is pinned directly.
    final bobList = hx(
      (ev['lists_checked'] as List).firstWhere(
        (l) => l['account_id'] == bob['account_id'],
      )['list'],
    );
    receiver.pins[receiver.account] = Pin(
      {
        1: PinnedList(
          SignedList(bobList, Uint8List(64)),
          DeviceList.decode(bobList),
        ),
      },
      1,
      1,
    );
    final chat = receiver.chat(conv, ann);
    await chat.acceptEpoch(1, v['epoch_seq'], hx(v['epoch_stored']));
    for (final x in v['receive'] as List) {
      await c.check('receive: ${x['why']}', () async {
        final r = x['row'];
        var outcome = 'refused';
        try {
          final got = await chat.receive(
            Row(
              seq: r['seq'],
              author: id(r['author_id']),
              keyEpoch: r['key_epoch'],
              quiet: r['quiet'],
              ciphertext: hx(r['ciphertext']),
            ),
          );
          outcome = got.dropped
              ? 'dropped'
              : got.outOfOrder
              ? 'shown_out_of_order'
              : got.gap
              ? 'shown_with_gap'
              : 'shown';
        } on MessageError {
          outcome = 'refused';
        }
        eq(outcome, x['outcome'], 'outcome');
      });
    }
    await c.check('shown after', () async {
      final view = chat.view();
      final want = v['shown_after'] as List;
      eq(view.length, want.length, 'messages shown');
      for (var i = 0; i < want.length; i++) {
        eq('${view[i].reference}', want[i]['reference'], 'reference $i');
        eq(view[i].text, want[i]['text'], 'text $i');
        if (want[i].containsKey('edited')) eq(view[i].edited, want[i]['edited'], 'edited $i');
        if (want[i].containsKey('out_of_order')) eq(view[i].outOfOrder, want[i]['out_of_order'], 'out of order $i');
      }
      eq(chat.unchecked, 7, 'rows shown as not checked');
    });
    c.finish();
  });

  test('file.json', () async {
    final v = loadVector('file.json');
    final c = Cases('file.json');
    final key = hx(v['key']);
    eq(v['chunk_size'], chunkSize, 'chunk size');
    for (final n in v['nonces'] as List) {
      await c.check(
        'nonce ${n['chunk']} ${n['last']}',
        () async => sameBytes(
          chunkNonce(n['chunk'], n['last']),
          hx(n['nonce']),
          'nonce',
        ),
      );
    }
    for (final f in v['files'] as List) {
      await c.check('file of ${f['size']} bytes', () async {
        final plain = filePattern(f['size']);
        sameBytes(sha256(plain), hx(f['plaintext_sha256']), 'plaintext');
        final blob = encryptFile(key, plain);
        eq(blob.length, f['blob_size'], 'blob size');
        if (f['blob'] != null) sameBytes(blob, hx(f['blob']), 'blob');
        sameBytes(
          blob.sublist(0, blob.length < 32 ? blob.length : 32),
          hx(f['blob_first_bytes']),
          'first bytes',
        );
        sameBytes(
          blob.sublist(blob.length < 32 ? 0 : blob.length - 32),
          hx(f['blob_last_bytes']),
          'last bytes',
        );
        sameBytes(sha256(blob), hx(f['blob_sha256']), 'blob sha256');
        eq(
          (blob.length + sealedChunkSize - 1) ~/ sealedChunkSize,
          f['chunks'],
          'chunks',
        );
        // As streams, fed in pieces that do not line up with chunks.
        final streamed = await collect(
          encryptFileStream(key, pieces(plain, 7777)),
        );
        sameBytes(streamed, blob, 'streamed blob');
        sameBytes(decryptFile(key, blob), plain, 'decrypted');
        sameBytes(
          await collect(decryptFileStream(key, pieces(blob, 10007))),
          plain,
          'stream-decrypted',
        );
      });
    }
    final big = encryptFile(key, filePattern(200000));
    final blobs = {
      'chunks reordered': cat([
        big.sublist(sealedChunkSize, 2 * sealedChunkSize),
        big.sublist(0, sealedChunkSize),
        big.sublist(2 * sealedChunkSize),
      ]),
      'the file cut short': big.sublist(0, 3 * sealedChunkSize),
      'the file cut short to one chunk': encryptFile(
        key,
        filePattern(65537),
      ).sublist(0, sealedChunkSize),
      'a missing last-chunk mark': encryptChunk(key, 0, false, filePattern(1)),
      'a changed byte': encryptFile(key, filePattern(1))..[0] ^= 1,
      'shorter than a tag': Uint8List(15),
    };
    for (final x in v['refusals'] as List) {
      await c.check('refused: ${x['why']}', () async {
        final blob = blobs[x['why']]!;
        eq(blob.length, x['blob_size'], 'blob size');
        sameBytes(sha256(blob), hx(x['blob_sha256']), 'blob sha256');
        if (x['blob'] != null) sameBytes(blob, hx(x['blob']), 'blob');
        await refused(() async => decryptFile(key, blob), x['why']);
        await refused(
          () => collect(decryptFileStream(key, pieces(blob, 4096))),
          '${x['why']} (stream)',
        );
      });
    }
    c.finish();
  });

  test('account_record.json', () async {
    final v = loadVector('account_record.json');
    final c = Cases('account_record.json');
    final f = v['fields'];
    final rec = AccountRecord(
      format: f['format'],
      accountId: id(f['account_id']),
      keyVersion: f['key_version'],
      revision: f['revision'],
      accountKey: hx(f['account_key']),
      accountKeyPrivate: hx(f['account_key_private']),
      ways: [
        for (final w in f['ways'] as List)
          Way(w['kind'], id(w['id']), hx(w['unlock_public'])),
      ],
      at: f['at'],
    );
    await c.check('content and stored', () async {
      sameBytes(rec.encode(), hx(v['content']), 'content');
      sameBytes(
        accountRecordContext(rec.accountId, rec.keyVersion, rec.revision),
        hx(v['context']),
        'context',
      );
      final stored = lockAccountRecord(hx(v['backup_key']), rec, hx(v['salt']));
      sameBytes(stored, hx(v['stored']), 'stored');
      final back = await openAccountRecord(
        hx(v['backup_key']),
        rec.accountId,
        rec.keyVersion,
        rec.revision,
        stored,
      );
      sameBytes(back.encode(), hx(v['content']), 'opened');
    });
    for (final x in v['refusals'] as List) {
      await c.check('refused: ${x['why']}', () async {
        await refused(
          () => openAccountRecord(
            hx(x['backup_key']),
            id(x['account_id']),
            x['key_version'],
            x['revision'],
            hx(x['stored']),
          ),
          x['why'],
        );
      });
    }
    c.finish();
  });

  test('backup_record.json', () async {
    final v = loadVector('backup_record.json');
    final c = Cases('backup_record.json');
    final f = v['fields'];
    final account = id(v['account_id']);
    final br = BackupRecord(
      format: f['format'],
      conversationId: id(f['conversation_id']),
      keyEpoch: f['key_epoch'],
      secret: hx(f['secret']),
      at: f['at'],
    );
    await c.check('content and stored', () async {
      sameBytes(br.encode(), hx(v['content']), 'content');
      sameBytes(
        backupRecordContext(
          account,
          br.conversationId,
          br.keyEpoch,
          v['key_version'],
        ),
        hx(v['context']),
        'context',
      );
      final stored = lockBackupRecord(
        hx(v['backup_key']),
        account,
        v['key_version'],
        br,
        hx(v['salt']),
      );
      eq(stored.length, 97, 'stored size');
      sameBytes(stored, hx(v['stored']), 'stored');
      final back = openBackupRecord(
        hx(v['backup_key']),
        account,
        br.conversationId,
        br.keyEpoch,
        v['key_version'],
        stored,
      );
      sameBytes(back.secret, br.secret, 'secret');
      sameBytes(
        epochCheck(back.secret, br.conversationId, br.keyEpoch),
        hx(v['epoch_check']),
        'epoch check',
      );
    });
    for (final x in v['refusals'] as List) {
      await c.check('refused: ${x['why']}', () async {
        await refused(
          () async => openBackupRecord(
            hx(v['backup_key']),
            id(x['account_id']),
            id(x['conversation_id']),
            x['key_epoch'],
            x['key_version'],
            hx(x['stored']),
          ),
          x['why'],
        );
      });
    }
    await c.check('opens but fails the epoch check', () async {
      final o = v['opens_but_fails_the_epoch_check'];
      final back = openBackupRecord(
        hx(v['backup_key']),
        account,
        br.conversationId,
        1,
        v['key_version'],
        hx(o['stored']),
      );
      sameBytes(back.secret, hx(o['secret']), 'secret');
      yes(
        !bytesEqual(
          epochCheck(back.secret, br.conversationId, 1),
          hx(v['epoch_check']),
        ),
        'refused at the epoch check',
      );
    });
    c.finish();
  });

  test('locked_copy.json', () async {
    final v = loadVector('locked_copy.json');
    final c = Cases('locked_copy.json');
    final account = id(v['account_id']);
    final kv = v['key_version'] as int;
    final accountKey = await SigningKey.fromSeed(hx(v['account_key_seed']));
    await c.check(
      'account key',
      () async =>
          sameBytes(accountKey.public, hx(v['account_key']), 'account key'),
    );
    for (final x in v['copies'] as List) {
      await c.check('copy ${x['way']}', () async {
        final kind = x['kind'] as int;
        final wid = id(x['id']);
        sameBytes(wayTag(kind, wid), hx(x['way_tag']), 'way tag');
        sameBytes(
          lockedCopyContext(account, kv, kind, wid),
          hx(x['context']),
          'context',
        );
        final pub = x25519Public(hx(x['unlock_private']));
        sameBytes(pub, hx(x['unlock_public']), 'unlock public');
        final cp = await makeLockedCopy(
          pub,
          account,
          kv,
          kind,
          wid,
          hx(v['backup_key']),
          accountKey,
          hx(x['one_time_private']),
        );
        sameBytes(cp.sublist(0, 80), hx(x['sealed']), 'sealed');
        sameBytes(
          cat([hx(x['context']), hx(x['sealed'])]),
          hx(x['signed_record']),
          'signed record',
        );
        yes(
          await verify(
            accountKey.public,
            labelLockedCopy,
            hx(x['signed_record']),
            hx(x['signature']),
          ),
          "the vector's signature verifies",
        );
        sameBytes(cp, hx(x['stored']), 'stored');
        sameBytes(
          openLockedCopy(
            hx(x['unlock_private']),
            account,
            kv,
            kind,
            wid,
            hx(x['stored']),
          ),
          hx(v['backup_key']),
          'opened',
        );
        yes(
          await verifyLockedCopy(
            accountKey.public,
            account,
            kv,
            kind,
            wid,
            hx(x['stored']),
          ),
          'copy verifies',
        );
      });
    }
    for (final x in v['refusals'] as List) {
      await c.check('refused: ${x['why']}', () async {
        await refused(
          () async => openLockedCopy(
            hx(x['unlock_private']),
            account,
            x['key_version'],
            x['kind'],
            id(x['id']),
            hx(x['stored']),
          ),
          x['why'],
        );
      });
    }
    await c.check('opens but is not signed by the account key', () async {
      final o = v['opens_but_is_not_signed_by_the_account_key'];
      openLockedCopy(
        hx(o['unlock_private']),
        account,
        o['key_version'],
        o['kind'],
        id(o['id']),
        hx(o['stored']),
      );
      yes(
        !await verifyLockedCopy(
          hx(o['account_key']),
          account,
          o['key_version'],
          o['kind'],
          id(o['id']),
          hx(o['stored']),
        ),
        'refused at the signature check',
      );
    });
    c.finish();
  });

  test('unlock_keys.json', () async {
    final v = loadVector('unlock_keys.json');
    final c = Cases('unlock_keys.json');
    final account = id(v['account_id']);
    await c.check(
      'prf input',
      () async =>
          sameBytes(prfInput(), hx(v['prf_input']['bytes']), 'prf input'),
    );
    await c.check('passkey', () async {
      final p = v['passkey'];
      sameBytes(
        labelled(labelUnlockPasskey, [account.bytes, hx(p['credential_id'])]),
        hx(p['info']),
        'info',
      );
      final priv = unlockKeyFromPrf(
        hx(p['prf_output']),
        account,
        hx(p['credential_id']),
      );
      sameBytes(priv, hx(p['unlock_private']), 'private');
      sameBytes(x25519Public(priv), hx(p['unlock_public']), 'public');
    });
    await c.check('phrase', () async {
      final p = v['phrase'];
      eq(phraseOf(hx(p['entropy'])), p['words'], 'words');
      sameBytes(readPhrase(p['words']), hx(p['entropy']), 'entropy');
      sameBytes(
        labelled(labelUnlockPhrase, [account.bytes]),
        hx(p['info']),
        'info',
      );
      final priv = unlockKeyFromPhrase(hx(p['entropy']), account);
      sameBytes(priv, hx(p['unlock_private']), 'private');
      sameBytes(x25519Public(priv), hx(p['unlock_public']), 'public');
    });
    for (final x in v['phrases'] as List) {
      await c.check('phrase ${x['entropy']}', () async {
        eq(phraseOf(hx(x['entropy'])), x['words'], 'words');
        sameBytes(readPhrase(x['words']), hx(x['entropy']), 'entropy');
      });
    }
    for (final x in v['reading'] as List) {
      await c.check('reading: ${x['why']}', () async {
        Uint8List? got;
        try {
          got = readPhrase(x['typed']);
        } on PhraseError {
          got = null;
        }
        eq(got != null, x['accepted'], 'accepted');
        if (got != null) sameBytes(got, hx(x['entropy']), 'entropy');
      });
    }
    c.finish();
  });

  test('handover.json', () async {
    final v = loadVector('handover.json');
    final c = Cases('handover.json');
    final account = id(v['account_id']);
    final app = hx(v['app_box_private']);
    await c.check(
      'app box key',
      () async =>
          sameBytes(x25519Public(app), hx(v['app_box_key']), 'app box key'),
    );
    final s = v['sign_in'];
    await c.check('sign-in hand-over', () async {
      sameBytes(
        cat([account.bytes, ascii.encode(s['code_challenge'])]),
        hx(s['context']),
        'context',
      );
      final ho = sealHandover(
        hx(v['app_box_key']),
        account,
        s['code_challenge'],
        hx(v['backup_key']),
        hx(s['one_time_private']),
      );
      eq(ho.length, 80, 'size');
      sameBytes(ho, hx(s['handover']), 'handover');
      sameBytes(
        openHandover(app, account, s['code_challenge'], ho),
        hx(v['backup_key']),
        'opened',
      );
    });
    await c.check('sign-in hand-over refused with another challenge', () async {
      await refused(
        () async => openHandover(
          app,
          account,
          s['refused_with_code_challenge'],
          hx(s['handover']),
        ),
        'another challenge',
      );
    });
    final p = v['pairing'];
    await c.check('pairing hand-over', () async {
      sameBytes(
        seal(
          hx(v['app_box_key']),
          labelSealPairing,
          account.bytes,
          hx(v['backup_key']),
          hx(p['one_time_private']),
        ),
        hx(p['inner']),
        'inner',
      );
      final ho = sealPairingHandover(
        hx(v['app_box_key']),
        hx(p['pairing_secret']),
        account,
        hx(v['backup_key']),
        hx(p['one_time_private']),
        hx(p['salt']),
      );
      eq(ho.length, 112, 'size');
      sameBytes(ho, hx(p['handover']), 'handover');
      sameBytes(
        openPairingHandover(app, hx(p['pairing_secret']), account, ho),
        hx(v['backup_key']),
        'opened',
      );
    });
    await c.check('pairing hand-over refused with another secret', () async {
      await refused(
        () async => openPairingHandover(
          app,
          hx(p['refused_with_pairing_secret']),
          account,
          hx(p['handover']),
        ),
        'another secret',
      );
    });
    c.finish();
  });

  test('unlock_request.json', () async {
    final v = loadVector('unlock_request.json');
    final c = Cases('unlock_request.json');
    final f = v['fields'];
    await c.check('request and code', () async {
      final u = UnlockRequest(
        format: f['format'],
        accountId: id(f['account_id']),
        deviceId: id(f['device_id']),
        signKey: hx(f['sign_key']),
        boxKey: hx(f['box_key']),
        platform: f['platform'],
        at: f['at'],
        proof: hx(f['proof']),
      );
      sameBytes(u.encode(), hx(v['request']), 'request');
      await checkUnlockRequest(hx(v['request']));
      sameBytes(
        hashOf(labelDeviceApproval, hx(v['request'])),
        hx(v['code_hash']),
        'code hash',
      );
      eq(approvalCode(hx(v['request'])), v['code'], 'code');
    });
    for (final t in v['typed_forms_that_match'] as List) {
      await c.check(
        'typed "$t"',
        () async => eq(normalizeApprovalCode(t), v['code'], 'normalised'),
      );
    }
    await c.check('answer', () async {
      final a = v['answer'];
      final key = await SigningKey.fromSeed(hx(a['account_key_seed']));
      final cp = await makeLockedCopy(
        hx(f['box_key']),
        id(f['account_id']),
        a['key_version'],
        wayDevice,
        id(f['device_id']),
        hx(a['backup_key']),
        key,
        hx(a['one_time_private']),
      );
      sameBytes(cp, hx(a['locked_copy']), 'locked copy');
      sameBytes(
        openLockedCopy(
          hx(a['device_box_private']),
          id(f['account_id']),
          a['key_version'],
          wayDevice,
          id(f['device_id']),
          cp,
        ),
        hx(a['backup_key']),
        'opened',
      );
    });
    for (final x in v['refusals'] as List) {
      await c.check('refused: ${x['why']}', () async {
        await refused(() => checkUnlockRequest(hx(x['request'])), x['why']);
        eq(approvalCode(hx(x['request'])), x['code'], 'its code');
        yes(x['code'] != v['code'], 'another request has another code');
      });
    }
    c.finish();
  });

  test('comparison_code.json', () async {
    final v = loadVector('comparison_code.json');
    final c = Cases('comparison_code.json');
    eq(v['rounds'], compareRounds, 'rounds');
    final halves = v['halves'] as List;
    for (final h in halves) {
      await c.check('half ${h['account_id']} ${h['server']}', () async {
        if (h['first_hash'] != null) {
          sameBytes(
            hashOf(
              labelCompare,
              cat([
                hx(h['account_key']),
                id(h['account_id']).bytes,
                utf8.encode(h['server']),
              ]),
            ),
            hx(h['first_hash']),
            'first hash',
          );
        }
        eq(
          compareHalf(hx(h['account_key']), id(h['account_id']), h['server']),
          h['half'],
          'half',
        );
      });
    }
    await c.check('code', () async {
      eq(compareCode(halves[0]['half'], halves[1]['half']), v['code'], 'code');
      eq(
        compareCode(halves[1]['half'], halves[0]['half']),
        v['code'],
        'from the other side',
      );
    });
    c.finish();
  });

  test('call_keys.json', () async {
    final v = loadVector('call_keys.json');
    final c = Cases('call_keys.json');
    final conv = id(v['conversation_id']);
    final k = v['key_epoch'] as int;
    final secret = callSecret(hx(v['epoch_secret']), conv, k, id(v['call_id']));
    await c.check('salt and call secret', () async {
      sameBytes(epochSalt(conv, k), hx(v['salt']), 'salt');
      sameBytes(secret, hx(v['call_secret']), 'call secret');
    });
    for (final e in (v['call_sender_keys'] as Map).entries) {
      await c.check(
        'call sender key ${e.key}',
        () async => sameBytes(
          callSenderKey(secret, conv, k, id(e.key)),
          hx(e.value),
          'key',
        ),
      );
    }
    for (final x in v['key_index'] as List) {
      await c.check(
        'key index ${x['key_epoch']}',
        () async => eq(callKeyIndex(x['key_epoch']), x['index'], 'index'),
      );
    }
    c.finish();
  });
}
