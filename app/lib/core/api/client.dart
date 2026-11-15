import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:http/http.dart' as http;

import '../models/json.dart';
import '../session_store.dart';
import '../wire/wire.dart';
import 'errors.dart';
import 'operations.dart';

/// The rate-limit headers of the last authenticated answer
/// (docs/API.md section 10). The numbers are the server's; the client
/// reads them and assumes nothing.
/// One file of a multipart upload: the form field it goes in, its name
/// and its bytes.
class UploadPart {
  const UploadPart(this.field, this.name, this.bytes);
  final String field;
  final String name;
  final List<int> bytes;
}

class RateLimit {
  const RateLimit(this.limit, this.remaining, this.resetSeconds);
  final int limit;
  final int remaining;
  final int resetSeconds;
}

/// The HTTP client for one server.
///
/// It owns the token pair: every request carries the access token, the
/// pair is refreshed shortly before it runs out and once on a 401, and a
/// rotated pair is handed to [onTokens] before it is used so it is never
/// lost. A refused refresh ends the session: [onSignedOut] is called and
/// requests throw [SignedOutException].
///
/// Requests name an [Op] from `operations.dart`; nothing else builds a
/// path. The typed methods live in extensions, one file per API area.
class ApiClient {
  ApiClient({
    required this.base,
    http.Client? httpClient,
    Tokens? tokens,
    this.onTokens,
    this.onSignedOut,
    this.protobuf = true,
    DateTime Function()? now,
    Future<void> Function(Duration)? sleep,
  }) : _http = httpClient ?? http.Client(),
       // ignore: prefer_initializing_formals
       _tokens = tokens,
       _now = now ?? DateTime.now,
       _sleep = sleep ?? Future<void>.delayed;

  /// The server's origin, such as `https://chat.example.org`.
  final Uri base;
  final http.Client _http;
  final DateTime Function() _now;
  final Future<void> Function(Duration) _sleep;

  /// Called with each new pair, before the first request that uses it.
  final Future<void> Function(Tokens)? onTokens;

  /// Called once when the refresh token is refused.
  final void Function()? onSignedOut;

  /// Whether the client speaks Protocol Buffers to the server
  /// (docs/WIRE-PROTOBUF.md). Every request then asks for protobuf in
  /// `Accept`, and an answer is read by the `Content-Type` it comes with,
  /// so a server that knows nothing of protobuf is simply answered in JSON
  /// as before. Request bodies go as protobuf only once the server has
  /// answered in it, since one that does not know it could not read them.
  final bool protobuf;
  bool _protobufBodies = false;

  Tokens? _tokens;
  Future<Tokens>? _refreshing;
  bool _signedOut = false;

  /// The headers of the last authenticated answer, or null before one.
  RateLimit? lastRateLimit;

  /// The token and revocation endpoints from the server's OAuth metadata,
  /// once fetched. Null uses the contract's paths.
  Uri? tokenEndpoint;
  Uri? revocationEndpoint;

  Tokens? get tokens => _tokens;
  bool get signedOut => _signedOut;

  /// How many times a `rate_limited` request is sent again after waiting
  /// the time the server asked for.
  static const _rateLimitRetries = 2;

  /// The longest the client waits on its own for a rate limit. A longer
  /// wait is the caller's to schedule, so the error is passed up.
  static const _maxRateLimitWait = Duration(seconds: 30);

  /// A token is refreshed this long before it runs out, so a request
  /// already on its way does not arrive with a dead one.
  static const _refreshMargin = Duration(seconds: 60);

  Uri uri(String path, [Map<String, Object?>? query]) {
    final q = <String, String>{
      if (query != null)
        for (final e in query.entries)
          if (e.value != null) e.key: '${e.value}',
    };
    return base.replace(path: path, queryParameters: q.isEmpty ? null : q);
  }

  /// Sets the pair after sign-in.
  Future<void> setTokens(Tokens t) async {
    _tokens = t;
    _signedOut = false;
    await onTokens?.call(t);
  }

  /// A valid access token, refreshed first if it is about to run out.
  /// The live connection calls this before each upgrade.
  Future<String> accessToken() async {
    if (_signedOut) throw SignedOutException();
    final t = _tokens;
    if (t == null) throw SignedOutException();
    if (t.refresh.isNotEmpty &&
        t.expiresAt.isBefore(_now().add(_refreshMargin))) {
      return (await refresh()).access;
    }
    return t.access;
  }

