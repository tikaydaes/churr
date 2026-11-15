import 'dart:typed_data';

import 'package:churr/core/api/client.dart';
import 'package:churr/core/db/conversation_queries.dart';
import 'package:churr/core/db/app_db.dart';
import 'package:churr/core/session_store.dart';
import 'package:churr/state/composer_controller.dart';
import 'package:churr/state/conversation_controller.dart';
import 'package:churr/state/message_actions.dart';
import 'package:churr/state/server_state.dart';
import 'package:churr/state/thread_controller.dart';
import 'package:flutter_test/flutter_test.dart';

import '../conversation_support.dart';

Future<(ServerState, FakeServer)> makeServer() async {
  final fake = FakeServer();
  final db = await conversationCache();
  final s = ServerState(
    entry: ServerEntry(
      localId: 's',
      address: 'https://chat.example.org',
      name: 'Work',
      position: 0,
    ),
    db: db,
    api: ApiClient(
      base: Uri.parse('https://chat.example.org'),
      httpClient: fake.client(),
      tokens: Tokens(
        access: 'a',
        refresh: 'r',
        expiresAt: DateTime.now().add(const Duration(hours: 1)),
      ),
    ),
  );
  await s.start(connect: false);
  return (s, fake);
}

Future<void> tick([int ms = 30]) =>
    Future<void>.delayed(Duration(milliseconds: ms));

/// Lets the outbox post: the sync engine holds sends until `ready`.
Future<void> ready(ServerState s, FakeServer fake) async {
  fake.on(
    'GET /api/v1/me',
    (_) => {
      'id': 'me',
      'username': 'kai',
      'display_name': 'Kai Tanaka',
      'role': 'member',
    },
  );
  fake.on(
    'GET /api/v1/conversations',
    (_) => {
      'conversations': [
        for (final c in s.conversations) c.conversation.toJson(),
      ],
    },
  );
  fake.on(
    'GET /api/v1/accounts',
    (_) => {
      'accounts': [for (final a in s.accounts.values) a.raw],
    },
  );
  await s.sync.onReady(const []);
}

