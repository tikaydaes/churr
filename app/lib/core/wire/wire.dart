/// Protocol Buffers for everything the app says to its server and hears
/// from it (ruling R65, docs/WIRE-PROTOBUF.md): the bodies of the HTTP API
/// and the frames of the live connection.
///
/// The rest of the app keeps working in the JSON shapes it always has. A
/// body or frame arrives as protobuf, is parsed by the generated classes
/// (`gen/churr.pb.dart`, from proto/churr/v1/churr.proto), and is mapped
/// here to exactly the object `jsonDecode` would have given for the same
/// thing in JSON, which the models then read with their `fromJson`. Going
/// out, the JSON object is mapped to the message and written as bytes.
///
/// The mapping walks the schema table in `schema.g.dart`. Both that table
/// and the classes come from one run of tools/protogen.sh.
///
/// Writing is exact or it is not done: [Wire.encodeRequest] gives null for
/// a body the schema cannot hold, and the caller sends JSON. Reading skips
/// what it does not know: a field or an enum value from a newer server is
/// left out, and a frame of a type this build has never heard of gives
/// null.
library;

import 'dart:convert';
import 'dart:typed_data';

import 'package:fixnum/fixnum.dart';
import 'package:protobuf/protobuf.dart';

import '../models/json.dart';
import 'gen/churr.pb.dart' as pb;
import 'schema.dart';
import 'schema.g.dart';

/// A value with no exact protobuf form.
class _Misfit implements Exception {
  const _Misfit(this.why);
  final String why;
  @override
  String toString() => 'no protobuf form: $why';
}

/// A value to leave out: an enum number this build does not have.
const _skip = Object();

abstract final class Wire {
  /// The media type of a protobuf body, with the schema version this build
  /// speaks. Sent in `Accept` and, with a protobuf body, `Content-Type`.
  static const mediaType = 'application/x-protobuf';
  static const contentType = '$mediaType; v=$wireSchemaVersion';

  /// The subprotocol to offer on the live connection, before `churr.v1`,
  /// to get binary frames. A server that does not know it picks `churr.v1`.
  static const liveSubprotocol = 'churr.v1+proto.$wireSchemaVersion';

  /// Whether a `Content-Type` says the body is protobuf.
  static bool isProtobuf(String? contentType) =>
      contentType != null &&
      contentType.split(';').first.trim().toLowerCase() == mediaType;

  static final Map<String, WireFrame> _frameByType = {
    for (final f in wireFrames) f.type: f,
  };
  static final Map<int, WireFrame> _frameByNumber = {
    for (final f in wireFrames) f.number: f,
  };
  static final Map<String, int> _messageByName = {
    for (var i = 0; i < wireMessages.length; i++) wireMessages[i].name: i,
  };

  // ---- the HTTP API

  /// The protobuf form of an operation's JSON request body, or null when
  /// the operation takes none or the body does not fit it; the caller then
  /// sends JSON.
  static Uint8List? encodeRequest(String operationId, Object? body) {
    final op = wireOperations[operationId];
    if (op == null || op.request < 0) return null;
    return encodeMessage(op.request, body);
  }

  /// The JSON value of an operation's protobuf answer with [status].
  /// Throws a [FormatException] if the bytes are not that message.
  static Object? decodeResponse(
    String operationId,
    int status,
    List<int> bytes,
  ) {
    var message = wireOperations[operationId]?.responses[status];
    if (message == null && status >= 400) message = wireErrorMessage;
    if (message == null) {
      throw FormatException('$operationId has no message for status $status');
    }
    return decodeMessage(message, bytes);
  }

  /// The number of a message in the table, by its name in churr.proto.
  static int? messageNamed(String name) => _messageByName[name];

  /// Writes a JSON value as message [index], or null if it does not fit.
  static Uint8List? encodeMessage(int index, Object? json) {
    try {
      return _write(index, json).writeToBuffer();
    } on _Misfit {
      return null;
    }
  }

  /// Reads message [index] back into its JSON value.
  static Object? decodeMessage(int index, List<int> bytes) {
    final m = wireMessages[index].make();
    try {
      m.mergeFromBuffer(bytes);
    } on InvalidProtocolBufferException catch (e) {
      throw FormatException(e.message);
    }
    return _read(index, m);
  }

