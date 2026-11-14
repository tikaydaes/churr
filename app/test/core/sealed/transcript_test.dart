// Replays testdata/sealed/transcript.json step by step "as a writer"
// (testdata/sealed/README.md section 3): the conversation of
// internal/sealed/ref/transcript.go is played again by this library's
// Devices, each fed the random values the transcript lists for it, against
// a stand-in server that only files what it is given. After every step:
//
// - every value drawn matches the transcript's list (purpose, length and
//   order), and none is left over;
// - what the server stored equals the step's `stored`, byte for byte;
// - what was refused equals `refused`;
// - the plaintext of a posted message equals `sent`;
// - every device's state equals its entry in `after`;
// - every device is then saved to a MemoryStore and loaded back, and the
//   replay carries on with the loaded copy, which checks that storage
//   keeps everything the state machine needs.

import 'dart:convert';
import 'dart:typed_data';

import 'package:churr/core/sealed/sealed.dart';
import 'package:flutter_test/flutter_test.dart';

import 'vectors_helper.dart';

final ann = Uuid.parse('0193a000-0000-7000-8000-00000000000a');
final bob = Uuid.parse('0193a000-0000-7000-8000-00000000000b');
final conv = Uuid.parse('0193a0e0-0000-7000-8000-0000000000c0');
final passkeyId = Uuid.parse('0193a0aa-0000-7000-8000-0000000000f1');
const server = 'chat.example.org';
const start = 1800000000000;
const day = 24 * 60 * 60 * 1000;

/// The reference's fixed stream (README section 1), used only for the
/// values no device draws: the passkey's PRF output and credential, and
/// the intruder's forged record.
Uint8List fixed(String seed, int n) {
  final out = BytesBuilder();
  for (var i = 0; out.length < n; i++) {
    out.add(
      sha256(
        cat([
          utf8.encode('churr test stream v1'),
          [0],
          utf8.encode(seed),
          u64be(i),
        ]),
      ),
    );
  }
  return out.takeBytes().sublist(0, n);
}

class Item {
  final int seq;
  final int keyEpoch;
  final Uint8List? stored; // an epoch record
  Row? row; // or a message
  bool erased = false;
  Item(this.seq, this.keyEpoch, {this.stored, this.row});
}

class SimDevice {
  final String name;
  Device d;
  final GivenRandom random;
  int pos = 0;
  bool gone = false;
  SimDevice(this.name, this.d, this.random);
}

class SimBackup {
  int keyVersion, revision;
  Uint8List record;
  List<Copy> copies;
  List<StoredRecord> records;
  SimBackup(
    this.keyVersion,
    this.revision,
    this.record,
    this.copies,
    this.records,
  );
}

/// Deep equality of decoded JSON, reporting the first path that differs
/// and, for hex strings, the first differing byte offset.
String? diff(Object? got, Object? want, String path) {
  if (want is Map) {
    if (got is! Map) return '$path: got $got, want an object';
    for (final k in {...want.keys, ...got.keys}) {
      final d = diff(got[k], want[k], '$path.$k');
      if (d != null) return d;
    }
    return null;
  }
  if (want is List) {
    if (got is! List) return '$path: got $got, want a list';
    for (var i = 0; i < want.length || i < got.length; i++) {
      if (i >= got.length || i >= want.length) return '$path: got ${got.length} items, want ${want.length}';
      final d = diff(got[i], want[i], '$path[$i]');
      if (d != null) return d;
    }
    return null;
  }
  if (got == want) return null;
  if (got is String &&
      want is String &&
      RegExp(r'^[0-9a-f]+$').hasMatch(want) &&
      want.length > 40) {
    try {
      sameBytes(unhex(got), unhex(want), path);
    } on Mismatch catch (e) {
      return e.message;
    }
  }
  return '$path: got ${jsonEncode(got)}, want ${jsonEncode(want)}';
}

