// A Chat is one sealed chat as one device holds it: the chain of epoch
// records, the secrets it has for them, its send counter, and what it
// shows. A port of internal/sealed/ref/chat.go.

part of 'device.dart';

/// One key epoch as a device holds it.
class Epoch {
  final int keyEpoch;

  /// The record's place in the conversation's order.
  final int seq;

  /// The record with its signature, exactly as received.
  final Uint8List stored;

  /// The parsed record; null if it does not parse.
  EpochRecord? record;

  /// Whether the record passed section 5.4; [refusal] is why not.
  bool accepted = false;
  SealedError? refusal;

  /// The recipient devices with their keys, from the lists the record names.
  Map<Uuid, ListDevice> devices = {};

  /// The epoch secret, kept only while the epoch is current; the history
  /// secret, kept as long as the epoch has messages.
  Uint8List? secret;
  Uint8List? history;

  Epoch(this.keyEpoch, this.seq, this.stored);
}

/// One message as a device draws it.
class Shown {
  final Uuid reference;
  final Uuid author;
  final Uuid device;
  final int seq;
  final int keyEpoch;
  String text;

  /// The `files` of the content, as parsed JSON.
  List<Object?> files;
  final String parentId;

  /// Typed with /me (ruling R67): drawn after the author's name.
  final bool action;
  bool edited = false;
  bool deleted = false;

  /// The mark that the server delivered it out of order.
  final bool outOfOrder;

  /// Each emoji with the accounts that put it there, in the order added.
  final Map<String, Set<Uuid>> reactions = {};

  /// Each checklist item's state; [_taskSeq] the sequence number that set
  /// it, since the latest by sequence wins.
  final Map<String, bool> tasks = {};
  final Map<String, int> _taskSeq = {};

  Shown({
    required this.reference,
    required this.author,
    required this.device,
    required this.seq,
    required this.keyEpoch,
    required this.text,
    required this.files,
    required this.parentId,
    required this.outOfOrder,
    this.action = false,
  });
}

/// A sealed message ready to post.
class Outgoing {
  final Uint8List ciphertext;
  final Uuid clientId; // the message reference
  final int keyEpoch;
  final bool quiet;
  final int counter;
  Outgoing(
    this.ciphertext,
    this.clientId,
    this.keyEpoch,
    this.quiet,
    this.counter,
  );
}

/// A sealed message's row as the server gives it.
class Row {
  final int seq;
  final Uuid author;
  final int keyEpoch;
  final bool quiet;
  final Uint8List ciphertext;
  Row({
    required this.seq,
    required this.author,
    required this.keyEpoch,
    required this.quiet,
    required this.ciphertext,
  });
}

/// What a device made of one row.
class Received {
  final Header header;
  String kind = '';
  Map<String, Object?> content = {};

  /// The same device and counter was seen at a lower sequence number: a
  /// replay, not shown.
  bool dropped = false;

  /// Its counter is not higher than the last one seen from that device at
  /// a lower sequence number: shown with a mark.
  bool outOfOrder = false;

  /// This very row was taken in before; nothing changed.
  bool already = false;

  /// The device's counters jumped: "A message may be missing here."
  bool gap = false;
  Received(this.header);
}

class _Sent {
  final Uuid device;
  int counter;
  final int seq;
  _Sent(this.device, this.counter, this.seq);
}

/// The seven-day rule, and how far ahead of the device's clock a record's
/// time may be.
const epochMaxAge = 7 * 24 * 60 * 60 * 1000;
const epochMaxFuture = 5 * 60 * 1000;

/// A device's state for one sealed chat.
class Chat {
  final Device _d;
  final Uuid id;
  final Uuid other;

  /// The chain of records the device was given, in order, refused ones
  /// included: a refused record keeps its place.
  final List<Epoch> epochs = [];

  /// The counter of the last message this device sent here.
  int counter = 0;

  /// Set when the device accepts a record in which the other account has a
  /// device again after epochs in which it had none.
  bool shareDue = false;

  // (device, counter) -> the sequence number it was seen at; per sending
  // device, the counters seen so far in sequence order.
  final Map<String, int> _seen = {};
  final Map<Uuid, List<_Sent>> _byDevice = {};

  /// The messages the device shows, by reference; [order] lists the
  /// references by sequence number.
  final Map<Uuid, Shown> messages = {};
  final List<Uuid> order = [];

