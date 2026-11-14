// A Device is one installed app or browser profile playing its whole part
// in sealed chats: its two key pairs, the account's backup key once it is
// unlocked, the device lists it has pinned, and its sealed chats
// (chat.dart). It is a port of the Go reference's Device
// (internal/sealed/ref/device.go) with the same behaviour.
//
// It talks to nobody. Its methods return the bytes to send to the server
// and take the bytes the server gave back. Its state is saved through a
// [SealedStore] (store.dart), which the app backs with its encrypted local
// store.

import 'dart:convert';
import 'dart:typed_data';

import 'backup.dart';
import 'bip39.dart';
import 'content.dart';
import 'device_list.dart';
import 'epoch.dart';
import 'epoch.dart' as ep show makeEpoch;
import 'errors.dart';
import 'message.dart';
import 'prim.dart';
import 'random.dart';
import 'seal.dart' show saltSize;
import 'sign.dart' show labelDeviceAttach, labelled;
import 'unlock.dart';

part 'chat.dart';
part 'store.dart';

/// Something a device shows its owner. Notices are drawn by the client;
/// they are not messages and the server does not know they were shown.
class Notice {
  /// device_added, device_removed, key_change, reset, list_refused or
  /// out_of_step.
  final String kind;

  /// Whose device list changed, or whose chat it is about.
  final Uuid account;

  /// The kind of device added; 0 otherwise.
  final int platform;

  /// Set for out_of_step.
  final Uuid? conversation;
  Notice(this.kind, this.account, {this.platform = 0, this.conversation});
}

const noticeDeviceAdded = 'device_added';
const noticeDeviceRemoved = 'device_removed';
const noticeKeyChange = 'key_change';
const noticeReset = 'reset';
const noticeListRefused = 'list_refused';
const noticeOutOfStep = 'out_of_step';

/// One accepted version of a device list.
class PinnedList {
  final SignedList signed;
  final DeviceList list;
  PinnedList(this.signed, this.list);
}

/// What a device keeps for one account: every version of its list it has
/// accepted, the newest of which is the pin proper.
class Pin {
  final Map<int, PinnedList> versions;
  int lowest, newest;

  /// The highest accepted version that was a reset; 0 if none. Nothing
  /// from before it is ever shared with the account.
  int lastReset;

  /// The mark set when the two people compared codes. A reset removes it;
  /// [hold] is then set until the person acknowledges the notice.
  bool verified, hold;

  Pin(
    this.versions,
    this.lowest,
    this.newest, {
    this.lastReset = 0,
    this.verified = false,
    this.hold = false,
  });

  PinnedList? get newestList => versions[newest];
}

/// One locked copy to store.
class Copy {
  final int kind;
  final Uuid id; // the passkey or device; zero for the phrase
  final Uint8List locked;
  Copy(this.kind, this.id, this.locked);
}

/// One backup record as the server files it.
class StoredRecord {
  final Uuid conversationId;
  final int keyEpoch;
  final int keyVersion;
  final Uint8List locked;
  StoredRecord(
    this.conversationId,
    this.keyEpoch,
    this.keyVersion,
    this.locked,
  );
}

/// What a device hands the server in one operation when it creates,
/// replaces or resets its account's backup.
class Bundle {
  final int keyVersion;

  /// The key version a replacement or reset takes the place of; 0 for a
  /// creation.
  int replaces = 0;
  final int revision;
  final Uint8List accountRecord;
  final List<Copy> copies;
  final List<StoredRecord> records;
  final int listVersion;
  final SignedList list;

  /// The twelve words, when new ones were made. Shown once, never sent.
  final String phrase;

  final Uint8List _backupKey;
  final SigningKey _accountKey;
  final AccountRecord _record;

  Bundle._(
    this.keyVersion,
    this.revision,
    this.accountRecord,
    this.copies,
    this.records,
    this.listVersion,
    this.list,
    this.phrase,
    this._backupKey,
    this._accountKey,
    this._record,
  );
}

/// What the server gives a complete session of an account to unlock with.
class StoredBackup {
  final int keyVersion;
  final int revision;
  final Uint8List accountRecord;
  StoredBackup(this.keyVersion, this.revision, this.accountRecord);
}