  // ---- the live connection

  /// The JSON object of one binary frame: what `jsonDecode` gives for the
  /// same frame as text. Null for a frame to skip: one whose type this
  /// build does not know and that carries no JSON.
  static Json? decodeFrame(List<int> bytes) {
    final pb.Envelope env;
    try {
      env = pb.Envelope.fromBuffer(bytes);
    } on InvalidProtocolBufferException catch (e) {
      throw FormatException(e.message);
    }
    return _readFrame(env);
  }

  /// One frame as an envelope. It always succeeds: a frame the schema
  /// cannot hold is carried whole as JSON inside the envelope.
  static Uint8List encodeFrame(Json frame) =>
      _writeFrame(frame).writeToBuffer();

  // ---- reading

  static Json? _readFrame(pb.Envelope env) {
    if (env.hasJson()) {
      final v = jsonDecode(utf8.decode(env.json));
      return v is Map ? v.cast<String, dynamic>() : null;
    }
    final frame = _frameByNumber[env.$_whichOneof(0)];
    if (frame == null) return null;
    final out = <String, dynamic>{};
    _readObject(
      wireMessages[frame.message],
      env.getField(frame.number) as GeneratedMessage,
      out,
    );
    out['type'] = frame.type;
    if (env.hasAt()) out['at'] = _formatTime(env.at.toInt());
    if (env.hasConversationId()) {
      out['conversation_id'] = _formatUuid(env.conversationId);
    }
    if (env.hasSeq()) out['seq'] = env.seq.toInt();
    for (final n in env.nullFields) {
      final name = const {4: 'at', 5: 'conversation_id', 6: 'seq'}[n];
      if (name != null && !out.containsKey(name)) out[name] = null;
    }
    return out;
  }

  static Object? _read(int index, GeneratedMessage m) {
    final def = wireMessages[index];
    if (def.shape == WireShape.wrap) {
      final f = def.fields[0];
      final v = m.getField(1);
      return f.card == WireCard.list ? _readList(f, v as List) : _readMap(f, v);
    }
    final out = <String, dynamic>{};
    _readObject(def, m, out);
    return out;
  }

  static void _readObject(WireMessage def, GeneratedMessage m, Json out) {
    for (final f in def.fields) {
      if (f.kind == WireKind.const_) {
        out[f.json] = f.text;
      } else if (f.card == WireCard.one) {
        // Not hasField, which says an empty `bytes` is not there.
        final raw = m.getFieldOrNull(f.number);
        if (raw != null) {
          final v = _readOne(f, raw);
          if (!identical(v, _skip)) out[f.json] = v;
        }
      } else if (f.card == WireCard.list) {
        final list = m.getField(f.number) as List;
        if (list.isNotEmpty) out[f.json] = _readList(f, list);
      } else {
        final v = m.getField(f.number);
        if ((v is Map && v.isNotEmpty) || (v is List && v.isNotEmpty)) {
          out[f.json] = _readMap(f, v);
        }
      }
    }
    if (def.hasEmpties) {
      final byNumber = {for (final f in def.fields) f.number: f};
      for (final n in m.getField(1) as List) {
        final f = byNumber[n];
        if (f == null || f.card == WireCard.one || out.containsKey(f.json)) {
          continue;
        }
        out[f.json] = f.card == WireCard.list
            ? <dynamic>[]
            : <String, dynamic>{};
      }
    }
    final nulls = m.getField(2) as List;
    if (nulls.isNotEmpty) {
      for (final f in def.fields) {
        if (nulls.contains(f.number) && !out.containsKey(f.json)) {
          out[f.json] = null;
        }
      }
    }
    if (def.extra != 0) {
      (m.getField(def.extra) as Map).forEach((k, v) {
        if (!out.containsKey(k) && !def.byName.containsKey(k)) {
          out[k as String] = _readValue(v as pb.Value);
        }
      });
    }
  }

  static List<dynamic> _readList(WireField f, List list) => [
    for (final e in list)
      if (!identical(_readOne(f, e), _skip)) _readOne(f, e),
  ];

