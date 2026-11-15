// Checks, without a browser, when the web chat's live connection tries
// again (docs/CLIENTS.md section 7, ruling R63): every wait is drawn evenly
// between half the retry interval and the interval plus the spread the
// server gave in `hello`, over the steps 1, 2, 5, 10 and 30 seconds; a 4029
// close uses an interval of 30; and the wait that was chosen is the one the
// "reconnecting in N seconds" line counts down.

const LIVE = new URL('../internal/web/static/shared/live.js', import.meta.url).href;

// What the connection touches of a page, played by the test.
const listeners = {};
globalThis.window = { addEventListener(type, fn) { (listeners[type] ||= []).push(fn); } };
globalThis.document = { addEventListener() {}, visibilityState: 'visible' };
globalThis.location = { protocol: 'http:', host: 'chat.example.org' };
const sockets = [];
globalThis.WebSocket = class {
  static OPEN = 1;
  constructor(url, protocol) { this.url = url; this.protocol = protocol; this.readyState = 1; this.sent = []; this.on = {}; sockets.push(this); }
  addEventListener(type, fn) { this.on[type] = fn; }
  send(text) { this.sent.push(JSON.parse(text)); }
  close() { this.readyState = 3; }
  // The server's side.
  say(frame) { this.on.message({ data: JSON.stringify(frame) }); }
  end(code) { this.readyState = 3; this.on.close({ code }); }
};

const { Live, STEPS, DEFAULT_SPREAD, reconnectDelay, secondsUntil } = await import(LIVE);

let failures = 0, checks = 0;
function check(name, ok, detail) {
  checks++;
  if (!ok) { failures++; console.log(`FAIL reconnect: ${name}${detail ? '\n       ' + detail : ''}`); }
}

// --- the rule ---

check('the steps are 1, 2, 5, 10 and 30 seconds', JSON.stringify(STEPS) === '[1,2,5,10,30]');
check('the spread is 5 seconds until a server says', DEFAULT_SPREAD === 5);
for (const spread of [2, 5, 20, 60]) {
  for (const interval of STEPS) {
    const low = interval * 500, high = (interval + spread) * 1000;
    const name = `interval ${interval}, spread ${spread}`;
    check(`${name}: the shortest wait is half the interval`, reconnectDelay(interval, spread, 0) === low, String(reconnectDelay(interval, spread, 0)));
    check(`${name}: the longest wait is the interval and the spread`, reconnectDelay(interval, spread, 1) === high, String(reconnectDelay(interval, spread, 1)));
    check(`${name}: the middle draw is the middle`, reconnectDelay(interval, spread, 0.5) === Math.round((low + high) / 2));
    let out = 0, least = Infinity, most = 0;
    for (let i = 0; i < 2000; i++) {
      const d = reconnectDelay(interval, spread, Math.random());
      if (d < low || d > high) out++;
      least = Math.min(least, d); most = Math.max(most, d);
    }
    check(`${name}: two thousand draws stay in range`, out === 0, `${out} outside ${low}..${high}`);
    // Evenly drawn: both ends of the range are reached.
    check(`${name}: the draws use the whole range`, least < low + (high - low) * 0.05 && most > high - (high - low) * 0.05, `${least}..${most} of ${low}..${high}`);
  }
}
check('the lower bounds are 0.5, 1, 2.5, 5 and 15 seconds',
  JSON.stringify(STEPS.map((i) => reconnectDelay(i, 5, 0))) === '[500,1000,2500,5000,15000]');

// --- the countdown's number ---

check('no attempt waiting: nothing to count', secondsUntil(0, 1000) === null);
check('7.4 seconds left reads 8', secondsUntil(8400, 1000) === 8);
check('exactly 7 seconds left reads 7', secondsUntil(8000, 1000) === 7);
check('a moment under 7 reads 7', secondsUntil(7999, 1000) === 7);
check('the last second reads 1', secondsUntil(1001, 1000) === 1 && secondsUntil(2000, 1000) === 1);
check('never 0 while waiting', secondsUntil(1000, 1000) === 1 && secondsUntil(500, 1000) === 1);

