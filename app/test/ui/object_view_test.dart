// How the objects of ruling R73 after references and message links are
// drawn in a message: the typed text always shown, with what each kind
// adds for the reader.

import 'dart:convert';

import 'package:churr/core/instant/zone.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/ui/message/contact_view.dart';
import 'package:churr/ui/message/dice_view.dart';
import 'package:churr/ui/message/event_view.dart';
import 'package:churr/l10n/app_localizations.dart';
import 'package:churr/ui/message/markdown_view.dart';
import 'package:churr/ui/theme/theme.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
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

  group('colours', () {
    for (final size in [const Size(390, 800), const Size(1440, 900)]) {
      testWidgets('a swatch that copies, ${size.width.toInt()} wide', (
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
        await show(tester, 'logo #fff here', [
          {'type': 'colour', 'at': 5, 'len': 4, 'value': '#ffffff'},
        ], size: size);
        final swatch = find.byKey(const ValueKey('colour-5'));
        expect(swatch, findsOneWidget);
        expect(find.text('#fff'), findsOneWidget);
        expect(tester.getSemantics(swatch).label, 'Colour #fff. Copy');
        await tester.tap(swatch);
        expect(copied, '#ffffff');
      });
    }
  });

  group('events', () {
    setUp(() {
      InstantEnv.deviceZone = 'Europe/Berlin';
      InstantEnv.now = () => DateTime.utc(2026, 11, 18, 1);
    });
    tearDown(() {
      InstantEnv.deviceZone = null;
      InstantEnv.now = DateTime.now;
    });
    const text = 'Dinner 2026-12-24 18:00–20:00 Europe/Berlin';
    final event = {
      'type': 'event',
      'at': 7,
      'len': 36,
      'time': '2026-12-24T17:00:00.000Z',
      'end': '2026-12-24T19:00:00.000Z',
      'precision': 'minute',
      'zone': 'Europe/Berlin',
    };
    for (final size in [const Size(390, 800), const Size(1440, 900)]) {
      testWidgets(
        'one range, added to a calendar, ${size.width.toInt()} wide',
        (tester) async {
          String? saved;
          final was = saveEventFile;
          saveEventFile = (name, Uint8List bytes) async =>
              saved = utf8.decode(bytes);
          addTearDown(() => saveEventFile = was);
          await show(
            tester,
            text,
            [event],
            size: size,
            mc: const MarkdownContext(
              eventFile: (
                title: 'Dinner',
                link: 'https://chat.example/chat/c/a/m/b',
                id: 'b',
              ),
            ),
          );
          final chip = find.byKey(const Key('event-7'));
          expect(chip, findsOneWidget);
          expect(
            find.textContaining(RegExp(r'Dec 24, 6:00\sPM – 8:00\sPM')),
            findsOneWidget,
          );
          expect(
            find.bySemanticsLabel(RegExp('^Event from .*Typed as 2026')),
            findsOneWidget,
          );
          await tester.tap(chip);
          await tester.pumpAndSettle();
          await tester.tap(find.byKey(const Key('event-add-to-calendar')));
          await tester.pumpAndSettle();
          expect(
            saved,
            contains('DTSTART:20261224T170000Z\r\nDTEND:20261224T190000Z'),
          );
          expect(saved, contains('SUMMARY:Dinner'));
          expect(saved, contains('URL:https://chat.example/chat/c/a/m/b'));
        },
      );
    }
  });

  group('phone numbers and email addresses', () {
    for (final size in [const Size(390, 800), const Size(1440, 900)]) {
      testWidgets('call, write or copy, ${size.width.toInt()} wide', (
        tester,
      ) async {
        final opened = <String>[];
        final was = openContact;
        openContact = (uri) async => opened.add('$uri');
        addTearDown(() => openContact = was);
        await show(tester, 'call +61 7 3123 4567 or sam@example.com', [
          {'type': 'phone', 'at': 5, 'len': 15, 'value': '+61731234567'},
          {'type': 'email', 'at': 24, 'len': 15, 'value': 'sam@example.com'},
        ], size: size);
        final phone = find.byKey(const Key('phone-5'));
        expect(
          tester.getSemantics(phone).label,
          'Phone number +61 7 3123 4567',
        );
        await tester.tap(phone);
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const Key('phone-open')));
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const Key('email-24')));
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const Key('email-open')));
        await tester.pumpAndSettle();
        expect(opened, ['tel:+61731234567', 'mailto:sam@example.com']);
      });
    }
  });

  group('places', () {
    final place = {
      'type': 'geo',
      'at': 3,
      'len': 18,
      'lat': '-27.4698',
      'lon': '153.0251',
    };
    testWidgets('a phone opens its maps', (tester) async {
      debugDefaultTargetPlatformOverride = TargetPlatform.android;
      addTearDown(() => debugDefaultTargetPlatformOverride = null);
      final opened = <String>[];
      final was = openContact;
      openContact = (uri) async => opened.add('$uri');
      addTearDown(() => openContact = was);
      await show(tester, 'at -27.4698, 153.0251 now', [place]);
      final chip = find.byKey(const Key('geo-3'));
      expect(
        tester.getSemantics(chip).label,
        'Place -27.4698, 153.0251. Open in maps',
      );
      await tester.tap(chip);
      expect(opened, ['geo:-27.4698,153.0251?q=-27.4698,153.0251']);
      debugDefaultTargetPlatformOverride = null;
    });
    testWidgets('a desktop copies, 1440 wide', (tester) async {
      debugDefaultTargetPlatformOverride = TargetPlatform.macOS;
      addTearDown(() => debugDefaultTargetPlatformOverride = null);
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
      await show(tester, 'at -27.4698, 153.0251 now', [
        place,
      ], size: const Size(1440, 900));
      await tester.tap(find.byKey(const Key('geo-3')));
      expect(copied, '-27.4698,153.0251');
      debugDefaultTargetPlatformOverride = null;
    });
  });

  group('inline dice', () {
    final rolled = {
      'type': 'dice',
      'at': 10,
      'len': 9,
      'value': '2d6+3',
      'terms': [
        {
          'sign': 1,
          'count': 2,
          'sides': 6,
          'rolls': [4, 4],
          'value': 8,
        },
        {'sign': 1, 'value': 3},
      ],
      'total': 11,
    };
    for (final size in [const Size(390, 800), const Size(1440, 900)]) {
      testWidgets(
        'the server\'s roll in the sentence, ${size.width.toInt()} wide',
        (tester) async {
          await show(tester, 'I hit for [[2d6+3]] damage', [
            rolled,
          ], size: size);
          final chip = find.byKey(const Key('dice-10'));
          expect(chip, findsOneWidget);
          expect(drawn(tester), contains('[[2d6+3]] 4 4 +3 = 11'));
          expect(tester.getSemantics(chip).label, contains('total 11'));
        },
      );
    }
    testWidgets('it spins once when it arrived live', (tester) async {
      var asked = 0;
      await show(tester, 'I hit for [[2d6+3]] damage', [
        rolled,
      ], mc: MarkdownContext(liveDice: (at) => asked++ == 0));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 60));
      final state = tester.state<DiceChipState>(find.byType(DiceChip));
      expect(state.spinning, isTrue);
      await tester.pump(const Duration(seconds: 2));
      expect(state.spinning, isFalse);
      expect(drawn(tester), contains('= 11'));
    });
    testWidgets('dice an edit wrote say nothing was rolled', (tester) async {
      await show(tester, 'now [[1d6]]', [
        {'type': 'dice', 'at': 4, 'len': 7, 'value': '1d6', 'removed': true},
      ]);
      expect(
        drawn(tester),
        contains('[[1d6]] (not rolled: written in an edit)'),
      );
    });
  });
}
