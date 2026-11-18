// A message's draw as the "Validate result" document (docs/FAIR.md section
// 11): its block, and for each value that took chance from it (or those a
// shield names) the primitives it asked, in order, and the outcome its data
// claims as their results. A value that recorded its primitives (a module's
// declared outcomes) gives them as it recorded them; a built-in kind gives
// its composition (FAIR.md section 5a). No DOM.

import { read } from '../types/catalogue.js';
import { kindAsks, kindClaims } from './kinds.js';
import { bytesOf, documentOf, fragmentOf } from './validate.js';

/** Every value of a message that took chance from its draw: { where, value }. */
export function drawnValues(m) {
  const out = [];
  if (m && m.view && m.view.draw) out.push({ where: 'view', value: m.view });
  for (const e of (m && m.entities) || []) if (e.type === 'object' && e.object && e.object.draw) out.push({ where: 'object@' + e.at, value: e.object });
  return out;
}

/** The document of a message's draw, for the objects named (all when none are). */
export async function exportMessage(m, objects) {
  const beta = bytesOf(m.fair && m.fair.beta);
  const named = Array.isArray(objects) && objects.length ? new Set(objects) : null;
  const list = [];
  for (const { value: v } of drawnValues(m)) {
    const i = v.draw.i;
    if (named && !named.has(i)) continue;
    // A roll an edit changed keeps its index and claims no outcome.
    if (v.removed) continue;
    const r = read(v);
    const o = { i, kind: v.kind };
    if (r.step === 'kind' && !r.removed && beta) {
      o.primitives = Array.isArray(v.draw.primitives) && v.draw.primitives.length ? v.draw.primitives : kindAsks(r.kind, r.data);
      o.results = await kindClaims(r.kind, r.data, beta, i);
    }
    list.push(o);
  }
  return documentOf(m.fair || {}, list);
}

/** The address of the Validate result page with a message's draw filled in, in the fragment. */
export async function validateLink(m, objects, origin = '') {
  return origin + '/validate' + fragmentOf(await exportMessage(m, objects));
}
