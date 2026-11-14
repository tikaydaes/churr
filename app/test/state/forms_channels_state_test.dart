import 'package:churr/core/db/app_db.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/core/session_store.dart';
import 'package:churr/state/app_state.dart';
import 'package:churr/state/channel_actions.dart';
import 'package:churr/state/channel_detail.dart';
import 'package:churr/state/form_actions.dart';
import 'package:churr/state/quick_switcher.dart';
import 'package:churr/state/unified_search.dart';
import 'package:churr/ui/screens/channels/browse_screen.dart';
import 'package:drift/native.dart';
import 'package:flutter_test/flutter_test.dart';

import '../forms_support.dart';
import '../ui/shell_test.dart' show MemoryStorage;

Message pollMessage({Map<String, dynamic>? mine}) => Message.fromJson({
  'id': 'm1',
  'conversation_id': 'c1',
  'seq': 10,
  'author_id': 'sam',
  'text': '',
  'created_at': '2026-11-13T09:30:00.000Z',
  'form': {
    'id': 'f1',
    'preset': 'poll',
    'title': 'Lunch?',
    'fields': [
      {
        'id': 'choice',
        'type': 'choice',
        'label': 'Lunch?',
        'options': [
          {'id': 'a', 'label': 'Noodles'},
          {'id': 'b', 'label': 'Tacos'},
        ],
      },
    ],
    'action': {'type': 'tally'},
    'results': 'live',
    'answer_count': 1,
    'tally': {
      'choice': {'a': 1, 'b': 0},
    },
    'my_answer': ?mine,
  },
});

