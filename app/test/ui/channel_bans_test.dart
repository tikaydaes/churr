import 'package:churr/l10n/app_localizations_en.dart';
import 'package:churr/state/server_state.dart';
import 'package:churr/ui/screens/channels/ban_notice.dart';
import 'package:churr/ui/screens/channels/channel_settings_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import '../forms_support.dart';

/// Channel bans in the app (ruling R72): the managers' list in the channel's
/// settings with who banned and when, lifting one, banning from a member's
/// menu, and what the one banned is told.
void main() {
  quietDrift();

  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets(
      'the bans list, lifting, and banning from the member menu ($label)',
      (tester) async {
        final requests = <http.Request>[];
        var banned = true;
        late final ServerState s;
        await tester.runAsync(() async {
          s = await fakeServerState(
            FakeServer((r) {
              requests.add(r);
              final p = r.url.path;
              if (p.endsWith('/members')) {
                return (
                  200,
                  {
                    'members': [
                      {
                        'account_id': 'me',
                        'level': 'manager',
                        'direct_level': 'manager',
                      },
                      {
                        'account_id': 'lee',
                        'level': 'member',
                        'direct_level': 'member',
                      },
                    ],
                  },
                );
              }
              if (p.endsWith('/groups')) return (200, {'groups': <Object>[]});
              if (p.endsWith('/pins')) return (200, {'pins': <Object>[]});
              if (p.endsWith('/bans/sam') && r.method == 'DELETE') {
                banned = false;
                return (204, null);
              }
              if (p.endsWith('/bans') && r.method == 'POST') {
                return (
                  201,
                  {
                    'ban': {'id': 'b2', 'account_id': 'lee'},
                  },
                );
              }
              if (p.endsWith('/bans')) {
                return (
                  200,
                  {
                    'bans': [
                      if (banned)
                        {
                          'id': 'b1',
                          'conversation_id': 'general',
                          'account_id': 'sam',
                          'reason': 'spam',
                          'created_by_id': 'me',
                          'created_at': '2026-11-14T09:30:00.000Z',
                        },
                    ],
                  },
                );
              }
              return null;
            }),
            conversations: [channel('general', 'General', level: 'manager')],
          );
        });
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
        await tester.scrollUntilVisible(
          find.byKey(const Key('ban-sam')),
          200,
          scrollable: find.byType(Scrollable).last,
        );
        expect(find.text('BANNED'), findsOneWidget);
        expect(find.textContaining('Banned by Kai'), findsOneWidget);
        expect(find.textContaining('“spam”'), findsOneWidget);

        await tester.tap(find.byKey(const Key('lift-ban-sam')));
        await settleAsync(tester);
        expect(
          requests.where(
            (r) => r.method == 'DELETE' && r.url.path.endsWith('/bans/sam'),
          ),
          hasLength(1),
        );
        expect(find.byKey(const Key('ban-sam')), findsNothing);

        await tester.scrollUntilVisible(
          find.byKey(const Key('member-menu-lee')),
          -200,
          scrollable: find.byType(Scrollable).last,
        );
        await tester.tap(find.byKey(const Key('member-menu-lee')));
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const Key('member-ban-lee')));
        await tester.pumpAndSettle();
        expect(find.text('Ban Lee from this channel'), findsOneWidget);
        await tester.enterText(find.byKey(const Key('ban-reason')), 'flooding');
        await tester.tap(find.byKey(const Key('confirm-ban')));
        await settleAsync(tester);
        final post = requests.singleWhere(
          (r) => r.method == 'POST' && r.url.path.endsWith('/bans'),
        );
        expect(body(post), {'account_id': 'lee', 'reason': 'flooding'});
        expect(tester.takeException(), isNull);
      },
    );
  }

  test('the one banned drops the channel at once; others keep it', () async {
    final s = await fakeServerState(
      FakeServer((_) => null),
      conversations: [
        channel('general', 'General'),
        channel('design', 'Design'),
      ],
    );
    Map<String, dynamic> ban(String conv, String who) => {
      'type': 'channel_ban.created',
      'conversation_name': conv,
      'ban': {'id': 'b-$conv', 'conversation_id': conv, 'account_id': who},
    };
    await s.sync.applyEvent(ban('design', 'lee'));
    expect(await s.db.conversation('design'), isNotNull);
    await s.sync.applyEvent(ban('general', 'me'));
    expect(await s.db.conversation('general'), isNull);
  });

  test('what the one banned is told', () {
    final t = L10nEn();
    expect(
      banNotice(t, {
        'conversation_name': 'ops',
        'ban': {'reason': ''},
      }),
      'You were banned from #ops.',
    );
    expect(
      banNotice(t, {
        'conversation_name': '',
        'ban': {'reason': 'spam'},
      }),
      'You were banned from a chat. The reason given: “spam”',
    );
    expect(
      banNotice(t, {
        'conversation_name': 'ops',
        'ban': {'reason': '', 'expires_at': '2026-11-20T10:00:00.000Z'},
      }),
      startsWith('You were banned from #ops until '),
    );
  });
}
