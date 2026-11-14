import '../models/json.dart';
import '../themes/theme.dart';
import 'client.dart';
import 'operations.dart';

/// The server's default dark and light themes (`DefaultThemes`), each a
/// copy (`CardTheme`); either may be absent.
class DefaultThemes {
  const DefaultThemes({this.dark, this.light});

  factory DefaultThemes.fromJson(Json? j, {String? serverAddress}) =>
      DefaultThemes(
        dark: object(j?['dark']) == null
            ? null
            : ChurrTheme.fromJson(
                object(j!['dark'])!,
                serverAddress: serverAddress,
              ),
        light: object(j?['light']) == null
            ? null
            : ChurrTheme.fromJson(
                object(j!['light'])!,
                serverAddress: serverAddress,
              ),
      );

  final ChurrTheme? dark;
  final ChurrTheme? light;

  ChurrTheme? of({required bool dark}) => dark ? this.dark : light;

  Json toJson() => {'dark': ?dark?.toJson(), 'light': ?light?.toJson()};
}

/// One page of the theme library.
class ThemePage {
  const ThemePage(this.themes, this.nextCursor);
  final List<ChurrTheme> themes;
  final String? nextCursor;
}

/// Themes (ruling R61): the caller's own, the library, defaults.
extension ThemesApi on ApiClient {
  String get _address => base.toString();

  ChurrTheme _theme(Json j) => ChurrTheme.fromJson(
    object(j['theme']) ?? const {},
    serverAddress: _address,
  );

  Future<List<ChurrTheme>> listMyThemes() async => listOf(
    (await call(Ops.listMyThemes))['themes'],
    (j) => ChurrTheme.fromJson(j, serverAddress: _address),
  );

  Future<ChurrTheme> createTheme(ChurrTheme t) async =>
      _theme(await call(Ops.createTheme, body: t.toInput()));

  Future<ChurrTheme> getTheme(String id) async =>
      _theme(await call(Ops.getTheme, path: {'id': id}));

  Future<ChurrTheme> updateTheme(String id, ChurrTheme t) async =>
      _theme(await call(Ops.updateTheme, path: {'id': id}, body: t.toInput()));

  Future<void> deleteTheme(String id) =>
      call(Ops.deleteTheme, path: {'id': id});

  Future<ChurrTheme> publishTheme(String id) async =>
      _theme(await call(Ops.publishTheme, path: {'id': id}));

  Future<ChurrTheme> unpublishTheme(String id) async =>
      _theme(await call(Ops.unpublishTheme, path: {'id': id}));

  /// One page of the library; [q] matches names, ignoring case.
  Future<ThemePage> listThemeLibrary({
    String? q,
    String? cursor,
    int limit = 50,
  }) async {
    final j = await call(
      Ops.listThemeLibrary,
      query: {
        if (q != null && q.isNotEmpty) 'q': q,
        'cursor': ?cursor,
        'limit': limit,
      },
    );
    return ThemePage(
      listOf(
        j['themes'],
        (t) => ChurrTheme.fromJson(t, serverAddress: _address),
      ),
      j['next_cursor'] as String?,
    );
  }

  /// A copy of a theme (from the library, or any by its link) becomes
  /// the caller's own.
  Future<ChurrTheme> addTheme(String id) async =>
      _theme(await call(Ops.addTheme, path: {'id': id}));

  Future<void> removeLibraryTheme(String id) =>
      call(Ops.removeLibraryTheme, path: {'id': id});

  /// Sets ([themeId]) or clears the server's default of a kind.
  Future<DefaultThemes> setDefaultTheme(String kind, {String? themeId}) async =>
      DefaultThemes.fromJson(
        object(
          (await call(
            Ops.setDefaultTheme,
            body: {'kind': kind, 'theme_id': ?themeId},
          ))['default_themes'],
        ),
        serverAddress: _address,
      );
}
