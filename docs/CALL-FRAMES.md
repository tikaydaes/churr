# Call frames in a sealed chat

How each audio and video frame of a sealed chat's call is encrypted on
the wire. This is the format the apps' WebRTC plugin produces natively
(flutter_webrtc 1.6.3 on the WebRTC-SDK 150.7871.04 library, libwebrtc's
FrameCryptor as LiveKit extended it), found by experiment on macOS on
2026-11-14 and checked byte for byte from Go. The web chat implements
the same format in JavaScript on encoded frames
(`internal/web/static/chat/call/frames-worker.js`). `docs/CALLS.md`
section 14 and `docs/SEALED.md` section 10 point here for the format.

Status of each statement: **[seen]** observed on frames the app sent and
decrypted independently in Go; **[app]** observed from the app's own
behaviour; **[source]** what the library is documented or known to do,
not separately observed.

## 1. Keys

1. Each device derives its **call sender key** (32 bytes) exactly as
   `docs/SEALED.md` section 5.2 says. Nothing changes there.
2. The plugin does not use that key directly. Its key provider derives
   the AES key from it **[seen]**:

       aes_key = HKDF-SHA-256(ikm  = call sender key (32 bytes),
                              salt = "churr call frames v1" (ASCII, 20 bytes),
                              info = 128 bytes of 0x00,
                              L    = 16)

   so frames are **AES-128-GCM** with a 16-byte tag **[seen]**. The salt
   is Churr's choice (the key provider's `ratchetSalt`); the 128 zero
   bytes of info and the 16-byte length are fixed by the library.
3. Why this and not raw AES-256 keys: the plugin offers no way to hand
   it an AES key as is. It derives with PBKDF2 (100,000 rounds) or with
   HKDF; the app chooses HKDF, which is one of the design's five
   primitives (SEALED.md 2.2) and is in Web Crypto. The cost is that
   call frames use AES-128-GCM where the rest of the design uses
   AES-256-GCM: **the one named exception, for call frames only.** The
   input key is still 256 bits from the epoch secret.
4. Web Crypto equivalent:

       const base = await crypto.subtle.importKey("raw", callSenderKey, "HKDF", false, ["deriveKey"]);
       const key = await crypto.subtle.deriveKey(
         {name: "HKDF", hash: "SHA-256",
          salt: new TextEncoder().encode("churr call frames v1"),
          info: new Uint8Array(128)},
         base, {name: "AES-GCM", length: 128}, false, ["encrypt", "decrypt"]);

5. **No ratcheting.** The key provider is set with `ratchetWindowSize`
   0 and `failureTolerance` -1: a frame that fails to decrypt never
   makes the library try a ratcheted key or give up on the key
   **[app: settings]**. Keys change only with the sealed chat's epoch.
6. **Key index** is `key_epoch mod 16` (SEALED.md 10). The sender sets
   it; it is the last byte of every frame **[seen]**: frames sent with
   index 5 end in `05`. The receiver keeps a ring of 16 keys per sending
   participant and picks the key by that byte. On a new epoch the
   sender switches index; the receiver keeps the old index's key for
   ten seconds.
7. **Per sending device.** Keys are set per participant identifier,
   and the identifier is the **device id**: every device of the epoch
   has its key set under its own id, a sender's cryptor is made for
   this device's id, and a receiver's for the id of the device the
   track comes from. The server names that device: `device_id` beside
   each track of `call.offer`, in `call.track_published` and in each
   participant of the call (`docs/CALLS.md` section 10). A track whose
   device is not in the epoch has no key and nothing of it decrypts.
   A track for which the server names no device (a server older than
   `device_id`) is decrypted only if its account has exactly one
   device in the epoch; otherwise it too has no key.
8. **A new epoch during a call.** Every device's new key is set under
   the new key index, senders switch to that index at once, and a
   receiver, which reads the index from each frame, opens both for
   ten seconds. After that the other devices' keys at the old index
   are forgotten: the web chat deletes them; the app, whose key
   provider cannot remove a key, overwrites each with random bytes
   **[the overwriting is not observed on the library]**. Sixteen
   epochs on, an index comes round again and the new key replaces the
   old at once.
9. **The key index is not authenticated.** It is outside the
   associated data. Changing it in transit makes a receiver try
   another key, which fails; it cannot make a frame open under a key
   it was not sealed with.

## 2. The frame

An encrypted frame (the encoded frame before packetisation: one Opus
packet, one whole VP8 frame) is **[seen]**:

    +--------------------+---------------------------+--------+------+-----+
    | readable header    | AES-128-GCM ciphertext    | IV     | 0x0C | idx |
    | (n bytes, clear)   | of the rest, + 16-byte tag| 12 B   | 1 B  | 1 B |
    +--------------------+---------------------------+--------+------+-----+

- **Associated data** is the readable header, exactly those n bytes
  **[seen]**.
