// One call this page is in: joining with a ticket, the two peer
// connections, publishing, subscribing, muting, reconnecting, leaving
// (docs/CALLS.md sections 2, 4, 7, 10 and 11).
//
// The view (view.js) draws from `call.state` and the tracks here, and
// tells the call which tiles are on screen with `want()`. The chat's live
// connection is reached through the `live` object handed in: `send(frame)`
// and its state.

import { post, get } from '/shared/api.js';
import { Requests, SignalError, reduce, initial, IceWatch, errorText } from './signal.js';
import { sameSubscriptions } from './layout.js';
import * as media from './media.js';
import { callFrameKeys, testing as e2eeTesting } from './frame-keys.js';
import { FrameCrypto } from './e2ee.js';

// How long `call.subscribe` waits after the last change of layout.
export const SUBSCRIBE_DELAY = 200;

export class Call {
  // conv is the conversation; live the chat's live connection; me the
  // reader's account id.
  constructor(conv, { live, me }) {
    this.conv = conv;
    this.conversationId = conv.id;
    this.live = live;
    this.me = me;
    this.state = initial(conv.call || null, me);
    this.phase = 'idle'; // idle → joining → live → (reconnecting) → over
    this.listeners = new Set();
    this.requests = new Requests({ send: (f) => this.live.send(f) });
    this.local = { mic: null, camera: null, screen: null };
    this.muted = { microphone: false, camera: true };
    this.tx = {};          // source → RTCRtpTransceiver on send
    this.myTracks = {};    // source → track id the server gave
    this.remote = new Map(); // track id → MediaStreamTrack
    this.mids = new Map();   // recv mid → { track_id, account_id, device_id }
    this.notes = [];       // what the person should know: { key, text, kind }
    this.wanted = [];      // the subscriptions last sent
    this.pendingWant = null;
    this.subTimer = null;
    this.negotiation = Promise.resolve();
    this.recvChain = Promise.resolve();
    this.held = false;
    this.everConnected = false;
    this.transport = null; // 'udp' | 'tcp' once known
    this.frames = [];      // the frames this call received, for the tests' replay
    this.ice = new IceWatch({
      onRestart: (n) => this.restartIce(n),
      onRejoin: () => this.rejoin('ice'),
      onHealthy: () => { this.dropNote('ice'); this.emit(); },
    });
  }

  on(fn) { this.listeners.add(fn); return () => this.listeners.delete(fn); }
  emit() { for (const fn of this.listeners) { try { fn(this); } catch (err) { console.error(err); } } }

  note(key, text, kind = 'warn') {
    this.notes = [...this.notes.filter((n) => n.key !== key), { key, text, kind }];
    this.emit();
  }
  dropNote(key) {
    if (!this.notes.some((n) => n.key === key)) return;
    this.notes = this.notes.filter((n) => n.key !== key);
    this.emit();
  }

  get callId() { return this.state.callId; }
  get over() { return this.phase === 'over'; }

  // --- joining ---

  // join takes the person into the conversation's call, starting one if
  // there is none. audio and video say whether to start with the
  // microphone and camera on. Media problems do not stop the join: the
  // person listens and is told why.
  async join({ audio = true, video = false, tracks = null } = {}) {
    this.phase = 'joining';
    this.emit();
    // The devices first, while the click that asked to join still counts
    // as the person's own action.
    const got = tracks || await media.open({ audio: true, video });
    this.local.mic = got.mic;
    this.local.camera = got.camera;
    for (const p of got.problems || []) this.note('media-' + p, media.problemText(p), p === 'insecure' ? 'danger' : 'warn');
    this.muted.microphone = !audio || !got.mic;
    this.muted.camera = !got.camera;
    let out;
    try {
      out = await post(`/conversations/${this.conversationId}/call/join`);
    } catch (err) {
      this.finish();
      throw err;
    }
    this.state = initial(out.call, this.me);
    // A sealed chat's call: every frame is encrypted with keys from the
    // sealed-chat code. Without them the page does not join at all.
    if (out.call.sealed || e2eeTesting.encryptEveryCall) {
      const keys = await callFrameKeys(out.call);
      if (!keys) {
        this.finish('failed');
        post(`/conversations/${this.conversationId}/call/leave`).catch(() => {});
        throw new Error('This sealed chat’s call keys are not in this browser, so the call was not joined.');
      }
      this.crypto = new FrameCrypto(out.call, keys, {
        onProblem: (who, bad) => { if (bad) this.note('decrypt', 'Cannot decrypt this call: nothing from ' + this.nameOf(who) + ' could be decrypted. Waiting for new keys.', 'danger'); else this.dropNote('decrypt'); },
      });
    }
    await this.signalJoin(out.call.id, out.ticket);
  }

