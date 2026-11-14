// The sealed-chat operations (docs/openapi.yaml, tag "Sealed chats", and
// the few device and passkey operations they need), typed for the glue in
// session.dart. Bytes travel as base64url without padding; the records
// themselves are the library's (lib/core/sealed) and are passed through as
// bytes.

import 'dart:typed_data';


import '../api/client.dart';
import '../api/errors.dart';
import '../api/operations.dart';
import '../models/json.dart';
import '../models/message.dart';
import '../sealed/sealed.dart';

/// One `EpochRecord` as the server files it.
class EpochRow {
  EpochRow(this.raw);
  final Json raw;
  int get keyEpoch => asInt(raw['key_epoch']);
  int get seq => asInt(raw['seq']);
  String get conversationId => raw['conversation_id'] as String? ?? '';
  Uint8List get record => unb64(raw['record'] as String?) ?? Uint8List(0);
}

/// One locked copy as `getBackup` lists it.
class LockedCopyRow {
  LockedCopyRow(this.raw);
  final Json raw;
  String get id => raw['id'] as String? ?? '';
  String get kind => raw['kind'] as String? ?? '';
  String? get passkeyId => raw['passkey_id'] as String?;
  String? get deviceId => raw['device_id'] as String?;
  int get keyVersion => asInt(raw['key_version']);
  Uint8List get locked => unb64(raw['locked_key'] as String?) ?? Uint8List(0);
}

/// `Backup`: what a complete session is given to unlock with.
class BackupRow {
  BackupRow(this.raw);
  final Json raw;
  int get keyVersion => asInt(raw['key_version']);
  bool get replacementDue => raw['replacement_due'] == true;
  Json get _record => object(raw['account_record']) ?? const {};
  int get revision => asInt(_record['revision']);
  Uint8List get accountRecord =>
      unb64(_record['record'] as String?) ?? Uint8List(0);
  List<LockedCopyRow> get copies =>
      listOf(raw['locked_copies'], LockedCopyRow.new);
  StoredBackup get stored =>
      StoredBackup(asInt(_record['key_version']), revision, accountRecord);

  LockedCopyRow? copyFor(String kind, {String? passkeyId, String? deviceId}) {
    for (final c in copies) {
      if (c.kind != kind) continue;
      if (passkeyId != null && c.passkeyId != passkeyId) continue;
      if (deviceId != null && c.deviceId != deviceId) continue;
      return c;
    }
    return null;
  }
}

/// `Passkey` as `listPasskeys` gives it.
class PasskeyRow {
  PasskeyRow(this.raw);
  final Json raw;
  String get id => raw['id'] as String? ?? '';
  String get name => raw['name'] as String? ?? '';
  bool get hasPrf => raw['has_prf'] == true;
  bool get canUnlock => raw['can_unlock'] == true;
  Uint8List? get unlockPublic => unb64(raw['unlock_public'] as String?);
  DateTime? get lastUsedAt => parseTime(raw['last_used_at']);
}

/// `UnlockRequest`.
class UnlockRequestRow {
  UnlockRequestRow(this.raw);
  final Json raw;
  String get id => raw['id'] as String? ?? '';
  String get deviceId => raw['device_id'] as String? ?? '';
  Json? get device => object(raw['device']);
  String get deviceName => device?['name'] as String? ?? '';
  String get platform => device?['platform'] as String? ?? '';
  Uint8List get request => unb64(raw['request'] as String?) ?? Uint8List(0);
  Uint8List? get answer => unb64(raw['answer'] as String?);
  DateTime? get createdAt => parseTime(raw['created_at']);
  DateTime? get expiresAt => parseTime(raw['expires_at']);
}

/// `Device`, with the keys sealed chats need.
class SealedDeviceRow {
  SealedDeviceRow(this.raw);
  final Json raw;
  String get id => raw['id'] as String? ?? '';
  String get name => raw['name'] as String? ?? '';
  String get platform => raw['platform'] as String? ?? '';
  bool get current => raw['current'] == true;
  bool get retired => raw['retired_at'] != null;
  Uint8List? get signKey => unb64(raw['sign_key'] as String?);
  Uint8List? get boxKey => unb64(raw['box_key'] as String?);
  DateTime? get createdAt => parseTime(raw['created_at']);
  DateTime? get lastSeenAt => parseTime(raw['last_seen_at']);
}

Json _list(SignedList s, int version) => {
  'version': version,
  'list': b64(s.list),
  'sig': b64(s.sig),
  if (s.prevSig != null) 'prev_sig': b64(s.prevSig!),
};

const _kindNames = {
  wayPasskey: 'passkey',
  wayPhrase: 'phrase',
  wayDevice: 'device',
};

Json _copy(Copy c, int keyVersion) => {
  'kind': _kindNames[c.kind],
  if (c.kind == wayPasskey) 'passkey_id': '${c.id}',
  if (c.kind == wayDevice) 'device_id': '${c.id}',
  'key_version': keyVersion,
  'locked_key': b64(c.locked),
};

