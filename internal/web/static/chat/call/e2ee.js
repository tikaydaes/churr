// End-to-end encryption of a call's frames, for a sealed chat
// (docs/CALLS.md section 14): the worker of frames-worker.js put on every
// sender and receiver of the call's two connections, with the keys the
// sealed-chat code gives through frame-keys.js. The frame format is
// docs/CALL-FRAMES.md.

import { callFrameKeys, onFrameKeysChanged, testing } from './frame-keys.js';

export class FrameCrypto {
  // onProblem(participant, bad) is told when nothing from someone has
  // decrypted for five seconds, and when it does again.
  constructor(call, keys, { onProblem } = {}) {
    this.call = call;
    this.worker = new Worker('/chat/call/frames-worker.js');
    // The tests can ask for the older interface, to check it too.
    this.script = 'RTCRtpScriptTransform' in window && !testing.legacyStreams;
    this.asked = new Map();
    this.asks = 0;
    this.worker.onmessage = (e) => {
      const m = e.data || {};
      if (m.type === 'undecryptable' && onProblem) onProblem(m.participant, true);
      if (m.type === 'decryptable' && onProblem) onProblem(m.participant, false);
      if (m.type === 'stats' && this.asked.has(m.id)) { this.asked.get(m.id)(m); this.asked.delete(m.id); }
    };
    this.setKeys(keys);
    // A new epoch in the sealed chat: ask for the keys again. The worker
    // sends under the new ones at once and keeps the old for ten seconds.
    this.offChanged = onFrameKeysChanged(async (callId) => {
      if (callId && callId !== call.id) return;
      const fresh = await callFrameKeys(call);
      if (!fresh || (fresh.keyEpoch !== undefined && fresh.keyEpoch === this.keyEpoch)) return;
      this.setKeys(fresh);
    });
  }

  setKeys(keys) {
    this.keyEpoch = keys.keyEpoch;
    this.keyIndex = keys.keyIndex | 0;
    this.deviceId = keys.deviceId;
    const all = keys.keys instanceof Map ? Object.fromEntries(keys.keys) : { ...(keys.keys || {}) };
    this.worker.postMessage({ type: 'keys', keyIndex: this.keyIndex, own: keys.deviceId, keys: all });
  }

  // pcConfig is what a peer connection needs made with, in a browser that
  // only has the older insertable streams.
  pcConfig() { return this.script ? {} : { encodedInsertableStreams: true }; }

  // attach puts the worker on one sender (encrypt) or receiver (decrypt).
  attach(senderOrReceiver, options) {
    try {
      if (this.script) {
        senderOrReceiver.transform = new RTCRtpScriptTransform(this.worker, options);
        return true;
      }
      if (senderOrReceiver.createEncodedStreams) {
        const { readable, writable } = senderOrReceiver.createEncodedStreams();
        this.worker.postMessage({ type: 'stream', readable, writable, options }, [readable, writable]);
        return true;
      }
    } catch (err) { console.error(err); }
    return false;
  }

  // route says which person a recv media line carries now, and from which
  // of their devices: that device's key decrypts it.
  route(mid, participant, device) { this.worker.postMessage({ type: 'route', mid, participant, device: device || null }); }

  // stats asks the worker what it has done: frames sent by key index,
  // frames opened by device and key index, frames dropped. For the tests.
  stats() {
    const id = ++this.asks;
    return new Promise((resolve) => { this.asked.set(id, resolve); this.worker.postMessage({ type: 'stats', id }); });
  }

  close() {
    if (this.offChanged) this.offChanged();
    this.worker.terminate();
  }
}
