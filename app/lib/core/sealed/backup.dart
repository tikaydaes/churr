// The key backup (docs/SEALED.md section 13): the account record, backup
// records, locked copies, the unlock keys they are sealed to, and the
// hand-overs to an app.

import 'dart:convert';
import 'dart:typed_data';

import 'enc.dart';
import 'errors.dart';
import 'prim.dart';
import 'seal.dart';
import 'sign.dart';

const wayPasskey = 1;
const wayPhrase = 2;
const wayDevice = 3;

/// The API's word for each kind of locked copy.
const wayNames = {1: 'passkey', 2: 'phrase', 3: 'device'};

/// One public unlock key the backup key may be locked to.
class Way {
  final int kind; // 1 passkey, 2 phrase
  final Uuid id; // the passkey; zero for the phrase
  final Uint8List unlockPublic;
  Way(this.kind, this.id, this.unlockPublic);
  Uint8List get sortKey => cat([
    [kind],
    id.bytes,
  ]);
}

void sortWays(List<Way> ways) =>
    ways.sort((a, b) => compareBytes(a.sortKey, b.sortKey));

/// The content of the account record of section 13.1.
class AccountRecord {
  int format;
  Uuid accountId;
  int keyVersion;
  int revision;
  Uint8List accountKey; // Ed25519 public key
  Uint8List accountKeyPrivate; // its seed
  List<Way> ways; // sorted by kind, then id
  int at;

  AccountRecord({
    this.format = 1,
    required this.accountId,
    required this.keyVersion,
    required this.revision,
    required this.accountKey,
    required this.accountKeyPrivate,
    required this.ways,
    required this.at,
  });

  AccountRecord copy() => AccountRecord(
    format: format,
    accountId: accountId,
    keyVersion: keyVersion,
    revision: revision,
    accountKey: accountKey,
    accountKeyPrivate: accountKeyPrivate,
    ways: [...ways],
    at: at,
  );

  Uint8List encode() {
    if (accountKey.length != 32 || accountKeyPrivate.length != 32) {
      throw ArgumentError('an account key and its seed are 32 bytes each');
    }
    final w = Writer()
      ..u8(format)
      ..uuid(accountId)
      ..u32(keyVersion)
      ..u64(revision)
      ..raw(accountKey)
      ..raw(accountKeyPrivate)
      ..u32(ways.length);
    for (var i = 0; i < ways.length; i++) {
      final way = ways[i];
      if (way.unlockPublic.length != 32) throw ArgumentError('an unlock public key is 32 bytes');
      if (i > 0 && compareBytes(ways[i - 1].sortKey, way.sortKey) >= 0) throw RecordError.order;
      w
        ..u8(way.kind)
        ..uuid(way.id)
        ..raw(way.unlockPublic);
    }
    w.time(at);
    return w.bytes();
  }

  static AccountRecord decode(List<int> b) {
    final r = Reader(b);
    final format = r.choice(const [1]);
    final account = r.uuid();
    final keyVersion = r.u32();
    final revision = r.u64();
    final key = r.raw(32);
    final priv = r.raw(32);
    final n = r.count(1 + 16 + 32);
    final ways = <Way>[];
    for (var i = 0; i < n && r.err == null; i++) {
      final w = Way(
        r.choice(const [wayPasskey, wayPhrase]),
        r.uuid(),
        r.raw(32),
      );
      if (i > 0 && compareBytes(ways[i - 1].sortKey, w.sortKey) >= 0) r.fail(RecordError.order);
      ways.add(w);
    }
    final at = r.u64();
    r.done();
    return AccountRecord(
      format: format,
      accountId: account,
      keyVersion: keyVersion,
      revision: revision,
      accountKey: key,
      accountKeyPrivate: priv,
      ways: ways,
      at: at,
    );
  }
}

/// account_id || key_version || revision.
Uint8List accountRecordContext(Uuid account, int keyVersion, int revision) =>
    cat([account.bytes, u32be(keyVersion), u64be(revision)]);

