import '../models/json.dart';

/// A failed API request, in the error shape of docs/CONVENTIONS.md:
/// `{"error": {"code", "message", "field", "retry_after_seconds"}}`.
/// [code] is what the app acts on; [message] is a plain sentence it may
/// show.
class ApiException implements Exception {
  ApiException({
    required this.status,
    required this.code,
    required this.message,
    this.field,
    this.retryAfterSeconds,
    this.serverTime,
  });

  /// Reads the error body of a response; a body in any other shape (a
  /// proxy's error page) becomes `internal` or `unavailable` by status.
  factory ApiException.fromResponse(int status, Object? body) {
    final e = body is Map ? object(body['error']) : null;
    if (e != null && e['code'] is String) {
      return ApiException(
        status: status,
        code: e['code'] as String,
        message: e['message'] as String? ?? '',
        field: e['field'] as String?,
        retryAfterSeconds: e['retry_after_seconds'] == null
            ? null
            : asInt(e['retry_after_seconds']),
        serverTime: e['server_time'] as String?,
      );
    }
    return ApiException(
      status: status,
      code: status == 401
          ? 'unauthenticated'
          : status == 503
          ? 'unavailable'
          : 'internal',
      message: 'The server answered $status.',
    );
  }

  /// The request never got an answer: no network, a refused connection,
  /// a timeout. [status] is 0 and [code] is `network`, which is not one
  /// of the server's codes.
  factory ApiException.network(Object cause) => ApiException(
    status: 0,
    code: 'network',
    message: 'Could not reach the server. ($cause)',
  );

  final int status;
  final String code;
  final String message;

  /// The field at fault, with `invalid_value`.
  final String? field;

  /// With `rate_limited` and `slow_mode`.
  final int? retryAfterSeconds;

  /// With `clock`: the server's time when it answered (docs/FAIR.md).
  final String? serverTime;

  bool get isNetwork => code == 'network';
  bool get isUnauthenticated => status == 401;

  /// Whether sending the same request again later could work without the
  /// person doing anything: the network failed or the server was busy.
  bool get isTransient => isNetwork || status >= 500 || code == 'rate_limited';

  @override
  String toString() => 'ApiException($status $code: $message)';
}

/// A failure of `/oauth/token` or `/oauth/revoke`, which alone use the
/// standard OAuth error shape `{"error": "invalid_grant"}` (docs/AUTH.md
/// section 11.3).
class OAuthException implements Exception {
  OAuthException(this.error, [this.description]);

  /// `invalid_grant`, `invalid_request`, `unsupported_grant_type` or
  /// `authorization_pending`.
  final String error;
  final String? description;

  @override
  String toString() =>
      'OAuthException($error${description == null ? '' : ': $description'})';
}

/// The session is over: the refresh token was refused. The person has to
/// sign in again through the browser.
class SignedOutException implements Exception {
  @override
  String toString() => 'SignedOutException';
}
