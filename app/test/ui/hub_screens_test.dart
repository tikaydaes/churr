// The hub screens on fake data (the seeded dev server has no hub): a hub's
// directory for a server's owner, joining and asking to join, sharing a
// channel to a hub, a hub channel's details kept current from
// `stream.channel_changed`, the moderators' tools, and "Delete for
// everyone" against a local manager's "Delete here".
import 'dart:convert';

import 'package:churr/core/api/hubs.dart';
import 'package:churr/core/db/conversation_queries.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/core/models/server.dart';
import 'package:churr/state/app_state.dart';
import 'package:churr/state/message_actions.dart';
import 'package:churr/ui/screens/conversation/message_menu.dart';
import 'package:churr/ui/screens/hubs/hub_screens.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import '../account_support.dart';

const hubId = '0193c000-0000-7000-8000-0000000000h1';
const streamId = '0193d100-0000-7000-8000-000000000001';

Map<String, dynamic> hubPeer() => {
  'id': hubId,
  'kind': 'hub',
  'name': 'hub.example.net',
  'dials': true,
  'state': 'active',
  'created_by_id': 'me',
  'created_at': '2026-10-01T00:00:00.000Z',
  'connected_at': '2026-11-14T08:00:00.000Z',
  'servers': [
    {
      'id': 'k1',
      'name': 's0.example.org',
      'fingerprint': 'AAAA',
      'first_seen_at': '2026-10-01T00:00:00.000Z',
    },
    {
      'id': 'k2',
      'name': 'b.example.com',
      'fingerprint': 'BBBB',
      'first_seen_at': '2026-10-01T00:00:00.000Z',
    },
    {
      'id': 'k3',
      'name': 'c.example.com',
      'fingerprint': 'CCCC',
      'first_seen_at': '2026-10-01T00:00:00.000Z',
    },
  ],
  'told_clock': {
    'offset_ms': -12,
    'latency_ms': 38,
    'jitter_ms': 3,
    'uncertainty_ms': 19,
    'forward_ms': 18,
    'forward_jitter_ms': 2,
    'back_ms': 20,
    'back_jitter_ms': 2,
    'asymmetry_ms': 0,
    'samples': 16,
    'measured_at': '2026-11-14T09:00:00.000Z',
  },
};

Map<String, dynamic> mod(String id, String server, String name) => {
  'account_id': id,
  'server': server,
  'username': name,
};

Map<String, dynamic> hubChannel({
  int version = 3,
  String listing = 'request',
  List<String> servers = const ['s0.example.org', 'b.example.com'],
  List<Map<String, dynamic>>? moderators,
  List<String> requests = const [],
  List<String> invited = const [],
  String name = 'Design guild',
}) => {
  'stream_id': streamId,
  'hub': 'hub.example.net',
  'version': version,
  'name': name,
  'description': 'Shared design reviews',
  'listing': listing,
  'servers': servers,
  'moderators': moderators ?? [mod('me', 's0.example.org', 'kai')],
  'invited': invited,
  'requests': requests,
  'key_maker': 's0.example.org',
  'closed': false,
  'updated_at': '2026-11-14T09:00:00.000Z',
};

/// Makes "general" on server 0 a channel shared through the hub.
Future<void> shareGeneral(
  WidgetTester tester,
  AppState app, {
  String level = 'member',
}) async {
  final db = app.servers[0].db;
  await tester.runAsync(() async {
    final row = await db.conversation('general');
    final raw = <String, dynamic>{...row!.conversation.toJson()};
    raw['shared'] = {
      'stream_id': streamId,
      'owner_server': 'hub.example.net',
      'servers': ['s0.example.org', 'b.example.com'],
    };
    raw['membership'] = {...raw['membership'] as Map, 'level': level};
    await db.upsertConversation(Conversation.fromJson(raw));
    await Future<void>.delayed(const Duration(milliseconds: 50));
  });
  app.servers[0].info = ServerInfo.fromJson({'name': 's0.example.org'});
}

