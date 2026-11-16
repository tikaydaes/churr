import 'dart:convert';
import 'dart:io' show gzip;

import 'models/json.dart';

/// "Set up another device" (docs/CLIENTS.md 8.2): the app preferences and
/// the addresses of the signed-in servers, carried in one or more QR
/// codes. It holds no token and no key. How each server's conversations
/// are arranged travels with no code: each server keeps it (ruling R83).
class SetupPayload {
  SetupPayload({
    this.prefs = const {},
    this.servers = const [],
  });

  factory SetupPayload.fromJson(Json j) => SetupPayload(
    prefs: (object(j['prefs']) ?? const {}).map((k, v) => MapEntry(k, '$v')),
    servers: strings(j['servers']),
  );

  /// The app preferences, by their `app.db` key. Only keys in
  /// [carriedPrefs] are taken.
  final Map<String, String> prefs;

  /// Server addresses, in the person's order.
  final List<String> servers;

  /// The preferences that travel. Per-device things (the sidebar's width,
  /// what is folded, sounds) stay behind.
  static const carriedPrefs = {
    'layout',
    'default_show',
    'show_hidden',
    'theme',
    // The theme choice itself: which dark and light themes, "use each
    // server's default", the copies of saved themes in use (so the new
    // device draws them before it has signed in anywhere), and the older
    // palette choice the built-in pairs fall back to.
    'theme_dark',
    'theme_light',
    'theme_per_server',
    'theme_copies',
    'palette',
    'text_scale',
    'enter_sends',
    'language',
    'shared_opt_out',
  };

  Json toJson() => {
    'v': 1,
    'prefs': prefs,
    'servers': servers,
  };

  /// The QR codes' texts: `churr://setup?p=<i>&n=<count>&d=<part>`, the
  /// JSON gzipped and in base64url, cut into parts small enough to scan.
  List<String> encode({int partSize = 800}) {
    final data = base64Url
        .encode(gzip.encode(utf8.encode(jsonEncode(toJson()))))
        .replaceAll('=', '');
    final parts = <String>[];
    for (var i = 0; i < data.length; i += partSize) {
      parts.add(
        data.substring(
          i,
          i + partSize > data.length ? data.length : i + partSize,
        ),
      );
    }
    if (parts.isEmpty) parts.add('');
    return [
      for (var i = 0; i < parts.length; i++)
        Uri(
          scheme: 'churr',
          host: 'setup',
          queryParameters: {
            'p': '${i + 1}',
            'n': '${parts.length}',
            'd': parts[i],
          },
        ).toString(),
    ];
  }

  static SetupPayload decode(List<String> parts) {
    var data = parts.join();
    while (data.length % 4 != 0) {
      data += '=';
    }
    return SetupPayload.fromJson(
      jsonDecode(utf8.decode(gzip.decode(base64Url.decode(data)))) as Json,
    );
  }
}

/// Gathers the parts of a set-up code as they are scanned, in any order.
class SetupCollector {
  final _parts = <int, String>{};
  int? total;

  /// Takes one scanned text. Returns false if it is not a set-up code.
  bool add(String text) {
    final u = Uri.tryParse(text.trim());
    if (u == null || u.scheme != 'churr' || u.host != 'setup') return false;
    final p = int.tryParse(u.queryParameters['p'] ?? '');
    final n = int.tryParse(u.queryParameters['n'] ?? '');
    final d = u.queryParameters['d'];
    if (p == null || n == null || d == null || p < 1 || p > n) return false;
    if (total != null && total != n) _parts.clear();
    total = n;
    _parts[p] = d;
    return true;
  }

  int get received => _parts.length;
  bool get complete => total != null && _parts.length == total;

  SetupPayload? get payload => complete
      ? SetupPayload.decode([for (var i = 1; i <= total!; i++) _parts[i]!])
      : null;
}

/// A server's pairing code (docs/AUTH.md 11.4): `churr://pair?server=…&code=…`.
/// From an unlocked web chat it may also carry `secret`, the pairing
/// secret of docs/SEALED.md 13.7 (32 bytes, base64url), which never goes
/// to the server.
class PairingCode {
  PairingCode(this.server, this.code, [this.secret]);
  final String server;
  final String code;
  final String? secret;

  static PairingCode? parse(String text) {
    final u = Uri.tryParse(text.trim());
    if (u == null || u.scheme != 'churr' || u.host != 'pair') return null;
    final s = u.queryParameters['server'], c = u.queryParameters['code'];
    if (s == null || s.isEmpty || c == null || c.isEmpty) return null;
    return PairingCode(s, c, u.queryParameters['secret']);
  }
}
