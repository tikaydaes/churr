// The glue between this app, the server and the sealed-chat state machine
// (lib/core/sealed): one [SealedSession] per signed-in server makes this
// app a device (docs/SEALED.md sections 4, 7 and 13), keeps its chats'
// epochs current, takes in sealed rows, and sends.
//
// It talks to the server through the API (api.dart) and keeps everything
// in the encrypted store (sqlite_store.dart). It hears the server through
// [handleEvent] (the sealed events the sync engine passes on) and
// [takeRows] (sealed rows as they reach the local database). Every
// operation runs one at a time, in order. Plain Dart: no Flutter.

import 'dart:async';
import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import '../api/client.dart';
import '../api/conversation_ops.dart';
import '../api/errors.dart';
import '../api/operations.dart';
import '../ids.dart';
import '../models/json.dart';
import '../api/me.dart';
import '../models/message.dart' hide Quote;
import '../sealed/sealed.dart';
import 'api.dart';
import 'content_view.dart';
import 'notify_keys.dart';
import 'sqlite_store.dart';

/// Where this device stands.
enum SealedStatus {
  /// Not started, or keys not registered yet.
  starting,

  /// The account has no backup yet: the first sealed chat makes it.
  noBackup,

  /// Signed in and registered, without the backup key.
  locked,

  /// Holds the backup key and is in the account's device list.
  unlocked,

  /// Another device took this one out of the list: everything was wiped.
  removed,
}

/// A notice drawn in a sealed chat (docs/SEALED.md section 4.4). Not a
/// message: the server does not know it was shown.
class SealedNotice {
  SealedNotice(
    this.kind,
    this.account,
    this.at, {
    this.platform = 0,
    this.conversation,
  });
  final String kind;
  final String account;
  final DateTime at;
  final int platform;
  final String? conversation;

  Json toJson() => {
    'kind': kind,
    'account': account,
    'at': at.millisecondsSinceEpoch,
    'platform': platform,
    'conversation': conversation,
  };
  static SealedNotice fromJson(Json j) => SealedNotice(
    j['kind'] as String,
    j['account'] as String,
    DateTime.fromMillisecondsSinceEpoch(j['at'] as int),
    platform: j['platform'] as int? ?? 0,
    conversation: j['conversation'] as String?,
  );
}

/// The two private keys an app makes before it signs in, so its box
/// public key can go with the authorization request and a passkey
/// sign-in can hand it the backup key (docs/SEALED.md section 13.3).
class PendingDeviceKeys {
  PendingDeviceKeys(this.signSeed, this.boxPrivate);
  factory PendingDeviceKeys.generate() {
    final r = SystemRandom();
    return PendingDeviceKeys(
      r.take('device_sign_seed', 32),
      r.take('device_box_private', 32),
    );
  }
  final Uint8List signSeed;
  final Uint8List boxPrivate;
  Uint8List get boxPublic => x25519Public(boxPrivate);
}

/// What sign-in brought back for sealed chats.
class SignInHandover {
  SignInHandover({
    required this.keys,
    this.handover,
    this.codeChallenge,
    this.pairingSecret,
  });
  final PendingDeviceKeys keys;
  final Uint8List? handover;
  final String? codeChallenge;

  /// Set when the app was signed in by QR pairing from an unlocked web
  /// chat: the secret the QR code carried, which never reached the server.
  final Uint8List? pairingSecret;
}

/// Serves the keys made before sign-in, then the system's generator.
class _PreparedRandom implements SealedRandom {
  _PreparedRandom(this._keys);
  PendingDeviceKeys? _keys;
  final _system = SystemRandom();
  @override
  Uint8List take(String what, int n) {
    final k = _keys;
    if (k != null && what == 'device_sign_seed') {
      return Uint8List.fromList(k.signSeed);
    }
    if (k != null && what == 'device_box_private') {
      _keys = null;
      return Uint8List.fromList(k.boxPrivate);
    }
    return _system.take(what, n);
  }
}

/// One file sent in a sealed message, before the message is sealed.
class SealedUpload {
  SealedUpload({
    required this.name,
    required this.bytes,
    required this.mediaType,
    required this.kind,
    this.width = 0,
    this.height = 0,
    this.caption = '',
    this.thumbnail,
    this.thumbWidth = 0,
    this.thumbHeight = 0,
  });
  final String name;
  final Uint8List bytes;
  final String mediaType;
  final String kind;
  final int width, height;
  final String caption;

  /// Made on the device: at most 720 pixels on the long edge, re-encoded.
  final Uint8List? thumbnail;
  final int thumbWidth, thumbHeight;
}

/// A way of making the epoch-start delay of section 7.1 step 3.
typedef DelayPicker = Duration Function();

Duration _upToTwoSeconds() => Duration(milliseconds: Random().nextInt(2000));

const _platformNumbers = {
  'android': 1,
  'ios': 2,
  'macos': 3,
  'windows': 4,
  'linux': 5,
  'web': 6,
};

class SealedSession {
  SealedSession({
    required this.api,
    required this.store,
    required this.platformName,
    this.deviceName,
    this.nse = const NoNotificationKeys(),
    this.delay = _upToTwoSeconds,
    int Function()? now,
  }) : now = now ?? (() => DateTime.now().millisecondsSinceEpoch);

