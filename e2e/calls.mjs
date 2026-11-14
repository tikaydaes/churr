// The pure logic of calls in the web chat, run under Node: where tiles go,
// which video is asked for at what size, the request queue and its pace,
// the call state against frame sequences shaped as internal/calls sends
// them, and when ICE is restarted or given up on (docs/CALLS.md sections
// 7, 10 and 11). No server, no browser.

import { grid, arrange, subscriptions, sameSubscriptions, expectedLayer, order, MAX_SUBSCRIPTIONS } from '../internal/web/static/chat/call/layout.js';
import { Requests, reduce, initial, people, sharer, IceWatch, RESTART_TRIES } from '../internal/web/static/chat/call/signal.js';

let failures = 0;
const waiting = [];
function check(name, fn) {
  const pass = () => console.log(`ok   calls-logic: ${name}`);
  const fail = (e) => { failures++; console.log(`FAIL calls-logic: ${name} - ${e.message}`); };
  try {
    const r = fn();
    if (r && r.then) waiting.push(r.then(pass, fail)); else pass();
  } catch (e) { fail(e); }
}
function eq(a, b, what) {
  const x = JSON.stringify(a), y = JSON.stringify(b);
  if (x !== y) throw new Error(`${what || 'value'}: got ${x}, want ${y}`);
}
function ok(c, what) { if (!c) throw new Error(what); }

// A fake clock for the timers.
function clock() {
  let t = 0; const timers = [];
  return {
    now: () => t,
    setTimer: (fn, ms) => { const id = { at: t + ms, fn }; timers.push(id); return id; },
    clearTimer: (id) => { const i = timers.indexOf(id); if (i >= 0) timers.splice(i, 1); },
    advance(ms) {
      const end = t + ms;
      for (;;) {
        timers.sort((a, b) => a.at - b.at);
        const next = timers[0];
        if (!next || next.at > end) break;
        timers.shift(); t = next.at; next.fn();
      }
      t = end;
    },
  };
}

// --- layout ---

check('grid: one tile fills the area at 16:9', () => {
  const g = grid(1, 1600, 900);
  eq([g.cols, g.rows, g.w, g.h], [1, 1, 1600, 900]);
});
check('grid: four tiles on a wide area are two by two', () => {
  const g = grid(4, 1200, 700);
  eq([g.cols, g.rows], [2, 2]);
  ok(g.w * 2 + 8 <= 1200 && g.h * 2 + 8 <= 700, 'the tiles do not fit');
});
check('grid: three tiles on a phone stack in one column', () => {
  const g = grid(3, 360, 640);
  eq(g.cols, 1, 'columns');
});
check('grid: nine tiles never overflow the area', () => {
  for (const [w, hh] of [[1280, 720], [390, 700], [800, 300]]) {
    const g = grid(9, w, hh);
    ok(g.cols * g.w + (g.cols - 1) * 8 <= w && g.rows * g.h + (g.rows - 1) * 8 <= hh, `9 tiles overflow ${w}x${hh}`);
  }
});

const P = (id, o = {}) => ({ account_id: id, camera: o.cam || null, cameraOn: o.on !== false && !!o.cam, screen: o.screen || null });

check('order: the pinned first, then speakers loudest first, then by joining', () => {
  const list = [P('a'), P('b'), P('c'), P('d')];
  eq(order(list, { speakers: ['c', 'b'], pinned: 'd' }).map((p) => p.account_id), ['d', 'c', 'b', 'a']);
});

check('arrange: alone, the reader\'s own picture fills the area', () => {
  const l = arrange({ people: [], width: 1000, height: 600 });
  ok(l.preview.self, 'the preview is not the whole area');
  eq(l.tiles.length, 0);
});

