import 'package:churr/core/api/client.dart';
import 'package:churr/core/db/cache_db.dart';
import 'package:churr/core/models/account.dart';
import 'package:churr/core/models/json.dart';
import 'package:churr/core/session_store.dart';
import 'package:churr/core/sync.dart';
import 'package:drift/native.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// What the sync engine does with presence (docs/EVENTS.md section 9):
/// `presence.changed` for one account, and `presence.batch`, which carries
/// several of the same objects in one frame when a great many people's
/// presence changes at once.
void main() {
  late CacheDb db;
  late SyncEngine sync;

  Json one(String id, String presence, {bool inCall = false}) => {
    'type': 'presence.changed',
    'at': '2026-11-15T09:00:00.000Z',
    'account_id': id,
    'presence': presence,
    'in_call': inCall,
  };

  Json batch(List<Object?> changes) => {
    'type': 'presence.batch',
    'at': '2026-11-15T09:00:00.000Z',
    'changes': changes,
  };

  setUp(() async {
    db = CacheDb(NativeDatabase.memory());
    sync = SyncEngine(
      db: db,
      api: ApiClient(
        base: Uri.parse('https://chat.example.org'),
        tokens: Tokens(
          access: 'a',
          refresh: 'r',
          expiresAt: DateTime.now().add(const Duration(hours: 1)),
        ),
        httpClient: MockClient((_) async => http.Response('{}', 404)),
      ),
    );
    await db.upsertAccounts([
      for (final id in ['ana', 'ben', 'kim'])
        Account.fromJson({
          'id': id,
          'username': id,
          'display_name': id,
          'role': 'member',
          'presence': 'offline',
          'title': 'kept',
        }),
    ]);
  });

  tearDown(() => db.close());

  Future<String> of(String id) async => (await db.account(id))!.presence;

  test('presence.changed still sets one account', () async {
    await sync.applyEvent(one('ana', 'online'));
    expect(await of('ana'), 'online');
    expect(await of('ben'), 'offline');
  });

  test(
    'presence.batch sets every account it names and keeps the rest of each',
    () async {
      await sync.applyEvent(
        batch([
          one('ana', 'away'),
          one('ben', 'online', inCall: true),
          one('kim', 'online'),
        ]),
      );
      expect(
        [await of('ana'), await of('ben'), await of('kim')],
        ['away', 'online', 'online'],
      );
      final ben = (await db.account('ben'))!;
      expect(ben.inCall, isTrue);
      expect(ben.title, 'kept');
      expect((await db.account('kim'))!.inCall, isFalse);
    },
  );

  test(
    'an account the app does not hold is passed over, not invented',
    () async {
      await sync.applyEvent(batch([one('zed', 'online'), one('kim', 'away')]));
      expect(await db.account('zed'), isNull);
      expect(await of('kim'), 'away');
    },
  );

  test('a later presence.changed replaces what a batch said', () async {
    await sync.applyEvent(batch([one('ana', 'online'), one('ben', 'online')]));
    await sync.applyEvent(one('ana', 'offline'));
    expect(await of('ana'), 'offline');
    expect(await of('ben'), 'online');
  });

  test('an empty or malformed batch changes nothing', () async {
    await sync.applyEvent(batch([]));
    await sync.applyEvent({'type': 'presence.batch', 'at': 't'});
    await sync.applyEvent(batch([null, 7, <String, dynamic>{}]));
    expect(await of('ana'), 'offline');
  });

  test('the screens are told once for a whole batch', () async {
    var told = 0;
    final sub = db.watchAccounts().listen((_) => told++);
    await pumpEventQueue();
    told = 0;
    await sync.applyEvent(
      batch([one('ana', 'online'), one('ben', 'online'), one('kim', 'online')]),
    );
    await pumpEventQueue();
    expect(told, 1);
    await sub.cancel();
  });
}