  final ApiClient api;
  final SqliteSealedStore store;
  final String platformName;
  final String? deviceName;
  final NotificationKeyStore nse;
  final DelayPicker delay;
  final int Function() now;

  int get platform => _platformNumbers[platformName] ?? 5;

  Device? device;
  String? myId;
  String serverName = '';
  SealedStatus status = SealedStatus.starting;

  /// The backup as last fetched; null when the account has none.
  BackupRow? backup;
  List<UnlockRequestRow> requests = const [];
  List<PasskeyRow> passkeys = const [];
  List<SealedDeviceRow> devices = const [];

  /// Sealed conversations the account is in, with the other person.
  final Map<String, String> conversations = {};

  /// The last thing that went wrong in the background, for the screens.
  String? problem;

  final _changes = StreamController<void>.broadcast();

  /// Fires after anything a screen draws has changed.
  Stream<void> get changes => _changes.stream;
  void _changed() {
    if (!_changes.isClosed) _changes.add(null);
  }

  Future<void> _tail = Future.value();

  /// Runs [f] after everything before it.
  Future<T> _serial<T>(Future<T> Function() f) {
    final c = Completer<T>();
    _tail = _tail.then((_) async {
      try {
        c.complete(await f());
      } on Object catch (e, s) {
        c.completeError(e, s);
      }
    });
    return c.future;
  }

  bool get unlocked => device?.unlocked ?? false;

  Future<void> _save() async {
    await device?.save();
    _changed();
  }

  // ---- starting ----

  /// Loads or makes this device, registers its keys, and catches up.
  /// [signIn] is what a sign-in that just finished brought back.
  Future<void> start({required String myId, SignInHandover? signIn}) =>
      _serial(() async {
        this.myId = myId;
        serverName = (await api.getServer()).name;
        var d = await loadDevice(store, random: SystemRandom(), now: now);
        if (d != null && ('${d.account}' != myId || signIn != null)) {
          // Another account, or a fresh sign-in with new keys: old keys go.
          await _wipeStore();
          d = null;
        }
        if (d != null) {
          d.store = store;
          d.fetchLists = _fetchLists;
          if (!await _stillRegistered(d)) {
            await _wipeStore();
            d = null;
          }
        }
        if (d == null) {
          d = await Device.create(
            account: Uuid.parse(myId),
            server: serverName,
            platform: platform,
            random: _PreparedRandom(signIn?.keys),
            now: now,
          );
          d.random = SystemRandom();
          final row = await api.registerSealedDevice(
            platform: platformName,
            signKey: d.signPublic,
            boxKey: d.boxPublic,
            name: deviceName,
          );
          d.id = Uuid.parse(row.id);
          d.store = store;
          d.fetchLists = _fetchLists;
          device = d;
          await _save();
        }
        device = d;
        await _refresh();
        if (!unlocked && signIn?.handover != null) {
          await _unlockWithHandover(signIn!);
        }
      });

  Future<List<SignedList>> _fetchLists(Uuid account, int after) =>
      api.deviceLists('$account', after: after);

  /// Whether the device row this app holds keys for is still this
  /// session's; attaches it to a new session if it can (section 4.1).
  Future<bool> _stillRegistered(Device d) async {
    try {
      final rows = await api.listSealedDevices();
      devices = rows;
      for (final r in rows) {
        if (r.id != '${d.id}') continue;
        if (r.retired) return false;
        if (r.current) return true;
        await api.reattachDevice(r.id, d.attachSignature);
        return true;
      }
      // Not listed under this id: register the keys on this session's row.
      final row = await api.registerSealedDevice(
        platform: platformName,
        signKey: d.signPublic,
        boxKey: d.boxPublic,
        name: deviceName,
      );
      return row.id == '${d.id}';
    } on ApiException {
      return false;
    }
  }

  /// Catches up with everything that is not replayed as events.
  Future<void> refresh() => _serial(_refresh);

  Future<void> _refresh() async {
    final d = device;
    if (d == null) return;
    problem = null;
    try {
      await _learnLists(d.account);
      if (await _checkRemoved()) return;
      await _refreshBackup();
      if (unlocked) {
        await _ensureListed();
        await _clearRetired();
        requests = await api.unlockRequests();
      }
      passkeys = await api.passkeys();
      for (final other in {...conversations.values}) {
        await _learnLists(Uuid.parse(other));
      }
      for (final e in conversations.entries) {
        await _syncChat(e.key, e.value, quiet: true);
      }
    } on ApiException catch (e) {
      problem = e.message;
    } on SealedError catch (e) {
      problem = e.message;
    }
    _status();
    await _save();
  }

  void _status() {
    if (status == SealedStatus.removed) return;
    final d = device;
    if (d == null) {
      status = SealedStatus.starting;
    } else if (d.unlocked) {
      status = SealedStatus.unlocked;
    } else if (backup == null) {
      status = SealedStatus.noBackup;
    } else {
      status = SealedStatus.locked;
    }
  }

  Future<void> _learnLists(Uuid account) async {
    final d = device!;
    final pin = d.pins[account];
    final lists = await api.deviceLists('$account', after: pin?.newest);
    if (lists.isEmpty) return;
    try {
      await d.learnLists(account, lists);
    } on SealedError catch (e) {
      problem = e.message;
    }
    _takeNotices();
  }

