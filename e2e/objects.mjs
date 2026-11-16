// Objects in the web chat (ruling R73), without a server or a browser:
//
//   1. the composer's choices: which token is offered at the caret, its
//      rows, following decisions as the text changes, the hints sent;
//   2. what each key does while the list is open, for class A (exact
//      tokens) and class B (loose phrases);
//   3. sealed chats: the sending device's entities honour the hints.
//
// The grammar itself is in the shared corpus (markdown.mjs, which compares
// the client's scanner with testdata/markdown). `node e2e/objects.mjs`.

import { candidate, choose, track, hintsOf, undo, keyAction, PLAIN } from '../internal/web/static/shared/objects/compose.js';
import { findObjects } from '../internal/web/static/shared/md/objects.js';
import { instantEntities } from '../internal/web/static/shared/instant/zone.js';
import { convert, regionSystem } from '../internal/web/static/shared/objects/measure.js';
import { readFileSync } from 'node:fs';

let checks = 0, failures = 0;
const check = (what, got, want) => {
  checks++;
  const g = JSON.stringify(got), w = JSON.stringify(want);
  if (g !== w) { failures++; console.log(`FAIL objects: ${what}\n       want ${w}\n       got  ${g}`); }
};

const patterns = [
  { id: 'p1', name: 'Tracker', prefix: 'PROJ-', url_template: 'https://t.example/{value}', created_at: '2026-11-16T00:00:00.000Z' },
  { id: 'p2', name: 'Issues', prefix: '#', url_template: 'https://g.example/i/{value}', created_at: '2026-11-16T00:00:00.000Z' },
];
const ctx = { patterns, zone: 'Australia/Brisbane' };

// 1. Candidates.
let c = candidate('see PROJ-12', 11, ctx);
check('a reference at the caret', c && { start: c.start, end: c.end, typed: c.typed, rows: c.rows.map((r) => r.kind) }, { start: 4, end: 11, typed: 'PROJ-12', rows: ['reference', PLAIN] });
check('its entity', c.rows[0].entity.url, 'https://t.example/12');
check('not before the caret', candidate('see PROJ-12 now', 15, ctx), null);
check('not when incomplete', candidate('see PROJ-', 9, ctx), null);
check('not inside a word', candidate('xPROJ-12', 8, ctx), null);
check('not in code', candidate('`PROJ-12', 8, ctx) && candidate('`PROJ-12`', 9, ctx), null);
check('no patterns, nothing', candidate('PROJ-12', 7, { zone: 'UTC' }), null);
c = candidate('at 2026-12-24 18:00', 19, ctx);
check('a typed instant', c && { typed: c.typed, rows: c.rows.map((r) => r.kind) }, { typed: '2026-12-24 18:00', rows: ['instant', PLAIN] });
check('read in the author\'s zone', c.rows[0].entity.time, '2026-12-24T08:00:00.000Z');

// Decisions: taken once, not offered again, followed as the text moves.
c = candidate('PROJ-12', 7, ctx);
let decided = [choose(c, 0)];
check('the first row sends nothing', hintsOf(decided), []);
check('a decided token is not offered again', candidate('PROJ-12', 7, { ...ctx, decided }), null);
decided = [choose(c, 1)];
check('plain text sends a plain hint', hintsOf(decided), [{ start: 0, end: 7, kind: PLAIN }]);
decided = track(decided, 'Re: PROJ-12 ');
check('a decision follows its text', hintsOf(decided), [{ start: 4, end: 11, kind: PLAIN }]);
check('kept plain, it is not found', findObjects('Re: PROJ-12 ', { patterns, hints: hintsOf(decided) }), []);
check('a decision whose text changed is dropped', track(decided, 'Re: PROJ-123 '), []);
check('the longer token is offered again', candidate('Re: PROJ-123', 12, { ...ctx, decided: track(decided, 'Re: PROJ-123') }).typed, 'PROJ-123');
decided = undo([choose(candidate('PROJ-1', 6, ctx), 0)], 0, 6);
check('undo makes an accepted object plain', hintsOf(decided), [{ start: 0, end: 6, kind: PLAIN }]);
decided = [choose(candidate('at 2026-12-24 18:00', 19, ctx), 1)];
check('a typed instant kept as plain text', instantEntities('at 2026-12-24 18:00', 'UTC', hintsOf(decided)), []);
check('and without the hint it resolves', instantEntities('at 2026-12-24 18:00', 'UTC').length, 1);