  pcConfig() {
    return { bundlePolicy: 'max-bundle', rtcpMuxPolicy: 'require', ...(this.crypto ? this.crypto.pcConfig() : {}) };
  }

  async signalJoin(callId, ticket) {
    // An encrypted call is VP8 only: the worker knows which bytes of a VP8
    // frame stay readable.
    const client = { simulcast: media.simulcast(), video_codecs: this.crypto ? ['vp8'] : media.decodes(), frame_encryption: !!this.crypto };
    let ok;
    try {
      ok = await this.requests.request({ type: 'call.join', call_id: callId, ticket, client }, { first: true });
    } catch (err) {
      this.finish();
      throw err;
    }
    this.apply(ok);
    this.resumeToken = ok.resume_token;
    this.limits = ok.limits || null;
    this.phase = 'live';
    this.held = false;
    this.rejoining = false;
    this.emit();
    // Publishing what the person has. A camera that is off is not
    // published until it is first turned on.
    await this.publish();
    if (this.local.mic && this.muted.microphone && this.myTracks.microphone) this.sendMute('microphone', true).catch(() => {});
    this.flushWant(true);
  }

  // apply runs a frame through the state.
  apply(f) {
    this.state = reduce(this.state, f);
  }

  // --- frames from the server ---

  // handle takes every `call.` frame of the live connection that concerns
  // this call. Answers to requests go to the requests; the rest change the
  // state or the connections.
  handle(f) {
    if (this.over) return;
    this.frames.push(f);
    if (this.frames.length > 2000) this.frames.splice(0, 500);
    if (f.reply_to && this.requests.answer(f)) return;
    switch (f.type) {
      case 'call.offer':
        if (f.pc === 'recv') this.recvChain = this.recvChain.then(() => this.answerOffer(f)).catch((err) => console.error(err));
        return;
      case 'call.error':
        // Found by the server itself, such as media that never arrived.
        if (f.code === 'media_timeout') this.note('blocked', errorText('media_timeout'), 'danger');
        else this.note('error-' + f.code, f.message || errorText(f.code));
        return;
      case 'call.participant_left':
        if (f.account_id === this.me) {
          // Joining again from this very connection makes the server tell
          // it that it "joined elsewhere": that is this page, so it is not
          // news.
          if (this.rejoining && f.reason === 'joined_elsewhere') return;
          this.apply(f);
          this.finish(f.reason || 'left');
          return;
        }
        break;
      case 'call.ended':
        this.apply(f);
        this.finish('ended');
        return;
      case 'call.demand_changed':
        this.apply(f);
        this.applyDemand(f.track_id);
        this.emit();
        return;
      default:
        break;
    }
    const before = this.state;
    this.apply(f);
    if (this.state !== before) {
      // Who is showing what may have changed: the subscriptions follow.
      this.emit();
    }
  }

  // --- the send connection ---

  sendPC() {
    if (this.send) return this.send;
    const pc = new RTCPeerConnection(this.pcConfig());
    this.send = pc;
    this.watch(pc, 'send');
    return pc;
  }

  watch(pc, name) {
    pc.addEventListener('icecandidate', (e) => {
      const c = e.candidate;
      // One frame per candidate, and one with null when gathering is over.
      // The server takes them for diagnostics only.
      if (c && !c.candidate) return;
      this.requests.tell({ type: 'call.candidate', pc: name, candidate: c ? c.candidate : null, sdp_mid: c ? c.sdpMid : null, sdp_mline_index: c ? c.sdpMLineIndex : null });
    });
    pc.addEventListener('iceconnectionstatechange', () => {
      const st = pc.iceConnectionState;
      if (st === 'connected' || st === 'completed') {
        this.everConnected = true;
        this.dropNote('blocked');
        this.checkTransport();
      }
      if (st === 'failed' && !this.everConnected) {
        this.note('blocked', 'This network blocks calls: no media connection could be made, over UDP or TCP. You can still read and write.', 'danger');
      }
      this.ice.set(name, st);
      this.emit();
    });
  }