  /// Another device took this one out of the list: wipe (section 7.3).
  Future<bool> _checkRemoved() async {
    final d = device!;
    final mine = d.pins[d.account]?.newestList?.list;
    final listed = await store.read('app/listed') != null;
    if (mine != null && listed && mine.device(d.id) == null) {
      await _wipeAll();
      status = SealedStatus.removed;
      _changed();
      return true;
    }
    if (mine != null && mine.device(d.id) != null && !listed) {
      await store.write('app/listed', Uint8List.fromList([1]));
    }
    return false;
  }

  Future<void> _refreshBackup() async {
    final d = device!;
    backup = await api.backup();
    final b = backup;
    if (b == null) return;
    final mine = b.copyFor('device', deviceId: '${d.id}');
    if (!d.unlocked) {
      // The unlock page, an approving device, or a key replacement left a
      // copy for this device's box key.
      if (mine != null && mine.keyVersion == b.keyVersion) {
        try {
          await d.unlockWithDeviceCopy(mine.locked, b.stored);
          await _join();
        } on SealedError catch (e) {
          problem = e.message;
        }
      }
      return;
    }
    if (b.keyVersion > d.keyVersion && mine != null) {
      // Another device replaced the backup key (section 13.6, step 8).
      await _learnLists(d.account);
      await d.unlockWithDeviceCopy(mine.locked, b.stored);
    }
    if (b.replacementDue) await _replace(const Replacement());
  }

  Future<void> _ensureListed() async {
    final d = device!;
    final mine = d.pins[d.account]?.newestList?.list;
    if (mine == null || mine.device(d.id) != null) return;
    final next = await d.listWithSelf();
    await api.publishDeviceList(next);
    await d.learnLists(d.account, [next]);
    await store.write('app/listed', Uint8List.fromList([1]));
    _takeNotices();
  }

  /// Devices the server reports retired are taken out of the list.
  Future<void> _clearRetired() async {
    final d = device!;
    devices = await api.listSealedDevices();
    final mine = d.pins[d.account]?.newestList?.list;
    if (mine == null) return;
    final gone = [
      for (final r in devices)
        if (r.retired && mine.device(Uuid.parse(r.id)) != null)
          Uuid.parse(r.id),
    ];
    if (gone.isEmpty) return;
    final next = await d.listWithout(gone);
    await api.publishDeviceList(next);
    await d.learnLists(d.account, [next]);
    _takeNotices();
    await _epochsForListChange(d.account, mine: true);
  }

  // ---- notices ----

  List<SealedNotice> _notices = const [];
  bool _noticesLoaded = false;

  Future<void> _loadNotices() async {
    if (_noticesLoaded) return;
    _noticesLoaded = true;
    final raw = await store.read('app/notices');
    if (raw == null) return;
    _notices = [
      for (final j in jsonDecode(utf8.decode(raw)) as List)
        SealedNotice.fromJson((j as Map).cast()),
    ];
  }

  void _takeNotices() {
    final d = device;
    if (d == null || d.notices.isEmpty) return;
    final at = DateTime.fromMillisecondsSinceEpoch(now());
    final next = [
      ..._notices,
      for (final n in d.notices)
        SealedNotice(
          n.kind,
          '${n.account}',
          at,
          platform: n.platform,
          conversation: n.conversation?.toString(),
        ),
    ];
    d.notices.clear();
    _notices = next;
    unawaited(
      store.write(
        'app/notices',
        Uint8List.fromList(
          utf8.encode(jsonEncode([for (final n in next) n.toJson()])),
        ),
      ),
    );
  }

  /// The notices of the chat [conversationId] with [other]: changes to
  /// either person's device list, and this chat's key trouble.
  Future<List<SealedNotice>> noticesFor(
    String conversationId,
    String other,
  ) async {
    await _loadNotices();
    return [
      for (final n in _notices)
        if (n.conversation == conversationId ||
            (n.conversation == null &&
                (n.account == other || n.account == myId)))
          n,
    ];
  }

  // ---- chats ----

  /// Tells the session which sealed chats the account is in.
  Future<void> setConversations(Map<String, String> byId) => _serial(() async {
    final added = [
      for (final k in byId.keys)
        if (!conversations.containsKey(k)) k,
    ];
    conversations
      ..clear()
      ..addAll(byId);
    if (device == null) return;
    for (final id in added) {
      try {
        await _syncChat(id, byId[id]!, quiet: true);
      } on Object catch (e) {
        problem = '$e';
      }
    }
    await _save();
  });

  Chat? chat(String conversationId) =>
      device?.chats[Uuid.tryParse(conversationId)];

  Chat _chat(String conv, String other) =>
      device!.chat(Uuid.parse(conv), Uuid.parse(other));

  /// Fetches the epochs this device has not seen, reads history secrets
  /// from the backup, writes new ones, shares what the other account
  /// missed, and makes an epoch if the lists moved on.
  Future<void> _syncChat(
    String conv,
    String other, {
    bool quiet = false,
  }) async {
    final d = device!;
    if (d.pins[Uuid.parse(other)] == null) await _learnLists(Uuid.parse(other));
    final c = _chat(conv, other);
    await _fetchEpochs(c);
    await _afterEpochs(c, conv, other, quiet: quiet);
  }

  Future<void> _fetchEpochs(Chat c) async {
    final from = (c.current?.keyEpoch ?? 0) + 1;
    final rows = await api.epochRecords('${c.id}', from: from);
    for (final r in rows) {
      await _accept(c, r.keyEpoch, r.seq, r.record);
    }
  }