Json _record(Bundle b) => {
  'key_version': b.keyVersion,
  'revision': b.revision,
  'record': b64(b.accountRecord),
};

Json _stored(StoredRecord r) => {
  'conversation_id': '${r.conversationId}',
  'key_epoch': r.keyEpoch,
  'key_version': r.keyVersion,
  'record': b64(r.locked),
};

/// The platform names of docs/SEALED.md section 4.3 as the API spells
/// them.
const sealedPlatformNames = {
  1: 'android',
  2: 'ios',
  3: 'macos',
  4: 'windows',
  5: 'linux',
  6: 'web',
};

extension SealedApi on ApiClient {
  /// Registers this session's device with its two public keys. Returns
  /// the device's row; its id is the device id the lists use.
  Future<SealedDeviceRow> registerSealedDevice({
    required String platform,
    required Uint8List signKey,
    required Uint8List boxKey,
    String? name,
  }) async {
    final j = await call(
      Ops.registerDevice,
      body: {
        'name': ?name,
        'platform': platform,
        'sign_key': b64(signKey),
        'box_key': b64(boxKey),
      },
    );
    return SealedDeviceRow(object(j['device']) ?? const {});
  }

  Future<List<SealedDeviceRow>> listSealedDevices() async =>
      listOf((await call(Ops.listDevices))['devices'], SealedDeviceRow.new);

  Future<void> removeSealedDevice(String id) =>
      call(Ops.removeDevice, path: {'id': id});

  /// Every version of [account]'s list above [after]; with null, the
  /// newest only.
  Future<List<SignedList>> deviceLists(String account, {int? after}) async {
    final j = await call(
      Ops.listDeviceListVersions,
      path: {'id': account},
      query: {'after': after},
    );
    return [
      for (final v in listOf(j['versions'], (x) => x))
        SignedList(
          unb64(v['list'] as String?)!,
          unb64(v['sig'] as String?)!,
          unb64(v['prev_sig'] as String?),
        ),
    ];
  }

  Future<void> publishDeviceList(SignedList s) async {
    final l = DeviceList.decode(s.list);
    await call(Ops.publishDeviceList, body: _list(s, l.version));
  }

  /// Starts (or finds) the sealed chat with [account]; returns the
  /// conversation's JSON and whether it was made now.
  Future<(Json, bool)> openSealedChat(String account, Uint8List epoch1) async {
    final j = await call(
      Ops.openSealedChat,
      body: {'account_id': account, 'epoch_record': b64(epoch1)},
    );
    final conv = object(j['conversation']) ?? const {};
    return (conv, conv['id'] != null);
  }

  Future<EpochRow> postEpochRecord(
    String conversationId,
    int keyEpoch,
    Uint8List record,
  ) async {
    final j = await call(
      Ops.postEpochRecord,
      path: {'id': conversationId},
      body: {'key_epoch': keyEpoch, 'record': b64(record)},
    );
    return EpochRow(object(j['epoch']) ?? const {});
  }

  /// Every record from [from] on, all pages.
  Future<List<EpochRow>> epochRecords(
    String conversationId, {
    int from = 1,
  }) async {
    final out = <EpochRow>[];
    var next = from;
    while (true) {
      final j = await call(
        Ops.listEpochRecords,
        path: {'id': conversationId},
        query: {'from': next, 'limit': 200},
      );
      final page = listOf(j['epochs'], EpochRow.new);
      out.addAll(page);
      if (j['has_more'] != true || page.isEmpty) return out;
      next = page.last.keyEpoch + 1;
    }
  }

  /// The backup, or null when the account has none yet.
  Future<BackupRow?> backup() async {
    try {
      return BackupRow(
        object((await call(Ops.getBackup))['backup']) ?? const {},
      );
    } on ApiException catch (e) {
      if (e.code == 'not_found') return null;
      rethrow;
    }
  }

  Future<BackupRow> createBackup(Bundle b) async => BackupRow(
    object(
          (await call(
            Ops.createBackup,
            body: {
              'account_record': _record(b),
              'locked_copies': [
                for (final c in b.copies) _copy(c, b.keyVersion),
              ],
              'device_list': _list(b.list, b.listVersion),
            },
          ))['backup'],
        ) ??
        const {},
  );

  Future<BackupRow> replaceBackup(Bundle b) async => BackupRow(
    object(
          (await call(
            Ops.replaceBackup,
            body: {
              'replaces_key_version': b.replaces,
              'account_record': _record(b),
              'locked_copies': [
                for (final c in b.copies) _copy(c, b.keyVersion),
              ],
              'records': [for (final r in b.records) _stored(r)],
              'device_list': _list(b.list, b.listVersion),
            },
          ))['backup'],
        ) ??
        const {},
  );

  Future<BackupRow> resetBackup(Bundle b) async => BackupRow(
    object(
          (await call(
            Ops.resetBackup,
            body: {
              'account_record': _record(b),
              'locked_copies': [
                for (final c in b.copies) _copy(c, b.keyVersion),
              ],
              'device_list': _list(b.list, b.listVersion),
            },
          ))['backup'],
        ) ??
        const {},
  );