  // publish brings the send connection in line with what the person is
  // sharing, and tells the server with `call.offer`. Offers on send go one
  // at a time.
  publish(opts = {}) {
    const run = () => this.doPublish(opts);
    this.negotiation = this.negotiation.then(run, run);
    return this.negotiation;
  }

  async doPublish({ iceRestart = false } = {}) {
    if (this.over || this.held) return;
    const pc = this.sendPC();
    const want = {
      microphone: this.local.mic,
      camera: this.tx.camera || (this.local.camera && !this.muted.camera) ? (this.local.camera || null) : null,
      screen: this.local.screen,
    };
    const fresh = [];
    // A source being added gets a transceiver of its own; one that stops
    // (a screen) has its track taken off, and its media line goes quiet.
    for (const source of ['microphone', 'camera', 'screen']) {
      const track = want[source];
      const t = this.tx[source];
      const publishedCamera = source === 'camera' && t; // a camera stays published while muted
      if ((track || publishedCamera) && !t) {
        const init = { direction: 'sendonly', streams: [] };
        if (source === 'camera') init.sendEncodings = media.cameraEncodings(track, media.simulcast());
        if (source === 'screen') init.sendEncodings = [{ ...media.SCREEN_ENCODING }];
        // A microphone that starts muted is published with nothing on it.
        const tr = pc.addTransceiver(source === 'microphone' && this.muted.microphone ? 'audio' : track, init);
        if (source !== 'microphone') media.preferVP8(tr, !!this.crypto);
        if (this.crypto) this.crypto.attach(tr.sender, { operation: 'encrypt', kind: source === 'microphone' ? 'audio' : 'video' });
        if (source === 'screen') { try { const p = tr.sender.getParameters(); p.degradationPreference = 'maintain-resolution'; await tr.sender.setParameters(p); } catch { /* a hint only */ } }
        this.tx[source] = tr;
        fresh.push(source);
      } else if (!track && t && source !== 'camera') {
        try { await t.sender.replaceTrack(null); } catch { /* gone */ }
        try { pc.removeTrack(t.sender); } catch { /* gone */ }
        delete this.tx[source];
        delete this.myTracks[source];
      }
    }
    if (!pc.getTransceivers().some((t) => t.currentDirection !== 'stopped' && t.direction === 'sendonly') && !Object.keys(this.myTracks).length && !iceRestart) {
      // Nothing to send and nothing was sent: no offer is needed. The
      // person is listening only.
      this.emit();
      return;
    }
    const offer = await pc.createOffer({ iceRestart });
    await pc.setLocalDescription(offer);
    const tracks = [];
    for (const [source, t] of Object.entries(this.tx)) {
      if (!t.mid) continue;
      const entry = { mid: t.mid, source };
      const track = source === 'camera' ? this.local.camera : source === 'screen' ? this.local.screen : null;
      const s = track && track.getSettings ? track.getSettings() : null;
      if (s && s.width && s.height) { entry.width = s.width; entry.height = s.height; }
      tracks.push(entry);
    }
    let ans;
    try {
      ans = await this.requests.request({ type: 'call.offer', pc: 'send', sdp: pc.localDescription.sdp, ice_restart: !!iceRestart, tracks });
    } catch (err) {
      // Refused: the offer is taken back and the new tracks go.
      try { await pc.setLocalDescription({ type: 'rollback' }); } catch { /* nothing to take back */ }
      for (const source of fresh) {
        const t = this.tx[source];
        try { t.stop(); } catch { /* already */ }
        delete this.tx[source];
        if (source === 'screen' && this.local.screen) { this.local.screen.stop(); this.local.screen = null; }
        if (source === 'camera') { if (this.local.camera) this.local.camera.stop(); this.local.camera = null; this.muted.camera = true; }
      }
      if (err instanceof SignalError) {
        const who = err.frame.account_id ? this.nameOf(err.frame.account_id) : null;
        this.note('refused-' + err.code, errorText(err.code, { who }));
      }
      this.emit();
      return;
    }
    await pc.setRemoteDescription({ type: 'answer', sdp: ans.sdp });
    for (const t of ans.tracks || []) {
      for (const [source, tr] of Object.entries(this.tx)) if (tr.mid === t.mid) this.myTracks[source] = t.track_id;
    }
    for (const id of Object.keys(this.state.demand)) this.applyDemand(id);
    this.emit();
  }

  // nameOf is set by the view so refusals can name people.
  nameOf() { return 'Someone'; }

