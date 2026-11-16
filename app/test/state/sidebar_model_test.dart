import 'package:churr/core/models/conversation.dart';
import 'package:churr/core/models/personal_layout.dart';
import 'package:churr/state/sidebar_model.dart';
import 'package:flutter_test/flutter_test.dart';

Conversation ch(
  String id, {
  String? name,
  String? section,
  String? parent,
  String mode = 'mentions',
  String? show,
  bool starred = false,
  int unread = 0,
  int badge = 0,
  bool member = true,
  bool private = false,
  String created = '2026-03-01T00:00:00.000Z',
  String? mutedUntil,
  List<String>? members,
  String? lastMessageAt,
}) => Conversation.fromJson({
  'id': id,
  'kind': 'channel',
  'private': private,
  if (members == null) 'name': name ?? id,
  'member_ids': ?members,
  'section_id': ?section,
  'parent_id': ?parent,
  'created_at': created,
  'last_message_at': ?lastMessageAt,
  'member_count': 3,
  if (member)
    'membership': {
      'level': 'member',
      'read_seq': 0,
      'mode': mode,
      'show': ?show,
      'muted_until': ?mutedUntil,
      'starred': starred,
      'unread_count': unread,
      'mention_count': 0,
      'badge_count': badge,
    },
});

final sections = [
  Section(id: 's1', name: 'Projects', position: 1),
  Section(id: 's2', name: 'Social', position: 2),
];

List<SidebarGroup> build(
  List<Conversation> cs, {
  PersonalLayout layout = PersonalLayout.empty,
  ShowSetting defaultShow = ShowSetting.always,
  bool showHidden = false,
  String? open,
  Set<String> collapsed = const {},
  Map<String, Conversation> outside = const {},
}) => buildServerGroups(
  SidebarInput(
    conversations: cs,
    sections: sections,
    outsideParents: outside,
    myId: 'me',
    defaultShow: defaultShow,
    showHidden: showHidden,
    openConversationId: open,
    collapsedParents: collapsed,
    layout: layout,
    now: DateTime.utc(2026, 11, 14),
  ),
);

List<String> names(List<SidebarGroup> groups) => [
  for (final g in groups) ...[
    '[${g.title}]',
    for (final r in g.rows)
      '${'  ' * r.indent}${r.parentName == null ? '' : '${r.parentName} › '}${r.name}',
  ],
];