  static Json _readMap(WireField f, Object? v) {
    final out = <String, dynamic>{};
    if (f.card == WireCard.umap) {
      final value = wireMessages[f.ref].fields[1];
      for (final e in v as List) {
        final entry = e as GeneratedMessage;
        final raw = entry.getFieldOrNull(2);
        final x = raw != null || value.kind == WireKind.msg
            ? _readOne(value, raw ?? entry.getField(2))
            : _zero(value);
        if (!identical(x, _skip)) {
          out[_formatUuid(entry.getField(1) as List<int>)] = x;
        }
      }
    } else {
      (v as Map).forEach((k, e) {
        final x = _readOne(f, e);
        if (!identical(x, _skip)) out[k as String] = x;
      });
    }
    return out;
  }

  static Object? _zero(WireField f) => switch (f.kind) {
    WireKind.string || WireKind.bytes => '',
    WireKind.int_ => 0,
    WireKind.double_ => 0,
    WireKind.bool_ => false,
    WireKind.value => null,
    _ => _skip,
  };

  static Object? _readOne(WireField f, Object? v) {
    switch (f.kind) {
      case WireKind.string:
      case WireKind.bool_:
        return v;
      case WireKind.int_:
        return (v as Int64).toInt();
      case WireKind.double_:
        return _number(v as double);
      case WireKind.uuid:
        final b = v as List<int>;
        if (b.length != 16) {
          throw const FormatException('an identifier is 16 bytes');
        }
        return _formatUuid(b);
      case WireKind.time:
        return _formatTime((v as Int64).toInt());
      case WireKind.bytes:
        return base64Url.encode(v as List<int>).replaceAll('=', '');
      case WireKind.enum_:
        final names = wireEnums[f.ref].names;
        final n = (v as ProtobufEnum).value;
        return n > 0 && n < names.length && names[n].isNotEmpty
            ? names[n]
            : _skip;
      case WireKind.msg:
        return _read(f.ref, v as GeneratedMessage);
      case WireKind.value:
        return _readValue(v as pb.Value);
      case WireKind.envelope:
        return _readFrame(v as pb.Envelope) ?? _skip;
      case WireKind.const_:
        return f.text;
    }
  }

  /// A double as `jsonDecode` gives it after the server has written it:
  /// the server writes a whole number without a fraction, and Dart reads
  /// that as an int.
  static num _number(double d) {
    if (d.isFinite &&
        d == d.truncateToDouble() &&
        d.abs() < 9223372036854775808.0 &&
        !(d == 0 && d.isNegative)) {
      return d.toInt();
    }
    return d;
  }

  static Object? _readValue(pb.Value v) => switch (v.whichKind()) {
    pb.Value_Kind.boolValue => v.boolValue,
    pb.Value_Kind.intValue => v.intValue.toInt(),
    pb.Value_Kind.doubleValue => _number(v.doubleValue),
    pb.Value_Kind.stringValue => v.stringValue,
    pb.Value_Kind.listValue => [
      for (final e in v.listValue.items) _readValue(e),
    ],
    pb.Value_Kind.mapValue => <String, dynamic>{
      for (final e in v.mapValue.fields.entries) e.key: _readValue(e.value),
    },
    pb.Value_Kind.nullValue || pb.Value_Kind.notSet => null,
  };

  // ---- writing

  static pb.Envelope _writeFrame(Json frame) {
    try {
      final type = frame['type'];
      final def = type is String ? _frameByType[type] : null;
      if (def == null) throw const _Misfit('no such frame');
      final env = pb.Envelope();
      final nulls = <int>[];
      void head(String name, int number, void Function(Object v) set) {
        if (!frame.containsKey(name)) return;
        final v = frame[name];
        v == null ? nulls.add(number) : set(v);
      }

      head('at', 4, (v) => env.at = Int64(_parseTime(v)));
      head('conversation_id', 5, (v) => env.conversationId = _parseUuid(v));
      head('seq', 6, (v) => env.seq = Int64(_int(v)));
      env.nullFields.addAll(nulls);
      final body = wireMessages[def.message].make();
      _writeObject(wireMessages[def.message], body, frame, const {
        'type',
        'at',
        'conversation_id',
        'seq',
      });
      env.setField(def.number, body);
      return env;
    } on _Misfit {
      final env = pb.Envelope()..json = utf8.encode(jsonEncode(frame));
      final type = frame['type'];
      if (type is String) env.type = type;
      return env;
    }
  }

