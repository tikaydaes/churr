// Frame encryption for a sealed chat's call, off the main thread. The
// format is docs/CALL-FRAMES.md, the one the apps' WebRTC plugin produces
// natively, and this file is checked against the same vectors as the app
// (testdata/call-frames/vectors.json, by e2e/call-frames.mjs):
//
//   readable header | AES-128-GCM ciphertext + 16-byte tag | IV (12 bytes)
//   | 0x0c (the IV's length) | key index
//
// The readable header is the associated data: 1 byte of an Opus frame, 10
// of a VP8 key frame, 3 of any other VP8 frame (so the forwarder can still
// tell key frames and switch layers). An encrypted call is VP8 only; an
// H.264 frame is not sent at all.
//
// The AES key is not the call sender key itself. It is
//   HKDF-SHA-256(ikm = call sender key, salt = "churr call frames v1",
//                info = 128 zero bytes, 16 bytes)
// because that is what the apps' plugin derives (CALL-FRAMES.md 1.2).
//
// Each sending device has its own call sender key, so keys are held per
// device, in a ring of 16 by key index (key_epoch mod 16). The server says
// which device each recv media line carries (`device_id` in `call.offer`).
//
// Two ways in: RTCRtpScriptTransform (the `rtctransform` event), and in
// older Chromium the insertable-streams interface, whose streams the page
// posts here.
//
// Messages from the page:
//   { type: 'keys', keyIndex, own, keys: { deviceId: 32 bytes } }
//       the call sender key of every device of the current epoch; `own` is
//       this browser's device
//   { type: 'route', mid, participant, device }
//       a recv media line now carries this account, from this device
//   { type: 'stream', readable, writable, options }   insertable streams
//   { type: 'stats', id }   answered with the counts below, for the tests
// Messages to the page:
//   { type: 'undecryptable', participant }   nothing from them decrypted for five seconds
//   { type: 'decryptable', participant }
//   { type: 'stats', id, sent: { index: n }, opened: { device: { index: n } }, failed: n }

const SALT = new TextEncoder().encode('churr call frames v1');
const INFO = new Uint8Array(128);
const IV_LENGTH = 12;
const TAG_LENGTH = 16;
const TRAILER = IV_LENGTH + 2;
const KEY_RING = 16;
const KEEP_OLD_KEYS = 10000; // receivers keep the previous keys for ten seconds
const UNDECRYPTABLE_AFTER = 5000;

// ---- The format: nothing below this line and above the next knows about
// workers, so the vectors can be run against it anywhere.

// frameKey derives the AES-128-GCM key frames are encrypted under from a
// device's 32-byte call sender key.
async function frameKey(callSenderKey) {
  const base = await crypto.subtle.importKey('raw', callSenderKey, 'HKDF', false, ['deriveKey']);
  return crypto.subtle.deriveKey({ name: 'HKDF', hash: 'SHA-256', salt: SALT, info: INFO }, base, { name: 'AES-GCM', length: 128 }, false, ['encrypt', 'decrypt']);
}

// headerLength says how many leading bytes of a frame stay readable. A VP8
// frame is a key frame when bit 0 of its first byte is 0.
function headerLength(kind, data) {
  if (kind === 'audio') return Math.min(1, data.length);
  const key = data.length > 0 && (data[0] & 1) === 0;
  return Math.min(key ? 10 : 3, data.length);
}

// sealFrame encrypts one encoded frame. iv is 12 bytes that never repeat
// under the key.
async function sealFrame(key, keyIndex, iv, kind, data) {
  const n = headerLength(kind, data);
  const head = data.subarray(0, n);
  const sealed = new Uint8Array(await crypto.subtle.encrypt({ name: 'AES-GCM', iv, additionalData: head, tagLength: TAG_LENGTH * 8 }, key, data.subarray(n)));
  const out = new Uint8Array(n + sealed.length + TRAILER);
  out.set(head, 0);
  out.set(sealed, n);
  out.set(iv, n + sealed.length);
  out[out.length - 2] = IV_LENGTH;
  out[out.length - 1] = keyIndex & 0xff;
  return out;
}