void main() {
  for (final size in [phone, desktop]) {
    final label = size == phone ? 'phone' : 'desktop';

    testWidgets('$label: the directory lists channels, and asks to join one', (
      tester,
    ) async {
      await sized(tester, size);
      final (app, fakes) = await makeAccountApp(tester);
      final f = fakes[0];
      f.on('GET /api/v1/admin/peers/$hubId/directory', {
        'channels': [
          {
            'stream_id': streamId,
            'name': 'Design guild',
            'description': 'Shared design reviews',
            'listing': 'request',
            'moderators': [mod('a1', 'b.example.com', 'robin')],
            'servers': 4,
            'state': 'none',
          },
          {
            'stream_id': '0193d100-0000-7000-8000-000000000002',
            'name': 'Open source',
            'description': '',
            'listing': 'open',
            'moderators': [mod('a2', 'c.example.com', 'lee')],
            'servers': 9,
            'state': 'member',
          },
          {
            'stream_id': '0193d100-0000-7000-8000-000000000003',
            'name': 'Security',
            'description': '',
            'listing': 'request',
            'moderators': [mod('a2', 'c.example.com', 'lee')],
            'servers': 2,
            'state': 'requested',
          },
        ],
      });
      f.on('GET /api/v1/admin/streams', {
        'streams': [
          {
            'id': '0193d100-0000-7000-8000-000000000004',
            'peer_id': hubId,
            'reference': 'hub',
            'kind': 'channel',
            'state': 'removed',
            'key_epoch': 1,
            'history_days': 0,
            'servers': [],
            'created_at': '2026-11-01T00:00:00.000Z',
            'offered_name': 'Finance',
          },
        ],
      });
      f.on('POST /api/v1/admin/peers/$hubId/directory/join', {
        'stream': {'id': streamId, 'peer_id': hubId, 'state': 'offered'},
        'conversation': {
          'id': 'new',
          'kind': 'channel',
          'name': 'design-guild',
        },
      });
      await tester.pumpWidget(
        harness(
          app,
          HubDirectoryScreen(server: app.servers[0], hub: Peer(hubPeer())),
        ),
      );
      await settleAll(tester);
      expect(find.text('Design guild'), findsOneWidget);
      expect(find.text('Shared design reviews'), findsOneWidget);
      expect(
        find.text('4 servers · moderators: robin@b.example.com'),
        findsOneWidget,
      );
      expect(find.text('BY REQUEST'), findsWidgets);
      expect(find.text('IN IT'), findsOneWidget);
      // Pending and declined requests.
      await tester.scrollUntilVisible(
        find.byKey(const Key('declined-0193d100-0000-7000-8000-000000000004')),
        200,
        scrollable: find.byType(Scrollable).first,
      );
      expect(
        find.byKey(const Key('pending-0193d100-0000-7000-8000-000000000003')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('declined-0193d100-0000-7000-8000-000000000004')),
        findsOneWidget,
      );
      expect(find.text('Finance'), findsOneWidget);

      await tester.scrollUntilVisible(
        find.byKey(const Key('join-$streamId')),
        -200,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.tap(find.byKey(const Key('join-$streamId')));
      await tester.pump();
      expect(find.text('Ask to join Design guild'), findsOneWidget);
      await tester.enterText(
        find.byKey(const Key('join-name')),
        'design-guild',
      );
      await tester.tap(find.text('Private here'));
      await tester.pump();
      await tester.tap(find.byKey(const Key('join-section')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Projects').last);
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('join-go')));
      await settleAll(tester);
      final post = f.requests.lastWhere(
        (r) => r.url.path.endsWith('/directory/join'),
      );
      expect(jsonDecode(post.body), {
        'stream_id': streamId,
        'name': 'design-guild',
        'private': true,
        'section_id': 's1',
      });
    });

    testWidgets(
      '$label: sharing a channel to a hub says what the hub owns and sees',
      (tester) async {
        await sized(tester, size);
        final (app, fakes) = await makeAccountApp(tester);
        final f = fakes[0];
        app.servers[0].info = ServerInfo.fromJson({'name': 's0.example.org'});
        f.on('GET /api/v1/admin/peers', {
          'peers': [hubPeer()],
        });
        f.on('POST /api/v1/admin/streams', {
          'stream': {'id': streamId, 'peer_id': hubId, 'state': 'active'},
        }, status: 201);
        final conv = app.servers[0].conversation('general')!;
        await tester.pumpWidget(
          harness(
            app,
            ShareToHubScreen(server: app.servers[0], conversation: conv),
          ),
        );
        await settleAll(tester);
        expect(
          find.text('hub.example.net will own this channel.'),
          findsOneWidget,
        );
        expect(find.textContaining('cannot read messages'), findsOneWidget);
        expect(
          find.textContaining('can see the channel\'s name'),
          findsOneWidget,
        );
        // Its own server is not offered for invitation.
        expect(find.text('s0.example.org'), findsNothing);
        await tester.tap(find.byKey(const Key('listing-open')));
        await tester.pump();
        await tester.scrollUntilVisible(
          find.byKey(const Key('invite-k3')),
          200,
          scrollable: find.byType(Scrollable).first,
        );
        await tester.tap(find.byKey(const Key('invite-k3')));
        await tester.pump();
        await tester.scrollUntilVisible(
          find.byKey(const Key('share-to-hub')),
          200,
          scrollable: find.byType(Scrollable).first,
        );
        await tester.tap(find.byKey(const Key('share-to-hub')));
        await settleAll(tester);
        final post = f.requests.lastWhere(
          (r) => r.url.path == '/api/v1/admin/streams',
        );
        expect(jsonDecode(post.body), {
          'conversation_id': 'general',
          'peer_id': hubId,
          'listing': 'open',
          'server_ids': ['k3'],
        });
        expect(find.textContaining('Shared.'), findsOneWidget);
      },
    );
  }

  testWidgets('no hub: sharing points to the admin console', (tester) async {
    await sized(tester, desktop);
    final (app, fakes) = await makeAccountApp(tester);
    fakes[0].on('GET /api/v1/admin/peers', {'peers': []});
    final conv = app.servers[0].conversation('general')!;
    await tester.pumpWidget(
      harness(
        app,
        ShareToHubScreen(server: app.servers[0], conversation: conv),
      ),
    );
    await settleAll(tester);
    expect(find.text('Link servers in the admin console'), findsOneWidget);
  });

  testWidgets('the hubs list shows the clock figures the hub measured', (
    tester,
  ) async {
    await sized(tester, desktop);
    final (app, fakes) = await makeAccountApp(tester);
    fakes[0].on('GET /api/v1/admin/peers', {
      'peers': [
        hubPeer(),
        {'id': 'p2', 'kind': 'server', 'state': 'active'},
      ],
    });
    await tester.pumpWidget(harness(app, HubsScreen(server: app.servers[0])));
    await settleAll(tester);
    expect(find.text('hub.example.net'), findsOneWidget);
    expect(find.textContaining('-12 ms off, within 19 ms'), findsOneWidget);
    expect(find.byKey(const Key('hub-p2')), findsNothing);
  });

  for (final size in [phone, desktop]) {
    testWidgets(
      '${size == phone ? 'phone' : 'desktop'}: a hub channel\'s details, kept current, with the moderators\' tools',
      (tester) async {
        await sized(tester, size);
        final (app, fakes) = await makeAccountApp(tester);
        final f = fakes[0];
        await shareGeneral(tester, app);
        var record = hubChannel(requests: ['c.example.com']);
        f.routes['GET /api/v1/streams/$streamId/channel'] = (_) =>
            http.Response(
              jsonEncode({'channel': record, 'moderator': true}),
              200,
            );
        f.routes['POST /api/v1/streams/$streamId/channel/actions'] = (r) {
          final body = jsonDecode(r.body) as Map;
          record = switch (body['action']) {
            'approve' => hubChannel(
              version: 4,
              servers: [
                's0.example.org',
                'b.example.com',
                body['server'] as String,
              ],
            ),
            'set_listing' => hubChannel(
              version: 5,
              listing: body['listing'] as String,
            ),
            _ => record,
          };
          return http.Response(jsonEncode({'channel': record}), 200);
        };
        final conv = app.servers[0].conversation('general')!;
        await tester.pumpWidget(
          harness(
            app,
            SingleChildScrollView(
              child: HubChannelSection(
                server: app.servers[0],
                conversation: conv,
              ),
            ),
          ),
        );
        await settleAll(tester);
        expect(find.text('Owned by the hub hub.example.net'), findsOneWidget);
        expect(find.text('2 servers'.toUpperCase()), findsOneWidget);
        expect(
          find.byKey(const Key('hub-server-b.example.com')),
          findsOneWidget,
        );
        expect(find.byKey(const Key('hub-mod-me')), findsOneWidget);
        expect(find.text('kai@s0.example.org'), findsOneWidget);
        // A moderator: requests and tools.
        expect(
          find.byKey(const Key('hub-request-c.example.com')),
          findsOneWidget,
        );
        await tester.ensureVisible(
          find.byKey(const Key('approve-c.example.com')),
        );
        await tester.tap(find.byKey(const Key('approve-c.example.com')));
        await settleAll(tester);
        expect(
          jsonDecode(
            f.requests.lastWhere((r) => r.url.path.endsWith('/actions')).body,
          ),
          {'action': 'approve', 'server': 'c.example.com'},
        );
        expect(
          find.byKey(const Key('hub-server-c.example.com')),
          findsOneWidget,
        );
        await tester.ensureVisible(find.byKey(const Key('hub-listing')));
        await tester.tap(find.text('Open').last);
        await settleAll(tester);
        expect(
          jsonDecode(
            f.requests.lastWhere((r) => r.url.path.endsWith('/actions')).body,
          ),
          {'action': 'set_listing', 'listing': 'open'},
        );

        // Another moderator renames it on the hub: the event as every member
        // sees it, and the details follow.
        final mods = [
          mod('me', 's0.example.org', 'kai'),
          mod('a1', 'b.example.com', 'robin'),
        ];
        // What a moderator's screen fetches again on the event.
        record = hubChannel(
          version: 9,
          name: 'Design guild (EU)',
          listing: 'open',
          moderators: mods,
        );
        await tester.runAsync(() async {
          await app.servers[0].sync.applyEvent({
            'type': 'stream.channel_changed',
            'at': '2026-11-14T10:00:00.000Z',
            'conversation_id': 'general',
            'stream_id': streamId,
            'channel': hubChannel(
              version: 9,
              name: 'Design guild (EU)',
              listing: 'open',
              moderators: mods,
            ),
          });
          await Future<void>.delayed(const Duration(milliseconds: 50));
        });
        await settleAll(tester);
        expect(find.text('Name on the hub: Design guild (EU)'), findsOneWidget);
        expect(find.text('robin@b.example.com'), findsOneWidget);
      },
    );
  }

  testWidgets('a member who is not a moderator sees the details and no tools', (
    tester,
  ) async {
    await sized(tester, desktop);
    final (app, fakes) = await makeAccountApp(tester);
    await shareGeneral(tester, app);
    fakes[0].on('GET /api/v1/streams/$streamId/channel', {
      'channel': hubChannel(moderators: [mod('a1', 'b.example.com', 'robin')]),
      'moderator': false,
    });
    final conv = app.servers[0].conversation('general')!;
    await tester.pumpWidget(
      harness(
        app,
        SingleChildScrollView(
          child: HubChannelSection(server: app.servers[0], conversation: conv),
        ),
      ),
    );
    await settleAll(tester);
    expect(find.text('robin@b.example.com'), findsOneWidget);
    expect(find.byKey(const Key('hub-invite')), findsNothing);
    expect(find.byKey(const Key('hub-close')), findsNothing);
  });

  testWidgets('a channel no hub owns shows nothing extra', (tester) async {
    await sized(tester, desktop);
    final (app, fakes) = await makeAccountApp(tester);
    await shareGeneral(tester, app);
    fakes[0].on('GET /api/v1/streams/$streamId/channel', {
      'error': {'code': 'not_found', 'message': 'No such channel.'},
    }, status: 404);
    final conv = app.servers[0].conversation('general')!;
    await tester.pumpWidget(
      harness(
        app,
        SingleChildScrollView(
          child: HubChannelSection(server: app.servers[0], conversation: conv),
        ),
      ),
    );
    await settleAll(tester);
    expect(find.byKey(const Key('hub-channel')), findsNothing);
  });

  group('deleting in a channel a hub owns', () {
    Future<List<MsgAction>> actionsFor(
      WidgetTester? tester, {
      required bool moderator,
      required String level,
      String author = 'sam',
    }) async {
      final (app, _) = await makeAccountApp(null);
      final s = app.servers[0];
      final raw = <String, dynamic>{...s.conversation('general')!.toJson()};
      raw['shared'] = {
        'stream_id': streamId,
        'owner_server': 'hub.example.net',
        'servers': [],
      };
      raw['membership'] = {...raw['membership'] as Map, 'level': level};
      final c = Conversation.fromJson(raw);
      s.info = ServerInfo.fromJson({'name': 's0.example.org'});
      s.putHubChannel(
        'general',
        HubChannel(
          hubChannel(
            moderators: [if (moderator) mod('me', 's0.example.org', 'kai')],
          ),
        ),
      );
      final m = Message.fromJson({
        'id': 'm1',
        'conversation_id': 'general',
        'seq': 3,
        'author_id': author,
        'created_at': '2026-11-14T09:00:00.000Z',
        'text': 'hello',
      });
      // The reader as a plain member, so only the hub's list decides.
      return availableActions(
        m: m,
        c: c,
        myId: 'me',
        actions: _MemberActions(s),
        limits: const ServerLimits(),
        threadState: null,
        bookmarked: false,
        inThread: false,
        now: DateTime.utc(2026, 11, 14, 9, 1),
        hubOwned: true,
        hubModerator: s.isHubModerator('general'),
      );
    }

    test('a moderator gets "Delete for everyone"', () async {
      final list = await actionsFor(null, moderator: true, level: 'member');
      expect(list, contains(MsgAction.deleteForEveryone));
      expect(list, isNot(contains(MsgAction.deleteHere)));
    });

    test('a local manager who is not a moderator gets "Delete here"', () async {
      final list = await actionsFor(null, moderator: false, level: 'manager');
      expect(list, contains(MsgAction.deleteHere));
      expect(list, isNot(contains(MsgAction.deleteForEveryone)));
    });

    test(
      'a member gets neither; their own message is deleted as anywhere',
      () async {
        final list = await actionsFor(null, moderator: false, level: 'member');
        expect(list.where((a) => a.name.startsWith('delete')), isEmpty);
        final own = await actionsFor(
          null,
          moderator: false,
          level: 'member',
          author: 'me',
        );
        expect(own, contains(MsgAction.delete));
      },
    );
  });
}

/// The reader's own role does not make them a manager here: whether they
/// manage the channel comes from its membership level only.
class _MemberActions extends MessageActions {
  _MemberActions(super.server);
  @override
  bool isManager(Conversation? c) => c?.membership?.level == 'manager';
}