check('arrange: a grid of everyone, with the preview in a corner', () => {
  const l = arrange({ people: [P('a', { cam: 't1' }), P('b', { cam: 't2' })], width: 1200, height: 700 });
  eq(l.mode, 'grid');
  eq(l.tiles.length, 2);
  ok(l.preview.x + l.preview.w <= 1200 && l.preview.y + l.preview.h <= 700, 'the preview is outside the area');
  for (const t of l.tiles) ok(t.x >= 0 && t.y >= 0 && t.x + t.w <= 1200 && t.y + t.h <= 700, 'a tile is outside the area');
});

check('arrange: a shared screen takes the main area, cameras in a strip beside it', () => {
  const l = arrange({ people: [P('a', { cam: 't1', screen: 't9' }), P('b', { cam: 't2' }), P('c', { cam: 't3' })], width: 1200, height: 700 });
  eq(l.mode, 'screen');
  const main = l.tiles.find((t) => t.main);
  eq([main.source, main.account_id], ['screen', 'a']);
  const strip = l.tiles.filter((t) => !t.main);
  eq(strip.map((t) => t.account_id).sort(), ['a', 'b', 'c'], 'the strip');
  for (const t of strip) ok(t.x >= main.x + main.w, 'a strip tile overlaps the screen');
});

check('arrange: on a phone the strip goes under the screen', () => {
  const l = arrange({ people: [P('a', { cam: 't1', screen: 't9' }), P('b', { cam: 't2' })], width: 390, height: 640 });
  const main = l.tiles.find((t) => t.main);
  for (const t of l.tiles.filter((x) => !x.main)) ok(t.y >= main.y + main.h, 'a strip tile is not under the screen');
});

check('arrange: pinning someone gives them the main tile', () => {
  const l = arrange({ people: [P('a', { cam: 't1' }), P('b', { cam: 't2' }), P('c', { cam: 't3' })], pinned: 'c', width: 1200, height: 700 });
  eq(l.mode, 'speaker');
  eq(l.tiles.find((t) => t.main).account_id, 'c');
  ok(!l.tiles.some((t) => !t.main && t.account_id === 'c'), 'the pinned person is also in the strip');
});

check('arrange: more people than tiles are counted as overflow', () => {
  const many = Array.from({ length: 20 }, (_, i) => P('p' + i, { cam: 't' + i }));
  const l = arrange({ people: many, width: 1400, height: 800 });
  eq(l.tiles.length, MAX_SUBSCRIPTIONS);
  eq(l.overflow, 4);
});

// --- subscriptions ---

check('subscriptions: live cameras at their tile size in physical pixels', () => {
  const ppl = [P('a', { cam: 't1' }), P('b', { cam: 't2', on: false })];
  const l = arrange({ people: ppl, width: 1200, height: 700 });
  const subs = subscriptions(l.tiles, ppl, { dpr: 2 });
  eq(subs.map((s) => s.track_id), ['t1'], 'a camera that is off is not asked for');
  const tile = l.tiles.find((t) => t.account_id === 'a');
  eq([subs[0].width, subs[0].height], [tile.w * 2, tile.h * 2]);
});

check('subscriptions: empty when nothing is on screen', () => {
  const ppl = [P('a', { cam: 't1' })];
  const l = arrange({ people: ppl, width: 1200, height: 700 });
  eq(subscriptions(l.tiles, ppl, { showing: false }), []);
});

check('subscriptions: the screen first, and never more than sixteen', () => {
  const many = Array.from({ length: 30 }, (_, i) => P('p' + i, { cam: 't' + i, screen: i === 7 ? 's7' : null }));
  const l = arrange({ people: many, width: 3000, height: 3000 });
  const subs = subscriptions(l.tiles, many, {});
  ok(subs.length <= MAX_SUBSCRIPTIONS, 'more than sixteen');
  eq(subs[0].track_id, 's7', 'first');
});

