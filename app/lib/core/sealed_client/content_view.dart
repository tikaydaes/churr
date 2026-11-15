// Between sealed content and the app's message shapes: the entities a
// sending device works out for its text (docs/SEALED.md section 8.3), and
// a decrypted message ([Shown]) as the API's `Message` JSON, so the
// conversation screen's message widgets can draw it.

import '../instant/zone.dart';
import '../markdown/nodes.dart';
import '../markdown/parser.dart';
import '../models/json.dart';
import '../sealed/sealed.dart';
import 'sqlite_store.dart';

/// The entities of [text] as a sending device works them out: a task
/// entity for each checklist item, with item ids `t1`, `t2`… in the
/// order they stand; an account entity for a mention of one of the two
/// people ([usernames]: username to account id); a link entity for each
/// bare link. The receiving device works out the same from the same text
/// (and draws a mention only for one of the two accounts).
///
/// Dates and times typed in the text become instant entities, read in
/// [zone] (the sending device's) when none is written (ruling R69). A
/// receiving device passes the sender's own entities as [sent], whose
/// instants it keeps where they cover a typed instant or plain text.
List<Json> workOutEntities(
  String text,
  Map<String, String> usernames, {
  String? zone,
  List<Object?>? sent,
}) {
  final out = <Json>[];
  var task = 0;
  void inlines(List<Inline> xs) {
    for (final s in xs) {
      switch (s.kind) {
        case InlineKind.mention:
          final id = usernames[s.name.toLowerCase()];
          if (id != null) {
            out.add({
              'type': 'account',
              'at': s.at,
              'len': s.len,
              'account_id': id,
            });
          }
        case InlineKind.bareLink:
          out.add({'type': 'link', 'at': s.at, 'len': s.len, 'url': s.url});
        default:
          inlines(s.children);
      }
    }
  }

  void blocks(List<Block> bs) {
    for (final b in bs) {
      inlines(b.inlines);
      for (final it in b.items) {
        if (it.task) {
          task++;
          out.add({
            'type': 'task',
            'at': it.textAt,
            'len': 0,
            'item_id': 't$task',
          });
        }
        inlines(it.inlines);
        blocks(it.lists);
      }
      for (final row in [b.head, ...b.rows]) {
        for (final cell in row) {
          inlines(cell.inlines);
        }
      }
      blocks(b.blocks);
    }
  }

  blocks(parseMarkdown(text).blocks);
  if (sent == null) {
    out.addAll(instantEntities(text, zone));
  } else {
    out.addAll(_sentInstants(text, sent, out));
  }
  out.sort((a, b) => (a['at'] as int).compareTo(b['at'] as int));
  return out;
}

/// The instants among a sender's entities that hold up: an RFC 3339
/// time, a known zone, inside the text and over no other entity.
List<Json> _sentInstants(String text, List<Object?> sent, List<Json> taken) {
  final out = <Json>[];
  for (final e in sent) {
    if (e is! Map || e['type'] != 'instant') continue;
    final at = e['at'], len = e['len'], time = e['time'], zone = e['zone'];
    if (at is! int ||
        len is! int ||
        at < 0 ||
        len <= 0 ||
        at + len > text.length) {
      continue;
    }
    if (time is! String ||
        DateTime.tryParse(time) == null ||
        zone is! String ||
        !validZone(zone)) {
      continue;
    }
    final overlaps = [...taken, ...out].any((x) {
      final a = x['at'] as int, l = x['len'] as int;
      return x['type'] != 'task' && at < a + l && a < at + len;
    });
    if (overlaps) continue;
    out.add({
      'type': 'instant',
      'at': at,
      'len': len,
      'time': time,
      'precision': e['precision'] == 'second' ? 'second' : 'minute',
      'zone': zone,
    });
  }
  return out;
}

/// A decrypted message as the API's `Message` JSON. Its `id` is the
/// message reference: inside sealed content every reference to another
/// message is its reference, so replies and reactions line up. Files are
/// left out (the sealed screen draws them, decrypted on the device).
Json shownAsMessage(
  Shown s,
  SealedRowMeta? meta,
  String conversationId, {
  required String? myId,
  required Map<String, String> usernames,
}) {
  final at =
      meta?.createdAt ?? DateTime.fromMillisecondsSinceEpoch(0, isUtc: true);
  return {
    'id': '${s.reference}',
    'conversation_id': conversationId,
    'seq': s.seq,
    'order': meta?.order.isNotEmpty == true
        ? meta!.order
        : s.seq.toString().padLeft(20, '0'),
    'author_id': '${s.author}',
    'client_id': '${s.reference}',
    if (s.parentId.isNotEmpty) 'parent_id': s.parentId,
    'text': s.deleted ? '' : s.text,
    if (s.action && !s.deleted) 'action': true,
    'entities': s.deleted
        ? const []
        : workOutEntities(s.text, usernames, sent: s.entities),
    'tasks': [
      for (final e in s.tasks.entries) {'id': e.key, 'checked': e.value},
    ],
    'reactions': [
      for (final e in s.reactions.entries)
        {
          'emoji': e.key,
          'count': e.value.length,
          'me': e.value.any((a) => '$a' == myId),
          'account_ids': [for (final a in e.value) '$a'],
        },
    ],
    'created_at': formatTime(at),
    if (s.edited) 'edited_at': formatTime(at),
    if (s.deleted) 'deleted_at': formatTime(at),
    if (s.deleted) 'deleted_by_id': '${s.author}',
  };
}

/// The files of a decrypted message, typed.
List<SealedFile> filesOf(Shown s) => [
  for (final f in s.files)
    if (f is Map && unb64(f['key'] as String?)?.length == 32)
      SealedFile(f.cast<String, Object?>()),
];

/// One file entry inside sealed content (docs/SEALED.md section 9).
class SealedFile {
  SealedFile(this.raw);
  final Map<String, Object?> raw;
  String get id => raw['id'] as String? ?? '';
  List<int> get key => unb64(raw['key'] as String?)!;
  String get name => raw['name'] as String? ?? 'file';
  String get mediaType =>
      raw['media_type'] as String? ?? 'application/octet-stream';
  String get kind => raw['kind'] as String? ?? 'file';
  int get size => asInt(raw['size']);
  int get width => asInt(raw['width']);
  int get height => asInt(raw['height']);
  String get caption => raw['caption'] as String? ?? '';
  Map<String, Object?>? get thumb =>
      (raw['thumb'] as Map?)?.cast<String, Object?>();
  String? get thumbId => thumb?['id'] as String?;
  List<int>? get thumbKey => unb64(thumb?['key'] as String?);

  /// The receiver decides how to draw a file from its content and treats
  /// the sender's type as a hint; SVG is always a download.
  bool get drawsAsImage =>
      kind == 'image' &&
      !mediaType.contains('svg') &&
      !name.toLowerCase().endsWith('.svg');
}