void main() {
  group('which conversations have a row (R46, CLIENTS.md 9.2)', () {
    test('always, activity, notification and hidden', () {
      final cs = [
        ch('always', show: 'always'),
        ch('quiet-activity', show: 'activity'),
        ch('busy-activity', show: 'activity', unread: 2),
        ch('unread-but-no-badge', show: 'notification', unread: 2),
        ch('badged', show: 'notification', unread: 2, badge: 1),
        ch('hidden', show: 'hidden', unread: 5, badge: 5),
      ];
      expect(names(build(cs)), [
        '[Channels]',
        'always',
        'badged',
        'busy-activity',
      ]);
    });

    test('show not set follows the app default', () {
      final cs = [ch('a'), ch('b', unread: 1), ch('c', unread: 1, badge: 1)];
      expect(names(build(cs)), ['[Channels]', 'a', 'b', 'c']);
      expect(names(build(cs, defaultShow: ShowSetting.activity)), [
        '[Channels]',
        'b',
        'c',
      ]);
      expect(names(build(cs, defaultShow: ShowSetting.notification)), [
        '[Channels]',
        'c',
      ]);
    });

    test(
      'the conversation being viewed always has its row, in its usual place',
      () {
        final cs = [
          ch('a'),
          ch('hidden', show: 'hidden'),
          ch('read', show: 'activity'),
          ch('z'),
        ];
        expect(names(build(cs, open: 'hidden')), [
          '[Channels]',
          'a',
          'hidden',
          'z',
        ]);
        // Reading it made its condition false; the row stays while open,
        // and goes when the person moves on.
        expect(names(build(cs, open: 'read')), [
          '[Channels]',
          'a',
          'read',
          'z',
        ]);
        expect(names(build(cs, open: 'a')), ['[Channels]', 'a', 'z']);
      },
    );

    test(
      '"Show hidden" draws hidden rows with a mark, in their usual places',
      () {
        final groups = build([
          ch('a'),
          ch('h', show: 'hidden'),
        ], showHidden: true);
        expect(names(groups), ['[Channels]', 'a', 'h']);
        expect(groups.single.rows.map((r) => r.hiddenMark), [false, true]);
      },
    );

    test('a group with no rows is not drawn', () {
      final groups = build([
        ch('p', section: 's1', show: 'hidden'),
        ch('s', section: 's2'),
      ]);
      expect(names(groups), ['[Social]', 's']);
    });

    test('starring does not override show', () {
      expect(build([ch('s', starred: true, show: 'activity')]), isEmpty);
      expect(
        names(build([ch('s', starred: true, show: 'activity', unread: 1)])),
        ['[Channels]', 's'],
      );
    });
  });

  group('how a row is drawn', () {
    test('bold when unread; dimmed and never bold when the mode is nothing or it is muted', () {
      final rows = build([
        ch('a-any', mode: 'any', unread: 3, badge: 3),
        ch('b-mentions', unread: 3),
        ch('c-nothing', mode: 'nothing', unread: 3, badge: 1),
        ch(
          'd-muted',
          mode: 'any',
          unread: 3,
          mutedUntil: '2026-11-15T00:00:00.000Z',
        ),
        ch(
          'e-mute-over',
          mode: 'any',
          unread: 3,
          mutedUntil: '2026-11-13T00:00:00.000Z',
        ),
        ch('f-read'),
      ]).single.rows;
      expect(rows.map((r) => r.bold), [true, true, false, false, true, false]);
      expect(rows.map((r) => r.dimmed), [
        false,
        false,
        true,
        true,
        false,
        false,
      ]);
      // A mention by name still badges in a dimmed conversation.
      expect(rows.map((r) => r.badge), [3, 0, 1, 0, 0, 0]);
    });

    test('the icon says what kind of channel it is', () {
      final rows = build([
        ch('a'),
        ch('b', private: true),
        ch('chat', members: ['me', 'sam']),
      ]);
      expect(rows[0].rows.map((r) => r.icon), [
        RowIcon.public,
        RowIcon.private,
      ]);
      expect(rows[1].rows.single.icon, RowIcon.chat);
    });
  });

  group('grouping and order', () {
    test('the sections in order, Channels, Chats newest first; a star at the top of its own section', () {
      final groups = build([
        ch('zeta', section: 's2'),
        ch('Alpha', section: 's2'),
        ch('ops', section: 's1'),
        ch('loose'),
        ch('star', section: 's1', starred: true),
        ch(
          'old',
          members: ['me', 'a'],
          lastMessageAt: '2026-11-01T00:00:00.000Z',
        ),
        ch(
          'new',
          members: ['me', 'b', 'c'],
          lastMessageAt: '2026-11-10T00:00:00.000Z',
        ),
      ]);
      expect(groups.map((g) => g.title), [
        'Projects',
        'Social',
        'Channels',
        'Chats',
      ]);
      // Sorting ignores case.
      expect(groups[1].rows.map((r) => r.name), ['Alpha', 'zeta']);
      expect(groups[3].rows.map((r) => r.id), ['new', 'old']);
      // A starred conversation is listed once, first in its section.
      expect(groups[0].rows.map((r) => r.id), ['star', 'ops']);
    });

    test(
      'two rows with the same name in one place each get their creation date',
      () {
        final rows = build([
          ch('d1', name: 'Design reviews', created: '2026-03-02T00:00:00.000Z'),
          ch('d2', name: 'design reviews', created: '2026-07-09T00:00:00.000Z'),
          ch('other'),
        ]).single.rows;
        expect(rows.map((r) => r.detail), [
          'since March 2026',
          'since July 2026',
          null,
        ]);
      },
    );

    test('the detail line has its parts in the fixed order', () {
      expect(
        detailLine(
          ch('c'),
          serverName: "Friends' server",
          parentName: 'design',
          sectionName: 'Projects',
        ),
        "Friends' server · design › · Projects · 3 members, you're in it · since March 2026",
      );
      expect(
        detailLine(ch('c', member: false)),
        '3 members · since March 2026',
      );
    });
  });

  group('sub-channels (R49, CLIENTS.md 9.25)', () {
    final design = ch('design', name: 'Design', section: 's1');
    final icons = ch('icons', name: 'Icons', parent: 'design');
    final type = ch(
      'type',
      name: 'Typography',
      parent: 'design',
      section: 's2',
      badge: 2,
      unread: 2,
    );

    test(
      'drawn indented under the parent, in the parent\'s section, by name',
      () {
        final groups = build([type, icons, design]);
        expect(names(groups), [
          '[Projects]',
          'Design',
          '  Icons',
          '  Typography',
        ]);
        expect(groups.single.rows.first.hasChildren, isTrue);
      },
    );

    test('a folded parent keeps its unread sub-channels and says how many more it holds', () {
      final groups = build([design, icons, type], collapsed: {'design'});
      // Typography is unread, so it stays; Icons is read, so it folds away.
      expect(names(groups), ['[Projects]', 'Design', '  Typography']);
      final parent = groups.single.rows.first;
      expect(parent.collapsed, isTrue);
      expect(parent.moreCount, 1);
      expect(
        parent.badge,
        0,
        reason: "a sub-channel's activity is not its parent's",
      );
      // The sub-channel being viewed stays too.
      expect(names(build([design, icons, type], collapsed: {'design'}, open: 'icons')), [
        '[Projects]',
        'Design',
        '  Icons',
        '  Typography',
      ]);
    });

    test('a folded section keeps unread and selected rows; a sub-channel without its parent\'s row reads parent › name', () {
      final general = ch('general', name: 'general', section: 's1');
      final news = ch('news', name: 'news', section: 's1', unread: 1);
      final groups = build([design, icons, type, general, news], collapsed: {'s:s1'});
      final g = groups.single;
      expect(g.folded, isTrue);
      expect(g.total, 5);
      expect(names(groups), ['[Projects]', 'Design › Typography', 'news']);
      // Selecting a read row keeps it drawn while folded.
      expect(names(build([design, icons, type, general, news], collapsed: {'s:s1'}, open: 'general')), [
        '[Projects]',
        'Design › Typography',
        'general',
        'news',
      ]);
      // An unread parent keeps its row, and its unread sub-channel stays
      // indented under it.
      final busyDesign = ch('design', name: 'Design', section: 's1', unread: 3);
      expect(names(build([busyDesign, icons, type], collapsed: {'s:s1'})), ['[Projects]', 'Design', '  Typography']);
    });

    test('when the parent has no row, the sub-channel sits at its level as parent › name', () {
      final hiddenParent = ch(
        'design',
        name: 'Design',
        section: 's1',
        show: 'hidden',
      );
      expect(names(build([hiddenParent, icons])), [
        '[Projects]',
        'Design › Icons',
      ]);
      // No row ever stands in for a parent, and when the parent's own row
      // appears the sub-channel moves under it and drops the prefix.
      expect(names(build([hiddenParent, icons], showHidden: true)), [
        '[Projects]',
        'Design',
        '  Icons',
      ]);
    });

    test('a parent\'s row does not bring its sub-channels\' rows with it', () {
      final quiet = ch(
        'icons',
        name: 'Icons',
        parent: 'design',
        show: 'activity',
      );
      expect(names(build([design, quiet])), ['[Projects]', 'Design']);
    });

    test('a starred sub-channel stays under its parent', () {
      final starred = ch(
        'icons',
        name: 'Icons',
        parent: 'design',
        starred: true,
      );
      expect(names(build([design, starred])), [
        '[Projects]',
        'Design',
        '  Icons',
      ]);
    });

    test('in a sub-channel and not in its public parent: parent › name in the parent\'s section', () {
      final tickets = ch(
        'tickets',
        name: 'Tickets',
        section: 's1',
        member: false,
      );
      final t1 = ch('t1', name: 'tickets-1', parent: 'tickets');
      expect(names(build([t1], outside: {'tickets': tickets})), [
        '[Projects]',
        'Tickets › tickets-1',
      ]);
    });

    test('a parent the person cannot see is not named: a top-level channel in its own section', () {
      // The server gives no parent_id for it.
      expect(names(build([ch('t1', name: 'tickets-1', section: 's2')])), [
        '[Social]',
        'tickets-1',
      ]);
      // And a parent_id naming nothing known is treated the same way.
      expect(
        names(
          build([ch('t1', name: 'tickets-1', parent: 'gone', section: 's2')]),
        ),
        ['[Social]', 'tickets-1'],
      );
    });
  });

  group('a person\'s own sidebar (R83, FEATURES.md 21.4)', () {
    final cs = [
      ch('ops', name: 'ops', section: 's1', lastMessageAt: '2026-11-03T00:00:00.000Z'),
      ch('alpha', name: 'Alpha', section: 's1', lastMessageAt: '2026-11-01T00:00:00.000Z'),
      ch('mid', name: 'mid', section: 's1', lastMessageAt: '2026-11-09T00:00:00.000Z'),
      ch('loose', name: 'loose'),
      ch('kid', name: 'kid', parent: 'ops'),
      ch('dm', members: ['me', 'a'], lastMessageAt: '2026-11-02T00:00:00.000Z'),
    ];
    PersonalLayout l(Map<String, dynamic> j) => PersonalLayout.fromJson(j);

    test('own sections come first by default, and stay drawn when empty', () {
      final groups = build(cs, layout: l({
        'sections': [{'id': 1, 'name': 'Focus'}, {'id': 2, 'name': 'Later'}],
        'placements': [{'conversation_id': 'loose', 'section_id': 1}],
      }));
      expect([for (final g in groups) g.key], ['p:1', 'p:2', 's:s1', 'chats']);
      expect(groups[0].rows.map((r) => r.id), ['loose']);
      expect(groups[1].rows, isEmpty);
      expect(groups[1].own, isTrue);
    });

    test('a placed parent takes its sub-channel with it; the heading order is the person\'s', () {
      final groups = build(cs, layout: l({
        'sections': [{'id': 1, 'name': 'Focus'}],
        'placements': [{'conversation_id': 'ops', 'section_id': 1}],
        'headings': ['s:', 's:s1', 'p:1'],
      }));
      expect([for (final g in groups) g.key], ['s:', 's:s1', 'p:1', 'chats']);
      expect(names([groups[2]]), ['[Focus]', 'ops', '  kid']);
    });

    test('the three sorts', () {
      List<String> order(String sort, [List<String> custom = const []]) => [
        for (final r in build(cs, layout: l({
          'groups': [{'key': 's:s1', 'sort': sort, 'order': custom}],
        })).first.rows)
          if (r.indent == 0) r.id,
      ];
      expect(order('alphabetical'), ['alpha', 'mid', 'ops']);
      expect(order('activity'), ['mid', 'ops', 'alpha']);
      expect(order('custom', ['ops', 'alpha']), ['ops', 'alpha', 'mid']);
      // Chats sort by last activity until changed.
      expect(PersonalLayout.empty.sortOf('chats'), SortMode.activity);
    });

    test('stars sit at the top of their heading, in the order of stars', () {
      final starred = [
        for (final c in cs)
          c.id == 'mid' || c.id == 'ops' ? ch(c.id, name: c.name, section: 's1', starred: true) : c,
      ];
      final g = build(starred, layout: l({'stars': ['ops', 'mid']})).first;
      expect(g.tops, ['ops', 'mid', 'alpha']);
      expect(g.starredCount, 2);
      final again = build(starred, layout: l({'stars': ['mid', 'ops']})).first;
      expect(again.tops, ['mid', 'ops', 'alpha']);
    });

    test('what a move sends', () {
      final g = build(cs).first;
      expect(reorderOps(g, 'ops', 0, PersonalLayout.empty), [
        {'op': 'reorder', 'key': 's:s1', 'ids': ['ops', 'alpha', 'mid']},
      ]);
      final starred = build([
        ch('a', section: 's1', starred: true),
        ch('b', section: 's1', starred: true),
        ch('c', section: 's1'),
      ], layout: l({'stars': ['a', 'b']})).first;
      expect(reorderOps(starred, 'b', 0, l({'stars': ['a', 'b']})), [
        {'op': 'order_stars', 'ids': ['b', 'a']},
      ]);
      final custom = l({
        'sections': [{'id': 1, 'name': 'Focus'}],
        'placements': [{'conversation_id': 'alpha', 'section_id': 1}],
        'groups': [{'key': 'p:1', 'sort': 'custom', 'order': ['alpha', 'gone']}],
      });
      final focus = build(cs, layout: custom).first;
      expect(moveOps('mid', focus, 'p:1', 0, custom), [
        {'op': 'move', 'conversation_id': 'mid', 'key': 'p:1', 'ids': ['mid', 'alpha', 'gone']},
      ]);
      expect(moveOps('mid', null, 's:s1', null, PersonalLayout.empty), [
        {'op': 'move', 'conversation_id': 'mid', 'key': 's:s1'},
      ]);
    });
  });
}
