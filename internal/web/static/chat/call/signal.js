// The signalling of a call (docs/CALLS.md sections 10 and 11), as pure
// parts the call controller (room.js) puts together:
//
//   Requests   ids, answers by `reply_to`, a queue while the live
//              connection is away, and a pace under the server's limit
//   reduce     what the call is, kept up to date from the frames
//   IceWatch   when to restart ICE, and when to give up and join again
//
// No DOM and no WebRTC here: the tests run this file under Node against
// recorded frame sequences.

// The server allows 20 frames a second from one connection, call
// signalling included (docs/EVENTS.md section 5). The client keeps well
// under it, since the chat sends its own frames on the same connection.
export const FRAMES_PER_SECOND = 12;
// How long a request may wait for its answer.
export const REQUEST_TIMEOUT = 15000;

export class SignalError extends Error {
  constructor(frame) {
    super((frame && frame.message) || 'The call server refused that.');
    this.name = 'SignalError';
    this.code = (frame && frame.code) || 'internal';
    this.frame = frame || {};
  }
}

// Requests sends `call.` requests and matches their answers.
//
//   send(frame) → bool   puts a frame on the live connection; false when
//                        it is not open
//   now(), setTimer(fn, ms), clearTimer(t)   the clock, replaceable in tests
//
// Frames wait in order while the connection is down (`hold()`), and go
// when `release()` is called: after `call.resume` has made the new
// connection the signalling one. `call.resume` itself and `call.join` are
// sent with `{ first: true }`, ahead of what waits.
export class Requests {
  constructor({ send, now = () => Date.now(), setTimer = (fn, ms) => setTimeout(fn, ms), clearTimer = (t) => clearTimeout(t), timeout = REQUEST_TIMEOUT, rate = FRAMES_PER_SECOND } = {}) {
    this.sendRaw = send;
    this.now = now;
    this.setTimer = setTimer;
    this.clearTimer = clearTimer;
    this.timeout = timeout;
    this.rate = rate;
    this.next = 1;
    this.pending = new Map(); // id → { resolve, reject, timer, frame }
    this.queue = [];
    this.held = false;
    this.sentAt = []; // times of the frames sent in the last second
    this.pump = null;
  }

  // request sends a frame with a fresh id and resolves with its answer:
  // `call.ok` or `call.answer`. `call.error` rejects with a SignalError.
  request(frame, opts = {}) {
    const id = 'c' + this.next++;
    const f = { ...frame, id };
    return new Promise((resolve, reject) => {
      const entry = { resolve, reject, frame: f, timer: null };
      this.pending.set(id, entry);
      entry.timer = this.setTimer(() => {
        if (!this.pending.has(id)) return;
        this.pending.delete(id);
        reject(new SignalError({ code: 'timeout', message: 'The call server did not answer.' }));
      }, opts.timeout || this.timeout);
      this.enqueue(f, opts.first);
    });
  }

  // tell sends a frame whose answer nobody waits for (a recv answer, a
  // candidate). It still gets an id: the server answers it, and the answer
  // is matched and dropped rather than mistaken for something else.
  tell(frame, opts = {}) {
    const id = 'c' + this.next++;
    this.enqueue({ ...frame, id }, opts.first);
    return id;
  }

  enqueue(f, first) {
    if (first) this.queue.unshift(f); else this.queue.push(f);
    this.flush();
  }

  // flush sends what it may now, keeping the pace.
  flush() {
    if (this.pump) return;
    while (this.queue.length) {
      const f = this.queue[0];
      // While held, only the frames that make a connection the signalling
      // one may go.
      if (this.held && f.type !== 'call.resume' && f.type !== 'call.join') return;
      const t = this.now();
      this.sentAt = this.sentAt.filter((x) => t - x < 1000);
      if (this.sentAt.length >= this.rate) {
        const wait = 1000 - (t - this.sentAt[0]) + 5;
        this.pump = this.setTimer(() => { this.pump = null; this.flush(); }, wait);
        return;
      }
      if (!this.sendRaw(f)) { this.held = true; return; }
      this.queue.shift();
      this.sentAt.push(t);
    }
  }

