// Instants (ruling R69) drawn in the app, in a fixed reader's zone and
// locale: the control in a message, the converter with a zone added, and
// how the words are formatted in other locales.

import 'package:churr/core/instant/format.dart';
import 'package:churr/core/instant/zone.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/l10n/app_localizations.dart';
import 'package:churr/ui/message/markdown_view.dart';
import 'package:churr/ui/theme/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:intl/date_symbol_data_local.dart';

const text = 'Party 2026-12-24 18:00 Europe/Berlin, lunch tomorrow at 1pm';
final entities = [
  Entity(
    type: 'instant',
    at: 6,
    len: 30,
    time: '2026-12-24T17:00:00.000Z',
    precision: 'minute',
    zone: 'Europe/Berlin',
  ),
  Entity(
    type: 'instant',
    at: 44,
    len: 15,
    time: '2026-11-19T18:00:00.000Z',
    precision: 'minute',
    zone: 'America/New_York',
  ),
];

Future<void> show(
  WidgetTester tester, {
  MarkdownContext context = const MarkdownContext(),
}) async {
  await tester.pumpWidget(
    MaterialApp(
      theme: buildTheme(dark: true),
      locale: const Locale('en'),
      localizationsDelegates: L10n.localizationsDelegates,
      supportedLocales: L10n.supportedLocales,
      home: Scaffold(
        body: MarkdownView(text: text, entities: entities, context_: context),
      ),
    ),
  );
  await tester.pump();
}

String plain(WidgetTester tester) => tester
    .widgetList<RichText>(find.byType(RichText))
    .map((r) => r.text.toPlainText(includePlaceholders: false))
    .join('|')
    .replaceAll(' ', ' ');

