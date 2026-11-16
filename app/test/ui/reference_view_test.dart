// A reference (ruling R73) is drawn as a link over the typed text, reached
// by the keyboard, labelled with the text as typed, opening the address it
// resolved to when it was written.

import 'package:churr/core/models/message.dart';
import 'package:churr/ui/message/markdown_view.dart';
import 'package:churr/ui/theme/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  Future<List<String>> show(
    WidgetTester tester,
    Size size,
    List<Entity> entities,
    String text,
  ) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final opened = <String>[];
    await tester.pumpWidget(
      MaterialApp(
        theme: buildTheme(dark: false),
        home: Scaffold(
          body: MarkdownView(
            text: text,
            entities: entities,
            context_: MarkdownContext(onOpenLink: opened.add),
          ),
        ),
      ),
    );
    return opened;
  }

  final ref = Entity(
    type: 'reference',
    at: 4,
    len: 7,
    url: 'https://tracker.example/browse/PROJ-12',
    patternId: 'p1',
    value: '12',
  );

  for (final size in [const Size(390, 800), const Size(1440, 900)]) {
    testWidgets('drawn as a link at ${size.width.toInt()} wide', (
      tester,
    ) async {
      final opened = await show(tester, size, [ref], 'See PROJ-12, please.');
      final link = find.byKey(const ValueKey('reference-4'));
      expect(link, findsOneWidget);
      expect(find.text('PROJ-12'), findsOneWidget);
      expect(
        tester.getSemantics(link),
        matchesSemantics(
          isLink: true,
          label: 'PROJ-12',
          hint: 'tracker.example',
          isFocusable: true,
          hasTapAction: true,
          hasFocusAction: true,
        ),
      );
      await tester.tap(link);
      expect(opened, ['https://tracker.example/browse/PROJ-12']);
      // The keyboard reaches it and Enter opens it.
      await tester.sendKeyEvent(LogicalKeyboardKey.tab);
      await tester.pump();
      await tester.sendKeyEvent(LogicalKeyboardKey.enter);
      await tester.pump();
      expect(opened, hasLength(2));
    });
  }

  testWidgets('an address that is not the web is drawn as text', (
    tester,
  ) async {
    await show(tester, const Size(1440, 900), [
      Entity(
        type: 'reference',
        at: 0,
        len: 7,
        url: 'javascript:alert(1)',
        value: '1',
      ),
    ], 'PROJ-1x');
    expect(find.byKey(const ValueKey('reference-0')), findsNothing);
  });
}
