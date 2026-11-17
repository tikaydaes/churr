import 'package:churr/core/conversation/threads.dart';
import 'package:churr/core/models/message.dart';
import 'package:flutter_test/flutter_test.dart';

Message m(
  String id,
  int seq, {
  String text = 'x',
  String author = 'sam',
  String? parent,
  int? lastReply,
  int replies = 0,
  Map<String, Object?>? quote,
  bool deleted = false,
  String? order,
  bool late = false,
}) => Message.fromJson({
  'id': id,
  'conversation_id': 'c',
  'seq': seq,
  // Unless a test says otherwise, `order` follows `seq`.
  'order': order ?? 'o${seq.toString().padLeft(6, '0')}',
  'late': ?(late ? true : null),
  'author_id': author,
  'parent_id': ?parent,
  'text': deleted ? '' : text,
  if (lastReply != null)
    'thread': {'reply_count': replies, 'last_reply_seq': lastReply},
  'quote': ?quote,
  if (deleted) 'deleted_at': '2026-11-13T10:00:00.000Z',
  'created_at': '2026-11-13T09:00:00.000Z',
});

ThreadState st(String parent, String state, int read, {int unread = 0}) =>
    ThreadState(
      parentId: parent,
      conversationId: 'c',
      state: state,
      readSeq: read,
      unreadCount: unread,
    );

