import 'package:churr/core/models/account.dart';
import 'package:churr/core/models/server.dart';
import 'package:churr/ui/screens/conversation/add_emoji.dart';
import 'package:churr/ui/screens/conversation/pickers.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../account_support.dart';

// A member adding a custom emoji where the server allows it
// (docs/CLIENTS.md 9.5, docs/PERMISSIONS.md note 7).

ServerInfo info({required bool membersMay}) => ServerInfo.fromJson({
  'name': 'x',
  'features': {'members_may_add_emoji': membersMay},
});

void main() {
  test('who may add an emoji', () async {
    final (app, _) = await makeAccountApp(null);
    final s = app.servers[0];
    for (final (role, membersMay, may) in [
      ('owner', false, true),
      ('admin', false, true),
      ('member', true, true),
      ('member', false, false),
      ('guest', true, false),
      ('bot', true, false),
    ]) {
      s.me = Me.fromJson(meJson(extra: {'role': role}));
      s.info = info(membersMay: membersMay);
      expect(mayAddEmoji(s), may, reason: '$role, setting $membersMay');
    }
  });

  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    group('at $label size', () {
      testWidgets('a member adds one from the picker, which picks it', (
        tester,
      ) async {
        await sized(tester, size);
        final (app, fakes) = await makeAccountApp(tester);
        final s = app.servers[0];
        s.me = Me.fromJson(meJson(extra: {'role': 'member'}));
        s.info = info(membersMay: true);
        final gecko = {
          'id': 'e1',
          'name': 'gecko',
          'url': '/api/v1/files/f1/gecko.png',
        };
        fakes[0].on('POST /api/v1/emoji', {'emoji': gecko}, status: 201);
        fakes[0].on('GET /api/v1/emoji', {
          'emoji': [gecko],
        });
        String? picked;
        await tester.pumpWidget(
          harness(
            app,
            Scaffold(
              body: Builder(
                builder: (context) => TextButton(
                  onPressed: () async => picked = await showDialog<String>(
                    context: context,
                    builder: (_) => Dialog(
                      child: SizedBox(
                        width: 380,
                        height: 460,
                        child: EmojiPicker(
                          server: s,
                          choose: () async => ('gecko.png', [1, 2, 3]),
                        ),
                      ),
                    ),
                  ),
                  child: const Text('open'),
                ),
              ),
            ),
          ),
        );
        await tester.tap(find.text('open'));
        await settleAll(tester);
        await tester.tap(find.byKey(const Key('emoji-picker-add')));
        await settleAll(tester);
        await tester.enterText(find.byKey(const Key('emoji-name')), 'gecko');
        await tester.tap(find.byKey(const Key('emoji-choose')));
        await settleAll(tester);

        expect(find.text('gecko.png'), findsOneWidget);
        await tester.tap(find.byKey(const Key('emoji-add')));
        await settleAll(tester);
        expect(fakes[0].calls, contains('POST /api/v1/emoji'));
        expect(picked, ':gecko:');
        expect(tester.takeException(), isNull);
      });

      testWidgets('not offered where the server does not allow it', (
        tester,
      ) async {
        await sized(tester, size);
        final (app, _) = await makeAccountApp(tester);
        final s = app.servers[0];
        s.me = Me.fromJson(meJson(extra: {'role': 'member'}));
        s.info = info(membersMay: false);
        await tester.pumpWidget(
          harness(app, Scaffold(body: EmojiPicker(server: s))),
        );
        await settleAll(tester);
        expect(find.byKey(const Key('emoji-picker-add')), findsNothing);
      });
    });
  }
}
