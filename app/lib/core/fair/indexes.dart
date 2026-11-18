// The canonical object indexes (docs/FAIR.md section 3), as internal/fair's
// CheckIndexes and the web chat's shared/fair/indexes.js: a command's own
// value (the view), when it took chance, is object 0; the object entities
// that took chance follow in the order of their position `at`, from 1 if the
// view took chance and 0 if not, with no gap and no repeat. On an edited
// message a roll the edit changed is marked removed and keeps its index, and
// one it deleted leaves its index unused: the indexes still rise with the
// position, never repeat, and a gap is left only there. A message never
// edited has no gap and nothing removed. Held to testdata/fair/indexes.json.
// Plain Dart, tested without Flutter.

/// One value as the rule sees it.
typedef ObjectIndex = ({int at, bool drew, int? i, bool removed});

/// Null when [objects] (and the [view], if any) carry the canonical
/// indexes, or why not.
String? checkIndexes(
  ObjectIndex? view,
  List<ObjectIndex> objects,
  bool edited,
) {
  var next = 0;
  if (view != null && view.removed) {
    return "The message's own value is marked removed.";
  }
  if (view != null && view.drew) {
    if (view.i != 0) {
      return "The command's own value is not object 0 of the draw.";
    }
    next = 1;
  }
  final objs = [for (var n = 0; n < objects.length; n++) (o: objects[n], n: n)]
    ..sort((a, b) {
      final c = a.o.at.compareTo(b.o.at);
      return c != 0 ? c : a.n.compareTo(b.n);
    });
  for (final x in objs) {
    final o = x.o;
    if (o.removed && !edited) {
      return 'A message never edited has a roll marked removed.';
    }
    if (!o.drew) continue;
    final i = o.i;
    if (i == null || i < next) {
      return 'Object $i of the draw is out of order or repeated.';
    }
    if (i > next && !edited) return 'Object $i of the draw leaves a gap.';
    next = i + 1;
  }
  return null;
}

/// The view and object entities of a message's JSON, as [checkIndexes]
/// reads them, and whether it was edited.
({ObjectIndex? view, List<ObjectIndex> objects, bool edited}) indexesOf(
  Map<String, dynamic> m,
) {
  ObjectIndex of(int at, Map v) {
    final d = v['draw'];
    final i = d is Map ? d['i'] : null;
    return (
      at: at,
      drew: d is Map,
      i: i is int ? i : null,
      removed: v['removed'] == true,
    );
  }

  final view = m['view'];
  return (
    view: view is Map ? of(-1, view) : null,
    objects: [
      for (final e in (m['entities'] as List? ?? const []))
        if (e is Map && e['type'] == 'object' && e['object'] is Map)
          of(e['at'] is int ? e['at'] as int : 0, e['object'] as Map),
    ],
    edited: (m['edited_at'] as String? ?? '').isNotEmpty,
  );
}