void main() {
  final messages = [
    m(
      'p',
      10,
      text: 'Deploy plan for Friday\nmore lines',
      lastReply: 24,
      replies: 2,
    ),
    m('a', 21),
    m('r1', 22, parent: 'p', author: 'lee'),
    m('b', 23),
    m('r2', 24, parent: 'p', author: 'lee', quote: {'message_id': 'r1'}),
  ];

  group('threaded view', () {
    test(
      'replies are left out and one marker sits where the latest reply would',
      () {
        final body = buildBody(
          messages: messages,
          states: {},
          flat: false,
          conversationReadSeq: 20,
          myId: 'me',
        );
        expect(
          body.map(
            (e) => e is MarkerEntry ? 'marker' : (e as MessageEntry).message.id,
          ),
          ['p', 'a', 'b', 'marker'],
        );
        final marker = body.last as MarkerEntry;
        expect(marker.unread, 2);
        expect(marker.label, '2 new replies to: Deploy plan for Friday');
      },
    );

    test(
      'the count follows the thread position, and reads "Replies to" at zero',
      () {
        var body = buildBody(
          messages: messages,
          states: {'p': st('p', 'following', 22)},
          flat: false,
          conversationReadSeq: 0,
          myId: 'me',
        );
        expect((body.last as MarkerEntry).unread, 1);
        body = buildBody(
          messages: messages,
          states: {'p': st('p', 'following', 24)},
          flat: false,
          conversationReadSeq: 0,
          myId: 'me',
        );
        expect(
          (body.last as MarkerEntry).label,
          'Replies to: Deploy plan for Friday',
        );
      },
    );

    test('no marker for an ignored thread', () {
      final body = buildBody(
        messages: messages,
        states: {'p': st('p', 'ignored', 0)},
        flat: false,
        conversationReadSeq: 0,
      );
      expect(body.whereType<MarkerEntry>(), isEmpty);
    });

    test('a later reply moves the marker down; the parent stays', () {
      final more = [...messages, m('c', 25), m('r3', 26, parent: 'p')];
      final withSummary = [
        m('p', 10, text: 'Deploy plan for Friday', lastReply: 26, replies: 3),
        ...more.skip(1),
      ];
      final body = buildBody(
        messages: withSummary,
        states: {},
        flat: false,
        conversationReadSeq: 0,
      );
      expect(
        body.map(
          (e) => e is MarkerEntry ? 'marker' : (e as MessageEntry).message.id,
        ),
        ['p', 'a', 'b', 'c', 'marker'],
      );
    });

    test('own replies and deleted replies do not count', () {
      final list = [
        m('p', 10, lastReply: 13, replies: 3),
        m('r1', 11, parent: 'p', author: 'me'),
        m('r2', 12, parent: 'p', deleted: true),
        m('r3', 13, parent: 'p'),
      ];
      final body = buildBody(
        messages: list,
        states: {},
        flat: false,
        conversationReadSeq: 0,
        myId: 'me',
      );
      expect((body.last as MarkerEntry).unread, 1);
    });

    test('the title is cut to 80 characters', () {
      expect(markerTitle(m('p', 1, text: 'a' * 100)).length, 80);
    });
  });

  group('order and late messages', () {
    String ids(List<BodyEntry> body) => body
        .map(
          (e) => e is MarkerEntry ? 'marker' : (e as MessageEntry).message.id,
        )
        .join(' ');

    test(
      'the body is sorted by order, not seq: a late message takes its place',
      () {
        final list = [
          m('a', 11, order: 'b'),
          m('b', 12, order: 'd'),
          m('late', 13, order: 'c', late: true),
        ];
        for (final flat in [false, true]) {
          expect(
            ids(
              buildBody(
                messages: list,
                states: {},
                flat: flat,
                conversationReadSeq: 0,
              ),
            ),
            'a late b',
          );
        }
      },
    );

    test('a marker sits by the order of its latest reply, not its seq', () {
      final list = [
        m('p', 10, order: 'a', lastReply: 14, replies: 2),
        m('x', 11, order: 'c'),
        m('r1', 12, parent: 'p', order: 'b'),
        m('y', 13, order: 'e'),
        // The highest seq, but ordered before y: the marker goes there.
        m('r2', 14, parent: 'p', order: 'd', late: true),
      ];
      expect(
        ids(
          buildBody(
            messages: list,
            states: {},
            flat: false,
            conversationReadSeq: 0,
          ),
        ),
        'p x marker y',
      );
      expect(
        ids(
          buildBody(
            messages: list,
            states: {},
            flat: true,
            conversationReadSeq: 0,
          ),
        ),
        'p r1 x r2 y',
      );
    });

    test('with no reply cached, the marker sits just after its parent', () {
      final list = [
        m('p', 10, order: 'a', lastReply: 9, replies: 1),
        m('x', 11, order: 'b'),
      ];
      expect(
        ids(
          buildBody(
            messages: list,
            states: {},
            flat: false,
            conversationReadSeq: 0,
          ),
        ),
        'p marker x',
      );
    });

    test('markers sort among themselves by order', () {
      final list = [
        m('p', 10, order: 'a', lastReply: 13, replies: 1),
        m('q', 11, order: 'b', lastReply: 12, replies: 1),
        m('rq', 12, parent: 'q', order: 'd'),
        m('rp', 13, parent: 'p', order: 'c', late: true),
        m('z', 14, order: 'e'),
      ];
      final body = buildBody(
        messages: list,
        states: {},
        flat: false,
        conversationReadSeq: 0,
      );
      expect(
        [
          for (final e in body)
            e is MarkerEntry
                ? 'marker-${e.parent.id}'
                : (e as MessageEntry).message.id,
        ],
        ['p', 'q', 'marker-p', 'marker-q', 'z'],
      );
    });
  });

  group('the "New" line', () {
    test('before the first unread message by order', () {
      final list = [
        m('a', 10, order: 'a'),
        m('b', 11, order: 'b'),
        m('c', 12, order: 'c'),
      ];
      expect(firstNewMessageId(list, 10), 'b');
      expect(firstNewMessageId(list, 12), isNull);
    });

    test('a late message is not where the line goes, though it is unread', () {
      // Read to 12. A late message (seq 14) sits up among what was read,
      // and a new one (seq 13) is at the bottom.
      final list = [
        m('a', 10, order: 'a'),
        m('late', 14, order: 'b', late: true),
        m('b', 12, order: 'c'),
        m('new', 13, order: 'd'),
      ];
      expect(firstNewMessageId(list, 12), 'new');
      // Only a late message unread: no line.
      expect(firstNewMessageId(list.take(3), 12), isNull);
    });

    test('no line when the first unread is the reader\'s own', () {
      final list = [
        m('a', 10, order: 'a'),
        m('mine', 11, order: 'b', author: 'me'),
        m('b', 12, order: 'c'),
      ];
      expect(firstNewMessageId(list, 10, myId: 'me'), isNull);
      expect(firstNewMessageId(list, 11, myId: 'me'), 'b');
    });
  });

  group('flat view', () {
    test('replies are in order with a header; no markers', () {
      final body = buildBody(
        messages: messages,
        states: {},
        flat: true,
        conversationReadSeq: 0,
      );
      expect(body.whereType<MarkerEntry>(), isEmpty);
      expect(body.map((e) => (e as MessageEntry).message.id), [
        'p',
        'a',
        'r1',
        'b',
        'r2',
      ]);
      final r1 = body[2] as MessageEntry;
      expect(r1.reply!.answeredId, 'p');
      expect(r1.reply!.showsParentLink, isFalse);
      final r2 = body[4] as MessageEntry;
      expect(r2.reply!.answeredId, 'r1');
      expect(r2.reply!.showsParentLink, isTrue);
      expect(r2.reply!.answered!.id, 'r1');
    });

    test('replies in ignored threads are left out', () {
      final body = buildBody(
        messages: messages,
        states: {'p': st('p', 'ignored', 0)},
        flat: true,
        conversationReadSeq: 0,
      );
      expect(body.map((e) => (e as MessageEntry).message.id), ['p', 'a', 'b']);
    });

    test('the view is the override, else the default', () {
      expect(
        isFlatView(conversationOverride: null, personDefault: 'flat'),
        isTrue,
      );
      expect(
        isFlatView(conversationOverride: 'threaded', personDefault: 'flat'),
        isFalse,
      );
      expect(
        isFlatView(conversationOverride: 'flat', personDefault: 'threaded'),
        isTrue,
      );
    });
  });

  group('quotes', () {
    test('a range, the first 200 characters, or null when changed', () {
      final q = m('q', 1, text: 'Hello there, world');
      expect(quoteText(q, Quote(messageId: 'q', start: 6, end: 11)), 'there');
      expect(quoteText(q, Quote(messageId: 'q')), 'Hello there, world');
      // A quoted message this reader cannot see: the server's text.
      final unseen = Quote(messageId: 'gone', text: 'old words');
      expect(unseen.unseen(null), isTrue);
      expect(quoteText(null, unseen), 'old words');
      expect(Quote(messageId: 'gone').unseen(null), isFalse);
      expect(
        quoteText(m('q', 1, text: 'b' * 300), Quote(messageId: 'q'))!.length,
        201,
      );
      expect(quoteText(q, Quote(messageId: 'q', start: 6, end: 99)), isNull);
      expect(
        quoteText(m('q', 1, deleted: true), Quote(messageId: 'q')),
        isNull,
      );
      expect(quoteText(null, Quote(messageId: 'q')), isNull);
    });

    test('a moved thread names its new channel', () {
      final pointer = Message.fromJson({
        'id': 'x',
        'conversation_id': 'c',
        'seq': 30,
        'author_id': 'me',
        'parent_id': 'p',
        'system': {'kind': 'thread_moved', 'conversation_id': 'd'},
        'created_at': '2026-11-13T09:00:00.000Z',
      });
      expect(movedTo([pointer]), 'd');
      expect(movedTo([m('r', 2, parent: 'p')]), isNull);
    });
  });
}