  // applyDemand stops encoding the camera layers nobody is receiving and
  // starts them again when someone is (CALLS.md section 7).
  async applyDemand(trackId) {
    if (!trackId || trackId !== this.myTracks.camera || !this.tx.camera) return;
    const layers = this.state.demand[trackId];
    if (!layers) return;
    const sender = this.tx.camera.sender;
    try {
      const p = sender.getParameters();
      if (!p.encodings || p.encodings.length < 2) return;
      let changed = false;
      for (const e of p.encodings) {
        const on = layers.includes(e.rid);
        if (e.active !== on) { e.active = on; changed = true; }
      }
      if (changed) await sender.setParameters(p);
    } catch { /* the browser keeps sending them all */ }
  }

  // --- the recv connection ---

  async answerOffer(f) {
    if (this.over) return;
    if (!this.recv) {
      const pc = new RTCPeerConnection(this.pcConfig());
      this.recv = pc;
      this.watch(pc, 'recv');
      pc.addEventListener('track', (e) => {
        if (this.crypto && e.transceiver && !e.transceiver._churrCrypto) {
          e.transceiver._churrCrypto = true;
          this.crypto.attach(e.receiver, { operation: 'decrypt', kind: e.track.kind, mid: e.transceiver.mid });
        }
        this.collectRemote();
      });
    }
    for (const t of f.tracks || []) {
      const device = t.device_id || (e2eeTesting.deviceOf ? e2eeTesting.deviceOf(t.account_id) : null);
      this.mids.set(t.mid, { track_id: t.track_id, account_id: t.account_id, device_id: device });
      // The device says whose call sender key decrypts the line.
      if (this.crypto) this.crypto.route(t.mid, t.account_id, device);
    }
    // Lines no longer listed carry nothing for now.
    const listed = new Set((f.tracks || []).map((t) => t.mid));
    for (const mid of [...this.mids.keys()]) if (!listed.has(mid)) this.mids.delete(mid);
    await this.recv.setRemoteDescription({ type: 'offer', sdp: f.sdp });
    const answer = await this.recv.createAnswer();
    await this.recv.setLocalDescription(answer);
    this.requests.tell({ type: 'call.answer', pc: 'recv', offer_id: f.offer_id, sdp: this.recv.localDescription.sdp });
    this.collectRemote();
  }

  // collectRemote maps each forwarded track id to the browser's track for
  // its media line. A line can be given another track by a later offer.
  collectRemote() {
    if (!this.recv) return;
    const next = new Map();
    for (const t of this.recv.getTransceivers()) {
      const m = t.mid != null && this.mids.get(t.mid);
      if (m && t.receiver && t.receiver.track) next.set(m.track_id, t.receiver.track);
    }
    let changed = next.size !== this.remote.size;
    for (const [k, v] of next) if (this.remote.get(k) !== v) changed = true;
    this.remote = next;
    if (changed) this.emit();
  }

  remoteTrack(id) { return id ? this.remote.get(id) || null : null; }

  // --- subscriptions ---

  // want is told by the view which video it is showing and how big; the
  // server hears 200 milliseconds after the last change, and only when the
  // list means something different.
  want(list) {
    this.pendingWant = list;
    clearTimeout(this.subTimer);
    this.subTimer = setTimeout(() => this.flushWant(false), SUBSCRIBE_DELAY);
  }

  async flushWant(force) {
    if (this.phase !== 'live' || this.held) return;
    const list = this.pendingWant || this.wanted || [];
    if (!force && sameSubscriptions(list, this.wanted)) return;
    this.wanted = list;
    try {
      const ok = await this.requests.request({ type: 'call.subscribe', video: list });
      this.apply(ok);
      this.emit();
    } catch (err) {
      if (err instanceof SignalError && err.code !== 'left') this.note('subscribe', errorText(err.code));
    }
  }

  // --- the person's own controls ---

  sendMute(source, muted) {
    return this.requests.request({ type: 'call.mute', source, muted });
  }

  async toggleMic() {
    if (!this.local.mic) {
      // Joined without one: ask again, as after a refusal was undone.
      const got = await media.openMic();
      if (!got.mic) { for (const p of got.problems) this.note('media-' + p, media.problemText(p)); return; }
      for (const k of ['mic-refused', 'no-mic', 'mic-busy', 'mic-failed']) this.dropNote('media-' + k);
      this.local.mic = got.mic;
      this.muted.microphone = false;
      await this.publish();
      this.emit();
      return;
    }
    const muted = !this.muted.microphone;
    this.muted.microphone = muted;
    // A muted microphone sends nothing at all.
    if (this.tx.microphone) { try { await this.tx.microphone.sender.replaceTrack(muted ? null : this.local.mic); } catch { /* the line is gone */ } }
    this.emit();
    if (this.myTracks.microphone) { try { await this.sendMute('microphone', muted); } catch { /* told again on resume */ } }
  }

