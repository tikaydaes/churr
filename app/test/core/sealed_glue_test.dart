import 'dart:convert';
import 'dart:io';
import 'dart:async';
import 'dart:typed_data';

import 'package:churr/core/db/cache_db.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:churr/core/sealed/sealed.dart';
import 'package:churr/core/sealed_client/content_view.dart';
import 'package:churr/core/sealed_client/session.dart';
import 'package:churr/core/models/message.dart' hide Quote;
import 'package:churr/core/sealed_client/sqlite_store.dart';
import 'package:churr/core/session_store.dart';
import 'package:churr/core/api/client.dart';
import 'package:churr/core/db/app_db.dart';
import 'package:churr/state/sealed_state.dart';
import 'package:churr/state/server_state.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import '../sealed_support.dart';

/// The glue between sync and the sealed state machine: the sealed events
/// the sync engine passes on reach the session, sealed conversations in
/// the cache are told to it with the other person, sealed rows written
/// to the cache are handed to it; and the content helpers.
void main() {
  group('content', () {
    test('a sending device works out task, mention and link entities', () {
      final text =
          'Plan for @sam:\n- [ ] floor one\n- [x] floor two\nsee https://example.org';
      final e = workOutEntities(text, {'sam': samId});
      expect(
        [for (final x in e) x['type']],
        ['account', 'task', 'task', 'link'],
      );
      expect(e.where((x) => x['type'] == 'task').map((x) => x['item_id']), [
        't1',
        't2',
      ]);
      expect(e.first['account_id'], samId);
      // A mention of someone who is not one of the two is not drawn.
      expect(workOutEntities('hi @lee', {'sam': samId}), isEmpty);
    });

    test('mentions are the shared ones (testdata/mentions/sealed.json)', () {
      final v =
          jsonDecode(File('../testdata/mentions/sealed.json').readAsStringSync())
              as Map;
      final people = (v['people'] as Map).cast<String, String>();
      for (final c in (v['cases'] as List).cast<Map>()) {
        final got = [
          for (final e in workOutEntities(c['text'] as String, people))
            if (e['type'] == 'account') e,
        ];
        expect(jsonEncode(got), jsonEncode(c['want']), reason: '${c['text']}');
      }
    });

    test(
      'a decrypted message becomes the API shape with its reference as id',
      () async {
        final (a, b) = await twoDevices();
        await chatBetween(
          a,
          b,
          messages: [(b, messageContent('Hello\n- [ ] one'))],
        );
        final chat = a.chats.values.single;
        final shown = chat.view().single;
        shown.reactions['👍'] = {a.account};
        shown.tasks['t1'] = true;
        final j = shownAsMessage(
          shown,
          null,
          sealedConv,
          myId: meId,
          usernames: const {},
        );
        expect(j['id'], '${shown.reference}');
        expect(j['author_id'], samId);
        expect((j['reactions'] as List).single, {
          'emoji': '👍',
          'count': 1,
          'me': true,
          'account_ids': [meId],
        });
        expect((j['tasks'] as List).single, {'id': 't1', 'checked': true});
      },
    );
  });

  group('sync to session', () {
    late CacheDb db;
    late ServerState server;
    late TestSealedSession session;

    setUp(() async {
      db = await sealedCache();
      final api = ApiClient(
        base: Uri.parse('https://chat.example.org'),
        httpClient: MockClient((_) async => http.Response('{}', 200)),
        tokens: Tokens(
          access: 'a',
          refresh: 'r',
          expiresAt: DateTime.now().add(const Duration(hours: 1)),
        ),
      );
      server = ServerState(
        entry: ServerEntry(
          localId: 's',
          address: 'https://chat.example.org',
          name: 'Work',
          position: 0,
        ),
        db: db,
        api: api,
      );
      await server.start(connect: false);
      session = await TestSealedSession.make();
      SealedHub.instance.reset();
      SealedHub.instance.opener = (_) async => session;
    });

    tearDown(() async {
      SealedHub.instance.reset();
      await server.shutDown();
    });

    Future<void> settle() =>
        Future<void>.delayed(const Duration(milliseconds: 80));

    test('sealed conversations are told with the other person, and rows are handed over', () async {
      final c = SealedHub.instance.attach(server)!;
      await settle();
      expect(c.otherOf(sealedConv), samId);
      expect(session.told.last, {sealedConv: samId});
      final (a, b) = await twoDevices();
      final rows = await chatBetween(
        a,
        b,
        messages: [(b, messageContent('row'))],
      );
      await db.upsertMessages(rows);
      await settle();
      expect(
        session.taken.any(
          (t) => t.$1 == sealedConv && t.$2 == samId && t.$3 == 1,
        ),
        isTrue,
      );
    });

    test(
      'each sealed event the sync engine applies reaches the session',
      () async {
        SealedHub.instance.attach(server);
        await settle();
        await server.sync.applyEvent({
          'type': 'device_list.changed',
          'account_id': samId,
          'version': 2,
        });
        await server.sync.applyEvent({
          'type': 'backup.changed',
          'key_version': 2,
          'replacement_due': false,
        });
        await server.sync.applyEvent({
          'type': 'unlock_request.created',
          'unlock_request': <String, Object?>{},
        });
        await server.sync.applyEvent({
          'type': 'unlock_request.answered',
          'unlock_request': <String, Object?>{},
        });
        await server.sync.applyEvent({
          'type': 'device.retired',
          'device': <String, Object?>{},
        });
        // A stored conversation event, in its place in the sequence.
        await server.sync.applyEvent({
          'type': 'sealed.epoch_started',
          'conversation_id': sealedConv,
          'seq': 4,
          'epoch': {
            'conversation_id': sealedConv,
            'key_epoch': 2,
            'seq': 4,
            'record': 'AA',
          },
        });
        await server.sync.applyEvent({
          'type': 'typing.started',
          'conversation_id': sealedConv,
        });
        await settle();
        expect(session.events.map((e) => e['type']), [
          'device_list.changed',
          'backup.changed',
          'unlock_request.created',
          'unlock_request.answered',
          'device.retired',
          'sealed.epoch_started',
        ]);
        expect((await db.conversation(sealedConv))!.syncedSeq, 4);
      },
    );

    test(
      'a channel is not a sealed chat; leaving the chat stops its rows',
      () async {
        final c = SealedHub.instance.attach(server)!;
        await settle();
        await db.upsertConversation(
          Conversation.fromJson({
            'id': '0193a6f0-0000-7000-8000-000000000001',
            'kind': 'channel',
            'name': 'General',
            'created_at': '2026-11-14T09:00:00.000Z',
          }),
        );
        await settle();
        expect(session.told.last.keys, [sealedConv]);
        await db.deleteConversation(sealedConv);
        await settle();
        expect(session.told.last, isEmpty);
        expect(c.otherOf(sealedConv), isNull);
      },
    );

    test(
      'signing out hands over to the session and forgets the controller',
      () async {
        SealedHub.instance.attach(server);
        await settle();
        await SealedHub.instance.signOut(server);
        expect(SealedHub.instance.of(server), isNull);
      },
    );
  });

  test('rows reach the state machine: taken once, a changed one refused, a replay dropped', () async {
    final (a, b) = await twoDevices();
    await chatBetween(a, b); // epoch 1 for both
    final store = await SqliteSealedStore.open(
      path: null,
      secrets: MemorySecretStore(),
      scope: 'g',
    );
    final s =
        SealedSession(
            api: ApiClient(
              base: Uri.parse('https://chat.example.org'),
              httpClient: MockClient(
                (r) async =>
                    http.Response('{"epochs": [], "has_more": false}', 200),
              ),
              tokens: Tokens(
                access: 'a',
                refresh: 'r',
                expiresAt: DateTime.now().add(const Duration(hours: 1)),
              ),
            ),
            store: store,
            platformName: 'macos',
            now: testNow,
          )
          ..device = a
          ..myId = meId;
    a.store = store;
    final cb = b.chats.values.single;
    Future<Message> row(int seq, String text, {bool tamper = false}) async {
      final o = await cb.seal(messageContent(text));
      final ct = Uint8List.fromList(o.ciphertext);
      if (tamper) ct[ct.length - 70] ^= 1;
      return Message.fromJson({
        'id': 'row$seq',
        'conversation_id': sealedConv,
        'seq': seq,
        'author_id': samId,
        'client_id': '${o.clientId}',
        'sealed': {'key_epoch': o.keyEpoch, 'ciphertext': b64(ct)},
        'quiet': o.quiet,
        'created_at': '2026-11-14T09:0$seq:00.000Z',
      });
    }

    final good = await row(2, 'first');
    final bad = await row(3, 'changed', tamper: true);
    await s.takeRows(sealedConv, samId, [bad, good, good]);
    expect(s.view(sealedConv).map((x) => x.$1.text), ['first']);
    expect(s.troubles(sealedConv).unchecked, 1);
    expect(s.highestSeq(sealedConv), 3);
    // The same row again changes nothing; the same ciphertext at a later
    // seq is the server replaying it.
    await s.takeRows(sealedConv, samId, [good]);
    final replay = Message.fromJson({...good.raw, 'id': 'row9', 'seq': 9});
    await s.takeRows(sealedConv, samId, [replay]);
    expect(s.view(sealedConv).map((x) => x.$1.text), ['first']);
    expect(store.row(sealedConv, 9)!.state, 'dropped');
    // A reaction is quiet and changes the message it names.
    final ref = s.view(sealedConv).single.$1.id;
    final o = await cb.seal(reactionContent(Uuid.parse(ref), '🎉', true));
    await s.takeRows(sealedConv, samId, [
      Message.fromJson({
        'id': 'row10',
        'conversation_id': sealedConv,
        'seq': 10,
        'author_id': samId,
        'sealed': {'key_epoch': 1, 'ciphertext': b64(o.ciphertext)},
        'quiet': true,
        'created_at': '2026-11-14T09:10:00.000Z',
      }),
    ]);
    expect(store.row(sealedConv, 10)!.state, 'quiet');
    expect(s.view(sealedConv).single.$1.reactions.single.emoji, '🎉');
    store.close();
  });

  test('the store keeps what the app adds about rows', () async {
    final s = await SqliteSealedStore.open(
      path: null,
      secrets: MemorySecretStore(),
      scope: 'x',
    );
    s.putRow(
      SealedRowMeta(
        conversationId: 'c',
        seq: 3,
        messageId: 'm3',
        reference: 'r3',
        createdAt: DateTime.utc(2026),
        state: 'nokey',
      ),
    );
    s.putRow(
      SealedRowMeta(
        conversationId: 'c',
        seq: 2,
        messageId: 'm2',
        reference: 'r2',
        createdAt: DateTime.utc(2026),
      ),
    );
    expect(s.rows('c').map((r) => r.seq), [2, 3]);
    expect(s.waiting('c').single.reference, 'r3');
    expect(s.rowByReference('c', 'r2')!.messageId, 'm2');
    s.close();
  });

  test('Uuid round trip used for conversation ids', () {
    expect('${Uuid.parse(sealedConv)}', sealedConv);
    unawaited(Future.value());
  });
}
