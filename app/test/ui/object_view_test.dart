// How the objects of ruling R73 after references and message links are
// drawn in a message: the typed text always shown, with what each kind
// adds for the reader.

import 'package:churr/core/models/message.dart';
import 'package:churr/l10n/app_localizations.dart';
import 'package:churr/ui/message/markdown_view.dart';
import 'package:churr/ui/theme/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

Future<void> show(
  WidgetTester tester,
  String text,
  List<Map<String, dynamic>> entities, {
  Size size = const Size(390, 800),
  MarkdownContext mc = const MarkdownContext(),
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    MaterialApp(
      theme: buildTheme(dark: false),
      localizationsDelegates: L10n.localizationsDelegates,
      home: Scaffold(
        body: MarkdownView(
          text: text,
          entities: [for (final e in entities) Entity.fromJson(e)],
          context_: mc,
        ),
      ),
    ),
  );
}

/// All the text drawn, joined.
String drawn(WidgetTester tester) => tester
    .widgetList<RichText>(find.byType(RichText))
    .map((r) => r.text.toPlainText())
    .join('|');

void main() {
  group('measurements', () {
    // Tests run in the en_US locale: imperial, unless chosen otherwise.
    for (final size in [const Size(390, 800), const Size(1440, 900)]) {
      testWidgets('the equivalent follows, ${size.width.toInt()} wide', (
        tester,
      ) async {
        await show(tester, 'a 5 km run', [
          {'type': 'measure', 'at': 2, 'len': 4, 'value': '5', 'unit': 'km'},
        ], size: size);
        expect(drawn(tester), contains('a 5 km (3.11 mi) run'));
      });
    }
    testWidgets('nothing extra in the reader\'s own system', (tester) async {
      await show(tester, 'only 3 mi', [
        {'type': 'measure', 'at': 5, 'len': 4, 'value': '3', 'unit': 'mi'},
      ]);
      expect(drawn(tester), contains('only 3 mi'));
      expect(drawn(tester), isNot(contains('(')));
    });
  });
}
