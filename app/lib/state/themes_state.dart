import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';

import '../core/api/themes.dart';
import '../core/live.dart';
import '../core/themes/palette.dart';
import '../core/themes/theme.dart';
import 'app_prefs.dart';
import 'app_state.dart';
import 'server_state.dart';

/// Which theme the app draws with, and the person's saved themes on every
/// signed-in server (ruling R61, docs/STYLE.md "Themes").
///
/// The choice is an app preference on this device (`app.db`):
///
/// - `theme` (dark, light or follow the system) says which of two themes
///   is in use;
/// - `theme_dark` and `theme_light` name the chosen dark and light theme
///   ([ChurrTheme.ref]): a built-in one or a saved one on some server;
/// - `theme_per_server` "use each server's default": the selected
///   server's default of that kind, where it has one, so each server
///   looks like itself;
/// - `theme_copies` keeps a copy of every saved theme in use, and
///   `server_default_themes` each server's defaults, so the app starts
///   with the right colours offline.
///
/// Saved themes live on the server they were made on; [mine] lists them
/// per server, fetched when the picker opens and after each `ready`.
class ThemesState extends ChangeNotifier {
  ThemesState(this.app) {
    app.addListener(_onApp);
  }

  final AppState app;
  AppPrefs get prefs => app.prefs;

  /// Saved themes by server local id; absent until fetched.
  final Map<String, List<ChurrTheme>> mine = {};

  /// Why fetching a server's themes failed, by server local id.
  final Map<String, String> errors = {};

  /// A theme applied without saving ("Try"), until [stopTrying].
  ChurrTheme? trying;

  final Map<String, LiveState> _seen = {};

  // ---- what is chosen ----

  bool get perServer => prefs.raw('theme_per_server') == '1';
  set perServer(bool v) {
    unawaited(prefs.putRaw('theme_per_server', v ? '1' : '0'));
    _keepCopies();
  }

  /// The chosen dark and light themes' refs. Before any choice: the
  /// built-in pair, high distinction if that palette was chosen before
  /// themes existed.
  String get darkRef =>
      prefs.raw('theme_dark') ??
      (prefs.palette == Palette.highDistinction
          ? BuiltInThemes.darkHd.ref
          : BuiltInThemes.dark.ref);
  String get lightRef =>
      prefs.raw('theme_light') ??
      (prefs.palette == Palette.highDistinction
          ? BuiltInThemes.lightHd.ref
          : BuiltInThemes.light.ref);

  Map<String, ChurrTheme> get _copies {
    final raw = prefs.raw('theme_copies');
    if (raw == null) return {};
    try {
      final j = jsonDecode(raw) as Map;
      return {
        for (final e in j.entries)
          '${e.key}': ChurrTheme.fromJson((e.value as Map).cast()),
      };
    } on Object {
      return {};
    }
  }

  /// The theme a ref names: built-in, a fetched saved theme, or the
  /// local copy. Null if none of them has it.
  ChurrTheme? byRef(String ref) {
    final b = BuiltInThemes.byRef(ref);
    if (b != null) return b;
    for (final list in mine.values) {
      for (final t in list) {
        if (t.ref == ref) return t;
      }
    }
    return _copies[ref];
  }

  /// The chosen theme of a kind (falling back to the built-in one when it
  /// is gone or no longer passes).
  ChurrTheme chosen({required bool dark}) {
    final t = byRef(dark ? darkRef : lightRef);
    if (t != null && t.dark == dark && t.usable) return t;
    return dark
        ? (prefs.palette == Palette.highDistinction
              ? BuiltInThemes.darkHd
              : BuiltInThemes.dark)
        : (prefs.palette == Palette.highDistinction
              ? BuiltInThemes.lightHd
              : BuiltInThemes.light);
  }

  /// A server's default themes: what it said this session, else the
  /// local copy.
  DefaultThemes defaultsOf(ServerState s) {
    final info = s.info;
    if (info != null) {
      return DefaultThemes.fromJson(
        info.defaultThemes,
        serverAddress: s.entry.address,
      );
    }
    final raw = _storedDefaults[s.entry.address];
    return DefaultThemes.fromJson(raw, serverAddress: s.entry.address);
  }

  Map<String, Map<String, dynamic>> get _storedDefaults {
    final raw = prefs.raw('server_default_themes');
    if (raw == null) return {};
    try {
      return {
        for (final e in (jsonDecode(raw) as Map).entries)
          '${e.key}': (e.value as Map).cast<String, dynamic>(),
      };
    } on Object {
      return {};
    }
  }

  /// The server whose look "use each server's default" follows: the open
  /// conversation's, else the selected one.
  ServerState? get followedServer {
    final open = app.open;
    if (open != null) return app.server(open.serverId) ?? app.selectedServer;
    return app.selectedServer;
  }

  /// The theme in use for one brightness: one being tried; else the
  /// followed server's default of that kind under "use each server's
  /// default"; else the chosen one.
  ChurrTheme current({required bool dark}) {
    final t = trying;
    if (t != null) return t;
    if (perServer) {
      final s = followedServer;
      final d = s == null ? null : defaultsOf(s).of(dark: dark);
      if (d != null && d.usable) return d;
    }
    return chosen(dark: dark);
  }

  /// Makes [t] the theme in use: it becomes the chosen theme of its kind,
  /// and unless the app follows the system, its kind is the one shown.
  Future<void> choose(ChurrTheme t) async {
    if (!t.usable) return;
    trying = null;
    await prefs.putRaw(t.dark ? 'theme_dark' : 'theme_light', t.ref);
    if (prefs.theme != ThemeChoice.system) {
      prefs.theme = t.dark ? ThemeChoice.dark : ThemeChoice.light;
    }
    _keepCopies(extra: t);
    notifyListeners();
  }