  static GeneratedMessage _write(int index, Object? json) {
    final def = wireMessages[index];
    final m = def.make();
    if (def.shape == WireShape.wrap) {
      if (json == null) throw const _Misfit('null inside a list or map');
      _writeField(m, def.fields[0], json);
      return m;
    }
    if (json is! Map) throw const _Misfit('not an object');
    _writeObject(def, m, json.cast<String, dynamic>(), const {});
    return m;
  }

  static void _writeObject(
    WireMessage def,
    GeneratedMessage m,
    Json obj,
    Set<String> skip,
  ) {
    final empties = <int>[];
    final nulls = <int>[];
    var known = 0;
    for (final f in def.fields) {
      if (f.kind == WireKind.const_) {
        // The reader writes it whether or not it was there, so it must be.
        if (obj[f.json] != f.text) throw _Misfit('${f.json} must be ${f.text}');
        known++;
        continue;
      }
      if (!obj.containsKey(f.json)) continue;
      known++;
      final v = obj[f.json];
      if (v == null) {
        nulls.add(f.number);
      } else if (f.card != WireCard.one &&
          ((v is List && v.isEmpty) || (v is Map && v.isEmpty))) {
        empties.add(f.number);
      } else {
        _writeField(m, f, v);
      }
    }
    for (final k in skip) {
      if (obj.containsKey(k)) known++;
    }
    if (known != obj.length) {
      if (def.extra == 0) throw const _Misfit('a field the schema lacks');
      final extra = m.getField(def.extra) as Map;
      for (final k in obj.keys.toList()..sort()) {
        if (!skip.contains(k) && !def.byName.containsKey(k)) {
          extra[k] = _writeValue(obj[k]);
        }
      }
    }
    if (empties.isNotEmpty) (m.getField(1) as List).addAll(empties..sort());
    if (nulls.isNotEmpty) (m.getField(2) as List).addAll(nulls..sort());
  }

  static void _writeField(GeneratedMessage m, WireField f, Object v) {
    switch (f.card) {
      case WireCard.one:
        m.setField(f.number, _writeOne(f, v)!);
      case WireCard.list:
        if (v is! List) throw const _Misfit('not a list');
        final list = m.getField(f.number) as List;
        for (final e in v) {
          list.add(_writeOne(f, e));
        }
      case WireCard.map:
        if (v is! Map) throw const _Misfit('not an object');
        final map = m.getField(f.number) as Map;
        for (final k in v.keys.toList()..sort()) {
          if (k is! String) throw const _Misfit('a key that is not text');
          map[k] = _writeOne(f, v[k]);
        }
      case WireCard.umap:
        if (v is! Map) throw const _Misfit('not an object');
        final def = wireMessages[f.ref];
        final list = m.getField(f.number) as List;
        for (final k in v.keys.toList()..sort()) {
          list.add(
            def.make()
              ..setField(1, _parseUuid(k))
              ..setField(2, _writeOne(def.fields[1], v[k])!),
          );
        }
    }
  }

  static Object? _writeOne(WireField f, Object? v) {
    if (f.kind == WireKind.value) return _writeValue(v);
    if (v == null) throw const _Misfit('null inside a list or map');
    switch (f.kind) {
      case WireKind.string:
        if (v is! String) throw const _Misfit('not text');
        return v;
      case WireKind.int_:
        return Int64(_int(v));
      case WireKind.double_:
        if (v is! num) throw const _Misfit('not a number');
        return v.toDouble();
      case WireKind.bool_:
        if (v is! bool) throw const _Misfit('not true or false');
        return v;
      case WireKind.uuid:
        return _parseUuid(v);
      case WireKind.time:
        return Int64(_parseTime(v));
      case WireKind.bytes:
        if (v is! String || v.contains('=')) {
          throw const _Misfit('not base64url');
        }
        try {
          final b = base64Url.decode(base64Url.normalize(v));
          if (base64Url.encode(b).replaceAll('=', '') != v) {
            throw const _Misfit('not base64url');
          }
          return b;
        } on FormatException {
          throw const _Misfit('not base64url');
        }
      case WireKind.enum_:
        final e = wireEnums[f.ref];
        final n = v is String && v.isNotEmpty ? e.names.indexOf(v) : -1;
        final value = n > 0 ? e.of(n) : null;
        if (value == null) throw const _Misfit('a value the schema lacks');
        return value;
      case WireKind.msg:
        return _write(f.ref, v);
      case WireKind.envelope:
        if (v is! Map) throw const _Misfit('not an object');
        return _writeFrame(v.cast<String, dynamic>());
      case WireKind.value:
      case WireKind.const_:
        throw const _Misfit('not a field');
    }
  }

