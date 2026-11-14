// The seam between calls and sealed chats in the web chat
// (docs/CALLS.md section 14, docs/SEALED.md section 10).
//
// A sealed chat's call encrypts every audio and video frame on the
// sending device. The keys come from the chat's epoch secret, which only
// the sealed-chat code holds. That code gives calls its keys through this
// one function and nothing else:
//
//   registerFrameKeys(provider)
//
//   provider(call) → Promise<null | {
//     keyEpoch,            // the epoch the keys belong to
//     keyIndex,            // the key index frames are sent under (key_epoch mod 16)
//     deviceId,            // this browser's own device
//     keys: Map(deviceId → 32 bytes),  // every device's call sender key
//   }>
//
// Keys are per device: a device encrypts under its own call sender key
// and decrypts each other device's frames under theirs. Which device a
// track comes from is the server's to say (`device_id` in `call.offer`,
// docs/CALLS.md section 10). The 32 bytes are the call sender key of
// SEALED.md 5.2; the worker derives the AES key from it
// (docs/CALL-FRAMES.md). When the epoch changes during a call, the
// sealed-chat code calls `frameKeysChanged(call.id)` and the call asks
// the provider again. The provider is chat/sealed/index.js's.
//
// `call` is the call object (CALLS.md section 10). The provider answers
// null when it has no keys for that call: the chat is locked in this
// browser, or the epoch is unknown.
//
// Until a provider is registered, and in a browser without the
// encoded-transform interface, the web chat refuses to join a sealed
// chat's call with a plain message. It never joins one unencrypted: the
// server would refuse that too (`frame_encryption_required`), and saying
// `frame_encryption: true` without encrypting would be a lie the other
// person's device would find out only as noise.

let provider = null;
const changed = new Set();

// frameKeysChanged is how the sealed-chat code says a new epoch began: the
// call in progress asks for its keys again.
export function frameKeysChanged(callId) { for (const fn of changed) fn(callId); }
export function onFrameKeysChanged(fn) { changed.add(fn); return () => changed.delete(fn); }

export function registerFrameKeys(fn) {
  provider = typeof fn === 'function' ? fn : null;
}

// callFrameKeys is what the call code asks: the frame keys for this call,
// or null.
export async function callFrameKeys(call) {
  if (!provider || !call) return null;
  try { return (await provider(call)) || null; } catch { return null; }
}

// canEncryptFrames says whether this browser has the interface frame
// encryption needs: script transforms of encoded frames in a worker.
// RTCRtpScriptTransform where the browser has it; in older Chromium the
// insertable-streams interface.
export function canEncryptFrames() {
  if (typeof window === 'undefined') return false;
  return 'RTCRtpScriptTransform' in window || !!(window.RTCRtpSender && RTCRtpSender.prototype.createEncodedStreams);
}

// FRAME_ENCRYPTION_READY: frames-worker.js implements the format of
// docs/CALL-FRAMES.md, the one the apps send, and is checked against the
// vectors the app is checked against.
export const FRAME_ENCRYPTION_READY = true;

// testing lets the browser tests encrypt an ordinary call's frames with
// fixed keys, to check the worker without a sealed chat; deviceOf(account)
// then names a device for a participant whose session has none. Never set
// by the chat itself.
export const testing = { encryptEveryCall: false, legacyStreams: false, deviceOf: null };

// sealedRefusal says why this browser will not join a sealed chat's call,
// or returns null when it can.
export async function sealedRefusal(call) {
  if (!canEncryptFrames()) return 'This browser cannot encrypt call media end to end, so it cannot join a sealed chat’s call. Use a recent browser or one of the apps.';
  if (!FRAME_ENCRYPTION_READY) return 'Calls in sealed chats are not in this build of the web chat: they need end-to-end encryption of the call media, which is not finished here. Use one of the apps for this call.';
  const keys = await callFrameKeys(call);
  if (!keys) return 'This sealed chat is not unlocked in this browser, so the call’s keys are not here. Unlock it, or use one of the apps.';
  return null;
}
