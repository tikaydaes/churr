// Checks, without a browser, how the web chat keeps drafts across reloads
// (shared/drafts.js, docs/CLIENTS.md section 22): per conversation and
// thread, under the account, gone when sent or emptied, old and surplus
// ones dropped, all of them gone on locking or signing out, and nothing
// failing where storage is closed.

const { keptDrafts, forgetDrafts, resetForTests } = await import(new URL('../internal/web/static/shared/drafts.js', import.meta.url).href);

let failures = 0, checks = 0;
function check(name, ok, detail) {
  checks++;
  if (!ok) { failures++; console.log(`FAIL drafts: ${name}${detail ? '\n       ' + detail : ''}`); }
}

class Memory {
  constructor() { this.m = new Map(); }
  getItem(k) { return this.m.has(k) ? this.m.get(k) : null; }
  setItem(k, v) { this.m.set(k, String(v)); }
  removeItem(k) { this.m.delete(k); }
  key(i) { return [...this.m.keys()][i] ?? null; }
  get length() { return this.m.size; }
}
const closed = { getItem() { throw new Error('closed'); }, setItem() { throw new Error('closed'); }, removeItem() { throw new Error('closed'); }, key() { throw new Error('closed'); }, get length() { throw new Error('closed'); } };

let clock = Date.UTC(2026, 10, 17);
const now = () => clock;
const store = new Memory();
store.setItem('churr.ada', '{"showHidden":true}');

let d = keptDrafts(store, 'ada', now);
d.set('c1:', { text: 'hello' });
d.set('c1:m9', { text: 'in a thread', quote: { message_id: 'm8', start: 0, end: 3 }, quoteOf: { id: 'm8', text: 'abc' } });
d = keptDrafts(store, 'ada', now); // a reload
check('a draft comes back after a reload', d.get('c1:')?.text === 'hello');
check('a thread has its own', d.get('c1:m9')?.text === 'in a thread' && d.get('c1:m9').quote.message_id === 'm8');
check('another account sees none of them', keptDrafts(store, 'sam', now).get('c1:') === null);
d.remove('c1:');
check('a sent draft is gone after a reload', keptDrafts(store, 'ada', now).get('c1:') === null);
d.remove('c1:m9');
check('with none left, nothing is stored', store.getItem('churr-drafts.ada') === null);

d.set('old:', { text: 'from last month' });
clock += 31 * 24 * 3600 * 1000;
check('a draft older than 30 days is dropped', keptDrafts(store, 'ada', now).get('old:') === null);

d = keptDrafts(store, 'ada', now);
for (let i = 0; i < 105; i++) { clock += 1; d.set(`c${i}:`, { text: 'n' + i }); }
const many = JSON.parse(store.getItem('churr-drafts.ada'));
check('at most 100 are kept, the newest', Object.keys(many).length === 100 && !many['c0:'] && many['c104:'], Object.keys(many).length);

keptDrafts(store, 'sam', now).set('c1:', { text: 'sam’s' });
forgetDrafts(store);
check('locking or signing out removes every account’s drafts', ![...store.m.keys()].some((k) => k.startsWith('churr-drafts.')), JSON.stringify([...store.m.keys()]));
check('and leaves the other preferences', store.getItem('churr.ada') === '{"showHidden":true}');
d.set('late:', { text: 'typed while the page goes' });
check('nothing is written after that', store.getItem('churr-drafts.ada') === null);
resetForTests();

const shut = keptDrafts(closed, 'ada', now);
let threw = false;
try { shut.set('c1:', { text: 'x' }); shut.remove('c1:'); forgetDrafts(closed); } catch { threw = true; }
resetForTests();
check('storage closed to the page fails nothing', !threw);
check('and the draft lasts for the page', (() => { const k = keptDrafts(closed, 'ada', now); k.set('c2:', { text: 'y' }); return k.get('c2:')?.text === 'y'; })());

store.setItem('churr-drafts.bob', 'not json');
check('a damaged entry is read as none', keptDrafts(store, 'bob', now).get('c1:') === null);

console.log(`${failures ? 'FAIL' : 'ok  '} drafts: ${checks} checks, ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