  hold() { this.held = true; }
  release() { this.held = false; this.flush(); }

  // answer takes a frame from the server; true when it answered a request.
  answer(frame) {
    const id = frame && frame.reply_to;
    if (!id) return false;
    const entry = this.pending.get(id);
    if (!entry) return true; // the answer to something sent with tell()
    this.pending.delete(id);
    this.clearTimer(entry.timer);
    if (frame.type === 'call.error') entry.reject(new SignalError(frame));
    else entry.resolve(frame);
    return true;
  }

  // drop forgets everything waiting, failing the requests.
  drop(reason = 'left') {
    for (const [id, entry] of this.pending) {
      this.clearTimer(entry.timer);
      entry.reject(new SignalError({ code: reason, message: 'The call is over.' }));
      this.pending.delete(id);
    }
    this.queue = [];
    if (this.pump) { this.clearTimer(this.pump); this.pump = null; }
  }
}

// --- the state of a call ---

// initial is the state before `call.ok`: the call as the join operation
// described it, without tracks.
export function initial(call, me) {
  return {
    callId: call ? call.id : null,
    conversationId: call ? call.conversation_id : null,
    me,
    sealed: !!(call && call.sealed),
    startedAt: call ? call.started_at : null,
    participants: {}, // account id → { account_id, joined_at, tracks: { id → track } }
    speakers: [],
    layers: {},       // track id → { state, layer, reason }: what this client is forwarded
    demand: {},       // own track id → [layers wanted by anyone]
    declined: [],     // account ids that declined the ring
    ended: null,      // { reason, duration_seconds } once over
    left: null,       // the reason this client is out, when it is
    ...(call ? fromCall(call) : {}),
  };
}

function fromCall(call) {
  const participants = {};
  for (const p of call.participants || []) {
    const tracks = {};
    for (const t of p.tracks || []) tracks[t.id] = { ...t };
    participants[p.account_id] = { account_id: p.account_id, joined_at: p.joined_at, tracks };
  }
  return { participants };
}

// reduce applies one frame to the state and returns the new state. Frames
// for another call are ignored. The state is replaced, never changed in
// place, so a view can compare.
export function reduce(s, f) {
  if (!f || typeof f.type !== 'string') return s;
  if (f.call_id && s.callId && f.call_id !== s.callId) return s;
  const parts = () => ({ ...s.participants });
  switch (f.type) {
    case 'call.ok': {
      // The answer to `call.join` or `call.resume` carries the whole call;
      // the answer to `call.subscribe` the state of each video asked for.
      let next = s;
      if (f.call) next = { ...next, ...fromCall(f.call), callId: f.call.id || s.callId, sealed: f.call.sealed != null ? !!f.call.sealed : s.sealed, startedAt: f.call.started_at || s.startedAt, left: null };
      if (Array.isArray(f.video)) {
        const layers = {};
        for (const v of f.video) layers[v.track_id] = { state: v.state, layer: v.layer || null, reason: null };
        next = { ...next, layers };
      }
      return next;
    }
    case 'call.participant_joined': {
      const p = f.participant || {};
      if (!p.account_id) return s;
      const ps = parts();
      ps[p.account_id] = { account_id: p.account_id, joined_at: p.joined_at, tracks: (ps[p.account_id] && ps[p.account_id].tracks) || {} };
      return { ...s, participants: ps };
    }
    case 'call.participant_left': {
      const ps = parts();
      const gone = ps[f.account_id];
      delete ps[f.account_id];
      const layers = { ...s.layers };
      if (gone) for (const id of Object.keys(gone.tracks)) delete layers[id];
      const next = { ...s, participants: ps, layers, speakers: s.speakers.filter((x) => x !== f.account_id) };
      if (f.account_id === s.me) next.left = f.reason || 'left';
      return next;
    }
    case 'call.track_published': {
      if (!f.track || !f.account_id) return s;
      const ps = parts();
      const p = ps[f.account_id] || { account_id: f.account_id, joined_at: f.at, tracks: {} };
      ps[f.account_id] = { ...p, tracks: { ...p.tracks, [f.track.id]: { ...f.track } } };
      return { ...s, participants: ps };
    }
    case 'call.track_unpublished': {
      const ps = parts();
      const p = ps[f.account_id];
      if (!p) return s;
      const tracks = { ...p.tracks };
      delete tracks[f.track_id];
      ps[f.account_id] = { ...p, tracks };
      const layers = { ...s.layers };
      delete layers[f.track_id];
      return { ...s, participants: ps, layers };
    }
    case 'call.mute_changed': {
      const ps = parts();
      const p = ps[f.account_id];
      if (!p) return s;
      const t = p.tracks[f.track_id] || Object.values(p.tracks).find((x) => x.source === f.source);
      if (!t) return s;
      ps[f.account_id] = { ...p, tracks: { ...p.tracks, [t.id]: { ...t, muted: !!f.muted } } };
      const next = { ...s, participants: ps };
      // A muted microphone is not a speaker.
      if (f.muted && f.source === 'microphone') next.speakers = s.speakers.filter((x) => x !== f.account_id);
      return next;
    }
    case 'call.speakers_changed':
      return { ...s, speakers: Array.isArray(f.account_ids) ? f.account_ids.slice(0, 3) : [] };
    case 'call.layer_changed':
      return { ...s, layers: { ...s.layers, [f.track_id]: { state: f.state, layer: f.layer || null, reason: f.reason || null } } };
    case 'call.demand_changed':
      return { ...s, demand: { ...s.demand, [f.track_id]: Array.isArray(f.layers) ? f.layers : [] } };
    case 'call.declined':
      return s.declined.includes(f.account_id) ? s : { ...s, declined: [...s.declined, f.account_id] };
    case 'call.ended':
      return { ...s, ended: { reason: f.reason || 'empty', duration_seconds: f.duration_seconds || 0 } };
    default:
      return s;
  }
}