// --- the connection ---

const hello = { type: 'hello', connection_id: 'c_1', heartbeat_seconds: 30, resume_within_seconds: 10 };
let unit = 0;
const states = [];
const live = new Live({ random: () => unit, positions: () => ({}), onState: (s) => states.push([s, live.retryAt]) });
const near = (got, want) => Math.abs(got - want) <= 50;
const wait = () => live.retryAt - Date.now();

live.start();
check('it connects, asking for the subprotocol', sockets.length === 1 && sockets[0].protocol === 'churr.v1');
check('connected: nothing to count', live.retryAt === 0 && live.retrySeconds() === null);

// An older server says nothing of a spread: the default stands.
sockets[0].say(hello);
check('a hello without the field leaves the default', live.spread === 5);
unit = 1;
sockets[0].end(1012);
check('the first wait, longest draw, default spread: 1 + 5 seconds', near(wait(), 6000), String(wait()));
check('the countdown shows that wait', live.retrySeconds() === 6, String(live.retrySeconds()));
check('whoever hears "offline" already has the moment', states.at(-1)[0] === 'offline' && states.at(-1)[1] === live.retryAt && live.retryAt > 0);
check('a second later it shows a second less', live.retrySeconds(Date.now() + 1000) === 5);

// Coming to the front, or the network coming back, connects at once.
listeners.online[0]();
check('"network is back" connects at once', sockets.length === 2 && live.retryAt === 0 && live.retrySeconds() === null);

// This server asks for twenty seconds.
sockets[1].say({ ...hello, reconnect_spread_seconds: 20 });
check('hello sets the spread', live.spread === 20);
sockets[1].end(1012);
check('the first wait, longest draw, spread 20: 21 seconds', near(wait(), 21000), String(wait()));
check('the countdown shows 21', live.retrySeconds() === 21);
live.wake();
unit = 0;
sockets[2].say(hello);
check('a later hello without the field keeps the last value heard', live.spread === 20);
sockets[2].end(1012);
check('the shortest draw is half the interval whatever the spread', near(wait(), 500), String(wait()));
check('and the countdown never shows less than 1', live.retrySeconds() === 1);
live.wake();

// The steps, while the server cannot be reached at all (no hello). The
// half second just checked was the first step.
unit = 0;
const lows = [];
for (let i = 0; i < 6; i++) { sockets.at(-1).end(1006); lows.push(Math.round(wait() / 100) * 100); live.wake(); }
check('without a hello the waits climb the steps and stay on the last', JSON.stringify(lows) === '[1000,2500,5000,15000,15000,15000]', JSON.stringify(lows));
sockets.at(-1).say({ ...hello, reconnect_spread_seconds: 2 });
unit = 1;
sockets.at(-1).end(1012);
check('a hello starts the steps again: 1 + 2 seconds', near(wait(), 3000), String(wait()));
live.wake();

// 4029: the same rule on thirty seconds.
sockets.at(-1).say({ ...hello, reconnect_spread_seconds: 20 });
unit = 0;
sockets.at(-1).end(4029);
check('4029, shortest draw: 15 seconds', near(wait(), 15000) && live.retrySeconds() === 15, String(wait()));
live.wake();
sockets.at(-1).say(hello);
unit = 1;
sockets.at(-1).end(4029);
check('4029, longest draw, spread 20: 50 seconds', near(wait(), 50000) && live.retrySeconds() === 50, String(wait()));
live.wake();

// What a server may say, and nonsense.
for (const [said, kept] of [[2, 2], [60, 60], [600, 60], [-3, 60], ['x', 60], [null, 60]]) {
  sockets.at(-1).say({ ...hello, reconnect_spread_seconds: said });
  check(`told a spread of ${JSON.stringify(said)}`, live.spread === kept, String(live.spread));
}

// Signed out is not something to count down to.
sockets.at(-1).end(4001);
check('4001 is not retried', states.at(-1)[0] === 'signed-out' && live.retryAt === 0);

live.stop();
console.log(`${failures ? 'FAIL' : 'ok  '} reconnect: ${checks} checks, ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
