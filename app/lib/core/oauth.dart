import 'dart:convert';
import 'dart:math';

import 'package:crypto/crypto.dart';

import 'api/client.dart';
import 'api/errors.dart';
import 'api/operations.dart';
import 'session_store.dart';

/// Sign-in as docs/AUTH.md section 11 sets it out: OAuth 2.0
/// authorization code with PKCE against the server's own address, in the
/// system browser. This file is the protocol; opening the browser and
/// catching the redirect is `lib/platform/browser_sign_in.dart`.

/// The label apps send as `client_id`. It is given no trust.
const oauthClientId = 'churr-app';

/// The apps' own address scheme, which the server accepts as a redirect
/// on phones (`AppScheme` in the server's `internal/auth`).
const appScheme = 'churr';

/// The redirect a phone uses.
const phoneRedirect = '$appScheme://oauth';

/// The grant type of QR pairing (docs/AUTH.md section 11.4).
const pairingGrant = 'urn:churr:params:oauth:grant-type:pairing-code';

/// The platform names the server accepts.
const oauthPlatforms = {'android', 'ios', 'macos', 'windows', 'linux'};

/// The redirect a desktop app uses: a loopback address by number. The
/// name `localhost` is refused by the server.
String desktopRedirect(int port) => 'http://127.0.0.1:$port/callback';

/// PKCE values and the `state` for one sign-in attempt.
class Pkce {
  Pkce._(this.verifier, this.challenge, this.state);

  /// A verifier of 256 random bits and its S256 challenge.
  factory Pkce.generate([Random? random]) {
    final r = random ?? Random.secure();
    String token(int bytes) => base64Url
        .encode(List<int>.generate(bytes, (_) => r.nextInt(256)))
        .replaceAll('=', '');
    final verifier = token(32);
    return Pkce._(verifier, challengeFor(verifier), token(16));
  }

  /// The base64url SHA-256 of a verifier, without padding.
  static String challengeFor(String verifier) => base64Url
      .encode(sha256.convert(ascii.encode(verifier)).bytes)
      .replaceAll('=', '');

  final String verifier;
  final String challenge;
  final String state;
}

/// Turns what a person typed into a server's origin, or null if it is
/// not an address. `https` is assumed and required, except for loopback
/// and `.localhost` names, where a local trial server speaks plain HTTP.
Uri? normalizeServer(String input) {
  var s = input.trim();
  if (s.isEmpty) return null;
  final hasScheme = s.contains('://');
  if (!hasScheme) s = 'https://$s';
  final u = Uri.tryParse(s);
  if (u == null || u.host.isEmpty) return null;
  final local =
      u.host == 'localhost' ||
      u.host == '127.0.0.1' ||
      u.host == '::1' ||
      u.host.endsWith('.localhost');
  var scheme = u.scheme;
  if (!hasScheme && local) scheme = 'http';
  if (scheme != 'https' && !(scheme == 'http' && local)) return null;
  return Uri(scheme: scheme, host: u.host, port: u.hasPort ? u.port : null);
}

/// The steps of the authorization-code flow that talk to the server.
extension OAuthApi on ApiClient {
  /// The authorization page to open in the system browser: [endpoint]
  /// from the server's metadata (`lib/core/oauth_metadata.dart`), or the
  /// contract's path.
  Uri authorizeUrl({
    required Pkce pkce,
    required String redirectUri,
    required String deviceName,
    required String platform,
    Uri? endpoint,
    String? boxKey,
  }) {
    assert(oauthPlatforms.contains(platform));
    final query = {
      'response_type': 'code',
      'client_id': oauthClientId,
      'redirect_uri': redirectUri,
      'code_challenge': pkce.challenge,
      'code_challenge_method': 'S256',
      'state': pkce.state,
      'device_name': deviceName,
      'platform': platform,
      // Sealed chats: the device's box public key (docs/SEALED.md 13.3).
      'box_key': ?boxKey,
    };
    if (endpoint == null) return uri(Ops.authorize.path, query);
    return endpoint.replace(
      queryParameters: {...endpoint.queryParameters, ...query},
    );
  }

  /// Checks the redirect the browser came back with and returns its
  /// code. `state` ties it to this attempt; `iss` must be this server,
  /// which stops a code from another server being accepted here.
  String codeFromRedirect(Uri redirect, Pkce pkce) {
    final q = redirect.queryParameters;
    if (q['error'] != null) {
      throw OAuthException(q['error']!, 'Access was denied in the browser.');
    }
    final code = q['code'] ?? '';
    if (q['state'] != pkce.state || code.isEmpty) {
      throw OAuthException(
        'invalid_request',
        'The browser returned an unexpected reply.',
      );
    }
    final iss = q['iss'];
    if (iss != null && _origin(Uri.parse(iss)) != _origin(base)) {
      throw OAuthException(
        'invalid_request',
        'The reply came from a different server.',
      );
    }
    return code;
  }

  /// Exchanges the code for the first token pair and keeps it.
  Future<Tokens> exchangeCode({
    required String code,
    required Pkce pkce,
    required String redirectUri,
  }) async {
    final j = await oauthPost({
      'grant_type': 'authorization_code',
      'code': code,
      'code_verifier': pkce.verifier,
      'redirect_uri': redirectUri,
    });
    final t = Tokens.fromResponse(j, DateTime.now());
    await setTokens(t);
    return t;
  }

  /// Asks for tokens with a scanned pairing code. Throws an
  /// [OAuthException] of `authorization_pending` until the person
  /// confirms the device in their browser; the caller asks again every
  /// two seconds.
  Future<Tokens> claimPairing({
    required String pairingCode,
    required String deviceName,
    required String platform,
    String? boxKey,
  }) async {
    final j = await oauthPost({
      'grant_type': pairingGrant,
      'pairing_code': pairingCode,
      'device_name': deviceName,
      'platform': platform,
      'box_key': ?boxKey,
    });
    final t = Tokens.fromResponse(j, DateTime.now());
    await setTokens(t);
    return t;
  }

  /// Revokes the session on the server (RFC 7009). Signing out goes on
  /// whether or not this arrives, so failures are swallowed.
  Future<void> revoke() async {
    final t = tokens;
    if (t == null) return;
    try {
      await oauthPost({
        'token': t.refresh.isNotEmpty ? t.refresh : t.access,
      }, op: Ops.revokeOAuthToken);
    } on Object {
      // The tokens are deleted locally either way.
    }
  }
}

String _origin(Uri u) => '${u.scheme}://${u.host}:${u.port}';