  /// Rows shown as "A message could not be checked".
  int unchecked = 0;

  /// Key epochs whose history secret was just learned and not yet written
  /// to the account's backup.
  final List<int> newSecrets = [];

  /// What this device sealed under the current key epoch, by reference:
  /// each message's content, kept so it can be sent again if another
  /// server's record for that key epoch stands instead ([replaceEpoch]).
  /// Only the current epoch's are kept.
  final Map<int, List<(Uuid, Uint8List)>> _sentUnder = {};

  /// What [_sentUnder] holds for one key epoch, oldest first.
  List<(Uuid, Uint8List)> sentUnder(int keyEpoch) =>
      List.unmodifiable(_sentUnder[keyEpoch] ?? const []);

  Chat._(this._d, this.id, this.other);

  static String _key(Uuid device, int counter) => '$device/$counter';

  /// Whether a (device, counter) pair has been taken in.
  bool hasSeen(Uuid device, int counter) =>
      _seen.containsKey(_key(device, counter));

  /// The newest record, accepted or not; null before any.
  Epoch? get current => epochs.isEmpty ? null : epochs.last;

  Epoch? epoch(int keyEpoch) {
    for (final e in epochs) {
      if (e.keyEpoch == keyEpoch) return e;
    }
    return null;
  }

  /// Whether this device must make a new epoch before it sends (7.1), and
  /// why: first, refused, device_list, age or clock. Null if not.
  String? needsEpoch() {
    final cur = current;
    if (cur == null) return 'first';
    if (!cur.accepted) return 'refused';
    for (final a in cur.record!.accounts) {
      final p = _d.pins[a.accountId];
      if (p != null && p.newest > a.listVersion) return 'device_list';
    }
    if (cur.record!.recipient(_d.id) == null) return 'device_list';
    final now = _d.now();
    if (cur.record!.at + epochMaxAge < now) return 'age';
    if (cur.record!.at > now + epochMaxFuture) return 'clock';
    return null;
  }

  /// Builds the chat's next epoch record from the newest lists the device
  /// has checked for both accounts (5.3). The record joins the chain only
  /// once the server has stored it and it comes back through
  /// [acceptEpoch]. Returns the stored form and its key epoch.
  Future<(Uint8List, int)> makeEpoch() async {
    final d = _d;
    if (!d.unlocked) throw const LockedError();
    var keyEpoch = 1;
    Uint8List? prev;
    final cur = current;
    if (cur != null) {
      keyEpoch = cur.keyEpoch + 1;
      prev = epochHash(cur.stored);
    }
    final lists = [
      for (final acc in [d.account, other])
        EpochList(acc, d.pins[acc]?.newestList?.signed.list),
    ];
    final mine = d.pins[d.account]?.newestList;
    if (mine == null) {
      throw const SealedError("the device's account has no device list");
    }
    if (mine.list.device(d.id) == null) {
      throw const SealedError("the device is not in its account's device list");
    }
    final secret = d._random('epoch_secret:$keyEpoch', 32);
    final stored = await ep.makeEpoch(
      conversationId: id,
      keyEpoch: keyEpoch,
      prev: prev,
      makerAccountId: d.account,
      makerDeviceId: d.id,
      makerSign: d.sign,
      lists: lists,
      secret: secret,
      oneTime: (device) =>
          d._random('epoch_seal_one_time:$keyEpoch:$device', 32),
      at: d.now(),
    );
    return (stored, keyEpoch);
  }