  Future<void> _accept(Chat c, int keyEpoch, int seq, Uint8List record) async {
    try {
      await c.acceptEpoch(keyEpoch, seq, record);
    } on EpochError {
      // Refused: it keeps its place; [needsEpoch] asks for a repair.
    } on SecondRecordError {
      // The lowest seq stands (section 5.5); the one held stays.
    } on ChainGapError {
      await _fetchEpochsFromStart(c);
    }
    _takeNotices();
  }

  Future<void> _fetchEpochsFromStart(Chat c) async {
    final have = c.current?.keyEpoch ?? 0;
    final rows = await api.epochRecords('${c.id}', from: have + 1);
    for (final r in rows) {
      if (c.epoch(r.keyEpoch) != null) continue;
      try {
        await c.acceptEpoch(r.keyEpoch, r.seq, r.record);
      } on SealedError {
        break;
      }
    }
  }

  Future<void> _afterEpochs(
    Chat c,
    String conv,
    String other, {
    bool quiet = false,
  }) async {
    final d = device!;
    if (d.unlocked) {
      final lacking = [
        for (final e in c.epochs)
          if (e.accepted && e.history == null) e.keyEpoch,
      ];
      if (lacking.isNotEmpty) {
        for (final r in await api.backupRecords(conversationId: conv)) {
          if (!lacking.contains(r.keyEpoch)) continue;
          try {
            c.readBackupRecord(r);
          } on BackupRecordError {
            // From before a key replacement, or not this chat's.
          }
        }
      }
      await _uploadSecrets(c);
      if (c.shareDue) {
        final h = c.historyShare();
        if (h != null) await _postContent(c, h);
      }
    }
    await _writeNotificationKeys(c);
    await _retryWaiting(c, conv);
    final need = c.needsEpoch();
    if (d.unlocked && (need == 'device_list' || need == 'refused')) {
      await _makeEpoch(c, conv);
    }
    await _save();
  }

  /// Makes and posts the chat's next epoch; on losing a race, takes the
  /// winner and makes the one after only if the winner does not cover
  /// what was wanted (section 5.5).
  Future<void> _makeEpoch(Chat c, String conv) async {
    for (var attempt = 0; attempt < 3; attempt++) {
      final (stored, keyEpoch) = await c.makeEpoch();
      try {
        final row = await api.postEpochRecord(conv, keyEpoch, stored);
        await _accept(c, row.keyEpoch, row.seq, stored);
      } on ApiException catch (e) {
        if (e.code != 'conflict') rethrow;
        await _fetchEpochs(c);
      }
      final need = c.needsEpoch();
      if (need != 'device_list' &&
          need != 'refused' &&
          need != 'age' &&
          need != 'clock') {
        break;
      }
    }
    await _uploadSecrets(c);
    await _writeNotificationKeys(c);
  }

  /// Section 7.1: a list change this device has no epoch for. The device
  /// that made the change goes at once; others wait up to two seconds.
  Future<void> _epochsForListChange(Uuid account, {bool mine = false}) async {
    final d = device!;
    if (!d.unlocked) return;
    if (!mine) await Future<void>.delayed(delay());
    for (final e in conversations.entries) {
      if ('$account' != e.value && account != d.account) continue;
      final c = _chat(e.key, e.value);
      await _fetchEpochs(c);
      if (c.needsEpoch() == 'device_list') await _makeEpoch(c, e.key);
    }
  }

  // ---- notification extension items ----

  Future<void> _writeNotificationKeys(Chat c) async {
    final raw = await store.read('app/nse');
    final had = raw == null
        ? <String>{}
        : {...(jsonDecode(utf8.decode(raw)) as List).cast<String>()};
    final now = <String>{...had};
    for (final e in c.epochs) {
      final account = notificationKeyAccount(c.id, e.keyEpoch);
      if (!e.accepted || e.history == null) {
        if (had.contains(account)) {
          await nse.forget(c, e.keyEpoch);
          now.remove(account);
        }
        continue;
      }
      if (!had.contains(account)) {
        await nse.share(c, e);
        now.add(account);
      }
    }
    if (now.length != had.length || !now.containsAll(had)) {
      await store.write(
        'app/nse',
        Uint8List.fromList(utf8.encode(jsonEncode([...now]))),
      );
    }
  }

  // ---- events ----

  /// A sealed event the sync engine passed on.
  Future<void> handleEvent(Json e) => _serial(() async {
    if (device == null) return;
    try {
      await _handle(e);
    } on ApiException catch (x) {
      problem = x.message;
    } on SealedError catch (x) {
      problem = x.message;
    }
    _status();
    await _save();
  });

