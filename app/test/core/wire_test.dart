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
