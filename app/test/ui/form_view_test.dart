import 'package:churr/core/models/message.dart';
import 'package:churr/state/server_state.dart';
import 'package:churr/ui/message/form_view.dart';
import 'package:churr/ui/message/markdown_view.dart';
import 'package:churr/ui/message/message_tile.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../forms_support.dart';

Map<String, dynamic> pollForm({Map<String, dynamic>? extra}) => {
  'id': 'f1',
  'preset': 'poll',
  'title': 'Lunch on Friday?',
  'fields': [
    {
      'id': 'choice',
      'type': 'choice',
      'label': 'Lunch on Friday?',
      'options': [
        {'id': 'a', 'label': 'Noodles'},
        {'id': 'b', 'label': 'Tacos'},
      ],
    },
  ],
  'action': {'type': 'tally'},
  'results': 'live',
  'answer_count': 3,
  'tally': {
    'choice': {'a': 2, 'b': 1},
  },
  'voters': {
    'choice': {
      'a': ['sam', 'lee'],
      'b': ['me'],
    },
  },
  'my_answer': {
    'choice': ['b'],
  },
  ...?extra,
};

Message msg(
  Map<String, dynamic> form, {
  String conv = 'c1',
  String fallback = 'Lunch on Friday?\nNoodles\nTacos',
}) => Message.fromJson({
  'id': 'm1',
  'conversation_id': conv,
  'seq': 10,
  'author_id': 'sam',
  'text': '',
  'fallback': fallback,
  'created_at': '2026-11-13T09:30:00.000Z',
  'form': form,
});