  Future<void> _handle(Json e) async {
    final d = device!;
    switch (e['type']) {
      case 'sealed.epoch_started':
        final conv = e['conversation_id'] as String? ?? '';
        final ep = object(e['epoch']);
        var other = conversations[conv];
        if (other == null && ep != null) {
          // A chat this device has not heard of: the record names both.
          final rec = EpochRecord.decode(
            splitStoredEpoch(unb64(ep['record'] as String?)!).$1,
          );
          for (final a in rec.accounts) {
            if (a.accountId != d.account) other = '${a.accountId}';
          }
          if (other != null) conversations[conv] = other;
        }
        if (other == null) return;
        if (d.pins[Uuid.parse(other)] == null) {
          await _learnLists(Uuid.parse(other));
        }
        final c = _chat(conv, other);
        try {
          final again = await c.epochStarted(e);
          for (final o in again) {
            await api.postSealed(conv, o);
          }
        } on ChainGapError {
          await _fetchEpochs(c);
        } on EpochError {
          // Refused; repaired below.
        } on SecondRecordError {
          // Ignored: the lowest seq stands.
        }
        _takeNotices();
        await _afterEpochs(c, conv, other);
      case 'device_list.changed':
        final account = Uuid.tryParse(e['account_id'] as String?);
        if (account == null) return;
        await _learnLists(account);
        if (account == d.account && await _checkRemoved()) return;
        await _epochsForListChange(account);
      case 'unlock_request.created':
        if (d.unlocked) requests = await api.unlockRequests();
      case 'unlock_request.answered':
        final r = UnlockRequestRow(object(e['unlock_request']) ?? const {});
        if (r.deviceId == '${d.id}' && !d.unlocked) {
          final full = r.answer == null ? await api.unlockRequest(r.id) : r;
          final b = await api.backup();
          if (full.answer != null && b != null) {
            backup = b;
            await d.unlockWithDeviceCopy(full.answer!, b.stored);
            await _join();
          }
        }
        if (d.unlocked) requests = await api.unlockRequests();
      case 'backup.changed':
        await _refreshBackup();
        passkeys = await api.passkeys();
      case 'device.retired':
        if (d.unlocked) await _clearRetired();
    }
  }

  // ---- rows ----

  /// Sealed rows of [conversationId] as they reach the local database,
  /// in any order and any number of times.
  Future<void> takeRows(
    String conversationId,
    String other,
    List<Message> rows,
  ) => _serial(() async {
    if (device == null) return;
    final c = _chat(conversationId, other);
    final sorted = [
      for (final m in rows)
        if (m.sealed != null) m,
    ]..sort((a, b) => a.seq.compareTo(b.seq));
    var changed = false;
    for (final m in sorted) {
      final had = store.row(conversationId, m.seq);
      if (had != null && had.state != 'nokey') continue;
      changed = true;
      await _takeRow(c, m);
    }
    if (changed) {
      if (unlocked) await _uploadSecrets(c);
      await _writeNotificationKeys(c);
      await _save();
    }
  });

  /// Writes the backup records for secrets just learned; if the server
  /// cannot be reached they stay due and go next time.
  Future<void> _uploadSecrets(Chat c) async {
    final due = [...c.newSecrets];
    if (due.isEmpty || !unlocked) return;
    try {
      for (final r in c.takeNewSecrets()) {
        await api.putBackupRecord(r);
      }
    } on Object {
      for (final k in due) {
        if (!c.newSecrets.contains(k)) c.newSecrets.add(k);
      }
    }
  }

  Future<void> _takeRow(Chat c, Message m) async {
    final keyEpoch = asInt(m.sealed!['key_epoch']);
    if (c.epoch(keyEpoch) == null) {
      try {
        await _fetchEpochs(c);
      } on Object {
        // Offline, or signed out: tried again with the next row.
      }
    }
    final ct = unb64(m.sealed!['ciphertext'] as String?);
    var state = 'ok';
    var reference = m.clientId ?? '';
    if (ct == null) {
      state = 'unchecked';
    } else {
      try {
        final r = await c.receive(
          Row(
            seq: m.seq,
            author: Uuid.parse(m.authorId),
            keyEpoch: keyEpoch,
            quiet: m.raw['quiet'] == true,
            ciphertext: ct,
          ),
        );
        reference = '${r.header.reference}';
        if (r.dropped) {
          state = 'dropped';
        } else if (r.kind != kindMessage) {
          state = 'quiet';
        }
        if (r.kind == kindHistory) await _retryWaiting(c, '${c.id}');
      } on NoKeyError {
        state = 'nokey';
        await store.write(
          'app/wait/${c.id}/${m.seq}',
          Uint8List.fromList(utf8.encode(jsonEncode(m.raw))),
        );
      } on SealedError {
        state = 'unchecked';
      } on FormatException {
        state = 'unchecked';
      }
    }
    if (state != 'nokey') await store.delete('app/wait/${c.id}/${m.seq}');
    store.putRow(
      SealedRowMeta(
        conversationId: '${c.id}',
        seq: m.seq,
        messageId: m.id,
        reference: reference,
        createdAt: m.createdAt,
        order: m.order,
        state: state,
      ),
    );
  }

  /// Rows that arrived before their epoch's secret, read again.
  Future<void> _retryWaiting(Chat c, String conv) async {
    final waiting = store.waiting(conv);
    if (waiting.isEmpty) return;
    for (final w in waiting) {
      final raw = await store.read('app/wait/$conv/${w.seq}');
      if (raw == null) continue;
      final m = Message.fromJson((jsonDecode(utf8.decode(raw)) as Map).cast());
      final ep = c.epoch(asInt(m.sealed?['key_epoch']));
      if (ep?.history == null) continue;
      await _takeRow(c, m);
    }
  }

  // ---- sending ----