check('subscriptions: sizes mean the layer the server should pick', () => {
  eq([expectedLayer(150), expectedLayer(400), expectedLayer(1080)], ['q', 'h', 'f']);
  // A two-person grid on a laptop asks for h-sized tiles; a phone's strip for q.
  const ppl = [P('a', { cam: 't1' }), P('b', { cam: 't2' })];
  const big = subscriptions(arrange({ people: ppl, width: 1100, height: 600 }).tiles, ppl, { dpr: 1 });
  ok(big.every((s) => expectedLayer(s.height) !== 'q'), 'big tiles would get q');
  const phone = arrange({ people: [P('a', { cam: 't1', screen: 's1' }), P('b', { cam: 't2' })], width: 390, height: 640 });
  const strip = subscriptions(phone.tiles, [P('a', { cam: 't1', screen: 's1' }), P('b', { cam: 't2' })], { dpr: 1 }).filter((s) => s.track_id !== 's1');
  ok(strip.every((s) => expectedLayer(s.height) === 'q'), 'strip tiles would get more than q');
});

check('sameSubscriptions: a few pixels are not a change; a new track is', () => {
  const a = [{ track_id: 't1', width: 640, height: 360 }];
  ok(sameSubscriptions(a, [{ track_id: 't1', width: 650, height: 366 }]), 'small change counted');
  ok(!sameSubscriptions(a, [{ track_id: 't1', width: 1280, height: 720 }]), 'big change missed');
  ok(!sameSubscriptions(a, [{ track_id: 't2', width: 640, height: 360 }]), 'other track missed');
  ok(!sameSubscriptions(a, []), 'empty missed');
});

// --- requests ---

check('requests: ids, answers by reply_to, errors reject', async () => {
  const c = clock(); const sent = [];
  const r = new Requests({ send: (f) => { sent.push(f); return true; }, ...c });
  let got, err;
  r.request({ type: 'call.subscribe', video: [] }).then((f) => { got = f; });
  r.request({ type: 'call.mute', source: 'camera', muted: false }).catch((e) => { err = e; });
  eq(sent.map((f) => f.id), ['c1', 'c2']);
  r.answer({ type: 'call.error', reply_to: 'c2', code: 'cameras_full', message: 'Full.' });
  r.answer({ type: 'call.ok', reply_to: 'c1', video: [] });
  return Promise.resolve().then(() => { ok(got && got.reply_to === 'c1', 'no answer'); ok(err && err.code === 'cameras_full', 'no refusal'); });
});

check('requests: held frames wait; call.resume goes first; then the rest in order', () => {
  const c = clock(); const sent = []; let open = true;
  const r = new Requests({ send: (f) => { if (!open) return false; sent.push(f.type); return true; }, ...c });
  open = false;
  r.request({ type: 'call.subscribe', video: [] }).catch(() => {});
  r.tell({ type: 'call.candidate', pc: 'send', candidate: null });
  eq(sent, [], 'sent while closed');
  open = true;
  r.request({ type: 'call.resume', call_id: 'x', resume_token: 'y' }, { first: true }).catch(() => {});
  eq(sent, ['call.resume'], 'only the resume');
  r.release();
  eq(sent, ['call.resume', 'call.subscribe', 'call.candidate']);
});

check('requests: never more than the pace in one second', () => {
  const c = clock(); const at = [];
  const r = new Requests({ send: () => { at.push(c.now()); return true; }, ...c, rate: 12 });
  for (let i = 0; i < 30; i++) r.tell({ type: 'call.candidate', pc: 'send', candidate: 'x' });
  c.advance(5000);
  eq(at.length, 30, 'all sent in the end');
  for (let i = 12; i < at.length; i++) ok(at[i] - at[i - 12] >= 1000, `13 frames within a second at ${i}`);
});

check('requests: a request without an answer times out', () => {
  const c = clock(); let err;
  const r = new Requests({ send: () => true, ...c, timeout: 1000 });
  r.request({ type: 'call.mute', source: 'microphone', muted: true }).catch((e) => { err = e; });
  c.advance(1500);
  return Promise.resolve().then(() => ok(err && err.code === 'timeout', 'no timeout'));
});

// --- the call state against frame sequences ---