/// The stored form in account_records.record.
Uint8List lockAccountRecord(
  List<int> backupKey,
  AccountRecord a,
  List<int> salt,
) => lock(
  backupKey,
  labelLockAccountRecord,
  accountRecordContext(a.accountId, a.keyVersion, a.revision),
  a.encode(),
  salt,
);

/// Opens a stored account record filed under this key version and
/// revision; the record must say the same, and its private half must be the
/// seed of its public half.
Future<AccountRecord> openAccountRecord(
  List<int> backupKey,
  Uuid account,
  int keyVersion,
  int revision,
  List<int> stored,
) async {
  final content = unlockLocked(
    backupKey,
    labelLockAccountRecord,
    accountRecordContext(account, keyVersion, revision),
    stored,
  );
  final a = AccountRecord.decode(content);
  if (a.accountId != account ||
      a.keyVersion != keyVersion ||
      a.revision != revision) {
    throw const SealedError(
      'the account record is not the one it was filed as',
    );
  }
  final k = await SigningKey.fromSeed(a.accountKeyPrivate);
  if (!bytesEqual(k.public, a.accountKey)) {
    throw const SealedError(
      "the account record's two key halves do not belong together",
    );
  }
  return a;
}

/// The content of a backup record of section 13.2.
class BackupRecord {
  final int format;
  final Uuid conversationId;
  final int keyEpoch;
  final Uint8List secret; // the history secret
  final int at;
  BackupRecord({
    this.format = 1,
    required this.conversationId,
    required this.keyEpoch,
    required this.secret,
    required this.at,
  });

  Uint8List encode() {
    if (secret.length != 32) throw ArgumentError('a history secret is 32 bytes');
    return (Writer()
          ..u8(format)
          ..uuid(conversationId)
          ..u64(keyEpoch)
          ..raw(secret)
          ..time(at))
        .bytes();
  }

  static BackupRecord decode(List<int> b) {
    final r = Reader(b);
    final rec = BackupRecord(
      format: r.choice(const [1]),
      conversationId: r.uuid(),
      keyEpoch: r.u64(),
      secret: r.raw(32),
      at: r.u64(),
    );
    r.done();
    return rec;
  }
}

/// account_id || conversation_id || key_epoch || key_version.
Uint8List backupRecordContext(
  Uuid account,
  Uuid conv,
  int keyEpoch,
  int keyVersion,
) => cat([account.bytes, conv.bytes, u64be(keyEpoch), u32be(keyVersion)]);

/// The stored form in key_backups.record: 97 bytes.
Uint8List lockBackupRecord(
  List<int> backupKey,
  Uuid account,
  int keyVersion,
  BackupRecord b,
  List<int> salt,
) => lock(
  backupKey,
  labelLockBackupRecord,
  backupRecordContext(account, b.conversationId, b.keyEpoch, keyVersion),
  b.encode(),
  salt,
);

/// Opens a stored backup record for the row it was filed in. The caller
/// then checks the secret against the epoch record's `check`.
BackupRecord openBackupRecord(
  List<int> backupKey,
  Uuid account,
  Uuid conv,
  int keyEpoch,
  int keyVersion,
  List<int> stored,
) {
  final content = unlockLocked(
    backupKey,
    labelLockBackupRecord,
    backupRecordContext(account, conv, keyEpoch, keyVersion),
    stored,
  );
  final b = BackupRecord.decode(content);
  if (b.conversationId != conv || b.keyEpoch != keyEpoch) {
    throw const SealedError('the backup record is not the one it was filed as');
  }
  return b;
}

/// The stored size of a locked copy: 80 sealed bytes and a signature.
const lockedCopySize = 80 + 64;

/// kind (u8) || id (uuid).
Uint8List wayTag(int kind, Uuid id) => cat([
  [kind],
  id.bytes,
]);

/// account_id || key_version || way tag.
Uint8List lockedCopyContext(Uuid account, int keyVersion, int kind, Uuid id) =>
    cat([account.bytes, u32be(keyVersion), wayTag(kind, id)]);

