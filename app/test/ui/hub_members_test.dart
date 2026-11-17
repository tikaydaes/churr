import 'package:churr/core/api/hubs.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:churr/core/models/server.dart';
import 'package:churr/state/server_state.dart';
import 'package:churr/ui/screens/channels/channel_settings_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import '../forms_support.dart';

/// The Members tab of a private channel on a hub: nobody adds people the
/// ordinary way; its moderators and managers add them through the hub,
/// remove them on every instance, and see who is a Manager or Moderator.
void main() {
  quietDrift();

  const streamId = '0193d100-0000-7000-8000-000000000001';

  Map<String, dynamic> acct(String id, String name) => {
    'account_id': id,
    'server': 's0.example.org',
    'username': name,
  };

  HubChannel record({required bool meModerator}) => HubChannel({
    'stream_id': streamId,
    'hub': 'hub.example.net',
    'version': 3,
    'name': 'Design guild',
    'description': '',
    'listing': 'unlisted',
    'servers': ['s0.example.org', 'b.example.com'],
    'managers': [acct('sam', 'sam')],
    'moderators': [if (meModerator) acct('me', 'kai')],
    'invited': <String>[],
    'requests': <String>[],
    'key_maker': 's0.example.org',
    'closed': false,
    'private': true,
    'updated_at': '2026-11-14T09:00:00.000Z',
  });

  Future<(ServerState, List<http.Request>)> start(
    WidgetTester tester, {
    required bool meModerator,
    String level = 'manager',
  }) async {
    final requests = <http.Request>[];
    late final ServerState s;
    await tester.runAsync(() async {
      final base = channel('general', 'General', level: level, private: true);
      s = await fakeServerState(
        FakeServer((r) {
          requests.add(r);
          final p = r.url.path;
          if (p == '/api/v1/streams/$streamId/channel/members') {
            return (204, null);
          }
          if (p.endsWith('/members')) {
            return (
              200,
              {
                'members': [
                  {'account_id': 'me', 'level': level, 'direct_level': level},
                  {
                    'account_id': 'sam',
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
          if (p.endsWith('/bans')) return (200, {'bans': <Object>[]});
          return null;
        }),
        conversations: [
          Conversation.fromJson({
            ...base.raw,
            'shared': {
              'stream_id': streamId,
              'owner_server': 'hub.example.net',
              'servers': ['s0.example.org', 'b.example.com'],
            },
          }),
        ],
      );
      s.info = ServerInfo.fromJson({'name': 's0.example.org'});
      s.putHubChannel('general', record(meModerator: meModerator));
    });
    return (s, requests);
  }

  testWidgets('a moderator adds through the hub and removes everywhere', (
    tester,
  ) async {
    final (s, requests) = await start(tester, meModerator: true);
    await pumpScreen(
      tester,
      appWith([s]),
      ChannelSettingsScreen(
        server: s,
        conversationId: 'general',
        initialTab: 1,
      ),
    );
    expect(find.byKey(const Key('members-add')), findsNothing);
    expect(find.byKey(const Key('members-hub-add')), findsOneWidget);
    expect(find.byKey(const Key('manager-badge-sam')), findsOneWidget);
    expect(find.byKey(const Key('moderator-badge-me')), findsOneWidget);

    await tester.tap(find.byKey(const Key('member-menu-lee')));
    await tester.pumpAndSettle();
    // The manager level follows the hub's manager list.
    expect(find.text('Make manager'), findsNothing);
    await tester.tap(find.byKey(const Key('member-hub-remove-lee')));
    await tester.pumpAndSettle();
    expect(find.text('Remove Lee on every instance?'), findsOneWidget);
    await tester.tap(find.byKey(const Key('confirm-hub-remove')));
    await settleAsync(tester);
    final post = requests.singleWhere(
      (r) =>
          r.method == 'POST' &&
          r.url.path == '/api/v1/streams/$streamId/channel/members',
    );
    expect(body(post), {'action': 'remove', 'account_id': 'lee'});
    expect(tester.takeException(), isNull);
  });

  testWidgets('a local manager on neither list cannot add anyone', (
    tester,
  ) async {
    final (s, _) = await start(tester, meModerator: false);
    await pumpScreen(
      tester,
      appWith([s]),
      ChannelSettingsScreen(
        server: s,
        conversationId: 'general',
        initialTab: 1,
      ),
    );
    expect(find.byKey(const Key('members-add')), findsNothing);
    expect(find.byKey(const Key('members-hub-add')), findsNothing);
    await tester.tap(find.byKey(const Key('member-menu-lee')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('member-hub-remove-lee')), findsNothing);
    expect(tester.takeException(), isNull);
  });
}
