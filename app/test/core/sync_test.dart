import 'dart:convert';

import 'package:churr/core/api/client.dart';
import 'package:churr/core/db/account_tables.dart';
import 'package:churr/core/db/cache_db.dart';
import 'package:churr/core/db/conversation_queries.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:churr/core/models/json.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/core/session_store.dart';
import 'package:churr/core/sync.dart';
import 'package:drift/native.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const me = 'acc-me';
const sam = 'acc-sam';

Json conv(String id, {int lastSeq = 0, Json? membership, String? parentId}) => {
  'id': id,
  'kind': 'channel',
  'private': false,
  'name': id,
  'topic': '',
  'parent_id': ?parentId,
  'created_at': '2026-11-13T09:00:00.000Z',
  'last_seq': lastSeq,
  'member_count': 2,
  'membership':
      membership ??
      {
        'level': 'member',
        'read_seq': 0,
        'mode': 'mentions',
        'starred': false,
        'unread_count': 0,
        'mention_count': 0,
        'badge_count': 0,
      },
};

Json msg(
  String id,
  int seq, {
  String c = 'c1',
  String author = sam,
  String text = 'hi',
  String? parent,
  String? order,
  Json? extra,
}) => {
  'id': id,
  'conversation_id': c,
  'seq': seq,
  'order': order ?? 'o${seq.toString().padLeft(6, '0')}',
  'author_id': author,
  'text': text,
  'parent_id': ?parent,
  'created_at': '2026-11-13T09:30:00.000Z',
  ...?extra,
};

Json created(Json m, {String? clientId, String notify = 'none'}) => {
  'type': 'message.created',
  'at': 't',
  'conversation_id': m['conversation_id'],
  'seq': m['seq'],
  'message': m,
  'notify': notify,
  'client_id': ?clientId,
};

/// The server, as far as the sync engine talks to it: routes answer from
/// maps the test fills, and every request is logged.
class FakeServer {
  final log = <String>[];
  final posts = <Json>[];
  Map<String, List<Json>> events = {};
  Map<String, Json> conversations = {};
  Map<String, List<Json>> pages = {};
  http.Response? Function(http.Request)? onPost;
  bool reloadEvents = false;

  http.Response _ok(Object body, [int status = 200]) => http.Response.bytes(
    utf8.encode(jsonEncode(body)),
    status,
    headers: {'content-type': 'application/json'},
  );