// Each kind of object after references, offered as typed.
const LINK = 'https://chat.example/chat/c/0193a000-0000-7000-8000-0000000000a1/m/0193a000-0000-7000-8000-0000000000b1';
c = candidate('see ' + LINK, 4 + LINK.length, { ...ctx, origin: 'https://chat.example' });
check('a pasted message link', c && c.rows.map((r) => r.kind), ['message_link', PLAIN]);
check('its message', c.rows[0].entity.message_id, '0193a000-0000-7000-8000-0000000000b1');
check('no message link without the origin (a sealed chat)', candidate('see ' + LINK, 4 + LINK.length, ctx), null);

c = candidate('a 5 km', 6, ctx);
check('a measurement', c && c.rows.map((r) => r.kind), ['measure', PLAIN]);
check('its unit', c.rows[0].entity.unit, 'km');
check('not before the unit is complete', candidate('5 k', 3, ctx), null);

c = candidate('logo #fff', 9, ctx);
check('a colour', c && c.rows.map((r) => r.kind), ['colour', PLAIN]);
check('its value', c.rows[0].entity.value, '#ffffff');
c = candidate('#123456', 7, ctx);
check('a reference first, then the colour', c && c.rows.map((r) => r.kind), ['reference', 'colour', PLAIN]);
check('choosing the colour sends a hint', hintsOf([choose(c, 1)]), [{ start: 0, end: 7, kind: 'colour' }]);

// The equivalents beside measurements, as the app shows them too.
const conv = JSON.parse(readFileSync(new URL('../testdata/objects/conversions.json', import.meta.url), 'utf8'));
for (const x of conv.conversions) check(`${x.value} ${x.unit} for ${x.system}`, convert(x.value, x.unit, x.system), x.shows);
for (const x of conv.regions) check(`${x.locale} measures in`, regionSystem(x.locale), x.system);

// 2. Keys.
const A = (key, o) => keyAction('A', key, o);
const B = (key, o) => keyAction('B', key, o);
check('A: arrows move', [A('ArrowDown'), A('ArrowUp')], ['down', 'up']);
check('A: space commits', A(' '), 'commit');
check('A: punctuation commits', [A('.'), A(','), A('!'), A(')')], ['commit', 'commit', 'commit', 'commit']);
check('A: a letter or digit types on', [A('a'), A('7'), A('é')], ['', '', '']);
check('A: Enter commits and sends', A('Enter'), 'commit-send');
check('A: Shift+Enter commits, then a new line', A('Enter', { shift: true }), 'commit');
check('A: Escape is plain text', A('Escape'), 'plain');
check('A: Tab takes the row', A('Tab'), 'accept');
check('A: Backspace straight after accepting undoes', A('Backspace', { open: false, afterAccept: true }), 'undo');
check('A: Backspace otherwise deletes', [A('Backspace', { open: false }), A('Backspace')], ['', '']);
check('B: Tab and Enter accept', [B('Tab'), B('Enter')], ['accept', 'accept']);
check('B: space does not accept', B(' '), '');
check('B: Escape dismisses', B('Escape'), 'dismiss');
check('B: arrows move', [B('ArrowDown'), B('ArrowUp')], ['down', 'up']);

if (checks < 30) { failures++; console.log('FAIL objects: too few checks ran'); }
console.log(`${failures ? 'FAIL' : 'ok  '} objects: ${checks} checks, ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