/// A new revision of the account record with the locked copy that goes
/// with it.
class RecordUpdate {
  final int revision;
  final Uint8List accountRecord;
  final Copy copy;
  final AccountRecord _record;
  RecordUpdate._(this.revision, this.accountRecord, this.copy, this._record);
}

/// What a key replacement takes away.
class Replacement {
  final List<Uuid> devices; // revoked: left out of the list, no copy
  final List<Uuid> passkeys; // ways removed
  final bool newPhrase;
  const Replacement({
    this.devices = const [],
    this.passkeys = const [],
    this.newPhrase = false,
  });
}

/// Fetches versions of an account's device list above [after].
typedef FetchLists = Future<List<SignedList>> Function(Uuid account, int after);

class _ListPlan {
  final int version;
  final Uint8List prev;
  final bool reset;
  final List<ListDevice> devices;
  final SigningKey? previous;
  _ListPlan(
    this.version,
    this.devices, {
    Uint8List? prev,
    this.reset = false,
    this.previous,
  }) : prev = prev ?? Uint8List(32);
}

/// The state of one device.
class Device {
  final Uuid account;
  final String server;
  Uuid id;
  final int platform;
  int addedAt;

  /// The device's own keys. They never leave it and are never backed up.
  final SigningKey sign;
  final Uint8List box;

  SealedRandom random;
  int Function() now;

  /// Told of every random value drawn and what it was for.
  void Function(String what, Uint8List value)? trace;

  /// How the device asks its server for list versions it has not checked.
  FetchLists? fetchLists;

  /// Where the device's state is saved; see store.dart.
  SealedStore? store;

  // The unlocked state; all null while the device is locked.
  Uint8List? backupKey;
  int keyVersion = 0;
  SigningKey? accountKey;
  AccountRecord? record;

  final Map<Uuid, Pin> pins = {};
  final Map<Uuid, Chat> chats = {};
  final List<Notice> notices = [];

  Device._(
    this.account,
    this.server,
    this.id,
    this.platform,
    this.addedAt,
    this.sign,
    this.box,
    this.random,
    this.now,
  );

  /// Makes a device and its two key pairs (drawn as `device_sign_seed`
  /// then `device_box_private`). The keys are made before the device signs
  /// in; [id] is the identifier the server gives its row, set once known.
  static Future<Device> create({
    required Uuid account,
    required String server,
    required int platform,
    required SealedRandom random,
    required int Function() now,
    Uuid? id,
    void Function(String what, Uint8List value)? trace,
  }) async {
    Uint8List draw(String what, int n) {
      final b = random.take(what, n);
      trace?.call(what, b);
      return b;
    }

    final sign = await SigningKey.fromSeed(draw('device_sign_seed', 32));
    final box = draw('device_box_private', 32);
    return Device._(
      account,
      server,
      id ?? Uuid.zero,
      platform,
      now(),
      sign,
      box,
      random,
      now,
    )..trace = trace;
  }

  Uint8List _random(String what, int n) {
    final b = random.take(what, n);
    trace?.call(what, b);
    return b;
  }

  Uint8List get signPublic => sign.public;
  Uint8List get boxPublic => x25519Public(box);
  bool get unlocked => backupKey != null;

  Future<ListDevice> _entry() async => ListDevice(
    deviceId: id,
    signKey: signPublic,
    boxKey: boxPublic,
    platform: platform,
    addedAt: addedAt,
    proof: await deviceProof(sign, account, id, boxPublic),
  );

  /// Proves the signing key when the same app signs in again (section 4.1).
  Future<Uint8List> attachSignature(List<int> challenge) =>
      sign.signRaw(labelled(labelDeviceAttach, [challenge]));

  /// The record of one accepted version of one account's list, or null.
  Uint8List? listOf(Uuid account, int version) =>
      pins[account]?.versions[version]?.signed.list;

  /// Takes versions of one account's device list and checks them (4.4).
  ///
  /// With no pin, the newest one given is pinned (trust on first use).
  /// Versions above the pin are accepted one step at a time, with a notice
  /// for each change. Versions below what the device holds are history:
  /// accepted only if they chain into it, and they move no pin. A version
  /// already held must be byte for byte the same. On a refusal the device
  /// keeps its pin and shows "The server sent a device list that does not
  /// check out".
  Future<void> learnLists(Uuid forAccount, List<SignedList> lists) async {
    try {
      await _learnLists(forAccount, lists);
    } on SealedError {
      notices.add(Notice(noticeListRefused, forAccount));
      rethrow;
    }
  }