  /// Takes one epoch record from the server, with the key epoch and
  /// sequence number it was filed under, and checks it (5.4). A record
  /// that passes becomes current. One that fails stays in the chain as
  /// refused, the device shows "This chat's keys are out of step", and the
  /// [EpochError] is thrown; [needsEpoch] then asks for a repair.
  Future<void> acceptEpoch(int keyEpoch, int seq, Uint8List stored) async {
    final d = _d;
    final have = epoch(keyEpoch);
    if (have != null) {
      if (bytesEqual(have.stored, stored)) return;
      throw const SecondRecordError();
    }
    final prev = current;
    if (prev != null && keyEpoch != prev.keyEpoch + 1) {
      throw const ChainGapError();
    }
    final e = Epoch(keyEpoch, seq, Uint8List.fromList(stored));
    // The lists the record names are fetched before it is checked.
    try {
      final parsed = EpochRecord.decode(splitStoredEpoch(e.stored).$1);
      e.record = parsed;
      for (final a in parsed.accounts) {
        await d._ensureList(a.accountId, a.listVersion);
      }
    } on RecordError {
      // Rule 1 refuses it below.
    }
    SealedError? err;
    AcceptedEpoch? ok;
    try {
      ok = await checkEpochRecord(
        stored: e.stored,
        conversationId: id,
        accounts: [d.account, other],
        previous: prev?.stored,
        listOf: d.listOf,
        deviceId: d.id,
        box: d.box,
      );
      if (ok.record.keyEpoch != keyEpoch) {
        err = const EpochError(
          2,
          'the record is not for the key epoch the server filed it under',
        );
      }
    } on EpochError catch (x) {
      err = x;
    }
    epochs.add(e);
    if (err != null) {
      e.refusal = err;
      d.notices.add(Notice(noticeOutOfStep, other, conversation: id));
      throw err;
    }
    e
      ..accepted = true
      ..record = ok!.record
      ..devices = ok.devices;
    if (ok.secret != null) {
      e.secret = ok.secret;
      e.history = historySecret(ok.secret!, id, keyEpoch);
      newSecrets.add(keyEpoch);
    }
    if (prev != null) {
      // The previous epoch secret is discarded; its history secret stays.
      prev.secret = null;
      if (prev.accepted &&
          e.record!.hasAccount(other) &&
          !prev.record!.hasAccount(other)) {
        shareDue = true;
      }
    }
  }

  /// The backup records to upload for history secrets just learned, one
  /// per key epoch, locked under the backup key (13.2).
  List<StoredRecord> takeNewSecrets() {
    final d = _d;
    if (!d.unlocked) throw const LockedError();
    final out = <StoredRecord>[];
    for (final keyEpoch in newSecrets) {
      final e = epoch(keyEpoch);
      if (e == null || e.history == null) continue;
      final locked = lockBackupRecord(
        d.backupKey!,
        d.account,
        d.keyVersion,
        BackupRecord(
          conversationId: id,
          keyEpoch: keyEpoch,
          secret: e.history!,
          at: d.now(),
        ),
        d._random('backup_record_salt:$id:$keyEpoch', saltSize),
      );
      out.add(StoredRecord(id, keyEpoch, d.keyVersion, locked));
    }
    newSecrets.clear();
    return out;
  }

  /// Opens one of the account's backup records for this chat and keeps its
  /// history secret if it matches the epoch record's `check` (13.2).
  void readBackupRecord(StoredRecord r) {
    final d = _d;
    if (!d.unlocked) throw const LockedError();
    final e = epoch(r.keyEpoch);
    if (e == null || !e.accepted || r.conversationId != id) {
      throw const BackupRecordError();
    }
    final BackupRecord b;
    try {
      b = openBackupRecord(
        d.backupKey!,
        d.account,
        id,
        r.keyEpoch,
        r.keyVersion,
        r.locked,
      );
    } on SealedError {
      throw const BackupRecordError();
    }
    if (!checkHistorySecret(b.secret, e.record!)) {
      throw const BackupRecordError();
    }
    e.history = b.secret;
  }

  /// Encrypts and signs one message in the current epoch. Every kind but
  /// `message` is sent quiet. The raised counter is saved before the
  /// message is returned, so a crash cannot reuse it.
  Future<Outgoing> seal(Uint8List content) => _seal(content);

