// The frame format of a sealed chat's call, as flutter_webrtc's frame
// cryptor produces it (docs/CALL-FRAMES.md, found by experiment and
// recorded in app/README.md, "Frame encryption"). The app does not
// encrypt frames itself: the plugin does, natively. This file holds the
// settings both sides must agree on, and the decisions about keys that
// need no plugin to make: whose key a line is decrypted with, and what
// changes when a new key epoch begins.
import 'call_media.dart';

/// The salt the key provider is given (`ratchetSalt`). The plugin uses
/// it as the salt of its own key derivation.
const callFrameRatchetSaltText = 'churr call frames v1';
final callFrameRatchetSalt = callFrameRatchetSaltText.codeUnits;

/// Whether the key provider derives the AES key with HKDF (true) or
/// PBKDF2 (false).
const callFrameUsesHkdf = true;

/// The info of that derivation: 128 zero bytes, fixed by the library.
const callFrameInfoLength = 128;

/// AES-128-GCM: the derived key's length in bytes.
const callFrameKeyLength = 16;

/// How many keys the plugin holds per participant, by key index.
const callFrameKeyRing = 16;

/// How long a receiver keeps the keys a new epoch replaced.
const callFrameKeepOldKeys = Duration(seconds: 10);

/// The readable bytes at the start of an encoded frame, which are the
/// associated data: 1 for audio (Opus), 10 for a VP8 key frame (bit 0 of
/// the first byte is 0), 3 for any other VP8 frame.
int callFrameHeaderLength({required bool audio, required List<int> frame}) {
  final n = audio ? 1 : (frame.isNotEmpty && frame[0] & 1 == 0 ? 10 : 3);
  return n < frame.length ? n : frame.length;
}

/// The key provider's name for the keys that decrypt one recv line: the
/// device the server says the line comes from. When the server names
/// none, the account's one device in the epoch. When that cannot be told
/// either, a name no key is ever set under, so the line's frames are
/// dropped and reported, never decoded as they are.
String frameParticipant(
  FrameKeys keys, {
  required String accountId,
  String? deviceId,
}) => deviceId ?? keys.soleDevice[accountId] ?? 'no-device:$accountId';

/// One key to give the key provider.
typedef FrameKeySlot = ({String device, int index, List<int> key});

/// What changes in the key provider when [fresh] keys replace [old]
/// (null at the start of a call).
class FrameKeyChange {
  const FrameKeyChange({required this.set, required this.forget});

  /// Set now: every device of the new epoch at the new key index.
  final List<FrameKeySlot> set;

  /// Forget after [callFrameKeepOldKeys]: the other devices' keys of the
  /// epoch before, which frames on their way may still need until then.
  /// This device's own old key is not listed: nothing is decrypted with it.
  final List<({String device, int index})> forget;
}

FrameKeyChange frameKeyChange(FrameKeys? old, FrameKeys fresh) {
  final index = fresh.keyIndex % callFrameKeyRing;
  return FrameKeyChange(
    set: [
      for (final e in fresh.keys.entries)
        (device: e.key, index: index, key: e.value),
    ],
    forget: [
      if (old != null)
        for (final device in old.keys.keys)
          if (device != old.ownDevice &&
              (old.keyIndex % callFrameKeyRing != index ||
                  !fresh.keys.containsKey(device)))
            (device: device, index: old.keyIndex % callFrameKeyRing),
    ],
  );
}
