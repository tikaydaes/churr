// Checks, without a browser, the logic of "Validate result"
// (docs/DESIGN-FAIR.md section 12; the document is docs/FAIR.md section 11):
// the document a message's draw makes, through the fragment and back, and
// its separate verdicts on good data and on each kind of bad data: a proof
// tampered with, a wrong beta, a wrong outcome, an alpha that is not well
// formed, and a key that is not attributed, not the server's, or not in
// force at the draw's second. testdata/fair/message.json is the draw.
//
//   node e2e/validate.mjs

import { readFileSync } from 'node:fs';

const S = '../internal/web/static/shared/';
const { validate, readDocument, fragmentOf, fromFragment, makeDocument, fetchHistory, readAlpha, SPEC, LIBRARY } = await import(S + 'fair/validate.js');
const { exportMessage } = await import(S + 'fair/export.js');

let checks = 0, failures = 0;
const check = (what, ok, detail) => {
  checks++;
  if (!ok) { failures++; console.log(`FAIL validate: ${what}${detail !== undefined ? '\n       ' + JSON.stringify(detail) : ''}`); }
};

const mv = JSON.parse(readFileSync(new URL('../testdata/fair/message.json', import.meta.url), 'utf8'));
const keys = { name: mv.server, history: mv.keys };
const verdict = (vs, id) => vs.filter((v) => v.id === id);
const one = (vs, id) => verdict(vs, id)[0];

// The document of the message's draw.
const doc = await exportMessage(mv.message);
check('the document says its layout and where to read how to check it', doc.churr_fair === 1 && doc.suite === 'ECVRF-SECP256K1-SHA256-TAI' && doc.spec === SPEC && doc.library === LIBRARY && doc.derive === 1, doc);
check('it carries the block as it is', doc.alpha === mv.message.fair.alpha && doc.beta === mv.message.fair.beta && doc.proof === mv.message.fair.proof && doc.pk === mv.message.fair.pk && doc.owner === mv.server);
check('and each value that drew, its primitives and claimed results', doc.objects.length === 2 && doc.objects.every((o) => o.primitives.length === 1 && o.primitives[0].op === 'dice' && o.results.length === 1), doc.objects);
const only = await exportMessage(mv.message, [1]);
check('a shield naming one outcome exports that one', only.objects.length === 1 && only.objects[0].i === 1);

// Through a link's fragment and back, and as pasted text.
const back = fromFragment(fragmentOf(doc));
check('the fragment carries it whole', JSON.stringify(back) === JSON.stringify(doc));
check('pasted, it reads', readDocument(JSON.stringify(doc)).doc.alpha === doc.alpha);
check('not JSON is said so', !!readDocument('{nope').error);
check('a document missing its proof is said so', /proof/.test(readDocument(JSON.stringify({ ...doc, proof: undefined })).error || ''));
check('another layout is said so', /layout 2/.test(readDocument(JSON.stringify({ ...doc, churr_fair: 2 })).error || ''));
check('a fragment that is not one gives nothing', fromFragment('#%%%') === null && fromFragment('') === null);

// Good data: every verdict passes, each on its own.
const good = await validate(doc, keys);
check('good: the proof is valid', one(good, 'proof').ok === true, one(good, 'proof'));
check('good: beta is its output', one(good, 'beta').ok === true, one(good, 'beta'));
check('good: one line per primitive, each re-derived', verdict(good, 'outcome').length === 2 && verdict(good, 'outcome').every((v) => v.ok === true), verdict(good, 'outcome'));
check('good: alpha is well formed, its time shown', one(good, 'alpha').ok === true && one(good, 'alpha').time === new Date(readAlpha(doc.alpha).t * 1000).toISOString());
check('good: the key is the server\'s, in force then', one(good, 'key').ok === true && one(good, 'key').why.includes(mv.server), one(good, 'key'));
check('good: every verdict has its reason', good.every((v) => typeof v.why === 'string' && v.why.length > 10));

// Indexes that repeat or fall back are refused: a draw numbers its objects
// once each (FAIR.md section 3).
const twice = await validate({ ...doc, objects: [doc.objects[0], { ...doc.objects[1], i: doc.objects[0].i }] }, keys);
check('objects that share an index are refused', verdict(twice, 'outcome').some((v) => v.ok === false && /repeat or are out of order/.test(v.why)));
const backwards = await validate({ ...doc, objects: [doc.objects[1], doc.objects[0]] }, keys);
check('objects whose indexes fall back are refused', verdict(backwards, 'outcome').some((v) => v.ok === false && /out of order/.test(v.why)));
check('good: indexes in order pass', !verdict(good, 'outcome').some((v) => /out of order/.test(v.why)));

// A proof tampered with: the proof fails, beta no longer matches it; the
// outcomes, from beta, are still what beta gives.
const flip = (s) => { const c = s.split(''); const at = 20; c[at] = c[at] === 'A' ? 'B' : 'A'; return c.join(''); };
const tampered = await validate({ ...doc, proof: flip(doc.proof) }, keys);
check('a tampered proof: the proof fails', one(tampered, 'proof').ok === false, one(tampered, 'proof'));
check('a tampered proof: beta is not its output', one(tampered, 'beta').ok === false, one(tampered, 'beta'));
check('a tampered proof: the outcomes are judged on their own', verdict(tampered, 'outcome').every((v) => v.ok === true));