  Future<http.Response> handle(http.Request r) async {
    final p = r.url.path;
    log.add('${r.method} $p${r.url.hasQuery ? '?${r.url.query}' : ''}');
    final seg = r.url.pathSegments;
    if (p == '/api/v1/me') {
      return _ok({
        'account': {
          'id': me,
          'username': 'kai',
          'display_name': 'Kai',
          'role': 'owner',
        },
      });
    }
    if (p == '/api/v1/status') {
      return _ok({
        'badge': 2,
        'mentions': 1,
        'unread_conversations': 1,
        'unread_threads': 0,
      });
    }
    if (p == '/api/v1/sections') {
      return _ok({
        'sections': [
          {'id': 's1', 'name': 'Projects', 'position': 1},
        ],
      });
    }
    if (p == '/api/v1/me/sidebar') {
      return _ok({
        'sidebar': {'version': 1},
      });
    }
    if (p == '/api/v1/groups') return _ok({'groups': []});
    if (p == '/api/v1/emoji') return _ok({'emoji': []});
    if (p == '/api/v1/me/chat-requests') return _ok({'requests': []});
    if (p == '/api/v1/me/channel-offers') return _ok({'offers': []});
    if (p == '/api/v1/accounts') {
      return _ok({
        'accounts': [
          {
            'id': sam,
            'username': 'sam',
            'display_name': 'Sam',
            'role': 'member',
          },
        ],
      });
    }
    if (p == '/api/v1/conversations') {
      return _ok({
        'conversations': conversations.values
            .where((c) => c['membership'] != null)
            .toList(),
      });
    }
    if (seg.length == 4 && seg[2] == 'conversations') {
      final c = conversations[seg[3]];
      return c == null
          ? _ok({
              'error': {
                'code': 'not_found',
                'message': 'No such conversation.',
              },
            }, 404)
          : _ok({'conversation': c});
    }
    if (seg.length == 5 && seg[4] == 'events') {
      if (reloadEvents) {
        return _ok({'events': [], 'has_more': false, 'reload': true});
      }
      final after = int.parse(r.url.queryParameters['after']!);
      final until = int.tryParse(r.url.queryParameters['until'] ?? '');
      return _ok({
        'events': [
          for (final e in events[seg[3]] ?? const <Json>[])
            if ((e['seq'] as int) > after &&
                (until == null || (e['seq'] as int) <= until))
              e,
        ],
        'has_more': false,
      });
    }
    if (seg.length == 5 && seg[4] == 'messages' && r.method == 'GET') {
      final before = int.tryParse(r.url.queryParameters['before'] ?? '');
      final all = pages[seg[3]] ?? const <Json>[];
      final older = [
        for (final m in all)
          if (before == null || (m['seq'] as int) < before) m,
      ];
      final limit = int.parse(r.url.queryParameters['limit'] ?? '50');
      final page = older.length > limit
          ? older.sublist(older.length - limit)
          : older;
      return _ok({
        'messages': page,
        'thread_states': [],
        'has_more': older.length > limit,
      });
    }
    if (seg.length == 5 && seg[4] == 'messages' && r.method == 'POST') {
      final body = jsonDecode(r.body) as Json;
      posts.add(body);
      final custom = onPost?.call(r);
      if (custom != null) return custom;
      return _ok({
        'message': msg(
          'm-${body['client_id']}',
          100 + posts.length,
          c: seg[3],
          author: me,
          text: body['text'] as String,
        ),
      }, 201);
    }
    if (seg.length == 5 && seg[4] == 'read') {
      final seq = (jsonDecode(r.body) as Json)['seq'];
      return _ok({
        'membership': {
          'level': 'member',
          'read_seq': seq,
          'mode': 'mentions',
          'starred': false,
          'unread_count': 0,
          'mention_count': 0,
          'badge_count': 0,
        },
      });
    }
    return _ok({
      'error': {'code': 'not_found', 'message': 'no route in the fake'},
    }, 404);
  }
}

