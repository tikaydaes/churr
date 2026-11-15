import 'dart:convert';

import 'package:churr/ui/screens/account/conversation_settings.dart';
import 'package:churr/ui/widgets/controls.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../account_support.dart';

/// "Ignore @channel, @here and @everyone" beside the notify mode in a
/// conversation's settings (ruling R64).
void main() {
  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets('Conversation settings: ignore broad mentions ($label)', (
      tester,
    ) async {
      await sized(tester, size);
      final (app, fakes) = await makeAccountApp(tester);
      fakes[0].on('PATCH /api/v1/conversations/general/membership', {
        'membership': {
          'level': 'member',
          'read_seq': 20,
          'mode': 'mentions',
          'unread_count': 2,
          'ignore_broad_mentions': true,
        },
      });
      await tester.pumpWidget(
        harness(
          app,
          ConversationSettings(
            server: app.servers[0],
            conversationId: 'general',
          ),
        ),
      );
      await settleAll(tester);
      final tile = find.byKey(const Key('ignore-broad-switch'));
      // Beside the mode, on the same screen.
      expect(find.byKey(const Key('mode-mentions')), findsOneWidget);
      await tester.ensureVisible(tile);
      await tester.pump(const Duration(milliseconds: 300));
      expect(find.text('Ignore @channel, @here and @everyone'), findsOneWidget);
      expect(find.textContaining('A mention of you by name'), findsOneWidget);
      expect(tester.widget<ChurrSwitchTile>(tile).value, isFalse);

      await tester.tap(tile);
      await settleAll(tester);
      expect(tester.widget<ChurrSwitchTile>(tile).value, isTrue);
      final sent = fakes[0].requests.lastWhere(
        (r) =>
            r.method == 'PATCH' &&
            r.url.path == '/api/v1/conversations/general/membership',
      );
      expect(jsonDecode(sent.body), {'ignore_broad_mentions': true});
      final row = await tester.runAsync(
        () => app.servers[0].db.conversation('general'),
      );
      expect(row!.conversation.membership!.ignoreBroadMentions, isTrue);
      expect(tester.takeException(), isNull);
    });
  }
}
