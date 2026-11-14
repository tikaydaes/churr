/// Themes as the app keeps them (ruling R61, docs/STYLE.md "Themes"): the
/// four built-in themes, saved themes from a server, server defaults, and
/// the `.churrtheme` file. Every theme reaches the screen through
/// [themeTokens], so the built-in and custom ones take one path.
///
/// Pure Dart: no Flutter.
library;

import 'dart:convert';

import 'palette.dart';

/// One theme: a name, a kind and the nine base colours.
class ChurrTheme {
  const ChurrTheme({
    required this.id,
    required this.name,
    required this.kind,
    required this.colours,
    this.builtIn = false,
    this.ownerId,
    this.authorId,
    this.published = false,
    this.serverAddress,
  });

  /// A `Theme` or `CardTheme` from the API, kept on [serverAddress].
  factory ChurrTheme.fromJson(
    Map<String, dynamic> j, {
    String? serverAddress,
  }) => ChurrTheme(
    id: j['id'] as String? ?? '',
    name: j['name'] as String? ?? '',
    kind: j['kind'] == kindLight ? kindLight : kindDark,
    colours: {
      for (final e in ((j['colours'] as Map?) ?? const {}).entries)
        '${e.key}': '${e.value}'.toLowerCase(),
    },
    builtIn: j['built_in'] == true,
    ownerId: j['owner_id'] as String?,
    authorId: j['author_id'] as String? ?? j['owner_id'] as String?,
    published: j['published'] == true,
    serverAddress: serverAddress ?? j['server'] as String?,
  );

  /// The server's id; for a built-in theme its short name (`dark`,
  /// `light`, `dark-hd`, `light-hd`); empty for one not saved anywhere.
  final String id;
  final String name;

  /// [kindDark] or [kindLight].
  final String kind;

  /// The nine base colours by key, `#rrggbb`.
  final Map<String, String> colours;
  final bool builtIn;
  final String? ownerId;

  /// Who made it: the owner, or for a card or default the account it was
  /// copied from.
  final String? authorId;
  final bool published;

  /// The server it is saved on; null for built-in and unsaved themes.
  final String? serverAddress;

  bool get dark => kind == kindDark;

  /// How the app's preferences name it: `builtin:dark`,
  /// `s:https://chat.example.org#<id>`, or `file:<hash>` for one only
  /// tried or imported.
  String get ref => builtIn
      ? 'builtin:$id'
      : serverAddress != null && id.isNotEmpty
      ? 's:$serverAddress#$id'
      : 'file:${Object.hashAll([name, kind, ...themeKeys.map((k) => colours[k])])}';

  /// The colours parsed; null if they do not parse.
  Colours? get parsed => parseColours(colours).$1;

  /// Whether it may be applied: built-in themes always; others when their
  /// colours parse, their kind fits and every pairing passes.
  bool get usable {
    if (builtIn) return true;
    final c = parsed;
    return c != null && valid(kind, c);
  }

  /// The first thing wrong with it, as the server words it; null if none.
  String? get problem {
    if (builtIn) return null;
    final (c, fault) = parseColours(colours);
    if (c == null) return fault!.message;
    if (!kindFits(kind, c)) {
      return kind == kindDark
          ? 'A dark theme needs a ground darker than its text.'
          : 'A light theme needs a ground lighter than its text.';
    }
    return check(kind, c)?.message;
  }

  ChurrTheme copyWith({
    String? id,
    String? name,
    String? kind,
    Map<String, String>? colours,
    bool? published,
    String? serverAddress,
    bool clearServer = false,
    bool builtIn = false,
  }) => ChurrTheme(
    id: id ?? this.id,
    name: name ?? this.name,
    kind: kind ?? this.kind,
    colours: colours ?? this.colours,
    builtIn: builtIn,
    ownerId: ownerId,
    authorId: authorId,
    published: published ?? this.published,
    serverAddress: clearServer ? null : (serverAddress ?? this.serverAddress),
  );

  /// Everything, for the app's local copy.
  Map<String, dynamic> toJson() => {
    'id': id,
    'name': name,
    'kind': kind,
    'colours': colours,
    if (builtIn) 'built_in': true,
    'owner_id': ?ownerId,
    'author_id': ?authorId,
    if (published) 'published': true,
    'server': ?serverAddress,
  };

