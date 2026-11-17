import 'package:churr/core/api/approvals.dart';
import 'package:churr/state/server_state.dart';
import 'package:churr/ui/screens/approvals/approvals_screen.dart';
import 'package:churr/ui/screens/channels/new_channel_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import '../forms_support.dart';

/// Approvals in the app (docs/FEATURES.md section 31): the count and
/// whether the entry shows, the list with its kinds as filters, approving
/// and declining with a reason, a list kept current by the live events, and
/// asking for a channel while new channels wait for approval.
void main() {
  quietDrift();

  Map<String, dynamic> approval(String id, String kind, String summary) => {
    'id': id,
    'kind': kind,
    'requester_id': 'sam',
    'requester': {'id': 'sam', 'username': 'sam', 'display_name': 'Sam'},
    'summary': summary,
    'note': kind == 'channel' ? 'For the launch' : '',
    'state': 'waiting',
    'created_at': '2026-11-16T09:00:00.000Z',
    'expires_at': '2026-11-30T09:00:00.000Z',
  };

  test('the summary decides the entry and its count', () async {
    final s = await fakeServerState(
      FakeServer((r) {
        if (r.url.path.endsWith('/approvals/summary')) {
          return (
            200,
            {
              'summary': {
                'may_approve': true,
                'kinds': ['registration', 'channel'],
                'counts': {'registration': 2, 'channel': 1},
              },
            },
          );
        }
        return null;
      }),
    );
    final app = appWith([s]);
    expect(app.decidesApprovals, isFalse);
    await s.refreshApprovals();
    expect(app.decidesApprovals, isTrue);
    expect(app.totalApprovals, 3);
  });

  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets('approvals: filters, approve, decline with a reason, and '
        'live changes ($label)', (tester) async {
      final requests = <http.Request>[];
      var waiting = [
        approval('a1', 'registration', 'ada'),
        approval('a2', 'channel', 'launch'),
      ];
      late final ServerState s;
      await tester.runAsync(() async {
        s = await fakeServerState(
          FakeServer((r) {
            requests.add(r);
            final p = r.url.path;
            if (p.endsWith('/approvals/summary')) {
              return (
                200,
                {
                  'summary': {
                    'kinds': ['registration', 'channel'],
                    'counts': {'registration': 1, 'channel': 1},
                  },
                },
              );
            }
            if (p.endsWith('/approvals')) return (200, {'approvals': waiting});
            if (r.method == 'POST' && p.contains('/approvals/')) {
              final id = p.split('/').last;
              waiting = [
                for (final a in waiting)
                  if (a['id'] != id) a,
              ];
              return (200, {'approval': approval(id, 'channel', 'x')});
            }
            return null;
          }),
        );
        s.approvals = const ApprovalSummary(
          kinds: ['registration', 'channel'],
          counts: {'registration': 1, 'channel': 1},
        );
      });
      await pumpScreen(
        tester,
        appWith([s]),
        const ApprovalsScreen(),
        size: size,
      );
      expect(find.text('Asks to join this instance'), findsOneWidget);
      expect(find.text('Asks for a new channel: launch'), findsOneWidget);
      expect(find.text('“For the launch”'), findsOneWidget);

      // The kinds are filters.
      await tester.tap(find.byKey(const Key('approvals-kind-channel')));
      await tester.pump();
      expect(find.text('Asks to join this instance'), findsNothing);
      await tester.tap(find.byKey(const Key('approvals-kind-all')));
      await tester.pump();

      await tester.tap(find.byKey(const Key('approval-approve-a1')));
      await settleAsync(tester);
      final yes = requests.singleWhere(
        (r) =>
            r.method == 'POST' &&
            r.url.path.endsWith('/approvals/registration/a1'),
      );
      expect(body(yes), {'approve': true});
      expect(find.byKey(const Key('approval-a1')), findsNothing);

      await tester.tap(find.byKey(const Key('approval-decline-a2')));
      await tester.pumpAndSettle();
      await tester.enterText(
        find.byKey(const Key('approval-reason')),
        'We have #launch already',
      );
      await tester.tap(find.byKey(const Key('approval-decline-go')));
      await settleAsync(tester);
      final no = requests.singleWhere(
        (r) =>
            r.method == 'POST' && r.url.path.endsWith('/approvals/channel/a2'),
      );
      expect(body(no), {'approve': false, 'reason': 'We have #launch already'});
      expect(find.byKey(const Key('approvals-empty')), findsOneWidget);

      // A new request arrives live: the list is read again.
      waiting = [approval('a3', 'channel', 'design-crit')];
      await tester.runAsync(
        () => s.sync.applyEvent({
          'type': 'approval.created',
          'approval': waiting.first,
        }),
      );
      await settleAsync(tester);
      expect(find.text('Asks for a new channel: design-crit'), findsOneWidget);
      expect(tester.takeException(), isNull);
    });

    testWidgets('a new channel that waits for approval is asked for, with a '
        'note ($label)', (tester) async {
      final requests = <http.Request>[];
      late final ServerState s;
      await tester.runAsync(() async {
        s = await fakeServerState(
          FakeServer((r) {
            requests.add(r);
            final p = r.url.path;
            if (r.method == 'GET') return (200, {'conversations': <Object>[]});
            if (p.endsWith('/approvals/channels')) {
              return (201, {'approval': approval('a9', 'channel', 'launch')});
            }
            if (p.endsWith('/channels')) {
              return (
                403,
                {
                  'error': {
                    'code': 'approval_required',
                    'message': 'New channels wait for approval here.',
                  },
                },
              );
            }
            return null;
          }),
        );
      });
      final app = appWith([s]);
      await pumpScreen(
        tester,
        app,
        NewChannelForm(app: app, server: s),
        size: size,
      );
      await tester.enterText(find.byKey(const Key('channel-name')), 'launch');
      await tester.pump(const Duration(milliseconds: 400));
      await settleAsync(tester);
      await tester.ensureVisible(find.byKey(const Key('channel-create')));
      await tester.tap(find.byKey(const Key('channel-create')));
      await settleAsync(tester);
      expect(find.byKey(const Key('channel-needs-approval')), findsOneWidget);
      expect(find.byKey(const Key('channel-create')), findsNothing);
      await tester.enterText(
        find.byKey(const Key('channel-approval-note')),
        'For the launch',
      );
      await tester.ensureVisible(find.byKey(const Key('channel-ask-approval')));
      await tester.tap(find.byKey(const Key('channel-ask-approval')));
      await settleAsync(tester);
      final ask = requests.singleWhere(
        (r) => r.url.path.endsWith('/approvals/channels'),
      );
      expect(body(ask), {
        'name': 'launch',
        'private': false,
        'note': 'For the launch',
      });
      expect(
        find.textContaining('Your request for launch was sent'),
        findsOneWidget,
      );
      expect(app.open, isNull);
      expect(tester.takeException(), isNull);
    });
  }
}
