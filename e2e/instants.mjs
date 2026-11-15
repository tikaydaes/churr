// Instants in the web chat (ruling R69), without a server or a browser:
//
//   1. testdata/instants/resolve.json: a typed date and time resolves as
//      the server resolves it (the sending device does this in sealed
//      chats);
//   2. testdata/instants/phrases.json: the phrases the composer offers;
//   3. how an instant is shown, in a fixed reader's zone and locale;
//   4. the composer's helpers: where a hint may go, following it as the
//      text changes, sending a typed instant as plain text.
//
// The Markdown grammar itself is in the shared corpus (markdown.mjs,
// testdata/markdown/instants.json). `node e2e/instants.mjs`.

import { readFileSync } from 'node:fs';
import { resolve, instantEntities } from '../internal/web/static/shared/instant/zone.js';
import { find } from '../internal/web/static/shared/instant/phrases.js';
import { compact, full, relative, zoneName } from '../internal/web/static/shared/instant/format.js';
import { hintFits, track, asPlain } from '../internal/web/static/shared/instant/compose.js';
import { parse } from '../internal/web/static/shared/md/parse.js';

const data = (name) => JSON.parse(readFileSync(new URL('../testdata/instants/' + name, import.meta.url), 'utf8'));
let checks = 0, failures = 0;
const check = (what, got, want) => {
  checks++;
  const g = JSON.stringify(got), w = JSON.stringify(want);
  if (g !== w) { failures++; console.log(`FAIL instants: ${what}\n       want ${w}\n       got  ${g}`); }
};
// Intl puts a narrow no-break space before "pm" in some locales; the
// cases compare words, not spaces.
const sp = (s) => s.replace(/[  ]/g, ' ');

// 1. Resolution.
const rv = data('resolve.json');
for (const c of rv.cases) check(`resolve "${c.name}"`, resolve(c.local, c.zone, c.author_zone), c.want);
if (rv.cases.length < 30) { failures++; console.log('FAIL instants: resolve.json holds too few cases'); }

// 2. Phrases.
const pv = data('phrases.json');
for (const c of pv.cases) check(`phrase "${c.name}"`, find(c.text, { now: Date.parse(c.now), zone: c.zone, lang: pv.lang }), c.want);
if (pv.cases.length < 40) { failures++; console.log('FAIL instants: phrases.json holds too few cases'); }

// 3. Showing an instant: reader in New York, English as in Australia and
// in the United States, now Wednesday 18 November 2026, 09:00 there.
const now = Date.parse('2026-11-18T14:00:00Z');
const ny = { zone: 'America/New_York', now };
const at = (s) => Date.parse(s);
check('compact, another day this year (en-AU)', sp(compact(at('2026-11-20T19:30:00Z'), { ...ny, locale: 'en-AU' })), 'Fri, 20 Nov, 2:30 pm');
check('compact, another day this year (en-GB)', sp(compact(at('2026-11-20T19:30:00Z'), { ...ny, locale: 'en-GB' })), 'Fri 20 Nov, 14:30');
check('compact, another day this year (en-US)', sp(compact(at('2026-11-20T19:30:00Z'), { ...ny, locale: 'en-US' })), 'Fri, Nov 20, 2:30 PM');
check('compact, today', sp(compact(at('2026-11-18T20:00:00Z'), { ...ny, locale: 'en-AU' })), 'Today, 3:00 pm');
check('compact, tomorrow', sp(compact(at('2026-11-19T13:00:00Z'), { ...ny, locale: 'en-AU' })), 'Tomorrow, 8:00 am');
check('compact, yesterday', sp(compact(at('2026-11-17T13:00:00Z'), { ...ny, locale: 'en-AU' })), 'Yesterday, 8:00 am');
check('compact, tomorrow by the reader\'s clock though today in UTC', sp(compact(at('2026-11-19T04:59:00Z'), { ...ny, locale: 'en-AU' })), 'Today, 11:59 pm');
check('compact, another year', sp(compact(at('2027-01-08T17:00:00Z'), { ...ny, locale: 'en-AU' })), 'Fri, 8 Jan 2027, 12:00 pm');
check('compact, seconds typed', sp(compact(at('2026-11-20T19:30:05Z'), { ...ny, locale: 'en-AU', precision: 'second' })), 'Fri, 20 Nov, 2:30:05 pm');
check('compact, the e2e instant', sp(compact(at('2026-12-24T17:00:00Z'), { ...ny, locale: 'en-US' })), 'Thu, Dec 24, 12:00 PM');
check('full', sp(full(at('2026-12-24T17:00:00Z'), { zone: 'America/New_York', locale: 'en-AU' })), 'Thursday 24 December 2026, 12:00 pm');
check('full in an offset zone', sp(full(at('2026-12-24T08:00:00Z'), { zone: '+10:00', locale: 'en-AU' })), 'Thursday 24 December 2026, 6:00 pm');
check('zone name', zoneName('Europe/Berlin', at('2026-12-24T17:00:00Z')), 'Europe/Berlin (UTC+1)');
check('zone name in summer', zoneName('Europe/Berlin', at('2026-07-01T17:00:00Z')), 'Europe/Berlin (UTC+2)');
check('zone name, half hours', zoneName('Asia/Kolkata', at('2026-07-01T17:00:00Z')), 'Asia/Kolkata (UTC+5:30)');
check('zone name, an offset', zoneName('-05', 0), 'UTC-05');
check('zone name, Z', zoneName('Z', 0), 'UTC');
check('relative, to come', relative(at('2026-11-21T14:00:00Z'), { locale: 'en', now }), 'in 3 days');
check('relative, past', relative(at('2026-11-18T12:00:00Z'), { locale: 'en', now }), '2 hours ago');
check('relative, seconds', relative(now + 42000, { locale: 'en', now }), 'in 42 seconds');

// 4. The composer.
check('sealed entities: in the author\'s zone, not in a checklist', instantEntities('at 2026-12-24 18:00\n- [ ] 2026-12-24 19:00Z', 'Australia/Brisbane'),
  [{ type: 'instant', at: 3, len: 16, time: '2026-12-24T08:00:00.000Z', precision: 'minute', zone: 'Australia/Brisbane' }]);
check('sealed entities: no zone, no entity', instantEntities('at 2026-12-24 18:00', ''), []);
check('a hint inside text fits', hintFits('see you tomorrow at 5pm', 8, 23), true);
check('a hint in code does not', hintFits('`tomorrow at 5pm`', 1, 16), false);
check('a hint across formatting does not', hintFits('**tomorrow** at 5pm', 0, 19), false);
check('a hint over a typed instant does not', hintFits('2026-12-24 18:00Z', 0, 17), false);
const h0 = { start: 8, end: 23, typed: 'tomorrow at 5pm', time: 't', precision: 'minute', zone: 'UTC' };
check('a hint follows its words', track([h0], 'well, see you tomorrow at 5pm').map((x) => [x.start, x.end]), [[14, 29]]);
check('an edited phrase drops its hint', track([h0], 'see you tomorrow at 6pm'), []);
const plain = asPlain('Party 2026-12-24 18:00 Europe/Berlin', 6);
check('keep as text escapes the date', plain, 'Party 2026\\-12-24 18:00 Europe/Berlin');
check('and the grammar no longer reads it', JSON.stringify(parse(plain)).includes('"instant"'), false);

console.log(`${failures ? 'FAIL' : 'ok  '} instants: ${checks} checks, ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
