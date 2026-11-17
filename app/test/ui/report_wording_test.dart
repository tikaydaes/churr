import 'package:churr/core/models/conversation.dart';
import 'package:churr/ui/screens/conversation/message_menu.dart';
import 'package:flutter_test/flutter_test.dart';

// Who a report reaches, as the report dialog says it (docs/PERMISSIONS.md
// section 8.4): a chat has no moderators, so its reports go to the admins.
void main() {
  Conversation conv(Map<String, Object?> j) => Conversation.fromJson({
    'id': 'c',
    'kind': 'channel',
    'created_at': '2026-11-17T00:00:00Z',
    ...j,
  });

  test('a chat\'s report goes to the admins, who see that one message', () {
    final t = reportGoesTo(conv({'private': true}));
    expect(t, contains("instance's admins"));
    expect(t, contains('nothing else'));
    expect(t, contains('share this chat'));
    expect(t, contains('not told'));
  });

  test('a private channel\'s report goes to its moderators first', () {
    final t = reportGoesTo(conv({'private': true, 'name': 'den'}));
    expect(t, startsWith("The report goes to this channel's moderators"));
    expect(t, contains('about one of them'));
  });

  test('a public channel\'s report goes to its moderators and the admins', () {
    final t = reportGoesTo(conv({'private': false, 'name': 'town'}));
    expect(t, contains('and to the admins'));
  });
}