// frameKeyIndex reads the key index a frame was sent under, or -1 for
// bytes that cannot be an encrypted frame.
function frameKeyIndex(kind, data) {
  if (data.length < TRAILER + TAG_LENGTH) return -1;
  if (data[data.length - 2] !== IV_LENGTH) return -1;
  if (data.length < headerLength(kind, data) + TAG_LENGTH + TRAILER) return -1;
  const index = data[data.length - 1];
  return index < KEY_RING ? index : -1;
}

// openFrame decrypts one frame with the key its index names (keyAt(index)
// gives it, or nothing). It returns the frame as it was before sealFrame,
// or null: no key, the wrong key, or bytes that were changed.
async function openFrame(keyAt, kind, data) {
  const index = frameKeyIndex(kind, data);
  if (index < 0) return null;
  const key = keyAt(index);
  if (!key) return null;
  const n = headerLength(kind, data);
  const ivAt = data.length - TRAILER;
  try {
    const plain = new Uint8Array(await crypto.subtle.decrypt({ name: 'AES-GCM', iv: data.subarray(ivAt, ivAt + IV_LENGTH), additionalData: data.subarray(0, n), tagLength: TAG_LENGTH * 8 }, key, data.subarray(n, ivAt)));
    const out = new Uint8Array(n + plain.length);
    out.set(data.subarray(0, n), 0);
    out.set(plain, n);
    return out;
  } catch {
    return null;
  }
}

// KeyRings holds every sending device's keys by key index. A new set of
// keys (a new epoch) takes effect at once; what it replaces, and the keys
// of devices it leaves out, stay for ten seconds for frames on their way.
class KeyRings {
  constructor(now = () => Date.now()) {
    this.now = now;
    this.rings = new Map(); // device → Map(index → CryptoKey)
    this.leaving = [];      // { at, device, index, key }
    this.send = null;       // { key, index }: this device's own
  }

  // set takes the keys of an epoch: keys is { device: CryptoKey }.
  set(keyIndex, own, keys) {
    const index = keyIndex & (KEY_RING - 1);
    const at = this.now() + KEEP_OLD_KEYS;
    for (const [device, ring] of this.rings) {
      for (const [i, key] of ring) {
        if (i !== index || !keys[device]) this.leaving.push({ at, device, index: i, key });
      }
    }
    for (const [device, key] of Object.entries(keys)) {
      if (!this.rings.has(device)) this.rings.set(device, new Map());
      this.rings.get(device).set(index, key);
    }
    // A device the epoch leaves out sends nothing.
    this.send = keys[own] ? { key: keys[own], index } : null;
  }

  // sweep drops the keys whose ten seconds are over, unless a later epoch
  // put another key in their place.
  sweep() {
    const now = this.now();
    this.leaving = this.leaving.filter((l) => {
      if (l.at > now) return true;
      const ring = this.rings.get(l.device);
      if (ring && ring.get(l.index) === l.key) ring.delete(l.index);
      return false;
    });
  }

  get(device, index) {
    this.sweep();
    const ring = device && this.rings.get(device);
    return (ring && ring.get(index)) || null;
  }
}

// ---- The worker.

const rings = new KeyRings();
const routes = new Map();    // mid → { participant, device }
const health = new Map();    // account → { lastOk, told }
const counts = { sent: {}, opened: {}, failed: 0 };

// The IV: four random bytes chosen when the worker starts, then a counter
// of every frame this worker encrypts. It cannot repeat within the worker,
// and two workers of one device in one call (a rejoin) differ in the first
// four bytes.
const ivPrefix = new Uint8Array(4);
let ivCounter = 0;
function nextIV() {
  const iv = new Uint8Array(IV_LENGTH);
  iv.set(ivPrefix, 0);
  const v = new DataView(iv.buffer);
  v.setUint32(4, Math.floor(ivCounter / 0x100000000));
  v.setUint32(8, ivCounter >>> 0);
  ivCounter++;
  return iv;
}

