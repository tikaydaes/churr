// Where a device's state is kept. The library writes opaque byte strings
// under string keys through [SealedStore]; the app backs it with its
// encrypted local store. Everything a device holds is written: its own
// keys, the backup key and account key, pins and verified marks, every
// chat's epochs, secrets, send counter, replay records and what it shows.
//
// The values are JSON with bytes in base64url. They hold every secret the
// device has, so the store must encrypt them at rest.

part of 'device.dart';

/// Byte strings under string keys.
abstract class SealedStore {
  Future<Uint8List?> read(String key);
  Future<void> write(String key, Uint8List value);
  Future<void> delete(String key);
  Future<List<String>> keys();
}

/// A store in memory, for tests and for a device that keeps nothing.
class MemoryStore implements SealedStore {
  final Map<String, Uint8List> values = {};
  @override
  Future<Uint8List?> read(String key) async => values[key];
  @override
  Future<void> write(String key, Uint8List value) async =>
      values[key] = Uint8List.fromList(value);
  @override
  Future<void> delete(String key) async => values.remove(key);
  @override
  Future<List<String>> keys() async => values.keys.toList();
}

const _deviceKey = 'sealed/device';
String _chatKey(Uuid id) => 'sealed/chat/$id';

String? _b(Uint8List? v) => v == null ? null : b64(v);
Uint8List? _ub(Object? v) => v == null ? null : unb64(v as String);
Uint8List _ubr(Object? v) => unb64(v as String)!;

Uint8List _jsonBytes(Object? v) =>
    Uint8List.fromList(utf8.encode(jsonEncode(v)));
Map<String, Object?> _jsonOf(Uint8List b) =>
    (jsonDecode(utf8.decode(b)) as Map).cast<String, Object?>();

extension DeviceStorage on Device {
  /// Writes the whole device: its own state and every chat.
  Future<void> save() async {
    final s = store;
    if (s == null) return;
    await s.write(_deviceKey, _jsonBytes(_deviceJson()));
    for (final c in chats.values) {
      await s.write(_chatKey(c.id), _jsonBytes(c._json()));
    }
  }

  /// Wipes everything the device has written: what signing out does.
  Future<void> wipe() async {
    final s = store;
    if (s == null) return;
    for (final k in await s.keys()) {
      if (k.startsWith('sealed/')) await s.delete(k);
    }
  }

  Map<String, Object?> _deviceJson() => {
    'v': 1,
    'account': '$account',
    'server': server,
    'id': '$id',
    'platform': platform,
    'added_at': addedAt,
    'sign_seed': b64(sign.seed),
    'box': b64(box),
    'backup_key': _b(backupKey),
    'key_version': keyVersion,
    'account_key_seed': _b(accountKey?.seed),
    'record': record == null ? null : b64(record!.encode()),
    'pins': {
      for (final e in pins.entries)
        '${e.key}': {
          'lowest': e.value.lowest,
          'newest': e.value.newest,
          'last_reset': e.value.lastReset,
          'verified': e.value.verified,
          'hold': e.value.hold,
          'versions': [
            for (final v in e.value.versions.values) b64(v.signed.encode()),
          ],
        },
    },
    'notices': [
      for (final n in notices)
        {
          'kind': n.kind,
          'account': '${n.account}',
          'platform': n.platform,
          'conversation': n.conversation?.toString(),
        },
    ],
  };

  /// Writes one chat; what [Chat.seal] does before it returns a message,
  /// so a raised counter is never lost. The device itself is written too
  /// if the store has none yet.
  Future<void> _saveChat(Chat c) async {
    final s = store;
    if (s == null) return;
    if (await s.read(_deviceKey) == null) {
      await s.write(_deviceKey, _jsonBytes(_deviceJson()));
    }
    await s.write(_chatKey(c.id), _jsonBytes(c._json()));
  }
}

/// Reads a device back from [store]; null if it holds none.
Future<Device?> loadDevice(
  SealedStore store, {
  required SealedRandom random,
  required int Function() now,
}) async {
  final raw = await store.read(_deviceKey);
  if (raw == null) return null;
  final j = _jsonOf(raw);
  final d =
      Device._(
          Uuid.parse(j['account'] as String),
          j['server'] as String,
          Uuid.parse(j['id'] as String),
          j['platform'] as int,
          j['added_at'] as int,
          await SigningKey.fromSeed(_ubr(j['sign_seed'])),
          _ubr(j['box']),
          random,
          now,
        )
        ..store = store
        ..backupKey = _ub(j['backup_key'])
        ..keyVersion = j['key_version'] as int;
  final seed = _ub(j['account_key_seed']);
  if (seed != null) d.accountKey = await SigningKey.fromSeed(seed);
  final rec = _ub(j['record']);
  if (rec != null) d.record = AccountRecord.decode(rec);
  (j['pins'] as Map).forEach((k, v) {
    final p = v as Map;
    final versions = <int, PinnedList>{};
    for (final s in p['versions'] as List) {
      final signed = SignedList.decode(_ubr(s));
      final l = DeviceList.decode(signed.list);
      versions[l.version] = PinnedList(signed, l);
    }
    d.pins[Uuid.parse(k as String)] = Pin(
      versions,
      p['lowest'] as int,
      p['newest'] as int,
      lastReset: p['last_reset'] as int,
      verified: p['verified'] as bool,
      hold: p['hold'] as bool,
    );
  });
  for (final n in j['notices'] as List) {
    final m = n as Map;
    d.notices.add(
      Notice(
        m['kind'] as String,
        Uuid.parse(m['account'] as String),
        platform: m['platform'] as int,
        conversation: Uuid.tryParse(m['conversation'] as String?),
      ),
    );
  }
  for (final k in await store.keys()) {
    if (!k.startsWith('sealed/chat/')) continue;
    final cj = await store.read(k);
    if (cj != null) _loadChat(d, _jsonOf(cj));
  }
  return d;
}