  /// Stores one locked copy, with the account record when the ways
  /// changed.
  Future<void> addLockedCopy(Copy c, int keyVersion, {RecordUpdate? record}) =>
      call(
        Ops.addLockedCopy,
        body: {
          'locked_copy': _copy(c, keyVersion),
          if (record != null)
            'account_record': {
              'key_version': keyVersion,
              'revision': record.revision,
              'record': b64(record.accountRecord),
            },
        },
      );

  Future<void> deleteLockedCopy(String id) =>
      call(Ops.deleteLockedCopy, path: {'id': id});

  Future<List<StoredRecord>> backupRecords({String? conversationId}) =>
      allPages(
        Ops.listBackupRecords,
        'records',
        (j) => StoredRecord(
          Uuid.parse(j['conversation_id'] as String),
          asInt(j['key_epoch']),
          asInt(j['key_version']),
          unb64(j['record'] as String?)!,
        ),
        query: {'conversation_id': conversationId},
      );

  Future<void> putBackupRecord(StoredRecord r) => call(
    Ops.putBackupRecord,
    path: {
      'conversation_id': '${r.conversationId}',
      'key_epoch': '${r.keyEpoch}',
    },
    body: {'key_version': r.keyVersion, 'record': b64(r.locked)},
  );

  Future<UnlockRequestRow> createUnlockRequest(Uint8List request) async =>
      UnlockRequestRow(
        object(
              (await call(
                Ops.createUnlockRequest,
                body: {'request': b64(request)},
              ))['unlock_request'],
            ) ??
            const {},
      );

  Future<List<UnlockRequestRow>> unlockRequests() async => listOf(
    (await call(Ops.listUnlockRequests))['unlock_requests'],
    UnlockRequestRow.new,
  );

  Future<UnlockRequestRow> unlockRequest(String id) async => UnlockRequestRow(
    object(
          (await call(
            Ops.getUnlockRequest,
            path: {'id': id},
          ))['unlock_request'],
        ) ??
        const {},
  );

  Future<void> answerUnlockRequest(
    String id,
    Uuid device,
    int keyVersion,
    Uint8List locked,
  ) => call(
    Ops.answerUnlockRequest,
    path: {'id': id},
    body: {
      'locked_copy': {
        'kind': 'device',
        'device_id': '$device',
        'key_version': keyVersion,
        'locked_key': b64(locked),
      },
    },
  );

  Future<List<PasskeyRow>> passkeys() async =>
      listOf((await call(Ops.listPasskeys))['passkeys'], PasskeyRow.new);

  Future<void> setPasskeyUnlockKey(String passkey, Uint8List unlockPublic) =>
      call(
        Ops.setPasskeyUnlockKey,
        path: {'id': passkey},
        body: {'unlock_public': b64(unlockPublic)},
      );

  /// Proves this app's signing key so its existing device row is
  /// attached to a new session (docs/SEALED.md section 4.1).
  Future<SealedDeviceRow> reattachDevice(
    String deviceId,
    Future<Uint8List> Function(List<int> challenge) sign,
  ) async {
    final b = await call(
      Ops.beginReattachDevice,
      body: {'device_id': deviceId},
    );
    final challenge = unb64(b['challenge'] as String?) ?? Uint8List(0);
    final j = await call(
      Ops.finishReattachDevice,
      body: {'device_id': deviceId, 'signature': b64(await sign(challenge))},
    );
    return SealedDeviceRow(object(j['device']) ?? const {});
  }

  /// The server's unlock page for [deviceId], opened in the system
  /// browser (docs/SEALED.md section 13.3, R43).
  Uri unlockPageUrl(String deviceId) =>
      uri(Ops.openUnlockPage.path, {'device_id': deviceId});

  /// Uploads an encrypted blob to a sealed chat and returns its file id.
  Future<String> uploadSealedBlob(
    String conversationId,
    Uint8List blob, {
    void Function(int sent, int total)? onProgress,
  }) async {
    final j = await upload(
      Ops.uploadFile,
      path: {'id': conversationId},
      parts: [UploadPart('file', 'sealed', blob)],
      onProgress: onProgress,
    );
    return (object(j['file']) ?? const {})['id'] as String? ?? '';
  }

  /// Posts a sealed message; returns the row the server made.
  Future<Message> postSealed(
    String conversationId,
    Outgoing o, {
    List<String> fileIds = const [],
  }) async {
    final j = await call(
      Ops.postMessage,
      path: {'id': conversationId},
      body: {
        'client_id': '${o.clientId}',
        'sealed': {'key_epoch': o.keyEpoch, 'ciphertext': b64(o.ciphertext)},
        'quiet': o.quiet,
        if (fileIds.isNotEmpty) 'file_ids': fileIds,
      },
    );
    return Message.fromJson(object(j['message']) ?? const {});
  }
}
