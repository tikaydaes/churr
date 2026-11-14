// Ringing while the page is open (docs/CALLS.md section 15, docs/PUSH.md
// section 13): the incoming-call prompt with Join and Decline, and the ring
// sound.
//
// The server decides who is rung: it sends `call.ring_started` only to
// people whose mode and do not disturb allow it. The page adds two rules
// of its own: do not disturb turned on during a ring silences it, and a
// second ring while already in a call shows the prompt without the sound.
// A browser plays sound only after the person has used the page; until
// then the prompt shows silently. Nothing rings when the page is closed.

import { h, fill, icon, button } from '/shared/dom.js';
import { store, nameOf, conversation, title } from '../store.js';

// The sound is shipped in two forms: AAC for the browsers that have it,
// WAV for the rest.
function soundURL() {
  const probe = document.createElement('audio');
  return probe.canPlayType('audio/mp4; codecs="mp4a.40.2"') ? '/sounds/churr-ring.m4a' : '/sounds/churr-ring.wav';
}

// Whether the person has used the page, which is what lets it play sound.
let used = false;
for (const type of ['pointerdown', 'keydown', 'touchstart']) {
  window.addEventListener(type, () => { used = true; }, { capture: true, passive: true });
}
export function mayPlaySound() {
  return used || !!(navigator.userActivation && navigator.userActivation.hasBeenActive);
}

// rung remembers the calls this page was rung for, for "Missed call".
export const rung = new Set();

export class Ringer {
  // onJoin(conversationId) joins; inCall() says whether the page is in a
  // call already.
  constructor({ onJoin, inCall, decline }) {
    this.onJoin = onJoin;
    this.inCall = inCall;
    this.declineFrame = decline;
    this.rings = new Map(); // call id → { frame, timer }
    this.el = null;
    this.audio = null;
    store.on('me', () => this.draw());
  }

  start(f) {
    if (!f || !f.call_id || this.rings.has(f.call_id)) return;
    // Nothing rings through do not disturb, whatever the cause.
    if (store.me && store.me.dnd) return;
    rung.add(f.call_id);
    const left = f.until ? new Date(f.until).getTime() - Date.now() : 45000;
    if (left <= 0) return;
    const timer = setTimeout(() => this.stop(f.call_id), Math.min(left, 45000));
    this.rings.set(f.call_id, { frame: f, timer });
    this.draw();
  }

  stop(callId) {
    const r = this.rings.get(callId);
    if (!r) return;
    clearTimeout(r.timer);
    this.rings.delete(callId);
    this.draw();
  }

  stopConversation(conversationId) {
    for (const [id, r] of this.rings) if (r.frame.conversation_id === conversationId) this.stop(id);
  }

  decline(callId) {
    this.declineFrame(callId);
    this.stop(callId);
  }

  draw() {
    // Do not disturb switched on while ringing: the ring goes quiet.
    if (store.me && store.me.dnd && this.rings.size) { for (const id of [...this.rings.keys()]) this.stop(id); return; }
    const list = [...this.rings.values()].map((r) => r.frame);
    if (!list.length) {
      this.silence();
      if (this.el) { this.el.remove(); this.el = null; }
      return;
    }
    if (!this.el) {
      this.el = h('div', { class: 'call-incoming', role: 'alertdialog', 'aria-label': 'Incoming call' });
      document.body.append(this.el);
    }
    const quiet = this.inCall() || !mayPlaySound();
    fill(this.el, list.map((f) => {
      const c = conversation(f.conversation_id);
      const named = c && c.name;
      return h('div', { class: 'call-ring', dataset: { call: f.call_id } },
        icon('call', null, 'ring-mark'),
        h('div', { class: 'grow' },
          h('strong', {}, h('bdi', {}, nameOf(f.from_account_id)), ' is calling'),
          h('small', {}, named ? h('bdi', {}, title(c)) : (c && c.kind === 'sealed' ? 'Sealed chat' : 'Chat'),
            this.inCall() ? ' · you are in another call' : quiet ? ' · the browser will not ring until you have used this page' : null)),
        button('Decline', () => this.decline(f.call_id), 'small'),
        button('Join', () => { this.stop(f.call_id); this.onJoin(f.conversation_id); }, 'small primary'));
    }));
    if (quiet) this.silence(); else this.play();
  }

  play() {
    if (!this.audio) {
      this.audio = h('audio', { src: soundURL(), loop: true, preload: 'auto' });
      this.audio.dataset.ring = '1';
      document.body.append(this.audio);
    }
    if (this.audio.paused) this.audio.play().then(() => { if (this.el) this.el.dataset.sound = 'on'; }).catch(() => { if (this.el) this.el.dataset.sound = 'refused'; });
  }

  silence() {
    if (this.audio) { this.audio.pause(); this.audio.currentTime = 0; }
    if (this.el) this.el.dataset.sound = 'off';
  }
}
