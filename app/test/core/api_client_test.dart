import 'dart:convert';
import 'dart:math';

import 'package:churr/core/api/client.dart';
import 'package:churr/core/api/conversations.dart';
import 'package:churr/core/api/errors.dart';
import 'package:churr/core/api/me.dart';
import 'package:churr/core/api/messages.dart';
import 'package:churr/core/oauth.dart';
import 'package:churr/core/session_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

final _base = Uri.parse('https://chat.example.org');

http.Response _json(
  Object body, [
  int status = 200,
  Map<String, String> headers = const {},
]) => http.Response(
  jsonEncode(body),
  status,
  headers: {'content-type': 'application/json', ...headers},
);

Tokens _tokens({
  String access = 'churr_app_A',
  String refresh = 'churr_ref_A',
  int minutes = 30,
}) => Tokens(
  access: access,
  refresh: refresh,
  expiresAt: DateTime.now().add(Duration(minutes: minutes)),
);

void main() {
  test(
    'requests carry the bearer token and read the rate-limit headers',
    () async {
      late http.Request seen;
      final api = ApiClient(
        base: _base,
        tokens: _tokens(),
        httpClient: MockClient((r) async {
          seen = r;
          return _json(
            {
              'badge': 4,
              'mentions': 2,
              'unread_conversations': 3,
              'unread_threads': 1,
              'dnd': false,
            },
            200,
            {
              'ratelimit-limit': '120',
              'ratelimit-remaining': '117',
              'ratelimit-reset': '42',
            },
          );
        }),
      );
      final s = await api.getStatusSummary();
      expect(s.badge, 4);
      expect(seen.url.toString(), 'https://chat.example.org/api/v1/status');
      expect(seen.headers['Authorization'], 'Bearer churr_app_A');
      expect(api.lastRateLimit!.remaining, 117);
      expect(api.lastRateLimit!.resetSeconds, 42);
    },
  );

  test('getServer needs no credential', () async {
    final api = ApiClient(
      base: _base,
      httpClient: MockClient((r) async {
        expect(r.headers.containsKey('Authorization'), isFalse);
        return _json({
          'server': {
            'name': 'chat.example.org',
            'community_name': 'Example',
            'version': '1',
            'min_app_version': '0',
            'setup_needed': false,
          },
        });
      }),
    );
    expect((await api.getServer()).communityName, 'Example');
  });

  test('a token about to run out is rotated first, and the new pair is saved before use', () async {
    final log = <String>[];
    final api = ApiClient(
      base: _base,
      tokens: _tokens(minutes: 0),
      onTokens: (t) async => log.add('saved ${t.access}'),
      httpClient: MockClient((r) async {
        if (r.url.path == '/oauth/token') {
          expect(r.bodyFields, {
            'grant_type': 'refresh_token',
            'refresh_token': 'churr_ref_A',
          });
          log.add('refresh');
          return _json({
            'access_token': 'churr_app_B',
            'refresh_token': 'churr_ref_B',
            'token_type': 'Bearer',
            'expires_in': 3600,
          });
        }
        log.add('request ${r.headers['Authorization']}');
        return _json({
          'account': {
            'id': 'a',
            'username': 'kai',
            'display_name': 'Kai',
            'role': 'owner',
          },
        });
      }),
    );
    await api.getMe();
    expect(log, ['refresh', 'saved churr_app_B', 'request Bearer churr_app_B']);
    expect(api.tokens!.refresh, 'churr_ref_B');
  });

  test(
    'concurrent requests share one refresh: a refresh token works once',
    () async {
      var refreshes = 0;
      final api = ApiClient(
        base: _base,
        tokens: _tokens(minutes: 0),
        httpClient: MockClient((r) async {
          if (r.url.path == '/oauth/token') {
            refreshes++;
            await Future<void>.delayed(const Duration(milliseconds: 10));
            return _json({
              'access_token': 'churr_app_B',
              'refresh_token': 'churr_ref_B',
              'expires_in': 3600,
            });
          }
          return _json({'badge': 0});
        }),
      );
      await Future.wait([
        api.getStatusSummary(),
        api.getStatusSummary(),
        api.getStatusSummary(),
      ]);
      expect(refreshes, 1);
    },
  );

  test('a 401 with a token believed good: one refresh, one more try', () async {
    final seen = <String>[];
    final api = ApiClient(
      base: _base,
      tokens: _tokens(),
      httpClient: MockClient((r) async {
        if (r.url.path == '/oauth/token') {
          return _json({
            'access_token': 'churr_app_B',
            'refresh_token': 'churr_ref_B',
            'expires_in': 3600,
          });
        }
        seen.add(r.headers['Authorization']!);
        return seen.length == 1
            ? _json({
                'error': {'code': 'unauthenticated', 'message': 'Sign in.'},
              }, 401)
            : _json({'badge': 1});
      }),
    );
    expect((await api.getStatusSummary()).badge, 1);
    expect(seen, ['Bearer churr_app_A', 'Bearer churr_app_B']);
  });

  test('a refused refresh ends the session', () async {
    var signedOut = 0;
    final api = ApiClient(
      base: _base,
      tokens: _tokens(minutes: 0),
      onSignedOut: () => signedOut++,
      httpClient: MockClient(
        (r) async => _json({'error': 'invalid_grant'}, 400),
      ),
    );
    await expectLater(api.getMe(), throwsA(isA<SignedOutException>()));
    await expectLater(api.getMe(), throwsA(isA<SignedOutException>()));
    expect(signedOut, 1);
    expect(api.signedOut, isTrue);
  });

  test('errors arrive in the error shape', () async {
    final api = ApiClient(
      base: _base,
      tokens: _tokens(),
      httpClient: MockClient(
        (r) async => _json({
          'error': {
            'code': 'forbidden',
            'message': 'You are not in this channel.',
          },
        }, 403),
      ),
    );
    await expectLater(
      api.getConversation('c1'),
      throwsA(
        isA<ApiException>()
            .having((e) => e.code, 'code', 'forbidden')
            .having((e) => e.status, 'status', 403),
      ),
    );
  });

  test('no answer at all is a network error, and transient', () async {
    final api = ApiClient(
      base: _base,
      tokens: _tokens(),
      httpClient: MockClient(
        (r) async => throw http.ClientException('no route'),
      ),
    );
    await expectLater(
      api.getMe(),
      throwsA(
        isA<ApiException>()
            .having((e) => e.isNetwork, 'network', isTrue)
            .having((e) => e.isTransient, 'transient', isTrue),
      ),
    );
  });

  test(
    'rate_limited waits the time the server asked for and tries again',
    () async {
      final slept = <Duration>[];
      var calls = 0;
      final api = ApiClient(
        base: _base,
        tokens: _tokens(),
        sleep: (d) async => slept.add(d),
        httpClient: MockClient(
          (r) async => ++calls < 3
              ? _json({
                  'error': {
                    'code': 'rate_limited',
                    'message': 'Slow down.',
                    'retry_after_seconds': 2,
                  },
                }, 429)
              : _json({'badge': 9}),
        ),
      );
      expect((await api.getStatusSummary()).badge, 9);
      expect(slept, [const Duration(seconds: 2), const Duration(seconds: 2)]);
    },
  );

  test(
    'rate_limited gives up after two more tries; slow_mode is never retried',
    () async {
      var calls = 0;
      final limited = ApiClient(
        base: _base,
        tokens: _tokens(),
        sleep: (_) async {},
        httpClient: MockClient((r) async {
          calls++;
          return _json({
            'error': {
              'code': 'rate_limited',
              'message': 'Slow down.',
              'retry_after_seconds': 1,
            },
          }, 429);
        }),
      );
      await expectLater(
        limited.getStatusSummary(),
        throwsA(
          isA<ApiException>().having((e) => e.code, 'code', 'rate_limited'),
        ),
      );
      expect(calls, 3);

      calls = 0;
      final slow = ApiClient(
        base: _base,
        tokens: _tokens(),
        sleep: (_) async => fail('must not wait'),
        httpClient: MockClient((r) async {
          calls++;
          return _json({
            'error': {
              'code': 'slow_mode',
              'message': 'Wait.',
              'retry_after_seconds': 30,
            },
          }, 429);
        }),
      );
      await expectLater(
        slow.postMessage('c1', clientId: 'x', text: 'hi'),
        throwsA(
          isA<ApiException>().having((e) => e.retryAfterSeconds, 'retry', 30),
        ),
      );
      expect(calls, 1);
    },
  );

  test('a post carries client_id, and parent_id only for a reply', () async {
    final bodies = <Map<String, dynamic>>[];
    final api = ApiClient(
      base: _base,
      tokens: _tokens(),
      httpClient: MockClient((r) async {
        expect(r.method, 'POST');
        expect(r.url.path, '/api/v1/conversations/c1/messages');
        bodies.add(jsonDecode(r.body) as Map<String, dynamic>);
        return _json({
          'message': {
            'id': 'm1',
            'conversation_id': 'c1',
            'seq': 7,
            'author_id': 'a',
            'text': 'hi',
            'created_at': '2026-11-13T09:30:00.000Z',
          },
        }, 201);
      }),
    );
    final m = await api.postMessage(
      'c1',
      clientId: '0193c000-1111-7222-8333-444455556666',
      text: 'hi',
    );
    expect(m.seq, 7);
    await api.postMessage(
      'c1',
      clientId: 'c2',
      text: 'a reply',
      parentId: 'm1',
    );
    expect(bodies[0], {
      'client_id': '0193c000-1111-7222-8333-444455556666',
      'text': 'hi',
    });
    expect(bodies[1], {
      'client_id': 'c2',
      'text': 'a reply',
      'parent_id': 'm1',
    });
  });

  test('cursor paging follows next_cursor to the end', () async {
    final cursors = <String?>[];
    final api = ApiClient(
      base: _base,
      tokens: _tokens(),
      httpClient: MockClient((r) async {
        cursors.add(r.url.queryParameters['cursor']);
        expect(r.url.queryParameters['limit'], '200');
        return switch (r.url.queryParameters['cursor']) {
          null => _json({
            'conversations': [
              {'id': 'c1', 'kind': 'channel', 'private': false},
            ],
            'next_cursor': 'p2',
          }),
          'p2' => _json({
            'conversations': [
              {'id': 'c2', 'kind': 'channel', 'private': false},
            ],
            'next_cursor': 'p3',
          }),
          _ => _json({
            'conversations': [
              {'id': 'c3', 'kind': 'channel', 'private': false},
            ],
          }),
        };
      }),
    );
    expect((await api.listConversations()).map((c) => c.id), [
      'c1',
      'c2',
      'c3',
    ]);
    expect(cursors, [null, 'p2', 'p3']);
  });

  test('paging by sequence number sends before, after and limit', () async {
    final api = ApiClient(
      base: _base,
      tokens: _tokens(),
      httpClient: MockClient((r) async {
        expect(r.url.queryParameters, {'before': '4771', 'limit': '50'});
        return _json({
          'messages': [],
          'thread_states': [
            {
              'parent_id': 'p',
              'conversation_id': 'c1',
              'state': 'following',
              'read_seq': 0,
              'unread_count': 3,
            },
          ],
          'has_more': true,
        });
      }),
    );
    final page = await api.listMessages('c1', before: 4771);
    expect(page.hasMore, isTrue);
    expect(page.threadStates.single.unreadCount, 3);
  });

  test(
    'updateMembership sends only what changes, with an explicit null for show',
    () async {
      final api = ApiClient(
        base: _base,
        tokens: _tokens(),
        httpClient: MockClient((r) async {
          expect(r.method, 'PATCH');
          expect(r.body, '{"show":null}');
          return _json({
            'membership': {
              'level': 'member',
              'read_seq': 0,
              'mode': 'mentions',
            },
          });
        }),
      );
      expect((await api.updateMembership('c1', {'show': null})).show, isNull);
    },
  );

  group('OAuth', () {
    test('PKCE: a long random verifier and its S256 challenge', () {
      final p = Pkce.generate(Random(1));
      expect(p.verifier.length, greaterThanOrEqualTo(43));
      expect(p.challenge, Pkce.challengeFor(p.verifier));
      // The example of RFC 7636, appendix B.
      expect(
        Pkce.challengeFor('dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk'),
        'E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM',
      );
      expect(Pkce.generate().state, isNot(Pkce.generate().state));
    });

    test(
      'the authorization address has everything AUTH.md section 11.2 lists',
      () {
        final api = ApiClient(base: _base);
        final p = Pkce.generate();
        final u = api.authorizeUrl(
          pkce: p,
          redirectUri: desktopRedirect(49152),
          deviceName: "Kai's laptop",
          platform: 'macos',
        );
        expect(u.path, '/oauth/authorize');
        expect(u.queryParameters, {
          'response_type': 'code',
          'client_id': 'churr-app',
          'redirect_uri': 'http://127.0.0.1:49152/callback',
          'code_challenge': p.challenge,
          'code_challenge_method': 'S256',
          'state': p.state,
          'device_name': "Kai's laptop",
          'platform': 'macos',
        });
      },
    );

    test('the redirect forms: loopback by number on desktop, the app scheme on phones', () {
      expect(desktopRedirect(5), 'http://127.0.0.1:5/callback');
      expect(phoneRedirect, 'churr://oauth');
    });

    test('the redirect is checked: state, an error, and iss', () {
      final api = ApiClient(base: _base);
      final p = Pkce.generate();
      Uri back(Map<String, String> q) =>
          Uri.parse('http://127.0.0.1:1/callback').replace(queryParameters: q);
      expect(
        api.codeFromRedirect(
          back({
            'code': 'abc',
            'state': p.state,
            'iss': 'https://chat.example.org',
          }),
          p,
        ),
        'abc',
      );
      expect(
        () => api.codeFromRedirect(back({'code': 'abc', 'state': 'other'}), p),
        throwsA(isA<OAuthException>()),
      );
      expect(
        () => api.codeFromRedirect(
          back({'error': 'access_denied', 'state': p.state}),
          p,
        ),
        throwsA(isA<OAuthException>()),
      );
      expect(
        () => api.codeFromRedirect(
          back({
            'code': 'abc',
            'state': p.state,
            'iss': 'https://evil.example',
          }),
          p,
        ),
        throwsA(isA<OAuthException>()),
      );
    });

    test(
      'the code is exchanged with its verifier and the same redirect',
      () async {
        Tokens? saved;
        final api = ApiClient(
          base: _base,
          onTokens: (t) async => saved = t,
          httpClient: MockClient((r) async {
            expect(r.url.path, '/oauth/token');
            expect(r.headers.containsKey('Authorization'), isFalse);
            expect(r.bodyFields['grant_type'], 'authorization_code');
            expect(r.bodyFields['code'], 'the-code');
            expect(r.bodyFields['redirect_uri'], 'http://127.0.0.1:9/callback');
            expect(r.bodyFields['code_verifier'], isNotEmpty);
            return _json({
              'access_token': 'churr_app_X',
              'refresh_token': 'churr_ref_X',
              'token_type': 'Bearer',
              'expires_in': 3600,
            });
          }),
        );
        final t = await api.exchangeCode(
          code: 'the-code',
          pkce: Pkce.generate(),
          redirectUri: 'http://127.0.0.1:9/callback',
        );
        expect(t.access, 'churr_app_X');
        expect(saved!.refresh, 'churr_ref_X');
      },
    );

    test('pairing is pending until the person confirms', () async {
      final api = ApiClient(
        base: _base,
        httpClient: MockClient((r) async {
          expect(r.bodyFields['grant_type'], pairingGrant);
          return _json({'error': 'authorization_pending'}, 400);
        }),
      );
      await expectLater(
        api.claimPairing(
          pairingCode: 'p',
          deviceName: 'Phone',
          platform: 'ios',
        ),
        throwsA(
          isA<OAuthException>().having(
            (e) => e.error,
            'error',
            'authorization_pending',
          ),
        ),
      );
    });

    test(
      'signing out revokes the refresh token and survives a dead server',
      () async {
        var revoked = '';
        final api = ApiClient(
          base: _base,
          tokens: _tokens(),
          httpClient: MockClient((r) async {
            expect(r.url.path, '/oauth/revoke');
            revoked = r.bodyFields['token']!;
            throw http.ClientException('down');
          }),
        );
        await api.revoke();
        expect(revoked, 'churr_ref_A');
      },
    );

    test(
      'server addresses: https assumed, plain http only for a local trial',
      () {
        expect(
          normalizeServer('chat.example.org').toString(),
          'https://chat.example.org',
        );
        expect(
          normalizeServer(' https://chat.example.org/some/page ').toString(),
          'https://chat.example.org',
        );
        expect(
          normalizeServer('localhost:8472').toString(),
          'http://localhost:8472',
        );
        expect(
          normalizeServer('http://localhost:8472').toString(),
          'http://localhost:8472',
        );
        expect(normalizeServer('http://chat.example.org'), isNull);
        expect(normalizeServer(''), isNull);
      },
    );
  });

  test('tokens are stored per server and survive a round trip', () async {
    final store = SessionStore(MemorySecretStore());
    final t = _tokens();
    await store.save('s1', t);
    expect((await store.load('s1'))!.access, t.access);
    expect(
      (await store.load('s1'))!.expiresAt.millisecondsSinceEpoch,
      t.expiresAt.millisecondsSinceEpoch,
    );
    expect(await store.load('s2'), isNull);
    await store.clear('s1');
    expect(await store.load('s1'), isNull);
  });
}