  static pb.Value _writeValue(Object? v) {
    final out = pb.Value();
    switch (v) {
      case null:
        out.nullValue = true;
      case bool():
        out.boolValue = v;
      case int():
        out.intValue = Int64(v);
      case double():
        out.doubleValue = v;
      case String():
        out.stringValue = v;
      case List():
        out.listValue = pb.ValueList(items: v.map(_writeValue));
      case Map():
        final map = pb.ValueMap();
        for (final k in v.keys.toList()..sort()) {
          if (k is! String) throw const _Misfit('a key that is not text');
          map.fields[k] = _writeValue(v[k]);
        }
        out.mapValue = map;
      default:
        throw const _Misfit('not a JSON value');
    }
    return out;
  }

  // ---- identifiers, times, whole numbers

  static int _int(Object v) {
    if (v is! int) throw const _Misfit('not a whole number');
    return v;
  }

  static const _hex = '0123456789abcdef';

  static String _formatUuid(List<int> b) {
    final s = StringBuffer();
    for (var i = 0; i < 16; i++) {
      if (i == 4 || i == 6 || i == 8 || i == 10) s.write('-');
      s
        ..write(_hex[b[i] >> 4])
        ..write(_hex[b[i] & 15]);
    }
    return s.toString();
  }

  static Uint8List _parseUuid(Object? v) {
    if (v is! String || v.length != 36) {
      throw const _Misfit('not an identifier');
    }
    final out = Uint8List(16);
    var n = 0;
    for (var i = 0; i < 36; i++) {
      final c = v.codeUnitAt(i);
      if (i == 8 || i == 13 || i == 18 || i == 23) {
        if (c != 0x2d) throw const _Misfit('not an identifier');
        continue;
      }
      final d = c >= 0x30 && c <= 0x39
          ? c - 0x30
          : (c >= 0x61 && c <= 0x66 ? c - 0x57 : -1);
      if (d < 0) throw const _Misfit('not an identifier');
      out[n >> 1] = (out[n >> 1] << 4) | d;
      n++;
    }
    return out;
  }

  static String _pad(int n, int width) => n.toString().padLeft(width, '0');

  /// Microseconds since the epoch as the API writes a time: UTC with
  /// milliseconds, or with microseconds when it is not a whole millisecond.
  static String _formatTime(int us) {
    final t = DateTime.fromMicrosecondsSinceEpoch(us, isUtc: true);
    final fraction = t.microsecond == 0
        ? _pad(t.millisecond, 3)
        : _pad(t.millisecond * 1000 + t.microsecond, 6);
    return '${_pad(t.year, 4)}-${_pad(t.month, 2)}-${_pad(t.day, 2)}T'
        '${_pad(t.hour, 2)}:${_pad(t.minute, 2)}:${_pad(t.second, 2)}.${fraction}Z';
  }

  static final _time = RegExp(
    r'^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)\.(\d{3}|\d{6})Z$',
  );

  static int _parseTime(Object? v) {
    final m = v is String ? _time.firstMatch(v) : null;
    if (m == null) throw const _Misfit('not a time in the contract\'s form');
    int g(int i) => int.parse(m.group(i)!);
    final fraction = m.group(7)!;
    final us =
        DateTime.utc(
          g(1),
          g(2),
          g(3),
          g(4),
          g(5),
          g(6),
        ).microsecondsSinceEpoch +
        (fraction.length == 3 ? g(7) * 1000 : g(7));
    // Only a time that will be written back exactly as it came.
    if (_formatTime(us) != v) {
      throw const _Misfit('not a time in the contract\'s form');
    }
    return us;
  }
}