  /// Rotates the pair (docs/AUTH.md section 11.3). Concurrent callers
  /// share one request: a refresh token works once, so two refreshes at
  /// the same moment would spend it twice.
  Future<Tokens> refresh() => _refreshing ??= _doRefresh().whenComplete(() {
    _refreshing = null;
  });

  Future<Tokens> _doRefresh() async {
    final current = _tokens;
    if (current == null || current.refresh.isEmpty) {
      _endSession();
      throw SignedOutException();
    }
    try {
      final j = await oauthPost({
        'grant_type': 'refresh_token',
        'refresh_token': current.refresh,
      });
      final next = Tokens.fromResponse(j, _now());
      // Saved before use: if the app died between the two, the old pair
      // would be spent and the new one lost.
      await setTokens(next);
      return next;
    } on OAuthException catch (e) {
      if (e.error == 'invalid_grant') {
        _endSession();
        throw SignedOutException();
      }
      rethrow;
    }
  }

  void _endSession() {
    if (_signedOut) return;
    _signedOut = true;
    onSignedOut?.call();
  }

  /// Posts a form to the token endpoint and returns its JSON, or throws
  /// an [OAuthException] in the OAuth error shape.
  Future<Json> oauthPost(
    Map<String, String> form, {
    Op op = Ops.exchangeToken,
  }) async {
    final http.Response res;
    // The endpoints the server's metadata names (docs/AUTH.md 11.1), set
    // by `lib/core/oauth_metadata.dart`; the contract's paths until then.
    final target = op.id == Ops.revokeOAuthToken.id
        ? (revocationEndpoint ?? uri(op.path))
        : (tokenEndpoint ?? uri(op.path));
    try {
      res = await _http.post(
        target,
        headers: {if (protobuf) 'Accept': Wire.contentType},
        body: form,
      );
    } on Object catch (e) {
      throw ApiException.network(e);
    }
    final body = _decode(res, op);
    if (res.statusCode == 200) return body is Map ? body.cast() : <String, dynamic>{};
    if (body is Map && body['error'] is String) {
      throw OAuthException(
        body['error'] as String,
        body['error_description'] as String?,
      );
    }
    throw ApiException.fromResponse(res.statusCode, body);
  }

  /// Calls [op] and returns the decoded JSON body (an empty map for an
  /// answer with no body).
  ///
  /// [auth] false is for the few operations open to anyone (`getServer`).
  Future<Json> call(
    Op op, {
    Map<String, String> path = const {},
    Map<String, Object?>? query,
    Object? body,
    bool auth = true,
  }) async {
    // The body as the operation's protobuf message where it is one;
    // as JSON otherwise (wire.dart says when).
    final proto = body != null && protobuf && _protobufBodies
        ? Wire.encodeRequest(op.id, body)
        : null;
    final res = await _send(
      op.method,
      uri(op.fill(path), query),
      body: proto ?? (body == null ? null : utf8.encode(jsonEncode(body))),
      contentType: body == null
          ? null
          : (proto != null ? Wire.contentType : 'application/json'),
      auth: auth,
      op: op,
    );
    final decoded = _decode(res, op);
    return decoded is Map
        ? decoded.cast<String, dynamic>()
        : <String, dynamic>{};
  }

  /// Fetches the bytes at a path the server gave (`url`, `thumb_url`,
  /// `avatar_url`), with the same credential as the API.
  Future<Uint8List> download(String serverPath) async {
    final res = await _send('GET', base.resolve(serverPath), auth: true);
    return res.bodyBytes;
  }

  /// Sends a multipart form (`uploadFile`) and returns the decoded JSON.
  /// [onProgress] is told the bytes handed to the connection so far and
  /// the total. A 401 refreshes once and sends again, as [call] does;
  /// other refusals are thrown.
  Future<Json> upload(
    Op op, {
    Map<String, String> path = const {},
    Map<String, String> fields = const {},
    required List<UploadPart> parts,
    void Function(int sent, int total)? onProgress,
  }) async {
    var refreshed = false;
    while (true) {
      final form = http.MultipartRequest(op.method, uri(op.fill(path)));
      form.fields.addAll(fields);
      for (final p in parts) {
        form.files.add(
          http.MultipartFile.fromBytes(p.field, p.bytes, filename: p.name),
        );
      }
      form.headers['Authorization'] = 'Bearer ${await accessToken()}';
      if (protobuf) form.headers['Accept'] = Wire.contentType;
      final total = form.contentLength;
      // finalize() is what sets the multipart Content-Type with its
      // boundary, so the headers are copied after it.
      final body = form.finalize();
      final req = http.StreamedRequest(op.method, form.url)
        ..headers.addAll(form.headers)
        ..contentLength = total;
      var sent = 0;
      body.listen(
        (chunk) {
          sent += chunk.length;
          onProgress?.call(sent, total);
          req.sink.add(chunk);
        },
        onDone: req.sink.close,
        onError: req.sink.addError,
      );
      final http.Response res;
      try {
        res = await http.Response.fromStream(await _http.send(req));
      } on SignedOutException {
        rethrow;
      } on Object catch (e) {
        throw ApiException.network(e);
      }
      _readRateLimit(res.headers);
      if (res.statusCode < 300) {
        final d = _decode(res, op);
        return d is Map ? d.cast<String, dynamic>() : <String, dynamic>{};
      }
      final err = ApiException.fromResponse(res.statusCode, _decode(res, op));
      if (err.code == 'unauthenticated' && !refreshed) {
        refreshed = true;
        await refresh();
        continue;
      }
      throw err;
    }
  }

