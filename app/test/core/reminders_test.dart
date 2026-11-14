import 'dart:convert';

import 'package:churr/core/api/client.dart';
import 'package:churr/core/api/reminders.dart';
import 'package:churr/core/session_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  test('"remind me about this" finds the own channel, then schedules a reminder about the message', () async {
    final seen = <http.Request>[];
    final api = ApiClient(
      base: Uri.parse('https://chat.example.org'),
      tokens: Tokens(
        access: 'a',
        refresh: '',
        expiresAt: DateTime.now().add(const Duration(hours: 1)),
      ),
      httpClient: MockClient((r) async {
        seen.add(r);
        if (r.url.path == '/api/v1/chats') {
          return http.Response(
            jsonEncode({
              'conversation': {
                'id': 'self',
                'kind': 'channel',
                'member_ids': ['me'],
              },
            }),
            200,
          );
        }
        return http.Response(
          jsonEncode({
            'scheduled': {'id': 's1'},
          }),
          201,
        );
      }),
    );
    final at = DateTime.utc(2026, 11, 15, 9);
    final j = await api.remindMeAbout('m42', at);
    expect(j['id'], 's1');
    expect(jsonDecode(seen[0].body), {'account_ids': []});
    expect(seen[1].url.path, '/api/v1/scheduled');
    final body = jsonDecode(seen[1].body) as Map<String, dynamic>;
    expect(body['conversation_id'], 'self');
    expect(body['kind'], 'reminder');
    expect(body['about_message_id'], 'm42');
    expect(body['content'], {'text': 'Reminder'});
    expect(DateTime.parse(body['run_at'] as String), at);
  });
}