class Sim {
  final Map<String, dynamic> transcript;
  final List steps;
  int now = start;
  final Map<Uuid, List<SignedList>> lists = {};
  final Map<Uuid, SimBackup> backups = {};
  final List<Item> items = [];
  int nextSeq = 0;
  int epochNow = 0;
  final List<SimDevice> devices = [];
  final Map<String, List<(String, Uint8List)>> randomOf = {};
  final Map<String, List<int>> drawsBy =
      {}; // cumulative draws per device after each step

  // The step being played.
  int stepNo = 0;
  List<Map<String, Object?>> stored = [];
  List<Map<String, Object?>> refusedNow = [];
  Map<String, Object?>? sent;
  final List<String> failures = [];
  int stepsMatched = 0;
  int comparisons = 0;

  Sim(this.transcript) : steps = transcript['steps'] as List {
    for (final s in steps) {
      for (final r in s['random'] as List) {
        randomOf.putIfAbsent(r['device'], () => []).add((
          r['for'] as String,
          hx(r['value']),
        ));
      }
    }
    // How many values each device has drawn once each step is over.
    for (final name in randomOf.keys) {
      var n = 0;
      drawsBy[name] = [
        for (final s in steps)
          n += (s['random'] as List).where((r) => r['device'] == name).length,
      ];
    }
  }

  Future<SimDevice> device(
    String name,
    Uuid account,
    int platform,
    String devId,
  ) async {
    final random = GivenRandom(randomOf[name] ?? []);
    final d = await Device.create(
      account: account,
      server: server,
      platform: platform,
      random: random,
      now: () => now,
      id: Uuid.parse(devId),
    );
    final dev = SimDevice(name, d, random);
    _wire(dev);
    devices.add(dev);
    return dev;
  }

  void _wire(SimDevice dev) {
    dev.d.fetchLists = (account, after) async => listsAfter(account, after);
    dev.d.now = () => now;
    dev.d.store ??= MemoryStore();
  }

  List<SignedList> listsAfter(Uuid account, int after) {
    final all = lists[account] ?? [];
    return [
      for (var i = 0; i < all.length; i++)
        if (i + 1 > after) all[i],
    ];
  }

  Uuid other(Uuid account) => account == ann ? bob : ann;
  Chat chatOf(SimDevice dev) => dev.d.chat(conv, other(dev.d.account));

  void begin() {
    stored = [];
    refusedNow = [];
    sent = null;
  }

  void store(Map<String, Object?> e) => stored.add(e);
  void refuse(Map<String, Object?> e) => refusedNow.add(e);

  void publish(SignedList l) {
    final parsed = DeviceList.decode(l.list);
    final have = lists.putIfAbsent(parsed.accountId, () => []);
    if (parsed.version != have.length + 1) throw StateError('not the next list version');
    have.add(l);
    store({
      'table': 'device_lists',
      'account_id': '${parsed.accountId}',
      'version': parsed.version,
      'signed_list': hexOf(l.encode()),
    });
  }

  void storeCopies(Uuid account, int keyVersion, List<Copy> copies) {
    for (final c in copies) {
      store({
        'table': 'backup_keys',
        'account_id': '$account',
        'kind': wayNames[c.kind],
        'key_version': keyVersion,
        'locked_key': hexOf(c.locked),
        if (c.kind == wayPasskey) 'passkey_id': '${c.id}',
        if (c.kind == wayDevice) 'device_id': '${c.id}',
      });
    }
  }

  Future<void> putBundle(SimDevice dev, Bundle b, String how) async {
    final account = dev.d.account;
    if (how != 'create') {
      store({
        'deleted': ['account_records', 'backup_keys', 'key_backups'],
        'account_id': '$account',
        'because': how,
      });
    }
    backups[account] = SimBackup(
      b.keyVersion,
      b.revision,
      b.accountRecord,
      [...b.copies],
      [...b.records],
    );
    store({
      'table': 'account_records',
      'account_id': '$account',
      'key_version': b.keyVersion,
      'revision': b.revision,
      'record': hexOf(b.accountRecord),
    });
    storeCopies(account, b.keyVersion, b.copies);
    for (final r in b.records) {
      store({
        'table': 'key_backups',
        'account_id': '$account',
        'conversation_id': '${r.conversationId}',
        'key_epoch': r.keyEpoch,
        'key_version': r.keyVersion,
        'record': hexOf(r.locked),
      });
    }
    publish(b.list);
    await dev.d.adopt(b);
  }