const ME = '0193a000-0000-7000-8000-000000000001';
const SAM = '0193a000-0000-7000-8000-000000000002';
const CONV = '0193a6f0-1111-7aaa-8bbb-000000000001';
const CALL = '0193b100-2c1e-7b3a-9f0e-2a4d5c6b7e8f';
const ev = (type, f) => ({ type, at: '2026-11-13T09:31:00.000Z', conversation_id: CONV, call_id: CALL, ...f });

// What the server sends a second participant who joins, publishes a
// microphone and a camera, subscribes, and sees the first share a screen
// and leave: the frames of internal/calls signal.go and room.go, in order.
const sequence = [
  { type: 'call.ok', reply_to: 'c1', resume_token: 'tok1', limits: { max_participants: 50 },
    call: { id: CALL, conversation_id: CONV, started_by_id: SAM, started_at: '2026-11-13T09:30:00.000Z', sealed: false, participants: [
      { account_id: SAM, joined_at: '2026-11-13T09:30:00.412Z', tracks: [{ id: 't1', source: 'microphone', kind: 'audio', muted: false }, { id: 't2', source: 'camera', kind: 'video', muted: false, codec: 'vp8', layers: ['q', 'h', 'f'], width: 1280, height: 720 }] },
      { account_id: ME, joined_at: '2026-11-13T09:31:00.000Z', tracks: [] }] } },
  ev('call.participant_joined', { participant: { account_id: ME, joined_at: '2026-11-13T09:31:00.000Z' } }),
  { type: 'call.answer', reply_to: 'c2', pc: 'send', sdp: 'v=0', tracks: [{ mid: '0', track_id: 't3' }, { mid: '1', track_id: 't4' }] },
  ev('call.track_published', { account_id: ME, track: { id: 't3', source: 'microphone', kind: 'audio', muted: false } }),
  ev('call.track_published', { account_id: ME, track: { id: 't4', source: 'camera', kind: 'video', muted: false, codec: 'vp8', layers: ['q', 'h', 'f'] } }),
  { type: 'call.offer', pc: 'recv', offer_id: 1, sdp: 'v=0', ice_restart: false, tracks: [{ mid: '0', track_id: 't1', account_id: SAM }] },
  { type: 'call.ok', reply_to: 'c5', video: [{ track_id: 't2', state: 'forwarding', layer: 'h' }] },
  ev('call.demand_changed', { track_id: 't4', layers: ['q'] }),
  ev('call.speakers_changed', { account_ids: [SAM] }),
  ev('call.mute_changed', { account_id: SAM, track_id: 't1', source: 'microphone', muted: true }),
  ev('call.layer_changed', { track_id: 't2', state: 'paused', reason: 'bandwidth' }),
  ev('call.track_published', { account_id: SAM, track: { id: 't5', source: 'screen', kind: 'video', muted: false, codec: 'vp8', width: 1920, height: 1080 } }),
  // A frame of another call is ignored.
  { type: 'call.track_published', at: 'x', conversation_id: CONV, call_id: 'other', account_id: SAM, track: { id: 't99', source: 'camera', kind: 'video' } },
  ev('call.track_unpublished', { account_id: SAM, track_id: 't5' }),
  ev('call.participant_left', { account_id: SAM, reason: 'left' }),
];

check('reduce: the join answer, publishing, subscribing, mute, speakers and layers', () => {
  let s = initial({ id: CALL, conversation_id: CONV, participants: [] }, ME);
  const states = [];
  for (const f of sequence) { s = reduce(s, f); states.push(s); }
  // after the join answer
  const joined = states[0];
  eq(Object.keys(joined.participants).sort(), [ME, SAM].sort(), 'participants');
  eq(people(joined).map((p) => [p.account_id, p.camera, p.cameraOn, p.microphone, p.micMuted]), [[SAM, 't2', true, 't1', false]]);
  // after the subscribe answer
  eq(states[6].layers, { t2: { state: 'forwarding', layer: 'h', reason: null } });
  eq(states[7].demand, { t4: ['q'] });
  eq(states[8].speakers, [SAM]);
  // a muted microphone is not a speaker
  eq(states[9].speakers, [], 'speakers after mute');
  eq(people(states[9])[0].micMuted, true);
  eq(states[10].layers.t2, { state: 'paused', layer: null, reason: 'bandwidth' });
  eq(sharer(states[11]), SAM, 'the sharer');
  ok(!states[12].participants[SAM].tracks.t99, 'a frame of another call was applied');
  eq(sharer(states[13]), null, 'the sharer after unpublishing');
  const last = states[states.length - 1];
  eq(Object.keys(last.participants), [ME], 'after Sam left');
  eq(last.layers, {}, 'Sam\'s layers stay after he left');
  eq(last.left, null, 'the reader is not out');
});

