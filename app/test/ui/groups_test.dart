import 'package:churr/core/models/server.dart';
import 'package:churr/ui/screens/channels/groups_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../account_support.dart';

// Joining and leaving an open group (docs/CLIENTS.md 9.15).

Map<String, dynamic> group(
  String id,
  String handle, {
  bool open = true,
  bool member = false,
  String? builtin,
  String? provider,
}) => {
  'id': id,
  'handle': handle,
  'name': handle.toUpperCase(),
  'description': '',
  'open': open,
  'is_member': member,
  'member_count': 3,
  'builtin': ?builtin,
  'provider_id': ?provider,
};

void main() {
  final groups = [
    group('g1', 'design'),
    group('g2', 'oncall', member: true),
    group('g3', 'leads', open: false),
    group('g4', 'staff', provider: 'p1'),
    group('g5', 'everyone', builtin: 'everyone'),
  ];

  test('who may join or leave by themselves', () {
    final open = Group.fromJson(group('g1', 'design'));
    expect(open.selfServe('member'), isTrue);
    expect(open.selfServe('guest'), isFalse);
    expect(
      Group.fromJson(group('g3', 'leads', open: false)).selfServe('owner'),
      isFalse,
    );
    expect(
      Group.fromJson(group('g4', 'staff', provider: 'p1')).selfServe('member'),
      isFalse,
    );
    // The cache keeps what the decision needs.
    final again = Group.fromJson(
      Group.fromJson(group('g4', 'staff', provider: 'p1')).toJson(),
    );
    expect(again.fromProvider, isTrue);
    expect(again.open, isTrue);
  });

  for (final (label, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets('at $label size: join an open group, leave one', (
      tester,
    ) async {
      await sized(tester, size);
      final (app, fakes) = await makeAccountApp(tester);
      final s = app.servers[0];
      await tester.runAsync(
        () => s.db.replaceGroups([for (final g in groups) Group.fromJson(g)]),
      );
      fakes[0].on('GET /api/v1/groups', {'groups': groups});
      await tester.pumpWidget(
        harness(app, Scaffold(body: GroupsScreen(server: s))),
      );
      await settleAll(tester);
      expect(find.byKey(const Key('group-design')), findsOneWidget);
      expect(find.byKey(const Key('group-everyone')), findsNothing);
      expect(find.byKey(const Key('join-group-leads')), findsNothing);
      expect(find.byKey(const Key('join-group-staff')), findsNothing);

      await tester.tap(find.byKey(const Key('join-group-design')));
      await settleAll(tester);
      expect(fakes[0].calls, contains('POST /api/v1/groups/g1/members/me'));

      await tester.tap(find.byKey(const Key('leave-group-oncall')));
      await settleAll(tester);
      expect(fakes[0].calls, contains('DELETE /api/v1/groups/g2/members/me'));
      expect(tester.takeException(), isNull);
    });
  }
}