  StoredBackup storedOf(Uuid account) {
    final b = backups[account]!;
    return StoredBackup(b.keyVersion, b.revision, b.record);
  }

  Uint8List copyOf(Uuid account, int kind, Uuid cid) => backups[account]!.copies
      .firstWhere((c) => c.kind == kind && c.id == cid)
      .locked;

  Future<void> learn(SimDevice dev) async {
    for (final account in [ann, bob]) {
      final all = lists[account] ?? [];
      if (all.isEmpty) continue;
      final p = dev.d.pins[account];
      final give = p == null ? [all.last] : listsAfter(account, p.newest);
      if (give.isEmpty) continue;
      try {
        await dev.d.learnLists(account, give);
      } on SealedError catch (e) {
        refuse({
          'device': dev.name,
          'refused': 'device_list',
          'account_id': '$account',
          'why': e.message,
        });
      }
    }
  }

  Future<void> sync(SimDevice dev) async {
    if (dev.gone) return;
    await learn(dev);
    final c = chatOf(dev);
    for (; dev.pos < items.length; dev.pos++) {
      final it = items[dev.pos];
      if (it.stored != null) {
        try {
          await c.acceptEpoch(it.keyEpoch, it.seq, it.stored!);
        } on SealedError catch (e) {
          refuse({
            'device': dev.name,
            'refused': 'epoch_record',
            'key_epoch': it.keyEpoch,
            'why': e.message,
          });
        }
        continue;
      }
      if (it.erased) continue; // a deletion marker
      try {
        await c.receive(it.row!);
      } on NoKeyError {
        // read once the secret arrives
      } on SealedError catch (e) {
        refuse({
          'device': dev.name,
          'refused': 'message',
          'seq': it.seq,
          'why': e.message,
        });
      }
    }
    upload(dev);
  }

  void upload(SimDevice dev) {
    if (!dev.d.unlocked) return;
    final recs = chatOf(dev).takeNewSecrets();
    final b = backups[dev.d.account]!;
    for (final r in recs) {
      if (b.records.any((o) => o.conversationId == r.conversationId && o.keyEpoch == r.keyEpoch,)) continue;
      b.records.add(r);
      store({
        'table': 'key_backups',
        'account_id': '${dev.d.account}',
        'conversation_id': '${r.conversationId}',
        'key_epoch': r.keyEpoch,
        'key_version': r.keyVersion,
        'record': hexOf(r.locked),
      });
    }
  }

  Future<void> syncAll() async {
    for (final dev in devices) {
      await sync(dev);
    }
  }

  Future<void> readBackup(SimDevice dev) async {
    for (final r in backups[dev.d.account]!.records) {
      final e = chatOf(dev).epoch(r.keyEpoch);
      if (e != null && e.history == null) {
        try {
          chatOf(dev).readBackupRecord(r);
        } on SealedError catch (err) {
          refuse({
            'device': dev.name,
            'refused': 'backup_record',
            'key_epoch': r.keyEpoch,
            'why': err.message,
          });
        }
      }
    }
    await reread(dev);
  }

  Future<void> reread(SimDevice dev) async {
    final c = chatOf(dev);
    for (final it in items.sublist(0, dev.pos)) {
      if (it.row == null || it.erased) continue;
      final h = Header.decode(it.row!.ciphertext);
      if (c.hasSeen(h.deviceId, h.counter)) continue;
      try {
        await c.receive(it.row!);
      } on SealedError {
        // as the reference: ignored here
      }
    }
  }

  Future<bool> makeEpoch(SimDevice dev) async {
    final (st, keyEpoch) = await chatOf(dev).makeEpoch();
    return postEpoch(dev, keyEpoch, st);
  }

  bool postEpoch(SimDevice dev, int keyEpoch, Uint8List st) {
    if (keyEpoch != epochNow + 1) {
      refuse({
        'device': dev.name,
        'refused': 'epoch_record',
        'by': 'server',
        'code': 'conflict',
        'key_epoch': keyEpoch,
        'record': hexOf(st),
        'why': 'another record already holds this key epoch',
      });
      return false;
    }
    forceEpoch(keyEpoch, st);
    return true;
  }