function codecOf(frame) {
  try { const m = frame.getMetadata(); return (m.mimeType || '').toLowerCase(); } catch { return ''; }
}

async function encrypt(frame, controller, opts) {
  // No key yet, or a codec this worker does not handle: nothing goes out,
  // rather than something readable.
  const send = rings.send;
  if (!send || /h264/.test(codecOf(frame))) return;
  try {
    const out = await sealFrame(send.key, send.index, nextIV(), opts.kind, new Uint8Array(frame.data));
    frame.data = out.buffer;
    controller.enqueue(frame);
    counts.sent[send.index] = (counts.sent[send.index] || 0) + 1;
  } catch { /* a frame that cannot be encrypted is dropped */ }
}

function note(account, ok) {
  if (!account) return;
  const now = Date.now();
  let h = health.get(account);
  if (!h) { h = { lastOk: now, told: false }; health.set(account, h); }
  if (ok) {
    h.lastOk = now;
    if (h.told) { h.told = false; self.postMessage({ type: 'decryptable', participant: account }); }
  } else if (!h.told && now - h.lastOk > UNDECRYPTABLE_AFTER) {
    h.told = true;
    self.postMessage({ type: 'undecryptable', participant: account });
  }
}

async function decrypt(frame, controller, opts) {
  const route = routes.get(opts.mid) || {};
  const data = new Uint8Array(frame.data);
  const out = await openFrame((index) => rings.get(route.device, index), opts.kind, data);
  if (!out) {
    // A frame that does not decrypt is dropped, never played as noise.
    counts.failed++;
    note(route.participant, false);
    return;
  }
  const index = data[data.length - 1];
  const by = counts.opened[route.device] || (counts.opened[route.device] = {});
  by[index] = (by[index] || 0) + 1;
  frame.data = out.buffer;
  controller.enqueue(frame);
  note(route.participant, true);
}

function pipe(readableStream, writableStream, opts) {
  const fn = opts.operation === 'encrypt' ? encrypt : decrypt;
  readableStream
    .pipeThrough(new TransformStream({ transform: (frame, controller) => fn(frame, controller, opts) }))
    .pipeTo(writableStream)
    .catch(() => { /* the line was closed */ });
}

// Key messages are taken one at a time, in the order they came: deriving
// is asynchronous, and an older epoch must not land after a newer one.
let keyWork = Promise.resolve();
function takeKeys(m) {
  keyWork = keyWork.then(async () => {
    const keys = {};
    for (const [device, raw] of Object.entries(m.keys || {})) keys[device] = await frameKey(raw);
    rings.set(m.keyIndex | 0, m.own, keys);
    setTimeout(() => rings.sweep(), KEEP_OLD_KEYS + 50);
  }).catch(() => { /* keys that cannot be derived are no keys */ });
}

if (typeof self !== 'undefined' && typeof self.postMessage === 'function' && typeof window === 'undefined') {
  crypto.getRandomValues(ivPrefix);

  self.onmessage = (e) => {
    const m = e.data || {};
    if (m.type === 'keys') {
      takeKeys(m);
    } else if (m.type === 'route') {
      routes.set(m.mid, { participant: m.participant, device: m.device || null });
    } else if (m.type === 'stream') {
      pipe(m.readable, m.writable, m.options || {});
    } else if (m.type === 'stats') {
      self.postMessage({ type: 'stats', id: m.id, sent: { ...counts.sent }, opened: JSON.parse(JSON.stringify(counts.opened)), failed: counts.failed });
    }
  };

  // RTCRtpScriptTransform: the transform arrives as an event here.
  self.onrtctransform = (e) => {
    const t = e.transformer;
    pipe(t.readable, t.writable, t.options || {});
  };
}
