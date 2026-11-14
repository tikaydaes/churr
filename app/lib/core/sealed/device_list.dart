// Device lists (docs/SEALED.md section 4): the signed list of an account's
// devices, its chain from version to version, and the checks a client
// makes before it trusts one.

import 'dart:typed_data';

import 'enc.dart';
import 'errors.dart';
import 'prim.dart';
import 'sign.dart';

// The platforms a device list names.
const platformAndroid = 1;
const platformIOS = 2;
const platformMacOS = 3;
const platformWindows = 4;
const platformLinux = 5;
const platformWeb = 6;
const allPlatforms = [1, 2, 3, 4, 5, 6];

/// The API's word for each platform number.
const platformNames = {
  1: 'android',
  2: 'ios',
  3: 'macos',
  4: 'windows',
  5: 'linux',
  6: 'web',
};

/// The inverse of [platformNames]; 0 for an unknown word.
int platformNumber(String name) => platformNames.entries
    .firstWhere((e) => e.value == name, orElse: () => const MapEntry(0, ''))
    .key;

/// The most devices a list may hold.
const maxDevices = 50;

/// One device in a list.
class ListDevice {
  final Uuid deviceId;
  final Uint8List signKey; // Ed25519 public key
  final Uint8List boxKey; // X25519 public key
  final int platform;
  final int addedAt;

  /// The device's own signature over account_id || device_id || sign_key
  /// || box_key, label "churr device proof v1".
  final Uint8List proof;

  ListDevice({
    required this.deviceId,
    required this.signKey,
    required this.boxKey,
    required this.platform,
    required this.addedAt,
    required this.proof,
  });
}

/// The list record of section 4.3.
class DeviceList {
  int format;
  Uuid accountId;
  String server;
  int version;
  Uint8List prev; // 32 bytes
  int reset;
  int keyVersion;
  Uint8List accountKey; // Ed25519 public key
  List<ListDevice> devices;
  int at;

  DeviceList({
    this.format = 1,
    required this.accountId,
    required this.server,
    required this.version,
    Uint8List? prev,
    this.reset = 0,
    required this.keyVersion,
    required this.accountKey,
    required this.devices,
    required this.at,
  }) : prev = prev ?? Uint8List(32);

  /// Writes the record. The devices must already be sorted by device_id.
  Uint8List encode() {
    if (accountKey.length != 32) throw ArgumentError('an account key is 32 bytes');
    if (devices.length > maxDevices) throw RecordError.count;
    final w = Writer()
      ..u8(format)
      ..uuid(accountId)
      ..text(server)
      ..u64(version)
      ..raw(prev)
      ..u8(reset)
      ..u32(keyVersion)
      ..raw(accountKey)
      ..u32(devices.length);
    for (var i = 0; i < devices.length; i++) {
      final d = devices[i];
      if (d.signKey.length != 32 ||
          d.boxKey.length != 32 ||
          d.proof.length != 64) {
        throw ArgumentError(
          'a device needs a 32-byte sign key, a 32-byte box key and a 64-byte proof',
        );
      }
      if (i > 0 && devices[i - 1].deviceId.compareTo(d.deviceId) >= 0) throw RecordError.order;
      w
        ..uuid(d.deviceId)
        ..raw(d.signKey)
        ..raw(d.boxKey)
        ..u8(d.platform)
        ..time(d.addedAt)
        ..raw(d.proof);
    }
    w.time(at);
    return w.bytes();
  }

  static const _deviceSize = 16 + 32 + 32 + 1 + 8 + 64;

  /// Reads a list record, refusing anything that is not its one valid form.
  static DeviceList decode(List<int> b) {
    final r = Reader(b);
    final format = r.choice(const [1]);
    final account = r.uuid();
    final server = r.text();
    final version = r.u64();
    final prev = r.raw(32);
    final reset = r.choice(const [0, 1]);
    final keyVersion = r.u32();
    final accountKey = r.raw(32);
    final n = r.count(_deviceSize);
    if (n > maxDevices) r.fail(RecordError.count);
    final devices = <ListDevice>[];
    for (var i = 0; i < n && r.err == null; i++) {
      final d = ListDevice(
        deviceId: r.uuid(),
        signKey: r.raw(32),
        boxKey: r.raw(32),
        platform: r.choice(allPlatforms),
        addedAt: r.u64(),
        proof: r.raw(64),
      );
      if (i > 0 && devices[i - 1].deviceId.compareTo(d.deviceId) >= 0) r.fail(RecordError.order);
      devices.add(d);
    }
    final at = r.u64();
    r.done();
    return DeviceList(
      format: format,
      accountId: account,
      server: server,
      version: version,
      prev: prev,
      reset: reset,
      keyVersion: keyVersion,
      accountKey: accountKey,
      devices: devices,
      at: at,
    );
  }

  ListDevice? device(Uuid id) {
    for (final d in devices) {
      if (d.deviceId == id) return d;
    }
    return null;
  }
}

/// What a device proof signs: account_id || device_id || sign_key || box_key.
Uint8List proofBytes(
  Uuid account,
  Uuid device,
  List<int> signKey,
  List<int> boxKey,
) => cat([account.bytes, device.bytes, signKey, boxKey]);

