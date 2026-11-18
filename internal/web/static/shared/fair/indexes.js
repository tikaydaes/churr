// The canonical object indexes (docs/FAIR.md section 3), as internal/fair's
// CheckIndexes: a command's own value (the view), when it took chance, is
// object 0; the object entities that took chance follow in the order of
// their position `at`, from 1 if the view took chance and 0 if not, with no
// gap and no repeat. On an edited message a roll the edit changed is marked
// removed and keeps its index, and one it deleted leaves its index unused:
// the indexes still rise with the position, never repeat, and a gap is left
// only there. A message never edited has no gap and nothing removed. Held to
// testdata/fair/indexes.json.

/**
 * view: null, or { drew, i, removed }; objects: [{ at, drew, i, removed }];
 * edited: whether the message has been edited. Gives null when the indexes
 * are the canonical ones, or why not.
 */
export function checkIndexes(view, objects, edited) {
  let next = 0;
  if (view && view.removed) return 'The message\'s own value is marked removed.';
  if (view && view.drew) {
    if (view.i !== 0) return 'The command\'s own value is not object 0 of the draw.';
    next = 1;
  }
  const objs = [...(objects || [])].map((o, n) => ({ ...o, n })).sort((a, b) => a.at - b.at || a.n - b.n);
  for (const o of objs) {
    if (o.removed && !edited) return 'A message never edited has a roll marked removed.';
    if (!o.drew) continue;
    if (!Number.isSafeInteger(o.i) || o.i < next) return `Object ${o.i} of the draw is out of order or repeated.`;
    if (o.i > next && !edited) return `Object ${o.i} of the draw leaves a gap.`;
    next = o.i + 1;
  }
  return null;
}

/** The view and object entities of a message, as checkIndexes reads them. */
export function indexesOf(m) {
  const of = (at, v) => ({ at, drew: !!v.draw, i: v.draw ? v.draw.i : undefined, removed: !!v.removed });
  return {
    view: m.view ? of(-1, m.view) : null,
    objects: (m.entities || []).filter((e) => e.type === 'object' && e.object).map((e) => of(e.at, e.object)),
    edited: !!m.edited_at,
  };
}
