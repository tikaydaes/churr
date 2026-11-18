// The proof that protobuf gives the app exactly what JSON does
// (docs/WIRE-PROTOBUF.md). The vectors under testdata/wirepb are written by
// the server's own codec (internal/wirepb): for every frame of the live
// connection and every message of the API, a JSON value and its protobuf
// bytes. Reading the bytes here must give what `jsonDecode` gives for the
// JSON, value for value and type for type, so every model reads the same
// thing whichever way it travelled. Writing must give the server's bytes.

import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:churr/core/models/account.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/core/wire/gen/churr.pb.dart' as pb;
import 'package:churr/core/wire/schema.g.dart';
import 'package:churr/core/wire/wire.dart';
import 'package:flutter_test/flutter_test.dart';

/// Where two JSON values differ, or null if they are the same. An int and
/// a double are different even when equal: the models tell them apart.
String? difference(Object? a, Object? b, [String at = r'$']) {
  if (a is Map && b is Map) {
    for (final k in {...a.keys, ...b.keys}) {
      if (!a.containsKey(k)) return '$at.$k is missing on the left';
      if (!b.containsKey(k)) return '$at.$k is missing on the right';
      final d = difference(a[k], b[k], '$at.$k');
      if (d != null) return d;
    }
    return null;
  }
  if (a is List && b is List) {
    if (a.length != b.length) {
      return '$at: ${a.length} items against ${b.length}';
    }
    for (var i = 0; i < a.length; i++) {
      final d = difference(a[i], b[i], '$at[$i]');
      if (d != null) return d;
    }
    return null;
  }
  if (a.runtimeType != b.runtimeType || a != b) {
    return '$at: $a (${a.runtimeType}) against $b (${b.runtimeType})';
  }
  return null;
}

List<Map<String, dynamic>> vectors(String name) => [
  for (final line in File('../testdata/wirepb/$name').readAsLinesSync())
    if (line.isNotEmpty) jsonDecode(line) as Map<String, dynamic>,
];

