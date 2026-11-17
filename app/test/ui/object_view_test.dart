// How the objects of ruling R73 after references and message links are
// drawn in a message: the typed text always shown, with what each kind
// adds for the reader.

import 'dart:convert';

import 'package:churr/core/instant/zone.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/ui/message/codepoint_view.dart';
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
        expect(drawn(tester), contains('5 km (3.11 mi)'));
      });
    }
    testWidgets('nothing extra in the reader\'s own system', (tester) async {
      await show(tester, 'only 3 mi', [
        {'type': 'measure', 'at': 5, 'len': 4, 'value': '3', 'unit': 'mi'},
      ]);
      expect(drawn(tester), contains('3 mi'));
      expect(drawn(tester), isNot(contains('(')));
    });
    for (final size in [const Size(390, 800), const Size(1440, 900)]) {
      testWidgets('the unit converter, ${size.width.toInt()} wide', (
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
        await show(tester, 'a 5 km run', [
          {'type': 'measure', 'at': 2, 'len': 4, 'value': '5', 'unit': 'km'},
        ], size: size);
        expect(
          find.byWidgetPredicate(
            (w) =>
                w is Semantics &&
                w.properties.label ==
                    'Measurement 5 km (3.11 mi). Opens the unit converter.',
          ),
          findsOneWidget,
        );
        // The keyboard: focus the control, Enter opens.
        final chip = find.byKey(const Key('measure-2'));
        Focus.of(
          tester.element(
            find.descendant(of: chip, matching: find.byType(Container)).first,
          ),
        ).requestFocus();
        await tester.pump();
        await tester.sendKeyEvent(LogicalKeyboardKey.enter);
        await tester.pumpAndSettle();
        expect(find.text('UNIT CONVERTER'), findsOneWidget);
        // Imperial first (the tests' locale is en_US), then metric.
        expect(find.text('3.107 mi'), findsOneWidget);
        expect(find.text('5,000,000 mm'), findsOneWidget);
        expect(
          tester.getTopLeft(find.text('196,900 in')).dy,
          lessThan(tester.getTopLeft(find.text('5,000,000 mm')).dy),
        );
        expect(find.text('nautical miles'), findsOneWidget);
        await tester.tap(find.byKey(const Key('measure-row-mi')));
        await tester.pump();
        expect(copied, '3.107 mi');
        expect(find.text('Copied 3.107 mi'), findsOneWidget);
        // Escape closes, and the keyboard is back on the control.
        await tester.sendKeyEvent(LogicalKeyboardKey.escape);
        await tester.pumpAndSettle();
        expect(find.text('UNIT CONVERTER'), findsNothing);
        expect(
          Focus.of(
            tester.element(
              find.descendant(of: chip, matching: find.byType(Container)).first,
            ),
          ).hasFocus,
          isTrue,
        );
        // Space opens it too.
        await tester.sendKeyEvent(LogicalKeyboardKey.space);
        await tester.pumpAndSettle();
        expect(find.text('UNIT CONVERTER'), findsOneWidget);
      });
    }
    testWidgets('temperatures list °C, °F and K', (tester) async {
      await show(tester, 'at 20 °C', [
        {'type': 'measure', 'at': 3, 'len': 5, 'value': '20', 'unit': '°C'},
      ], size: const Size(1440, 900));
      await tester.tap(find.byKey(const Key('measure-3')));
      await tester.pumpAndSettle();
      expect(find.text('68°F'), findsOneWidget);
      expect(find.text('293.1 K'), findsOneWidget);
    });
    testWidgets('data has no equivalent beside it, only the converter', (
      tester,
    ) async {
      await show(tester, 'it is 5 GB', [
        {'type': 'measure', 'at': 6, 'len': 4, 'value': '5', 'unit': 'GB'},
      ], size: const Size(1440, 900));
      expect(drawn(tester), isNot(contains('(')));
      await tester.tap(find.byKey(const Key('measure-6')));
      await tester.pumpAndSettle();
      expect(find.text('5,000,000,000 B'), findsOneWidget);
      expect(find.text('4.657 GiB'), findsOneWidget);
    });
  });

  group('numbers in another base', () {
    for (final size in [const Size(390, 800), const Size(1440, 900)]) {
      testWidgets('the number converter, ${size.width.toInt()} wide', (
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
        await show(tester, 'mask 0x4f22 set', [
          {
            'type': 'number',
            'at': 5,
            'len': 6,
            'value': '20258',
            'base': 16,
          },
        ], size: size);
        expect(
          find.byWidgetPredicate(
            (w) =>
                w is Semantics &&
                w.properties.label ==
                    'Number 0x4f22, 20,258 in decimal. Opens the number converter.',
          ),
          findsOneWidget,
        );
        final chip = find.byKey(const Key('number-5'));
        final focus = Focus.of(
          tester.element(
            find.descendant(of: chip, matching: find.byType(Container)).first,
          ),
        );
        focus.requestFocus();
        await tester.pump();
        await tester.sendKeyEvent(LogicalKeyboardKey.enter);
        await tester.pumpAndSettle();
        expect(find.text('NUMBER CONVERTER'), findsOneWidget);
        expect(find.text('20,258'), findsOneWidget);
        expect(find.text('0x4F22'), findsOneWidget);
        expect(find.text('0o47442'), findsOneWidget);
        expect(find.text('0b0100 1111 0010 0010'), findsOneWidget);
        await tester.tap(find.byKey(const Key('number-row-binary')));
        await tester.pump();
        expect(copied, '0b100111100100010');
        await tester.sendKeyEvent(LogicalKeyboardKey.escape);
        await tester.pumpAndSettle();
        expect(find.text('NUMBER CONVERTER'), findsNothing);
        expect(focus.hasFocus, isTrue);
        // As a character, which opens the code point.
        await tester.sendKeyEvent(LogicalKeyboardKey.space);
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const Key('number-as-char')));
        await tester.pumpAndSettle();
        expect(find.text('CODE POINT'), findsOneWidget);
        expect(find.text('E4 BC A2'), findsOneWidget);
      });
    }
  });

  group('code points', () {
    testWidgets('the character beside what was typed', (tester) async {
      await show(tester, 'see U+1F600 and U+200D', [
        {'type': 'codepoint', 'at': 4, 'len': 7, 'value': 'U+1F600'},
        {'type': 'codepoint', 'at': 16, 'len': 6, 'value': 'U+200D'},
      ]);
      expect(drawn(tester), contains('U+1F600 '));
      expect(find.text('😀'), findsOneWidget);
      // What cannot be seen is a box with its short name.
      expect(find.text('ZWJ'), findsOneWidget);
    });
    for (final size in [const Size(390, 800), const Size(1440, 900)]) {
      testWidgets('what it is, ${size.width.toInt()} wide', (tester) async {
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
        await show(tester, 'see U+1F600', [
          {'type': 'codepoint', 'at': 4, 'len': 7, 'value': 'U+1F600'},
        ], size: size);
        final chip = find.byKey(const Key('codepoint-4'));
        final focus = Focus.of(
          tester.element(
            find.descendant(of: chip, matching: find.byType(Container)).first,
          ),
        );
        focus.requestFocus();
        await tester.pump();
        await tester.sendKeyEvent(LogicalKeyboardKey.enter);
        await tester.pumpAndSettle();
        expect(find.text('CODE POINT'), findsOneWidget);
        expect(find.text('F0 9F 98 80'), findsOneWidget);
        expect(find.text(r'\uD83D\uDE00'), findsOneWidget);
        expect(find.text(r'\U0001F600'), findsOneWidget);
        // No server here: everything but the name.
        expect(find.text('The server could not say.'), findsOneWidget);
        await tester.tap(find.byKey(const Key('cp-row-utf8')));
        await tester.pump();
        expect(copied, 'F0 9F 98 80');
        await tester.sendKeyEvent(LogicalKeyboardKey.escape);
        await tester.pumpAndSettle();
        expect(find.text('CODE POINT'), findsNothing);
        expect(focus.hasFocus, isTrue);
      });
    }
    testWidgets('the name, once the server answers', (tester) async {
      var asked = 0;
      await tester.pumpWidget(
        MaterialApp(
          theme: buildTheme(dark: true),
          localizationsDelegates: L10n.localizationsDelegates,
          home: Scaffold(
            body: CodePointSheet(
              value: 'U+10F2',
              typed: 'U+10F2',
              lookup: (v) async {
                asked++;
                await Future<void>.delayed(const Duration(milliseconds: 50));
                return (
                  name: 'GEORGIAN LETTER HIE',
                  category: 'Ll',
                  categoryName: 'Lowercase Letter',
                );
              },
            ),
          ),
        ),
      );
      expect(find.text('Asking the server…'), findsOneWidget);
      expect(find.text('E1 83 B2'), findsOneWidget);
      await tester.pump(const Duration(milliseconds: 60));
      expect(find.text('GEORGIAN LETTER HIE'), findsOneWidget);
      expect(find.text('Lowercase Letter (Ll)'), findsOneWidget);
      expect(asked, 1);
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
      await show(
        tester,
        'I hit for [[2d6+3]] damage',
        [rolled],
        mc: MarkdownContext(
          liveDice: (at) => asked++ == 0 ? Duration.zero : null,
        ),
      );
      // From its first frame it rolls, and the total waits for it.
      final state = tester.state<DiceChipState>(find.byType(DiceChip));
      expect(state.spinning, isTrue);
      expect(drawn(tester), isNot(contains('= 11')));
      await tester.pump(const Duration(milliseconds: 600));
      expect(state.spinning, isTrue);
      expect(drawn(tester), isNot(contains('=')));
      await tester.pump(const Duration(seconds: 2));
      expect(state.spinning, isFalse);
      expect(drawn(tester), contains('= 11'));
      // A tap does not roll it again.
      await tester.tap(find.byKey(const Key('dice-10')));
      await tester.pump(const Duration(milliseconds: 60));
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
