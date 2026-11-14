// Key epochs (docs/SEALED.md section 5): the record that announces one,
// what is derived from its secret, and the checks a device makes before it
// accepts one. Also the call keys of section 10, which come from the epoch
// secret.

import 'dart:typed_data';

import 'device_list.dart';
import 'enc.dart';
import 'errors.dart';
import 'prim.dart';
import 'seal.dart';
import 'sign.dart';

/// Which device list of an account the maker used.
class AccountRef {
  final Uuid accountId;
  final int listVersion; // 0 if the account has no list yet
  Uint8List listHash; // zero bytes if listVersion is 0
  AccountRef(this.accountId, this.listVersion, [Uint8List? hash])
    : listHash = hash ?? Uint8List(32);
}

/// One device the epoch secret is sealed to.
class Recipient {
  Uuid accountId;
  final Uuid deviceId;
  final Uint8List sealed; // 80 bytes
  Recipient(this.accountId, this.deviceId, this.sealed);
}

/// The 64 bytes after an epoch record, and after a sealed message's content.
const signatureSize = 64;

/// The record of section 5.1.
class EpochRecord {
  int format;
  Uuid conversationId;
  int keyEpoch;
  Uint8List prev;
  Uuid makerAccountId;
  Uuid makerDeviceId;
  List<AccountRef> accounts; // exactly two, sorted by account_id
  Uint8List check;
  List<Recipient> recipients; // sorted by device_id
  int at;

  EpochRecord({
    this.format = 1,
    required this.conversationId,
    required this.keyEpoch,
    Uint8List? prev,
    required this.makerAccountId,
    required this.makerDeviceId,
    required this.accounts,
    Uint8List? check,
    required this.recipients,
    required this.at,
  }) : prev = prev ?? Uint8List(32),
       check = check ?? Uint8List(32);

  static const _accountRefSize = 16 + 8 + 32;
  static const _recipientSize = 16 + 16 + 80;

  /// Writes the record, without the maker's signature.
  Uint8List encode() {
    if (accounts.length != 2) throw RecordError.count;
    final w = Writer()
      ..u8(format)
      ..uuid(conversationId)
      ..u64(keyEpoch)
      ..raw(prev)
      ..uuid(makerAccountId)
      ..uuid(makerDeviceId)
      ..u32(accounts.length);
    for (var i = 0; i < accounts.length; i++) {
      final a = accounts[i];
      if (i > 0 && accounts[i - 1].accountId.compareTo(a.accountId) >= 0) throw RecordError.order;
      w
        ..uuid(a.accountId)
        ..u64(a.listVersion)
        ..raw(a.listHash);
    }
    w
      ..raw(check)
      ..u32(recipients.length);
    for (var i = 0; i < recipients.length; i++) {
      final r = recipients[i];
      if (r.sealed.length != 80) throw ArgumentError('a sealed epoch secret is 80 bytes');
      if (i > 0 && recipients[i - 1].deviceId.compareTo(r.deviceId) >= 0) throw RecordError.order;
      w
        ..uuid(r.accountId)
        ..uuid(r.deviceId)
        ..raw(r.sealed);
    }
    w.time(at);
    return w.bytes();
  }

  /// Reads a record without its signature.
  static EpochRecord decode(List<int> b) {
    final r = Reader(b);
    final format = r.choice(const [1]);
    final conv = r.uuid();
    final keyEpoch = r.u64();
    final prev = r.raw(32);
    final makerAccount = r.uuid();
    final makerDevice = r.uuid();
    var n = r.count(_accountRefSize);
    if (n != 2) r.fail(RecordError.count);
    final accounts = <AccountRef>[];
    for (var i = 0; i < n && r.err == null; i++) {
      final a = AccountRef(r.uuid(), r.u64(), r.raw(32));
      if (i > 0 && accounts[i - 1].accountId.compareTo(a.accountId) >= 0) r.fail(RecordError.order);
      accounts.add(a);
    }
    final check = r.raw(32);
    n = r.count(_recipientSize);
    final recipients = <Recipient>[];
    for (var i = 0; i < n && r.err == null; i++) {
      final rc = Recipient(r.uuid(), r.uuid(), r.raw(80));
      if (i > 0 && recipients[i - 1].deviceId.compareTo(rc.deviceId) >= 0) r.fail(RecordError.order);
      recipients.add(rc);
    }
    final at = r.u64();
    r.done();
    return EpochRecord(
      format: format,
      conversationId: conv,
      keyEpoch: keyEpoch,
      prev: prev,
      makerAccountId: makerAccount,
      makerDeviceId: makerDevice,
      accounts: accounts,
      check: check,
      recipients: recipients,
      at: at,
    );
  }

