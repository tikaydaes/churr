import 'package:flutter/material.dart';

import '../../../core/api/errors.dart';
import '../../../core/themes/palette.dart';
import '../../../core/themes/theme.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/server_state.dart';
import '../../scopes.dart';
import '../../theme/shapes.dart';
import '../../theme/theme.dart';
import '../../theme/tokens.dart';

/// Pieces the theme screens share: a theme's name, its swatches, the
/// theme card, and the bar shown while a theme is being tried.

/// A theme's name as shown: the built-in ones in the person's language.
String themeName(L10n t, ChurrTheme th) => !th.builtIn
    ? th.name
    : switch (th.id) {
        'light' => t.themeBuiltInLight,
        'dark-hd' => t.themeBuiltInDarkHd,
        'light-hd' => t.themeBuiltInLightHd,
        _ => t.themeBuiltInDark,
      };

Color colourOf(String? hex) => Color(Rgb.parse(hex ?? '')?.argb ?? 0xFF000000);

/// A theme's swatches, drawn in its own colours: its ground holding a
/// panel with "Aa" in its ink and dim, then its four role colours. The
/// name always goes beside it, so the colours never work alone.
class Swatches extends StatelessWidget {
  const Swatches({super.key, required this.theme, this.size = 18});
  final ChurrTheme theme;
  final double size;

  @override
  Widget build(BuildContext context) {
    final tok = themeTokens(theme);
    Color k(String n) => colourOf(tok[n]);
    Widget sq(String n) => Container(width: size, height: size, color: k(n));
    return ExcludeSemantics(
      child: Container(
        color: k('bg'),
        padding: const EdgeInsets.all(3),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Container(
              height: size,
              padding: const EdgeInsets.symmetric(horizontal: 4),
              color: k('panel'),
              alignment: Alignment.center,
              child: Text.rich(
                TextSpan(
                  children: [
                    TextSpan(
                      text: 'A',
                      style: TextStyle(color: k('ink')),
                    ),
                    TextSpan(
                      text: 'a',
                      style: TextStyle(color: k('dim')),
                    ),
                  ],
                ),
                style: archivo(size: size * 0.7, weight: 800, height: 1),
              ),
            ),
            const SizedBox(width: 3),
            Container(width: size * 0.6, height: size, color: k('raise')),
            for (final n in const ['lav', 'cyan', 'pink', 'yel']) ...[
              const SizedBox(width: 3),
              sq(n),
            ],
          ],
        ),
      ),
    );
  }
}

/// A small word naming the kind: "dark" or "light".
class KindTag extends StatelessWidget {
  const KindTag(this.theme, {super.key});
  final ChurrTheme theme;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    return Tag(
      theme.dark ? t.kindDarkTag : t.kindLightTag,
      fill: theme.dark ? c.lavender : c.cyan,
      degrees: theme.dark ? -2 : 2,
    );
  }
}

/// A theme card (STYLE.md "Sharing"): swatches, name and author, and
/// "Try" and "Add". For a message card carrying `theme`, an attached
/// `.churrtheme` file, and a tapped theme link.
///
/// [server] is where "Add" puts it: a theme of that server is added as a
/// copy (`addTheme`); a theme from a file is saved there as new.
class ThemeCardView extends StatelessWidget {
  const ThemeCardView({
    super.key,
    required this.server,
    required this.theme,
    this.author,
  });
  final ServerState server;
  final ChurrTheme theme;
  final String? author;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final app = AppScope.of(context);
    final problem = theme.problem;
    return Container(
      key: Key('theme-card-${theme.ref}'),
      margin: const EdgeInsets.only(top: 6),
      constraints: const BoxConstraints(maxWidth: 420),
      decoration: ShapeDecoration(
        color: c.panel,
        shape: const CutCornerBorder(cut: 8),
      ),
      padding: const EdgeInsets.fromLTRB(12, 10, 12, 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(
                t.themeCardLabel.toUpperCase(),
                style: ChurrType.label(c.faint),
              ),
              const SizedBox(width: 8),
              KindTag(theme),
            ],
          ),
          const SizedBox(height: 6),
          Text(themeName(t, theme), style: ChurrType.title(c.ink, size: 17)),
          if (author != null)
            Text(
              t.themeBy(author!),
              style: TextStyle(color: c.dim, fontSize: 13),
            ),
          const SizedBox(height: 8),
          Swatches(theme: theme, size: 22),
          if (problem != null) ...[
            const SizedBox(height: 8),
            Text(
              t.themeCannotApply(problem),
              style: TextStyle(color: c.pink, fontSize: 13),
            ),
          ],
          const SizedBox(height: 10),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              OutlinedButton(
                key: const Key('theme-card-try'),
                onPressed: problem != null
                    ? null
                    : () => app.themes.tryTheme(theme),
                child: Text(t.themeTry),
              ),
              FilledButton(
                key: const Key('theme-card-add'),
                onPressed: problem != null
                    ? null
                    : () => addTheme(context, server, theme),
                child: Text(t.themeAdd),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

/// "Add": a theme of [server] becomes a copy among the person's own
/// there; any other (a file, another server's) is saved there as new.
Future<void> addTheme(
  BuildContext context,
  ServerState server,
  ChurrTheme theme,
) async {
  final t = L10n.of(context);
  final themes = AppScope.read(context).themes;
  final messenger = ScaffoldMessenger.maybeOf(context);
  try {
    if (theme.id.isNotEmpty && theme.serverAddress == server.entry.address) {
      await themes.add(server, theme.id);
    } else {
      await themes.save(server, theme.copyWith(id: '', clearServer: true));
    }
    messenger?.showSnackBar(SnackBar(content: Text(t.themeAdded(server.name))));
  } on ApiException catch (e) {
    messenger?.showSnackBar(SnackBar(content: Text(e.message)));
  } on Exception catch (e) {
    messenger?.showSnackBar(SnackBar(content: Text('$e')));
  }
}

/// Draws [child] in [theme]'s colours (the editor's preview, a card).
class InTheme extends StatelessWidget {
  const InTheme({super.key, required this.theme, required this.child});
  final ChurrTheme theme;
  final Widget child;

  @override
  Widget build(BuildContext context) => Theme(
    data: buildTheme(dark: theme.dark, theme: theme),
    child: child,
  );
}

/// While a theme is tried: a strip across the top saying so, with "Keep
/// it" and "Stop". Sits above the app's navigator.
class TryingBar extends StatelessWidget {
  const TryingBar({super.key, required this.child});
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final app = AppScope.of(context);
    final trying = app.themes.trying;
    if (trying == null) return child;
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    return Column(
      children: [
        Material(
          color: c.raised,
          child: SafeArea(
            bottom: false,
            child: Container(
              key: const Key('trying-bar'),
              decoration: BoxDecoration(
                border: Border(left: BorderSide(color: c.yellow, width: 4)),
              ),
              padding: const EdgeInsets.fromLTRB(12, 6, 8, 6),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      t.themeTrying(themeName(t, trying)),
                      style: TextStyle(
                        color: c.ink,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
                  TextButton(
                    key: const Key('trying-keep'),
                    onPressed: () => app.themes.choose(trying),
                    child: Text(t.themeKeep),
                  ),
                  const SizedBox(width: 6),
                  TextButton(
                    key: const Key('trying-stop'),
                    onPressed: app.themes.stopTrying,
                    child: Text(t.themeStopTrying),
                  ),
                ],
              ),
            ),
          ),
        ),
        Expanded(child: child),
      ],
    );
  }
}
