import 'package:churr/core/db/cache_db.dart';
import 'package:churr/state/app_state.dart';
import 'package:churr/ui/screens/conversation/keep_focus.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

import '../conversation_support.dart';

/// The composer keeps the keyboard (docs/CLIENTS.md section 9.5): typing
/// anywhere in a conversation goes into the message box, while selecting
/// and copying text, other text boxes, keyboard navigation and phones are
/// left alone.
Finder rich(String s) => find.textContaining(s, findRichText: true);

final _box = find.byKey(const Key('composer'));
Finder _threadBox() => find.descendant(
  of: find.byKey(const Key('thread-pane')),
  matching: find.byKey(const Key('composer')),
);
Finder _mainBox() => find.descendant(
  of: find.byKey(const Key('message-list')).evaluate().isEmpty
      ? find.byType(Scaffold)
      : find
            .ancestor(
              of: find.byKey(const Key('message-list')),
              matching: find.byType(Column),
            )
            .first,
  matching: _box,
);

TextField _field(WidgetTester tester, [Finder? f]) =>
    tester.widget<TextField>(f ?? _box.first);
bool _focused(WidgetTester tester, [Finder? f]) =>
    _field(tester, f).focusNode!.hasFocus;
String _text(WidgetTester tester, [Finder? f]) =>
    _field(tester, f).controller!.text;

Future<AppState> _open(
  WidgetTester tester, {
  TargetPlatform platform = TargetPlatform.macOS,
  Size size = desktop,
  FakeServer? server,
}) async {
  await setSize(tester, size);
  late CacheDb db;
  final fake = server ?? FakeServer();
  await tester.runAsync(() async => db = await conversationCache());
  final app = await conversationApp(tester, db, fake);
  await pumpConversation(tester, app, platform: platform);
  return app;
}

Future<void> _click(WidgetTester tester, Finder f) async {
  await tester.tap(f, kind: PointerDeviceKind.mouse);
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 50));
}

Future<void> _type(WidgetTester tester, String s) async {
  for (final ch in s.split('')) {
    final key = ch == ' '
        ? LogicalKeyboardKey.space
        : LogicalKeyboardKey(ch.toLowerCase().codeUnitAt(0));
    await tester.sendKeyEvent(key, character: ch);
  }
  await tester.pump();
}