  Uint8List lastEpoch() => items.lastWhere((i) => i.stored != null).stored!;

  void forceEpoch(int keyEpoch, Uint8List st) {
    epochNow = keyEpoch;
    nextSeq++;
    items.add(Item(nextSeq, keyEpoch, stored: st));
    store({
      'table': 'sealed_epochs',
      'conversation_id': '$conv',
      'key_epoch': keyEpoch,
      'seq': nextSeq,
      'record': hexOf(st),
    });
  }

  Future<Uuid> send(SimDevice dev, Uint8List content) async {
    final out = await chatOf(dev).seal(content);
    if (out.keyEpoch != epochNow) throw StateError('a message in a stale epoch');
    nextSeq++;
    final row = Row(
      seq: nextSeq,
      author: dev.d.account,
      keyEpoch: out.keyEpoch,
      quiet: out.quiet,
      ciphertext: out.ciphertext,
    );
    items.add(Item(nextSeq, 0, row: row));
    sent = {
      'content': utf8.decode(content),
      'reference': '${out.clientId}',
      'counter': out.counter,
      'key_epoch': out.keyEpoch,
      'quiet': out.quiet,
    };
    store({
      'table': 'messages',
      'conversation_id': '$conv',
      'seq': nextSeq,
      'author_id': '${dev.d.account}',
      'client_id': '${out.clientId}',
      'key_epoch': out.keyEpoch,
      'quiet': out.quiet,
      'ciphertext': hexOf(out.ciphertext),
    });
    return out.clientId;
  }

  void erase(Uuid ref) {
    for (final it in items) {
      if (it.row != null &&
          !it.erased &&
          Header.decode(it.row!.ciphertext).reference == ref) {
        it.erased = true;
        store({
          'erased': 'messages.ciphertext',
          'seq': it.row!.seq,
          'because': 'deleted by its author',
        });
      }
    }
  }

  Future<void> join(SimDevice dev) async {
    await learn(dev);
    final list = await dev.d.listWithSelf();
    publish(list);
    await dev.d.learnLists(dev.d.account, [list]);
    await sync(dev);
    await readBackup(dev);
    if (items.isNotEmpty) await makeEpoch(dev);
  }

  Map<String, Object?> view(SimDevice dev) {
    final d = dev.d;
    final c = d.chats[conv];
    return {
      'unlocked': d.unlocked,
      'key_version': d.keyVersion,
      'pins': {for (final e in d.pins.entries) '${e.key}': e.value.newest},
      'epochs': [
        for (final e in c?.epochs ?? <Epoch>[])
          {
            'key_epoch': e.keyEpoch,
            'accepted': e.accepted,
            'can_read': e.history != null,
          },
      ],
      'shows': [
        for (final m in c?.view() ?? <Shown>[])
          {
            'reference': '${m.reference}',
            'author_id': '${m.author}',
            'seq': m.seq,
            'text': m.text,
            if (m.edited) 'edited': true,
            if (m.deleted) 'deleted': true,
            if (m.outOfOrder) 'out_of_order': true,
            if (m.reactions.isNotEmpty)
              'reactions': {
                for (final e in m.reactions.entries)
                  e.key: ([for (final a in e.value) '$a']..sort()),
              },
            if (m.tasks.isNotEmpty) 'tasks': m.tasks,
          },
      ],
      'unchecked': c?.unchecked ?? 0,
      'notices': [
        for (final n in d.notices)
          {
            'kind': n.kind,
            'account_id': '${n.account}',
            if (n.platform != 0) 'platform': platformNames[n.platform],
          },
      ],
    };
  }

