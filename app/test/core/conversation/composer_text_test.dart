import 'package:churr/core/conversation/composer_text.dart';
import 'package:churr/core/conversation/diff.dart';
import 'package:churr/core/conversation/emoji_data.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:churr/core/models/message.dart';
import 'package:flutter_test/flutter_test.dart';

const id1 = '0193a0c7-0000-7000-8000-000000000001';
const id2 = '0193a0c7-0000-7000-8000-000000000002';

Conversation ch(
  String id,
  String name, {
  String? parent,
  bool joined = true,
  String? at,
}) => Conversation.fromJson({
  'id': id,
  'kind': 'channel',
  'private': false,
  'name': name,
  'parent_id': ?parent,
  'member_count': 14,
  'created_at': '2026-03-05T00:00:00.000Z',
  'last_message_at': ?at,
  if (joined)
    'membership': {'level': 'member', 'read_seq': 0, 'mode': 'mentions'},
});

void main() {
  group('channel chips', () {
    test('a chip is one character and is sent as <#id>', () {
      final chips = ChannelChips();
      final chip = chips.chipFor(id1);
      expect(chip.length, 1);
      expect(chips.decode('see $chip now'), 'see <#$id1> now');
    });

    test('a draft or an edit shows its links as chips again, and sends them back as they were', () {
      final chips = ChannelChips();
      final wire = 'a <#$id1> b <#$id2> c';
      final editing = chips.encode(wire, (id) => id == id1);
      expect(editing.contains('<#$id1>'), isFalse);
      expect(
        editing.contains('<#$id2>'),
        isTrue,
        reason: 'a channel the person cannot see stays as written',
      );
      expect(chips.decode(editing), wire);
    });

    test('a chip character this composer did not make is dropped', () {
      expect(ChannelChips().decode('xy'), 'xy');
    });
  });

  group('triggers', () {
    test('a mention at the start of a word, with a server part', () {
      expect(
        findTrigger('hi @sa', 6).toString(),
        'Trigger(TriggerKind.mention, 3, 6, "sa")',
      );
      expect(findTrigger('hi @sam@friends.org', 19)!.query, 'sam@friends.org');
      expect(findTrigger('mail@x', 6), isNull);
    });

    test('a channel query may hold spaces and stops at a line break', () {
      final t = findTrigger('look at #design rev', 19)!;
      expect(t.kind, TriggerKind.channel);
      expect(t.query, 'design rev');
      expect(t.start, 8);
      expect(findTrigger('#design\nrev', 11), isNull);
      expect(findTrigger('a#b', 3), isNull);
      expect(findTrigger('# heading', 9), isNull);
    });

    test('emoji after two characters; commands only at the start', () {
      expect(findTrigger('nice :th', 8)!.kind, TriggerKind.emoji);
      expect(findTrigger('nice :t', 7), isNull);
      expect(findTrigger('/po', 3)!.kind, TriggerKind.command);
      expect(findTrigger('x /po', 5), isNull);
    });
  });

  group('matching channels', () {
    test('the start of a word, ignoring case and accents', () {
      expect(matchesWordStart('Design reviews', 'design rev'), isTrue);
      expect(matchesWordStart('Design reviews', 'rev'), isTrue);
      expect(matchesWordStart('Design reviews', 'esign'), isFalse);
      expect(matchesWordStart('Crème brûlée', 'creme bru'), isTrue);
      expect(matchesWordStart('デザイン', 'デザ'), isTrue);
    });

    test('those the person is in first, then by recent activity; the parent name narrows', () {
      final channels = [
        ch(id1, 'Design', joined: false, at: '2026-11-13T00:00:00.000Z'),
        ch(id2, 'Design', parent: 'p', at: '2026-11-01T00:00:00.000Z'),
        ch('p', 'Projects'),
      ];
      final found = matchChannels(
        channels,
        'des',
        parentOf: (id) => channels.firstWhere((c) => c.id == id),
      );
      expect(found.map((c) => c.id), [id2, id1]);
      expect(
        matchChannels(
          channels,
          'projects des',
          parentOf: (id) => channels.firstWhere((c) => c.id == id),
        ).map((c) => c.id),
        [id2],
      );
    });

    test('the detail line is in the fixed order', () {
      expect(
        channelDetailLine(
          ch(id1, 'Design', parent: 'p'),
          serverName: "Friends' server",
          parentName: 'design',
          managerNames: ['Sam', 'Lee', 'Kim'],
        ),
        "Friends' server · design › · 14 members, you're in it · managed by Sam, Lee · since March 2026",
      );
    });
  });

  test('copying a message writes channel names, never identifiers', () {
    final text =
        'see <#$id1> and <#$id2> and <#0193a0c7-0000-7000-8000-00000000000f>';
    final m = Message.fromJson({
      'id': 'm',
      'conversation_id': 'c',
      'author_id': 'a',
      'created_at': '2026-11-13T09:00:00.000Z',
      'text': text,
      'entities': [
        {'type': 'conversation', 'at': 4, 'len': 39, 'conversation_id': id1},
        {'type': 'conversation', 'at': 48, 'len': 39, 'conversation_id': id2},
      ],
    });
    final out = copyAsText(
      m,
      (id) => id == id1 ? ch(id1, 'Design reviews') : null,
    );
    expect(out, 'see #Design reviews and private channel and unknown channel');
  });

  group('formatting', () {
    test('wraps the selection, or a whole line', () {
      expect(applyFormat('make bold', 5, 9, Format.bold), (
        'make **bold**',
        7,
        11,
      ));
      expect(applyFormat('a\nb', 0, 3, Format.bullets).$1, '- a\n- b');
      expect(applyFormat('one', 3, 3, Format.checklist).$1, '- [ ] one');
      expect(applyFormat('x', 0, 1, Format.link).$1, '[x](https://)');
    });
  });

  test('emoji search puts familiar short names first', () {
    final data = EmojiData.parse(
      '{"groups":[{"name":"People","emoji":[["👍","thumbs_up"],["😂","face_with_tears_of_joy"]]}],"aliases":{"thumbsup":"👍","joy":"😂"}}',
    );
    expect(data.search('thumbs').map((e) => e.char), ['👍']);
    expect(data.search('joy').first.code, 'joy');
    expect(data.byCode('thumbs_up'), '👍');
    expect(data.codeOf('😂'), 'face_with_tears_of_joy');
  });

  test('a word diff marks what was removed and added', () {
    final d = wordDiff('the quick fox', 'the slow fox');
    expect(
      d.map((p) => p.toString()).join('|'),
      'same:"the "|removed:"quick "|added:"slow "|same:"fox"',
    );
  });
}