  Future<void> _learnLists(Uuid forAccount, List<SignedList> lists) async {
    final given = <(SignedList, DeviceList)>[];
    for (final s in lists) {
      final l = await checkListAlone(s);
      if (l.accountId != forAccount) throw const ListError(1, "the list is another account's");
      given.add((s, l));
    }
    given.sort((a, b) => a.$2.version.compareTo(b.$2.version));
    if (given.isEmpty) return;
    var p = pins[forAccount];
    if (p == null) {
      final first = given.last;
      await checkFirstList(first.$1);
      p = Pin(
        {first.$2.version: PinnedList(first.$1, first.$2)},
        first.$2.version,
        first.$2.version,
        lastReset: first.$2.reset == 1 ? first.$2.version : 0,
      );
      pins[forAccount] = p;
    }
    // Upwards from the pin.
    for (final (s, l) in given) {
      final v = l.version;
      final have = p.versions[v];
      if (have != null) {
        if (!bytesEqual(have.signed.list, s.list)) {
          throw const ListError(
            1,
            'the server sent a different list for a version already accepted',
          );
        }
        continue;
      }
      if (v < p.lowest) continue; // history, below
      if (v != p.newest + 1) throw const ListError(1, 'a version is missing between the pin and this list',);
      final prev = p.versions[p.newest]!;
      final (checked, step) = await checkListStep(prev.signed.list, s);
      p.versions[v] = PinnedList(s, checked);
      p.newest = v;
      _noteStep(forAccount, p, prev.list, checked, step);
    }
    // Downwards, for history: each older version must step into the one
    // above it.
    for (final (s, l) in given.reversed) {
      final v = l.version;
      if (v >= p.lowest) continue;
      if (v != p.lowest - 1) throw const ListError(1, 'a version is missing below what this device holds',);
      final above = p.versions[p.lowest]!;
      final (_, step) = await checkListStep(s.list, above.signed);
      p.versions[v] = PinnedList(s, l);
      p.lowest = v;
      if (step == stepReset && above.list.version > p.lastReset) p.lastReset = above.list.version;
    }
  }

  void _noteStep(
    Uuid forAccount,
    Pin p,
    DeviceList before,
    DeviceList after,
    String step,
  ) {
    switch (step) {
      case stepReset:
        notices.add(Notice(noticeReset, forAccount));
        p.lastReset = after.version;
        if (p.verified) {
          p.verified = false;
          p.hold = true;
        }
        return;
      case stepKeyChange:
        notices.add(Notice(noticeKeyChange, forAccount));
    }
    for (final dev in after.devices) {
      if (before.device(dev.deviceId) == null) notices.add(Notice(noticeDeviceAdded, forAccount, platform: dev.platform),);
    }
    for (final dev in before.devices) {
      if (after.device(dev.deviceId) == null) notices.add(Notice(noticeDeviceRemoved, forAccount));
    }
  }

  /// The person acknowledging a reset notice, which lets the device send.
  void acknowledge(Uuid forAccount) => pins[forAccount]?.hold = false;

  /// The person marking another as verified after their codes matched.
  void markVerified(Uuid forAccount) => pins[forAccount]?.verified = true;

  /// The 60 digits this device shows for its chat with another account.
  String compareCodeWith(Uuid other, String otherServer) {
    final mine = pins[account]?.newestList, theirs = pins[other]?.newestList;
    if (mine == null || theirs == null) {
      throw const SealedError(
        'no device list is pinned for one of the two accounts',
      );
    }
    return compareCode(
      compareHalf(mine.list.accountKey, account, server),
      compareHalf(theirs.list.accountKey, other, otherServer),
    );
  }

  /// Makes sure the device has checked one version of an account's list,
  /// asking the server for versions if it must.
  Future<bool> _ensureList(Uuid forAccount, int version) async {
    if (version == 0 || listOf(forAccount, version) != null) return true;
    if (fetchLists == null) return false;
    try {
      final lists = await fetchLists!(forAccount, 0);
      await learnLists(forAccount, lists);
    } on SealedError {
      return false;
    }
    return listOf(forAccount, version) != null;
  }

