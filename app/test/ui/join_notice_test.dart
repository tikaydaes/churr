import 'package:churr/core/models/account.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/ui/message/message_tile.dart';
import 'package:flutter_test/flutter_test.dart';

Account _acc(String id, String name) => Account.fromJson({
  'id': id,
  'username': id,
  'display_name': name,
  'role': 'member',
  'presence': 'online',
});

void main() {
  final accounts = {'ada': _acc('ada', 'Ada'), 'lee': _acc('lee', 'Lee')};
  Message notice(Map<String, dynamic> system) => Message.fromJson({
    'id': 'm1',
    'conversation_id': 'c1',
    'author_id': 'ada',
    'system': system,
    'via': {'kind': 'system'},
    'created_at': '2026-11-17T00:00:00Z',
  });

  test('a private channel announces who joined and who was added', () {
    expect(
      systemText(notice({'kind': 'member_joined'}), accounts),
      'Ada joined',
    );
    expect(
      systemText(
        notice({'kind': 'member_joined', 'by_id': 'lee'}),
        accounts,
      ),
      'Ada was added by Lee',
    );
  });
}