  Future<Outgoing> _seal(Uint8List content, {Uuid? reference}) async {
    final d = _d;
    if (d.pins[other]?.hold ?? false) throw const HeldError();
    if (needsEpoch() != null) throw const NeedsEpochError();
    final cur = current!;
    if (cur.history == null) throw const NeedsEpochError();
    final Object? parsed;
    try {
      parsed = jsonDecode(utf8.decode(content));
    } on FormatException {
      throw ArgumentError('content must be a JSON object');
    }
    if (parsed is! Map) throw ArgumentError('content must be a JSON object');
    final kind = parsed['kind'] is String ? parsed['kind'] as String : '';
    final Uint8List ref;
    if (reference != null) {
      // Sent again under a replacing record: the same message, so the
      // same reference, and edits and reactions that name it still apply.
      ref = Uint8List.fromList(reference.bytes);
    } else {
      ref = d._random('message_reference', 16);
      // A reference is a UUID; these bits make it a version 4 one.
      ref[6] = (ref[6] & 0x0f) | 0x40;
      ref[8] = (ref[8] & 0x3f) | 0x80;
    }
    final h = Header(
      flags: kind != kindMessage ? flagQuiet : 0,
      conversationId: id,
      keyEpoch: cur.keyEpoch,
      accountId: d.account,
      deviceId: d.id,
      counter: counter + 1,
      reference: Uuid(ref),
    );
    final sk = senderKey(
      contentKey(cur.history!, id, cur.keyEpoch),
      id,
      cur.keyEpoch,
      d.id,
    );
    final sealed = await sealMessage(
      h,
      sk,
      d._random('message_salt', saltSize),
      content,
      d.sign,
    );
    counter++;
    _sentUnder.removeWhere((k, _) => k != cur.keyEpoch);
    _sentUnder.putIfAbsent(cur.keyEpoch, () => []).add((
      h.reference,
      Uint8List.fromList(content),
    ));
    await d._saveChat(this);
    return Outgoing(sealed, h.reference, cur.keyEpoch, h.quiet, h.counter);
  }

  /// `sealed.epoch_started` with `"replaced": true` (EVENTS.md section
  /// 12): in a chat across servers, the other server's record for
  /// [keyEpoch] carried the earlier stamp and stands. It takes the place
  /// of the record this device was given, as when a device loses the
  /// race on its own server: the device takes it and returns, sealed
  /// again under it with the same references and new counters, what it
  /// had sent under the record that gave way, for the app to post.
  ///
  /// Only the newest record can be replaced: any after it would chain to
  /// the one that gave way ([ChainGapError]). A replacing record that
  /// fails the checks stays in the chain as refused and its [EpochError]
  /// is thrown, as from [acceptEpoch]; nothing is resent then.
  Future<List<Outgoing>> replaceEpoch(
    int keyEpoch,
    int seq,
    Uint8List stored,
  ) async {
    final have = epoch(keyEpoch);
    if (have == null) {
      await acceptEpoch(keyEpoch, seq, stored);
      return const [];
    }
    if (bytesEqual(have.stored, stored)) return const [];
    if (!identical(have, current)) throw const ChainGapError();
    final pending = [...?_sentUnder.remove(keyEpoch)];
    epochs.removeLast();
    newSecrets.remove(keyEpoch);
    await acceptEpoch(keyEpoch, seq, stored);
    final out = <Outgoing>[];
    for (final (ref, content) in pending) {
      out.add(await _seal(content, reference: ref));
    }
    if (pending.isEmpty) await _d._saveChat(this);
    return out;
  }

  /// Takes a `sealed.epoch_started` event as the server sent it: a new
  /// record ([acceptEpoch]), or with `replaced` one that takes another's
  /// place ([replaceEpoch]). Returns what must be posted again.
  Future<List<Outgoing>> epochStarted(Map<String, Object?> event) async {
    final e = event['epoch'];
    if (e is! Map) throw const SealedError('the event has no epoch record');
    final keyEpoch = (e['key_epoch'] as num).toInt();
    final seq = ((e['seq'] ?? event['seq']) as num).toInt();
    final stored = unb64(e['record'] as String?);
    if (stored == null) {
      throw const SealedError('the epoch record is not base64');
    }
    if (event['replaced'] == true) return replaceEpoch(keyEpoch, seq, stored);
    await acceptEpoch(keyEpoch, seq, stored);
    return const [];
  }

  /// Takes back the counter of a message the server refused, so it can be
  /// encrypted again in the current epoch with the same counter.
  void unsend(Outgoing o) {
    if (counter == o.counter) counter--;
    _sentUnder[o.keyEpoch]?.removeWhere((x) => x.$1 == o.clientId);
  }

  /// Checks one row (8.2, "Receiving") and, if it passes, applies its
  /// content. A row that fails throws a [MessageError] and is counted in
  /// [unchecked]; a row in an epoch with no key yet throws [NoKeyError].
  Future<Received> receive(Row row) async {
    try {
      return await _receive(row);
    } on MessageError {
      unchecked++;
      rethrow;
    }
  }