- **IV length byte** is 12 **[seen]**. A receiver reads it, and drops
  a frame where it is anything else.
- **Key index** is the last byte **[seen]**.
- **IV** (12 bytes) as the library builds it **[seen on audio and video]**:
  bytes 0–3 the RTP SSRC, bytes 4–7 the RTP timestamp, bytes 8–11 the
  timestamp minus (a per-sender frame counter mod 65,535), all big-endian.
  A receiver must not rely on this: it takes the IV from the trailer.
  A JavaScript sender may use any IV that never repeats under a key.
  The web chat uses 4 random bytes chosen when its worker starts,
  then an 8-byte count of the frames that worker has encrypted. (The library's IV
  can repeat only if SSRC, timestamp and counter all repeat, which
  within one call they do not.)
- **Readable header, n:**

  | Codec | n | Seen |
  |-------|---|------|
  | Opus (audio) | 1 (the TOC byte) | yes: 400 of 400 frames opened |
  | VP8 key frame | 10 (the 3-byte frame tag, start code `9d 01 2a`, width and height) | yes |
  | VP8 delta frame | 3 (the frame tag) | yes: 213 of 213 single-packet frames opened |
  | H.264 | not settled: the library walks NAL units and keeps NAL headers readable. Not to be used in a sealed call | no |

  The frame is what the RTP depacketiser produces: for VP8, the payload
  after the VP8 payload descriptor of RFC 7741, all packets of the frame
  joined. A browser's encoded-frame interface hands over exactly that.

  **The app's camera goes out as VP8.** On macOS the library offers
  H.264 first and the server takes the first codec it shares, so the app
  reorders its offer to put VP8 first (found in this experiment: before
  the fix, the app's camera was H.264).

  The key-frame bit is bit 0 of the first byte (0 = key frame), which
  is why it stays readable: the server switches layers at key frames.
  The web chat tells a key frame by that bit, on the way out and on
  the way in. A frame shorter than its readable header is all header
  (an Opus frame of one byte is that byte, then the tag of an empty
  ciphertext); a VP8 key frame shorter than ten bytes does not occur
  and would not open, since the receiver would count ten.

## 3. What a receiver does with a bad frame

- A frame that does not decrypt (wrong key, no key at that index, not
  encrypted at all) is **dropped**, never passed to the decoder, and
  the receiver reports "decryption failed" or "missing key" for that
  sender **[app: "decryption failed" was reported for frames the Go
  peer sent unencrypted; that they are dropped rather than decoded is
  the library's documented behaviour, not separately observed]**. The app shows "Cannot decrypt this
  call" after five seconds with no frame decrypted (CALLS.md 14).
- Before the receiver has a key, frames are dropped
  (`discardFrameWhenCryptorNotReady`) **[app: settings]**.
- The library sends nothing unencrypted once encryption is on. (It can
  pass through frames ending in configured "magic bytes"; Churr sets
  none.)

## 4. Test vector

    call sender key  1111…11 (32 bytes of 0x11)
    salt             "churr call frames v1"
    AES-128 key      3ee6a5a851bb3e6f98e06de3933a5cf4
    readable header  78                      (an Opus TOC byte)
    plaintext        0102030405
    IV               93a37d1a33cd967433cd9672
    key index        5
    frame            78 b69613a57e5cdd6a75a08bdc65af5fa00cc3dc091f 93a37d1a33cd967433cd9672 0c 05

(spaces added). This frame and others, with the keys derived from an
epoch secret (SEALED.md 5.2), a second epoch, and frames that must be
dropped, are in **`testdata/call-frames/vectors.json`**. The Go
reference writes and checks it (`go test ./internal/calls -run
TestCallFrameVectors`), the web chat's worker is run against it
(`e2e/run.sh call-frames`) and so is the app
(`app/test/core/calls/frame_vectors_test.dart`, where pure-Dart
AES-GCM stands for the plugin). `e2e/run.sh sealed-call` is a live
call in a sealed chat between two browsers, with a new epoch in the
middle and a device without the keys.

How the frames were captured and checked: the app's
README, "Frame encryption"; the Go side is `tools/callpeer`, whose
`open` function is a complete decryptor in 20 lines.

Interoperation seen: the app's audio and VP8 frames open in Go with
the derivation above, and frames the Go peer encrypted the same way
(its own IV layout: a fixed 4 bytes, the time, a counter) open in the
app (the app's receiver reported "ok").

## 5. Settings the app gives the plugin

    KeyProviderOptions(sharedKey: false,
                       ratchetSalt: "churr call frames v1",
                       ratchetWindowSize: 0, failureTolerance: -1,
                       keyRingSize: 16,
                       discardFrameWhenCryptorNotReady: true,
                       keyDerivationAlgorithm: HKDF)
    Algorithm.kAesGcm; one FrameCryptor per RTP sender (participant =
    own device id) and per RTP receiver (participant = the sending
    device's id, section 1.7).
