// A message link (ruling R73) is drawn as a compact quote of the message it
// names, as the reader may see it, and goes to it when selected. One the
// reader may not see is "A message you can't see".

import 'package:churr/core/models/conversation.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/l10n/app_localizations.dart';
import 'package:churr/ui/message/markdown_view.dart';
import 'package:churr/ui/message/message_link_view.dart';
import 'package:churr/ui/theme/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

const conv = '0193a000-0000-7000-8000-0000000000a1';
const msg = '0193a000-0000-7000-8000-0000000000b1';
const link = 'https://chat.example/chat/c/$conv/m/$msg';

void main() {
  Future<List<String>> show(
    WidgetTester tester,
    Size size,
    Future<Message> Function(String id) fetch,
  ) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final opened = <String>[];
    final linked = LinkedMessages(fetch);
    await tester.pumpWidget(
      MaterialApp(
        theme: buildTheme(dark: false),
        localizationsDelegates: L10n.localizationsDelegates,
        home: Scaffold(
          body: MarkdownView(
            text: 'see $link',
            entities: [
              Entity(
                type: 'message_link',
                at: 4,
                len: link.length,
                url: link,
                conversationId: conv,
                messageId: msg,
              ),
            ],
            context_: MarkdownContext(
              messageLink: (e, typed) => MessageLinkView(
                entity: e,
                typed: typed,
                linked: linked,
                nameOf: (id) => id == 'sam' ? 'Sam' : null,
                conversationOf: (id) => id == conv
                    ? Conversation(
                        id: conv,
                        kind: 'channel',
                        private: true,
                        name: 'plans',
                      )
                    : null,
                onOpen: (c, m, parent) => opened.add('$c/$m/$parent'),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    return opened;
  }

  Message message({String? deletedAt}) => Message.fromJson({
    'id': msg,
    'conversation_id': conv,
    'author_id': 'sam',
    'text': 'the plan is ready\nsecond line',
    'created_at': '2026-11-16T09:30:00.000Z',
    'deleted_at': ?deletedAt,
  });

  for (final size in [const Size(390, 800), const Size(1440, 900)]) {
    testWidgets('a message the reader may see, ${size.width.toInt()} wide', (
      tester,
    ) async {
      final opened = await show(tester, size, (_) async => message());
      expect(find.textContaining('the plan is ready'), findsOneWidget);
      expect(find.textContaining('Sam'), findsOneWidget);
      expect(find.textContaining('#plans'), findsOneWidget);
      final view = find.byKey(const ValueKey('message-link-4'));
      expect(
        tester.getSemantics(view).label,
        contains('Link to a message ($link)'),
      );
      await tester.tap(find.textContaining('the plan is ready'));
      expect(opened, ['$conv/$msg/null']);
    });
  }

  testWidgets('a message the reader may not see', (tester) async {
    await show(
      tester,
      const Size(390, 800),
      (_) async => throw Exception('not found'),
    );
    expect(find.text('A message you can’t see'), findsOneWidget);
    expect(find.textContaining('the plan'), findsNothing);
  });

  testWidgets('a deleted message shows as deleted', (tester) async {
    await show(
      tester,
      const Size(390, 800),
      (_) async => message(deletedAt: '2026-11-16T10:00:00.000Z'),
    );
    expect(find.text('This message was deleted.'), findsOneWidget);
  });

  testWidgets('without a builder it is drawn as typed', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: buildTheme(dark: false),
        home: Scaffold(
          body: MarkdownView(
            text: link,
            entities: [
              Entity(
                type: 'message_link',
                at: 0,
                len: link.length,
                url: link,
                conversationId: conv,
                messageId: msg,
              ),
            ],
          ),
        ),
      ),
    );
    expect(find.text(link), findsOneWidget);
  });
}