  Recipient? recipient(Uuid device) {
    for (final r in recipients) {
      if (r.deviceId == device) return r;
    }
    return null;
  }

  /// Whether any recipient belongs to [account].
  bool hasAccount(Uuid account) =>
      recipients.any((r) => r.accountId == account);

  AccountRef? ref(Uuid account) {
    for (final a in accounts) {
      if (a.accountId == account) return a;
    }
    return null;
  }
}

/// Splits the stored form into the record and the maker's signature.
(Uint8List, Uint8List) splitStoredEpoch(Uint8List stored) {
  if (stored.length > maxRecord + signatureSize) throw RecordError.tooLong;
  if (stored.length < signatureSize) throw RecordError.short;
  final n = stored.length - signatureSize;
  return (
    Uint8List.sublistView(stored, 0, n),
    Uint8List.sublistView(stored, n),
  );
}

/// The hash of an epoch's stored bytes, signature included.
Uint8List epochHash(List<int> stored) => hashOf(labelEpochHash, stored);

/// conversation_id || key_epoch: the salt of every derivation of 5.2.
Uint8List epochSalt(Uuid conv, int keyEpoch) =>
    cat([conv.bytes, u64be(keyEpoch)]);

Uint8List historySecret(List<int> epochSecret, Uuid conv, int keyEpoch) =>
    _derive(epochSecret, conv, keyEpoch, labelHistory);

Uint8List _derive(
  List<int> key,
  Uuid conv,
  int keyEpoch,
  String label, [
  List<int>? id,
]) => hkdf(
  key,
  epochSalt(conv, keyEpoch),
  id == null ? Uint8List.fromList(label.codeUnits) : labelled(label, [id]),
  32,
);

Uint8List contentKey(List<int> history, Uuid conv, int keyEpoch) =>
    _derive(history, conv, keyEpoch, labelContent);

Uint8List epochCheck(List<int> history, Uuid conv, int keyEpoch) =>
    _derive(history, conv, keyEpoch, labelEpochCheck);

Uint8List senderKey(
  List<int> contentKey,
  Uuid conv,
  int keyEpoch,
  Uuid device,
) => _derive(contentKey, conv, keyEpoch, labelSender, device.bytes);

Uint8List callSecret(
  List<int> epochSecret,
  Uuid conv,
  int keyEpoch,
  Uuid call,
) => _derive(epochSecret, conv, keyEpoch, labelCall, call.bytes);

Uint8List callSenderKey(
  List<int> callSecret,
  Uuid conv,
  int keyEpoch,
  Uuid device,
) => _derive(callSecret, conv, keyEpoch, labelCallSender, device.bytes);

/// The key index carried in each call frame.
int callKeyIndex(int keyEpoch) => keyEpoch % 16;

/// conversation_id || key_epoch || device_id.
Uint8List epochSealContext(Uuid conv, int keyEpoch, Uuid device) =>
    cat([epochSalt(conv, keyEpoch), device.bytes]);

/// One account's list for [makeEpoch]: the list record to seal to, or null
/// when the account has no list yet.
class EpochList {
  final Uuid accountId;
  final Uint8List? list;
  EpochList(this.accountId, this.list);
}