  /// Switches to the keys of a bundle the server has accepted, and pins
  /// the device list that came with it.
  Future<void> adopt(Bundle b) async {
    backupKey = b._backupKey;
    accountKey = b._accountKey;
    keyVersion = b.keyVersion;
    record = b._record;
    await learnLists(account, [b.list]);
  }

  Way _phraseWay(List<int> entropy) => Way(
    wayPhrase,
    Uuid.zero,
    x25519Public(unlockKeyFromPhrase(entropy, account)),
  );

  Future<List<Copy>> _copiesFor(
    Uint8List backupKey,
    SigningKey accountKey,
    int keyVersion,
    List<Way> ways,
    List<ListDevice> devices,
  ) async {
    final out = <Copy>[];
    for (final w in ways) {
      final locked = await makeLockedCopy(
        w.unlockPublic,
        account,
        keyVersion,
        w.kind,
        w.id,
        backupKey,
        accountKey,
        _random('locked_copy_one_time:${wayNames[w.kind]}:${w.id}', 32),
      );
      out.add(Copy(w.kind, w.id, locked));
    }
    for (final dev in devices) {
      final locked = await makeLockedCopy(
        dev.boxKey,
        account,
        keyVersion,
        wayDevice,
        dev.deviceId,
        backupKey,
        accountKey,
        _random('locked_copy_one_time:device:${dev.deviceId}', 32),
      );
      out.add(Copy(wayDevice, dev.deviceId, locked));
    }
    return out;
  }

  /// What the first device of an account does when it finds the account
  /// has no backup (section 13.1): backup key, account key, phrase, and
  /// version 1 of the device list holding itself.
  Future<Bundle> createBackup() async {
    final backupKey = _random('backup_key', 32);
    final accountKey = await SigningKey.fromSeed(
      _random('account_key_seed', 32),
    );
    final entropy = _random('phrase_entropy', 16);
    final rec = AccountRecord(
      accountId: account,
      keyVersion: 1,
      revision: 1,
      accountKey: accountKey.public,
      accountKeyPrivate: accountKey.seed,
      ways: [_phraseWay(entropy)],
      at: now(),
    );
    return _bundle(
      backupKey,
      accountKey,
      rec,
      phraseOf(entropy),
      _ListPlan(1, [await _entry()]),
      const [],
    );
  }

  Future<Bundle> _bundle(
    Uint8List backupKey,
    SigningKey accountKey,
    AccountRecord rec,
    String phrase,
    _ListPlan lp,
    List<StoredRecord> records,
  ) async {
    final locked = lockAccountRecord(
      backupKey,
      rec,
      _random('account_record_salt', saltSize),
    );
    // A device copy for every listed device but this one.
    final others = [
      for (final d in lp.devices)
        if (d.deviceId != id) d,
    ];
    final copies = await _copiesFor(
      backupKey,
      accountKey,
      rec.keyVersion,
      rec.ways,
      others,
    );
    final list = await _signedList(lp, rec.keyVersion, accountKey);
    return Bundle._(
      rec.keyVersion,
      rec.revision,
      locked,
      copies,
      records,
      lp.version,
      list,
      phrase,
      backupKey,
      accountKey,
      rec,
    );
  }

  Future<SignedList> _signedList(
    _ListPlan lp,
    int keyVersion,
    SigningKey accountKey,
  ) {
    final devices = [...lp.devices]
      ..sort((a, b) => a.deviceId.compareTo(b.deviceId));
    final l = DeviceList(
      accountId: account,
      server: server,
      version: lp.version,
      prev: lp.prev,
      reset: lp.reset ? 1 : 0,
      keyVersion: keyVersion,
      accountKey: accountKey.public,
      devices: devices,
      at: now(),
    );
    return signList(l.encode(), accountKey, lp.previous);
  }

  Future<SignedList> _nextOwnList(
    Future<List<ListDevice>> Function(List<ListDevice>) change,
  ) async {
    if (!unlocked) throw const LockedError();
    final cur = pins[account]?.newestList;
    if (cur == null) throw const SealedError("the device has not pinned its own account's list",);
    if (!bytesEqual(cur.list.accountKey, accountKey!.public)) {
      throw const SealedError(
        "the device's account key is not the one in the list; it must unlock again",
      );
    }
    return _signedList(
      _ListPlan(
        cur.list.version + 1,
        await change([...cur.list.devices]),
        prev: listHash(cur.signed.list),
      ),
      keyVersion,
      accountKey!,
    );
  }