void main() {
  quietDrift();

  Future<(ServerState, FakeServer)> setUp(
    WidgetTester tester, {
    String level = 'member',
    bool member = true,
    (int, Object?)? Function(dynamic r)? answer,
  }) async {
    final fake = FakeServer(
      (r) =>
          answer?.call(r) ??
          (
            200,
            {
              'form': pollForm(
                extra: {
                  'my_answer': {
                    'choice': ['a'],
                  },
                },
              ),
            },
          ),
    );
    late ServerState s;
    await tester.runAsync(() async {
      s = await fakeServerState(
        fake,
        conversations: [channel('c1', 'General', level: level, member: member)],
      );
    });
    return (s, fake);
  }

  for (final size in [phone, desktop]) {
    final label = size == phone ? 'phone' : 'desktop';

    testWidgets(
      'a poll draws bars, counts and voters, and answers on tap ($label)',
      (tester) async {
        final (s, fake) = await setUp(tester);
        await tester.runAsync(() => s.db.upsertMessages([msg(pollForm())]));
        await pumpScreen(
          tester,
          appWith([s]),
          FormView(server: s, message: msg(pollForm())),
          size: size,
        );
        expect(find.text('Lunch on Friday?'), findsOneWidget);
        expect(find.text('Noodles'), findsOneWidget);
        expect(find.text('2'), findsOneWidget);
        expect(find.textContaining('3 answers'), findsOneWidget);
        expect(
          find.bySemanticsLabel(RegExp('Noodles, 2 votes, Sam Okafor, Lee')),
          findsOneWidget,
        );
        expect(find.byKey(const Key('form-withdraw')), findsOneWidget);
        await tester.tap(find.byKey(const Key('form-option-a')));
        await settleAsync(tester);
        expect(fake.requests.single.method, 'PUT');
        expect(body(fake.requests.single), {
          'answers': {
            'choice': ['a'],
          },
        });
      },
    );

    testWidgets(
      'a form this app cannot draw shows the fallback and says so ($label)',
      (tester) async {
        final (s, _) = await setUp(tester);
        final form = pollForm(
          extra: {
            'fields': [
              {'id': 'x', 'type': 'colour', 'label': 'Favourite colour'},
            ],
          },
        );
        await pumpScreen(
          tester,
          appWith([s]),
          FormView(server: s, message: msg(form)),
          size: size,
        );
        expect(find.byKey(const Key('form-too-old')), findsOneWidget);
        expect(find.textContaining('Noodles'), findsOneWidget);
      },
    );

    testWidgets('other fields are filled in a dialog ($label)', (tester) async {
      final (s, fake) = await setUp(
        tester,
        answer: (_) => (200, {'form': <String, dynamic>{}}),
      );
      final form = {
        'id': 'f2',
        'title': 'Kit order',
        'fields': [
          {
            'id': 'size',
            'type': 'text',
            'label': 'Shirt size',
            'required': true,
          },
          {'id': 'agree', 'type': 'checkbox', 'label': 'I will pay'},
        ],
        'action': {'type': 'collect'},
        'answer_count': 0,
      };
      await tester.runAsync(() => s.db.upsertMessages([msg(form)]));
      await pumpScreen(
        tester,
        appWith([s]),
        FormView(server: s, message: msg(form)),
        size: size,
      );
      expect(find.textContaining('answers go to the author'), findsOneWidget);
      await tester.tap(find.byKey(const Key('form-answer')));
      await tester.pumpAndSettle();
      // Required and empty: refused before sending.
      await tester.tap(find.byKey(const Key('answer-send')));
      await tester.pump();
      expect(find.text('Required.'), findsOneWidget);
      await tester.enterText(find.byKey(const Key('field-size')), 'M');
      await tester.tap(find.byKey(const Key('field-agree')));
      await tester.pump();
      await tester.tap(find.byKey(const Key('answer-send')));
      await settleAsync(tester);
      expect(body(fake.requests.single), {
        'answers': {'size': 'M', 'agree': true},
      });
    });

    testWidgets(
      'a closed form takes no answers; a reader who has not joined is told to join ($label)',
      (tester) async {
        final (s, fake) = await setUp(tester, member: false);
        final closed = pollForm(
          extra: {'closed_at': '2026-11-13T10:00:00.000Z'},
        );
        await pumpScreen(
          tester,
          appWith([s]),
          FormView(server: s, message: msg(closed)),
          size: size,
        );
        expect(find.textContaining('closed'), findsOneWidget);
        await tester.tap(find.byKey(const Key('form-option-a')));
        await settleAsync(tester);
        expect(fake.requests, isEmpty);
        await pumpScreen(
          tester,
          appWith([s]),
          FormView(server: s, message: msg(pollForm())),
          size: size,
        );
        expect(find.textContaining('Join to take part'), findsOneWidget);
      },
    );
  }

  testWidgets(
    'results hidden until close show no tally; approve buttons press',
    (tester) async {
      final (s, fake) = await setUp(
        tester,
        answer: (_) => (200, {'form': <String, dynamic>{}}),
      );
      final approve = {
        'id': 'f3',
        'preset': 'approve',
        'title': 'Ship 2.0',
        'fields': <Object>[],
        'buttons': [
          {'id': 'approve', 'label': 'Approve', 'style': 'primary'},
          {'id': 'decline', 'label': 'Decline', 'style': 'danger'},
        ],
        'action': {'type': 'tally'},
        'results': 'after_close',
        'answer_count': 4,
      };
      await tester.runAsync(() => s.db.upsertMessages([msg(approve)]));
      await pumpScreen(
        tester,
        appWith([s]),
        FormView(server: s, message: msg(approve)),
      );
      expect(find.textContaining('results when it closes'), findsOneWidget);
      await tester.tap(find.byKey(const Key('form-button-approve')));
      await settleAsync(tester);
      expect(fake.requests.single.url.path, endsWith('/form/buttons/approve'));
    },
  );

  testWidgets('more than five options become a menu', (tester) async {
    final (s, _) = await setUp(tester);
    final form = pollForm(
      extra: {
        'fields': [
          {
            'id': 'choice',
            'type': 'choice',
            'label': 'Day',
            'options': [
              for (final d in ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'])
                {'id': d.toLowerCase(), 'label': d},
            ],
          },
        ],
        'tally': null,
        'voters': null,
        'my_answer': null,
      },
    );
    await pumpScreen(
      tester,
      appWith([s]),
      FormView(server: s, message: msg(form)),
    );
    expect(find.byKey(const Key('form-menu')), findsOneWidget);
    expect(find.text('Choose…'), findsOneWidget);
  });

  group('checklists', () {
    Message checklist() => Message.fromJson({
      'id': 'm2',
      'conversation_id': 'c1',
      'seq': 11,
      'author_id': 'sam',
      'text': '- [ ] Book the room\n- [x] Order food',
      'entities': [
        {'type': 'task', 'at': 6, 'len': 13, 'item_id': 't1'},
        {'type': 'task', 'at': 26, 'len': 10, 'item_id': 't2'},
      ],
      'tasks': [
        {'id': 't1', 'checked': false},
        {
          'id': 't2',
          'checked': true,
          'checked_by_id': 'sam',
          'checked_at': '2026-11-13T09:45:00.000Z',
        },
      ],
      'created_at': '2026-11-13T09:30:00.000Z',
    });

    Future<void> draw(
      WidgetTester tester,
      ServerState s, {
      Size size = desktop,
    }) async {
      await pumpScreen(
        tester,
        appWith([s]),
        MessageTile(
          server: s,
          message: checklist(),
          markdown: MarkdownContext(
            accounts: s.accounts,
            onToggleTask: (_, _) {},
          ),
        ),
        size: size,
      );
    }

    for (final size in [phone, desktop]) {
      testWidgets(
        'a member ticks; a done item shows who and when (${size == phone ? 'phone' : 'desktop'})',
        (tester) async {
          final (s, _) = await setUp(tester);
          await draw(tester, s, size: size);
          final boxes = tester
              .widgetList<Checkbox>(find.byType(Checkbox))
              .toList();
          expect(boxes.length, 2);
          expect(boxes.every((b) => b.onChanged != null), isTrue);
          expect(
            find.textContaining('Sam Okafor ·', findRichText: true),
            findsOneWidget,
          );
        },
      );
    }

    testWidgets('a reader cannot tick', (tester) async {
      final (s, _) = await setUp(tester, level: 'reader');
      await draw(tester, s);
      final boxes = tester.widgetList<Checkbox>(find.byType(Checkbox)).toList();
      expect(boxes.every((b) => b.onChanged == null), isTrue);
    });

    testWidgets('an item\'s history lists every tick and untick', (
      tester,
    ) async {
      final (s, fake) = await setUp(
        tester,
        answer: (_) => (
          200,
          {
            'changes': [
              {
                'item_id': 't2',
                'account_id': 'lee',
                'checked': true,
                'at': '2026-11-13T09:40:00.000Z',
              },
              {
                'item_id': 't2',
                'account_id': 'lee',
                'checked': false,
                'at': '2026-11-13T09:41:00.000Z',
              },
              {
                'item_id': 't2',
                'account_id': 'sam',
                'checked': true,
                'at': '2026-11-13T09:45:00.000Z',
              },
            ],
          },
        ),
      );
      await draw(tester, s);
      await tester.tap(find.byKey(const Key('task-history-t2')));
      await settleAsync(tester);
      expect(fake.requests.single.url.path, endsWith('/checklist/t2/history'));
      expect(find.text('Lee ticked it'), findsOneWidget);
      expect(find.text('Lee unticked it'), findsOneWidget);
      expect(find.text('Sam Okafor ticked it'), findsOneWidget);
    });
  });
}