  Future<Message> _postContent(
    Chat c,
    Uint8List content, {
    List<String> fileIds = const [],
  }) async {
    final conv = '${c.id}';
    final need = c.needsEpoch();
    if (need != null) await _makeEpoch(c, conv);
    for (var attempt = 0; ; attempt++) {
      final o = await c.seal(content);
      try {
        final m = await api.postSealed(conv, o, fileIds: fileIds);
        await _takeRow(c, m);
        return m;
      } on ApiException catch (e) {
        c.unsend(o);
        if (e.code != 'conflict' || attempt >= 2) rethrow;
        // A newer epoch, or a stale one: catch up and seal again.
        await _fetchEpochs(c);
        if (c.needsEpoch() != null) await _makeEpoch(c, conv);
      }
    }
  }

  /// Seals and posts [content] in [conversationId].
  Future<Message> send(
    String conversationId,
    String other,
    Uint8List content, {
    List<String> fileIds = const [],
  }) => _serial(() async {
    final c = _chat(conversationId, other);
    final m = await _postContent(c, content, fileIds: fileIds);
    await _uploadSecrets(c);
    await _save();
    return m;
  });

  Map<String, String> usernames = const {};

  /// A new message: Churr Markdown text, files already encrypted and
  /// uploaded ([FileEntry]), and the reference of the message it replies
  /// to.
  Future<Message> sendText(
    String conversationId,
    String other,
    String text, {
    List<FileEntry> files = const [],
    String? parent,
    Quote? quote,
  }) => send(
    conversationId,
    other,
    messageContent(
      text,
      entities: workOutEntities(text, usernames),
      files: files,
      parent: Uuid.tryParse(parent),
      quote: quote,
    ),
    fileIds: [
      for (final f in files) ...[f.id, ?f.thumb?.id],
    ],
  );

  Future<void> edit(String conv, String other, String reference, String text) =>
      send(
        conv,
        other,
        editContent(
          Uuid.parse(reference),
          text,
          entities: workOutEntities(text, usernames),
        ),
      );

  /// Deletes one of this person's messages: the sealed `delete`, then
  /// the server's row (whose ciphertext and blobs it erases at once).
  Future<void> delete(String conv, String other, String reference) async {
    await send(conv, other, deleteContent(Uuid.parse(reference)));
    final row = store.rowByReference(conv, reference);
    if (row != null && row.messageId.isNotEmpty) {
      try {
        await api.deleteMessage(row.messageId);
      } on ApiException {
        // Gone already; the sealed delete is what the devices act on.
      }
    }
  }

  Future<void> react(
    String conv,
    String other,
    String reference,
    String emoji,
    bool on,
  ) => send(conv, other, reactionContent(Uuid.parse(reference), emoji, on));

  Future<void> tick(
    String conv,
    String other,
    String reference,
    String item,
    bool checked,
  ) => send(conv, other, taskContent(Uuid.parse(reference), item, checked));

  /// Encrypts [u] (and its thumbnail) on the device and uploads the blobs.
  Future<FileEntry> upload(
    String conversationId,
    SealedUpload u, {
    void Function(double)? onProgress,
  }) async {
    final r = SystemRandom();
    Future<String> put(
      Uint8List plain,
      Uint8List key,
      void Function(int, int)? p,
    ) async {
      final blob = BytesBuilder(copy: false);
      await for (final chunk in encryptFileStream(key, Stream.value(plain))) {
        blob.add(chunk);
      }
      return api.uploadSealedBlob(
        conversationId,
        blob.takeBytes(),
        onProgress: p,
      );
    }

    final key = r.take('file_key', 32);
    final id = await put(
      u.bytes,
      key,
      (s, t) => onProgress?.call(t == 0 ? 1 : s / t),
    );
    FileThumb? thumb;
    if (u.thumbnail != null) {
      final tk = r.take('file_key', 32);
      thumb = FileThumb(
        id: await put(u.thumbnail!, tk, null),
        key: tk,
        width: u.thumbWidth,
        height: u.thumbHeight,
      );
    }
    return FileEntry(
      id: id,
      key: key,
      name: u.name,
      mediaType: u.mediaType,
      kind: u.kind,
      size: u.bytes.length,
      width: u.width,
      height: u.height,
      caption: u.caption,
      thumb: thumb,
    );
  }

  /// Fetches a blob and decrypts it chunk by chunk; the bytes are used
  /// only once the last chunk has checked out.
  Future<Uint8List> download(
    String fileId,
    List<int> key, {
    void Function(double)? onProgress,
  }) async {
    final blob = await api.download(
      Ops.downloadFile.fill({'id': fileId, 'name': 'sealed'}),
    );
    final out = BytesBuilder(copy: false);
    var done = 0;
    await for (final chunk in decryptFileStream(key, Stream.value(blob))) {
      out.add(chunk);
      done += chunk.length + 16;
      onProgress?.call(blob.isEmpty ? 1 : done / blob.length);
    }
    return out.takeBytes();
  }

  // ---- what the chat view draws ----

  /// The decrypted messages of a chat, as the API's `Message` JSON, with
  /// their files.
  List<(Message, List<SealedFile>, bool outOfOrder)> view(
    String conversationId,
  ) {
    final c = chat(conversationId);
    if (c == null) return const [];
    final out = <(Message, List<SealedFile>, bool)>[];
    for (final s in c.view()) {
      final meta = store.rowByReference(conversationId, '${s.reference}');
      out.add((
        Message.fromJson(
          shownAsMessage(
            s,
            meta,
            conversationId,
            myId: myId,
            usernames: usernames,
          ),
        ),
        s.deleted ? const <SealedFile>[] : filesOf(s),
        s.outOfOrder,
      ));
    }
    return out;
  }

