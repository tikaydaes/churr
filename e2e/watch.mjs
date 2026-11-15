// Checks, without a browser, how the web chat's live connection tells the
// server which accounts it is showing (`presence.subscribe`, docs/EVENTS.md
// section 4): only to a server that said it takes the frame, only once the
// connection is ready, once for a view that is still changing, the whole
// set each time, and again on every new connection.

const LIVE = new URL('../internal/web/static/shared/live.js', import.meta.url).href;

globalThis.window = { addEventListener() {} };
globalThis.document = { addEventListener() {}, visibilityState: 'visible' };
globalThis.location = { protocol: 'http:', host: 'chat.example.org' };
const sockets = [];
globalThis.WebSocket = class {
  static OPEN = 1;
  constructor() { this.readyState = 1; this.sent = []; this.on = {}; sockets.push(this); }
  addEventListener(type, fn) { this.on[type] = fn; }
  send(text) { this.sent.push(JSON.parse(text)); }
  close() { this.readyState = 3; }
  say(frame) { this.on.message({ data: JSON.stringify(frame) }); }
  end(code) { this.readyState = 3; this.on.close({ code }); }
};

const { Live, WATCH_DELAY } = await import(LIVE);

let failures = 0, checks = 0;
function check(name, ok, detail) {
  checks++;
  if (!ok) { failures++; console.log(`FAIL watch: ${name}${detail ? '\n       ' + detail : ''}`); }
}
const settle = () => new Promise((r) => setTimeout(r, WATCH_DELAY + 50));
const subs = (s) => s.sent.filter((f) => f.type === 'presence.subscribe');
const hello = { type: 'hello', connection_id: 'c_1', heartbeat_seconds: 30, resume_within_seconds: 10 };

const live = new Live({ positions: () => ({}) });
live.start();

// A server from before subscriptions does not know the frame, and ten
// frames it cannot read close the connection: it is never sent one.
sockets[0].say(hello);
sockets[0].say({ type: 'ready', new_conversation_ids: [] });
live.watch(['b', 'a']);
await settle();
check('an older server is never sent presence.subscribe', subs(sockets[0]).length === 0, JSON.stringify(sockets[0].sent));
sockets[0].end(1012);
live.wake();

// A server that takes it: nothing before ready, then the set as it stands.
const s = sockets.at(-1);
s.say({ ...hello, presence_subscribe_max: 3 });
live.watch(['c', 'a', 'a', 'b']);
await settle();
check('nothing is sent before ready', subs(s).length === 0);
s.say({ type: 'ready', new_conversation_ids: [] });
check('ready sends the accounts on screen, each once', subs(s).length === 1 && JSON.stringify(subs(s)[0].account_ids) === '["a","b","c"]', JSON.stringify(subs(s)));

// A view that is still being put together is sent once, whole.
live.watch(['a']); live.watch(['a', 'd']); live.watch(['d', 'a', 'e']);
check('a change waits a moment', subs(s).length === 1);
await settle();
check('several changes in a moment are one frame with the whole set', subs(s).length === 2 && JSON.stringify(subs(s)[1].account_ids) === '["a","d","e"]', JSON.stringify(subs(s)));
live.watch(['e', 'd', 'a']);
await settle();
check('the same set is not sent again', subs(s).length === 2);
live.watch(['a', 'b', 'c', 'd', 'e']);
await settle();
check('no more accounts than the server takes', subs(s).length === 3 && subs(s)[2].account_ids.length === 3, JSON.stringify(subs(s).at(-1)));
live.watch([]);
await settle();
check('showing nobody is said too', subs(s).length === 4 && subs(s)[3].account_ids.length === 0);

// A new connection is showing nothing until told.
live.watch(['a']);
await settle();
s.end(1012);
live.wake();
const t = sockets.at(-1);
t.say({ ...hello, presence_subscribe_max: 500 });
t.say({ type: 'ready', new_conversation_ids: [] });
check('a new connection is told the set again', subs(t).length === 1 && JSON.stringify(subs(t)[0].account_ids) === '["a"]', JSON.stringify(subs(t)));

live.stop();
console.log(`${failures ? 'FAIL' : 'ok  '} watch: ${checks} checks, ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