void main() {
  quietDrift();
  group('form actions', () {
    test(
      'an answer shows at once, then the server\'s form replaces it',
      () async {
        final fake = FakeServer((r) {
          if (r.method == 'PUT' && r.url.path.endsWith('/form/answer')) {
            return (
              200,
              {
                'form': {
                  ...pollMessage().form!,
                  'answer_count': 2,
                  'tally': {
                    'choice': {'a': 1, 'b': 1},
                  },
                  'my_answer': {
                    'choice': ['b'],
                  },
                },
              },
            );
          }
          return null;
        });
        final s = await fakeServerState(
          fake,
          conversations: [channel('c1', 'General')],
        );
        await s.db.upsertMessages([pollMessage()]);
        await FormActions(s).answer(pollMessage(), {
          'choice': ['b'],
        });
        expect(body(fake.requests.single), {
          'answers': {
            'choice': ['b'],
          },
        });
        final m = await s.db.message('m1');
        expect(m!.form!['answer_count'], 2);
        expect(m.form!['my_answer'], {
          'choice': ['b'],
        });
      },
    );

    test('a refusal puts the earlier form back', () async {
      final fake = FakeServer(
        (r) => (
          409,
          {
            'error': {'code': 'conflict', 'message': 'Closed.'},
          },
        ),
      );
      final s = await fakeServerState(
        fake,
        conversations: [channel('c1', 'General')],
      );
      await s.db.upsertMessages([pollMessage()]);
      await expectLater(
        FormActions(s).answer(pollMessage(), {
          'choice': ['a'],
        }),
        throwsA(anything),
      );
      expect((await s.db.message('m1'))!.form!['my_answer'], isNull);
    });

    test('who may read results and close', () async {
      final s = await fakeServerState(
        FakeServer((_) => null),
        conversations: [channel('c1', 'General')],
      );
      final m = pollMessage();
      // A named poll whose tally is shown: anyone may list it.
      expect(FormActions(s).canReadResults(m), isTrue);
      // Not the author, not a manager.
      expect(FormActions(s).canClose(m), isFalse);
      final anon = Message.fromJson({
        ...m.raw,
        'form': {...m.form!, 'anonymous': true},
      });
      expect(FormActions(s).canReadResults(anon), isFalse);
    });

    test('posting sends the form with a client id', () async {
      final fake = FakeServer(
        (r) => (
          201,
          {
            'message': {...pollMessage().raw, 'seq': 11},
          },
        ),
      );
      final s = await fakeServerState(
        fake,
        conversations: [channel('c1', 'General')],
      );
      await FormActions(s).post('c1', {'preset': 'poll'});
      final b = body(fake.requests.single);
      expect(b['form'], {'preset': 'poll'});
      expect(b['client_id'], isA<String>());
      expect(await s.db.message('m1'), isNotNull);
    });
  });

  group('channel actions', () {
    test(
      'a created channel is kept at its newest event and leaving forgets it',
      () async {
        final fake = FakeServer((r) {
          if (r.url.path.endsWith('/channels')) {
            return (
              201,
              {
                'conversation': channel(
                  'new',
                  'Design reviews',
                  level: 'manager',
                ).toJson(),
              },
            );
          }
          if (r.url.path.endsWith('/leave')) return (204, null);
          return null;
        });
        final s = await fakeServerState(fake);
        final c = await ChannelActions(s).create(
          name: 'Design reviews',
          private: false,
          parentId: 'p1',
          sectionId: 's1',
        );
        expect(c.id, 'new');
        final sent = body(fake.requests.single);
        expect(sent['parent_id'], 'p1');
        // A sub-channel takes its parent's section, so none is sent.
        expect(sent.containsKey('section_id'), isFalse);
        expect((await s.db.conversation('new'))!.syncedSeq, 10);
        await ChannelActions(s).leave('new');
        expect(await s.db.conversation('new'), isNull);
      },
    );

    test('channel names follow the free-form rule', () {
      expect(checkChannelName('Design reviews'), isNull);
      expect(checkChannelName('デザイン'), isNull);
      expect(checkChannelName(''), isNotNull);
      expect(checkChannelName(' lead'), isNotNull);
      expect(checkChannelName('a\u0007b'), isNotNull);
      expect(checkChannelName('x' * 65), isNotNull);
    });

    test('parent choices and refusals', () async {
      final s = await fakeServerState(
        FakeServer((_) => null),
        conversations: [
          channel('design', 'Design', level: 'manager'),
          channel('icons', 'Icons', parent: 'design'),
          channel('general', 'General'),
        ],
      );
      final a = ChannelActions(s);
      final design = s.conversation('design')!;
      expect(a.parentRefusal(design), contains('sub-channels'));
      expect(a.parentChoices(design), isEmpty);
      final general = s.conversation('general')!;
      expect([for (final c in a.parentChoices(general)) c.id], ['design']);
    });
  });

  group('names and detail', () {
    test('matching ignores case and accents and takes the parent', () {
      expect(nameMatches('design', 'Design reviews'), isTrue);
      expect(nameMatches('rev', 'Design reviews'), isTrue);
      expect(nameMatches('cafe', 'Café'), isTrue);
      expect(
        nameMatches('design icons', 'Icons', parentName: 'Design'),
        isTrue,
      );
      expect(
        nameMatches('marketing icons', 'Icons', parentName: 'Design'),
        isFalse,
      );
    });

    test('the detail line is in the fixed order', () async {
      final s = await fakeServerState(
        FakeServer((_) => null),
        name: "Friends' server",
        conversations: [
          channel('design', 'design', section: 's1'),
          channel('icons', 'Icons', parent: 'design', members: 14),
        ],
      );
      expect(
        channelDetail(
          s,
          s.conversation('icons')!,
          showServer: true,
          managerIds: ['sam', 'lee', 'me'],
        ),
        "Friends' server · design › · Projects · 14 members, you're in it · managed by Sam Okafor, Lee · since March 2026",
      );
    });

    test('the browser lists a tree, same names together, joined first', () {
      final tree = browseTree([
        channel('b2', 'Books', member: false),
        channel('sub', 'Sci-fi', parent: 'b1'),
        channel('b1', 'books'),
        channel('a', 'Art'),
      ]);
      expect(
        [for (final (c, d) in tree) '${c.id}:$d'],
        ['a:0', 'b1:0', 'sub:1', 'b2:0'],
      );
    });
  });

  group('across servers', () {
    Future<AppState> app(List<(FakeServer, String)> servers) async {
      final storage = MemoryStorage();
      final appDb = AppDb(NativeDatabase.memory());
      final secrets = MemorySecretStore();
      final state = AppState(
        appDb: appDb,
        storage: storage,
        secrets: secrets,
        connectOnStart: false,
      );
      for (var i = 0; i < servers.length; i++) {
        final s = await fakeServerState(
          servers[i].$1,
          id: 'server-$i',
          name: servers[i].$2,
          conversations: [channel('c$i', i == 0 ? 'General' : 'Garden')],
        );
        state.servers.add(s);
      }
      return state;
    }

    Map<String, dynamic> hit(String id, String conv, String at) => {
      'message': {
        'id': id,
        'conversation_id': conv,
        'seq': 1,
        'author_id': 'sam',
        'text': 'deploy $id',
        'created_at': at,
      },
      'snippet': 'deploy',
    };

    test(
      'search asks every server, merges newest first, and names one that fails',
      () async {
        final a = FakeServer(
          (r) => (
            200,
            {
              'results': [hit('a1', 'c0', '2026-11-10T10:00:00.000Z')],
              'next_cursor': 'more',
            },
          ),
        );
        final b = FakeServer(
          (r) => (
            200,
            {
              'results': [hit('b1', 'c1', '2026-11-12T10:00:00.000Z')],
            },
          ),
        );
        final broken = FakeServer(
          (r) => (
            503,
            {
              'error': {'code': 'unavailable', 'message': 'Down.'},
            },
          ),
        );
        final state = await app([
          (a, 'Work'),
          (b, 'Friends'),
          (broken, 'Club'),
        ]);
        final search = UnifiedSearch(state)
          ..text = 'deploy from:@sam since:2026-11-01 has:file';
        await search.run();
        expect([for (final h in search.results) h.message.id], ['b1', 'a1']);
        expect(search.failures.keys, ['server-2']);
        expect(search.hasMore, isTrue);
        final q = a.requests.single.url.queryParameters;
        expect(q['q'], 'deploy');
        expect(q['from'], 'sam');
        expect(q['has_file'], 'true');
        expect(DateTime.parse(q['since']!), DateTime(2026, 11, 1).toUtc());
      },
    );

    test('a conversation filter asks only its server', () async {
      final a = FakeServer((r) => (200, {'results': <Object>[]}));
      final b = FakeServer((r) => (200, {'results': <Object>[]}));
      final state = await app([(a, 'Work'), (b, 'Friends')]);
      final search = UnifiedSearch(
        state,
        inConversation: const ServerPick('server-1', 'c1', 'Garden'),
      )..text = 'x';
      await search.run();
      expect(a.requests, isEmpty);
      expect(b.requests.single.url.queryParameters['in'], 'c1');
    });

    test('the quick switcher looks through every server', () async {
      final state = await app([
        (FakeServer((_) => null), 'Work'),
        (FakeServer((_) => null), 'Friends'),
      ]);
      final qs = QuickSwitcher(state)..setQuery('ga');
      final hits = qs.results;
      expect([for (final h in hits) h.conversation?.id], ['c1']);
      expect(hits.single.server.name, 'Friends');
      qs.setQuery('sam');
      expect(qs.results.where((h) => h.account?.id == 'sam').length, 2);
      qs.dispose();
    });
  });
}