  /// What `createTheme` and `updateTheme` take.
  Map<String, dynamic> toInput() => {
    'name': name,
    'kind': kind,
    'colours': {for (final k in themeKeys) k: colours[k]},
  };

  @override
  bool operator ==(Object other) =>
      other is ChurrTheme &&
      other.ref == ref &&
      other.name == name &&
      _sameColours(other);

  bool _sameColours(ChurrTheme o) =>
      o.kind == kind && themeKeys.every((k) => o.colours[k] == colours[k]);

  @override
  int get hashCode => ref.hashCode;
}

// ---- the built-in themes ----

/// The guide's hand-set token tables (docs/STYLE.md "Tokens" and "The
/// high-distinction palette"), every token the app draws with. The
/// built-in themes are these tables, not derived.
abstract final class BuiltInTokens {
  static const dark = {
    'bg': '#0d1026', 'panel': '#161a3d', 'raise': '#1d2250', //
    'line': '#2c3170', 'ink': '#efeaff', 'dim': '#a09dc9',
    'faint': '#8b88b8', 'edge': '#6368bd', 'lav': '#b9a0ff',
    'cyan': '#5df0e4', 'pink': '#ff5ccf', 'yel': '#ffd95e',
    'on-lav': '#0d1026', 'on-cyan': '#0d1026', 'on-pink': '#0d1026',
    'on-yel': '#15173a', 'yel-ink': '#ffd95e', 'focus': '#ffd95e',
    'grid': '#1d2250',
  };

  static const light = {
    'bg': '#f5f2ff', 'panel': '#ffffff', 'raise': '#ebe6ff', //
    'line': '#c9c1ee', 'ink': '#15173a', 'dim': '#55527f',
    'faint': '#6b6894', 'edge': '#7e73c2', 'lav': '#5b3fd6',
    'cyan': '#006e66', 'pink': '#b3127f', 'yel': '#ffd95e',
    'on-lav': '#ffffff', 'on-cyan': '#ffffff', 'on-pink': '#ffffff',
    'on-yel': '#15173a',
    // Yellow is never text on light, and a yellow focus ring would not
    // reach 3:1 on the pale paper: the dark amber does both jobs.
    'yel-ink': '#7a5600', 'focus': '#7a5600', 'grid': '#dcd5f7',
  };

  /// High distinction: pink becomes orange and lavender blue, so the four
  /// roles differ along blue to yellow and in lightness.
  static const darkHighDistinction = {
    'lav': '#7fb2ff',
    'pink': '#ff9a4d',
    'yel': '#ffe066',
    'yel-ink': '#ffe066',
    'focus': '#ffe066',
  };
  static const lightHighDistinction = {
    'lav': '#1f56c4',
    'cyan': '#00626b',
    'pink': '#a8480a',
    'yel': '#ffe066',
  };

  static Map<String, String> of(String id) => switch (id) {
    'light' => light,
    'dark-hd' => {...dark, ...darkHighDistinction},
    'light-hd' => {...light, ...lightHighDistinction},
    _ => dark,
  };
}

ChurrTheme _builtIn(String id, String name) {
  final t = BuiltInTokens.of(id);
  return ChurrTheme(
    id: id,
    name: name,
    kind: id.startsWith('light') ? kindLight : kindDark,
    colours: {for (final k in themeKeys) k: t[k]!},
    builtIn: true,
  );
}

/// The four built-in themes. Their names here are the guide's; screens
/// show the localised ones ([ChurrTheme.id] tells which).
abstract final class BuiltInThemes {
  static final dark = _builtIn('dark', 'Dark');
  static final light = _builtIn('light', 'Light');
  static final darkHd = _builtIn('dark-hd', 'Dark, high distinction');
  static final lightHd = _builtIn('light-hd', 'Light, high distinction');
  static final all = [dark, light, darkHd, lightHd];

  static ChurrTheme? byRef(String ref) {
    for (final t in all) {
      if (t.ref == ref) return t;
    }
    return null;
  }
}