/// A device's own signature that it holds its signing key and belongs to
/// this account.
Future<Uint8List> deviceProof(
  SigningKey sign_,
  Uuid account,
  Uuid device,
  List<int> boxKey,
) => sign(
  sign_,
  labelDeviceProof,
  proofBytes(account, device, sign_.public, boxKey),
);

/// The hash of a list record.
Uint8List listHash(List<int> list) => hashOf(labelDeviceListHash, list);

/// What is stored and sent for one version.
class SignedList {
  final Uint8List list;
  final Uint8List sig;
  final Uint8List? prevSig; // null unless has_prev_sig is 1
  SignedList(this.list, this.sig, [this.prevSig]);

  Uint8List encode() {
    if (sig.length != 64 || (prevSig != null && prevSig!.length != 64)) {
      throw ArgumentError('a signature is 64 bytes');
    }
    final w = Writer()
      ..blob(list)
      ..raw(sig);
    if (prevSig != null) {
      w
        ..u8(1)
        ..raw(prevSig!);
    } else {
      w.u8(0);
    }
    return w.bytes();
  }

  static SignedList decode(List<int> b) {
    final r = Reader(b);
    final list = r.blob();
    final sig = r.raw(64);
    Uint8List? prev;
    if (r.choice(const [0, 1]) == 1) prev = r.raw(64);
    r.done();
    return SignedList(list, sig, prev);
  }
}

/// Signs a list record with its account key and, for a key change, with
/// the previous version's account key too.
Future<SignedList> signList(
  Uint8List list,
  SigningKey accountKey, [
  SigningKey? previous,
]) async {
  final sig = await sign(accountKey, labelDeviceList, list);
  final prev = previous == null
      ? null
      : await sign(previous, labelDeviceListRollover, list);
  return SignedList(list, sig, prev);
}

/// The checks one version allows by itself: it parses, its own account key
/// signed it, every proof verifies and no identifier or key repeats.
Future<DeviceList> checkListAlone(SignedList s) async {
  final DeviceList l;
  try {
    l = DeviceList.decode(s.list);
  } on RecordError catch (e) {
    throw ListError(0, e.message);
  }
  if (l.version == 0) throw const ListError(1, "a list's version starts at 1");
  final seen = <String>{};
  for (final d in l.devices) {
    if (!await verify(
      d.signKey,
      labelDeviceProof,
      proofBytes(l.accountId, d.deviceId, d.signKey, d.boxKey),
      d.proof,
    )) {
      throw const ListError(2, "a device's proof does not verify");
    }
    for (final k in ['s${hexOf(d.signKey)}', 'b${hexOf(d.boxKey)}']) {
      if (!seen.add(k)) throw const ListError(2, 'two devices have the same key');
    }
  }
  if (!await verify(l.accountKey, labelDeviceList, s.list, s.sig)) {
    throw const ListError(3, 'the list is not signed by its account key');
  }
  return l;
}

/// Checks a list a client has no pin for: trust on first use.
Future<DeviceList> checkFirstList(SignedList s) async {
  final l = await checkListAlone(s);
  if (l.version == 1) {
    if (!l.prev.every((b) => b == 0)) throw const ListError(1, 'version 1 has a previous hash');
    if (s.prevSig != null) throw const ListError(3, 'version 1 has a rollover signature');
  }
  return l;
}

const stepOrdinary = 'ordinary';
const stepKeyChange = 'key_change';
const stepReset = 'reset';

/// Checks the step from one accepted version (its record, exactly as
/// signed) to the next, and says which of the three kinds it is.
Future<(DeviceList, String)> checkListStep(
  Uint8List prev,
  SignedList next,
) async {
  final DeviceList p;
  try {
    p = DeviceList.decode(prev);
  } on RecordError {
    throw const SealedError('the previous list does not parse');
  }
  final l = await checkListAlone(next);
  // 1. The chain.
  if (l.accountId != p.accountId || l.server != p.server) {
    throw const ListError(1, 'the account or server changed');
  }
  if (l.version != p.version + 1) throw const ListError(1, 'the version is not one higher');
  if (!bytesEqual(l.prev, listHash(prev))) {
    throw const ListError(1, 'prev is not the hash of the previous version');
  }
  // 3. One of the three kinds of step.
  final sameKey = bytesEqual(l.accountKey, p.accountKey);
  if (l.reset == 1) {
    if (l.keyVersion != p.keyVersion + 1) throw const ListError(3, 'a reset must raise the key version by one');
    if (next.prevSig != null) throw const ListError(3, 'a reset carries no rollover signature');
    return (l, stepReset);
  }
  if (sameKey && l.keyVersion == p.keyVersion) {
    if (next.prevSig != null) throw const ListError(3, 'an ordinary change carries no rollover signature',);
    return (l, stepOrdinary);
  }
  if (!sameKey && l.keyVersion == p.keyVersion + 1) {
    if (next.prevSig == null ||
        !await verify(
          p.accountKey,
          labelDeviceListRollover,
          next.list,
          next.prevSig!,
        )) {
      throw const ListError(
        3,
        'the previous account key did not sign the key change',
      );
    }
    return (l, stepKeyChange);
  }
  throw const ListError(
    3,
    'the account key and key version do not fit any kind of step',
  );
}
