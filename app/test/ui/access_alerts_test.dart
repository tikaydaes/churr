import 'package:churr/core/api/notices.dart';
import 'package:churr/core/models/account.dart';
import 'package:churr/core/models/message.dart';
import 'package:churr/ui/message/message_tile.dart';
import 'package:churr/ui/screens/notices/notices_screen.dart';
import 'package:flutter_test/flutter_test.dart';

Account _acc(String id, String name) => Account.fromJson({
  'id': id,
  'username': id,
  'display_name': name,
  'role': 'member',
  'presence': 'online',
});

// The access alerts (PERMISSIONS.md section 9.2) and the notices kept for
// an account (FEATURES.md section 21.13), as the app words them.
void main() {
  final accounts = {
    'bot': _acc('bot', 'Helper'),
    'ada': _acc('ada', 'Ada'),
    'lee': _acc('lee', 'Lee'),
  };
  Message alert(String author, Map<String, dynamic> system) => Message.fromJson({
    'id': 'm1',
    'conversation_id': 'c1',
    'author_id': author,
    'system': system,
    'via': {'kind': 'system'},
    'created_at': '2026-11-17T00:00:00Z',
  });

  test('each access alert names who did it and wants attention', () {
    final token = alert('bot', {'kind': 'bot_token_made', 'by_id': 'ada'});
    expect(systemText(token, accounts), contains('A new token was made for Helper by Ada.'));
    expect(isAttentionNotice(token), isTrue);
    expect(
      systemText(alert('lee', {'kind': 'account_recovered', 'by_id': 'ada'}), accounts),
      contains('Lee’s account was recovered with a link made by Ada.'),
    );
    expect(
      systemText(alert('lee', {'kind': 'account_recovered'}), accounts),
      contains('made on the server'),
    );
    expect(
      systemText(alert('lee', {'kind': 'account_claimed', 'by_id': 'ada'}), accounts),
      'Lee’s imported account was claimed with a link made by Ada.',
    );
    expect(
      systemText(alert('ada', {'kind': 'group_access', 'by_id': 'ada', 'name': 'Crew'}), accounts),
      'Ada added themself through the group Crew',
    );
    expect(
      systemText(alert('lee', {'kind': 'group_access', 'by_id': 'ada', 'name': 'Crew'}), accounts),
      'Lee was added through the group Crew, by Ada',
    );
    final shared = alert('ada', {'kind': 'channel_shared', 'by_id': 'ada', 'name': 'b.example'});
    expect(systemText(shared, accounts), startsWith('Ada shared this channel with b.example.'));
    expect(isAttentionNotice(shared), isTrue);
    expect(isAttentionNotice(alert('ada', {'kind': 'member_joined'})), isFalse);
  });

  test('a kept notice is worded by its kind', () {
    Notice n(Map<String, dynamic> j) => Notice({'id': 'n1', 'created_at': '2026-11-17T00:00:00Z', ...j});
    expect(
      noticeText(n({'kind': 'recovery_link_used', 'by_id': 'ada'}), accounts),
      startsWith('Someone signed in to your account with a recovery link Ada made.'),
    );
    expect(noticeText(n({'kind': 'owner_made', 'by_id': 'ada', 'subject_id': 'lee'}), accounts), 'Lee was made an owner by Ada.');
    expect(noticeText(n({'kind': 'owner_stepped_down', 'subject_id': 'lee'}), accounts), 'Lee stepped down as an owner.');
    expect(noticeText(n({'kind': 'export_started', 'by_id': 'ada'}), accounts), 'Ada started the full export.');
    expect(noticeText(n({'kind': 'export_downloaded', 'by_id': 'ada'}), accounts), 'Ada downloaded the full export.');
    expect(
      noticeText(
        n({'kind': 'override_used', 'by_id': 'ada', 'subject_id': 'lee', 'conversation_id': 'c1'}),
        accounts,
        convName: (id) => id == 'c1' ? 'ops' : null,
      ),
      'Ada added Lee to #ops by override.',
    );
  });
}
