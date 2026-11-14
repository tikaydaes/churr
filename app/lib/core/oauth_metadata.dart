import 'dart:convert';

import 'api/client.dart';
import 'api/errors.dart';
import 'api/operations.dart';
import 'db/cache_db.dart';
import 'models/json.dart';

/// The server's OAuth authorization server metadata (RFC 8414,
/// docs/AUTH.md 11.1). The app needs only the server's address: the
/// endpoints come from here, not from fixed paths.
class OAuthMetadata {
  OAuthMetadata({
    required this.issuer,
    required this.authorizationEndpoint,
    required this.tokenEndpoint,
    this.revocationEndpoint,
    this.raw = const {},
  });

  /// Reads and checks the document for the server at [base]. The issuer
  /// must be that server's origin and every endpoint must be on it: a
  /// document that sends codes or tokens anywhere else is refused.
  factory OAuthMetadata.parse(Json j, Uri base) {
    Uri? endpoint(String key) {
      final v = j[key];
      if (v is! String || v.isEmpty) return null;
      final u = base.resolve(v);
      if (_origin(u) != _origin(base)) {
        throw OAuthException(
          'invalid_request',
          'The server named a sign-in address on another server.',
        );
      }
      return u;
    }

    final issuer = j['issuer'];
    if (issuer is! String || _origin(Uri.parse(issuer)) != _origin(base)) {
      throw OAuthException(
        'invalid_request',
        'The server described itself as a different server.',
      );
    }
    final methods = strings(j['code_challenge_methods_supported']);
    if (methods.isNotEmpty && !methods.contains('S256')) {
      throw OAuthException(
        'invalid_request',
        'The server does not offer the sign-in this app needs.',
      );
    }
    final auth = endpoint('authorization_endpoint');
    final token = endpoint('token_endpoint');
    if (auth == null || token == null) {
      throw OAuthException(
        'invalid_request',
        'The server did not say where to sign in.',
      );
    }
    return OAuthMetadata(
      issuer: issuer,
      authorizationEndpoint: auth,
      tokenEndpoint: token,
      revocationEndpoint: endpoint('revocation_endpoint'),
      raw: j,
    );
  }

  final String issuer;
  final Uri authorizationEndpoint;
  final Uri tokenEndpoint;
  final Uri? revocationEndpoint;
  final Json raw;

  /// Whether the server takes QR pairing.
  bool get pairing =>
      strings(raw['grant_types_supported'])
          .contains('urn:churr:params:oauth:grant-type:pairing-code');

  /// Points [api]'s token requests (refresh, revoke, code and pairing
  /// exchanges) at these endpoints.
  void applyTo(ApiClient api) {
    api.tokenEndpoint = tokenEndpoint;
    api.revocationEndpoint = revocationEndpoint;
  }
}

String _origin(Uri u) => '${u.scheme}://${u.host}:${u.port}';

/// Fetches the metadata from the server's well-known address and points
/// [api] at its endpoints.
Future<OAuthMetadata> fetchOAuthMetadata(ApiClient api) async {
  final j = await api.call(Ops.getOAuthMetadata, auth: false);
  final m = OAuthMetadata.parse(j, api.base);
  m.applyTo(api);
  return m;
}

/// Keeps the metadata in a server's `cache.db`, so its token requests
/// use the right endpoints from the first refresh after a restart.
Future<void> storeOAuthMetadata(CacheDb db, OAuthMetadata m) =>
    db.putSetting('oauth_metadata', jsonEncode(m.raw));

/// Points [api] at the endpoints kept for its server, if any.
Future<void> restoreOAuthMetadata(CacheDb db, ApiClient api) async {
  final raw = await db.setting('oauth_metadata');
  if (raw == null || raw.isEmpty) return;
  try {
    OAuthMetadata.parse(jsonDecode(raw) as Json, api.base).applyTo(api);
  } on Object {
    // Fetched again at the next sign-in.
  }
}
