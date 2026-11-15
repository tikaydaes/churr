// Checks, without a browser, what the web chat's store does with presence
// (docs/EVENTS.md section 9): `presence.changed` for one account, and
// `presence.batch`, which carries several of the same objects in one frame
// when a great many people's presence changes at once.

import { register } from 'node:module';

// The chat's modules import each other by the paths the server serves
// them at ('/shared/api.js'); these are mapped onto the source tree.
const STATIC = new URL('../internal/web/static/', import.meta.url).href;
register('data:text/javascript,' + encodeURIComponent(`
  export async function resolve(spec, ctx, next) {
    if (spec.startsWith('/shared/') || spec.startsWith('/chat/')) return next(${JSON.stringify(STATIC)} + spec.slice(1), ctx);
    return next(spec, ctx);
  }`));

// store.js starts a live connection object at load; it only listens.
globalThis.window = { addEventListener() {} };
globalThis.document = { addEventListener() {}, visibilityState: 'hidden' };

const { store, apply } = await import('../internal/web/static/chat/store.js');

let failures = 0, checks = 0;
function check(name, ok, detail) {
  checks++;
  if (!ok) { failures++; console.log(`FAIL presence: ${name}${detail ? '\n       ' + detail : ''}`); }
}

store.me = { id: 'me' };
for (const id of ['ana', 'ben', 'kim']) store.accounts.set(id, { id, username: id, presence: 'offline' });
let told = 0;
store.on('accounts', () => { told++; });
const of = (id) => store.accounts.get(id).presence;
const at = '2026-11-15T09:00:00.000Z';
const one = (account_id, presence, in_call = false) => ({ type: 'presence.changed', at, account_id, presence, in_call });

// The single event still works as it always has.
apply(one('ana', 'online'));
check('presence.changed sets one account', of('ana') === 'online' && of('ben') === 'offline');
check('presence.changed redraws once', told === 1, `told ${told} times`);

// A batch sets every account it names, and redraws once for all of them.
told = 0;
apply({ type: 'presence.batch', at, changes: [one('ana', 'away'), one('ben', 'online', true), one('kim', 'online')] });
check('presence.batch sets every account named', of('ana') === 'away' && of('ben') === 'online' && of('kim') === 'online',
  `${of('ana')} ${of('ben')} ${of('kim')}`);
check('presence.batch carries in_call', store.accounts.get('ben').in_call === true && store.accounts.get('kim').in_call === false);
check('presence.batch redraws once', told === 1, `told ${told} times`);

// An account the client does not hold is passed over, not invented.
told = 0;
apply({ type: 'presence.batch', at, changes: [one('zed', 'online'), one('kim', 'away')] });
check('an unknown account in a batch is passed over', !store.accounts.has('zed') && of('kim') === 'away');

// A single change after a batch replaces what the batch said.
apply(one('kim', 'offline'));
check('a later presence.changed wins over the batch', of('kim') === 'offline');

// A batch that is empty or malformed changes nothing and does not throw.
told = 0;
let threw = false;
try {
  apply({ type: 'presence.batch', at, changes: [] });
  apply({ type: 'presence.batch', at });
  apply({ type: 'presence.batch', at, changes: [null, 7, {}] });
} catch { threw = true; }
check('an empty or malformed batch is harmless', !threw && told === 0 && of('ana') === 'away', `threw ${threw}, told ${told}`);

console.log(`${failures ? 'FAIL' : 'ok  '} presence: ${checks} checks, ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
