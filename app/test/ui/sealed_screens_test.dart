import 'package:churr/core/sealed/sealed.dart' show b64;
import 'package:churr/core/sealed_client/api.dart';
import 'package:churr/core/sealed_client/session.dart';
import 'package:churr/l10n/app_localizations.dart';
import 'package:churr/state/app_state.dart';
import 'package:churr/state/sealed_state.dart';
import 'package:churr/ui/screens/sealed/compare_code_screen.dart';
import 'package:churr/ui/screens/sealed/new_sealed_chat.dart';
import 'package:churr/ui/screens/sealed/sealed_chat_screen.dart';
import 'package:churr/ui/screens/sealed/sealed_devices_screen.dart';
import 'package:churr/ui/screens/sealed/sealed_unlock_screen.dart';
import 'package:churr/ui/scopes.dart';
import 'package:churr/ui/theme/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'dart:math';

import '../conversation_support.dart'
    show FakeServer, conversationApp, serverOf, settle, setSize;
import '../sealed_support.dart';

/// Every sealed-chat screen at phone and desktop widths, on in-memory
/// databases and a session with no network.
void main() {
  late AppState app;
  late TestSealedSession session;

  Future<void> boot(
    WidgetTester tester, {
    SealedStatus status = SealedStatus.unlocked,
  }) async {
    await tester.runAsync(() async {
      session = await TestSealedSession.make(status: status);
    });
    final db = await tester.runAsync(sealedCache);
    app = await conversationApp(tester, db!, FakeServer());
    SealedHub.instance.reset();
    SealedHub.instance.opener = (_) async => session;
    await tester.runAsync(() async {
      SealedHub.instance.attach(serverOf(app));
      await Future<void>.delayed(const Duration(milliseconds: 50));
    });
  }

  Future<void> pump(
    WidgetTester tester,
    Widget child, {
    bool dark = false,
  }) async {
    await tester.pumpWidget(
      AppScope(
        state: app,
        child: MaterialApp(
          localizationsDelegates: L10n.localizationsDelegates,
          supportedLocales: L10n.supportedLocales,
          theme: buildTheme(dark: dark),
          home: Scaffold(body: child),
        ),
      ),
    );
    await settle(tester);
  }

  tearDown(() => SealedHub.instance.reset());

  for (final (label, size) in [
    ('phone', const Size(390, 800)),
    ('desktop', const Size(1440, 900)),
  ]) {
    group(label, () {
      testWidgets(
        'the chat: decrypted messages, lock, menu, composer sends sealed',
        (tester) async {
          await setSize(tester, size);
          await boot(tester);
          await pump(
            tester,
            SealedChatScreen(server: serverOf(app), conversationId: sealedConv),
          );
          expect(find.text('End-to-end encrypted'), findsOneWidget);
          expect(find.textContaining('Hello from sam'), findsOneWidget);
          expect(find.text('Hello back'), findsOneWidget);
          // A checklist item can be ticked.
          expect(find.byType(Checkbox), findsOneWidget);
          await tester.enterText(find.byType(TextField).last, 'A sealed reply');
          await tester.tap(find.byKey(const Key('send')));
          await settle(tester);
          expect(session.sent, contains('A sealed reply'));
          await tester.tap(find.byKey(const Key('sealed-menu')));
          await settle(tester);
          expect(find.text('Compare codes'), findsOneWidget);
          expect(find.text('Sealed chats and devices'), findsOneWidget);
        },
      );

      testWidgets('a locked device lists the chat and offers to unlock', (
        tester,
      ) async {
        await setSize(tester, size);
        await boot(tester, status: SealedStatus.locked);
        await pump(
          tester,
          SealedChatScreen(server: serverOf(app), conversationId: sealedConv),
          dark: true,
        );
        expect(find.byKey(const Key('sealed-locked')), findsOneWidget);
        expect(find.byKey(const Key('sealed-unlock')), findsOneWidget);
      });

      testWidgets('unlock: the ways, R43 said plainly, the phrase field', (
        tester,
      ) async {
        await setSize(tester, size);
        await boot(tester, status: SealedStatus.locked);
        await pump(tester, SealedUnlockScreen(server: serverOf(app)));
        for (final w in ['passkey', 'phrase', 'device', 'startAgain']) {
          expect(find.byKey(Key('unlock-way-$w')), findsOneWidget);
        }
        expect(find.byKey(const Key('unlock-carry-on')), findsOneWidget);
        await tester.tap(find.byKey(const Key('unlock-way-passkey')));
        await settle(tester);
        expect(find.textContaining('page the server supplies'), findsOneWidget);
        expect(find.byKey(const Key('unlock-open-page')), findsOneWidget);
        await tester.tap(find.byKey(const Key('unlock-way-phrase')));
        await settle(tester);
        expect(find.byKey(const Key('unlock-phrase')), findsOneWidget);
      });

      testWidgets('the first sealed chat offers to make the phrase', (
        tester,
      ) async {
        await setSize(tester, size);
        await boot(tester, status: SealedStatus.noBackup);
        await pump(tester, SealedUnlockScreen(server: serverOf(app)));
        expect(find.byKey(const Key('sealed-make-backup')), findsOneWidget);
        expect(find.textContaining('page the server supplies'), findsOneWidget);
      });

      testWidgets('the phrase is shown once and two words are checked', (
        tester,
      ) async {
        await setSize(tester, size);
        await boot(tester);
        var done = false;
        const words =
            'abandon ability able about above absent absorb abstract absurd abuse access accident';
        await pump(
          tester,
          PhraseScreen(
            phrase: words,
            onDone: () => done = true,
            random: Random(1),
          ),
        );
        expect(find.textContaining('abandon'), findsOneWidget);
        await tester.tap(find.byKey(const Key('phrase-written')));
        await settle(tester);
        await tester.enterText(
          find.byKey(const Key('phrase-check-0')),
          'wrong',
        );
        await tester.tap(find.byKey(const Key('phrase-check-go')));
        await settle(tester);
        expect(done, isFalse);
        expect(find.textContaining('do not match'), findsOneWidget);
        final w = words.split(' ');
        String wordFor(Key k) {
          final label = (tester
              .widget<TextField>(find.byKey(k))
              .decoration!
              .labelText)!;
          return w[int.parse(label.split(' ').last) - 1];
        }

        await tester.enterText(
          find.byKey(const Key('phrase-check-0')),
          wordFor(const Key('phrase-check-0')),
        );
        await tester.enterText(
          find.byKey(const Key('phrase-check-1')),
          wordFor(const Key('phrase-check-1')),
        );
        await tester.tap(find.byKey(const Key('phrase-check-go')));
        await settle(tester);
        expect(done, isTrue);
      });

      testWidgets('devices and passkeys', (tester) async {
        await setSize(tester, size);
        await boot(tester);
        await pump(tester, SealedDevicesScreen(server: serverOf(app)));
        expect(find.text('Kai laptop'), findsOneWidget);
        expect(find.text('this device'), findsOneWidget);
        expect(find.text('Kai phone'), findsOneWidget);
        expect(find.text('can unlock sealed chats'), findsOneWidget);
        expect(find.text('cannot unlock sealed chats'), findsOneWidget);
        expect(
          find.byKey(
            const Key('sealed-remove-0193aa00-0000-7000-8000-0000000000a2'),
          ),
          findsOneWidget,
        );
      });

      testWidgets(
        'approving a device: a wrong code is refused and offers to end its session',
        (tester) async {
          await setSize(tester, size);
          await boot(tester);
          final request = (await tester.runAsync(() async {
            final other = await TestSealedSession.make();
            return (await other.device!.newUnlockRequest()).$1;
          }))!;
          final r = UnlockRequestRow({
            'id': 'req1',
            'device_id': '0193aa00-0000-7000-8000-0000000000a9',
            'request': b64(request),
            'device': {'name': 'Kai tablet', 'platform': 'android'},
            'created_at': '2026-11-14T09:00:00Z',
          });
          await pump(
            tester,
            ApproveDeviceScreen(server: serverOf(app), request: r),
          );
          expect(find.textContaining('Kai tablet'), findsOneWidget);
          await tester.enterText(
            find.byKey(const Key('approve-code')),
            'AAAAA-AAAAA-AAAAA-AAAAA',
          );
          await tester.tap(find.byKey(const Key('approve-go')));
          await tester.runAsync(
            () => Future<void>.delayed(const Duration(milliseconds: 200)),
          );
          await settle(tester);
          expect(find.byKey(const Key('approve-end-session')), findsOneWidget);
        },
      );

      testWidgets('compare codes: 60 digits in twelve groups, and the mark', (
        tester,
      ) async {
        await setSize(tester, size);
        await boot(tester);
        await pump(
          tester,
          CompareCodeScreen(
            server: serverOf(app),
            other: samId,
            name: 'Sam Okafor',
          ),
        );
        final digits = find.descendant(
          of: find.byKey(const Key('compare-digits')),
          matching: find.byType(Text),
        );
        expect(digits, findsNWidgets(12));
        await tester.tap(find.byKey(const Key('compare-mark')));
        await tester.runAsync(
          () => Future<void>.delayed(const Duration(milliseconds: 50)),
        );
        await settle(tester);
        expect(session.verified(samId), isTrue);
        expect(find.textContaining('not synced'), findsOneWidget);
      });

      testWidgets('new sealed chat: what it is, the passkeys, the people', (
        tester,
      ) async {
        await setSize(tester, size);
        await boot(tester);
        await pump(
          tester,
          NewSealedChatScreen(app: app, server: serverOf(app)),
        );
        expect(find.textContaining('between two people only'), findsOneWidget);
        expect(find.textContaining('Work laptop'), findsOneWidget);
        await tester.scrollUntilVisible(
          find.byKey(const Key('sealed-person-$samId')),
          200,
          scrollable: find.byType(Scrollable).first,
        );
        expect(find.byKey(const Key('sealed-person-$samId')), findsOneWidget);
        expect(find.byKey(const Key('sealed-person-$meId')), findsNothing);
      });
    });
  }
}