/// Builds and signs an epoch record (section 5.3) and returns its stored
/// form. The device set is not chosen: it is every device in the two lists.
Future<Uint8List> makeEpoch({
  required Uuid conversationId,
  required int keyEpoch,
  Uint8List? prev,
  required Uuid makerAccountId,
  required Uuid makerDeviceId,
  required SigningKey makerSign,
  required List<EpochList> lists,
  required Uint8List secret,
  required Uint8List Function(Uuid device) oneTime,
  required int at,
}) async {
  if (secret.length != 32) throw ArgumentError('an epoch secret is 32 bytes');
  final accounts = <AccountRef>[];
  final recipients = <Recipient>[];
  for (final input in lists) {
    var ref = AccountRef(input.accountId, 0);
    if (input.list != null) {
      final l = DeviceList.decode(input.list!);
      if (l.accountId != input.accountId) throw ArgumentError('a list is not that account\'s');
      ref = AccountRef(input.accountId, l.version, listHash(input.list!));
      for (final d in l.devices) {
        final sealed = seal(
          d.boxKey,
          labelSealEpoch,
          epochSealContext(conversationId, keyEpoch, d.deviceId),
          secret,
          oneTime(d.deviceId),
        );
        recipients.add(Recipient(input.accountId, d.deviceId, sealed));
      }
    }
    accounts.add(ref);
  }
  accounts.sort((a, b) => a.accountId.compareTo(b.accountId));
  _stableSort(recipients, (a, b) => a.deviceId.compareTo(b.deviceId));
  final e = EpochRecord(
    conversationId: conversationId,
    keyEpoch: keyEpoch,
    prev: prev,
    makerAccountId: makerAccountId,
    makerDeviceId: makerDeviceId,
    accounts: accounts,
    check: epochCheck(
      historySecret(secret, conversationId, keyEpoch),
      conversationId,
      keyEpoch,
    ),
    recipients: recipients,
    at: at,
  );
  final record = e.encode();
  return cat([record, await sign(makerSign, labelEpoch, record)]);
}

void _stableSort<T>(List<T> l, int Function(T, T) cmp) {
  final indexed = [for (var i = 0; i < l.length; i++) (i, l[i])];
  indexed.sort((a, b) {
    final c = cmp(a.$2, b.$2);
    return c != 0 ? c : a.$1 - b.$1;
  });
  for (var i = 0; i < l.length; i++) {
    l[i] = indexed[i].$2;
  }
}

/// An epoch record that passed, and what the device got from it.
class AcceptedEpoch {
  final EpochRecord record;

  /// Each recipient device's entry in the list version the record names:
  /// where the signing keys for the epoch's messages come from.
  final Map<Uuid, ListDevice> devices;

  /// The epoch secret, if the checking device is a recipient.
  final Uint8List? secret;
  AcceptedEpoch(this.record, this.devices, this.secret);
}

