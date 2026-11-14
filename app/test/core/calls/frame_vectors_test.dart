// Call frames of a sealed chat's call against testdata/call-frames/
// vectors.json (docs/CALL-FRAMES.md): the file the Go reference writes and
// the web chat's worker is checked against too.
//
// The app does not encrypt frames in Dart: flutter_webrtc's frame cryptor
// does, natively, from the call sender key and the settings in
// lib/core/calls/frame_crypto.dart. What is checked here is everything
// the app decides: the keys it derives from an epoch secret, the salt and
// lengths it hands the plugin, how many bytes stay readable, and (with
// AES-GCM in pure Dart standing where the plugin stands) that those give
// the file's bytes exactly. The file's first frame is one the plugin
// itself was seen to send.
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:churr/core/calls/call_media.dart';
import 'package:churr/core/calls/frame_crypto.dart';
import 'package:churr/core/calls/sealed_call_keys.dart';
import 'package:churr/core/sealed/sealed.dart';
import 'package:cryptography/cryptography.dart'
    show Mac, SecretBox, SecretKeyData;
import 'package:cryptography/dart.dart' show DartAesGcm;
import 'package:flutter_test/flutter_test.dart';

import '../sealed/negative_test.dart' show World, annId, bobId;
import '../sealed/vectors_helper.dart';

Map<String, dynamic> frameVectors() {
  for (final p in ['../testdata', 'testdata', '../../testdata']) {
    final f = File('$p/call-frames/vectors.json');
    if (f.existsSync()) {
      return jsonDecode(f.readAsStringSync()) as Map<String, dynamic>;
    }
  }
  throw StateError('testdata/call-frames/vectors.json not found');
}

/// The AES key as the plugin's key provider derives it.
Uint8List frameKey(List<int> callSenderKey) => hkdf(
  callSenderKey,
  callFrameRatchetSalt,
  Uint8List(callFrameInfoLength),
  callFrameKeyLength,
);

final _gcm = DartAesGcm.with128bits();
const _trailer = 14, _tag = 16;

Uint8List sealFrame(
  List<int> key,
  int index,
  List<int> iv,
  bool audio,
  List<int> plain,
) {
  final n = callFrameHeaderLength(audio: audio, frame: plain);
  final box = _gcm.encryptSync(
    plain.sublist(n),
    secretKeyData: SecretKeyData(key),
    nonce: iv,
    aad: plain.sublist(0, n),
  );
  return Uint8List.fromList([
    ...plain.sublist(0, n),
    ...box.cipherText,
    ...box.mac.bytes,
    ...iv,
    iv.length,
    index,
  ]);
}

/// What a receiver holding [key] at index [held] makes of a frame: the
/// frame as it was, or null (dropped).
Uint8List? openFrame(List<int> key, int held, bool audio, List<int> frame) {
  if (frame.length < _tag + _trailer || frame[frame.length - 2] != 12) {
    return null;
  }
  final n = callFrameHeaderLength(audio: audio, frame: frame);
  if (frame.length < n + _tag + _trailer ||
      frame.last != held ||
      held >= callFrameKeyRing) {
    return null;
  }
  final ivAt = frame.length - _trailer;
  try {
    final plain = _gcm.decryptSync(
      SecretBox(
        frame.sublist(n, ivAt - _tag),
        nonce: frame.sublist(ivAt, ivAt + 12),
        mac: Mac(frame.sublist(ivAt - _tag, ivAt)),
      ),
      secretKeyData: SecretKeyData(key),
      aad: frame.sublist(0, n),
    );
    return Uint8List.fromList([...frame.sublist(0, n), ...plain]);
  } on Object {
    return null;
  }
}