  /// Makes [t] the chosen theme of its kind without changing whether
  /// dark or light is shown.
  Future<void> setChosen(ChurrTheme t) async {
    if (!t.usable) return;
    await prefs.putRaw(t.dark ? 'theme_dark' : 'theme_light', t.ref);
    _keepCopies(extra: t);
    notifyListeners();
  }

  /// Applies [t] without saving or choosing it.
  void tryTheme(ChurrTheme t) {
    if (!t.usable) return;
    trying = t;
    notifyListeners();
  }

  void stopTrying() {
    trying = null;
    notifyListeners();
  }

  /// Every theme the person can choose: the built-in four, then each
  /// server's saved ones (marked with their server by the screen).
  List<ChurrTheme> get all => [
    ...BuiltInThemes.all,
    for (final s in app.servers) ...?mine[s.localId],
  ];

  ServerState? serverOf(ChurrTheme t) {
    for (final s in app.servers) {
      if (s.entry.address == t.serverAddress) return s;
    }
    return null;
  }

  // ---- fetching and changing saved themes ----

  /// Fetches the person's themes on [s] (`listMyThemes`).
  Future<void> refresh(ServerState s) async {
    try {
      mine[s.localId] = await s.api.listMyThemes();
      errors.remove(s.localId);
    } on Exception catch (e) {
      errors[s.localId] = '$e';
    }
    _keepCopies();
    notifyListeners();
  }

  Future<void> refreshAll() =>
      Future.wait([for (final s in app.servers) refresh(s)]);

  void _put(ServerState s, ChurrTheme t) {
    final list = [...?mine[s.localId]];
    final i = list.indexWhere((x) => x.id == t.id);
    i < 0 ? list.add(t) : list[i] = t;
    mine[s.localId] = list;
    _keepCopies();
    notifyListeners();
  }

  /// Saves [t] on [s]: a new theme, or a change to one of the person's
  /// own there. Throws the server's refusal (`ApiException`).
  Future<ChurrTheme> save(ServerState s, ChurrTheme t) async {
    final ownHere =
        t.id.isNotEmpty && t.serverAddress == s.entry.address && !t.builtIn;
    final saved = ownHere
        ? await s.api.updateTheme(t.id, t)
        : await s.api.createTheme(t);
    _put(s, saved);
    return saved;
  }

  Future<void> delete(ChurrTheme t) async {
    final s = serverOf(t);
    if (s == null) return;
    await s.api.deleteTheme(t.id);
    mine[s.localId] = [...?mine[s.localId]]..removeWhere((x) => x.id == t.id);
    // A deleted theme in use gives way to the built-in one of its kind.
    if (darkRef == t.ref) {
      await prefs.putRaw('theme_dark', BuiltInThemes.dark.ref);
    }
    if (lightRef == t.ref) {
      await prefs.putRaw('theme_light', BuiltInThemes.light.ref);
    }
    _keepCopies();
    notifyListeners();
  }

  Future<ChurrTheme> setPublished(ChurrTheme t, bool on) async {
    final s = serverOf(t)!;
    final next = on
        ? await s.api.publishTheme(t.id)
        : await s.api.unpublishTheme(t.id);
    _put(s, next);
    return next;
  }

  /// Adds a copy of a theme on [s] (from its library, a card or a link).
  Future<ChurrTheme> add(ServerState s, String id) async {
    final t = await s.api.addTheme(id);
    _put(s, t);
    return t;
  }

  // ---- local copies ----

  /// Keeps a copy of every saved theme in use and every server's
  /// defaults in `app.db`, so the next start has them offline.
  void _keepCopies({ChurrTheme? extra}) {
    final copies = _copies;
    final want = <String, ChurrTheme>{};
    for (final ref in [darkRef, lightRef]) {
      if (ref.startsWith('builtin:')) continue;
      final t = (extra?.ref == ref ? extra : null) ?? byRef(ref);
      if (t != null) want[ref] = t;
    }
    final encoded = jsonEncode({
      for (final e in want.entries) e.key: e.value.toJson(),
    });
    final before = jsonEncode({
      for (final e in copies.entries) e.key: e.value.toJson(),
    });
    if (encoded != before) unawaited(prefs.putRaw('theme_copies', encoded));
  }

  void _onApp() {
    // Each server's defaults, as it last said them, are kept locally.
    final stored = _storedDefaults;
    var changed = false;
    for (final s in app.servers) {
      final info = s.info;
      if (info == null) continue;
      final j = info.defaultThemes ?? const <String, dynamic>{};
      if (jsonEncode(stored[s.entry.address] ?? const {}) != jsonEncode(j)) {
        stored[s.entry.address] = j;
        changed = true;
      }
    }
    if (changed) {
      unawaited(prefs.putRaw('server_default_themes', jsonEncode(stored)));
    }
    // After each `ready` the saved themes are fetched again.
    for (final s in app.servers) {
      final was = _seen[s.localId];
      _seen[s.localId] = s.liveState;
      if (s.liveState == LiveState.ready && was != LiveState.ready) {
        unawaited(refresh(s));
      }
    }
  }

  @override
  void dispose() {
    app.removeListener(_onApp);
    super.dispose();
  }
}

/// A new theme made from [from], under [name]: the editor's starting
/// point. Built-in themes cannot be edited, only copied.
ChurrTheme startFrom(ChurrTheme from, String name) => ChurrTheme(
  id: '',
  name: name,
  kind: from.kind,
  colours: {for (final k in themeKeys) k: from.colours[k]!},
);
