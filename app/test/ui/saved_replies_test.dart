import 'dart:convert';

import 'package:churr/ui/screens/account/saved_replies_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../account_support.dart';

// Saved replies: add, edit and delete (docs/CLIENTS.md 9.16).

void main() {
  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets('at $label size: add, edit and delete a saved reply', (
      tester,
    ) async {
      await sized(tester, size);
      final (app, fakes) = await makeAccountApp(tester);
      fakes[0].on('GET /api/v1/saved-replies', {
        'saved_replies': [
          {
            'id': 'r1',
            'community': false,
            'name': 'oncall',
            'text': 'Page Ada',
          },
          // A community command is the admins'; it is not listed here.
          {'id': 'c1', 'community': true, 'name': 'faq', 'text': 'See wiki'},
        ],
      });
      await tester.pumpWidget(
        harness(
          app,
          Scaffold(body: SavedRepliesScreen(server: app.servers[0])),
        ),
      );
      await settleAll(tester);
      expect(find.text('/oncall'), findsOneWidget);
      expect(find.text('/faq'), findsNothing);

      await tester.tap(find.byKey(const Key('new-saved-reply')));
      await settleAll(tester);
      await tester.enterText(find.byKey(const Key('saved-name')), '/lunch');
      await tester.enterText(find.byKey(const Key('saved-text')), 'Back at 2');
      await tester.tap(find.byKey(const Key('saved-save')));
      await settleAll(tester);
      final made = fakes[0].requests.lastWhere(
        (r) => r.method == 'POST' && r.url.path == '/api/v1/saved-replies',
      );
      expect(jsonDecode(made.body), {'name': 'lunch', 'text': 'Back at 2'});

      await tester.tap(find.byKey(const Key('edit-saved-r1')));
      await settleAll(tester);
      await tester.enterText(find.byKey(const Key('saved-text')), 'Page Sam');
      await tester.tap(find.byKey(const Key('saved-save')));
      await settleAll(tester);
      final changed = fakes[0].requests.lastWhere(
        (r) => r.method == 'PATCH' && r.url.path == '/api/v1/saved-replies/r1',
      );
      expect(jsonDecode(changed.body), {'name': 'oncall', 'text': 'Page Sam'});

      await tester.tap(find.byKey(const Key('delete-saved-r1')));
      await settleAll(tester);
      expect(fakes[0].calls, contains('DELETE /api/v1/saved-replies/r1'));
      expect(tester.takeException(), isNull);
    });
  }
}