// people turns the state into what the layout needs: everyone but the
// reader, in the order they joined, with their video track ids.
export function people(s) {
  return Object.values(s.participants)
    .filter((p) => p.account_id !== s.me)
    .sort((a, b) => String(a.joined_at || '').localeCompare(String(b.joined_at || '')) || (a.account_id < b.account_id ? -1 : 1))
    .map((p) => {
      const tracks = Object.values(p.tracks);
      const cam = tracks.find((t) => t.source === 'camera');
      const scr = tracks.find((t) => t.source === 'screen');
      const mic = tracks.find((t) => t.source === 'microphone');
      return {
        account_id: p.account_id,
        camera: cam ? cam.id : null,
        cameraOn: !!(cam && !cam.muted),
        screen: scr ? scr.id : null,
        microphone: mic ? mic.id : null,
        // No microphone published counts as muted: nothing is heard.
        micMuted: !mic || !!mic.muted,
        speaking: s.speakers.includes(p.account_id),
      };
    });
}

// sharer is who is sharing a screen, or null.
export function sharer(s) {
  for (const p of Object.values(s.participants)) {
    if (Object.values(p.tracks).some((t) => t.source === 'screen')) return p.account_id;
  }
  return null;
}

// --- ICE: when to restart, and when to give up ---

// IceWatch follows the ICE state of the two media connections and decides
// what to do (CALLS.md section 11): restart ICE on both when one has been
// `disconnected` for three seconds or has `failed`; try up to three times,
// five seconds apart; then tear down and join again with a new ticket.
//
//   onRestart(attempt)   restart ICE on both connections
//   onRejoin()           give up on these connections and join again
//   onHealthy()          both are connected again
export const DISCONNECTED_GRACE = 3000;
export const RESTART_EVERY = 5000;
export const RESTART_TRIES = 3;