void main() {
  setUpAll(() async {
    await initializeDateFormatting('en_AU');
    await initializeDateFormatting('en_GB');
  });
  setUp(() {
    // The reader is in New York at 9 in the morning of 18 November 2026.
    InstantEnv.deviceZone = 'America/New_York';
    InstantEnv.now = () => DateTime.utc(2026, 11, 18, 14);
  });
  tearDown(() {
    InstantEnv.deviceZone = null;
    InstantEnv.now = DateTime.now;
  });

  testWidgets(
    'an instant is shown in the reader\'s zone, as a control with the whole story in its label',
    (tester) async {
      final semantics = tester.ensureSemantics();
      await show(tester);
      final all = plain(tester);
      expect(all, contains('Thu, Dec 24, 12:00 PM'));
      // A phrase the author made into a time is cut out of its text run.
      expect(all, contains('Tomorrow, 1:00 PM'));
      expect(all, contains('Party '));
      expect(all, isNot(contains('tomorrow at 1pm')));
      final label = tester
          .getSemantics(find.byKey(const Key('instant-6')))
          .label
          .replaceAll(' ', ' ');
      expect(label, contains('Thursday, December 24, 2026, 12:00 PM'));
      expect(label, contains('America/New York'));
      expect(label, contains('Written as “2026-12-24 18:00 Europe/Berlin”'));
      semantics.dispose();
    },
  );

  testWidgets(
    'an entity of no instant node, or with no time, is drawn as its text',
    (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: buildTheme(dark: true),
          localizationsDelegates: L10n.localizationsDelegates,
          supportedLocales: L10n.supportedLocales,
          home: Scaffold(
            body: MarkdownView(
              text: 'at 2026-12-24 18:00',
              entities: [Entity(type: 'instant', at: 3, len: 16)],
            ),
          ),
        ),
      );
      expect(plain(tester), contains('2026-12-24 18:00'));
    },
  );

  testWidgets(
    'the converter: your time, as written, UTC, how long, a zone added, remind me',
    (tester) async {
      DateTime? reminded;
    await tester.binding.setSurfaceSize(const Size(1280, 1000));
    addTearDown(() => tester.binding.setSurfaceSize(null));
      await show(
        tester,
        context: MarkdownContext(onRemindAt: (at) async => reminded = at),
      );
      await tester.tap(find.byKey(const Key('instant-6')));
      await tester.pumpAndSettle();
      String row(String zone) => tester
          .widgetList<Text>(
            find.descendant(
              of: find.byKey(Key('instant-row-$zone')),
              matching: find.byType(Text),
            ),
          )
          .map((t) => t.data)
          .join(' ')
          .replaceAll(' ', ' ');
      expect(
        row('America/New_York'),
        allOf(
          contains('YOUR TIME · AMERICA/NEW YORK (UTC−5)'),
          contains('12:00 PM'),
        ),
      );
      expect(
        row('Europe/Berlin'),
        allOf(
          contains('AS WRITTEN · EUROPE/BERLIN (UTC+1)'),
          contains('6:00 PM'),
        ),
      );
      expect(row('UTC'), contains('5:00 PM'));
      expect(
        tester.widget<Text>(find.byKey(const Key('instant-relative'))).data,
        'in 36 days',
      );
      await tester.enterText(find.byKey(const Key('instant-search')), 'tokyo');
      await tester.pump();
      await tester.tap(find.byKey(const Key('instant-zone-Asia/Tokyo')));
      await tester.pump();
      expect(
        row('Asia/Tokyo'),
        allOf(
          contains('ASIA/TOKYO (UTC+9)'),
          contains('Friday, December 25, 2026'),
          contains('2:00 AM'),
        ),
      );
      await tester.tap(find.text('Remind me'));
      await tester.pumpAndSettle();
      expect(reminded, DateTime.utc(2026, 12, 24, 17));
    },
  );

  testWidgets('no "Remind me" for a time gone by', (tester) async {
    InstantEnv.now = () => DateTime.utc(2027, 1, 2);
    await show(tester, context: MarkdownContext(onRemindAt: (at) async {}));
    await tester.tap(find.byKey(const Key('instant-6')));
    await tester.pumpAndSettle();
    expect(find.text('Remind me'), findsNothing);
    expect(
      tester.widget<Text>(find.byKey(const Key('instant-relative'))).data,
      '8 days ago',
    );
  });

  test('formatting in other locales and years', () {
    final now = DateTime.utc(2026, 11, 18, 14);
    final w = InstantWords.english;
    int at(String s) => DateTime.parse(s).millisecondsSinceEpoch;
    String sp(String s) => s.replaceAll(' ', ' ');
    expect(
      sp(
        compactInstant(
          at('2026-11-20T19:30:00Z'),
          'America/New_York',
          'en_AU',
          w,
          now: now,
        ),
      ),
      'Fri, 20 Nov, 2:30 pm',
    );
    expect(
      sp(
        compactInstant(
          at('2026-11-20T19:30:00Z'),
          'America/New_York',
          'en_GB',
          w,
          now: now,
        ),
      ),
      'Fri 20 Nov, 14:30',
    );
    expect(
      sp(
        compactInstant(
          at('2027-01-08T17:00:00Z'),
          'America/New_York',
          'en',
          w,
          now: now,
        ),
      ),
      'Fri, Jan 8, 2027, 12:00 PM',
    );
    expect(
      sp(
        compactInstant(
          at('2026-11-17T13:00:00Z'),
          'America/New_York',
          'en',
          w,
          now: now,
        ),
      ),
      'Yesterday, 8:00 AM',
    );
    expect(
      sp(
        compactInstant(
          at('2026-11-19T04:59:00Z'),
          'America/New_York',
          'en',
          w,
          now: now,
        ),
      ),
      'Today, 11:59 PM',
    );
    expect(
      sp(
        compactInstant(
          at('2026-11-20T19:30:05Z'),
          'America/New_York',
          'en',
          w,
          precision: 'second',
          now: now,
        ),
      ),
      'Fri, Nov 20, 2:30:05 PM',
    );
    expect(
      zoneName('Asia/Kolkata', at('2026-07-01T00:00:00Z')),
      'Asia/Kolkata (UTC+5:30)',
    );
    expect(zoneName('-05', 0), 'UTC-05');
    expect(
      sp(fullInstant(at('2026-12-24T08:00:00Z'), '+10:00', 'en')),
      'Thursday, December 24, 2026, 6:00 PM',
    );
  });
}
