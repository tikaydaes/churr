import 'dart:convert';

/// An app's token pair for one server (docs/AUTH.md section 11.3). The
/// access token works for an hour; the refresh token works once.
class Tokens {
  Tokens({
    required this.access,
    required this.refresh,
    required this.expiresAt,
    this.handover,
  });

  /// From a token endpoint answer, counting `expires_in` from [now].
  factory Tokens.fromResponse(Map<String, dynamic> j, DateTime now) => Tokens(
    access: j['access_token'] as String,
    refresh: j['refresh_token'] as String? ?? '',
    expiresAt: now.add(
      Duration(seconds: (j['expires_in'] as num?)?.toInt() ?? 3600),
    ),
    handover: j['handover'] as String?,
  );

  final String access;

  /// Empty for a development token added without one; such a session
  /// simply ends when the access token does.
  final String refresh;
  final DateTime expiresAt;

  /// Sealed chats: the backup key sealed to the box key the app sent,
  /// when the sign-in could open it (base64url). Not stored.
  final String? handover;

  Map<String, dynamic> toJson() => {
    'access': access,
    'refresh': refresh,
    'expires': expiresAt.millisecondsSinceEpoch,
  };

  static Tokens? fromStored(String raw) {
    try {
      final j = jsonDecode(raw) as Map<String, dynamic>;
      return Tokens(
        access: j['access'] as String,
        refresh: j['refresh'] as String,
        expiresAt: DateTime.fromMillisecondsSinceEpoch(j['expires'] as int),
      );
    } on Object {
      return null;
    }
  }
}

/// Where secrets are kept: the platform key store in the app
/// (`lib/platform/keychain_store.dart`), memory in tests.
abstract class SecretStore {
  Future<String?> read(String key);
  Future<void> write(String key, String value);
  Future<void> delete(String key);
}

/// A [SecretStore] in memory, for tests.
class MemorySecretStore implements SecretStore {
  final Map<String, String> values = {};

  @override
  Future<String?> read(String key) async => values[key];
  @override
  Future<void> write(String key, String value) async => values[key] = value;
  @override
  Future<void> delete(String key) async => values.remove(key);
}

/// The tokens of each signed-in server, one entry per server's local
/// identifier. The foreground app and (later) the background entry points
/// read and write the same entries, which is how each sees a pair the
/// other has rotated.
class SessionStore {
  SessionStore(this.secrets);
  final SecretStore secrets;

  String _key(String localId) => 'churr.tokens.$localId';

  Future<Tokens?> load(String localId) async {
    final raw = await secrets.read(_key(localId));
    return raw == null ? null : Tokens.fromStored(raw);
  }

  Future<void> save(String localId, Tokens t) =>
      secrets.write(_key(localId), jsonEncode(t.toJson()));

  Future<void> clear(String localId) => secrets.delete(_key(localId));
}
