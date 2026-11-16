// A person's own sidebar (ruling R83, docs/FEATURES.md section 21.4):
// which heading each conversation sits under, the order of the headings,
// and the order of rows under each. The layout comes from the server
// (getSidebar, sidebar.changed). This module has no imports, so it can be
// checked under Node (e2e/arrange.mjs).
//
// Heading keys: p:<n> one of the person's own sections, s:<id> an admin
// section, s: channels in no section, chats, sealed.

export const SORTS = [['alphabetical', 'Alphabetical'], ['activity', 'Last activity'], ['custom', 'Custom order']];

export function emptyLayout() {
  return { version: 0, sections: [], headings: [], groups: [], placements: [], stars: [] };
}

export function defaultSort(key) { return key === 'chats' || key === 'sealed' ? 'activity' : 'alphabetical'; }

export function groupOf(layout, key) { return layout.groups.find((g) => g.key === key) || null; }
export function sortOf(layout, key) { const g = groupOf(layout, key); return g ? g.sort : defaultSort(key); }
export const isOwn = (key) => /^p:\d+$/.test(key);

// headings is every heading in the person's order: the channel headings
// (own sections, the admin's, Channels) as `layout.headings` says, those
// missing from it after, in the default order; then Chats and Sealed.
export function headings(layout, sections) {
  const def = [
    ...layout.sections.map((s) => ({ key: 'p:' + s.id, name: s.name, own: true })),
    ...sections.map((s) => ({ key: 's:' + s.id, name: s.name, own: false })),
    { key: 's:', name: 'Channels', own: false },
  ];
  const out = [];
  for (const k of layout.headings) { const h = def.find((d) => d.key === k); if (h && !out.includes(h)) out.push(h); }
  for (const h of def) if (!out.includes(h)) out.push(h);
  return [...out, { key: 'chats', name: 'Chats', own: false }, { key: 'sealed', name: 'Sealed', own: false }];
}

// homeKey is where the server lists a conversation, whatever the person
// did: its admin section (a sub-channel's parent's), Channels, Chats or
// Sealed. `look` gives { kind(c) → 'channel'|'chat'|'sealed', parent(c),
// sections }.
export function homeKey(c, look) {
  const kind = look.kind(c);
  if (kind !== 'channel') return kind === 'chat' ? 'chats' : 'sealed';
  const p = look.parent(c);
  const sid = (p ? p.section_id : c.section_id) || '';
  return sid && look.sections.some((s) => s.id === sid) ? 's:' + sid : 's:';
}

// keyOf is the heading a conversation is listed under: the person's own
// section if they put it (or its parent) in one, else its home.
export function keyOf(c, layout, look) {
  const own = (id) => {
    const p = layout.placements.find((x) => x.conversation_id === id);
    return p && layout.sections.some((s) => s.id === p.section_id) ? 'p:' + p.section_id : null;
  };
  const p = look.kind(c) === 'channel' ? look.parent(c) : null;
  return own(c.id) || (p && own(p.id)) || homeKey(c, look);
}

const activity = (c) => String(c.last_message_at || c.created_at || '');

// sortItems orders the rows of one heading: starred first, in the order of
// stars; then by the heading's sort. A custom order lists what it names
// first; the rest follow by name.
export function sortItems(items, key, layout, title) {
  const byName = (a, b) => title(a.c).localeCompare(title(b.c), undefined, { sensitivity: 'base' }) || String(a.c.created_at).localeCompare(String(b.c.created_at));
  const rank = (list, id) => { const i = list.indexOf(id); return i < 0 ? Infinity : i; };
  const sort = sortOf(layout, key);
  const order = (groupOf(layout, key) || {}).order || [];
  const starred = (x) => !!(x.c.membership && x.c.membership.starred);
  return [...items].sort((a, b) => {
    const sa = starred(a), sb = starred(b);
    if (sa !== sb) return sa ? -1 : 1;
    if (sa) return (rank(layout.stars, a.c.id) - rank(layout.stars, b.c.id)) || byName(a, b);
    if (sort === 'activity') return activity(b.c).localeCompare(activity(a.c)) || byName(a, b);
    if (sort === 'custom') return (rank(order, a.c.id) - rank(order, b.c.id)) || byName(a, b);
    return byName(a, b);
  });
}

// arrange puts the conversations that have rows under their headings. It
// returns every heading, in order, as { key, name, own, sort, items },
// where an item is { c, kids } and kids are the sub-channels drawn under
// it; headings with no rows have no items.
export function arrange(rows, layout, sections, look, title) {
  const keyed = new Map();
  const rowSet = new Set(rows);
  for (const c of rows) {
    const p = look.kind(c) === 'channel' ? look.parent(c) : null;
    const k = keyOf(c, layout, look);
    // A sub-channel sits under its parent's row when that row is there,
    // under the same heading.
    if (p && rowSet.has(p) && keyOf(p, layout, look) === k) continue;
    if (!keyed.has(k)) keyed.set(k, []);
    keyed.get(k).push(c);
  }
  return headings(layout, sections).map((h) => {
    const tops = keyed.get(h.key) || [];
    const items = tops.map((c) => ({
      c,
      kids: sortItems(rows.filter((k) => k !== c && look.kind(k) === 'channel' && look.parent(k) === c && keyOf(k, layout, look) === h.key).map((k) => ({ c: k })), 's:', { ...layout, groups: [] }, title).map((x) => x.c),
    }));
    return { ...h, sort: sortOf(layout, h.key), items: sortItems(items, h.key, layout, title) };
  });
}

// reorderOps is what moving a row to `index` among its heading's rows (as
// drawn, top-level only) sends. A starred row moves among the stars;
// any other among the rest, which makes the heading's order custom.
export function reorderOps(group, id, index, layout) {
  const starred = (x) => !!(x.c.membership && x.c.membership.starred);
  const it = group.items.find((x) => x.c.id === id);
  if (!it) return [];
  const pool = group.items.filter((x) => starred(x) === starred(it)).map((x) => x.c.id);
  const offset = starred(it) ? 0 : group.items.filter(starred).length;
  const ids = pool.filter((x) => x !== id);
  ids.splice(Math.max(0, Math.min(ids.length, index - offset)), 0, id);
  if (starred(it)) return [{ op: 'order_stars', ids: [...ids, ...layout.stars.filter((x) => !ids.includes(x))] }];
  return [{ op: 'reorder', key: group.key, ids: keepRest(ids, layout, group.key) }];
}

// moveOps is what moving a conversation to another heading sends: into
// one of the person's own sections, or back to where the server lists
// it. In a heading sorted by hand it goes in at `index`.
export function moveOps(c, target, index, layout) {
  const op = { op: 'move', conversation_id: c.id, key: target.key };
  if (sortOf(layout, target.key) === 'custom') {
    const starredCount = target.items.filter((x) => x.c.membership && x.c.membership.starred).length;
    const ids = target.items.filter((x) => !(x.c.membership && x.c.membership.starred)).map((x) => x.c.id).filter((x) => x !== c.id);
    ids.splice(Math.max(0, Math.min(ids.length, (index == null ? ids.length : index - starredCount))), 0, c.id);
    op.ids = keepRest(ids, layout, target.key);
  }
  return [op];
}

// keepRest keeps, after a new order of the rows drawn, the conversations
// the old order named that have no row now (hidden, or shown only on
// activity), so they are not forgotten.
function keepRest(ids, layout, key) {
  const old = (groupOf(layout, key) || {}).order || [];
  return [...ids, ...old.filter((x) => !ids.includes(x))];
}