// A wrong beta: the proof is still valid, but beta is not its output, and
// the outcomes re-derived from it are not the claimed ones.
const otherBeta = 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA';
const wrongBeta = await validate({ ...doc, beta: otherBeta }, keys);
check('a wrong beta: the proof is valid', one(wrongBeta, 'proof').ok === true);
check('a wrong beta: beta is not the output', one(wrongBeta, 'beta').ok === false);
check('a wrong beta: the outcomes are not the claimed', verdict(wrongBeta, 'outcome').some((v) => v.ok === false));

// A wrong outcome: only that primitive's line fails.
const lied = structuredClone(doc);
lied.objects[1].results[0].rows[0].terms[0].rolls[0] = lied.objects[1].results[0].rows[0].terms[0].rolls[0] === 6 ? 5 : 6;
lied.objects[1].results[0].rows[0].terms[0].value += 1;
lied.objects[1].results[0].rows[0].total += 1;
const wrongOutcome = await validate(lied, keys);
check('a wrong outcome: the proof and beta pass', one(wrongOutcome, 'proof').ok && one(wrongOutcome, 'beta').ok);
check('a wrong outcome: its line fails, the other passes', JSON.stringify(verdict(wrongOutcome, 'outcome').map((v) => [v.i, v.ok])) === JSON.stringify([[0, true], [1, false]]), verdict(wrongOutcome, 'outcome'));
check('a wrong outcome: it says what beta gives', verdict(wrongOutcome, 'outcome')[1].why.includes('not the claimed'));
const generic = await validate(makeDocument({ ...doc, objects: [{ i: 0, primitives: [{ op: 'int', min: 1, max: 6 }, { op: 'yesno', num: 1, den: 2 }], results: [{ value: 9 }, { yes: true }] }] }), keys);
check('a primitive outside derive 1\'s ranges of result fails, alone', verdict(generic, 'outcome')[0].ok === false && verdict(generic, 'outcome').length === 2, verdict(generic, 'outcome'));
const notOne = await validate(makeDocument({ ...doc, objects: [{ i: 0, primitives: [{ op: 'coin' }, { op: 'int', min: 1, max: 6 }], results: [{}, { value: 1 }] }] }), keys);
check('a primitive derive 1 does not have fails, and those after it are not checked', verdict(notOne, 'outcome').every((v) => v.ok === false) && /not a primitive/.test(verdict(notOne, 'outcome')[0].why) && /Not checked/.test(verdict(notOne, 'outcome')[1].why));

// A malformed alpha: it is said not to be well formed, and the proof is
// not valid over it.
for (const bad of ['01790000000|' + readAlpha(doc.alpha).nonce, '1790000000|' + readAlpha(doc.alpha).nonce.toUpperCase(), '1790000000-' + readAlpha(doc.alpha).nonce, '+1790000000|x', '']) {
  const r = await validate({ ...doc, alpha: bad }, keys);
  check(`a malformed alpha ${JSON.stringify(bad)} is said so`, one(r, 'alpha').ok === false && one(r, 'proof').ok === false, [one(r, 'alpha'), one(r, 'proof')]);
}

// Whose key: not attributed without a server, or when its history cannot be
// had; refused when the history does not hold it, or held it at another time.
check('no server named: key not attributed', one(await validate(doc, null), 'key').ok === null && /not attributed/.test(one(await validate(doc, null), 'key').why));
const unreached = await validate(doc, { name: 'gone.example', history: null, why: 'gone.example could not be reached from here.' });
check('a server that cannot be reached: key not attributed, with why', one(unreached, 'key').ok === null && one(unreached, 'key').why.includes('could not be reached'));
check('another server\'s history: not its key', one(await validate(doc, { name: 'b.example', history: [mv.keys[0]] }), 'key').ok === false);
const later = [{ key: mv.keys[0].key, began: mv.keys[0].began }, { key: mv.keys[1].key, began: '2099-01-01T00:00:00Z' }];
check('the server\'s key, but not in force at the draw\'s second', one(await validate(doc, { name: mv.server, history: later }), 'key').ok === false);

// The published history fetched, as a page does: from https://<name>.
const asked = [];
const fake = async (url) => { asked.push(url); return { ok: true, status: 200, json: async () => ({ name: mv.server, vrf_keys: mv.keys }) }; };
const got = await fetchHistory(mv.server, fake);
check('a server\'s history is fetched from its well-known address', asked[0] === `https://${mv.server}/.well-known/churr` && got.history.length === 2);
check('a name that is not one is not fetched', (await fetchHistory('a b/c', fake)).history === null && asked.length === 1);
check('a server that cannot be reached says so', (await fetchHistory('x.example', async () => { throw new Error('no'); })).why.includes('could not be reached'));

if (checks < 30) { failures++; console.log('FAIL validate: too few checks ran'); }
console.log(`${failures ? 'FAIL' : 'ok  '} validate: ${checks} checks, ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
