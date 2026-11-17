import 'package:churr/core/api/channels.dart';
import 'package:churr/core/models/account.dart';
import 'package:churr/core/models/chat_request.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:churr/state/server_state.dart';
import 'package:churr/state/sidebar_model.dart';
import 'package:churr/ui/layout/channel_offers.dart';
import 'package:churr/ui/screens/channels/browse_screen.dart';
import 'package:churr/ui/screens/channels/channel_settings_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import '../forms_support.dart';

/// First contact (ruling R103, docs/CLIENTS.md 9.28): channels offered
/// through a group or as a default channel sit in the left panel with
/// Accept and Decline; member lists end with the people invited; Browse
/// joins the private channels the person may join; and a group chat with
/// people still invited names them so.
void main() {
  quietDrift();

  Json offerJson(
    String id,
    String name, {
    String access = 'public',
    String source = 'group',
  }) => {
    'conversation_id': id,
    'source': source,
    'level': 'member',
    'created_at': '2026-11-16T09:00:00.000Z',
    'channel': {
      'id': id,
      'kind': 'channel',
      'name': name,
      'topic': 'About $name',
      'access': access,
      'private': access != 'public',
      'member_count': 4,
    },
  };

  Future<ServerState> serverWith(
    WidgetTester tester,
    (int, Object?)? Function(http.Request r) answer, {
    List<Conversation> conversations = const [],
    List<Json> offers = const [],
  }) async {
    late ServerState s;
    await tester.runAsync(() async {
      s = await fakeServerState(
        FakeServer(answer),
        conversations: conversations,
      );
      await s.db.putChannelOffers([
        for (final o in offers) ChannelOffer.fromJson(o),
      ]);
      await Future<void>.delayed(const Duration(milliseconds: 30));
    });
    return s;
  }

  test('an offer, an invited member and invited people are read', () {
    final o = ChannelOffer.fromJson(
      offerJson('d', 'garden', access: 'private', source: 'default'),
    );
    expect(o.conversationId, 'd');
    expect(o.viaDefault, isTrue);
    expect(o.level, 'member');
    expect(o.channel.name, 'garden');
    expect(o.previewable, isFalse);
    expect(ChannelOffer.fromJson(offerJson('p', 'pub')).previewable, isTrue);

    final m = Member({'account_id': 'lee', 'level': 'member', 'invited': true});
    expect(m.invited, isTrue);
    expect(Member({'account_id': 'lee'}).invited, isFalse);

    final chat = Conversation.fromJson({
      'id': 'gc',
      'kind': 'channel',
      'private': true,
      'member_ids': ['me', 'sam'],
      'invited_ids': ['lee'],
    });
    expect(chat.invitedIds, ['lee']);
    // Not a direct chat while someone is still invited.
    expect(chat.isDirect, isFalse);
    expect(
      conversationName(chat, {
        'sam': Account.fromJson({'id': 'sam', 'display_name': 'Sam'}),
        'lee': Account.fromJson({'id': 'lee', 'display_name': 'Lee'}),
      }, 'me'),
      'Sam, Lee (invited)',
    );
  });

  test('sync keeps the offers from channel_offer.added and .removed', () async {
    final s = await fakeServerState(FakeServer((_) => null));
    await s.sync.applyEvent({
      'type': 'channel_offer.added',
      'at': '2026-11-16T09:00:00.000Z',
      'offer': offerJson('a', 'art'),
    });
    await s.sync.applyEvent({
      'type': 'channel_offer.added',
      'at': '2026-11-16T09:00:00.000Z',
      'offer': offerJson('b', 'books'),
    });
    await s.sync.applyEvent({
      'type': 'channel_offer.added',
      'at': '2026-11-16T09:00:00.000Z',
      'offer': offerJson('a', 'art'),
    });
    expect(
      [for (final o in await s.db.channelOffers()) o.conversationId],
      ['b', 'a'],
    );
    await s.sync.applyEvent({
      'type': 'channel_offer.removed',
      'at': '2026-11-16T09:00:00.000Z',
      'conversation_id': 'a',
    });
    expect(
      [for (final o in await s.db.channelOffers()) o.conversationId],
      ['b'],
    );
  });

  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets('offers in the left panel: accept, decline, preview ($label)', (
      tester,
    ) async {
      final requests = <http.Request>[];
      final s = await serverWith(
        tester,
        (r) {
          requests.add(r);
          final p = r.url.path;
          if (p == '/api/v1/me/channel-offers/art/accept') {
            return (200, {'conversation': channel('art', 'art').toJson()});
          }
          if (p.endsWith('/decline')) return (204, null);
          if (p == '/api/v1/conversations/pub') {
            return (
              200,
              {'conversation': channel('pub', 'pub', member: false).toJson()},
            );
          }
          return null;
        },
        offers: [
          offerJson('art', 'art', access: 'private'),
          offerJson('books', 'books', access: 'private', source: 'default'),
          offerJson('pub', 'pub'),
        ],
      );
      final app = appWith([s]);
      await pumpScreen(tester, app, ChannelOffersBlock(server: s), size: size);
      expect(find.text('OFFERED TO YOU'), findsOneWidget);
      expect(find.text('art'), findsOneWidget);
      expect(find.text('About art'), findsOneWidget);
      expect(find.text('Through a group you are in'), findsNWidgets(2));
      expect(find.text('A default channel of this instance'), findsOneWidget);

      Finder inside(String id, String key) => find.descendant(
        of: find.byKey(Key('channel-offer-$id')),
        matching: find.byKey(Key(key)),
      );

      // A private one shows nothing before it is accepted.
      await tester.tap(find.text('books'));
      await settleAsync(tester);
      expect(app.open, isNull);

      // A public one opens to read without joining.
      await tester.tap(find.text('pub'));
      await settleAsync(tester);
      expect(app.open?.conversationId, 'pub');
      expect(s.conversation('pub')?.membership, isNull);

      await tester.tap(inside('books', 'channel-offer-decline'));
      await settleAsync(tester);
      expect(
        requests.map((r) => '${r.method} ${r.url.path}'),
        contains('POST /api/v1/me/channel-offers/books/decline'),
      );
      expect(find.byKey(const Key('channel-offer-books')), findsNothing);

      await tester.tap(inside('art', 'channel-offer-accept'));
      await settleAsync(tester);
      expect(
        requests.map((r) => '${r.method} ${r.url.path}'),
        contains('POST /api/v1/me/channel-offers/art/accept'),
      );
      expect(app.open?.conversationId, 'art');
      expect(s.conversation('art')?.membership, isNotNull);
      expect(find.byKey(const Key('channel-offer-art')), findsNothing);

      // Declining the one previewed drops it from this device too.
      await tester.tap(inside('pub', 'channel-offer-decline'));
      await settleAsync(tester);
      expect(s.conversation('pub'), isNull);
      expect(find.text('OFFERED TO YOU'), findsNothing);
      expect(tester.takeException(), isNull);
    });

    testWidgets('browse joins a private channel it may, and never knocks on '
        'one offered ($label)', (tester) async {
      final requests = <http.Request>[];
      Json open(String id, String name, String access) => {
        ...channel(
          id,
          name,
          member: false,
          private: access != 'public',
        ).toJson(),
        'access': access,
      };
      final s = await serverWith(tester, (r) {
        requests.add(r);
        if (r.url.path.endsWith('/join')) {
          return (200, {'conversation': channel('team', 'team').toJson()});
        }
        if (r.url.queryParameters['parent_id'] != null) {
          return (200, {'conversations': <Object>[]});
        }
        return (
          200,
          {
            'conversations': [
              open('team', 'team', 'private'),
              open('den', 'den', 'knock'),
              open('lab', 'lab', 'knock'),
            ],
          },
        );
      }, offers: [offerJson('lab', 'lab', access: 'knock')]);
      final app = appWith([s]);
      await pumpScreen(
        tester,
        app,
        BrowseScreen(app: app, server: s),
        size: size,
      );
      expect(find.byKey(const Key('join-team')), findsOneWidget);
      expect(find.byKey(const Key('knock-team')), findsNothing);
      expect(find.byKey(const Key('knock-den')), findsOneWidget);
      expect(find.byKey(const Key('join-lab')), findsOneWidget);
      await tester.tap(find.byKey(const Key('join-team')));
      await settleAsync(tester);
      expect(
        requests.map((r) => '${r.method} ${r.url.path}'),
        contains('POST /api/v1/conversations/team/join'),
      );
      expect(tester.takeException(), isNull);
    });

    testWidgets('the member list ends with the people invited ($label)', (
      tester,
    ) async {
      final requests = <http.Request>[];
      final s = await serverWith(tester, (r) {
        requests.add(r);
        final p = r.url.path;
        if (p.endsWith('/members') && r.method == 'GET') {
          return (
            200,
            {
              'members': [
                {'account_id': 'lee', 'level': 'manager', 'invited': true},
                {'account_id': 'me', 'level': 'moderator'},
                {'account_id': 'sam', 'level': 'member'},
              ],
            },
          );
        }
        if (p.endsWith('/groups')) return (200, {'groups': <Object>[]});
        if (p.endsWith('/pins')) return (200, {'pins': <Object>[]});
        if (p.endsWith('/bans')) return (200, {'bans': <Object>[]});
        if (r.method == 'DELETE') return (204, null);
        return null;
      }, conversations: [channel('general', 'General', level: 'moderator')]);
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
      expect(
        find.textContaining('invited, not yet joined · Manager'),
        findsOneWidget,
      );
      final lee = tester.getTopLeft(find.byKey(const Key('member-lee')));
      final sam = tester.getTopLeft(find.byKey(const Key('member-sam')));
      expect(lee.dy, greaterThan(sam.dy), reason: 'invited come last');
      await tester.tap(find.byKey(const Key('member-menu-lee')));
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('member-ban-lee')), findsNothing);
      await tester.tap(find.byKey(const Key('member-withdraw-lee')));
      await settleAsync(tester);
      expect(
        requests.where(
          (r) => r.method == 'DELETE' && r.url.path.endsWith('/members/lee'),
        ),
        hasLength(1),
      );
      expect(tester.takeException(), isNull);
    });
  }
}