check('reduce: being moved to another device, and the call ending', () => {
  let s = initial({ id: CALL, conversation_id: CONV, participants: [] }, ME);
  s = reduce(s, sequence[0]);
  const moved = reduce(s, ev('call.participant_left', { account_id: ME, reason: 'joined_elsewhere' }));
  eq(moved.left, 'joined_elsewhere');
  const ended = reduce(s, ev('call.ended', { reason: 'empty', duration_seconds: 1284 }));
  eq(ended.ended, { reason: 'empty', duration_seconds: 1284 });
});

check('reduce: a resume answer replaces what was known', () => {
  let s = initial({ id: CALL, conversation_id: CONV, participants: [] }, ME);
  for (const f of sequence.slice(0, 9)) s = reduce(s, f);
  // While away, Sam left and someone else joined; the resume answer says so.
  const third = '0193a000-0000-7000-8000-000000000003';
  s = reduce(s, { type: 'call.ok', reply_to: 'c9', resume_token: 'tok2', call: { id: CALL, participants: [{ account_id: ME, tracks: [] }, { account_id: third, tracks: [{ id: 't7', source: 'camera', kind: 'video', muted: true }] }] } });
  eq(people(s).map((p) => [p.account_id, p.cameraOn]), [[third, false]]);
});

check('reduce: the state is replaced, not changed in place', () => {
  const s = initial({ id: CALL, conversation_id: CONV, participants: [] }, ME);
  const a = reduce(s, sequence[0]);
  const b = reduce(a, sequence[8]);
  ok(a !== b && a.speakers.length === 0 && b.speakers.length === 1, 'changed in place');
  ok(reduce(b, { type: 'call.candidate' }) === b, 'an unknown frame made a new state');
});

// --- ICE ---

check('ice: disconnected for three seconds restarts both; healthy again resets', () => {
  const c = clock(); const log = [];
  const w = new IceWatch({ onRestart: (n) => log.push('restart' + n), onRejoin: () => log.push('rejoin'), onHealthy: () => log.push('healthy'), ...c });
  w.set('send', 'connected'); w.set('recv', 'connected');
  w.set('send', 'disconnected');
  c.advance(2000);
  eq(log, [], 'restarted too soon');
  w.set('send', 'connected'); // came back by itself
  c.advance(5000);
  eq(log, [], 'restarted after it came back');
  w.set('recv', 'disconnected');
  c.advance(3100);
  eq(log, ['restart1']);
  w.set('recv', 'connected');
  eq(log, ['restart1', 'healthy']);
});

check('ice: failed restarts at once; three tries five seconds apart, then join again', () => {
  const c = clock(); const log = [];
  const w = new IceWatch({ onRestart: (n) => log.push('restart' + n), onRejoin: () => log.push('rejoin'), ...c });
  w.set('send', 'connected'); w.set('recv', 'connected');
  w.set('send', 'failed');
  eq(log, ['restart1']);
  c.advance(5000); eq(log, ['restart1', 'restart2']);
  c.advance(5000); eq(log, ['restart1', 'restart2', 'restart3']);
  c.advance(5000); eq(log, ['restart1', 'restart2', 'restart3', 'rejoin']);
  eq(RESTART_TRIES, 3);
});

await Promise.all(waiting);
console.log(failures ? `calls-logic: ${failures} check(s) failed` : 'calls-logic: all checks passed');
if (failures) process.exitCode = 1;