  Future<http.Response> _send(
    String method,
    Uri url, {
    List<int>? body,
    String? contentType,
    required bool auth,
    Op? op,
  }) async {
    var refreshed = false;
    for (var attempt = 0; ; attempt++) {
      final req = http.Request(method, url);
      if (body != null) req.bodyBytes = body;
      if (contentType != null) req.headers['Content-Type'] = contentType;
      if (protobuf && op != null) req.headers['Accept'] = Wire.contentType;
      if (auth) req.headers['Authorization'] = 'Bearer ${await accessToken()}';

      final http.Response res;
      try {
        res = await http.Response.fromStream(await _http.send(req));
      } on SignedOutException {
        rethrow;
      } on Object catch (e) {
        throw ApiException.network(e);
      }
      if (auth) _readRateLimit(res.headers);
      if (res.statusCode < 300 || res.statusCode == 304) return res;

      final err = ApiException.fromResponse(res.statusCode, _decode(res, op));
      // A 401 `unauthenticated` with a token we believed in: it was
      // revoked or the clocks disagree. One refresh, one more try.
      if (auth && err.code == 'unauthenticated' && !refreshed) {
        refreshed = true;
        await refresh();
        continue;
      }
      // `rate_limited` means the request was not performed, so sending
      // it again is safe for every method. `slow_mode` is the channel's
      // rule for one person and is shown to them instead.
      if (err.code == 'rate_limited' && attempt < _rateLimitRetries) {
        final wait = Duration(seconds: err.retryAfterSeconds ?? 1);
        if (wait <= _maxRateLimitWait) {
          await _sleep(wait);
          continue;
        }
      }
      throw err;
    }
  }

  void _readRateLimit(Map<String, String> h) {
    final limit = int.tryParse(h['ratelimit-limit'] ?? '');
    final remaining = int.tryParse(h['ratelimit-remaining'] ?? '');
    final reset = int.tryParse(h['ratelimit-reset'] ?? '');
    if (limit != null && remaining != null && reset != null) {
      lastRateLimit = RateLimit(limit, remaining, reset);
    }
  }

  /// The body of an answer as the JSON value it is, whichever way it
  /// came: protobuf is mapped to exactly what `jsonDecode` would have
  /// given (wire.dart), so nothing after this line knows the difference.
  Object? _decode(http.Response res, [Op? op]) {
    if (res.bodyBytes.isEmpty) return null;
    if (op != null && Wire.isProtobuf(res.headers['content-type'])) {
      _protobufBodies = true;
      try {
        return Wire.decodeResponse(op.id, res.statusCode, res.bodyBytes);
      } on Object {
        return null;
      }
    }
    try {
      return jsonDecode(utf8.decode(res.bodyBytes));
    } on Object {
      return null;
    }
  }

  /// Fetches every page of a cursor-paged list (docs/API.md section 4):
  /// `limit` and `cursor` in, the list under [key] and `next_cursor` out.
  /// A cursor is opaque and is never kept.
  Future<List<T>> allPages<T>(
    Op op,
    String key,
    T Function(Json) parse, {
    Map<String, String> path = const {},
    Map<String, Object?> query = const {},
    int limit = 200,
  }) async {
    final out = <T>[];
    String? cursor;
    do {
      final j = await call(
        op,
        path: path,
        query: {...query, 'limit': limit, 'cursor': cursor},
      );
      out.addAll(listOf(j[key], parse));
      cursor = j['next_cursor'] as String?;
    } while (cursor != null && cursor.isNotEmpty);
    return out;
  }

  void close() => _http.close();
}
