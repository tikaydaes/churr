// The app's check of a message's draw (lib/core/fair/verify.dart) on
// testdata/fair/message.json, the message the server's and the web chat's
// checks read too: it passes as it is, and fails, saying why, for each thing
// changed; a message that drew nothing has nothing to check.

import 'dart:convert';
import 'dart:io';

import 'package:churr/core/fair/verify.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  final v =
      jsonDecode(File('../testdata/fair/message.json').readAsStringSync())
          as Map<String, dynamic>;
  final keys = (v['keys'] as List).cast<Map<String, dynamic>>();
  final chains = (v['chains'] as List).cast<String>();
  Map<String, dynamic> message() =>
      jsonDecode(jsonEncode(v['message'])) as Map<String, dynamic>;
  FairCheck check(
    Map<String, dynamic> m, {
    List<Map<String, dynamic>>? pinned,
    List<String>? names,
  }) => verifyMessage(
    m,
    server: v['server'] as String,
    keys: pinned ?? keys,
    chains: names ?? chains,
  );

  test('the message checks out', () {
    final r = check(message());
    expect(r.state, FairState.verified, reason: r.why);
    expect(r.block['n'], 3);
  });

  test('a message with no draw has nothing to check', () {
    expect(check({'text': 'plain'}).state, FairState.none);
  });

  test('each thing changed fails, and says why', () {
    final cases = <String, FairCheck Function()>{
      'a die of the card': () {
        final m = message();
        final t = ((m['view']['data']['rows'] as List)[0]['terms'] as List)[0];
        final rolls = t['rolls'] as List;
        rolls[0] = rolls[0] == 1 ? 2 : 1;
        return check(m);
      },
      'an inline total': () {
        final m = message();
        final row = ((m['entities'] as List)[0]['object']['data']['rows']
            as List)[0];
        row['total'] = (row['total'] as int) + 1;
        return check(m);
      },
      'another object index': () {
        final m = message();
        m['view']['draw']['i'] = 5;
        return check(m);
      },
      'another beta': () {
        final m = message();
        m['fair']['beta'] = m['fair']['prev'];
        return check(m);
      },
      'another n': () {
        final m = message();
        m['fair']['n'] = 4;
        return check(m);
      },
      'a message older than its key': () {
        final m = message();
        m['created_at'] = '2026-02-01T00:00:00.000Z';
        return check(m);
      },
      'a key not pinned': () {
        final m = message();
        m['fair']['pk'] = keys[0]['key'];
        return check(m);
      },
      'another owner': () {
        final m = message();
        m['fair']['owner'] = 'b.example';
        return check(m);
      },
      'a rule not known': () {
        final m = message();
        m['fair']['derive'] = 2;
        return check(m);
      },
      'another conversation': () =>
          check(message(), names: ['01a1ee00-0000-7000-8000-0000000000aa']),
      'another pin': () => check(message(), pinned: [keys[0]]),
    };
    for (final e in cases.entries) {
      final r = e.value();
      expect(r.state, FairState.failed, reason: e.key);
      expect(r.why, isNotEmpty, reason: e.key);
    }
  });
}
