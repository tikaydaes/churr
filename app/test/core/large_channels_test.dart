import 'package:churr/core/api/client.dart';
import 'package:churr/core/conversation/large.dart';
import 'package:churr/core/db/cache_db.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:churr/core/models/json.dart';
import 'package:churr/core/session_store.dart';
import 'package:churr/core/sync.dart';
import 'package:drift/native.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// Large channels and announcement channels (ruling R66): the rules read
/// off the conversation object, and what the sync engine does with
/// `conversation.changed` (docs/EVENTS.md section 4.1).
Conversation _c({
  String level = 'member',
  bool announcement = false,
  bool replies = false,
  bool mayPost = false,
  bool large = false,
  bool archived = false,
  bool member = true,
  int lastSeq = 10,
  int unread = 0,
}) => Conversation.fromJson({
  'id': 'c1',
  'kind': 'channel',
  'name': 'news',
  'private': false,
  'last_seq': lastSeq,
  'member_count': 2400,
  'announcement': announcement,
  'announcement_replies': replies,
  'large': large || announcement,
  'archived_at': ?(archived ? '2026-11-01T00:00:00Z' : null),
  if (member)
    'membership': {
      'level': level,
      'read_seq': 0,
      'mode': 'mentions',
      'may_post': mayPost,
      'unread_count': unread,
    },
});