  async toggleCamera() {
    if (this.muted.camera) {
      // On: the server may refuse when the most cameras are on.
      if (this.myTracks.camera) {
        try { await this.sendMute('camera', false); } catch (err) {
          if (err instanceof SignalError) this.note('refused-' + err.code, errorText(err.code));
          return;
        }
      }
      if (!this.local.camera) {
        const got = await media.openCamera();
        if (!got.camera) {
          for (const p of got.problems) this.note('media-' + p, media.problemText(p));
          if (this.myTracks.camera) this.sendMute('camera', true).catch(() => {});
          return;
        }
        for (const k of ['camera-refused', 'no-camera', 'camera-busy', 'camera-failed']) this.dropNote('media-' + k);
        this.local.camera = got.camera;
      }
      this.muted.camera = false;
      if (this.tx.camera) { try { await this.tx.camera.sender.replaceTrack(this.local.camera); } catch { /* gone */ } this.emit(); } else await this.publish();
      this.emit();
      return;
    }
    // Off is a mute; the camera itself is let go, so its light goes out.
    this.muted.camera = true;
    if (this.tx.camera) { try { await this.tx.camera.sender.replaceTrack(null); } catch { /* gone */ } }
    if (this.local.camera) { this.local.camera.stop(); this.local.camera = null; }
    this.emit();
    if (this.myTracks.camera) { try { await this.sendMute('camera', true); } catch { /* told again on resume */ } }
  }

  async toggleScreen() {
    if (this.local.screen) return this.stopScreen();
    if (!media.canShareScreen()) { this.note('screen', 'Screens cannot be shared from a phone browser.'); return undefined; }
    let track;
    try { track = await media.screen(); } catch { this.note('screen', media.problemText('screen-refused')); return undefined; }
    this.dropNote('screen');
    this.local.screen = track;
    // The browser's own "Stop sharing" button ends it too.
    track.addEventListener('ended', () => { if (this.local.screen === track) this.stopScreen(); });
    this.emit();
    return this.publish();
  }

  // Stopping a screen share is an unpublish: a new offer without its line.
  stopScreen() {
    if (this.local.screen) { this.local.screen.stop(); this.local.screen = null; }
    this.emit();
    return this.publish();
  }

  // setDevice changes the microphone, camera or speaker in use, without a
  // new offer: the new track takes the old one's place on its line.
  async setDevice(kind, id) {
    media.prefs[kind] = id;
    media.savePrefs();
    if (kind === 'speaker') { this.emit(); return; }
    if (kind === 'mic' && this.local.mic) {
      const got = await media.openMic();
      if (!got.mic) return;
      const old = this.local.mic;
      this.local.mic = got.mic;
      if (this.tx.microphone && !this.muted.microphone) await this.tx.microphone.sender.replaceTrack(got.mic);
      old.stop();
    }
    if (kind === 'camera' && this.local.camera) {
      const got = await media.openCamera();
      if (!got.camera) return;
      const old = this.local.camera;
      this.local.camera = got.camera;
      if (this.tx.camera && !this.muted.camera) await this.tx.camera.sender.replaceTrack(got.camera);
      old.stop();
    }
    this.emit();
  }

  // --- reconnection ---

  // signalLost: the live connection dropped. Media may still flow; the
  // server holds the participant for thirty seconds.
  signalLost() {
    if (this.phase !== 'live') return;
    this.held = true;
    this.requests.hold();
    this.note('signal', 'Reconnecting to the server. You can still hear each other.', 'warn');
  }

