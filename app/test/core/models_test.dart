import 'dart:convert';

import 'package:churr/core/api/errors.dart';
import 'package:churr/core/models/account.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:churr/core/models/json.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/core/models/server.dart';
import 'package:flutter_test/flutter_test.dart';

/// The message object of docs/MESSAGE-FORMAT.md section 1, as written
/// there, with a file and a card added.
const _message = '''
{
  "id": "0193a6f2-7c1e-7b3a-9f0e-2a4d5c6b7e8f",
  "conversation_id": "0193a6f0-0000-7000-8000-000000000001",
  "seq": 4821,
  "author_id": "0193a000-0000-7000-8000-000000000001",
  "parent_id": null,
  "quote": null,
  "text": "**Deploy** at 5.",
  "entities": [
    {"type": "account", "at": 17, "len": 4, "account_id": "0193a001-0000-7000-8000-000000000001"},
    {"type": "task", "at": 43, "len": 21, "item_id": "t1"},
    {"type": "hologram", "at": 70, "len": 2}
  ],
  "tasks": [
    {"id": "t1", "checked": true, "checked_by_id": "0193a001-0000-7000-8000-000000000001", "checked_at": "2026-11-13T09:41:02.118Z"},
    {"id": "t2", "checked": false}
  ],
  "files": [
    {"id": "f1", "name": "floorplan.png", "size": 482113, "media_type": "image/png", "kind": "image",
     "width": 1600, "height": 900, "caption": "Second floor",
     "url": "/api/v1/files/f1/floorplan.png", "thumb_url": "/api/v1/files/f1/thumb"},
    {"id": "f2", "name": "scene.usdz", "size": 9, "media_type": "model/usd", "kind": "model3d"}
  ],
  "cards": [
    {"color": "pink", "author": {"name": "CI", "icon_file_id": null}, "title": "Build #482 failed",
     "url": "https://ci.example/builds/482", "text": "`main`",
     "fields": [{"label": "Branch", "value": "main", "short": true}],
     "footer": "ci.example", "at": "2026-11-13T09:28:10.000Z", "sparkle": 3}
  ],
  "form": null,
  "fallback": "",
  "reactions": [
    {"emoji": "👍", "count": 3, "me": true, "account_ids": ["a", "b", "c"]}
  ],
  "thread": {"reply_count": 2, "last_reply_seq": 4830, "last_reply_at": "2026-11-13T09:50:11.000Z", "participant_ids": ["a"]},
  "via": null,
  "pinned": false,
  "notified_anyway": false,
  "has_history": false,
  "created_at": "2026-11-13T09:30:00.000Z",
  "edited_at": null,
  "deleted_at": null,
  "deleted_by_id": null,
  "a_field_from_the_future": {"x": 1}
}''';