void main() {
  group('who may write', () {
    test('an ordinary channel: members yes, readers and outsiders no', () {
      expect(cannotPost(_c(), manager: false), isNull);
      expect(cannotPost(_c(level: 'reader'), manager: false), NoPost.reader);
      expect(cannotPost(_c(member: false), manager: false), NoPost.notMember);
      expect(cannotPost(_c(archived: true), manager: true), NoPost.archived);
    });

    test('an announcement channel: managers and those they allow', () {
      final c = _c(announcement: true);
      expect(cannotPost(c, manager: false), NoPost.announcement);
      expect(cannotPost(c, manager: false, reply: true), NoPost.announcement);
      expect(cannotPost(c, manager: true), isNull);
      expect(
        cannotPost(_c(announcement: true, mayPost: true), manager: false),
        isNull,
      );
      // A reader is a reader whatever a manager allowed.
      expect(
        cannotPost(
          _c(announcement: true, mayPost: true, level: 'reader'),
          manager: false,
        ),
        NoPost.reader,
      );
    });

    test('replies, when the channel lets its members reply in threads', () {
      final c = _c(announcement: true, replies: true);
      expect(cannotPost(c, manager: false), NoPost.announcementReplyOnly);
      expect(cannotPost(c, manager: false, reply: true), isNull);
    });

    test('the member list is the managers\' in an announcement channel', () {
      expect(showsMembers(_c(), manager: false), isTrue);
      expect(showsMembers(_c(announcement: true), manager: false), isFalse);
      expect(showsMembers(_c(announcement: true), manager: true), isTrue);
    });

    test('large, lazy delivery and last seen are read as sent', () {
      expect(isLarge(_c()), isFalse);
      expect(isLarge(_c(large: true)), isTrue);
      expect(isLarge(_c(announcement: true)), isTrue);
      expect(lazyDelivery(_c()), 'server');
      expect(LastSeen.parse('within_week'), LastSeen.withinWeek);
      expect(LastSeen.parse('2026-11-15T09:00:00Z'), isNull);
      expect(LastSeen.parse(null), isNull);
    });

    test('may_post survives the cache', () {
      final back = Conversation.fromJson(
        _c(announcement: true, mayPost: true).toJson(),
      );
      expect(back.membership!.mayPost, isTrue);
    });
  });

  group('conversation.changed', () {
    late CacheDb db;
    late SyncEngine sync;
    final requests = <String>[];

    setUp(() async {
      requests.clear();
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
          httpClient: MockClient((r) async {
            requests.add('${r.method} ${r.url.path}');
            return http.Response('{}', 404);
          }),
        ),
      );
      await db.upsertConversation(_c(unread: 1), syncedSeq: 10);
    });

    tearDown(() => db.close());

    Json signal({int last = 14, int messages = 3, Json? counts, Json? event}) =>
        {
          'type': 'conversation.changed',
          'at': '2026-11-15T09:00:00.000Z',
          'conversation_id': 'c1',
          'last_seq': last,
          if (event == null) 'messages': messages,
          if (event == null) 'mentioned': false,
          ...?counts,
          'event': ?event,
        };

    test('moves last_seq and the unread count, and leaves the cache where it '
        'ends: nothing is fetched', () async {
      await sync.applyEvent(signal());
      final row = (await db.conversation('c1'))!;
      expect(row.conversation.lastSeq, 14);
      expect(row.conversation.membership!.unreadCount, 4);
      expect(row.syncedSeq, 10, reason: 'opening catches up from here');
      expect(requests, isEmpty, reason: 'a signal is not a gap to fill');
    });

    test('takes the numbers the server sends over its own count', () async {
      await sync.applyEvent(
        signal(
          counts: {
            'unread_count': 9,
            'mention_count': 2,
            'badge_count': 2,
            'total': {'badge': 5, 'mentions': 2, 'unread_conversations': 1},
          },
        ),
      );
      final m = (await db.conversation('c1'))!.conversation.membership!;
      expect((m.unreadCount, m.mentionCount, m.badgeCount), (9, 2, 2));
      expect((await db.statusSummary()).badge, 5);
    });

    test('a signal that is not newer counts nothing twice', () async {
      await sync.applyEvent(signal());
      await sync.applyEvent(signal());
      final m = (await db.conversation('c1'))!.conversation.membership!;
      expect(m.unreadCount, 4);
    });

    test('a message that notifies arrives inside the signal: it is announced, '
        'and not put among the cached messages', () async {
      final seen = <Json>[];
      final sub = sync.localEvents.listen(seen.add);
      await sync.applyEvent(
        signal(
          last: 15,
          event: {
            'type': 'message.created',
            'at': '2026-11-15T09:00:00.000Z',
            'conversation_id': 'c1',
            'seq': 15,
            'notify': 'notify',
            'message': {
              'id': 'm15',
              'conversation_id': 'c1',
              'seq': 15,
              'author_id': 'ana',
              'text': '@you look',
              'created_at': '2026-11-15T09:00:00.000Z',
            },
          },
        ),
      );
      await Future<void>.delayed(Duration.zero);
      await sub.cancel();
      expect(seen.map((e) => e['type']), ['local.notify']);
      expect(await db.message('m15'), isNull);
      final row = (await db.conversation('c1'))!;
      expect(row.conversation.lastSeq, 15);
      expect(row.syncedSeq, 10);
      expect(requests, isEmpty);
    });

    test('an event that is not message traffic is applied as it is', () async {
      await sync.applyEvent(
        signal(
          last: 11,
          event: {
            'type': 'conversation.updated',
            'at': '2026-11-15T09:00:00.000Z',
            'conversation_id': 'c1',
            'seq': 11,
            'conversation': {
              ..._c().toJson()..remove('membership'),
              'name': 'bulletins',
            },
          },
        ),
      );
      final row = (await db.conversation('c1'))!;
      expect(row.conversation.name, 'bulletins');
      expect(row.conversation.membership, isNotNull);
      expect(row.syncedSeq, 10);
    });

    test('presence.counts is passed on for the screens', () async {
      final seen = <Json>[];
      final sub = sync.localEvents.listen(seen.add);
      await sync.applyEvent({
        'type': 'presence.counts',
        'at': '2026-11-15T09:00:00.000Z',
        'online_count': 41,
        'conversations': {'c1': 17},
      });
      await Future<void>.delayed(Duration.zero);
      await sub.cancel();
      expect(seen.single['online_count'], 41);
    });
  });
}
