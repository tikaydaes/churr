// The live connection: one WebSocket to /api/v1/events, with hello and
// resume, heartbeats, activity, and reconnecting (docs/EVENTS.md sections 1
// to 7, docs/CLIENTS.md section 7).
//
// This file knows the connection and nothing about chat. It is given
// callbacks:
//
//   positions()        → { conversationId: lastSeqApplied } for `resume`
//   onEvent(frame)     every event (a frame whose type has a dot)
//   onReload(frame)    a `reload` control frame
//   onReady(frame)     the replay is over; frame.new_conversation_ids
//   onState(state)     'connecting' | 'catching-up' | 'live' | 'offline' | 'signed-out'

const SUBPROTOCOL = 'churr.v1';
// Reconnecting waits 1, 2, 5, 10 and then 30 seconds, with a random part.
const DELAYS = [1000, 2000, 5000, 10000, 30000];

export class Live {
  constructor(handlers) {
    this.h = handlers;
    this.ws = null;
    this.state = 'offline';
    this.attempt = 0;
    this.heartbeat = 30000;
    this.timers = {};
    this.lastActivity = 0;
    this.stopped = false;
    this.nextId = 1;
    // Come back at once when the network returns or the page is looked at
    // again, instead of waiting out the delay.
    window.addEventListener('online', () => this.wake());
    document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'visible') this.wake(); });
    // The person doing something is what "online" means; the server is
    // told at most once a minute.
    for (const type of ['keydown', 'pointerdown', 'pointermove', 'focus']) {
      window.addEventListener(type, () => this.activity(), { passive: true, capture: true });
    }
  }

  setState(state) {
    if (this.state === state) return;
    this.state = state;
    this.h.onState && this.h.onState(state);
  }

  start() {
    this.stopped = false;
    this.connect();
  }

  stop() {
    this.stopped = true;
    clearTimeout(this.timers.retry);
    this.clearBeat();
    if (this.ws) { try { this.ws.close(1000); } catch { /* already closed */ } }
    this.ws = null;
  }

  wake() {
    if (this.stopped || this.ws) return;
    clearTimeout(this.timers.retry);
    this.connect();
  }

  connect() {
    if (this.stopped || this.ws) return;
    this.setState('connecting');
    const url = (location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/api/v1/events';
    let ws;
    try { ws = new WebSocket(url, SUBPROTOCOL); } catch { this.retry(); return; }
    this.ws = ws;
    ws.addEventListener('message', (e) => { if (this.ws === ws) this.receive(e.data); });
    ws.addEventListener('close', (e) => { if (this.ws === ws) this.closed(e.code); });
    ws.addEventListener('error', () => { /* a close event follows */ });
  }

  send(frame) {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return false;
    this.ws.send(JSON.stringify(frame));
    return true;
  }

  receive(data) {
    let f;
    try { f = JSON.parse(data); } catch { return; }
    if (!f || typeof f.type !== 'string') return;
    if (f.type.includes('.')) { this.h.onEvent && this.h.onEvent(f); return; }
    switch (f.type) {
      case 'hello':
        this.attempt = 0;
        this.heartbeat = (f.heartbeat_seconds || 30) * 1000;
        this.setState('catching-up');
        this.send({ type: 'resume', positions: (this.h.positions && this.h.positions()) || {} });
        this.beat();
        this.lastActivity = 0;
        this.activity();
        break;
      case 'reload':
        this.h.onReload && this.h.onReload(f);
        break;
      case 'ready':
        this.setState('live');
        this.h.onReady && this.h.onReady(f);
        break;
      case 'pong':
        clearTimeout(this.timers.pong);
        break;
      default:
        // `ack` and `error` answer frames this client sent; nothing here
        // waits on them. Control frames it does not know are ignored.
        break;
    }
  }

  // A ping every heartbeat; no pong within one more means the connection
  // is dead.
  beat() {
    this.clearBeat();
    this.timers.ping = setInterval(() => {
      if (!this.send({ type: 'ping' })) return;
      clearTimeout(this.timers.pong);
      this.timers.pong = setTimeout(() => { if (this.ws) { try { this.ws.close(4000); } catch { /* gone */ } this.closed(4008); } }, this.heartbeat);
    }, this.heartbeat);
  }

  clearBeat() {
    clearInterval(this.timers.ping);
    clearTimeout(this.timers.pong);
  }

  closed(code) {
    this.ws = null;
    this.clearBeat();
    if (this.stopped) return;
    // 4001: the session ended. 4003: the account was removed or banned.
    // Neither is cured by trying again.
    if (code === 4001 || code === 4003) { this.setState('signed-out'); return; }
    this.setState('offline');
    this.retry(code === 4029 ? 30000 : undefined);
  }

  retry(fixed) {
    clearTimeout(this.timers.retry);
    const base = fixed || DELAYS[Math.min(this.attempt, DELAYS.length - 1)];
    this.attempt++;
    this.timers.retry = setTimeout(() => this.connect(), base + Math.random() * base * 0.3);
  }

  activity() {
    const now = Date.now();
    if (now - this.lastActivity < 60000) return;
    if (this.send({ type: 'activity' })) this.lastActivity = now;
  }

  // typing says the person is writing in a conversation, or in a thread
  // of it. The caller decides how often.
  typing(conversationId, parentId) {
    const f = { type: 'typing', conversation_id: conversationId };
    if (parentId) f.parent_id = parentId;
    this.send(f);
  }
}