void main() {
  test('a message parses with all its parts', () {
    final m = Message.fromJson(jsonDecode(_message) as Json);
    expect(m.seq, 4821);
    expect(m.parentId, isNull);
    expect(m.isReply, isFalse);
    expect(m.entities.map((e) => e.type), ['account', 'task', 'hologram']);
    expect(m.entities[1].itemId, 't1');
    expect(m.task('t1')!.checked, isTrue);
    expect(m.task('t1')!.checkedAt, DateTime.utc(2026, 11, 13, 9, 41, 2, 118));
    expect(m.files.first.kind, FileKind.image);
    expect(m.files.first.thumbUrl, '/api/v1/files/f1/thumb');
    expect(m.cards.single.color, 'pink');
    expect(m.cards.single.authorName, 'CI');
    expect(m.cards.single.fields.single.short, isTrue);
    expect(m.reactions.single.me, isTrue);
    expect(m.thread!.replyCount, 2);
    expect(m.createdAt, DateTime.utc(2026, 11, 13, 9, 30));
  });

  test('what a client does not understand (MESSAGE-FORMAT.md section 10)', () {
    final m = Message.fromJson(jsonDecode(_message) as Json);
    // An unknown file kind is drawn as a plain file.
    expect(m.files[1].kind, FileKind.file);
    // An unknown entity type is kept, to be drawn as the text it covers.
    expect(m.entities.last.type, 'hologram');
    // Unknown fields are ignored, and survive storage.
    expect(m.toJson()['a_field_from_the_future'], {'x': 1});
  });

  test('copyWith changes what changes without an edit, in the API shape', () {
    final m = Message.fromJson(jsonDecode(_message) as Json);
    final next = m.copyWith(
      reactions: [
        Reaction(emoji: '🚀', count: 1, me: false, accountIds: ['z']),
      ],
      tasks: [TaskState(id: 't1', checked: false)],
    );
    expect(next.reactions.single.emoji, '🚀');
    expect(next.task('t1')!.checked, isFalse);
    expect((next.toJson()['reactions'] as List).single['account_ids'], ['z']);
    expect(next.toJson()['a_field_from_the_future'], {'x': 1});
    expect(m.reactions.single.emoji, '👍', reason: 'the original is untouched');
    expect(m.copyWith(clearThread: true).thread, isNull);
  });

  group('order, late and unsent', () {
    Message of(Json extra) =>
        Message.fromJson({...jsonDecode(_message) as Json, ...extra});

    test('parsed when present; absent means not late and not unsent', () {
      final plain = of({'order': '0000004821'});
      expect(plain.seq, 4821);
      expect(plain.order, '0000004821');
      expect(plain.late, isFalse);
      expect(plain.unsent, isFalse);
      final shared = of({
        'order': '2026-11-13T09:29:58.120Z/7f',
        'late': true,
        'unsent': true,
      });
      expect(shared.late, isTrue);
      expect(shared.unsent, isTrue);
      expect(shared.order, '2026-11-13T09:29:58.120Z/7f');
    });

    test('the server\'s old pending flag means nothing any more', () {
      final m = of({'order': 'a', 'pending': true});
      expect(m.unsent, isFalse);
      expect(m.toJson()['pending'], isTrue, reason: 'kept as an unknown field');
    });

    test('display order is order as plain text, not seq', () {
      final list = [
        of({'id': 'b', 'seq': 1, 'order': 'b'}),
        of({'id': 'late', 'seq': 3, 'order': 'a', 'late': true}),
        of({'id': 'c', 'seq': 2, 'order': 'c'}),
        // Compared character by character, not as numbers.
        of({'id': 'ten', 'seq': 4, 'order': '10'}),
        of({'id': 'nine', 'seq': 5, 'order': '9'}),
      ]..sort(Message.compareOrder);
      expect(list.map((m) => m.id), ['ten', 'nine', 'late', 'b', 'c']);
    });

    test('copyWith clears unsent in the API shape', () {
      final m = of({'order': 'a', 'unsent': true});
      final sent = m.copyWith(unsent: false);
      expect(sent.unsent, isFalse);
      expect(sent.toJson().containsKey('unsent'), isFalse);
      expect(sent.order, 'a');
      expect(m.copyWith(reactions: const []).unsent, isTrue);
    });
  });

  test(
    'a conversation carries parent_id and the membership with mode and show',
    () {
      final c = Conversation.fromJson({
        'id': 'c1',
        'kind': 'channel',
        'private': false,
        'name': 'Icons',
        'topic': '',
        'parent_id': 'c0',
        'default_level': 'member',
        'slow_mode_seconds': 0,
        'created_at': '2026-11-14T02:30:45.597Z',
        'last_seq': 4,
        'member_count': 2,
        'membership': {
          'level': 'manager',
          'via_group': false,
          'joined_at': '2026-11-14T02:30:45.597Z',
          'read_seq': 3,
          'mode': 'nothing',
          'show': 'activity',
          'starred': true,
          'unread_count': 1,
          'mention_count': 0,
          'badge_count': 0,
        },
      });
      expect(c.parentId, 'c0');
      expect(c.isNamed, isTrue);
      expect(c.membership!.mode, NotifyMode.nothing);
      expect(c.membership!.show, ShowSetting.activity);
      expect(c.membership!.starred, isTrue);
      // The membership round-trips through the stored shape.
      final again = Conversation.fromJson(c.toJson());
      expect(again.membership!.show, ShowSetting.activity);
      expect(again.membership!.unreadCount, 1);
    },
  );

  test(
    'show absent means the app default; an unknown mode is read as mentions',
    () {
      final m = Membership.fromJson({
        'level': 'member',
        'read_seq': 0,
        'mode': 'whisper',
      });
      expect(m.show, isNull);
      expect(m.mode, NotifyMode.mentions);
      expect(m.toJson().containsKey('show'), isFalse);
    },
  );

  test('an ad-hoc channel has no name and is not named', () {
    final c = Conversation.fromJson({
      'id': 'c',
      'kind': 'channel',
      'private': true,
      'member_ids': ['a', 'b'],
    });
    expect(c.isAdHoc, isTrue);
    expect(c.isNamed, isFalse);
  });

  test('mute runs until its time', () {
    final m = Membership.fromJson({
      'level': 'member',
      'read_seq': 0,
      'mode': 'any',
      'muted_until': '2026-11-14T10:00:00.000Z',
    });
    expect(m.mutedAt(DateTime.utc(2026, 11, 14, 9)), isTrue);
    expect(m.mutedAt(DateTime.utc(2026, 11, 14, 11)), isFalse);
  });

  test('the account, and Me with what only the caller sees', () {
    final me = Me.fromJson({
      'id': 'a1',
      'username': 'kai',
      'role': 'owner',
      'display_name': 'Kai Tanaka',
      'presence': 'online',
      'created_at': '2026-11-14T02:30:45.573Z',
      'prefs': {
        'thread_view': 'flat',
        'meeting_pauses': false,
        'allow_notify_anyway': true,
      },
      'dnd_state': {
        'on': true,
        'sources': ['manual'],
      },
      'passkey_count': 1,
    });
    expect(me.name, 'Kai Tanaka');
    expect(me.threadView, 'flat');
    expect(me.dndOn, isTrue);
    expect(
      Account.fromJson({
        'id': 'b',
        'username': 'ci',
        'display_name': '',
        'role': 'bot',
      }).name,
      'ci',
    );
  });

  test('the error shape', () {
    final e = ApiException.fromResponse(429, {
      'error': {
        'code': 'slow_mode',
        'message': 'Wait a little.',
        'retry_after_seconds': 12,
      },
    });
    expect(e.code, 'slow_mode');
    expect(e.retryAfterSeconds, 12);
    expect(e.isTransient, isFalse);
    final v = ApiException.fromResponse(400, {
      'error': {
        'code': 'invalid_value',
        'message': 'Say whether the channel is private.',
        'field': 'private',
      },
    });
    expect(v.field, 'private');
    // A body in no known shape (a proxy's page) still becomes an error.
    expect(ApiException.fromResponse(502, null).code, 'internal');
    expect(ApiException.fromResponse(503, 'oops').isTransient, isTrue);
  });

  test('times are written as the API writes them', () {
    expect(
      formatTime(DateTime.utc(2026, 11, 13, 9, 30)),
      '2026-11-13T09:30:00.000Z',
    );
    expect(
      parseTime('2026-11-13T09:30:00.000Z'),
      DateTime.utc(2026, 11, 13, 9, 30),
    );
    expect(parseTime(null), isNull);
  });

  test('the status summary and the server description', () {
    expect(
      StatusSummary.fromJson({
        'badge': 5,
        'mentions': 1,
        'unread_conversations': 5,
        'unread_threads': 1,
      }).badge,
      5,
    );
    final s = ServerInfo.fromJson({
      'name': 'localhost',
      'community_name': 'Seeded',
      'version': 'dev',
      'min_app_version': '0.0.0',
      'setup_needed': false,
      'features': {'sealed_chats': true},
      'limits': {'file_size': 104857600, 'message_text': 16000},
    });
    expect(s.communityName, 'Seeded');
    expect(s.fileSizeLimit, 104857600);
    expect(s.features['sealed_chats'], isTrue);
  });
}
