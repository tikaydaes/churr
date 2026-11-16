import 'package:churr/core/models/account.dart';
import 'package:churr/ui/screens/account/blocked_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../account_support.dart';

// The blocked accounts, and unblocking one.

void main() {
  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets('at $label size: the list, and Unblock', (tester) async {
      await sized(tester, size);
      final (app, fakes) = await makeAccountApp(tester);
      final s = app.servers[0];
      await tester.runAsync(
        () => s.db.upsertAccounts([
          Account.fromJson({
            'id': 'sam',
            'username': 'sam',
            'display_name': 'Sam Okafor',
            'role': 'member',
          }),
        ]),
      );
      fakes[0].on('GET /api/v1/me/blocks', {
        'account_ids': ['sam', 'gone'],
      });
      // An account the person can no longer see.
      fakes[0].on('GET /api/v1/accounts/gone', {}, status: 404);
      await tester.pumpWidget(
        harness(app, Scaffold(body: BlockedScreen(server: s))),
      );
      await settleAll(tester);
      expect(find.text('Sam Okafor'), findsOneWidget);
      expect(find.text('An account you can no longer see'), findsOneWidget);
      await tester.tap(find.byKey(const Key('unblock-sam')));
      await settleAll(tester);
      expect(fakes[0].calls, contains('DELETE /api/v1/me/blocks/sam'));
      expect(tester.takeException(), isNull);
    });
  }
}