void main() {
  test('every frame reads as its JSON and writes as the server writes it', () {
    final seen = <String>{};
    final all = vectors('frames.jsonl');
    for (final v in all) {
      final name = v['name'] as String;
      final json = v['json'] as Map<String, dynamic>;
      final bytes = base64.decode(v['pb'] as String);
      seen.add(json['type'] as String);
      expect(difference(Wire.decodeFrame(bytes), json), isNull, reason: name);
      expect(Wire.encodeFrame(json), bytes, reason: 'writing $name');
    }
    for (final f in wireFrames) {
      expect(seen, contains(f.type));
    }
    expect(all.length, greaterThan(300));
  });

  test('every message of the API reads as its JSON and writes the same', () {
    final seen = <String>{};
    for (final v in vectors('bodies.jsonl')) {
      final name = v['name'] as String;
      final index = Wire.messageNamed(v['message'] as String)!;
      final bytes = base64.decode(v['pb'] as String);
      seen.add(v['message'] as String);
      expect(
        difference(Wire.decodeMessage(index, bytes), v['json']),
        isNull,
        reason: name,
      );
      expect(
        Wire.encodeMessage(index, v['json']),
        bytes,
        reason: 'writing $name',
      );
    }
    for (final m in wireMessages) {
      if (m.shape.name == 'object') expect(seen, contains(m.name));
    }
  });

  test('what this build has never heard of is skipped or carried', () {
    for (final v in vectors('unknown.jsonl')) {
      final got = Wire.decodeFrame(base64.decode(v['pb'] as String));
      if (v['skip'] == true) {
        expect(got, isNull, reason: v['name'] as String);
      } else {
        expect(difference(got, v['json']), isNull, reason: v['name'] as String);
      }
    }
    // Writing: a frame type or a field the schema lacks goes as JSON and
    // comes back whole.
    for (final frame in <Map<String, dynamic>>[
      {
        'type': 'future.happened',
        'n': 1,
        'list': [null, 2.5],
      },
      {'type': 'ping', 'surprise': true},
      {'type': 'typing', 'conversation_id': 'not-an-id'},
      {'no_type': 1},
    ]) {
      final env = pb.Envelope.fromBuffer(Wire.encodeFrame(frame));
      expect(env.hasJson(), isTrue, reason: '$frame');
      expect(difference(Wire.decodeFrame(env.writeToBuffer()), frame), isNull);
    }
    // A request body that does not fit is not sent as protobuf at all.
    expect(
      Wire.encodeRequest('postMessage', {'text': 'hi', 'surprise': 1}),
      isNull,
    );
    expect(Wire.encodeRequest('postMessage', {'text': 7}), isNull);
    expect(Wire.encodeRequest('getMe', {}), isNull);
    expect(Wire.encodeRequest('postMessage', {'text': 'hi'}), isNotNull);
    // Bytes that are not protobuf are an error, not a crash.
    expect(
      () => Wire.decodeFrame(Uint8List.fromList([0xff])),
      throwsFormatException,
    );
  });

  // A value of the catalogue (internal/wirepb TestKinds): its data is its
  // kind's object in JSON and that kind's message as bytes on the wire,
  // and data this build cannot read is kept as the bytes it came as.
  test('a value\'s data is its kind\'s object, or the bytes it came as', () {
    final roll = {
      'dice': '2d6+3, 4d6kh3',
      'rows': [
        {
          'dice': '2d6+3',
          'terms': [
            {
              'sign': 1,
              'count': 2,
              'sides': 6,
              'rolls': [4, 1],
              'value': 5,
            },
            {'sign': 1, 'value': 3},
          ],
          'total': 8,
        },
        {
          'dice': '4d6kh3',
          'terms': [
            {
              'sign': 1,
              'count': 4,
              'sides': 6,
              'keep': 'kh',
              'keep_n': 3,
              'rolls': [6, 1, 5, 2],
              'dropped': [1],
              'value': 13,
            },
          ],
          'total': 13,
        },
      ],
    };
    expect(
      wireValueKinds.map(
        (k) => (k.name, k.level, wireMessages[k.message].name),
      ),
      [('dice_roll', 1, 'DiceRoll')],
    );
    final mi = Wire.messageNamed('ValueEnvelope')!;
    final envelope = {
      'kind': 'dice_roll',
      'level': 1,
      'by': 'dice',
      'data': roll,
    };
    final bytes = Wire.encodeMessage(mi, envelope)!;
    final onWire = pb.ValueEnvelope.fromBuffer(bytes);
    final asRoll = pb.DiceRoll.fromBuffer(onWire.data);
    expect(asRoll.unknownFields.isEmpty, isTrue);
    expect(asRoll.rows.length, 2);
    expect(difference(Wire.decodeMessage(mi, bytes), envelope), isNull);

    // As a client is sent it: a message's view and an object entity.
    final msg = {
      'id': '0193a6f2-7c1e-7b3a-9f0e-000000000001',
      'text': 'Rolled',
      'view': envelope,
      'entities': [
        {'type': 'object', 'at': 0, 'len': 6, 'object': envelope},
      ],
    };
    final mm = Wire.messageNamed('Message')!;
    final mb = Wire.encodeMessage(mm, msg)!;
    expect(difference(Wire.decodeMessage(mm, mb), msg), isNull);
    final m = Message.fromJson(
      (Wire.decodeMessage(mm, mb) as Map).cast<String, dynamic>(),
    );
    expect((m.raw['view'] as Map)['data'], roll);
    expect(m.entities.single.raw['object'], envelope);

    // An object of a kind this build does not know has no protobuf form.
    expect(
      Wire.encodeMessage(mi, {
        'kind': 'tarot_spread',
        'level': 4,
        'data': {
          'cards': [1, 2],
        },
      }),
      isNull,
    );
    final frame = {
      'type': 'message.created',
      'at': '2026-11-13T09:30:00.000Z',
      'message': {
        'id': '0193a6f2-7c1e-7b3a-9f0e-000000000001',
        'text': 'x',
        'view': {
          'kind': 'tarot_spread',
          'level': 4,
          'data': {
            'cards': [1, 2],
          },
        },
      },
    };
    final env = pb.Envelope.fromBuffer(Wire.encodeFrame(frame));
    expect(env.hasJson(), isTrue);
    expect(difference(Wire.decodeFrame(env.writeToBuffer()), frame), isNull);

    // Bytes of a kind it does not know, and of a dice_roll with a field
    // newer than this schema: text, and the same bytes again.
    final future = [3 << 3, 7]; // field 3, a varint
    final newer = [...asRoll.writeToBuffer(), 0x98, 0x06, 1]; // field 99: 1
    for (final (name, in_) in [
      (
        'an unknown kind',
        pb.ValueEnvelope(kind: 'tarot_spread', data: future).writeToBuffer(),
      ),
      (
        'a newer dice_roll',
        pb.ValueEnvelope(kind: 'dice_roll', data: newer).writeToBuffer(),
      ),
    ]) {
      final got = Wire.decodeMessage(mi, in_) as Map;
      expect(got['data'], isA<String>(), reason: name);
      expect(Wire.encodeMessage(mi, got), in_, reason: name);
    }
    // A known kind's data that is not protobuf at all is an error.
    expect(
      () => Wire.decodeMessage(
        mi,
        pb.ValueEnvelope(kind: 'dice_roll', data: [0xff]).writeToBuffer(),
      ),
      throwsFormatException,
    );
  });

  test('the models read the same from a protobuf frame as from JSON', () {
    var messages = 0;
    for (final v in vectors('frames.jsonl')) {
      final json = v['json'] as Map<String, dynamic>;
      final frame = Wire.decodeFrame(base64.decode(v['pb'] as String))!;
      Map<String, dynamic>? pick(Map<String, dynamic> f, String k) =>
          f[k] is Map ? (f[k] as Map).cast<String, dynamic>() : null;
      final m = pick(json, 'message');
      if (m != null && m['id'] is String && m['conversation_id'] is String) {
        final a = Message.fromJson(m),
            b = Message.fromJson(pick(frame, 'message')!);
        expect(b.id, a.id);
        expect(b.seq, a.seq);
        expect(b.text, a.text);
        expect(b.createdAt, a.createdAt);
        expect(b.entities.length, a.entities.length);
        expect(b.reactions.length, a.reactions.length);
        messages++;
      }
      final c = pick(json, 'conversation');
      if (c != null && c['id'] is String) {
        final a = Conversation.fromJson(c),
            b = Conversation.fromJson(pick(frame, 'conversation')!);
        expect(b.id, a.id);
        expect(b.name, a.name);
        expect(b.lastSeq, a.lastSeq);
      }
      final acc = pick(json, 'account');
      if (acc != null && acc['id'] is String) {
        final a = Account.fromJson(acc),
            b = Account.fromJson(pick(frame, 'account')!);
        expect(b.id, a.id);
        expect(b.displayName, a.displayName);
        expect(b.presence, a.presence);
      }
    }
    expect(messages, greaterThan(5));
  });
}