export class IceWatch {
  constructor({ onRestart, onRejoin, onHealthy, setTimer = (fn, ms) => setTimeout(fn, ms), clearTimer = (t) => clearTimeout(t) }) {
    this.onRestart = onRestart;
    this.onRejoin = onRejoin;
    this.onHealthy = onHealthy || (() => {});
    this.setTimer = setTimer;
    this.clearTimer = clearTimer;
    this.states = {}; // 'send' | 'recv' → ICE connection state
    this.attempts = 0;
    this.graceTimer = null;
    this.retryTimer = null;
    this.restarting = false;
    this.stopped = false;
  }

  bad() {
    return Object.values(this.states).some((st) => st === 'disconnected' || st === 'failed');
  }

  // set records a connection's new ICE state.
  set(pc, state) {
    if (this.stopped) return;
    this.states[pc] = state;
    const all = Object.values(this.states);
    if (all.length && all.every((st) => st === 'connected' || st === 'completed')) {
      this.clear();
      if (this.restarting || this.attempts) { this.restarting = false; this.attempts = 0; this.onHealthy(); }
      return;
    }
    if (this.restarting) return; // a restart is running; its timer decides
    if (state === 'failed') { this.clear(); this.restart(); return; }
    if (state === 'disconnected' && !this.graceTimer) {
      this.graceTimer = this.setTimer(() => { this.graceTimer = null; if (this.bad()) this.restart(); }, DISCONNECTED_GRACE);
    }
  }

  restart() {
    if (this.stopped) return;
    if (this.attempts >= RESTART_TRIES) { this.restarting = false; this.onRejoin(); return; }
    this.attempts++;
    this.restarting = true;
    this.onRestart(this.attempts);
    this.retryTimer = this.setTimer(() => {
      this.retryTimer = null;
      if (this.bad() || this.restarting) this.restart();
    }, RESTART_EVERY);
  }

  clear() {
    if (this.graceTimer) { this.clearTimer(this.graceTimer); this.graceTimer = null; }
    if (this.retryTimer) { this.clearTimer(this.retryTimer); this.retryTimer = null; }
  }

  // reset starts counting again, for a fresh pair of connections.
  reset() { this.clear(); this.states = {}; this.attempts = 0; this.restarting = false; this.stopped = false; }
  stop() { this.clear(); this.stopped = true; }
}

// --- words ---

// errorText says in plain words what a refusal means for the person.
export function errorText(code, extra = {}) {
  switch (code) {
    case 'cameras_full': return 'The most cameras allowed in this call are already on. Your microphone still works.';
    case 'screen_in_use': return extra.who ? `${extra.who} is already sharing a screen. One screen at a time.` : 'Someone else is sharing a screen. One screen at a time.';
    case 'call_full': return 'The call is full.';
    case 'frame_encryption_required': return 'This is a sealed chat’s call, and it needs end-to-end encryption this browser cannot do yet.';
    case 'media_timeout': return 'No media connection could be made. This network may block calls.';
    case 'feature_off': return 'Calls are switched off on this server.';
    case 'forbidden': return 'You may no longer be in this call.';
    case 'ticket_invalid': return 'The call could not be joined. Try again.';
    case 'host_unreachable': return 'The server hosting this call cannot be reached.';
    case 'too_many_subscriptions': return 'Too many videos were asked for at once.';
    case 'rate_limited': return 'Too many call messages at once. Wait a moment.';
    case 'timeout': return 'The call server did not answer.';
    default: return 'The call server could not do that.';
  }
}

// endedText says why a call is over.
export function endedText(reason) {
  switch (reason) {
    case 'alone': return 'The call ended: you were alone in it for an hour.';
    case 'too_long': return 'The call ended after 24 hours.';
    case 'closed': return 'The call ended because the conversation was closed.';
    case 'server': return 'The call ended because the server restarted or calls were switched off.';
    default: return 'The call ended.';
  }
}

// leftText says why this client is out of a call it did not leave.
export function leftText(reason) {
  switch (reason) {
    case 'joined_elsewhere': return 'You joined this call on another device, so it moved there.';
    case 'timeout': return 'You were out of touch with the call for too long, so it let you go.';
    case 'removed': return 'You are no longer in this conversation, so you left its call.';
    case 'signed_out': return 'Your session ended, so you left the call.';
    default: return 'You left the call.';
  }
}
