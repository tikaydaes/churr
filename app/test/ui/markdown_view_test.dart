import 'package:churr/core/highlight/languages.dart';
import 'package:churr/core/models/account.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/core/models/server.dart';
import 'package:churr/ui/message/highlight.dart';
import 'package:churr/ui/message/markdown_view.dart';
import 'package:churr/ui/theme/theme.dart';
import 'package:churr/ui/theme/tokens.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

const chan = '0193a000-0000-7000-8000-000000000020';

Future<void> show(
  WidgetTester tester,
  String text, {
  List<Entity> entities = const [],
  List<TaskState> tasks = const [],
  MarkdownContext context = const MarkdownContext(),
}) => tester.pumpWidget(
  MaterialApp(
    theme: buildTheme(dark: true),
    home: Scaffold(
      body: SingleChildScrollView(
        child: MarkdownView(
          text: text,
          entities: entities,
          tasks: tasks,
          context_: context,
        ),
      ),
    ),
  ),
);

/// Everything drawn as text, in order.
String shown(WidgetTester tester) => tester
    .widgetList<RichText>(find.byType(RichText))
    .map((r) => r.text.toPlainText(includePlaceholders: false))
    .join('|');

Finder rich(String s) => find.textContaining(s, findRichText: true);

void main() {
  final accounts = {
    'a-sam': Account.fromJson({
      'id': 'a-sam',
      'username': 'sam',
      'display_name': 'Sam Okafor',
      'role': 'member',
    }),
  };

  testWidgets(
    'a mention is drawn from its entity, as the current display name',
    (tester) async {
      await show(
        tester,
        'hello @sam and @nobody',
        entities: [Entity(type: 'account', at: 6, len: 4, accountId: 'a-sam')],
        context: MarkdownContext(accounts: accounts),
      );
      // `@nobody` has the shape of a mention and no entity: plain text.
      expect(shown(tester), contains('hello @Sam Okafor and @nobody'));
    },
  );

  testWidgets('a mention of the reader is highlighted with the accent fill', (
    tester,
  ) async {
    await show(
      tester,
      '@sam',
      entities: [Entity(type: 'account', at: 0, len: 4, accountId: 'a-sam')],
      context: MarkdownContext(accounts: accounts, myId: 'a-sam'),
    );
    final span = tester.widget<RichText>(rich('@Sam Okafor')).text;
    TextStyle? found;
    span.visitChildren((s) {
      if (s is TextSpan && s.text == '@Sam Okafor') found = s.style;
      return true;
    });
    expect(found!.backgroundColor, ChurrColors.resolve(dark: true).lavender);
    expect(found!.color, ChurrColors.resolve(dark: true).onLavender);
  });

  testWidgets('a channel link is drawn by id as the channel\'s current name', (
    tester,
  ) async {
    String? opened;
    final renamed = Conversation.fromJson({
      'id': chan,
      'kind': 'channel',
      'private': false,
      'name': 'Operations (renamed)',
    });
    await show(
      tester,
      'see <#$chan> now',
      entities: [
        Entity(type: 'conversation', at: 4, len: 39, conversationId: chan),
      ],
      context: MarkdownContext(
        conversation: (id) => id == chan ? renamed : null,
        onOpenChannel: (id) => opened = id,
      ),
    );
    expect(rich('Operations (renamed)'), findsOneWidget);
    expect(rich(chan), findsNothing, reason: 'the identifier is never shown');
    await tester.tap(rich('Operations (renamed)'));
    expect(opened, chan);
  });

  testWidgets(
    'a link to a channel the reader cannot see, and one that names no channel',
    (tester) async {
      await show(
        tester,
        'a <#$chan> b <#0193a000-0000-7000-8000-00000000dead> c #ops',
        entities: [
          Entity(type: 'conversation', at: 2, len: 39, conversationId: chan),
        ],
        context: MarkdownContext(conversation: (_) => null),
      );
      // `#ops` typed by hand is plain text and is never resolved.
      expect(
        shown(tester),
        contains('a private channel b unknown channel c #ops'),
      );
    },
  );

  testWidgets(
    'a custom emoji is drawn as its image; a short code with no entity as typed',
    (tester) async {
      await show(
        tester,
        ':party: and :smile:',
        entities: [Entity(type: 'emoji', at: 0, len: 7, emojiId: 'e1')],
        context: MarkdownContext(
          emoji: {'e1': CustomEmoji(id: 'e1', name: 'party', url: '/x')},
          emojiImage: (e, size) =>
              SizedBox(key: Key('emoji-${e.name}'), width: size, height: size),
        ),
      );
      expect(find.byKey(const Key('emoji-party')), findsOneWidget);
      expect(rich(':smile:'), findsOneWidget);
    },
  );

  testWidgets(
    'links: a named link, a bare link from its entity, and a disallowed scheme as its text',
    (tester) async {
      final opened = <String>[];
      await show(
        tester,
        '[docs](https://example.org/docs) https://example.org/a [x](javascript:alert(1))',
        entities: [
          Entity(type: 'link', at: 33, len: 21, url: 'https://example.org/a'),
        ],
        context: MarkdownContext(onOpenLink: opened.add),
      );
      expect(shown(tester), contains('docs https://example.org/a x'));
      await tester.tap(rich('docs'), warnIfMissed: false);
    },
  );

  testWidgets(
    'a link whose text looks like another address shows its real host',
    (tester) async {
      await show(tester, '[bank.example](https://evil.example/login)');
      expect(shown(tester), contains('bank.example (evil.example)'));
    },
  );

  testWidgets('an unknown entity type is drawn as the plain text it covers', (
    tester,
  ) async {
    await show(
      tester,
      'hello @sam',
      entities: [Entity(type: 'hologram', at: 6, len: 4)],
    );
    expect(shown(tester), contains('hello @sam'));
  });

  testWidgets(
    'checklist items take their state from tasks, not the text, and can be ticked',
    (tester) async {
      final toggled = <(String, bool)>[];
      await show(
        tester,
        '- [ ] Tag the release\n- [x] Tell support\n- plain bullet',
        entities: [
          Entity(type: 'task', at: 6, len: 15, itemId: 't1'),
          Entity(type: 'task', at: 28, len: 12, itemId: 't2'),
        ],
        tasks: [
          TaskState(id: 't1', checked: true, checkedById: 'a-sam'),
          TaskState(id: 't2', checked: false),
        ],
        context: MarkdownContext(
          accounts: accounts,
          onToggleTask: (id, v) => toggled.add((id, v)),
        ),
      );
      final boxes = tester.widgetList<Checkbox>(find.byType(Checkbox)).toList();
      expect(boxes.map((b) => b.value), [
        true,
        false,
      ], reason: 'the typed [ ] and [x] only set the start');
      // A ticked item is followed by who ticked it.
      expect(rich('Sam Okafor'), findsOneWidget);
      await tester.tap(find.byType(Checkbox).last);
      expect(toggled, [('t2', true)]);
    },
  );

  testWidgets('blocks: headings, a quote, nested lists, a table and a rule', (
    tester,
  ) async {
    await show(
      tester,
      '# Title\n\n> quoted **bold**\n\n- one\n  - two\n\n3. three\n4. four\n\n'
      '| A | B |\n|---|--:|\n| 1 | 2 |\n\n---\nafter ||secret|| `code` ~~gone~~ \\*star\\*',
    );
    expect(
      shown(tester),
      allOf(
        contains('Title'),
        contains('quoted bold'),
        contains('3.'),
        contains('4.'),
      ),
    );
    expect(find.byType(Table), findsOneWidget);
    expect(find.byType(Divider), findsOneWidget);
    expect(rich('*star*'), findsOneWidget);
    // The spoiler is hidden until the reader selects it.
    final hidden = find.byWidgetPredicate(
      (w) => w is Opacity && w.opacity == 0,
    );
    expect(hidden, findsOneWidget);
    await tester.tap(hidden);
    await tester.pump();
    expect(hidden, findsNothing);
  });

  testWidgets(
    'a code block is highlighted with the theme tokens; an unknown language is plain',
    (tester) async {
      await show(
        tester,
        '```go\nfunc main() { return 42 }\n```\n\n```nosuch\nfunc main()\n```',
      );
      final blocks = tester.widgetList<RichText>(rich('func main()')).toList();
      expect(blocks, hasLength(2));
      final c = ChurrColors.resolve(dark: true);
      Set<Color?> colours(RichText r) {
        final out = <Color?>{};
        r.text.visitChildren((s) {
          if (s is TextSpan && s.text != null) out.add(s.style?.color);
          return true;
        });
        return out;
      }

      expect(
        colours(blocks[0]),
        containsAll([c.pink, c.cyan]),
        reason: 'keywords pink, numbers cyan',
      );
      expect(
        colours(blocks[1]).difference({null, c.ink}),
        isEmpty,
        reason: 'no language is guessed',
      );
      expect(find.byTooltip('Copy code'), findsNWidgets(2));
    },
  );

  test('the grammars are exactly the format\'s language list', () {
    // `plaintext` is plain on purpose. `zig` has no grammar in the
    // package yet: recorded in README.md.
    expect(
      grammarLanguages,
      highlightLanguages.keys.toSet().difference({'plaintext', 'zig'}),
    );
    expect(canonicalLanguage('GoLang'), 'go');
    expect(canonicalLanguage('c#'), 'csharp');
    expect(canonicalLanguage('brainfuck'), '');
    final c = ChurrColors.resolve(dark: true);
    for (final lang in grammarLanguages) {
      expect(
        highlightCode('x = 1 // a\n"s"', lang, c),
        isNotEmpty,
        reason: lang,
      );
    }
  });

  test('token names map to the colours of CLIENTS.md section 12', () {
    final c = ChurrColors.resolve(dark: false);
    expect(tokenColor('keyword', c), c.pink);
    expect(tokenColor('string', c), c.yellowInk);
    expect(tokenColor('number', c), c.cyan);
    expect(tokenColor('title.function', c), c.lavender);
    expect(tokenColor('comment', c), c.dim);
    expect(tokenColor('punctuation', c), isNull);
  });
}
