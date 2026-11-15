import 'dart:convert';
import 'dart:typed_data';

import 'package:churr/core/api/client.dart';
import 'package:churr/core/db/cache_db.dart';
import 'package:churr/core/models/account.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:churr/core/models/json.dart';
import 'package:churr/core/models/message.dart' hide Quote;
import 'package:churr/core/sealed/sealed.dart';
import 'package:churr/core/sealed_client/api.dart';
import 'package:churr/core/sealed_client/session.dart';
import 'package:churr/core/sealed_client/sqlite_store.dart';
import 'package:churr/core/session_store.dart';
import 'package:drift/drift.dart' show driftRuntimeOptions;
import 'package:drift/native.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// Test support for sealed chats: two accounts with real device keys,
/// a backup each, a chat with epoch 1 and messages, all made offline
/// with the library (no server), behind a [SealedSession] whose network
/// steps are stubbed.

const meId = '0193a000-0000-7000-8000-00000000000a';
const samId = '0193a000-0000-7000-8000-00000000000b';
const sealedConv = '0193a6f0-0000-7000-8000-0000000000ee';

int _clock = DateTime.utc(2026, 11, 14, 9).millisecondsSinceEpoch;
int testNow() => _clock;

/// Two devices, one per account, each unlocked with its account's list
/// pinned by the other.
Future<(Device, Device)> twoDevices() async {
  Future<Device> make(String account, String device) async {
    final d = await Device.create(
      account: Uuid.parse(account),
      server: 'chat.example.org',
      platform: 3,
      random: SystemRandom(),
      now: testNow,
      id: Uuid.parse(device),
    );
    await d.adopt(await d.createBackup());
    return d;
  }

  final a = await make(meId, '0193aa00-0000-7000-8000-0000000000a1');
  final b = await make(samId, '0193aa00-0000-7000-8000-0000000000b1');
  await a.learnLists(b.account, [b.pins[b.account]!.newestList!.signed]);
  await b.learnLists(a.account, [a.pins[a.account]!.newestList!.signed]);
  a.notices.clear();
  b.notices.clear();
  return (a, b);
}

/// Epoch 1 of [sealedConv], made by [a] and taken by both, then
/// [messages] (author device, content) sealed and taken by both in turn.
/// Returns the rows as the server would hold them.
Future<List<Message>> chatBetween(
  Device a,
  Device b, {
  List<(Device, Uint8List)> messages = const [],
}) async {
  final ca = a.chat(Uuid.parse(sealedConv), b.account);
  final cb = b.chat(Uuid.parse(sealedConv), a.account);
  final (stored, k) = await ca.makeEpoch();
  await ca.acceptEpoch(k, 1, stored);
  await cb.acceptEpoch(k, 1, stored);
  final rows = <Message>[];
  var seq = 2;
  for (final (from, content) in messages) {
    final chat = from == a ? ca : cb;
    final o = await chat.seal(content);
    final row = Row(
      seq: seq,
      author: from.account,
      keyEpoch: o.keyEpoch,
      quiet: o.quiet,
      ciphertext: o.ciphertext,
    );
    await ca.receive(row);
    await cb.receive(row);
    rows.add(
      Message.fromJson({
        'id': '0193a900-0000-7000-8000-${seq.toString().padLeft(12, '0')}',
        'conversation_id': sealedConv,
        'seq': seq,
        'order': 'o${seq.toString().padLeft(6, '0')}',
        'author_id': '${from.account}',
        'client_id': '${o.clientId}',
        'sealed': {'key_epoch': o.keyEpoch, 'ciphertext': b64(o.ciphertext)},
        'quiet': o.quiet,
        'created_at': formatTime(DateTime.utc(2026, 11, 14, 9, seq)),
      }),
    );
    seq++;
  }
  return rows;
}

/// A session with no network: [start], [refresh] and the rest do what a
/// test needs, and record what they were handed.
class TestSealedSession extends SealedSession {
  TestSealedSession(SqliteSealedStore store, {http.Client? client})
    : super(
        api: ApiClient(
          base: Uri.parse('https://chat.example.org'),
          httpClient:
              client ?? MockClient((_) async => http.Response('{}', 200)),
          tokens: Tokens(
            access: 'a',
            refresh: 'r',
            expiresAt: DateTime.now().add(const Duration(hours: 1)),
          ),
        ),
        store: store,
        platformName: 'macos',
        delay: () => Duration.zero,
        now: testNow,
      );