void main() {
  test(
    'a reply with a quote and a file goes through the outbox with both',
    () async {
      final (s, fake) = await makeServer();
      fake.on(
        'POST /api/v1/conversations/$general/files',
        (_) => {
          'file': {
            'id': 'f1',
            'name': 'plan.txt',
            'size': 4,
            'media_type': 'text/plain',
            'kind': 'text',
            'url': '/api/v1/files/f1/plan.txt',
          },
        },
      );
      fake.on(
        'POST /api/v1/conversations/$general/messages',
        (r) => {
          'message': {
            'id': 'new',
            'conversation_id': general,
            'seq': 31,
            'author_id': 'me',
            'parent_id': 'm1',
            'text': 'ok @lee',
            'created_at': '2026-11-13T10:00:00.000Z',
            'entities': [
              {'type': 'account', 'at': 3, 'len': 4, 'account_id': 'lee'},
            ],
          },
          'cannot_read_ids': ['sam'],
        },
      );
      final c = ComposerController(s, general, parentId: 'm1');
      await c.start();
      await tick();
      final r1 = await s.db.message('r1');
      c.setQuote(r1!, start: 0, end: 5);
      c.text.text = 'ok @lee';
      expect(
        await c.addFiles([
          ('plan.txt', Uint8List.fromList([1, 2, 3, 4])),
        ]),
        isEmpty,
      );
      final out = await c.send();
      expect(out, isA<Sent>());
      final queued = await s.db.outbox(conversationId: general);
      expect(queued.single.extra!['quote'], {
        'message_id': 'r1',
        'start': 0,
        'end': 5,
      });
      expect(queued.single.extra!['file_ids'], ['f1']);
      expect(c.text.text, isEmpty);
      expect(c.quoted, isNull);

      await ready(s, fake);
      await tick(80);
      final post = fake.where('POST', '/messages').single;
      expect((post.body as Map)['quote'], {
        'message_id': 'r1',
        'start': 0,
        'end': 5,
      });
      expect((post.body as Map)['file_ids'], ['f1']);
      expect((post.body as Map)['parent_id'], 'm1');
      // Lee is in do not disturb: notify anyway is offered. Sam cannot
      // read the conversation: the author is told.
      expect(c.offers.map((o) => o.account.id), ['lee']);
      expect(c.notNotified.map((a) => a.id), ['sam']);
      await c.notifyAnyway(c.offers.single);
      expect(
        (fake.where('POST', '/notify-anyway').single.body as Map)['account_id'],
        'lee',
      );
      c.dispose();
    },
  );

  test(
    'a file over the server limit is refused before uploading; at most ten',
    () async {
      final (s, _) = await makeServer();
      await s.db.putServerLimits({'file_size': 10});
      final c = ComposerController(s, general);
      await c.start();
      final refused = await c.addFiles([
        ('big.bin', Uint8List(11)),
        for (var i = 0; i < 11; i++) ('f$i.txt', Uint8List(1)),
      ]);
      expect(refused.length, 2);
      expect(c.attachments.length, 10);
      c.dispose();
    },
  );

  test('the draft keeps chips as <#id> and comes back as chips', () async {
    final (s, _) = await makeServer();
    final c = ComposerController(s, general);
    await c.start();
    c.text.text = 'see ';
    c.text.insertChip(4, 4, design);
    await c.saveDraft();
    expect(await s.db.draft(general), 'see <#$design> ');
    c.dispose();
    await tick();
    final again = ComposerController(s, general);
    await again.start();
    expect(again.text.text.length, 6, reason: 'one character for the chip');
    expect(again.text.wireText, 'see <#$design> ');
    again.dispose();
  });

  test('commands: saved replies and bots go through runCommand; built-ins are handed back', () async {
    final (s, fake) = await makeServer();
    final c = ComposerController(s, general);
    await c.start();
    await tick(80);
    expect(c.commands.map((x) => x.name), ['poll', 'standup', 'deploy']);
    c.text.text = '/deploy main';
    expect(await c.send(), isA<Sent>());
    expect(fake.where('POST', '/commands').single.body, {
      'name': 'deploy',
      'text': 'main',
    });
    c.text.text = '/poll Lunch?';
    final out = await c.send();
    expect(out, isA<RunBuiltin>());
    expect((out as RunBuiltin).args, 'Lunch?');
    c.dispose();
  });

  test('/me goes as an action through the outbox; /roll and /kick go to the screen as read', () async {
    final (s, fake) = await makeServer();
    fake.on(
      'POST /api/v1/conversations/$general/messages',
      (r) => {
        'message': {
          'id': 'act',
          'conversation_id': general,
          'seq': 40,
          'author_id': 'me',
          'text': 'waves',
          'action': true,
          'created_at': '2026-11-13T10:00:00.000Z',
        },
      },
    );
    final c = ComposerController(s, general);
    await c.start();
    await tick();
    c.text.text = '/me waves';
    expect(await c.send(), isA<Sent>());
    final queued = await s.db.outbox(conversationId: general);
    expect(queued.single.text, 'waves');
    expect(queued.single.extra!['action'], true);
    await ready(s, fake);
    await tick(80);
    final post = fake.where('POST', '/messages').single.body as Map;
    expect(post['text'], 'waves');
    expect(post['action'], true);

    c.text.text = '/shrug fine';
    expect(await c.send(), isA<Sent>());

    c.text.text = '/roll 2d6+3';
    final roll = await c.send();
    expect(roll, isA<RunBuiltin>());
    expect((roll as RunBuiltin).parsed, {'name': 'roll', 'dice': '2d6+3'});
    c.text.text = '/kick';
    final kick = await c.send() as RunBuiltin;
    expect(kick.parsed, {'name': 'kick', 'error': 'needs_person'});
    c.dispose();
  });

  test(
    'typing: others are listed for eight seconds; a message ends it',
    () async {
      final (s, _) = await makeServer();
      final c = ComposerController(s, general);
      await c.start();
      await s.sync.applyEvent({
        'type': 'typing.started',
        'conversation_id': general,
        'account_id': 'sam',
      });
      await tick();
      expect(c.typingLine, 'Sam Okafor is typing…');
      await s.sync.applyEvent({
        'type': 'typing.started',
        'conversation_id': general,
        'account_id': 'lee',
        'parent_id': 'm1',
      });
      await tick();
      expect(c.typing.keys, [
        'sam',
      ], reason: 'typing in a thread is shown in the thread');
      c.messageArrived('sam');
      expect(c.typingLine, isNull);
      c.dispose();
    },
  );

  test(
    'editing in place saves through editMessage and restores the draft',
    () async {
      final (s, fake) = await makeServer();
      final m3 = (await s.db.message('m3'))!;
      fake.on(
        'PATCH /api/v1/messages/m3',
        (_) => {
          'message': {
            ...m3.raw,
            'text': 'Sounds great',
            'edited_at': '2026-11-13T10:00:00.000Z',
          },
        },
      );
      final mine = await s.db.message('m2');
      expect(mine, isNotNull);
      final c = ComposerController(s, general);
      await c.start();
      c.text.text = 'half a thought';
      c.startEdit(m3);
      expect(c.text.text, 'Sounds good');
      c.text.text = 'Sounds great';
      expect(await c.send(), isA<Sent>());
      expect(
        (fake.where('PATCH', '/messages/m3').single.body as Map)['text'],
        'Sounds great',
      );
      expect((await s.db.message('m3'))!.editedAt, isNotNull);
      expect(c.text.text, 'half a thought');
      c.dispose();
    },
  );

  test('send later posts a scheduled message', () async {
    final (s, fake) = await makeServer();
    final c = ComposerController(s, general);
    await c.start();
    c.text.text = 'tomorrow';
    final at = DateTime.utc(2026, 11, 15, 9);
    expect(await c.schedule(at), isA<Sent>());
    final body = fake.where('POST', '/api/v1/scheduled').single.body as Map;
    expect(body['kind'], 'message');
    expect(body['run_at'], '2026-11-15T09:00:00.000Z');
    expect((body['content'] as Map)['text'], 'tomorrow');
    c.dispose();
  });

  group('message actions', () {
    test(
      'follow and ignore write the cache first; neither drops the row',
      () async {
        final (s, fake) = await makeServer();
        final a = MessageActions(s);
        final p = (await s.db.message('m1'))!;
        await a.setThreadState(p, 'following', readSeq: 24);
        expect((await s.db.threadStates(general))['m1']!.state, 'following');
        expect(fake.where('PUT', '/thread/state').single.body, {
          'state': 'following',
        });
        await a.setThreadState(p, 'none');
        expect((await s.db.threadStates(general)), isEmpty);
      },
    );

    test('a refused change is put back', () async {
      final (s, fake) = await makeServer();
      fake.on(
        'PUT /api/v1/conversations/$general/pins',
        (_) => {
          'error': {'code': 'forbidden', 'message': 'Managers only.'},
        }.toResponse(403),
      );
      final a = MessageActions(s);
      final m = (await s.db.message('m2'))!;
      await expectLater(a.setPinned(m, true), throwsA(anything));
      expect((await s.db.message('m2'))!.pinned, isFalse);
    });

    test('delete leaves a marker with who deleted it', () async {
      final (s, fake) = await makeServer();
      final m = (await s.db.message('m3'))!;
      fake.on(
        'DELETE /api/v1/messages/m3',
        (_) => {
          'message': {
            ...m.raw,
            'text': '',
            'deleted_at': '2026-11-13T10:00:00.000Z',
            'deleted_by_id': 'me',
          },
        },
      );
      await MessageActions(s).delete(m);
      final after = (await s.db.message('m3'))!;
      expect(after.isDeleted, isTrue);
      expect(after.deletedById, 'me');
    });

    test('who may edit: the author, inside the window, not archived', () async {
      final (s, _) = await makeServer();
      final a = MessageActions(s);
      final mine = msg('x', 40, 'mine', author: 'me');
      final conv = s.conversation(general);
      expect(
        a.canEdit(mine, conv, const ServerLimits(), DateTime.utc(2030)),
        isTrue,
      );
      expect(
        a.canEdit(
          mine,
          conv,
          const ServerLimits(editWindowSeconds: 60),
          DateTime.utc(2030),
        ),
        isFalse,
      );
      expect(
        a.canEdit(
          msg('y', 41, 'theirs'),
          conv,
          const ServerLimits(),
          DateTime.utc(2030),
        ),
        isFalse,
      );
    });
  });

  test(
    'the thread pane keeps replies older than the cached run out of the cache',
    () async {
      final (s, fake) = await makeServer();
      fake.on(
        'GET /api/v1/messages/m1/thread',
        (_) => {
          'parent': msg('m1', 10, 'Deploy plan for Friday').raw,
          'messages': [
            msg('old', 5, 'very old', parent: 'm1').raw,
            msg('r1', 22, 'I can do Friday', parent: 'm1').raw,
          ],
          'thread_state': {
            'parent_id': 'm1',
            'conversation_id': general,
            'state': 'following',
            'read_seq': 22,
            'unread_count': 0,
          },
          'has_more': false,
        },
      );
      final t = ThreadController(s, general, 'm1');
      await t.start();
      await tick(60);
      expect(t.replies.map((r) => r.id), ['old', 'r1', 'r2']);
      expect(await s.db.message('old'), isNull);
      expect(t.state!.state, 'following');
      await t.markRead();
      expect(fake.where('PUT', '/thread/read').single.body, {'seq': 24});
      t.dispose();
    },
  );

  test(
    'marking read goes to the highest seq loaded, not the last message shown',
    () async {
      final (s, fake) = await makeServer();
      // Arrived last (seq 31) but ordered up between m2 and m3.
      await s.db.upsertMessages([
        msg(
          'late',
          31,
          'From the other server',
          extra: {'order': 'o000021x', 'late': true},
        ),
      ]);
      final c = ConversationController(s, general);
      await c.start();
      await tick(60);
      expect(c.messages.last.id, 'r2');
      expect(c.messages.map((m) => m.id), contains('late'));
      expect(highestSeq(c.messages), 31);
      await c.markRead();
      expect(fake.where('PUT', '/read').single.body, {'seq': 31});
      c.dispose();
    },
  );

  test('file.changed replaces a file from another server', () async {
    final (s, _) = await makeServer();
    await s.db.upsertMessages([
      msg(
        'fm',
        29,
        '',
        extra: {
          'files': [
            {
              'id': 'f9',
              'name': 'a.png',
              'size': 1,
              'media_type': 'image/png',
              'kind': 'image',
              'state': 'arriving',
            },
          ],
        },
      ),
    ]);
    await s.sync.applyEvent({
      'type': 'file.changed',
      'conversation_id': general,
      'message_id': 'fm',
      'file': {
        'id': 'f9',
        'name': 'a.png',
        'size': 1,
        'media_type': 'image/png',
        'kind': 'image',
        'state': 'ready',
        'url': '/api/v1/files/f9/a.png',
      },
    });
    expect((await s.db.message('fm'))!.files.single.state, 'ready');
  });
}
