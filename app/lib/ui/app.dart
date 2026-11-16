import 'package:flutter/material.dart';

import '../l10n/app_localizations.dart';
import '../state/account_prefs.dart';

import '../state/app_prefs.dart';
import '../state/app_state.dart';
import 'layout/shell.dart';
import 'scopes.dart';
import 'screens/lock/lock_screen.dart';
import 'screens/themes/theme_widgets.dart';
import 'screens/add_server/add_server_screen.dart';
import 'theme/shapes.dart';
import 'theme/theme.dart';
import 'widgets/controls.dart';

/// The root widget: hands the [AppState] down, picks the theme, and
/// shows the shell, or "Add a server" when none is signed in.
class ChurrApp extends StatelessWidget {
  const ChurrApp({super.key, required this.state});
  final AppState state;

  @override
  Widget build(BuildContext context) => AppScope(
    state: state,
    child: ListenableBuilder(
      listenable: Listenable.merge([
        state,
        state.prefs,
        state.themes,
        state.lock,
      ]),
      builder: (context, _) {
        final prefs = state.prefs;
        final palette = prefs.palette;
        // Built-in, saved, a server's default or one being tried: every
        // theme takes the same path (lib/core/themes/theme.dart).
        final light = state.themes.current(dark: false);
        final dark = state.themes.current(dark: true);
        return MaterialApp(
          title: 'Churr',
          debugShowCheckedModeBanner: false,
          // Wording comes from lib/l10n (README.md, "Wording").
          localizationsDelegates: L10n.localizationsDelegates,
          supportedLocales: L10n.supportedLocales,
          locale: prefs.language == null ? null : Locale(prefs.language!),
          theme: buildTheme(dark: false, palette: palette, theme: light),
          darkTheme: buildTheme(dark: true, palette: palette, theme: dark),
          // The system's "increase contrast": every line takes the edge
          // colour (docs/style-guide.html, "contrast and colour vision").
          highContrastTheme: buildTheme(
            dark: false,
            palette: palette,
            highContrast: true,
            theme: light,
          ),
          highContrastDarkTheme: buildTheme(
            dark: true,
            palette: palette,
            highContrast: true,
            theme: dark,
          ),
          themeAnimationDuration: Duration.zero,
          themeMode: switch (prefs.theme) {
            ThemeChoice.dark => ThemeMode.dark,
            ThemeChoice.light => ThemeMode.light,
            ThemeChoice.system => ThemeMode.system,
          },
          builder: (context, child) => MediaQuery.withClampedTextScaling(
            // The app follows the system text size, times the person's
            // own choice; layouts hold up to 200%.
            minScaleFactor: prefs.textScale,
            maxScaleFactor: 2.0 * prefs.textScale,
            // Locked, the lock screen takes the navigator's place: every
            // screen and route of the account leaves the tree
            // (lib/state/app_lock.dart). Any touch counts as use for the
            // automatic lock.
            child: Listener(
              behavior: HitTestBehavior.translucent,
              onPointerDown: (_) => state.lock.noteUse(),
              child: state.lock.locked
                  ? LockScreen(lock: state.lock)
                  : state.themes.trying == null
                  ? child!
                  : TryingBar(child: child!),
            ),
          ),
          home: !state.loaded
              // The start-up screen: the glitched wordmark on the dot grid.
              ? const Scaffold(
                  body: DotGrid(child: Center(child: Wordmark(size: 96))),
                )
              : state.servers.isEmpty
              ? const AddServerScreen(first: true)
              : const Scaffold(body: Shell()),
        );
      },
    ),
  );
}