void main() {
  late CacheDb db;
  late FakeServer server;
  late SyncEngine sync;

  setUp(() async {
    db = CacheDb(NativeDatabase.memory());
    server = FakeServer();
    sync = SyncEngine(
      db: db,
      api: ApiClient(
        base: Uri.parse('https://chat.example.org'),
        tokens: Tokens(
          access: 'a',
          refresh: 'r',
          expiresAt: DateTime.now().add(const Duration(hours: 1)),
        ),
        httpClient: MockClient(server.handle),
      ),
    );
    await db.upsertConversation(
      Conversation.fromJson(conv('c1', lastSeq: 10)),
      syncedSeq: 10,
    );
  });

  tearDown(() => db.close());

  Future<int> synced([String id = 'c1']) async =>
      (await db.conversation(id))!.syncedSeq;

  group('applying events by sequence number', () {
    test(
      'the next event is applied, with the new position, in order',
      () async {
        await sync.applyEvent(created(msg('m11', 11)));
        await sync.applyEvent(created(msg('m12', 12, text: 'second')));
        expect((await db.messages('c1')).map((m) => m.id), ['m11', 'm12']);
        expect(await synced(), 12);
        // The conversation's own newest number moves with it.
        expect((await db.conversation('c1'))!.conversation.lastSeq, 12);
      },
    );

    test('an event already applied is skipped: replays and duplicates are harmless', () async {
      await sync.applyEvent(created(msg('m11', 11)));
      await sync.applyEvent({
        'type': 'reaction.added',
        'at': 't',
        'conversation_id': 'c1',
        'seq': 12,
        'message_id': 'm11',
        'account_id': sam,
        'reaction': {
          'emoji': '👍',
          'count': 1,
          'me': false,
          'account_ids': [sam],
        },
      });
      // The whole run again, as a reconnect might deliver it.
      await sync.applyEvent(created(msg('m11', 11, text: 'a stale copy')));
      await sync.applyEvent({
        'type': 'reaction.added',
        'at': 't',
        'conversation_id': 'c1',
        'seq': 12,
        'message_id': 'm11',
        'account_id': sam,
        'reaction': {
          'emoji': '👍',
          'count': 99,
          'me': false,
          'account_ids': [sam],
        },
      });
      final m = (await db.messages('c1')).single;
      expect(m.text, 'hi');
      expect(m.reactions.single.count, 1);
      expect(await synced(), 12);
    });

    test('a gap is filled with listEvents for exactly the missing range, before going on', () async {
      server.events['c1'] = [
        created(msg('m11', 11)),
        created(msg('m12', 12)),
        created(msg('m13', 13)),
      ];
      await sync.applyEvent(created(msg('m14', 14)));
      expect(server.log, [
        'GET /api/v1/conversations/c1/events?after=10&until=13&limit=200',
      ]);
      expect((await db.messages('c1')).map((m) => m.seq), [11, 12, 13, 14]);
      expect(await synced(), 14);
    });

    test(
      'a gap the server cannot fill (a shared conversation) may stay a gap',
      () async {
        server.events['c1'] = [created(msg('m12', 12))];
        await sync.applyEvent(created(msg('m14', 14)));
        expect((await db.messages('c1')).map((m) => m.seq), [12, 14]);
        expect(await synced(), 14);
      },
    );

    test('a gap too old to fill reloads the conversation', () async {
      await db.upsertMessages([Message.fromJson(msg('old', 9))]);
      server.reloadEvents = true;
      server.pages['c1'] = [msg('m30', 30), msg('m31', 31)];
      await sync.applyEvent(created(msg('m32', 32)));
      expect((await db.messages('c1')).map((m) => m.id), ['m30', 'm31', 'm32']);
      expect(await synced(), 32);
    });

    test('an event this build does not know still takes its place in the numbering', () async {
      await sync.applyEvent({
        'type': 'hologram.projected',
        'at': 't',
        'conversation_id': 'c1',
        'seq': 11,
      });
      await sync.applyEvent(created(msg('m12', 12)));
      expect(await synced(), 12);
      expect(server.log, isEmpty, reason: 'no gap was seen');
    });
  });

  group('what each event does', () {
    setUp(() => sync.applyEvent(created(msg('m11', 11))));

    test('edit and delete replace the message; one outside the cache is left alone', () async {
      await sync.applyEvent({
        'type': 'message.edited',
        'at': 't',
        'conversation_id': 'c1',
        'seq': 12,
        'message': msg(
          'm11',
          11,
          text: 'edited',
          extra: {'edited_at': '2026-11-13T09:40:00.000Z'},
        ),
      });
      expect((await db.message('m11'))!.text, 'edited');
      await sync.applyEvent({
        'type': 'message.edited',
        'at': 't',
        'conversation_id': 'c1',
        'seq': 13,
        'message': msg('ancient', 2, text: 'x'),
      });
      expect(await db.message('ancient'), isNull);
      await sync.applyEvent({
        'type': 'message.deleted',
        'at': 't',
        'conversation_id': 'c1',
        'seq': 14,
        'message': msg(
          'm11',
          11,
          text: '',
          extra: {'deleted_at': '2026-11-13T09:50:00.000Z'},
        ),
      });
      expect((await db.message('m11'))!.isDeleted, isTrue);
    });

    test('reactions: the entry after the change replaces the old one; count 0 removes it', () async {
      Json ev(int seq, String type, String emoji, int count) => {
        'type': type,
        'at': 't',
        'conversation_id': 'c1',
        'seq': seq,
        'message_id': 'm11',
        'account_id': sam,
        'reaction': {
          'emoji': emoji,
          'count': count,
          'me': false,
          'account_ids': [sam],
        },
      };
      await sync.applyEvent(ev(12, 'reaction.added', '👍', 1));
      await sync.applyEvent(ev(13, 'reaction.added', '🚀', 1));
      await sync.applyEvent(ev(14, 'reaction.added', '👍', 2));
      expect(
        (await db.message('m11'))!.reactions.map((r) => '${r.emoji}${r.count}'),
        ['👍2', '🚀1'],
      );
      await sync.applyEvent(ev(15, 'reaction.removed', '👍', 0));
      expect((await db.message('m11'))!.reactions.map((r) => r.emoji), ['🚀']);
    });

    test('a tick changes the task state, not the text', () async {
      await sync.applyEvent(
        created(
          msg(
            'm12',
            12,
            text: '- [ ] one',
            extra: {
              'tasks': [
                {'id': 't1', 'checked': false},
              ],
            },
          ),
        ),
      );
      await sync.applyEvent({
        'type': 'task.changed',
        'at': 't',
        'conversation_id': 'c1',
        'seq': 13,
        'message_id': 'm12',
        'task': {
          'id': 't1',
          'checked': true,
          'checked_by_id': sam,
          'checked_at': '2026-11-13T09:41:02.118Z',
        },
      });
      final m = (await db.message('m12'))!;
      expect(m.task('t1')!.checked, isTrue);
      expect(m.task('t1')!.checkedById, sam);
      expect(m.text, '- [ ] one');
    });

    test('a reply updates the parent\'s thread summary; deleting it lowers the count', () async {
      await sync.applyEvent(created(msg('r1', 12, parent: 'm11')));
      await sync.applyEvent(created(msg('r2', 13, parent: 'm11', author: me)));
      var t = (await db.message('m11'))!.thread!;
      expect(t.replyCount, 2);
      expect(t.lastReplySeq, 13);
      expect(t.participantIds, [sam, me]);
      await sync.applyEvent({
        'type': 'message.deleted',
        'at': 't',
        'conversation_id': 'c1',
        'seq': 14,
        'message': msg(
          'r1',
          12,
          parent: 'm11',
          text: '',
          extra: {'deleted_at': '2026-11-13T09:50:00.000Z'},
        ),
      });
      t = (await db.message('m11'))!.thread!;
      expect(t.replyCount, 1);
    });

    test('a form event replaces the form on its message', () async {
      await sync.applyEvent(
        created(
          msg(
            'poll',
            12,
            text: '',
            extra: {
              'form': {'id': 'f', 'preset': 'poll', 'answer_count': 0},
            },
          ),
        ),
      );
      await sync.applyEvent({
        'type': 'form.answered',
        'at': 't',
        'conversation_id': 'c1',
        'seq': 13,
        'message_id': 'poll',
        'form': {'id': 'f', 'preset': 'poll', 'answer_count': 1},
      });
      expect((await db.message('poll'))!.form!['answer_count'], 1);
    });

    test(
      'conversation.updated carries the conversation whole, parent included',
      () async {
        await sync.applyEvent({
          'type': 'conversation.updated',
          'at': 't',
          'conversation_id': 'c1',
          'seq': 12,
          'conversation': {
            ...conv('c1', lastSeq: 12, parentId: 'c0'),
            'name': 'Renamed',
          },
        });
        final c = (await db.conversation('c1'))!;
        expect(c.conversation.name, 'Renamed');
        expect(c.conversation.parentId, 'c0');
        expect(c.syncedSeq, 12);
      },
    );

    test(
      'thread.moved drops the replies and puts the pointer in their place',
      () async {
        await sync.applyEvent(created(msg('r1', 12, parent: 'm11')));
        await sync.applyEvent({
          'type': 'thread.moved',
          'at': 't',
          'conversation_id': 'c1',
          'seq': 13,
          'to_conversation_id': 'c5',
          'parent': msg(
            'm11',
            11,
            extra: {
              'thread': {
                'reply_count': 1,
                'last_reply_seq': 13,
                'last_reply_at': '2026-11-13T09:50:11.000Z',
                'participant_ids': [sam],
              },
            },
          ),
          'pointer': msg(
            'ptr',
            13,
            parent: 'm11',
            text: '',
            extra: {
              'system': {'kind': 'thread_moved', 'conversation_id': 'c5'},
            },
          ),
        });
        expect((await db.messages('c1')).map((m) => m.id), ['m11', 'ptr']);
      },
    );

    test('when this account leaves, the conversation is dropped; when another does, it is not', () async {
      await sync.refreshAccountState();
      server.log.clear();
      await db.upsertConversation(
        Conversation.fromJson(conv('c1', lastSeq: 11)),
        syncedSeq: 11,
      );
      await sync.applyEvent({
        'type': 'member.left',
        'at': 't',
        'conversation_id': 'c1',
        'seq': 12,
        'account_id': sam,
        'reason': 'left',
      });
      expect(await db.conversation('c1'), isNotNull);
      await sync.applyEvent({
        'type': 'member.left',
        'at': 't',
        'conversation_id': 'c1',
        'seq': 13,
        'account_id': me,
        'reason': 'removed',
      });
      expect(await db.conversation('c1'), isNull);
      expect(await db.messages('c1'), isEmpty);
    });

    test('joining: member.joined carries the conversation, which starts at that event', () async {
      await sync.applyEvent({
        'type': 'member.joined',
        'at': 't',
        'conversation_id': 'c7',
        'seq': 40,
        'member': {},
        'conversation': conv('c7', lastSeq: 40),
      });
      expect(await synced('c7'), 40);
      await sync.applyEvent(created(msg('n1', 41, c: 'c7')));
      expect((await db.messages('c7')).single.id, 'n1');
    });

    test('a message marked notify or badge becomes an item of the notifications list', () async {
      await sync.applyEvent(created(msg('m12', 12), notify: 'notify'));
      await sync.applyEvent(created(msg('m13', 13), notify: 'none'));
      expect(await db.unclearedNotifications(), 1);
      await sync.applyEvent({
        'type': 'read_position.moved',
        'at': 't',
        'conversation_id': 'c1',
        'read_seq': 13,
        'unread_count': 0,
        'mention_count': 0,
        'badge_count': 0,
        'total': {
          'badge': 0,
          'mentions': 0,
          'unread_conversations': 0,
          'unread_threads': 0,
        },
      });
      expect(await db.unclearedNotifications(), 0);
    });
  });

  group('account events (no seq)', () {
    test(
      'badge.changed and read_position.moved set the counts and the totals',
      () async {
        await sync.applyEvent({
          'type': 'badge.changed',
          'at': 't',
          'conversation_id': 'c1',
          'unread_count': 4,
          'mention_count': 1,
          'badge_count': 1,
          'total': {
            'badge': 4,
            'mentions': 2,
            'unread_conversations': 3,
            'unread_threads': 1,
          },
        });
        var m = (await db.conversation('c1'))!.conversation.membership!;
        expect([m.unreadCount, m.mentionCount, m.badgeCount], [4, 1, 1]);
        expect((await db.statusSummary()).badge, 4);
        expect(
          await synced(),
          10,
          reason: 'an account event moves no position',
        );
        await sync.applyEvent({
          'type': 'read_position.moved',
          'at': 't',
          'conversation_id': 'c1',
          'read_seq': 10,
          'unread_count': 0,
          'mention_count': 0,
          'badge_count': 0,
          'total': {
            'badge': 3,
            'mentions': 1,
            'unread_conversations': 2,
            'unread_threads': 1,
          },
        });
        m = (await db.conversation('c1'))!.conversation.membership!;
        expect([m.readSeq, m.unreadCount, m.badgeCount], [10, 0, 0]);
        expect((await db.statusSummary()).badge, 3);
      },
    );

    test('membership.updated carries mode and show', () async {
      await sync.applyEvent({
        'type': 'membership.updated',
        'at': 't',
        'conversation_id': 'c1',
        'membership': {
          'level': 'member',
          'read_seq': 10,
          'mode': 'nothing',
          'show': 'hidden',
          'starred': true,
          'unread_count': 0,
          'mention_count': 0,
          'badge_count': 0,
        },
      });
      final m = (await db.conversation('c1'))!.conversation.membership!;
      expect(m.mode, NotifyMode.nothing);
      expect(m.show, ShowSetting.hidden);
      expect(m.starred, isTrue);
    });

    test(
      'thread_state.changed writes the row, and drops it for "neither"',
      () async {
        await sync.applyEvent({
          'type': 'thread_state.changed',
          'at': 't',
          'parent_id': 'p',
          'conversation_id': 'c1',
          'thread_state': {
            'parent_id': 'p',
            'conversation_id': 'c1',
            'state': 'following',
            'read_seq': 4830,
            'unread_count': 0,
          },
        });
        expect((await db.threadStates('c1'))['p']!.state, 'following');
        await sync.applyEvent({
          'type': 'thread_state.changed',
          'at': 't',
          'parent_id': 'p',
          'conversation_id': 'c1',
        });
        expect(await db.threadStates('c1'), isEmpty);
      },
    );
  });

  group('connect and reconnect', () {
    test('positions are every conversation held and its synced_seq', () async {
      await db.upsertConversation(
        Conversation.fromJson(conv('c2', lastSeq: 96)),
        syncedSeq: 96,
      );
      expect(await sync.positions(), {'c1': 10, 'c2': 96});
    });

    test('reload, too_far_behind: cached messages go, the newest page comes, and it carries on from last_seq', () async {
      await db.upsertMessages([Message.fromJson(msg('stale', 9))]);
      server.pages['c1'] = [for (var s = 9060; s <= 9120; s++) msg('m$s', s)];
      await sync.onReload({
        'type': 'reload',
        'conversation_id': 'c1',
        'reason': 'too_far_behind',
        'last_seq': 9120,
      });
      final cached = await db.messages('c1');
      expect(cached, hasLength(50));
      expect(cached.first.seq, 9071);
      expect(await db.message('stale'), isNull);
      expect(await synced(), 9120);
      expect((await db.conversation('c1'))!.historyComplete, isFalse);
    });

    test(
      'reload, no_longer_member or unknown: the conversation is dropped',
      () async {
        await sync.onReload({
          'type': 'reload',
          'conversation_id': 'c1',
          'reason': 'no_longer_member',
        });
        expect(await db.conversation('c1'), isNull);
      },
    );

    test('after ready: account state is fetched again, new conversations are taken in at their last_seq, left ones dropped', () async {
      await db.upsertConversation(
        Conversation.fromJson(conv('gone')),
        syncedSeq: 5,
      );
      server.conversations = {
        'c1': conv(
          'c1',
          lastSeq: 10,
          membership: {
            'level': 'member',
            'read_seq': 8,
            'mode': 'any',
            'starred': false,
            'unread_count': 2,
            'mention_count': 0,
            'badge_count': 2,
          },
        ),
        'c9': conv('c9', lastSeq: 77, parentId: 'outside'),
        'outside': {...conv('outside'), 'name': 'Tickets'}
          ..remove('membership'),
      };
      await sync.onReady(['c9']);
      expect(
        await synced(),
        10,
        reason: 'a held conversation keeps its own position',
      );
      expect(
        (await db.conversation('c1'))!.conversation.membership!.badgeCount,
        2,
      );
      expect(await synced('c9'), 77);
      expect(await db.conversation('gone'), isNull);
      expect((await db.me())!.id, me);
      expect((await db.statusSummary()).badge, 2);
      expect((await db.sections()).single.name, 'Projects');
      expect((await db.accounts())[sam]!.name, 'Sam');
      // The parent the account is not in is fetched for its name, and is
      // not one of the account's conversations.
      expect((await db.outsideParents())['outside']!.name, 'Tickets');
      expect(await db.conversation('outside'), isNull);
      expect((await sync.positions()).keys, unorderedEquals(['c1', 'c9']));
    });
  });

  group('order: where a message is shown', () {
    test('the window is the last to arrive by seq, drawn sorted by order; a late message takes its place', () async {
      for (final m in [
        msg('m11', 11, order: 'b'),
        msg('m12', 12, order: 'd'),
        msg('m13', 13, order: 'f'),
        // Arrived last, belongs between m11 and m12.
        msg('late', 14, order: 'c', extra: {'late': true}),
      ]) {
        await sync.applyEvent(created(m));
      }
      final all = await db.messages('c1');
      expect(all.map((m) => m.id), ['m11', 'late', 'm12', 'm13']);
      expect(all[1].late, isTrue);
      // The window of the last three to arrive keeps the late one (seq
      // 14) and drops m11 (seq 11), whatever their order.
      expect((await db.messages('c1', limit: 3)).map((m) => m.id), [
        'late',
        'm12',
        'm13',
      ]);
    });

    test('a thread and its cached replies are sorted by order', () async {
      await sync.applyEvent(created(msg('p', 11, order: 'a')));
      await sync.applyEvent(created(msg('r1', 12, parent: 'p', order: 'c')));
      await sync.applyEvent(
        created(msg('r2', 13, parent: 'p', order: 'b', extra: {'late': true})),
      );
      expect((await db.thread('p')).map((m) => m.id), ['p', 'r2', 'r1']);
      expect((await db.cachedReplies('p', limit: 1)).map((m) => m.id), ['r1']);
      expect((await db.cachedReplies('p')).map((m) => m.id), ['r2', 'r1']);
    });

    test(
      'message.placed replaces the message and it is sorted again',
      () async {
        await sync.applyEvent(created(msg('m11', 11, order: 'b')));
        await sync.applyEvent(created(msg('m12', 12, order: 'd')));
        await sync.applyEvent(
          created(
            msg('mine', 13, author: me, order: 'f', extra: {'unsent': true}),
          ),
        );
        expect((await db.messages('c1')).map((m) => m.id), [
          'm11',
          'm12',
          'mine',
        ]);
        await sync.applyEvent({
          'type': 'message.placed',
          'at': 't',
          'conversation_id': 'c1',
          'seq': 14,
          'message': msg(
            'mine',
            13,
            author: me,
            order: 'a',
            extra: {'unsent': true},
          ),
        });
        expect((await db.messages('c1')).map((m) => m.id), [
          'mine',
          'm11',
          'm12',
        ]);
        expect((await db.message('mine'))!.order, 'a');
        expect(await synced(), 14, reason: 'it is a stored event');
        // One outside the cached run is left alone.
        await sync.applyEvent({
          'type': 'message.placed',
          'at': 't',
          'conversation_id': 'c1',
          'seq': 15,
          'message': msg('ancient', 2, order: 'a'),
        });
        expect(await db.message('ancient'), isNull);
      },
    );

    test('message.sent clears unsent, and is not numbered', () async {
      await sync.applyEvent(
        created(msg('mine', 11, author: me, extra: {'unsent': true})),
      );
      expect((await db.message('mine'))!.unsent, isTrue);
      await sync.applyEvent({
        'type': 'message.sent',
        'at': 't',
        'conversation_id': 'c1',
        'message_id': 'mine',
      });
      final m = (await db.message('mine'))!;
      expect(m.unsent, isFalse);
      expect(m.raw.containsKey('unsent'), isFalse);
      expect(await synced(), 11);
    });
  });

  group('history', () {
    test(
      'scrolling back extends the cached run downwards until the start',
      () async {
        server.pages['c1'] = [for (var s = 1; s <= 80; s++) msg('m$s', s)];
        expect(await sync.loadOlder('c1'), isTrue);
        expect((await db.messages('c1', limit: 500)).first.seq, 31);
        expect(await sync.loadOlder('c1'), isFalse);
        expect((await db.messages('c1', limit: 500)), hasLength(80));
        expect((await db.conversation('c1'))!.historyComplete, isTrue);
        server.log.clear();
        expect(await sync.loadOlder('c1'), isFalse);
        expect(server.log, isEmpty, reason: 'nothing older to ask for');
      },
    );
  });

  group('sending', () {
    test('held until ready, then posted with its client_id and reconciled by the answer', () async {
      final id = await sync.send('c1', 'hello');
      await pumpEventQueue();
      expect(server.posts, isEmpty, reason: 'the connection is not ready');
      expect((await db.outbox()).single.clientId, id);

      server.conversations = {'c1': conv('c1', lastSeq: 10)};
      await sync.onReady([]);
      expect(server.posts.single, {'client_id': id, 'text': 'hello'});
      expect(await db.outbox(), isEmpty);
      expect((await db.messages('c1')).single.text, 'hello');
    });

    test('reconciled by the event when it comes first: one message, no pending row', () async {
      server.conversations = {'c1': conv('c1', lastSeq: 10)};
      await sync.onReady([]);
      late String clientId;
      server.onPost = (r) {
        clientId = (jsonDecode(r.body) as Json)['client_id'] as String;
        return null;
      };
      // The event arrives on the socket before the HTTP answer is read.
      final sending = sync.send('c1', 'racing');
      await sending;
      await pumpEventQueue();
      await sync.applyEvent(
        created(
          msg('m-$clientId', 102, author: me, text: 'racing'),
          clientId: clientId,
        ),
      );
      await pumpEventQueue();
      expect(await db.outbox(), isEmpty);
      expect(
        (await db.messages('c1')).where((m) => m.text == 'racing'),
        hasLength(1),
      );
    });

    test('a refused message is marked failed with the server\'s sentence, and can be retried', () async {
      server.conversations = {'c1': conv('c1', lastSeq: 10)};
      await sync.onReady([]);
      server.onPost = (r) => http.Response(
        '{"error":{"code":"archived","message":"This channel is archived."}}',
        403,
      );
      final id = await sync.send('c1', 'too late');
      await pumpEventQueue();
      final item = (await db.outbox()).single;
      expect(item.failed, isTrue);
      expect(item.error, 'This channel is archived.');

      server.onPost = null;
      await sync.retry(id);
      await pumpEventQueue();
      expect(await db.outbox(), isEmpty);
      expect(server.posts.map((p) => p['client_id']), [
        id,
        id,
      ], reason: 'the same client_id, so a retry cannot post twice');
    });

    test('a network failure leaves it pending; the outbox goes out, oldest first, after the next ready', () async {
      server.conversations = {'c1': conv('c1', lastSeq: 10)};
      await sync.onReady([]);
      server.onPost = (r) => throw http.ClientException('offline');
      await sync.send('c1', 'first');
      await pumpEventQueue();
      await sync.send('c1', 'second');
      await pumpEventQueue();
      expect((await db.outbox()).map((o) => o.state), ['pending', 'pending']);

      server.onPost = null;
      server.posts.clear();
      await sync.positions(); // a reconnect: sends are held again
      await sync.onReady([]);
      expect(server.posts.map((p) => p['text']), ['first', 'second']);
      expect(await db.outbox(), isEmpty);
    });
  });

  test('reading: the database first, then the server', () async {
    await db.upsertConversation(
      Conversation.fromJson(
        conv(
          'c1',
          lastSeq: 14,
          membership: {
            'level': 'member',
            'read_seq': 10,
            'mode': 'any',
            'starred': false,
            'unread_count': 4,
            'mention_count': 1,
            'badge_count': 4,
          },
        ),
      ),
    );
    await sync.markRead('c1', 14);
    final m = (await db.conversation('c1'))!.conversation.membership!;
    expect([m.readSeq, m.unreadCount, m.badgeCount], [14, 0, 0]);
    expect(server.log, ['PUT /api/v1/conversations/c1/read']);
    server.log.clear();
    await sync.markRead('c1', 14);
    expect(server.log, isEmpty, reason: 'already read');
  });

  test('watched queries tell the screen when sync writes', () async {
    final seen = <int>[];
    final sub = db.watchMessages('c1').listen((m) => seen.add(m.length));
    await pumpEventQueue();
    await sync.applyEvent(created(msg('m11', 11)));
    await pumpEventQueue();
    await sync.applyEvent(created(msg('m12', 12)));
    await pumpEventQueue();
    expect(seen, [0, 1, 2]);
    await sub.cancel();
  });
}