  /// The next version of the account's list with this device in it: what
  /// a device publishes when it has just been unlocked (section 7.2).
  Future<SignedList> listWithSelf() => _nextOwnList((devices) async {
    if (devices.any((d) => d.deviceId == id)) return devices;
    return [...devices, await _entry()];
  });

  /// The next version without some devices: signing out, or clearing a
  /// retired one away (section 7.3).
  Future<SignedList> listWithout(List<Uuid> gone) => _nextOwnList(
    (devices) async => [
      for (final d in devices)
        if (!gone.contains(d.deviceId)) d,
    ],
  );

  /// The checks every way of unlocking ends with (13.3, "Why the
  /// signature"); if they pass, the device is unlocked.
  Future<void> _adoptBackupKey(
    Uint8List key,
    StoredBackup sb,
    int copyKind,
    Uuid copyId,
    Uint8List? copyStored,
  ) async {
    final r = record;
    if (r != null &&
        (sb.keyVersion < r.keyVersion ||
            (sb.keyVersion == r.keyVersion && sb.revision < r.revision))) {
      throw const UnlockError(
        'the server gave an account record older than one this device has seen',
      );
    }
    final AccountRecord rec;
    try {
      rec = await openAccountRecord(
        key,
        account,
        sb.keyVersion,
        sb.revision,
        sb.accountRecord,
      );
    } on SealedError {
      throw const UnlockError('the account record does not open with that key');
    }
    if (copyStored != null &&
        !await verifyLockedCopy(
          rec.accountKey,
          account,
          sb.keyVersion,
          copyKind,
          copyId,
          copyStored,
        )) {
      throw const UnlockError(
        'the locked copy is not signed by the account key',
      );
    }
    final pin = pins[account]?.newestList;
    if (pin != null &&
        (!bytesEqual(pin.list.accountKey, rec.accountKey) ||
            pin.list.keyVersion != rec.keyVersion)) {
      throw const UnlockError(
        "the account key is not the one in the account's device list",
      );
    }
    backupKey = key;
    keyVersion = rec.keyVersion;
    accountKey = await SigningKey.fromSeed(rec.accountKeyPrivate);
    record = rec;
  }

  /// Opens a locked copy with an unlock key's private half and unlocks.
  Future<void> unlockWithCopy(
    Uint8List unlockPrivate,
    int kind,
    Uuid copyId,
    Uint8List copyStored,
    StoredBackup sb,
  ) async {
    final Uint8List key;
    try {
      key = openLockedCopy(
        unlockPrivate,
        account,
        sb.keyVersion,
        kind,
        copyId,
        copyStored,
      );
    } on SealedError {
      throw const UnlockError('the locked copy does not open');
    }
    await _adoptBackupKey(key, sb, kind, copyId, copyStored);
  }

  /// Unlocks with the twelve words.
  Future<void> unlockWithPhrase(
    String typed,
    Uint8List copyStored,
    StoredBackup sb,
  ) => unlockWithCopy(
    unlockKeyFromPhrase(readPhrase(typed), account),
    wayPhrase,
    Uuid.zero,
    copyStored,
    sb,
  );

  /// Unlocks with a passkey's PRF output.
  Future<void> unlockWithPrf(
    Uint8List prfOutput,
    Uint8List credentialId,
    Uuid passkey,
    Uint8List copyStored,
    StoredBackup sb,
  ) => unlockWithCopy(
    unlockKeyFromPrf(prfOutput, account, credentialId),
    wayPasskey,
    passkey,
    copyStored,
    sb,
  );

  /// Unlocks with a copy sealed to this device's own box key: the answer to
  /// its unlock request, or its copy after a key replacement.
  Future<void> unlockWithDeviceCopy(Uint8List copyStored, StoredBackup sb) =>
      unlockWithCopy(box, wayDevice, id, copyStored, sb);