  // signalBack: a new live connection is up. `call.resume` makes it the
  // signalling connection, and what waited is sent.
  async signalBack() {
    if (!this.held || this.over) return;
    try {
      const ok = await this.requests.request({ type: 'call.resume', call_id: this.callId, resume_token: this.resumeToken }, { first: true });
      this.apply(ok);
      this.resumeToken = ok.resume_token;
      this.held = false;
      this.requests.release();
      this.dropNote('signal');
      // Mute state may have moved on meanwhile.
      if (this.myTracks.microphone) this.sendMute('microphone', this.muted.microphone).catch(() => {});
      if (this.myTracks.camera) this.sendMute('camera', this.muted.camera).catch(() => {});
      this.flushWant(true);
      this.emit();
    } catch (err) {
      this.requests.release();
      if (err instanceof SignalError && err.code === 'ticket_invalid') {
        // The server no longer knows this participant: the call ended
        // while the page was away, or the server restarted.
        let live = null;
        try { live = (await get(`/conversations/${this.conversationId}/call`)).call || null; } catch { /* treated as gone */ }
        if (live && live.id === this.callId) { this.held = false; this.rejoin('resume'); return; }
        this.finish(live ? 'ended' : 'server_restart');
        return;
      }
      this.note('signal', 'The call could not be taken up again on the new connection.', 'danger');
    }
  }

  // restartIce is CALLS.md section 11: new ICE credentials on both
  // connections. send makes a new offer; recv asks the server for one.
  restartIce(attempt) {
    this.note('ice', `The media connection dropped. Reconnecting (try ${attempt} of 3).`, 'warn');
    if (this.send) this.publish({ iceRestart: true });
    if (this.recv) this.requests.request({ type: 'call.restart', pc: 'recv' }).catch(() => {});
  }

  // rejoin tears both connections down and joins again with a new ticket.
  async rejoin(why) {
    if (this.over || this.rejoining) return;
    if (why === 'ice' && !this.everConnected) {
      this.note('blocked', 'This network blocks calls: no media connection could be made, over UDP or TCP. You can still read and write.', 'danger');
      this.leave();
      return;
    }
    this.rejoining = true;
    this.note('ice', 'Joining the call again.', 'warn');
    this.closeConnections();
    this.ice.reset();
    try {
      const out = await post(`/conversations/${this.conversationId}/call/join`);
      if (out.call.id !== this.callId) { this.finish('ended'); return; }
      await this.signalJoin(out.call.id, out.ticket);
      this.dropNote('ice');
    } catch (err) {
      this.rejoining = false;
      this.note('ice', 'The call could not be joined again: ' + (err.message || 'unknown problem'), 'danger');
      this.finish('failed');
    }
  }

  closeConnections() {
    for (const pc of [this.send, this.recv]) { if (pc) { try { pc.close(); } catch { /* closed */ } } }
    this.send = null; this.recv = null;
    this.tx = {}; this.myTracks = {}; this.remote = new Map(); this.mids = new Map();
    this.wanted = [];
    this.negotiation = Promise.resolve();
    this.recvChain = Promise.resolve();
  }

  // checkTransport looks at which path the media took: over TCP the
  // person is shown a "poor connection" mark (CALLS.md section 5).
  async checkTransport() {
    const pc = this.send || this.recv;
    if (!pc) return;
    try {
      const stats = await pc.getStats();
      let pair = null;
      stats.forEach((s) => { if (s.type === 'transport' && s.selectedCandidatePairId) pair = stats.get(s.selectedCandidatePairId); });
      if (!pair) stats.forEach((s) => { if (s.type === 'candidate-pair' && s.nominated && s.state === 'succeeded') pair = s; });
      const remote = pair && stats.get(pair.remoteCandidateId);
      this.transport = remote ? remote.protocol : null;
      this.emit();
    } catch { /* unknown */ }
  }

  // --- leaving ---

  async leave() {
    if (this.over) return;
    const id = this.callId;
    const live = this.phase === 'live' && !this.held;
    this.finish('left');
    if (!id) return;
    if (live && this.live.send({ type: 'call.leave', id: 'leave-' + Date.now(), call_id: id })) return;
    // No live connection: the leave operation does the same.
    try { await post(`/conversations/${this.conversationId}/call/leave`); } catch { /* the server lets go after thirty seconds */ }
  }

  // finish lets go of everything. why: 'left', 'ended', a reason from
  // `call.participant_left`, 'server_restart' or 'failed'.
  finish(why) {
    if (this.over) return;
    this.phase = 'over';
    this.why = why || 'left';
    this.ice.stop();
    clearTimeout(this.subTimer);
    this.requests.drop('left');
    this.closeConnections();
    if (this.crypto) { this.crypto.close(); this.crypto = null; }
    for (const k of ['mic', 'camera', 'screen']) { if (this.local[k]) { try { this.local[k].stop(); } catch { /* stopped */ } this.local[k] = null; } }
    this.emit();
  }
}
