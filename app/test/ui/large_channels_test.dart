import 'package:churr/core/db/cache_db.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:churr/l10n/app_localizations.dart';
import 'package:churr/ui/scopes.dart';
import 'package:churr/ui/screens/channels/channel_settings_screen.dart';
import 'package:churr/ui/theme/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../conversation_support.dart';

/// Large channels and announcement channels on screen (ruling R66): the
/// notice in place of the message box, the member list that is the
/// managers', and the online count in place of who is online.

Future<CacheDb> _cache(
  Map<String, dynamic> more, {
  String level = 'member',
}) async {
  final db = await conversationCache();
  final c = channel(general, 'General', members: 2400, level: level);
  await db.upsertConversation(
    Conversation.fromJson({...c.raw, ...more}),
    syncedSeq: c.lastSeq,
  );
  return db;
}

void main() {
  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets('an announcement channel: a notice where the box was, and '
        'nothing said about typing ($label)', (tester) async {
      await setSize(tester, size);
      final fake = FakeServer();
      final db = (await tester.runAsync(
        () => _cache({'announcement': true, 'large': true}),
      ))!;
      final app = await conversationApp(tester, db, fake);
      await pumpConversation(tester, app);
      await settle(tester);
      expect(
        find.textContaining('only its managers and the people they allow post'),
        findsOneWidget,
      );
      expect(find.textContaining('reply in threads'), findsNothing);
      expect(find.byKey(const Key('composer')), findsNothing);
      expect(fake.requests.where((r) => r.path.endsWith('/typing')), isEmpty);
      expect(tester.takeException(), isNull);
    });

    testWidgets('an announcement channel that takes replies says so, and a '
        'manager keeps the box ($label)', (tester) async {
      await setSize(tester, size);
      final fake = FakeServer();
      final db = (await tester.runAsync(
        () => _cache({
          'announcement': true,
          'announcement_replies': true,
          'large': true,
        }),
      ))!;
      final app = await conversationApp(tester, db, fake);
      await pumpConversation(tester, app);
      await settle(tester);
      expect(find.textContaining('reply in threads'), findsOneWidget);

      // The same channel for one of its managers.
      await tester.runAsync(() async {
        final c = channel(general, 'General', members: 2400, level: 'manager');
        await db.upsertConversation(
          Conversation.fromJson({
            ...c.raw,
            'announcement': true,
            'large': true,
          }),
        );
      });
      await settle(tester);
      expect(
        find.textContaining('only its managers and the people they allow post'),
        findsNothing,
      );
      expect(tester.takeException(), isNull);
    });

    testWidgets('a large channel shows how many are online, from the '
        'server\'s count ($label)', (tester) async {
      await setSize(tester, size);
      final fake = FakeServer();
      final db = (await tester.runAsync(() => _cache({'large': true})))!;
      final app = await conversationApp(tester, db, fake);
      await pumpConversation(tester, app);
      await settle(tester);
      expect(find.byKey(const Key('header-online')), findsNothing);
      await tester.runAsync(
        () => serverOf(app).sync.applyEvent({
          'type': 'presence.counts',
          'at': '2026-11-15T09:00:00.000Z',
          'online_count': 310,
          'conversations': {general: 212},
        }),
      );
      await settle(tester);
      expect(serverOf(app).onlineCount, 310);
      expect(find.textContaining('212 online'), findsOneWidget);
      expect(tester.takeException(), isNull);
    });

    testWidgets('the members of an announcement channel are not listed for '
        'a member, nor asked for ($label)', (tester) async {
      await setSize(tester, size);
      final fake = FakeServer();
      final db = (await tester.runAsync(
        () => _cache({'announcement': true, 'large': true}),
      ))!;
      final app = await conversationApp(tester, db, fake);
      await tester.pumpWidget(
        AppScope(
          state: app,
          child: MaterialApp(
            localizationsDelegates: L10n.localizationsDelegates,
            supportedLocales: L10n.supportedLocales,
            theme: buildTheme(dark: false),
            home: Scaffold(
              body: ChannelSettingsScreen(
                server: serverOf(app),
                conversationId: general,
                initialTab: 1,
              ),
            ),
          ),
        ),
      );
      await settle(tester);
      expect(find.byKey(const Key('members-hidden')), findsOneWidget);
      expect(find.textContaining('2400 members'), findsWidgets);
      expect(fake.requests.where((r) => r.path.endsWith('/members')), isEmpty);
      expect(tester.takeException(), isNull);
    });
  }
}