  /// Unlocks an app with the hand-over that came with its tokens.
  Future<void> unlockWithHandover(
    Uint8List handover,
    String codeChallenge,
    StoredBackup sb,
  ) async {
    final Uint8List key;
    try {
      key = openHandover(box, account, codeChallenge, handover);
    } on SealedError {
      throw const UnlockError('the hand-over does not open');
    }
    await _adoptBackupKey(key, sb, 0, Uuid.zero, null);
  }

  /// Unlocks an app signed in by QR pairing.
  Future<void> unlockWithPairingHandover(
    Uint8List handover,
    Uint8List pairingSecret,
    StoredBackup sb,
  ) async {
    final Uint8List key;
    try {
      key = openPairingHandover(box, pairingSecret, account, handover);
    } on SealedError {
      throw const UnlockError('the hand-over does not open');
    }
    await _adoptBackupKey(key, sb, 0, Uuid.zero, null);
  }

  /// Forgets the backup key and account key.
  void lockDevice() {
    backupKey = null;
    accountKey = null;
    record = null;
    keyVersion = 0;
  }

  /// What an unlocked page makes for an app after a passkey sign-in.
  Uint8List handoverFor(Uint8List appBoxKey, String codeChallenge) {
    if (!unlocked) throw const LockedError();
    return sealHandover(
      appBoxKey,
      account,
      codeChallenge,
      backupKey!,
      _random('handover_one_time', 32),
    );
  }

  /// What an unlocked web chat makes for a device that scanned its QR code.
  Uint8List pairingHandoverFor(Uint8List appBoxKey, Uint8List pairingSecret) {
    if (!unlocked) throw const LockedError();
    return sealPairingHandover(
      appBoxKey,
      pairingSecret,
      account,
      backupKey!,
      _random('pairing_one_time', 32),
      _random('pairing_salt', saltSize),
    );
  }

  /// The unlock request a new, locked device posts, and the code it shows.
  Future<(Uint8List, String)> newUnlockRequest() async {
    final request = UnlockRequest(
      accountId: account,
      deviceId: id,
      signKey: signPublic,
      boxKey: boxPublic,
      platform: platform,
      at: now(),
      proof: await deviceProof(sign, account, id, boxPublic),
    ).encode();
    return (request, approvalCode(request));
  }

  /// Answers an unlock request after the owner scanned or typed the new
  /// device's code: only if the code computed from the request is that
  /// code, with a locked copy for the new device's box key.
  Future<Uint8List> answerUnlockRequest(Uint8List request, String code) async {
    if (!unlocked) throw const LockedError();
    final u = await checkUnlockRequest(request);
    if (u.accountId != account) throw const SealedError('the request is for another account');
    if (normalizeApprovalCode(code) != approvalCode(request)) throw const CodeError();
    return makeLockedCopy(
      u.boxKey,
      account,
      keyVersion,
      wayDevice,
      u.deviceId,
      backupKey!,
      accountKey!,
      _random('locked_copy_one_time:device:${u.deviceId}', 32),
    );
  }

  /// A locked copy for another device's box key.
  Future<Uint8List> deviceCopyFor(Uuid device, Uint8List boxKey) {
    if (!unlocked) throw const LockedError();
    return makeLockedCopy(
      boxKey,
      account,
      keyVersion,
      wayDevice,
      device,
      backupKey!,
      accountKey!,
      _random('locked_copy_one_time:device:$device', 32),
    );
  }

  /// "Let the passkey unlock sealed chats?" answered yes (section 13.4).
  Future<RecordUpdate> approvePasskey(
    Uuid passkey,
    Uint8List unlockPublic,
  ) async {
    if (!unlocked) throw const LockedError();
    final rec = record!.copy();
    rec.ways = [
      for (final w in record!.ways)
        if (!(w.kind == wayPasskey && w.id == passkey)) w,
      Way(wayPasskey, passkey, unlockPublic),
    ];
    sortWays(rec.ways);
    rec.revision++;
    rec.at = now();
    final locked = lockAccountRecord(
      backupKey!,
      rec,
      _random('account_record_salt', saltSize),
    );
    final cp = await makeLockedCopy(
      unlockPublic,
      account,
      keyVersion,
      wayPasskey,
      passkey,
      backupKey!,
      accountKey!,
      _random('locked_copy_one_time:passkey:$passkey', 32),
    );
    return RecordUpdate._(
      rec.revision,
      locked,
      Copy(wayPasskey, passkey, cp),
      rec,
    );
  }

