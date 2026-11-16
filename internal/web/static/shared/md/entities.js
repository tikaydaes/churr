// Matching the entities the server sent to the nodes of the parsed tree.
//
// The parser makes candidate nodes from the shape of the text alone
// (mention, channel, emoji, url). Whether a candidate is anything is the
// server's answer, stored as entities beside the message. A candidate with
// an entity over exactly its range is drawn as that entity; one without is
// drawn as the text it covers (docs/MESSAGE-FORMAT.md section 3). The
// client never resolves a name itself.
//
// This file has no DOM in it, so the corpus runner can use it as it is.

// Which entity types a kind of node may be drawn as.
const DRAWS = {
  mention: ['account', 'group', 'broadcast'],
  channel: ['conversation'],
  emoji: ['emoji'],
  url: ['link', 'message_link'],
  instant: ['instant', 'event'],
};

// entityAt returns the entity that covers exactly the node, if the node is
// a candidate and the entity is of a type that node can be.
export function entityAt(entities, node) {
  const types = DRAWS[node.t];
  if (!types || !entities) return null;
  for (const e of entities) {
    if (e.at === node.at && e.len === node.len && types.includes(e.type)) return e;
    if (e.at > node.at) break; // entities are in order of `at`
  }
  return null;
}

// INSIDE is the entity types that lie inside a run of plain text rather
// than over a node: instants the author's client resolved from phrases
// ("this Friday at 5pm", ruling R69), and the objects of ruling R73
// (objects.js). The renderer cuts the text around them.
export const INSIDE = new Set(['instant', 'reference', 'message_link', 'measure', 'colour']);

// objectsIn returns the entities of those types that lie inside a text
// node, in order of `at`.
export function objectsIn(entities, node) {
  if (!entities || node.t !== 'text') return [];
  const end = node.at + node.len;
  return entities.filter((e) => INSIDE.has(e.type) && e.at >= node.at && e.at + e.len <= end && e.len > 0);
}

// taskItems lists the checklist items of a tree in order: the range of
// each item's text after its box, that text, and the box the author typed.
// The n-th item is the n-th task entity of the message.
export function taskItems(tree, text) {
  const out = [];
  const walk = (nodes) => {
    for (const n of nodes || []) {
      if (n.t === 'quote') walk(n.b);
      else if (n.t === 'list') {
        for (const it of n.items || []) {
          if (it.checked !== undefined) {
            const at = itemTextStart(it, text), len = it.at + it.len - at;
            out.push({ at, len, text: text.slice(at, at + len), checked: it.checked, node: it });
          }
          walk(it.b);
        }
      }
    }
  };
  walk(tree);
  return out;
}

// Where the text of a checklist item starts: after the marker, the box and
// the spaces that follow it.
function itemTextStart(it, text) {
  if (it.c && it.c.length) return it.c[0].at;
  let i = it.at + 1;
  while (text[i] === ' ' || text[i] === '\t') i++;
  i += 3;
  while (text[i] === ' ' || text[i] === '\t') i++;
  return i;
}