void main() {
  final v = frameVectors();

  test('the settings handed to the plugin are the file\'s', () {
    expect(callFrameRatchetSaltText, v['salt']);
    expect(callFrameInfoLength, v['info_length']);
    expect(callFrameUsesHkdf, isTrue);
    expect(callFrameKeyLength, 16);
  });

  test('from an epoch secret to each device\'s keys', () async {
    final c = Cases('call-frames epochs');
    final conv = id(v['conversation_id']), call = id(v['call_id']);
    for (final e in v['epochs'] as List) {
      final k = e['key_epoch'] as int;
      final secret = callSecret(hx(e['epoch_secret']), conv, k, call);
      await c.check('epoch $k', () async {
        sameBytes(secret, hx(e['call_secret']), 'call secret');
        eq(callKeyIndex(k), e['key_index'], 'key index');
      });
      for (final d in e['devices'] as List) {
        await c.check('epoch $k device ${d['device_id']}', () async {
          final sender = callSenderKey(secret, conv, k, id(d['device_id']));
          sameBytes(sender, hx(d['call_sender_key']), 'call sender key');
          sameBytes(frameKey(sender), hx(d['aes_key']), 'AES key');
        });
      }
    }
    c.finish();
  });

  test('frames: sealed to the file\'s bytes and opened again', () async {
    final c = Cases('call-frames frames');
    for (final f in v['frames'] as List) {
      final audio = f['kind'] == 'audio';
      final plain = hx(f['plain']), frame = hx(f['frame']);
      final key = frameKey(hx(f['call_sender_key']));
      final index = f['key_index'] as int;
      await c.check('${f['name']}: key and header', () async {
        sameBytes(key, hx(f['aes_key']), 'AES key');
        eq(
          callFrameHeaderLength(audio: audio, frame: plain),
          f['header_length'],
          'header length of the frame',
        );
        eq(
          callFrameHeaderLength(audio: audio, frame: frame),
          f['header_length'],
          'header length of the encrypted frame',
        );
      });
      await c.check('${f['name']}: seal', () async {
        sameBytes(
          sealFrame(key, index, hx(f['iv']), audio, plain),
          frame,
          'frame',
        );
      });
      await c.check('${f['name']}: open', () async {
        final got = openFrame(key, index, audio, frame);
        yes(got != null, 'the frame did not open');
        sameBytes(got!, plain, 'plain');
      });
    }
    c.finish();
  });

  test('frames that must be dropped', () async {
    final c = Cases('call-frames refused');
    for (final r in v['refused'] as List) {
      await c.check('${r['name']}', () async {
        final got = openFrame(
          frameKey(hx(r['call_sender_key'])),
          r['key_index_held'] as int,
          r['kind'] == 'audio',
          hx(r['frame']),
        );
        yes(got == null, 'opened, and should have been dropped');
      });
    }
    c.finish();
  });

  test('sealedFrameKeys: every device of the epoch, by device id, and the '
      'two sides agree', () async {
    final w = await World.make();
    const call = '0193a0ca-0000-7000-8000-00000000ca11';
    final a = sealedFrameKeys(w.annChat, w.ann.id, call)!;
    final b = sealedFrameKeys(w.bobChat, w.bob.id, call)!;
    expect(a.ownDevice, w.ann.id.toString());
    expect(b.ownDevice, w.bob.id.toString());
    expect(a.keys.keys.toSet(), {w.ann.id.toString(), w.bob.id.toString()});
    // What Ann decrypts Bob's device with is what Bob encrypts with.
    expect(a.keys[b.ownDevice], b.ownKey);
    expect(b.keys[a.ownDevice], a.ownKey);
    expect(a.ownKey, isNot(b.ownKey));
    expect(a.keyEpoch, 1);
    expect(a.keyIndex, 1);
    expect(a.soleDevice, {
      annId.toString(): w.ann.id.toString(),
      bobId.toString(): w.bob.id.toString(),
    });
    // Another call in the same epoch has other keys.
    final other = sealedFrameKeys(
      w.annChat,
      w.ann.id,
      '0193a0ca-0000-7000-8000-00000000ca12',
    )!;
    expect(other.ownKey, isNot(a.ownKey));

    // A new epoch: new keys under the next index, still agreeing.
    final (stored, k) = await w.bobChat.makeEpoch();
    await w.post(stored, k);
    final a2 = sealedFrameKeys(w.annChat, w.ann.id, call)!;
    final b2 = sealedFrameKeys(w.bobChat, w.bob.id, call)!;
    expect((a2.keyEpoch, a2.keyIndex), (2, 2));
    expect(a2.ownKey, isNot(a.ownKey));
    expect(a2.keys[b2.ownDevice], b2.ownKey);

    // A device that is not in the epoch has no keys.
    expect(
      sealedFrameKeys(
        w.annChat,
        id('0193aa00-0000-7000-8000-0000000000ff'),
        call,
      ),
      isNull,
    );
    expect(sealedFrameKeys(w.annChat, w.ann.id, 'not a call id'), isNull);
  });

  group('whose key decrypts a line', () {
    const keys = FrameKeys(
      ownDevice: 'own',
      keys: {
        'own': [1],
        'phone': [2],
        'laptop': [3],
        'solo': [4],
      },
      keyIndex: 1,
      soleDevice: {'bob': 'solo'},
    );
    test('the device the server names', () {
      expect(
        frameParticipant(keys, accountId: 'ann', deviceId: 'laptop'),
        'laptop',
      );
      // Named but not in the epoch yet: its own name, so a later epoch
      // that has it starts decrypting without anything else changing.
      expect(frameParticipant(keys, accountId: 'ann', deviceId: 'new'), 'new');
      // The server's word wins over the guess.
      expect(
        frameParticipant(keys, accountId: 'bob', deviceId: 'phone'),
        'phone',
      );
    });
    test('no device named: the account\'s one device, or no key at all', () {
      expect(frameParticipant(keys, accountId: 'bob'), 'solo');
      final none = frameParticipant(keys, accountId: 'ann');
      expect(keys.keys.containsKey(none), isFalse);
    });
  });

  group('a new epoch', () {
    const e1 = FrameKeys(
      ownDevice: 'own',
      keys: {
        'own': [1],
        'phone': [2],
        'laptop': [3],
      },
      keyIndex: 1,
      keyEpoch: 1,
    );
    test('the first keys: everything set, nothing to forget', () {
      final c = frameKeyChange(null, e1);
      expect(c.set.map((s) => '${s.device} ${s.index} ${s.key}'), [
        'own 1 [1]',
        'phone 1 [2]',
        'laptop 1 [3]',
      ]);
      expect(c.forget, isEmpty);
    });
    test('new keys at the new index; the others\' old keys go after ten '
        'seconds, a removed device\'s too', () {
      const e2 = FrameKeys(
        ownDevice: 'own',
        keys: {
          'own': [11],
          'phone': [12],
        },
        keyIndex: 2,
        keyEpoch: 2,
      );
      final c = frameKeyChange(e1, e2);
      expect(c.set.map((s) => (s.device, s.index)), [('own', 2), ('phone', 2)]);
      expect(c.forget.map((s) => (s.device, s.index)), [
        ('phone', 1),
        ('laptop', 1),
      ]);
      expect(callFrameKeepOldKeys, const Duration(seconds: 10));
    });
    test('sixteen epochs on the index is the same: replaced at once, and '
        'only a device left out is forgotten', () {
      const e17 = FrameKeys(
        ownDevice: 'own',
        keys: {
          'own': [21],
          'phone': [22],
        },
        keyIndex: 17,
        keyEpoch: 17,
      );
      final c = frameKeyChange(e1, e17);
      expect(c.set.map((s) => (s.device, s.index)), [('own', 1), ('phone', 1)]);
      expect(c.forget.map((s) => (s.device, s.index)), [('laptop', 1)]);
    });
  });
}