/// Seals the backup key to an unlock key's public half and signs the result
/// with the account key.
Future<Uint8List> makeLockedCopy(
  List<int> unlockPublic,
  Uuid account,
  int keyVersion,
  int kind,
  Uuid id,
  List<int> backupKey,
  SigningKey accountKey,
  List<int> oneTime,
) async {
  final ctx = lockedCopyContext(account, keyVersion, kind, id);
  final sealed = seal(
    unlockPublic,
    labelSealBackupKey,
    ctx,
    backupKey,
    oneTime,
  );
  return cat([
    sealed,
    await sign(accountKey, labelLockedCopy, cat([ctx, sealed])),
  ]);
}

/// Opens a locked copy. It is not trusted yet: the caller opens the
/// account record with the key and calls [verifyLockedCopy].
Uint8List openLockedCopy(
  List<int> unlockPrivate,
  Uuid account,
  int keyVersion,
  int kind,
  Uuid id,
  List<int> stored,
) {
  if (stored.length != lockedCopySize) throw const OpenError();
  final key = openSealed(
    unlockPrivate,
    labelSealBackupKey,
    lockedCopyContext(account, keyVersion, kind, id),
    stored.sublist(0, 80),
  );
  if (key.length != 32) throw const OpenError();
  return key;
}

/// Checks a locked copy's signature under the account key.
Future<bool> verifyLockedCopy(
  List<int> accountKey,
  Uuid account,
  int keyVersion,
  int kind,
  Uuid id,
  List<int> stored,
) async {
  if (stored.length != lockedCopySize) return false;
  return verify(
    accountKey,
    labelLockedCopy,
    cat([
      lockedCopyContext(account, keyVersion, kind, id),
      stored.sublist(0, 80),
    ]),
    stored.sublist(80),
  );
}

/// The first evaluation input of the WebAuthn PRF extension.
Uint8List prfInput() => sha256(ascii.encode(labelSealedUnlock));

/// A passkey's unlock private key from its PRF output.
Uint8List unlockKeyFromPrf(
  List<int> prfOutput,
  Uuid account,
  List<int> credentialId,
) => hkdf(
  prfOutput,
  Uint8List(32),
  labelled(labelUnlockPasskey, [account.bytes, credentialId]),
  32,
);

/// The phrase's unlock private key from the 16 bytes the words stand for.
Uint8List unlockKeyFromPhrase(List<int> entropy, Uuid account) => hkdf(
  entropy,
  Uint8List(32),
  labelled(labelUnlockPhrase, [account.bytes]),
  32,
);

Uint8List _handoverContext(Uuid account, String codeChallenge) =>
    cat([account.bytes, ascii.encode(codeChallenge)]);

/// What the sign-in page makes for an app: the backup key sealed to the
/// app's box key, bound to the account and the PKCE code challenge.
Uint8List sealHandover(
  List<int> appBoxKey,
  Uuid account,
  String codeChallenge,
  List<int> backupKey,
  List<int> oneTime,
) => seal(
  appBoxKey,
  labelSealHandover,
  _handoverContext(account, codeChallenge),
  backupKey,
  oneTime,
);

Uint8List openHandover(
  List<int> appBox,
  Uuid account,
  String codeChallenge,
  List<int> handover,
) {
  final key = openSealed(
    appBox,
    labelSealHandover,
    _handoverContext(account, codeChallenge),
    handover,
  );
  if (key.length != 32) throw const OpenError();
  return key;
}

/// The two-layer hand-over of QR pairing: 112 bytes.
Uint8List sealPairingHandover(
  List<int> appBoxKey,
  List<int> pairingSecret,
  Uuid account,
  List<int> backupKey,
  List<int> oneTime,
  List<int> salt,
) {
  final inner = seal(
    appBoxKey,
    labelSealPairing,
    account.bytes,
    backupKey,
    oneTime,
  );
  return lock(pairingSecret, labelLockPairing, account.bytes, inner, salt);
}

Uint8List openPairingHandover(
  List<int> appBox,
  List<int> pairingSecret,
  Uuid account,
  List<int> handover,
) {
  final inner = unlockLocked(
    pairingSecret,
    labelLockPairing,
    account.bytes,
    handover,
  );
  final key = openSealed(appBox, labelSealPairing, account.bytes, inner);
  if (key.length != 32) throw const OpenError();
  return key;
}