/// The app's token names (lib/ui/theme/tokens.dart `SchemeTokens.names`).
const appTokenNames = [
  'bg', 'panel', 'raise', 'line', 'edge', 'ink', 'dim', 'faint', //
  'lav', 'cyan', 'pink', 'yel', 'on-lav', 'on-cyan', 'on-pink', 'on-yel',
  'yel-ink', 'focus', 'grid',
];

/// Every token the app draws with, for one theme, as `#rrggbb`.
///
/// A built-in theme is its hand-set table. Any other is derived by
/// docs/STYLE.md "Themes": the nine base colours, the derived partner
/// colours, faint, edge and yel-ink, and what a theme does not name
/// (`line`, `grid`) from the built-in theme of the same kind. The focus
/// outline is yellow on dark and yel-ink on light, as in the built-ins.
/// A theme whose colours do not parse falls back to the built-in of its
/// kind (it can never have been applied: [ChurrTheme.usable]).
Map<String, String> themeTokens(ChurrTheme t) {
  if (t.builtIn) return Map.of(BuiltInTokens.of(t.id));
  final base = t.dark ? BuiltInTokens.dark : BuiltInTokens.light;
  final c = t.parsed;
  if (c == null) return Map.of(base);
  final d = derive(c);
  return {
    for (final k in themeKeys) k: c[k]!.hex,
    'on-lav': d.onLav.hex,
    'on-cyan': d.onCyan.hex,
    'on-pink': d.onPink.hex,
    'on-yel': d.onYel.hex,
    'faint': d.faint.hex,
    'edge': d.edge.hex,
    'yel-ink': d.yelInk.hex,
    'focus': t.dark ? c['yel']!.hex : d.yelInk.hex,
    'line': base['line']!,
    'grid': base['grid']!,
  };
}

// ---- the .churrtheme file ----

/// What a `.churrtheme` file is called and holds: JSON `{"name", "kind",
/// "colours"}` with the nine keys.
const churrThemeExtension = 'churrtheme';

/// The file's bytes for [t].
String encodeThemeFile(ChurrTheme t) =>
    const JsonEncoder.withIndent('  ').convert({
      'name': t.name,
      'kind': t.kind,
      'colours': {for (final k in themeKeys) k: t.colours[k]},
    });

/// Why a file was not taken.
class ThemeFileError implements Exception {
  const ThemeFileError(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Reads a `.churrtheme` file. Throws [ThemeFileError] with the reason
/// when it is not a theme, or one that fails the check (a theme that fails
/// cannot be applied, so the app does not take it).
ChurrTheme decodeThemeFile(String text) {
  final Object? j;
  try {
    j = jsonDecode(text);
  } on FormatException {
    throw const ThemeFileError('This file is not a Churr theme.');
  }
  if (j is! Map) throw const ThemeFileError('This file is not a Churr theme.');
  final name = j['name'];
  final kind = j['kind'];
  final colours = j['colours'];
  if (name is! String || name.trim().isEmpty || name.length > 64) {
    throw const ThemeFileError(
      'The theme has no name, or one longer than 64 characters.',
    );
  }
  if (kind != kindDark && kind != kindLight) {
    throw const ThemeFileError('The theme\'s kind must be "dark" or "light".');
  }
  if (colours is! Map) throw const ThemeFileError("Give the theme's colours.");
  final t = ChurrTheme(
    id: '',
    name: name.trim(),
    kind: kind as String,
    colours: {
      for (final e in colours.entries) '${e.key}': '${e.value}'.toLowerCase(),
    },
  );
  final p = t.problem;
  if (p != null) throw ThemeFileError(p);
  return t;
}

// ---- links ----

/// A theme link, `{origin}/themes/{id}` (STYLE.md "Sharing").
String themeLink(String serverAddress, String id) =>
    '${Uri.parse(serverAddress).origin}/themes/$id';

/// The origin and id of a theme link; null if [url] is not one.
(String origin, String id)? parseThemeLink(String url) {
  final u = Uri.tryParse(url);
  if (u == null || !u.hasScheme || u.host.isEmpty) return null;
  final seg = u.pathSegments.where((s) => s.isNotEmpty).toList();
  if (seg.length != 2 || seg[0] != 'themes') return null;
  if (!RegExp(r'^[0-9a-fA-F-]{36}$').hasMatch(seg[1])) return null;
  return (u.origin, seg[1]);
}
