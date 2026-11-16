// What a composer needs for instants (ruling R69): where a phrase may be
// made into a time, keeping an accepted phrase's range as the text around
// it changes, and turning a typed instant back into plain text. No DOM.

import { parse } from '../md/parse.js';

// textRuns lists the runs of plain text of a text's tree, outside links
// and code: where the server accepts a hint (markdown.TextSpans).
export function textRuns(text) {
  const out = [];
  const inl = (nodes) => {
    for (const n of nodes || []) {
      if (n.t === 'text' || n.t === 'instant') out.push([n.at, n.at + n.len]);
      else if (n.c && n.t !== 'link' && n.t !== 'nolink' && n.t !== 'image') inl(n.c);
    }
  };
  const blk = (nodes) => {
    for (const n of nodes || []) {
      if (n.t === 'p' || n.t === 'h') inl(n.c);
      else if (n.t === 'quote') blk(n.b);
      else if (n.t === 'list') for (const it of n.items || []) { inl(it.c); blk(it.b); }
      else if (n.t === 'table') for (const c of [...(n.head || []), ...(n.rows || []).flat()]) inl(c.c);
    }
  };
  blk(parse(String(text || '')));
  return out;
}

// hintFits says whether [start, end) may carry a hint: inside one run of
// plain text, overlapping no typed instant and no other hint.
export function hintFits(text, start, end, others = []) {
  const runs = textRuns(text);
  if (!runs.some(([a, b]) => start >= a && end <= b)) return false;
  const instants = [];
  const inl = (nodes) => { for (const n of nodes || []) { if (n.t === 'instant') instants.push([n.at, n.at + n.len]); if (n.c) inl(n.c); } };
  const blk = (nodes) => { for (const n of nodes || []) { inl(n.c); blk(n.b); for (const it of n.items || []) { inl(it.c); blk(it.b); } } };
  blk(parse(text));
  return ![...instants, ...others.map((o) => [o.start, o.end])].some(([a, b]) => start < b && a < end);
}

// track moves accepted hints to where their words are in the new text: the
// same place, or the nearest place the same words are found. A hint whose
// words were edited is dropped, so the phrase may be offered again.
export function track(hints, text) {
  const out = [];
  for (const hnt of hints) {
    let best = -1;
    if (text.slice(hnt.start, hnt.end) === hnt.typed) best = hnt.start;
    else {
      for (let i = text.indexOf(hnt.typed); i >= 0; i = text.indexOf(hnt.typed, i + 1)) {
        if (best < 0 || Math.abs(i - hnt.start) < Math.abs(best - hnt.start)) best = i;
      }
    }
    if (best < 0) continue;
    const moved = { ...hnt, start: best, end: best + hnt.typed.length };
    if (hintFits(text, moved.start, moved.end, out)) out.push(moved);
  }
  return out;
}
