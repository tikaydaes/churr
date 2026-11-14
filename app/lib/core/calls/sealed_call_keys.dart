// The frame keys of a sealed chat's call, from the sealed-chat library
// (docs/SEALED.md section 10, docs/CALL-FRAMES.md section 1).
import '../sealed/device.dart';
import '../sealed/prim.dart';
import 'call_media.dart';

/// The call sender key of every device of the chat's current epoch, for
/// the call [callId]: this device's own and the others'. The server names
/// the device each track of the call comes from, and that device's key
/// decrypts it. Null when the device holds no current epoch secret, or is
/// not in the epoch itself.
FrameKeys? sealedFrameKeys(Chat chat, Uuid ownDevice, String callId) {
  final call = Uuid.tryParse(callId);
  final cur = chat.current;
  final record = cur?.record;
  if (call == null || cur == null || record == null) return null;
  final Map<Uuid, List<int>> keys;
  final int index;
  try {
    (keys, index) = chat.callKeys(call);
  } on Object {
    return null;
  }
  if (keys[ownDevice] == null) return null;
  final byAccount = <String, List<String>>{};
  for (final r in record.recipients) {
    byAccount
        .putIfAbsent(r.accountId.toString(), () => [])
        .add(r.deviceId.toString());
  }
  return FrameKeys(
    ownDevice: ownDevice.toString(),
    keys: {for (final e in keys.entries) e.key.toString(): e.value},
    keyIndex: index,
    keyEpoch: cur.keyEpoch,
    soleDevice: {
      for (final e in byAccount.entries)
        if (e.value.length == 1) e.key: e.value.single,
    },
  );
}