extension on Chat {
  Map<String, Object?> _json() => {
    'v': 1,
    'id': '$id',
    'other': '$other',
    'counter': counter,
    'share_due': shareDue,
    'unchecked': unchecked,
    'new_secrets': newSecrets,
    'epochs': [
      for (final e in epochs)
        {
          'key_epoch': e.keyEpoch,
          'seq': e.seq,
          'stored': b64(e.stored),
          'accepted': e.accepted,
          'refusal': e.refusal?.message,
          'refusal_rule': e.refusal is EpochError
              ? (e.refusal as EpochError).rule
              : null,
          'secret': _b(e.secret),
          'history': _b(e.history),
        },
    ],
    'seen': _seen,
    'by_device': {
      for (final x in _byDevice.entries)
        '${x.key}': [
          for (final s in x.value) [s.counter, s.seq],
        ],
    },
    'messages': [
      for (final r in order)
        if (messages[r] != null) _shownJson(messages[r]!),
    ],
    if (_sentUnder.isNotEmpty)
      'sent_under': {
        for (final e in _sentUnder.entries)
          '${e.key}': [
            for (final (ref, content) in e.value) ['$ref', b64(content)],
          ],
      },
  };
}

Map<String, Object?> _shownJson(Shown m) => {
  'reference': '${m.reference}',
  'author': '${m.author}',
  'device': '${m.device}',
  'seq': m.seq,
  'key_epoch': m.keyEpoch,
  'text': m.text,
  'files': m.files,
  'parent_id': m.parentId,
  'edited': m.edited,
  'deleted': m.deleted,
  'out_of_order': m.outOfOrder,
  'reactions': {
    for (final e in m.reactions.entries) e.key: [for (final a in e.value) '$a'],
  },
  'tasks': m.tasks,
  'task_seq': m._taskSeq,
};

void _loadChat(Device d, Map<String, Object?> j) {
  final c =
      d.chat(Uuid.parse(j['id'] as String), Uuid.parse(j['other'] as String))
        ..counter = j['counter'] as int
        ..shareDue = j['share_due'] as bool
        ..unchecked = j['unchecked'] as int;
  c.newSecrets.addAll((j['new_secrets'] as List).cast<int>());
  for (final x in j['epochs'] as List) {
    final m = x as Map;
    final e = Epoch(m['key_epoch'] as int, m['seq'] as int, _ubr(m['stored']))
      ..accepted = m['accepted'] as bool
      ..secret = _ub(m['secret'])
      ..history = _ub(m['history']);
    if (m['refusal'] != null) {
      final rule = m['refusal_rule'] as int?;
      e.refusal = rule != null
          ? EpochError(rule, m['refusal'] as String)
          : SealedError(m['refusal'] as String);
    }
    try {
      e.record = EpochRecord.decode(splitStoredEpoch(e.stored).$1);
    } on RecordError {
      e.record = null;
    }
    if (e.accepted) {
      // The recipients' keys come from the lists the record names, which
      // the device has pinned.
      for (final a in e.record!.accounts) {
        final raw = d.listOf(a.accountId, a.listVersion);
        if (raw == null) continue;
        for (final dev in DeviceList.decode(raw).devices) {
          e.devices[dev.deviceId] = dev;
        }
      }
    }
    c.epochs.add(e);
  }
  (j['seen'] as Map).forEach((k, v) => c._seen[k as String] = v as int);
  (j['by_device'] as Map).forEach((k, v) {
    final dev = Uuid.parse(k as String);
    c._byDevice[dev] = [
      for (final p in v as List) _Sent(dev, (p as List)[0] as int, p[1] as int),
    ];
  });
  for (final x in j['messages'] as List) {
    final m = x as Map;
    final s =
        Shown(
            reference: Uuid.parse(m['reference'] as String),
            author: Uuid.parse(m['author'] as String),
            device: Uuid.parse(m['device'] as String),
            seq: m['seq'] as int,
            keyEpoch: m['key_epoch'] as int,
            text: m['text'] as String,
            files: List<Object?>.from(m['files'] as List),
            parentId: m['parent_id'] as String,
            outOfOrder: m['out_of_order'] as bool,
          )
          ..edited = m['edited'] as bool
          ..deleted = m['deleted'] as bool;
    (m['reactions'] as Map).forEach(
      (k, v) => s.reactions[k as String] = {
        for (final a in v as List) Uuid.parse(a as String),
      },
    );
    (m['tasks'] as Map).forEach((k, v) => s.tasks[k as String] = v as bool);
    (m['task_seq'] as Map).forEach(
      (k, v) => s._taskSeq[k as String] = v as int,
    );
    c.messages[s.reference] = s;
    c.order.add(s.reference);
  }
  (j['sent_under'] as Map? ?? const {}).forEach((k, v) {
    c._sentUnder[int.parse(k as String)] = [
      for (final x in v as List)
        (Uuid.parse((x as List)[0] as String), _ubr(x[1])),
    ];
  });
}
