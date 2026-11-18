// The catalogue in the app (docs/DESIGN-MODULES.md sections 3 and 4), the
// same as the web chat's (internal/web/static/shared/types/catalogue.js):
// the kinds of value this client knows, its catalogue level, and the
// day-one rules for what it does not know. A value is an envelope, `{kind,
// level, data, alt, by, source, removed}`, on a whole message as its `view`
// or on a span of the text as an `object` entity. The text is the message;
// the value is a view of it.
//
// No Flutter here. What each kind looks like (its chip, tooltip, dialog
// and card) is in lib/ui/types/, beside the kind's own files.

import '../models/json.dart';
import '../models/message.dart';
import 'dice_roll.dart';

/// The catalogue level this client reads in full (day-one rule 3). A
/// value whose level is above it is drawn by its alt, or as its text.
const catalogueLevel = 1;

/// One kind this client knows: the lowest level that reads it, its data's
/// check (which gives what is drawn, or null), and whether it moves when
/// it arrives live.
class CatalogueKind {
  const CatalogueKind({
    required this.level,
    required this.data,
    this.spins = false,
  });
  final int level;
  final Json? Function(Object? data) data;
  final bool spins;
}

/// Every kind this client knows.
const catalogueKinds = <String, CatalogueKind>{
  'dice_roll': CatalogueKind(level: 1, data: diceRollData, spins: true),
};

/// How deep a value's renditions are followed: an alt of an alt is the
/// most a kind needs; past that the text stands.
const _maxAlts = 3;

/// How a value is drawn: by a kind ([kind] is set), or as its text.
class ValueReading {
  const ValueReading.kind(
    String this.kind,
    this.data, {
    this.removed = false,
    this.alt = false,
  }) : newer = false;
  const ValueReading.text({this.newer = false})
    : kind = null,
      data = null,
      removed = false,
      alt = false;

  /// The kind that draws it, or null when the text stands.
  final String? kind;

  /// The checked copy of the data, or null when [removed].
  final Json? data;

  /// Nothing was resolved for it: an edit wrote or changed its text.
  final bool removed;

  /// It is the value's generic rendition that is drawn.
  final bool alt;

  /// The text stands because of a kind or level this client does not
  /// read: the built-in state "this needs a newer app" (day-one rule 4).
  final bool newer;

  bool get drawn => kind != null;

  @override
  String toString() => drawn
      ? 'kind($kind, removed: $removed, alt: $alt)'
      : 'text(newer: $newer)';
}

/// Decides how a value is drawn: the three-step fallback (day-one rule
/// 1), the kind, else its alt, else the text. Data that fails its check
/// falls back a step and is no error to the reader (section 4.2). [level]
/// is the client's own ([catalogueLevel]), given for tests.
ValueReading readValue(Object? v, {int level = catalogueLevel, int depth = 0}) {
  if (v is! Map || v['kind'] is! String) return const ValueReading.text();
  final kind = v['kind'] as String;
  final def = catalogueKinds[kind];
  final vLevel = v['level'];
  var newer = false;
  if (def != null && vLevel is int && vLevel <= level) {
    if (v['removed'] == true) {
      return ValueReading.kind(kind, null, removed: true, alt: depth > 0);
    }
    final data = def.data(v['data']);
    if (data != null) return ValueReading.kind(kind, data, alt: depth > 0);
  } else {
    newer = true;
  }
  if (v['alt'] != null && depth < _maxAlts) {
    final r = readValue(v['alt'], level: level, depth: depth + 1);
    if (r.drawn) return r;
    newer = newer || r.newer;
  }
  return ValueReading.text(newer: newer);
}

/// A message's view: the value it is as a whole, or null.
Json? viewOf(Message m) => object(m.raw['view']);

/// An `object` entity's value, or null.
Json? valueOf(Entity e) => e.type == 'object' ? object(e.raw['object']) : null;

bool _moves(Object? v) {
  final r = readValue(v);
  return r.drawn && !r.removed && catalogueKinds[r.kind]!.spins;
}

/// Whether a message's view moves once when it arrives live (a roll).
bool viewSpins(Message m) => _moves(viewOf(m));

/// Whether the value of an `object` entity moves once when it arrives
/// live (inline dice).
bool objectSpins(Entity e) => _moves(valueOf(e));

/// Whether a message has a value that moves once when it arrives live: a
/// roll, as its view or inline.
bool spins(Message m) => viewSpins(m) || m.entities.any(objectSpins);

/// Carries a message's values through an edit this client makes itself (a
/// sealed chat's, where no server reads the text): day-one rule 5, values
/// it does not understand kept untouched. Each value over a span of the
/// old text is kept, the same object, wherever the same characters still
/// stand in the new text, left to right, clear of the entities the new
/// text has of its own; one whose characters are gone goes with them. A
/// message's view is not the client's to change: an edit never touches it.
List<Json> keepValues(
  String oldText,
  List<Json> oldEntities,
  String newText,
  List<Json> newEntities,
) {
  final ours = [...newEntities];
  bool clear(int at, int len) => ours.every((e) {
    final eAt = asInt(e['at']), eLen = asInt(e['len']);
    return eAt + eLen <= at || eAt >= at + len;
  });
  final kept = <Json>[];
  var from = 0;
  for (final e in oldEntities) {
    if (e['type'] != 'object' || e['object'] == null) continue;
    final at0 = asInt(e['at']);
    final end = (at0 + asInt(e['len'])).clamp(0, oldText.length);
    if (at0 < 0 || at0 >= end) continue;
    final typed = oldText.substring(at0, end);
    var at = newText.indexOf(typed, from);
    while (at >= 0 && !clear(at, typed.length)) {
      at = newText.indexOf(typed, at + 1);
    }
    if (at < 0) continue;
    kept.add({...e, 'at': at, 'len': typed.length});
    from = at + typed.length;
  }
  // In order of place; at one place, the new text's own first (a stable
  // sort, as the web chat's).
  final all = [...ours, ...kept];
  final order = List.generate(all.length, (i) => i)
    ..sort((a, b) {
      final by = asInt(all[a]['at']).compareTo(asInt(all[b]['at']));
      return by != 0 ? by : a.compareTo(b);
    });
  return [for (final i in order) all[i]];
}