  /// Ends a step: compares everything with the transcript, then saves and
  /// reloads every device.
  Future<void> end() async {
    final want = steps[stepNo] as Map<String, dynamic>;
    stepNo++;
    final where = 'step $stepNo';
    final problems = <String>[];
    void cmp(Object? got, Object? w, String what) {
      comparisons++;
      final d = diff(got, w, '$where $what');
      if (d != null) problems.add(d);
    }

    cmp(now, want['clock'], 'clock');
    cmp(stored, want['stored'], 'stored');
    cmp(sent, want['sent'], 'sent');
    // A refusal's `why` is the reference's error text with its "ref: "
    // prefix; this library's messages are the same text.
    final wantRefused = [
      for (final r in (want['refused'] as List?) ?? [])
        {...(r as Map), 'why': (r['why'] as String).replaceFirst('ref: ', '')},
    ];
    cmp(refusedNow, wantRefused, 'refused');
    final after = want['after'] as Map;
    final gotAfter = {
      for (final dev in devices)
        if (!dev.gone) dev.name: view(dev),
    };
    cmp(
      gotAfter.keys.toList()..sort(),
      after.keys.cast<String>().toList()..sort(),
      'devices in after',
    );
    for (final name in after.keys) {
      cmp(gotAfter[name], after[name], 'after.$name');
    }
    for (final dev in devices) {
      final drawn = drawsBy[dev.name]?[stepNo - 1] ?? 0;
      cmp(dev.random.used, drawn, 'random values drawn by ${dev.name}');
    }
    if (problems.isEmpty) {
      stepsMatched++;
    } else {
      failures.addAll(problems);
    }
    // Save and load every device; carry on with the loaded copy.
    for (final dev in devices) {
      if (dev.gone) continue;
      await dev.d.save();
      final loaded = await loadDevice(
        dev.d.store!,
        random: dev.random,
        now: () => now,
      );
      loaded!.store = dev.d.store;
      dev.d = loaded;
      _wire(dev);
      final d = diff(
        view(dev),
        gotAfter[dev.name],
        '$where after reload ${dev.name}',
      );
      if (d != null) failures.add(d);
    }
  }

  Future<Uuid> say(SimDevice dev, Uint8List content) async {
    begin();
    final ref = await send(dev, content);
    await syncAll();
    await end();
    return ref;
  }
}

Uint8List text(String t) => messageContent(t);