/// Applies the seven rules of section 5.4.
///
/// [previous] is the stored record before this one, accepted or not; null
/// for the oldest record the device was given. [listOf] returns the record
/// of a list version the device has checked, or null. [box] may be null for
/// a device that only checks the chain.
Future<AcceptedEpoch> checkEpochRecord({
  required Uint8List stored,
  required Uuid conversationId,
  required List<Uuid> accounts,
  Uint8List? previous,
  required Uint8List? Function(Uuid account, int version) listOf,
  required Uuid deviceId,
  Uint8List? box,
}) async {
  // 1. It parses, with nothing left over, and is this chat's.
  final Uint8List record, sig;
  final EpochRecord e;
  try {
    (record, sig) = splitStoredEpoch(stored);
    e = EpochRecord.decode(record);
  } on RecordError catch (err) {
    throw EpochError(1, err.message);
  }
  if (e.conversationId != conversationId) throw const EpochError(1, 'the record is for another chat');
  // 2. It is the next number and names the hash of the one before.
  EpochRecord? before;
  if (previous != null) {
    try {
      before = EpochRecord.decode(splitStoredEpoch(previous).$1);
    } on RecordError {
      before = null;
    }
    if (before != null && e.keyEpoch != before.keyEpoch + 1) {
      throw const EpochError(
        2,
        "the key epoch is not one above the previous record's",
      );
    }
    if (!bytesEqual(e.prev, epochHash(previous))) {
      throw const EpochError(2, 'prev is not the hash of the previous record');
    }
  } else if (e.keyEpoch == 0) {
    throw const EpochError(2, "a chat's first key epoch is 1");
  } else if (e.keyEpoch == 1 && !e.prev.every((b) => b == 0)) {
    throw const EpochError(2, 'epoch 1 has a previous hash');
  }
  // 3. It names exactly the chat's two accounts.
  final want = [...accounts]..sort();
  if (e.accounts[0].accountId != want[0] ||
      e.accounts[1].accountId != want[1]) {
    throw const EpochError(
      3,
      "the record does not name the chat's two accounts",
    );
  }
  // 4. The lists it names are checked, with those hashes, never older.
  final devices = <Uuid, ListDevice>{};
  final owner = <Uuid, Uuid>{};
  for (final a in e.accounts) {
    if (before != null) {
      final was = before.ref(a.accountId);
      if (was != null && a.listVersion < was.listVersion) {
        throw const EpochError(4, 'a device list went backwards');
      }
    }
    if (a.listVersion == 0) {
      if (!a.listHash.every((b) => b == 0)) throw const EpochError(4, 'an account with no list has a list hash');
      continue;
    }
    final raw = listOf(a.accountId, a.listVersion);
    if (raw == null) throw const EpochError(4, 'the device has not checked a device list the record names',);
    if (!bytesEqual(listHash(raw), a.listHash)) {
      throw const EpochError(4, 'a list hash is not the hash of that version');
    }
    final DeviceList l;
    try {
      l = DeviceList.decode(raw);
    } on RecordError {
      throw const EpochError(4, 'a device list does not parse');
    }
    for (final d in l.devices) {
      devices[d.deviceId] = d;
      owner[d.deviceId] = a.accountId;
    }
  }
  // 5. The recipients are exactly those lists' devices, each under its
  // right account.
  if (e.recipients.length != devices.length) {
    throw const EpochError(
      5,
      'the recipients are not exactly the devices of the two lists',
    );
  }
  for (final r in e.recipients) {
    if (owner[r.deviceId] != r.accountId) {
      throw const EpochError(
        5,
        'the recipients are not exactly the devices of the two lists',
      );
    }
  }
  // 6. The maker is a recipient, and signed.
  final maker = devices[e.makerDeviceId];
  if (maker == null || owner[e.makerDeviceId] != e.makerAccountId) {
    throw const EpochError(6, 'the maker is not one of the recipients');
  }
  if (!await verify(maker.signKey, labelEpoch, record, sig)) {
    throw const EpochError(6, "the maker's signature does not verify");
  }
  // 7. If the device is a recipient, its sealed value opens to the secret
  // the record announces.
  Uint8List? secret;
  final mine = e.recipient(deviceId);
  if (mine != null && box != null) {
    try {
      secret = openSealed(
        box,
        labelSealEpoch,
        epochSealContext(e.conversationId, e.keyEpoch, deviceId),
        mine.sealed,
      );
    } on OpenError {
      throw const EpochError(7, 'the sealed epoch secret does not open');
    }
    if (secret.length != 32) throw const EpochError(7, 'the sealed epoch secret does not open');
    if (!bytesEqual(
      epochCheck(
        historySecret(secret, e.conversationId, e.keyEpoch),
        e.conversationId,
        e.keyEpoch,
      ),
      e.check,
    )) {
      throw const EpochError(
        7,
        'the secret is not the one the record announces',
      );
    }
  }
  return AcceptedEpoch(e, devices, secret);
}

/// Whether a history secret from a backup or from the other person is the
/// one an epoch record announced.
bool checkHistorySecret(List<int>? history, EpochRecord e) =>
    history != null &&
    history.length == 32 &&
    bytesEqual(epochCheck(history, e.conversationId, e.keyEpoch), e.check);