  /// The decrypted text of the server's row [messageId], for lists such
  /// as notifications; null when this device cannot read it.
  String? textOfRow(String messageId) {
    final meta = store.rowByMessageId(messageId);
    if (meta == null || meta.state != 'ok') return null;
    final s = chat(meta.conversationId)
        ?.messages[Uuid.tryParse(meta.reference)];
    if (s == null || s.deleted) return null;
    return s.text;
  }

  /// Rows that failed their checks, and rows still waiting for a key.
  ({int unchecked, int waiting}) troubles(String conversationId) {
    final rows = store.rows(conversationId);
    return (
      unchecked: rows.where((r) => r.state == 'unchecked').length,
      waiting: rows.where((r) => r.state == 'nokey').length,
    );
  }

  /// The highest seq taken in, quiet rows included: the read position.
  int? highestSeq(String conversationId) {
    final rows = store.rows(conversationId);
    return rows.isEmpty ? null : rows.last.seq;
  }

  /// Whether the other person's devices include a browser.
  bool otherHasBrowser(String other) =>
      device?.pins[Uuid.tryParse(other)]?.newestList?.list.devices.any(
        (d) => d.platform == 6,
      ) ??
      false;

  bool verified(String other) =>
      device?.pins[Uuid.tryParse(other)]?.verified ?? false;
  bool held(String other) => device?.pins[Uuid.tryParse(other)]?.hold ?? false;

  /// The 60 digits for the chat with [other], or null before both lists
  /// are pinned.
  String? compareCode(String other) {
    final d = device;
    final o = Uuid.tryParse(other);
    final theirs = o == null ? null : d?.pins[o]?.newestList?.list;
    if (d == null || theirs == null || d.pins[d.account]?.newestList == null) {
      return null;
    }
    return d.compareCodeWith(o!, theirs.server);
  }

  Future<void> markVerified(String other) => _serial(() async {
    device?.markVerified(Uuid.parse(other));
    await _save();
  });

  Future<void> acknowledgeReset(String other) => _serial(() async {
    device?.acknowledge(Uuid.parse(other));
    await _save();
  });

  /// The devices in a list, for the screens.
  List<ListDevice> listedDevices(String account) =>
      device?.pins[Uuid.tryParse(account)]?.newestList?.list.devices ??
      const [];

  // ---- starting a chat ----

  /// Starts (or finds) the sealed chat with [other]: the app makes the
  /// conversation's id and epoch record 1 (R54). Returns the conversation.
  Future<Json> startChat(String other) => _serial(() async {
    final d = device!;
    if (!d.unlocked) throw const LockedError();
    final o = Uuid.parse(other);
    await _learnLists(d.account);
    await _learnLists(o);
    final id = newUuid(DateTime.fromMillisecondsSinceEpoch(now()));
    final c = d.chat(Uuid.parse(id), o);
    final (stored, _) = await c.makeEpoch();
    final (conv, _) = await api.openSealedChat(other, stored);
    final got = conv['id'] as String? ?? id;
    if (got != id) d.chats.remove(Uuid.parse(id));
    conversations[got] = other;
    await _syncChat(got, other);
    await _save();
    return conv;
  });

  // ---- making the backup, unlocking ----

  /// The first device of an account (section 13.1): backup key, account
  /// key, phrase, list version 1. Returns the twelve words, to be shown
  /// once. If another device of the account won the race, null: the
  /// device then unlocks in the ordinary way.
  Future<String?> createBackup() => _serial(() async {
    final d = device!;
    final b = await d.createBackup();
    try {
      backup = await api.createBackup(b);
    } on ApiException catch (e) {
      if (e.code == 'already_exists') {
        await _refreshBackup();
        _status();
        return null;
      }
      rethrow;
    }
    await d.adopt(b);
    await store.write('app/listed', Uint8List.fromList([1]));
    _takeNotices();
    _status();
    await _save();
    return b.phrase;
  });

  /// Unlocks with the twelve words typed in.
  Future<void> unlockWithPhrase(String words) => _serial(() async {
    final d = device!;
    final b = backup = await api.backup();
    final copy = b?.copyFor('phrase');
    if (b == null || copy == null) {
      throw const UnlockError('the account has no phrase copy');
    }
    await _learnLists(d.account);
    await d.unlockWithPhrase(words, copy.locked, b.stored);
    await _join();
    _status();
    await _save();
  });

  Future<void> _unlockWithHandover(SignInHandover h) async {
    if (h.handover == null ||
        (h.pairingSecret == null && h.codeChallenge == null)) {
      return;
    }
    final d = device!;
    final b = backup ??= await api.backup();
    if (b == null) return;
    try {
      await _learnLists(d.account);
      if (h.pairingSecret != null) {
        await d.unlockWithPairingHandover(
          h.handover!,
          h.pairingSecret!,
          b.stored,
        );
      } else {
        await d.unlockWithHandover(h.handover!, h.codeChallenge!, b.stored);
      }
      await _join();
    } on SealedError catch (e) {
      problem = e.message;
    }
    _status();
    await _save();
  }