void main() {
  test('transcript.json, replayed step by step', () async {
    final t = loadVector('transcript.json');
    final s = Sim(t);
    final cast = {for (final d in t['devices'] as List) d['name']: d};
    Future<SimDevice> dev(String name) {
      final c = cast[name];
      return s.device(
        name,
        id(c['account_id']),
        platformNumber(c['platform']),
        c['device_id'],
      );
    }

    // Setting up: each account's first device makes its backup.
    final annPhone = await dev('ann-phone');
    s.begin();
    final annBundle = await annPhone.d.createBackup();
    await s.putBundle(annPhone, annBundle, 'create');
    await s.end();
    final annPhrase = annBundle.phrase;

    final bobPhone = await dev('bob-phone');
    s.begin();
    final bobBundle = await bobPhone.d.createBackup();
    await s.putBundle(bobPhone, bobBundle, 'create');
    await s.end();
    final bobPhrase = bobBundle.phrase;

    // A second device each, before the chat exists.
    final annLaptop = await dev('ann-laptop');
    s.begin();
    await s.learn(annLaptop);
    await annLaptop.d.unlockWithPhrase(
      annPhrase,
      s.copyOf(ann, wayPhrase, Uuid.zero),
      s.storedOf(ann),
    );
    await s.join(annLaptop);
    await s.syncAll();
    await s.end();

    final bobDesktop = await dev('bob-desktop');
    s.begin();
    await s.learn(bobDesktop);
    final (request, code) = await bobDesktop.d.newUnlockRequest();
    final answer = await bobPhone.d.answerUnlockRequest(request, code);
    s.store({
      'table': 'unlock_requests',
      'account_id': '$bob',
      'device_id': '${bobDesktop.d.id}',
      'request': hexOf(request),
      'code_shown': code,
      'answer': hexOf(answer),
    });
    await bobDesktop.d.unlockWithDeviceCopy(answer, s.storedOf(bob));
    await s.join(bobDesktop);
    await s.syncAll();
    await s.end();

    // Creation: epoch 1.
    s.begin();
    await s.learn(annPhone);
    await s.makeEpoch(annPhone);
    await s.syncAll();
    await s.end();

    // Messages from every device, and every kind of content.
    final hello = await s.say(annPhone, text('Hello Bob'));
    await s.say(bobPhone, text('Hello Ann'));
    final list = await s.say(
      annLaptop,
      text('- [ ] bring the keys\n- [ ] lock up'),
    );
    final typo = await s.say(bobDesktop, text('See you at sevn'));
    await s.say(bobPhone, reactionContent(hello, '👍', true));
    await s.say(bobDesktop, editContent(typo, 'See you at seven'));
    await s.say(annPhone, taskContent(list, 't1', true));
    final oops = await s.say(annLaptop, text('wrong chat, sorry'));
    s.begin();
    await s.send(annLaptop, deleteContent(oops));
    s.erase(oops);
    await s.syncAll();
    await s.end();

    // A device is added, through a passkey.
    final prf = fixed('transcript passkey prf output', 32);
    final credential = fixed('transcript passkey credential id', 20);
    s.begin();
    final unlockPublic = x25519Public(unlockKeyFromPrf(prf, ann, credential));
    final up = await annPhone.d.approvePasskey(passkeyId, unlockPublic);
    final ab = s.backups[ann]!;
    ab.revision = up.revision;
    ab.record = up.accountRecord;
    ab.copies.add(up.copy);
    annPhone.d.adoptRecord(up);
    s.store({
      'passkey_id': '$passkeyId',
      'credential_id': hexOf(credential),
      'prf_output': hexOf(prf),
      'unlock_public': hexOf(unlockPublic),
      'note': 'not stored by the server except unlock_public; given here so the unlock key can be derived',
    });
    s.store({
      'table': 'account_records',
      'account_id': '$ann',
      'key_version': ab.keyVersion,
      'revision': up.revision,
      'record': hexOf(up.accountRecord),
    });
    s.storeCopies(ann, ab.keyVersion, [up.copy]);
    await s.end();

    final annTablet = await dev('ann-tablet');
    s.begin();
    await s.learn(annTablet);
    await annTablet.d.unlockWithPrf(
      prf,
      credential,
      passkeyId,
      s.copyOf(ann, wayPasskey, passkeyId),
      s.storedOf(ann),
    );
    await s.join(annTablet);
    await s.syncAll();
    await s.end();
    await s.say(annTablet, text('Hello from the tablet'));

    // A device is removed.
    s.begin();
    final without = await bobDesktop.d.listWithout([bobDesktop.d.id]);
    s.publish(without);
    await s.learn(bobPhone);
    await s.makeEpoch(bobPhone);
    await s.syncAll();
    await s.end();
    final afterRemoval = await s.say(bobPhone, text('Only my phone now'));
    expect(
      s.chatOf(bobDesktop).messages[afterRemoval],
      isNull,
      reason: 'a removed device read a later message',
    );
    final e3 = s.chatOf(bobDesktop).epoch(3);
    expect(
      e3 != null && e3.accepted && e3.history == null,
      isTrue,
      reason: 'a removed device has a key for epoch 3',
    );
    bobDesktop.gone = true;

    // An epoch made for age.
    s.now += 8 * day;
    s.begin();
    expect(s.chatOf(annLaptop).needsEpoch(), 'age');
    await s.makeEpoch(annLaptop);
    await s.syncAll();
    await s.end();
    await s.say(annLaptop, text('A week later'));

    // Two devices make the same epoch at once.
    s.now += 8 * day;
    s.begin();
    final (annFive, _) = await s.chatOf(annPhone).makeEpoch();
    final (bobFive, _) = await s.chatOf(bobPhone).makeEpoch();
    s.postEpoch(annPhone, 5, annFive);
    s.postEpoch(bobPhone, 5, bobFive);
    await s.syncAll();
    expect(
      s.chatOf(bobPhone).needsEpoch(),
      isNull,
      reason: 'the loser of the race still wants an epoch',
    );
    await s.end();
    await s.say(bobPhone, text('Still here'));

    // An account with no device for two epochs, then a history share.
    s.begin();
    final empty = await bobPhone.d.listWithout([bobPhone.d.id]);
    s.publish(empty);
    bobPhone.gone = true;
    await s.learn(annPhone);
    await s.makeEpoch(annPhone);
    await s.syncAll();
    await s.end();
    await s.say(annPhone, text('Call me when you are back'));
    s.now += 8 * day;
    s.begin();
    await s.makeEpoch(annLaptop);
    await s.syncAll();
    await s.end();
    await s.say(annLaptop, text('Any news?'));

    final bobNew = await dev('bob-new-phone');
    s.begin();
    await s.learn(bobNew);
    await bobNew.d.unlockWithPhrase(
      bobPhrase,
      s.copyOf(bob, wayPhrase, Uuid.zero),
      s.storedOf(bob),
    );
    await s.join(bobNew);
    await s.syncAll();
    await s.end();
    s.begin();
    expect(
      s.chatOf(annPhone).shareDue,
      isTrue,
      reason: 'no history share is due',
    );
    final share = s.chatOf(annPhone).historyShare();
    expect(share, isNotNull);
    await s.send(annPhone, share!);
    await s.syncAll();
    await s.reread(bobNew);
    s.upload(bobNew);
    await s.end();
    await s.say(bobNew, text('Back now, calling'));

    // A key replacement.
    s.begin();
    annTablet.gone = true;
    final replacement = await annPhone.d.replaceBackup(
      Replacement(devices: [annTablet.d.id]),
      s.backups[ann]!.records,
    );
    await s.putBundle(annPhone, replacement, 'replace');
    await s.makeEpoch(annPhone);
    await s.learn(annLaptop);
    await annLaptop.d.unlockWithDeviceCopy(
      s.copyOf(ann, wayDevice, annLaptop.d.id),
      s.storedOf(ann),
    );
    await s.syncAll();
    await s.end();
    await s.say(annLaptop, text('Lost my tablet; it is revoked'));

    // A reset.
    final bobReset = await dev('bob-reset-phone');
    s.begin();
    bobNew.gone = true;
    await s.learn(bobReset);
    final reset = await bobReset.d.resetBackup();
    await s.putBundle(bobReset, reset, 'reset');
    await s.sync(bobReset);
    await s.makeEpoch(bobReset);
    await s.syncAll();
    await s.end();
    await s.say(bobReset, text('New phone. Lost everything, sorry'));
    await s.say(annPhone, text('Compare codes with me when we meet'));

    // A refused record and its repair.
    s.begin();
    final intruder = await SigningKey.fromSeed(
      fixed('transcript intruder sign seed', 32),
    );
    final forged = await makeEpoch(
      conversationId: conv,
      keyEpoch: 11,
      prev: epochHash(s.lastEpoch()),
      makerAccountId: ann,
      makerDeviceId: annPhone.d.id,
      makerSign: intruder,
      lists: [
        EpochList(ann, s.lists[ann]!.last.list),
        EpochList(bob, s.lists[bob]!.last.list),
      ],
      secret: fixed('transcript intruder epoch secret', 32),
      oneTime: (d) => fixed('transcript intruder one-time $d', 32),
      at: s.now,
    );
    s.forceEpoch(11, forged);
    await s.syncAll();
    await s.end();
    s.begin();
    expect(s.chatOf(annPhone).needsEpoch(), 'refused');
    await s.makeEpoch(annPhone);
    await s.syncAll();
    await s.end();
    await s.say(bobReset, text('Working again'));

    // ignore: avoid_print
    print(
      'TRANSCRIPT: ${s.stepsMatched}/${s.steps.length} steps matched in every compared value '
      '(${s.comparisons} comparisons); every device saved and reloaded after every step',
    );
    expect(
      s.stepNo,
      s.steps.length,
      reason: 'every step of the transcript was played',
    );
    for (final dev in s.devices) {
      expect(
        dev.random.left,
        0,
        reason: '${dev.name} left given random values unused',
      );
    }
    expect(s.failures, isEmpty, reason: s.failures.take(20).join('\n'));
  }, timeout: const Timeout(Duration(minutes: 10)));
}