  /// Switches to an account record revision the server has accepted.
  void adoptRecord(RecordUpdate u) => record = u._record;

  /// Replaces the backup key and account key together (section 13.6).
  /// [records] are every backup record of the account; each is opened and
  /// locked again. The ways come from the old account record, never from
  /// the server.
  Future<Bundle> replaceBackup(
    Replacement r,
    List<StoredRecord> records,
  ) async {
    if (!unlocked) throw const LockedError();
    final cur = pins[account]?.newestList;
    if (cur == null) throw const SealedError("the device has not pinned its own account's list",);
    final newBackupKey = _random('backup_key', 32);
    final newAccountKey = await SigningKey.fromSeed(
      _random('account_key_seed', 32),
    );
    final rec = AccountRecord(
      accountId: account,
      keyVersion: keyVersion + 1,
      revision: record!.revision + 1,
      accountKey: newAccountKey.public,
      accountKeyPrivate: newAccountKey.seed,
      ways: [],
      at: now(),
    );
    var phrase = '';
    for (var w in record!.ways) {
      final drop = w.kind == wayPasskey && r.passkeys.contains(w.id);
      if (w.kind == wayPhrase && r.newPhrase) {
        final entropy = _random('phrase_entropy', 16);
        w = _phraseWay(entropy);
        phrase = phraseOf(entropy);
      }
      if (!drop) rec.ways.add(w);
    }
    sortWays(rec.ways);
    final relocked = <StoredRecord>[];
    for (final old in records) {
      final BackupRecord b;
      try {
        b = openBackupRecord(
          backupKey!,
          account,
          old.conversationId,
          old.keyEpoch,
          old.keyVersion,
          old.locked,
        );
      } on SealedError {
        continue; // left by an earlier key and lost already
      }
      final locked = lockBackupRecord(
        newBackupKey,
        account,
        rec.keyVersion,
        b,
        _random(
          'backup_record_salt:${old.conversationId}:${old.keyEpoch}',
          saltSize,
        ),
      );
      relocked.add(
        StoredRecord(old.conversationId, old.keyEpoch, rec.keyVersion, locked),
      );
    }
    final devices = [
      for (final d in cur.list.devices)
        if (!r.devices.contains(d.deviceId)) d,
    ];
    final b = await _bundle(
      newBackupKey,
      newAccountKey,
      rec,
      phrase,
      _ListPlan(
        cur.list.version + 1,
        devices,
        prev: listHash(cur.signed.list),
        previous: accountKey,
      ),
      relocked,
    );
    b.replaces = keyVersion;
    return b;
  }

  /// What a device does when no way of unlocking is left (section 13.8):
  /// new keys and phrase with the next key version, and a list marked
  /// reset holding only itself.
  Future<Bundle> resetBackup() async {
    final cur = pins[account]?.newestList;
    if (cur == null) throw const SealedError("the device has not pinned its own account's list",);
    final newBackupKey = _random('backup_key', 32);
    final newAccountKey = await SigningKey.fromSeed(
      _random('account_key_seed', 32),
    );
    final entropy = _random('phrase_entropy', 16);
    final rec = AccountRecord(
      accountId: account,
      keyVersion: cur.list.keyVersion + 1,
      revision: 1,
      accountKey: newAccountKey.public,
      accountKeyPrivate: newAccountKey.seed,
      ways: [_phraseWay(entropy)],
      at: now(),
    );
    final b = await _bundle(
      newBackupKey,
      newAccountKey,
      rec,
      phraseOf(entropy),
      _ListPlan(
        cur.list.version + 1,
        [await _entry()],
        prev: listHash(cur.signed.list),
        reset: true,
      ),
      const [],
    );
    b.replaces = cur.list.keyVersion;
    // Nothing the device held from before the reset is of use.
    for (final c in chats.values) {
      for (final e in c.epochs) {
        e.history = null;
        e.secret = null;
      }
    }
    return b;
  }

  /// The device's state for a sealed chat, made if needed.
  Chat chat(Uuid conv, Uuid other) =>
      chats.putIfAbsent(conv, () => Chat._(this, conv, other));
}