  Future<Received> _receive(Row row) async {
    // 1. The header agrees with the row it arrived in.
    final h = Header.decode(row.ciphertext);
    if (h.conversationId != id ||
        h.keyEpoch != row.keyEpoch ||
        h.accountId != row.author) {
      throw const MessageError(
        'the header does not match the row it arrived in',
      );
    }
    if (h.quiet != row.quiet) {
      throw const MessageError('the quiet flag was changed');
    }
    // 2. The device accepted that epoch's record, the message sits in that
    // epoch's part of the sequence, and the sender is a recipient.
    final e = epoch(h.keyEpoch);
    if (e == null || !e.accepted) {
      throw const MessageError("the device did not accept that epoch's record");
    }
    if (row.seq <= e.seq) {
      throw const MessageError("the message comes before its epoch's record");
    }
    final next = epoch(h.keyEpoch + 1);
    if (next != null && row.seq > next.seq) {
      throw const MessageError('the message comes after a newer epoch began');
    }
    final rc = e.record!.recipient(h.deviceId);
    if (rc == null || rc.accountId != h.accountId) {
      throw const MessageError('the sending device is not in that epoch');
    }
    if (e.history == null) throw const NoKeyError();
    // 3. The signature verifies under the device's key from the list the
    // epoch names, and the content decrypts.
    final sk = senderKey(
      contentKey(e.history!, id, e.keyEpoch),
      id,
      e.keyEpoch,
      h.deviceId,
    );
    final (_, content) = await openMessage(
      row.ciphertext,
      sk,
      e.devices[h.deviceId]?.signKey,
    );
    final out = Received(h);
    // 4. Replays: of two rows with one device and counter, the lower
    // sequence number is the message.
    final key = _key(h.deviceId, h.counter);
    final at = _seen[key];
    if (at != null && at == row.seq) {
      out.already = true;
      return out;
    }
    if (at != null) {
      if (at < row.seq) {
        out.dropped = true;
        return out;
      }
      // The copy seen first was the replay; this one takes its place.
      // (The reference removes it from its messages but not from its order,
      // so it would draw the message twice; it is removed from both here.)
      final gone = [
        for (final m in messages.values)
          if (m.seq == at && m.device == h.deviceId) m.reference,
      ];
      for (final ref in gone) {
        messages.remove(ref);
        order.remove(ref);
      }
    }
    _seen[key] = row.seq;
    // 5. Order: the counter should be higher than the last one seen from
    // that device at the nearest lower sequence number.
    final rows = _byDevice.putIfAbsent(h.deviceId, () => []);
    var i = 0;
    while (i < rows.length && rows[i].seq < row.seq) {
      i++;
    }
    if (i < rows.length && rows[i].seq == row.seq) {
      rows[i].counter = h.counter;
    } else {
      rows.insert(i, _Sent(h.deviceId, h.counter, row.seq));
    }
    if (i == 0) {
      out.gap = h.counter > 1;
    } else if (h.counter <= rows[i - 1].counter) {
      out.outOfOrder = true;
    } else {
      out.gap = h.counter > rows[i - 1].counter + 1;
    }
    final Object? parsed;
    try {
      parsed = jsonDecode(utf8.decode(content, allowMalformed: true));
    } on FormatException {
      throw const MessageError('the content is not a JSON object');
    }
    if (parsed != null && parsed is! Map) {
      throw const MessageError('the content is not a JSON object');
    }
    out.content = parsed == null
        ? {}
        : Map<String, Object?>.from(parsed as Map);
    out.kind = out.content['kind'] is String
        ? out.content['kind'] as String
        : '';
    _apply(out, row.seq, e.keyEpoch);
    return out;
  }

  static String _str(Map<String, Object?> m, String k) =>
      m[k] is String ? m[k] as String : '';

