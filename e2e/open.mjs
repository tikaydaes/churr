// Checks, without a browser, how the web chat's live connection tells the
// server which conversations are on screen (`conversations.open` and `open`
// in `resume`, docs/EVENTS.md section 4.1): only to a server that said it
// delivers lazily, in `resume` on every connection, afterwards only once
// the connection is ready and only when the set changes, with the seq
// where what the page holds of each ends.

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

const { Live, openIds } = await import(LIVE);

let failures = 0, checks = 0;
function check(name, ok, detail) {
  checks++;
  if (!ok) { failures++; console.log(`FAIL open: ${name}${detail ? '\n       ' + detail : ''}`); }
}
const opens = (s) => s.sent.filter((f) => f.type === 'conversations.open');
const resume = (s) => s.sent.find((f) => f.type === 'resume');
const hello = { type: 'hello', connection_id: 'c_1', heartbeat_seconds: 30, resume_within_seconds: 10 };

let onScreen = {};
const live = new Live({ positions: () => ({ a: 7, b: 3 }), open: () => onScreen });
live.start();

// A server from before lazy delivery refuses a `resume` with a field it
// does not know, and does not know the frame: it is sent neither.
sockets[0].say(hello);
check('an older server is sent a plain resume', resume(sockets[0]) && !('open' in resume(sockets[0])), JSON.stringify(sockets[0].sent));
sockets[0].say({ type: 'ready', new_conversation_ids: [] });
live.open({ a: 7 });
check('an older server is never sent conversations.open', opens(sockets[0]).length === 0);
sockets[0].end(1012);
live.wake();

// A server that delivers lazily: `resume` says what is on screen, even
// when that is nothing, which is what marks the client as one that says.
onScreen = {};
let s = sockets.at(-1);
s.say({ ...hello, conversations_open_max: 2 });
check('resume carries an empty open list', JSON.stringify(resume(s).open) === '[]', JSON.stringify(resume(s)));
check('resume still carries every position', JSON.stringify(resume(s).positions) === '{"a":7,"b":3}');
live.open({ a: 7 });
check('nothing is sent before ready', opens(s).length === 0);
s.say({ type: 'ready', new_conversation_ids: [] });
check('ready sends what came to be on screen meanwhile', opens(s).length === 1 && JSON.stringify(opens(s)[0].positions) === '{"a":7}', JSON.stringify(opens(s)));
live.open({ a: 9 });
check('the same conversations are not said again', opens(s).length === 1);
live.open({ b: 3 });
check('another conversation is said, with where the page is', opens(s).length === 2 && JSON.stringify(opens(s)[1].positions) === '{"b":3}');
live.open({});
check('showing nothing is said too', opens(s).length === 3 && JSON.stringify(opens(s)[2].positions) === '{}');
live.open({ c: 1, a: 2, b: 3 });
check('no more conversations than the server takes', opens(s).length === 4 && Object.keys(opens(s)[3].positions).length === 2, JSON.stringify(opens(s).at(-1)));
check('openIds sorts and cuts', JSON.stringify(openIds({ z: 1, a: 2, m: 3 }, 2)) === '["a","m"]');

// A new connection says what is on screen in its resume and not again.
onScreen = { b: 4 };
s.end(1012);
live.wake();
s = sockets.at(-1);
s.say({ ...hello, conversations_open_max: 8 });
check('a new connection names what is on screen in resume', JSON.stringify(resume(s).open) === '["b"]', JSON.stringify(resume(s)));
s.say({ type: 'ready', new_conversation_ids: [] });
check('and does not repeat it after ready', opens(s).length === 0, JSON.stringify(opens(s)));
live.open({});
check('closing it is said', opens(s).length === 1);

live.stop();
console.log(`${failures ? 'FAIL' : 'ok  '} open: ${checks} checks, ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
