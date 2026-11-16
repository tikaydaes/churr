import 'dart:convert';

import 'package:churr/core/db/account_tables.dart';
import 'package:churr/core/models/account.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/core/models/personal.dart';
import 'package:churr/state/account_hub.dart';
import 'package:churr/state/account_prefs.dart';
import 'package:churr/state/app_prefs.dart';
import 'package:churr/state/device_setup.dart';
import 'package:churr/state/server_order.dart';
import 'package:churr/state/personal_sidebar.dart';
import 'package:churr/state/notifications_controller.dart';
import 'package:churr/state/threads_bookmarks.dart';
import 'package:churr/core/setup_code.dart';
import 'package:http/http.dart' as http;
import 'package:flutter_test/flutter_test.dart';

import '../account_support.dart';

Message m(
  String id,
  String conv,
  int seq,
  DateTime at, {
  String? parent,
  String author = 'sam',
}) => Message.fromJson({
  'id': id,
  'conversation_id': conv,
  'seq': seq,
  'author_id': author,
  'parent_id': ?parent,
  'text': 'text of $id',
  'created_at': at.toUtc().toIso8601String(),
});

Future<void> pause() => Future<void>.delayed(const Duration(milliseconds: 30));

void main() {
  group('status and do not disturb set once', () {
    test(
      'goes to every server taking part, and not to one opted out',
      () async {
        final (app, fakes) = await makeAccountApp(null, servers: 3);
        final hub = AccountHub.of(app);
        for (final f in fakes) {
          f.on('PUT /api/v1/me/status', {
            'account': meJson(
              extra: {
                'status': {
                  'text': 'Lunch',
                  'pause_notifications': false,
                  'appear_away': true,
                },
              },
            ),
          });
        }
        app.prefs.setTakesPartInShared('https://s2.example.org', false);
        final r = await hub.setStatus(
          const FullStatus(text: 'Lunch', appearAway: true),
        );
        expect(r.allDone, isTrue);
        expect(fakes[0].calls, contains('PUT /api/v1/me/status'));
        expect(fakes[1].calls, contains('PUT /api/v1/me/status'));
        expect(fakes[2].calls, isEmpty, reason: 'opted out');
        final body = jsonDecode(
          fakes[0].requests.firstWhere((q) => q.method == 'PUT').body,
        ) as Map;
        expect(body['text'], 'Lunch');
        expect(body['appear_away'], isTrue);
        await pause();
        expect(app.servers[0].me!.fullStatus!.text, 'Lunch');
      },
    );

    test('a server that cannot be reached is sent the change when it next connects', () async {
      final (app, fakes) = await makeAccountApp(null);
      final hub = AccountHub.of(app);
      fakes[1].down = true;
      final r = await hub.setDnd(null);
      expect(r.waiting, ['Friends']);
      expect(await hub.hasPending(app.servers[1]), isTrue);
      fakes[1].down = false;
      await hub.flushPending(app.servers[1]);
      expect(fakes[1].calls, contains('PUT /api/v1/me/dnd'));
      expect(await hub.hasPending(app.servers[1]), isFalse);
    });

    test('a later change replaces a waiting one of the same kind', () async {
      final (app, fakes) = await makeAccountApp(null);
      final hub = AccountHub.of(app);
      fakes[1].down = true;
      await hub.setDnd(null);
      await hub.clearDnd();
      fakes[1].down = false;
      await hub.flushPending(app.servers[1]);
      expect(fakes[1].calls.where((c) => c.contains('/me/dnd')), [
        'DELETE /api/v1/me/dnd',
      ]);
    });

    test('a refusal is named by server', () async {
      final (app, fakes) = await makeAccountApp(null);
      fakes[1].on('PUT /api/v1/me/status', {
        'error': {'code': 'invalid_value', 'message': 'Too long'},
      }, status: 400);
      final r = await AccountHub.of(app).setStatus(const FullStatus(text: 'x'));
      expect(r.refused.keys, ['Friends']);
    });

    test(
      '"differs on" names the server whose status is not the first one\'s',
      () async {
        final (app, _) = await makeAccountApp(null);
        await app.servers[1].db.putMe(
          Me.fromJson(
            meJson(
              extra: {
                'status': {
                  'text': 'Elsewhere',
                  'pause_notifications': false,
                  'appear_away': false,
                },
              },
            ),
          ),
        );
        await pause();
        final hub = AccountHub.of(app);
        expect(hub.statusDiffers().map((s) => s.name), ['Friends']);
        app.prefs.setTakesPartInShared('https://s1.example.org', false);
        expect(
          hub.statusDiffers(),
          isEmpty,
          reason: 'an opted-out server is its own',
        );
      },
    );
  });

  group('muting a whole server', () {
    test(
      'turns do not disturb on there alone and leaves it out of shared changes',
      () async {
        final (app, fakes) = await makeAccountApp(null);
        final hub = AccountHub.of(app);
        await hub.muteServer(
          app.servers[1],
          DateTime.now().add(const Duration(hours: 1)),
        );
        expect(fakes[1].calls, contains('PUT /api/v1/me/dnd'));
        expect(fakes[0].calls, isNot(contains('PUT /api/v1/me/dnd')));
        expect(hub.isMuted(app.servers[1]), isTrue);
        await hub.setDnd(null);
        expect(
          fakes[1].calls.where((c) => c == 'PUT /api/v1/me/dnd').length,
          1,
          reason: 'muted servers are left out',
        );
        await hub.unmuteServer(app.servers[1]);
        expect(fakes[1].calls, contains('DELETE /api/v1/me/dnd'));
        expect(hub.isMuted(app.servers[1]), isFalse);
      },
    );
  });

  group('profile on chosen servers', () {
    test('changes the ticked servers and names one that refuses', () async {
      final (app, fakes) = await makeAccountApp(null, servers: 3);
      fakes[1].on('PATCH /api/v1/me', {
        'error': {'code': 'invalid_value', 'message': 'Not allowed here'},
      }, status: 400);
      fakes[0].on('PATCH /api/v1/me', {
        'account': meJson(extra: {'display_name': 'Kai T'}),
      });
      final refused = await AccountHub.of(app).applyProfile({
        'display_name': 'Kai T',
      }, to: app.servers.take(2).toList());
      expect(refused, {'Friends': 'Not allowed here'});
      expect(fakes[2].calls, isEmpty);
      await pause();
      expect(app.servers[0].me!.displayName, 'Kai T');
    });
  });

  group('blocking', () {
    test('writes the database first and then the server', () async {
      final (app, fakes) = await makeAccountApp(null);
      await AccountHub.of(app).setBlocked(app.servers[0], 'sam', true);
      expect((await app.servers[0].db.me())!.blockedIds, ['sam']);
      expect(fakes[0].calls, ['PUT /api/v1/me/blocks/sam']);
    });
  });

  group('the notifications list', () {
    test('merges servers newest first, folds a burst from one conversation, and filters', () async {
      final (app, _) = await makeAccountApp(null);
      final t0 = DateTime.utc(2026, 11, 14, 9);
      await app.servers[0].db.addNotification(
        m('w1', 'general', 21, t0),
        'notify',
      );
      await app.servers[0].db.addNotification(
        m('w2', 'general', 22, t0.add(const Duration(minutes: 1))),
        'notify',
      );
      await app.servers[1].db.addNotification(
        m('f1', 'design', 30, t0.add(const Duration(minutes: 5))),
        'badge',
      );
      // A hidden conversation's items are here like any other.
      await app.servers[1].db.addNotification(
        m('h1', 'secret', 40, t0.subtract(const Duration(hours: 1))),
        'notify',
      );
      final c = NotificationsController(app)..start();
      await pause();
      final unread = c.unread;
      expect([for (final e in unread) e.newest.messageId], ['f1', 'w2', 'h1']);
      expect(unread[1].count, 2);
      expect(
        unread.map((e) => e.key).toSet().length,
        3,
        reason: 'keys include the server',
      );
      c.setServerFilter('server-0');
      expect([for (final e in c.unread) e.newest.messageId], ['w2']);
      c.dispose();
    });

    test(
      'mark all read moves each read position and the items go to Earlier',
      () async {
        final (app, fakes) = await makeAccountApp(null);
        for (final f in fakes) {
          f.on('PUT /api/v1/conversations/general/read', {
            'membership': {
              'level': 'member',
              'read_seq': 25,
              'mode': 'mentions',
            },
          });
        }
        final t0 = DateTime.utc(2026, 11, 14, 9);
        await app.servers[0].db.addNotification(
          m('w1', 'general', 25, t0),
          'notify',
        );
        final c = NotificationsController(app)..start();
        await pause();
        await c.markAllRead();
        await pause();
        expect(c.unread, isEmpty);
        expect(c.earlier.single.newest.messageId, 'w1');
        expect(
          fakes[0].calls,
          contains('PUT /api/v1/conversations/general/read'),
        );
        c.dispose();
      },
    );

    test('opening an item opens its message in place', () async {
      final (app, _) = await makeAccountApp(null);
      await app.servers[1].db.addNotification(
        m('r1', 'design', 30, DateTime.utc(2026, 11, 14), parent: 'p1'),
        'notify',
      );
      final c = NotificationsController(app)..start();
      await pause();
      await c.open(c.unread.single);
      expect(app.open?.serverId, 'server-1');
      expect(app.open?.conversationId, 'design');
      expect(app.focusMessageId, 'r1');
      expect(app.focusThreadId, 'p1');
      c.dispose();
    });
  });

  group('threads and bookmarks', () {
    test(
      'threads with unread replies, newest reply first; read ones on request',
      () async {
        final (app, fakes) = await makeAccountApp(null, servers: 1);
        final db = app.servers[0].db;
        Message parent(String id, DateTime last) => Message.fromJson({
          ...m(id, 'general', 5, DateTime.utc(2026, 11, 1)).raw,
          'thread': {
            'reply_count': 2,
            'last_reply_seq': 9,
            'last_reply_at': last.toIso8601String(),
            'participant_ids': [],
          },
        });
        await db.putThreadParents([
          parent('p-old', DateTime.utc(2026, 11, 2)),
          parent('p-new', DateTime.utc(2026, 11, 3)),
          parent('p-read', DateTime.utc(2026, 11, 4)),
        ]);
        await db.upsertThreadStates([
          ThreadState(
            parentId: 'p-old',
            conversationId: 'general',
            state: 'following',
            readSeq: 5,
            unreadCount: 1,
          ),
          ThreadState(
            parentId: 'p-new',
            conversationId: 'general',
            state: 'following',
            readSeq: 5,
            unreadCount: 2,
          ),
          ThreadState(
            parentId: 'p-read',
            conversationId: 'general',
            state: 'following',
            readSeq: 9,
            unreadCount: 0,
          ),
        ]);
        final c = ThreadsController(app)..start();
        await pause();
        expect([for (final t in c.items) t.parentId], ['p-new', 'p-old']);
        c.setShowRead(true);
        expect(c.items.first.parentId, 'p-read');
        fakes[0].on('PUT /api/v1/messages/p-new/thread/read', {
          'thread_state': {
            'parent_id': 'p-new',
            'conversation_id': 'general',
            'state': 'following',
            'read_seq': 9,
            'unread_count': 0,
          },
        });
        c.setShowRead(false);
        await c.markRead(c.items.first);
        await pause();
        expect([for (final t in c.items) t.parentId], ['p-old']);
        c.dispose();
      },
    );

    test('bookmarks newest first; removing writes the database first', () async {
      final (app, fakes) = await makeAccountApp(null);
      await app.servers[0].db.putBookmark(
        'm1',
        DateTime.utc(2026, 11, 1),
        message: m('m1', 'general', 1, DateTime.utc(2026, 10, 1)),
      );
      await app.servers[1].db.putBookmark(
        'm2',
        DateTime.utc(2026, 11, 2),
        message: m('m2', 'design', 1, DateTime.utc(2026, 10, 1)),
      );
      // The list is fetched fresh as the screen opens; the server has the same.
      fakes[0].on('GET /api/v1/bookmarks', {
        'bookmarks': [
          {
            'message_id': 'm1',
            'created_at': '2026-11-01T00:00:00.000Z',
            'message': m('m1', 'general', 1, DateTime.utc(2026, 10, 1)).raw,
          },
        ],
      });
      fakes[1].on('GET /api/v1/bookmarks', {
        'bookmarks': [
          {
            'message_id': 'm2',
            'created_at': '2026-11-02T00:00:00.000Z',
            'message': m('m2', 'design', 1, DateTime.utc(2026, 10, 1)).raw,
          },
        ],
      });
      final c = BookmarksController(app)..start();
      await pause();
      expect([for (final b in c.items) b.row.messageId], ['m2', 'm1']);
      await c.remove(c.items.first);
      await pause();
      expect([for (final b in c.items) b.row.messageId], ['m1']);
      expect(fakes[1].calls, contains('DELETE /api/v1/messages/m2/bookmark'));
      c.dispose();
    });
  });

  group('app preferences', () {
    test(
      'the sidebar is kept by the server: a change names its version, and a stale one is read again and sent once more',
      () async {
        final (app, fakes) = await makeAccountApp(null);
        final s = app.servers.first;
        final sent = <Map<String, dynamic>>[];
        var current = 3;
        fakes[0].routes['POST /api/v1/me/sidebar'] = (r) {
          final body = jsonDecode(r.body) as Map<String, dynamic>;
          sent.add(body);
          if (body['version'] != current) {
            return http.Response(
              jsonEncode({
                'error': {'code': 'conflict', 'message': 'changed elsewhere'},
              }),
              409,
              headers: {'content-type': 'application/json'},
            );
          }
          current++;
          return http.Response(
            jsonEncode({
              'sidebar': {
                'version': current,
                'sections': [
                  {'id': 1, 'name': 'Focus'},
                ],
              },
            }),
            200,
            headers: {'content-type': 'application/json'},
          );
        };
        fakes[0].on('GET /api/v1/me/sidebar', {
          'sidebar': {'version': 3},
        });
        final l = await changeLayout(s, [
          {'op': 'create_section', 'name': 'Focus'},
        ]);
        expect([for (final b in sent) b['version']], [0, 3]);
        expect(l.version, 4);
        expect(s.layout.sections.single.name, 'Focus');
        expect((await s.db.sidebarLayout()).version, 4);
      },
    );

    test('servers reorder', () async {
      final (app, _) = await makeAccountApp(null, servers: 3);
      await moveServer(app, 'server-2', 0);
      expect(
        [for (final s in app.servers) s.localId],
        ['server-2', 'server-0', 'server-1'],
      );
      expect(
        [for (final e in await app.appDb.servers()) e.localId],
        ['server-2', 'server-0', 'server-1'],
      );
    });

    test('a set-up code carries the layout and the default for show to a new device', () async {
      final (from, _) = await makeAccountApp(null);
      from.prefs.layout = SidebarLayout.merged;
      await from.prefs.putRaw('default_show', 'activity');
      await from.prefs.putRaw('sidebar_width', '333');
      final codes = (await setupPayloadOf(from)).encode();

      final (to, _) = await makeAccountApp(null, servers: 0);
      final collector = SetupCollector();
      for (final c in codes) {
        collector.add(c);
      }
      await applySetupPayload(to, collector.payload!);
      expect(to.prefs.layout, SidebarLayout.merged);
      expect(to.prefs.raw('default_show'), 'activity');
      expect(
        to.prefs.raw('sidebar_width'),
        isNull,
        reason: 'per-device things stay behind',
      );
      expect(pendingSetupServers(to), [
        'https://s0.example.org',
        'https://s1.example.org',
      ]);
      await donePendingServer(to, 'https://s0.example.org');
      expect(pendingSetupServers(to), ['https://s1.example.org']);
    });
  });
}