  /// Draws one checked message. A kind the device does not know is
  /// ignored, and so is anything that refers to a message it does not have.
  void _apply(Received r, int seq, int keyEpoch) {
    final h = r.header;
    final m = r.content;
    Shown? target() {
      final ref = Uuid.tryParse(
        m['message_id'] is String ? m['message_id'] as String : null,
      );
      return ref == null ? null : messages[ref];
    }

    switch (r.kind) {
      case kindMessage:
        if (messages.containsKey(h.reference)) {
          return; // a reference is used once
        }
        final files = m['files'];
        messages[h.reference] = Shown(
          reference: h.reference,
          author: h.accountId,
          device: h.deviceId,
          seq: seq,
          keyEpoch: keyEpoch,
          text: _str(m, 'text'),
          files: files is List ? List<Object?>.from(files) : [],
          parentId: _str(m, 'parent_id'),
          outOfOrder: r.outOfOrder,
          action: m['action'] == true,
        );
        order.add(h.reference);
        _stableSortOrder();
      case kindEdit:
        final t = target();
        if (t != null && t.author == h.accountId && !t.deleted) {
          t
            ..text = _str(m, 'text')
            ..edited = true;
        }
      case kindDelete:
        final t = target();
        if (t != null && t.author == h.accountId) {
          t
            ..deleted = true
            ..text = ''
            ..files = [];
        }
      case kindReaction:
        final t = target();
        final emoji = _str(m, 'emoji');
        final on = m['on'] == true;
        if (t == null || emoji.isEmpty || t.deleted) return;
        if (!on) {
          t.reactions[emoji]?.remove(h.accountId);
          if (t.reactions[emoji]?.isEmpty ?? false) t.reactions.remove(emoji);
          return;
        }
        if (!t.reactions.containsKey(emoji)) {
          if (t.reactions.length >= 20) return;
          t.reactions[emoji] = <Uuid>{};
        }
        t.reactions[emoji]!.add(h.accountId);
      case kindTask:
        final t = target();
        final item = _str(m, 'item_id');
        final checked = m['checked'] == true;
        if (t != null && item.isNotEmpty && seq > (t._taskSeq[item] ?? 0)) {
          t.tasks[item] = checked;
          t._taskSeq[item] = seq;
        }
      case kindHistory:
        final list = m['secrets'];
        if (list is! List) return;
        for (final raw in list) {
          final entry = raw is Map ? raw : const {};
          final n = entry['key_epoch'] is num
              ? (entry['key_epoch'] as num).toInt()
              : 0;
          final secret = unb64(
            entry['secret'] is String ? entry['secret'] as String : null,
          );
          final e = epoch(n);
          // Each secret is checked against that epoch's record.
          if (e == null ||
              !e.accepted ||
              e.history != null ||
              !checkHistorySecret(secret, e.record!)) {
            continue;
          }
          e.history = secret;
          newSecrets.add(e.keyEpoch);
        }
    }
  }

  void _stableSortOrder() {
    final indexed = [for (var i = 0; i < order.length; i++) (i, order[i])];
    int seqOf(Uuid r) => messages[r]?.seq ?? 0;
    indexed.sort((a, b) {
      final c = seqOf(a.$2).compareTo(seqOf(b.$2));
      return c != 0 ? c : a.$1 - b.$1;
    });
    order
      ..clear()
      ..addAll(indexed.map((x) => x.$2));
  }

  /// The `history` message to send when the other account has a device
  /// again after epochs in which it had none (12.5); null if there is
  /// nothing to share. Nothing from before a reset of the other account's
  /// list is ever shared.
  Uint8List? historyShare() {
    shareDue = false;
    final reset = _d.pins[other]?.lastReset ?? 0;
    final secrets = <HistorySecretEntry>[];
    for (final e in epochs) {
      if (!e.accepted || e.history == null || e.record!.hasAccount(other)) {
        continue;
      }
      if ((e.record!.ref(other)?.listVersion ?? 0) < reset) continue;
      secrets.add(HistorySecretEntry(e.keyEpoch, e.history!));
    }
    return secrets.isEmpty ? null : historyContent(secrets);
  }

  /// For a call in the current epoch: each device's call sender key and
  /// the key index every frame carries (section 10).
  (Map<Uuid, Uint8List>, int) callKeys(Uuid call) {
    final cur = current;
    if (cur == null || !cur.accepted || cur.secret == null) {
      throw const SealedError(
        'the device does not hold the current epoch secret',
      );
    }
    final secret = callSecret(cur.secret!, id, cur.keyEpoch, call);
    return (
      {
        for (final r in cur.record!.recipients)
          r.deviceId: callSenderKey(secret, id, cur.keyEpoch, r.deviceId),
      },
      callKeyIndex(cur.keyEpoch),
    );
  }

  /// What the device shows, in order.
  List<Shown> view() => [
    for (final r in order)
      if (messages[r] != null) messages[r]!,
  ];
}