  final events = <Json>[];
  final taken = <(String, String, int)>[];
  final told = <Map<String, String>>[];
  int refreshes = 0;
  final sent = <String>[];

  /// The texts sent with the /me mark (ruling R67).
  final actions = <String>[];

  static Future<TestSealedSession> make({
    SealedStatus status = SealedStatus.unlocked,
    bool chat = true,
  }) async {
    final store = await SqliteSealedStore.open(
      path: null,
      secrets: MemorySecretStore(),
      scope: 't',
    );
    final s = TestSealedSession(store);
    s.myId = meId;
    s.serverName = 'chat.example.org';
    final (a, b) = await twoDevices();
    if (chat) {
      await chatBetween(
        a,
        b,
        messages: [
          (b, messageContent('Hello from sam\n- [ ] the plan')),
          (a, messageContent('Hello back')),
        ],
      );
      s.conversations[sealedConv] = samId;
    }
    if (status != SealedStatus.unlocked) a.lockDevice();
    s.device = a;
    s.status = status;
    s.backup = status == SealedStatus.noBackup ? null : BackupRow(const {});
    s.passkeys = [
      PasskeyRow({
        'id': '0193ab00-0000-7000-8000-000000000001',
        'name': 'Work laptop',
        'has_prf': true,
        'can_unlock': true,
      }),
      PasskeyRow({
        'id': '0193ab00-0000-7000-8000-000000000002',
        'name': 'Old key',
        'has_prf': false,
        'can_unlock': false,
      }),
    ];
    s.devices = [
      SealedDeviceRow({
        'id': '${a.id}',
        'name': 'Kai laptop',
        'platform': 'macos',
        'current': true,
        'created_at': '2026-11-01T00:00:00Z',
      }),
      SealedDeviceRow({
        'id': '0193aa00-0000-7000-8000-0000000000a2',
        'name': 'Kai phone',
        'platform': 'ios',
        'created_at': '2026-11-02T00:00:00Z',
      }),
    ];
    s.usernames = {'kai': meId, 'sam': samId};
    return s;
  }

  @override
  Future<void> start({required String myId, SignInHandover? signIn}) async {}

  @override
  Future<void> refresh() async => refreshes++;

  @override
  Future<void> setConversations(Map<String, String> byId) async {
    told.add(Map.of(byId));
    conversations
      ..clear()
      ..addAll(byId);
  }

  @override
  Future<void> handleEvent(Json e) async => events.add(e);

  @override
  Future<void> takeRows(
    String conversationId,
    String other,
    List<Message> rows,
  ) async {
    taken.add((conversationId, other, rows.length));
  }

  @override
  Future<Message> sendText(
    String conversationId,
    String other,
    String text, {
    List<FileEntry> files = const [],
    String? parent,
    Quote? quote,
    bool action = false,
  }) async {
    sent.add(text);
    if (action) actions.add(text);
    return Message.fromJson(const {});
  }
}

/// A cache with the two people and the sealed chat between them.
Future<CacheDb> sealedCache() async {
  driftRuntimeOptions.dontWarnAboutMultipleDatabases = true;
  final db = CacheDb(NativeDatabase.memory());
  final me = {
    'id': meId,
    'username': 'kai',
    'display_name': 'Kai Tanaka',
    'role': 'member',
    'prefs': <String, Object?>{},
  };
  await db.putMe(Me.fromJson(me));
  await db.upsertAccounts([
    Account.fromJson(me),
    Account.fromJson({
      'id': samId,
      'username': 'sam',
      'display_name': 'Sam Okafor',
      'role': 'member',
      'presence': 'online',
    }),
  ]);
  final c = Conversation.fromJson({
    'id': sealedConv,
    'kind': 'sealed',
    'private': true,
    'member_ids': [meId, samId],
    'created_at': '2026-11-14T09:00:00.000Z',
    'last_seq': 3,
    'key_epoch': 1,
    'membership': {
      'level': 'member',
      'read_seq': 1,
      'mode': 'any',
      'unread_count': 0,
      'mention_count': 0,
      'badge_count': 0,
    },
  });
  await db.upsertConversation(c, syncedSeq: 3);
  return db;
}

String prettyJson(Object? o) => const JsonEncoder.withIndent(' ').convert(o);
