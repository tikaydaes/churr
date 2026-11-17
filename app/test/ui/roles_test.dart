import 'package:churr/core/api/channels.dart';
import 'package:churr/core/models/account.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/state/channel_actions.dart';
import 'package:churr/state/message_actions.dart';
import 'package:churr/state/server_state.dart';
import 'package:churr/ui/message/message_tile.dart';
import 'package:churr/ui/screens/channels/channel_settings_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import '../forms_support.dart';

/// Moderators, managers and owners in the app (rulings R97, R99;
/// docs/FEATURES.md 21.12): the level a channel gives is all that counts,
/// a moderator keeps the peace without setting levels, the target rule in
/// the member menu, guests and moderator levels, and the owner override
/// notice.
void main() {
  quietDrift();

  Account acc(String id, String name, String role) => Account.fromJson({
    'id': id,
    'username': id,
    'display_name': name,
    'role': role,
  });

  Member member(String id, String level) =>
      Member({'account_id': id, 'level': level, 'direct_level': level});

  group('the owner override notice', () {
    final accounts = {
      'ada': acc('ada', 'Ada', 'member'),
      'olga': acc('olga', 'Olga', 'owner'),
    };
    Message notice(Map<String, dynamic> system) => Message.fromJson({
      'id': 'm1',
      'conversation_id': 'c1',
      'author_id': 'ada',
      'system': system,
      'via': {'kind': 'system'},
      'created_at': '2026-11-17T00:00:00Z',
    });

    test('names the person added and the owner, and wants attention', () {
      final m = notice({'kind': 'owner_override', 'by_id': 'olga'});
      expect(
        systemText(m, accounts),
        'Ada was added by override, by Olga',
      );
      expect(isAttentionNotice(m), isTrue);
    });

    test('a manager dismissed from the console names who did it, and wants attention', () {
      final m = notice({'kind': 'manager_dismissed', 'by_id': 'olga'});
      expect(systemText(m, accounts), 'Ada is no longer a manager, by Olga');
      expect(isAttentionNotice(m), isTrue);
    });

    test('an ordinary add stays the quiet notice it was', () {
      final m = notice({'kind': 'member_joined', 'by_id': 'olga'});
      expect(systemText(m, accounts), 'Ada was added by Olga');
      expect(isAttentionNotice(m), isFalse);
    });
  });

  test('the target rule: nobody acts on an owner or on themselves, and a '
      'moderator not on a moderator or a manager', () {
    bool may(bool manager, Member m, [String role = 'member']) => mayActOn(
      manager: manager,
      myId: 'me',
      target: m,
      account: acc(m.accountId, 'X', role),
    );
    expect(may(false, member('lee', 'member')), isTrue);
    expect(may(false, member('lee', 'reader')), isTrue);
    expect(may(false, member('lee', 'moderator')), isFalse);
    expect(may(false, member('lee', 'manager')), isFalse);
    expect(may(true, member('lee', 'moderator')), isTrue);
    expect(may(true, member('lee', 'manager')), isTrue);
    expect(may(true, member('olga', 'member'), 'owner'), isFalse);
    expect(may(true, member('me', 'member')), isFalse);
    // An admin has no shield in a channel.
    expect(may(false, member('ann', 'member'), 'admin'), isTrue);
  });

  test('the role on the instance gives nothing in a channel (R99)', () async {
    final s = await fakeServerState(
      FakeServer((_) => null),
      role: 'owner',
      conversations: [
        channel('general', 'General'),
        channel('mods', 'Mods', level: 'moderator', private: true),
        channel('mine', 'Mine', level: 'manager'),
      ],
    );
    final ch = ChannelActions(s);
    final msg = MessageActions(s);
    final general = s.conversation('general')!;
    final mods = s.conversation('mods')!;
    final mine = s.conversation('mine')!;
    expect(ch.actsAsManager(general), isFalse);
    expect(ch.keepsPeace(general), isFalse);
    expect(msg.isManager(general), isFalse);
    expect(ch.canSetParent(general), isFalse);
    expect(ch.actsAsManager(mods), isFalse);
    expect(ch.keepsPeace(mods), isTrue);
    // A moderator deletes others' messages and is exempt from slow mode.
    expect(msg.keepsPeace(mods), isTrue);
    expect(ch.actsAsManager(mine), isTrue);
    expect(ch.keepsPeace(mine), isTrue);
  });

  Future<ServerState> withMembers(
    WidgetTester tester,
    String myLevel,
    List<http.Request> requests,
  ) async {
    late final ServerState s;
    await tester.runAsync(() async {
      s = await fakeServerState(
        FakeServer((r) {
          requests.add(r);
          final p = r.url.path;
          if (p.endsWith('/members') && r.method == 'GET') {
            return (
              200,
              {
                'members': [
                  for (final (id, level) in [
                    ('me', myLevel),
                    ('lee', 'member'),
                    ('sam', 'manager'),
                    ('mo', 'moderator'),
                    ('olga', 'member'),
                    ('gus', 'member'),
                  ])
                    {'account_id': id, 'level': level, 'direct_level': level},
                ],
              },
            );
          }
          if (p.endsWith('/groups')) return (200, {'groups': <Object>[]});
          if (p.endsWith('/pins')) return (200, {'pins': <Object>[]});
          if (p.endsWith('/bans')) return (200, {'bans': <Object>[]});
          if (r.method == 'PATCH' || r.method == 'DELETE') return (204, null);
          return null;
        }),
        conversations: [
          channel('general', 'General', level: myLevel, private: true),
        ],
      );
      await s.db.upsertAccounts([
        acc('mo', 'Mo', 'member'),
        acc('olga', 'Olga', 'owner'),
        acc('gus', 'Gus', 'guest'),
      ]);
      await Future<void>.delayed(const Duration(milliseconds: 30));
    });
    return s;
  }

  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets('a moderator removes and bans members, sets no levels, and '
        'adds at the default level ($label)', (tester) async {
      final requests = <http.Request>[];
      final s = await withMembers(tester, 'moderator', requests);
      await pumpScreen(
        tester,
        appWith([s]),
        ChannelSettingsScreen(
          server: s,
          conversationId: 'general',
          initialTab: 1,
        ),
        size: size,
      );
      // No menu on a manager, another moderator or an owner.
      expect(find.byKey(const Key('member-menu-sam')), findsNothing);
      expect(find.byKey(const Key('member-menu-mo')), findsNothing);
      expect(find.byKey(const Key('member-menu-olga')), findsNothing);
      expect(find.text('Moderator'), findsWidgets);
      await tester.tap(find.byKey(const Key('member-menu-lee')));
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('member-remove-lee')), findsOneWidget);
      expect(find.byKey(const Key('member-ban-lee')), findsOneWidget);
      expect(find.text('Make moderator'), findsNothing);
      expect(find.text('Make manager'), findsNothing);
      await tester.tap(find.byKey(const Key('member-remove-lee')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('confirm-remove')));
      await settleAsync(tester);
      expect(
        requests.where(
          (r) => r.method == 'DELETE' && r.url.path.endsWith('/members/lee'),
        ),
        hasLength(1),
      );
      // Adding people: the channel's default level, with no choice.
      await tester.tap(find.byKey(const Key('members-add')));
      await settleAsync(tester);
      expect(find.text('Add as'), findsNothing);
      expect(tester.takeException(), isNull);
    });

    testWidgets('a manager appoints moderators, but never a guest, who is '
        'shown as a Guest ($label)', (tester) async {
      final requests = <http.Request>[];
      final s = await withMembers(tester, 'manager', requests);
      await pumpScreen(
        tester,
        appWith([s]),
        ChannelSettingsScreen(
          server: s,
          conversationId: 'general',
          initialTab: 1,
        ),
        size: size,
      );
      // A manager acts on moderators and managers; still never an owner.
      expect(find.byKey(const Key('member-menu-mo')), findsOneWidget);
      expect(find.byKey(const Key('member-menu-sam')), findsOneWidget);
      expect(find.byKey(const Key('member-menu-olga')), findsOneWidget);
      expect(find.text('Guest'), findsOneWidget);

      await tester.tap(find.byKey(const Key('member-menu-olga')));
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('member-remove-olga')), findsNothing);
      expect(find.byKey(const Key('member-ban-olga')), findsNothing);
      await tester.tapAt(Offset.zero);
      await tester.pumpAndSettle();

      final gus = find.byKey(const Key('member-menu-gus'));
      await tester.scrollUntilVisible(
        gus,
        100,
        scrollable: find.byType(Scrollable).last,
      );
      await tester.tap(gus);
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('member-level-reader-gus')), findsOneWidget);
      expect(find.byKey(const Key('member-level-moderator-gus')), findsNothing);
      expect(find.byKey(const Key('member-level-manager-gus')), findsNothing);
      await tester.tapAt(Offset.zero);
      await tester.pumpAndSettle();

      final lee = find.byKey(const Key('member-menu-lee'));
      await tester.scrollUntilVisible(
        lee,
        -100,
        scrollable: find.byType(Scrollable).last,
      );
      await tester.tap(lee);
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('member-level-moderator-lee')));
      await settleAsync(tester);
      final patch = requests.singleWhere(
        (r) => r.method == 'PATCH' && r.url.path.endsWith('/members/lee'),
      );
      expect(body(patch), {'level': 'moderator'});
      expect(tester.takeException(), isNull);
    });
  }
}
