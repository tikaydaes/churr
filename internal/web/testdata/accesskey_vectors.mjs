// Runs static/shared/accesskey.js against testdata/accesskey/vectors.json
// under Node, which has the same Web Crypto as a browser. Called by
// accesskey_test.go with the module and the vectors as arguments.

import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';

const [modPath, vecPath] = process.argv.slice(2);
const ak = await import(pathToFileURL(modPath).href);
const v = JSON.parse(readFileSync(vecPath, 'utf8'));
const hex = (b) => Buffer.from(b).toString('hex');
const unhex = (s) => new Uint8Array(Buffer.from(s, 'hex'));
let n = 0;
const fail = (what) => { console.error('FAIL ' + what); process.exitCode = 1; };

if (ak.ALPHABET !== v.alphabet) fail('alphabet');
for (const k of v.keys) {
  const r = unhex(k.random);
  if (await ak.formatAccessKey(r) !== k.key) fail(`${k.name}: format`);
  if (hex(await ak.accessKeyAuth(r)) !== k.auth) fail(`${k.name}: auth`);
  if (hex(await ak.accessKeyWrap(r)) !== k.wrap) fail(`${k.name}: wrap`);
  const sum = new Uint8Array(await crypto.subtle.digest('SHA-256', await ak.accessKeyAuth(r)));
  if (hex(sum) !== k.auth_hash) fail(`${k.name}: auth hash`);
  if (hex(await ak.parseAccessKey(k.key)) !== k.random) fail(`${k.name}: parse`);
  n++;
}
for (const c of v.inputs) {
  try {
    const got = await ak.parseAccessKey(c.input);
    if (c.error) fail(`${c.name}: accepted, want ${c.error}`);
    else if (hex(got) !== c.random) fail(`${c.name}: read as ${hex(got)}`);
  } catch (err) {
    if (!c.error) fail(`${c.name}: ${err.message}`);
    else if (err.kind !== c.error || err.message !== c.message) fail(`${c.name}: ${err.kind} "${err.message}", want ${c.error} "${c.message}"`);
  }
  n++;
}
const made = await ak.newAccessKey();
if (hex(await ak.parseAccessKey(made.text)) !== hex(made.random)) fail('a new key does not read back');
if (!process.exitCode) console.log(`ok ${n} vectors`);