void main() {
  testWidgets('a desktop: opening a conversation puts focus in the box', (
    tester,
  ) async {
    await _open(tester);
    await tester.pump();
    expect(_focused(tester), isTrue);
  });

  testWidgets(
    'a click on a message or empty space gives the box its focus back',
    (tester) async {
      await _open(tester);
      await _click(tester, rich('Lunch?'));
      expect(_focused(tester), isTrue, reason: 'the click gave focus back');
      // Empty space and a button do the same.
      await _click(tester, find.text('Sounds good'));
      expect(_focused(tester), isTrue);
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets(
    'a printable key outside the box goes into it, once; Tab still moves focus',
    (tester) async {
      await _open(tester);
      expect(_focused(tester), isTrue);
      // Shift+Tab moves focus out of the box, as it always did.
      await tester.sendKeyDownEvent(LogicalKeyboardKey.shiftLeft);
      await tester.sendKeyEvent(LogicalKeyboardKey.tab);
      await tester.sendKeyUpEvent(LogicalKeyboardKey.shiftLeft);
      await tester.pump();
      expect(_focused(tester), isFalse);
      final away = FocusManager.instance.primaryFocus;
      expect(KeepFocus.isTextBox(away), isFalse);
      await _type(tester, 'h');
      expect(_text(tester), 'h');
      expect(_focused(tester), isTrue, reason: 'focus went with the letter');
      // Now in the box, keys are the box's own: the net adds nothing.
      await _type(tester, 'i');
      expect(_text(tester), 'h');
      // Alt with a letter outside the box is left alone.
      await tester.sendKeyDownEvent(LogicalKeyboardKey.shiftLeft);
      await tester.sendKeyEvent(LogicalKeyboardKey.tab);
      await tester.sendKeyUpEvent(LogicalKeyboardKey.shiftLeft);
      await tester.pump();
      expect(_focused(tester), isFalse);
      await tester.sendKeyDownEvent(LogicalKeyboardKey.altLeft);
      await tester.sendKeyEvent(LogicalKeyboardKey.keyB, character: '∫');
      await tester.sendKeyUpEvent(LogicalKeyboardKey.altLeft);
      await tester.pump();
      expect(_text(tester), 'h');
      expect(_focused(tester), isFalse);
    },
  );

  testWidgets('selecting text in a message keeps it, and Ctrl+C copies it', (
    tester,
  ) async {
    String? copied;
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
      SystemChannels.platform,
      (call) async {
        if (call.method == 'Clipboard.setData') {
          copied = (call.arguments as Map)['text'] as String?;
        }
        return null;
      },
    );
    addTearDown(
      () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        null,
      ),
    );
    await _open(tester);
    final r = tester.getRect(
      find.text('Deploy plan for Friday', findRichText: true),
    );
    await tester.dragFrom(
      r.centerLeft + const Offset(1, 0),
      Offset(r.width - 2, 0),
      kind: PointerDeviceKind.mouse,
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));
    expect(KeepFocus.selecting, isTrue, reason: 'the drag selected text');
    expect(_focused(tester), isFalse, reason: 'the selection keeps focus');
    // The test binding's shortcuts are those of its default platform.
    await tester.sendKeyDownEvent(LogicalKeyboardKey.controlLeft);
    await tester.sendKeyEvent(LogicalKeyboardKey.keyC);
    await tester.sendKeyUpEvent(LogicalKeyboardKey.controlLeft);
    await tester.pump();
    expect(copied, contains('Deploy plan'));
    expect(_text(tester), isEmpty);
    // Typing after copying goes into the box.
    await _type(tester, 'ok');
    expect(_text(tester), 'o');
    expect(_focused(tester), isTrue);
  });

  testWidgets('after sending with the button the box keeps focus', (
    tester,
  ) async {
    final fake = FakeServer();
    await _open(tester, server: fake);
    await tester.enterText(_box, 'hello');
    await _click(tester, find.byKey(const Key('send')));
    await settle(tester);
    expect(_focused(tester), isTrue);
  });

  testWidgets('a menu opened by a click gives focus back when it closes', (
    tester,
  ) async {
    await _open(tester);
    await _click(tester, find.byKey(const Key('thread-view')));
    await tester.pump(const Duration(milliseconds: 300));
    expect(_focused(tester), isFalse, reason: 'the menu has focus');
    await tester.sendKeyEvent(LogicalKeyboardKey.escape);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));
    expect(_focused(tester), isTrue);
  });

  testWidgets(
    'with a thread open, typing goes to the box used last; switching conversation focuses the new box',
    (tester) async {
      final fake = FakeServer();
      fake.on(
        'GET /api/v1/messages/m1/thread',
        (_) => {
          'parent': msg(
            'm1',
            10,
            'Deploy plan for Friday',
            thread: {'reply_count': 2, 'last_reply_seq': 24},
          ).raw,
          'messages': [
            msg('r1', 22, 'I can do Friday', author: 'lee', parent: 'm1').raw,
          ],
          'has_more': false,
        },
      );
      final app = await _open(tester, server: fake);
      await _click(tester, find.byKey(const Key('marker-m1')));
      await settle(tester);
      expect(find.byKey(const Key('thread-pane')), findsOneWidget);
      expect(_focused(tester, _threadBox()), isTrue);
      await _click(tester, rich('Lunch?'));
      expect(
        _focused(tester, _threadBox()),
        isTrue,
        reason: 'the thread box was used last',
      );
      await _click(tester, _mainBox());
      await _click(tester, rich('Lunch?'));
      expect(_focused(tester, _mainBox()), isTrue);

      await pumpConversation(tester, app, id: design);
      await tester.pump();
      expect(_focused(tester), isTrue, reason: 'the new conversation');
    },
  );

  testWidgets('a phone: only sending keeps focus', (tester) async {
    await _open(tester, platform: TargetPlatform.android, size: phone);
    await tester.pump();
    expect(_focused(tester), isFalse, reason: 'no keyboard pops up');
    await tester.tap(rich('Lunch?'));
    await tester.pump(const Duration(milliseconds: 50));
    expect(_focused(tester), isFalse);
    await tester.enterText(_box, 'from the phone');
    await tester.tap(find.byKey(const Key('send')));
    await settle(tester);
    expect(_focused(tester), isTrue);
  });
}
