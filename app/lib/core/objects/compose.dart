// What the composer does with objects while the author types (ruling R73;
// docs/CLIENTS.md, "Objects in the composer"). The same rules as the web
// chat's internal/web/static/shared/objects/compose.js. No widgets.
//
// Class A is an exact token: a reference, a typed date and time, and the
// kinds after them. When one is complete at the caret a list opens with
// each reading of it and then "Plain text", the first pre-selected.
// Finishing the word (space, punctuation, Enter to send) takes the
// selected row; Escape takes plain text; Backspace straight after an
// accepted object makes it plain text again.
//
// Class B is a loose phrase ("tomorrow at 5pm"): offered, never taken by
// itself; Tab or Enter takes the selected row and Enter then does not
// send. Escape, or typing on, dismisses it; space does not take it.
//
// What the author chose goes to the server as object hints: `plain` for a
// range kept as typed, or a kind for a range read as other than its first
// reading. Taking the first reading sends nothing.

import '../instant/zone.dart';
import '../markdown/objects.dart';
import '../markdown/parser.dart';
import '../models/json.dart';

/// One row of the list: a reading of the token, or plain text
/// ([entity] null, [kind] `plain`).
class ObjectRow {
  const ObjectRow(this.kind, [this.entity]);
  final String kind;
  final Json? entity;
}

/// The exact token at the caret, with its rows.
class ObjectCandidate {
  const ObjectCandidate(this.start, this.end, this.typed, this.rows);
  final int start, end;
  final String typed;
  final List<ObjectRow> rows;
}

/// What the author chose for a token.
class ObjectDecision {
  const ObjectDecision(this.start, this.end, this.typed, this.kind, this.first);
  final int start, end;
  final String typed;
  final String kind;

  /// It was the first row: the reading the server makes anyway.
  final bool first;

  ObjectDecision at(int start) =>
      ObjectDecision(start, start + typed.length, typed, kind, first);
}

/// The class A token that ends at [caret] and has not been decided about,
/// or null.
ObjectCandidate? objectCandidate(
  String text,
  int caret, {
  List<Json> patterns = const [],
  String? zone,
  List<ObjectDecision> decided = const [],
  String origin = '',
}) {
  final doc = parseMarkdown(text);
  final found = <Json>[
    ...instantEntities(text, zone).map((e) => Map<String, dynamic>.of(e)),
    ...findObjects(
      text,
      patterns: patterns,
      hints: objectHintsOf(decided),
      origin: origin,
      doc: doc,
    ),
  ];
  Json? e;
  for (final x in found) {
    final at = x['at'] as int, len = x['len'] as int;
    if (len > 0 && at + len == caret) {
      e = x;
      break;
    }
  }
  if (e == null) return null;
  final start = e['at'] as int, end = start + (e['len'] as int);
  if (decided.any((d) => d.start == start && d.end == end)) return null;
  final rows = [ObjectRow(e['type'] as String, e)];
  if (e['type'] != 'instant' && e['type'] != 'event') {
    for (final r in readingsAt(
      text,
      start,
      patterns: patterns,
      origin: origin,
    )) {
      if (r['type'] != e['type'] && r['len'] == e['len']) {
        rows.add(ObjectRow(r['type'] as String, r));
      }
    }
  }
  rows.add(const ObjectRow(objectPlain));
  return ObjectCandidate(start, end, text.substring(start, end), rows);
}

/// The author's choice of row [index].
ObjectDecision chooseObject(ObjectCandidate c, int index) =>
    ObjectDecision(c.start, c.end, c.typed, c.rows[index].kind, index == 0);

/// Moves decisions to where their text now stands alone: the same place,
/// or the nearest such place. One whose text was edited is dropped, so the
/// token may be offered again.
List<ObjectDecision> trackObjects(List<ObjectDecision> decided, String text) {
  final out = <ObjectDecision>[];
  bool alone(int i, String n) =>
      i >= 0 &&
      i + n.length <= text.length &&
      text.startsWith(n, i) &&
      leftEdge(text, i) &&
      rightEdge(text, i + n.length);
  for (final d in decided) {
    var best = -1;
    if (alone(d.start, d.typed)) {
      best = d.start;
    } else {
      for (
        var i = text.indexOf(d.typed);
        i >= 0;
        i = text.indexOf(d.typed, i + 1)
      ) {
        if (!alone(i, d.typed) || out.any((o) => o.start == i)) continue;
        if (best < 0 || (i - d.start).abs() < (best - d.start).abs()) best = i;
      }
    }
    if (best >= 0) out.add(d.at(best));
  }
  return out;
}

/// The object hints decisions send: plain text, and readings other than
/// the first.
List<Json> objectHintsOf(List<ObjectDecision> decided) => [
  for (final d in decided)
    if (d.kind == objectPlain || !d.first)
      {'start': d.start, 'end': d.end, 'kind': d.kind},
];

/// Makes the decision over [start, end) plain text.
List<ObjectDecision> undoObject(
  List<ObjectDecision> decided,
  int start,
  int end,
) => [
  for (final d in decided)
    d.start == start && d.end == end
        ? ObjectDecision(d.start, d.end, d.typed, objectPlain, false)
        : d,
];

/// What a key does while the list is open, or straight after an object
/// was accepted.
enum ObjectKeyAction {
  none,
  down,
  up,
  commit,
  commitSend,
  accept,
  plain,
  dismiss,
  undo,
}

final _word = RegExp(r'^[\p{L}\p{N}]$', unicode: true);

/// [key] is the key's character (' ', '.') or its name ('Enter', 'Tab',
/// 'Escape', 'ArrowUp', 'ArrowDown', 'Backspace').
ObjectKeyAction objectKeyAction(
  String cls,
  String key, {
  bool open = true,
  bool afterAccept = false,
  bool shift = false,
}) {
  if (!open) {
    return afterAccept && key == 'Backspace'
        ? ObjectKeyAction.undo
        : ObjectKeyAction.none;
  }
  if (key == 'ArrowDown') return ObjectKeyAction.down;
  if (key == 'ArrowUp') return ObjectKeyAction.up;
  if (key == 'Escape') {
    return cls == 'A' ? ObjectKeyAction.plain : ObjectKeyAction.dismiss;
  }
  if (cls == 'A') {
    if (key == 'Enter') {
      return shift ? ObjectKeyAction.commit : ObjectKeyAction.commitSend;
    }
    if (key == 'Tab') return ObjectKeyAction.accept;
    if (key.length == 1 && !_word.hasMatch(key)) return ObjectKeyAction.commit;
    return ObjectKeyAction.none;
  }
  if (key == 'Tab' || key == 'Enter') return ObjectKeyAction.accept;
  return ObjectKeyAction.none;
}