  /// Unlocks with a hand-over that came later (QR pairing).
  Future<void> unlockWithSignIn(SignInHandover h) =>
      _serial(() => _unlockWithHandover(h));

  /// Asks another device for the backup key (section 13.7). Returns the
  /// request's id, its record (for the QR code) and the 20-character
  /// code.
  Future<(String, Uint8List, String)> requestApproval() => _serial(() async {
    final d = device!;
    final (request, code) = await d.newUnlockRequest();
    final row = await api.createUnlockRequest(request);
    return (row.id, request, code);
  });

  /// Looks once for the answer to [requestId]; true once unlocked.
  Future<bool> checkApproval(String requestId) => _serial(() async {
    final d = device!;
    if (d.unlocked) return true;
    final r = await api.unlockRequest(requestId);
    if (r.answer == null) return false;
    final b = backup = await api.backup();
    if (b == null) return false;
    await d.unlockWithDeviceCopy(r.answer!, b.stored);
    await _join();
    _status();
    await _save();
    return true;
  });

  /// The unlock page for this device (R43: a page the server supplies).
  Uri unlockPage() => api.unlockPageUrl('${device!.id}');

  /// After the unlock page: looks for the copy it left; true once unlocked.
  Future<bool> checkUnlockPage() => _serial(() async {
    await _refreshBackup();
    _status();
    await _save();
    return unlocked;
  });

  /// Section 7.2: just unlocked, the device lists itself, reads its
  /// account's history secrets and makes a new epoch in each chat.
  Future<void> _join() async {
    final d = device!;
    await _learnLists(d.account);
    await _ensureListed();
    for (final e in conversations.entries) {
      final c = _chat(e.key, e.value);
      await _fetchEpochs(c);
      await _afterEpochs(c, e.key, e.value);
      if (c.needsEpoch() == 'device_list') await _makeEpoch(c, e.key);
    }
  }

  /// Approving another device of this account: only if [code] is the
  /// code worked out from the request the server passed on.
  Future<void> approve(UnlockRequestRow r, String code) => _serial(() async {
    final d = device!;
    final copy = await d.answerUnlockRequest(r.request, code);
    final u = await checkUnlockRequest(r.request);
    await api.answerUnlockRequest(r.id, u.deviceId, d.keyVersion, copy);
    requests = await api.unlockRequests();
    _changed();
  });

  /// "Let the passkey unlock sealed chats?" answered yes (section 13.4).
  Future<void> approvePasskey(PasskeyRow p) => _serial(() async {
    final d = device!;
    final u = await d.approvePasskey(Uuid.parse(p.id), p.unlockPublic!);
    await api.addLockedCopy(u.copy, d.keyVersion, record: u);
    d.adoptRecord(u);
    passkeys = await api.passkeys();
    await _save();
  });

  /// Removes another device of the account: its session ends, it leaves
  /// the list, and the backup key and account key are replaced because
  /// it held both (sections 7.3 and 13.6). New epochs follow.
  Future<void> removeDevice(String id) => _serial(() async {
    await api.removeSealedDevice(id);
    if (unlocked &&
        device!.pins[device!.account]?.newestList?.list.device(
              Uuid.parse(id),
            ) !=
            null) {
      await _replace(Replacement(devices: [Uuid.parse(id)]));
    }
    devices = await api.listSealedDevices();
    await _save();
  });

  /// Replaces the backup key and account key (section 13.6).
  Future<void> replaceKeys() => _serial(() => _replace(const Replacement()));

  Future<void> _replace(Replacement r) async {
    final d = device!;
    final records = await api.backupRecords();
    final b = await d.replaceBackup(r, records);
    backup = await api.replaceBackup(b);
    await d.adopt(b);
    _takeNotices();
    for (final e in conversations.entries) {
      final c = _chat(e.key, e.value);
      await _fetchEpochs(c);
      if (c.needsEpoch() != null) await _makeEpoch(c, e.key);
    }
  }

  /// Start again when no way is left (section 13.8). Returns the new
  /// twelve words.
  Future<String> resetBackup() => _serial(() async {
    final d = device!;
    await _learnLists(d.account);
    final b = await d.resetBackup();
    backup = await api.resetBackup(b);
    await d.adopt(b);
    await store.write('app/listed', Uint8List.fromList([1]));
    _takeNotices();
    for (final e in conversations.entries) {
      final c = _chat(e.key, e.value);
      await _fetchEpochs(c);
      if (c.needsEpoch() != null) await _makeEpoch(c, e.key);
    }
    _status();
    await _save();
    return b.phrase;
  });

  // ---- leaving ----

  /// Signing out on this device: its last act is to take itself out of
  /// the list; then every key and the store are wiped.
  Future<void> signOut() => _serial(() async {
    final d = device;
    if (d != null &&
        d.unlocked &&
        d.pins[d.account]?.newestList?.list.device(d.id) != null) {
      try {
        await api.publishDeviceList(await d.listWithout([d.id]));
      } on Object {
        // Wiped anyway; the account's other devices clear the entry.
      }
    }
    await _wipeAll();
  });

  Future<void> _wipeStore() async {
    await store.clearAll();
    device = null;
  }

  Future<void> _wipeAll() async {
    await nse.forgetAll();
    await store.wipe();
    device = null;
    _changed();
  }

  void dispose() => _changes.close();
}
