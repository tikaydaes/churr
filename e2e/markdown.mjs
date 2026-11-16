// Runs the shared Churr Markdown corpus (testdata/markdown) against the
// web chat's parser, internal/web/static/shared/md/parse.js.
//
// A client parser draws from the tree and from the entities the server
// sent; it never resolves names. So, as testdata/markdown/README.md says a
// client implementation should, this checks:
//
//   1. the tree of every case, compared as JSON values;
//   2. that drawing the tree with the case's entities marks exactly the
//      ranges they cover (the renderer's own matching of entities to
//      nodes, run without a page);
//   3. the checklist items the tree holds against the case's `tasks`;
//   4. that the objects of ruling R73 the client finds for itself (for a
//      sealed chat, and in the composer) are the case's entities of those
//      types, given the case's patterns and hints.
//
// The case's `entities` and `plain` are the server's to produce and are
// not recomputed here. It needs no server and no browser: `node
// e2e/markdown.mjs`.

import { readFileSync, readdirSync } from 'node:fs';
import { parse } from '../internal/web/static/shared/md/parse.js';
import { entityAt, objectsIn, taskItems } from '../internal/web/static/shared/md/entities.js';
import { findObjects, OBJECT_KINDS } from '../internal/web/static/shared/md/objects.js';

const dir = new URL('../testdata/markdown/', import.meta.url).pathname;
const files = readdirSync(dir).filter((f) => f.endsWith('.json') && f !== 'resolver.json').sort();

// Absent and empty mean the same for the lists of children.
const LISTS = new Set(['c', 'b', 'items', 'head', 'rows', 'align']);
function norm(v) {
  if (Array.isArray(v)) return v.map(norm);
  if (v && typeof v === 'object') {
    const out = {};
    for (const k of Object.keys(v).sort()) {
      const x = norm(v[k]);
      if (LISTS.has(k) && Array.isArray(x) && x.length === 0) continue;
      if (x === undefined || x === null) continue;
      out[k] = x;
    }
    return out;
  }
  return v;
}
const same = (a, b) => JSON.stringify(norm(a)) === JSON.stringify(norm(b));

// Every inline node of a tree, in order, with whether it sits inside a
// checklist item.
function* inlines(nodes) {
  for (const n of nodes || []) {
    if (n.t === 'list') { for (const it of n.items || []) { yield* inlines(it.c); yield* inlines(it.b); } continue; }
    if (n.t === 'table') { for (const cell of [...(n.head || []), ...(n.rows || []).flat()]) yield* inlines(cell.c); continue; }
    if (n.t === 'quote') { yield* inlines(n.b); continue; }
    if (n.t === 'p' || n.t === 'h') { yield* inlines(n.c); continue; }
    yield n;
    if (n.c) yield* inlines(n.c);
  }
}

let cases = 0, failures = 0;
const names = new Set();
const fail = (file, name, what, detail) => {
  failures++;
  console.log(`FAIL markdown: ${file} "${name}": ${what}${detail ? '\n       ' + detail : ''}`);
};

for (const file of files) {
  for (const c of JSON.parse(readFileSync(dir + file, 'utf8'))) {
    cases++;
    if (names.has(c.name)) fail(file, c.name, 'the name is used twice');
    names.add(c.name);
    const tree = parse(c.text);

    if (!same(tree, c.tree)) {
      fail(file, c.name, 'the tree differs', `want ${JSON.stringify(norm(c.tree))}\n       got  ${JSON.stringify(norm(tree))}`);
      continue;
    }

    // Drawing with the entities: every entity but a task must be claimed
    // by exactly one node covering exactly its range, and no node may
    // claim an entity that is not there.
    const entities = (c.entities || []).filter((e) => e.type !== 'task');
    const claimed = new Set();
    for (const n of inlines(tree)) {
      const e = entityAt(c.entities || [], n);
      if (!e) continue;
      if (e.at !== n.at || e.len !== n.len) fail(file, c.name, `a ${n.t} node at ${n.at} was matched to an entity of another range`);
      if (claimed.has(e)) fail(file, c.name, `the entity at ${e.at} was drawn twice`);
      claimed.add(e);
    }
    // What lies inside a run of text: hinted instants and objects.
    for (const n of inlines(tree)) {
      for (const e of objectsIn(c.entities || [], n)) {
        if (claimed.has(e)) fail(file, c.name, `the entity at ${e.at} was drawn twice`);
        claimed.add(e);
      }
    }
    for (const e of entities) {
      if (!claimed.has(e)) fail(file, c.name, `the ${e.type} entity at ${e.at} was not drawn`, JSON.stringify(e));
    }

    // The objects, found as the server finds them.
    const r = c.resolver || {};
    const objs = findObjects(c.text, { patterns: r.patterns || [], hints: r.object_hints || [] }, tree);
    const wantObjs = (c.entities || []).filter((e) => OBJECT_KINDS.includes(e.type));
    if (!same(objs, wantObjs)) fail(file, c.name, 'the objects differ', `want ${JSON.stringify(norm(wantObjs))}\n       got  ${JSON.stringify(norm(objs))}`);

    // The checklist items of the tree against the case's tasks.
    const items = taskItems(tree, c.text);
    const want = (c.tasks || []).map((t) => ({ at: t.at, len: t.len, text: t.text, checked: t.checked }));
    if (JSON.stringify(items.map(({ at, len, text, checked }) => ({ at, len, text, checked }))) !== JSON.stringify(want)) {
      fail(file, c.name, 'the checklist items differ', `want ${JSON.stringify(want)}\n       got  ${JSON.stringify(items)}`);
    }
    // And each item's task entity is found by its range.
    const taskEntities = (c.entities || []).filter((e) => e.type === 'task');
    items.forEach((it, i) => {
      const e = taskEntities[i];
      if (taskEntities.length && (!e || e.at !== it.at || e.len !== it.len)) fail(file, c.name, `checklist item ${i + 1} has no task entity over its text`);
    });
  }
}

// A wrong path must not pass by finding nothing.
if (cases < 60) { failures++; console.log(`FAIL markdown: the corpus holds only ${cases} cases; expected at least 60`); }
console.log(`${failures ? 'FAIL' : 'ok  '} markdown: ${cases} cases from ${files.length} files, ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
