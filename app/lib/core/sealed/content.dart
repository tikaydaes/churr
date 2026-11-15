// What is inside a sealed message (docs/SEALED.md section 8.3,
// docs/MESSAGE-FORMAT.md section 9): a JSON object the two people's devices
// understand. It is encrypted and signed as bytes by its author and only
// parsed by others, so it needs no canonical form. The builders here write
// the same bytes the Go reference does (field order, HTML-safe escaping),
// which is what lets the transcript be reproduced byte for byte.
//
// Every reference to another message is that message's reference (the
// identifier its sender made, also its client_id). Bytes are base64url
// without padding.

import 'dart:convert';
import 'dart:typed_data';

import 'prim.dart';

const kindMessage = 'message';
const kindEdit = 'edit';
const kindDelete = 'delete';
const kindReaction = 'reaction';
const kindTask = 'task';
const kindHistory = 'history';

/// Bytes as JSON carries them: base64url without padding.
String b64(List<int> b) => base64Url.encode(b).replaceAll('=', '');

/// The inverse of [b64]; null if the text is not base64url.
Uint8List? unb64(String? s) {
  if (s == null || s.contains('=')) return null;
  try {
    return base64Url.decode(s.padRight((s.length + 3) ~/ 4 * 4, '='));
  } on FormatException {
    return null;
  }
}

/// JSON already written, put in place as it is.
class RawJson {
  final String text;
  const RawJson(this.text);
}

/// Writes JSON as Go's encoding/json does: objects in the order given,
/// `<`, `>`, `&`, U+2028 and U+2029 escaped, lone surrogates as U+FFFD.
String goJson(Object? v) {
  final out = StringBuffer();
  void write(Object? v) {
    if (v == null) {
      out.write('null');
    } else if (v is bool) {
      out.write(v ? 'true' : 'false');
    } else if (v is int) {
      out.write(v);
    } else if (v is String) {
      _goString(out, v);
    } else if (v is RawJson) {
      out.write(v.text);
    } else if (v is List) {
      out.write('[');
      for (var i = 0; i < v.length; i++) {
        if (i > 0) out.write(',');
        write(v[i]);
      }
      out.write(']');
    } else if (v is Map) {
      out.write('{');
      var first = true;
      v.forEach((k, x) {
        if (!first) out.write(',');
        first = false;
        _goString(out, k as String);
        out.write(':');
        write(x);
      });
      out.write('}');
    } else {
      throw ArgumentError('goJson: cannot write ${v.runtimeType}');
    }
  }

  write(v);
  return out.toString();
}

void _goString(StringBuffer out, String s) {
  const hex = '0123456789abcdef';
  out.write('"');
  for (final r in s.runes) {
    switch (r) {
      case 0x22:
        out.write(r'\"');
      case 0x5c:
        out.write(r'\\');
      case 0x08:
        out.write(r'\b');
      case 0x0c:
        out.write(r'\f');
      case 0x0a:
        out.write(r'\n');
      case 0x0d:
        out.write(r'\r');
      case 0x09:
        out.write(r'\t');
      case 0x3c || 0x3e || 0x26 || 0x2028 || 0x2029:
        out.write(
          '\\u${hex[(r >> 12) & 15]}${hex[(r >> 8) & 15]}${hex[(r >> 4) & 15]}${hex[r & 15]}',
        );
      default:
        if (r < 0x20) {
          out.write('\\u00${hex[r >> 4]}${hex[r & 15]}');
        } else if (r >= 0xd800 && r <= 0xdfff) {
          out.write('�');
        } else {
          out.writeCharCode(r);
        }
    }
  }
  out.write('"');
}

/// The encrypted thumbnail of an image or video.
class FileThumb {
  final String id;
  final Uint8List key;
  final int width, height;
  FileThumb({
    required this.id,
    required this.key,
    required this.width,
    required this.height,
  });
  Map<String, Object?> toJson() => {
    'id': id,
    'key': b64(key),
    'width': width,
    'height': height,
  };
}

/// A file inside a sealed message: what the server was not given.
class FileEntry {
  final String id;
  final Uint8List key;
  final String name, mediaType, kind;
  final int size;
  final int width, height; // 0 when absent
  final String caption; // '' when absent
  final FileThumb? thumb;
  FileEntry({
    required this.id,
    required this.key,
    required this.name,
    required this.mediaType,
    required this.kind,
    required this.size,
    this.width = 0,
    this.height = 0,
    this.caption = '',
    this.thumb,
  });

  Map<String, Object?> toJson() => {
    'id': id,
    'key': b64(key),
    'name': name,
    'media_type': mediaType,
    'kind': kind,
    'size': size,
    if (width != 0) 'width': width,
    if (height != 0) 'height': height,
    if (caption != '') 'caption': caption,
    if (thumb != null) 'thumb': thumb!.toJson(),
  };
}

/// What a reply quotes: the quoted message's reference and, for part of
/// it, a range in UTF-16 code units.
class Quote {
  final Uuid messageId;
  final int? start, end;
  Quote(this.messageId, {this.start, this.end});
  Map<String, Object?> toJson() => {
    'message_id': messageId.toString(),
    if (start != null) 'start': start,
    if (end != null) 'end': end,
  };
}

/// One epoch's history secret in a `history` message.
class HistorySecretEntry {
  final int keyEpoch;
  final Uint8List secret;
  HistorySecretEntry(this.keyEpoch, this.secret);
}

Uint8List _bytes(Object? v) => Uint8List.fromList(utf8.encode(goJson(v)));

/// A `message`. [entities] are JSON objects (maps, in the order to write).
/// [action] marks text typed with /me (ruling R67); it is written only when
/// set, so every other message's bytes are as they were.
Uint8List messageContent(
  String text, {
  List<Object?> entities = const [],
  List<FileEntry> files = const [],
  Uuid? parent,
  Quote? quote,
  bool action = false,
}) => _bytes({
  'kind': kindMessage,
  'text': text,
  'entities': entities,
  'files': [for (final f in files) f.toJson()],
  'parent_id': parent?.toString(),
  'quote': quote?.toJson(),
  if (action) 'action': true,
});

/// An `edit` of a message the sender wrote. Its files stay.
Uint8List editContent(
  Uuid target,
  String text, {
  List<Object?> entities = const [],
}) => _bytes({
  'kind': kindEdit,
  'message_id': target.toString(),
  'text': text,
  'entities': entities,
});

/// A `delete` of a message the sender wrote.
Uint8List deleteContent(Uuid target) =>
    _bytes({'kind': kindDelete, 'message_id': target.toString()});

/// A `reaction`: one emoji put on or taken off.
Uint8List reactionContent(Uuid target, String emoji, bool on) => _bytes({
  'kind': kindReaction,
  'message_id': target.toString(),
  'emoji': emoji,
  'on': on,
});

/// A `task`: one checklist item ticked or unticked.
Uint8List taskContent(Uuid target, String item, bool checked) => _bytes({
  'kind': kindTask,
  'message_id': target.toString(),
  'item_id': item,
  'checked': checked,
});

/// A `history`: secrets for epochs the other account had no device in.
Uint8List historyContent(List<HistorySecretEntry> secrets) => _bytes({
  'kind': kindHistory,
  'secrets': [
    for (final s in secrets) {'key_epoch': s.keyEpoch, 'secret': b64(s.secret)},
  ],
});
