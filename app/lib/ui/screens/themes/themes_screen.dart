import 'dart:async';
import 'dart:convert';

import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../../core/api/errors.dart';
import '../../../core/themes/theme.dart';
import '../../../l10n/app_localizations.dart';
import '../../../platform/media_files.dart';
import '../../../state/app_prefs.dart';
import '../../../state/server_state.dart';
import '../../../state/themes_state.dart';
import '../../scopes.dart';
import '../../theme/churr_icons.dart';
import '../../theme/shapes.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import '../account/common.dart';
import 'theme_editor.dart';
import 'theme_library.dart';
import 'theme_widgets.dart';

/// Opens the theme picker: every theme the person can choose, the
/// built-in ones and their saved themes on every signed-in server.
Future<void> openThemes(BuildContext context) => showAccountScreen<void>(
  context,
  title: L10n.of(context).themes,
  width: 680,
  child: const ThemesScreen(),
);

/// The theme section of App settings: dark, light or follow the system;
/// the chosen dark and light theme; "use each server's default"; and the
/// way to every theme, the editor and the libraries.
class ThemeChoiceSection extends StatelessWidget {
  const ThemeChoiceSection({super.key});

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final app = AppScope.of(context);
    final themes = app.themes;
    final prefs = app.prefs;
    final c = ChurrColors.of(context);
    return ListenableBuilder(
      listenable: Listenable.merge([themes, prefs]),
      builder: (context, _) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          PickRow<ThemeChoice>(
            key: const Key('theme-choice'),
            value: prefs.theme,
            onChanged: (v) => prefs.theme = v,
            options: [
              (ThemeChoice.system, t.themeSystem),
              (ThemeChoice.dark, t.themeDark),
              (ThemeChoice.light, t.themeLight),
            ],
          ),
          const SizedBox(height: 10),
          for (final dark in [true, false])
            _ChosenRow(
              dark: dark,
              theme: themes.chosen(dark: dark),
            ),
          ChurrSwitchTile(
            key: const Key('theme-per-server'),
            contentPadding: EdgeInsets.zero,
            title: Text(t.themePerServer),
            subtitle: Text(t.themePerServerNote),
            value: themes.perServer,
            onChanged: (v) => themes.perServer = v,
          ),
          Align(
            alignment: AlignmentDirectional.centerStart,
            child: OutlinedButton.icon(
              key: const Key('open-themes'),
              icon: const Glyph(ChurrIcons.edit, size: 16),
              label: Text(t.themesAndEditor),
              onPressed: () => openThemes(context),
            ),
          ),
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Text(
              t.themesNote,
              style: TextStyle(fontSize: 13, color: c.dim),
            ),
          ),
        ],
      ),
    );
  }
}

class _ChosenRow extends StatelessWidget {
  const _ChosenRow({required this.dark, required this.theme});
  final bool dark;
  final ChurrTheme theme;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final app = AppScope.of(context);
    final s = app.themes.serverOf(theme);
    final c = ChurrColors.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            (dark ? t.darkTheme : t.lightTheme).toUpperCase(),
            style: ChurrType.label(c.dim),
          ),
          Row(
            children: [
              Swatches(theme: theme),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  s == null
                      ? themeName(t, theme)
                      : t.themeOnServer(themeName(t, theme), s.name),
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(fontWeight: FontWeight.w700),
                ),
              ),
              TextButton(
                key: Key('change-${dark ? 'dark' : 'light'}'),
                onPressed: () => _pick(context, dark),
                child: Text(t.change),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Future<void> _pick(BuildContext context, bool dark) async {
    final t = L10n.of(context);
    final themes = AppScope.read(context).themes;
    unawaited(themes.refreshAll());
    final picked = await showDialog<ChurrTheme>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (ctx) => ListenableBuilder(
        listenable: themes,
        builder: (ctx, _) => SimpleDialog(
          title: Text(dark ? t.chooseDarkTheme : t.chooseLightTheme),
          children: [
            for (final th in themes.all.where(
              (x) => x.dark == dark && x.usable,
            ))
              SimpleDialogOption(
                key: Key('pick-theme-${th.ref}'),
                onPressed: () => Navigator.pop(ctx, th),
                child: _ThemeLine(theme: th, server: themes.serverOf(th)),
              ),
          ],
        ),
      ),
    );
    if (picked != null) await themes.setChosen(picked);
  }
}

/// Swatches, name, the server it is on and its kind.
class _ThemeLine extends StatelessWidget {
  const _ThemeLine({required this.theme, this.server, this.inUse = false});
  final ChurrTheme theme;
  final ServerState? server;
  final bool inUse;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    return Row(
      children: [
        Swatches(theme: theme),
        const SizedBox(width: 10),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                themeName(t, theme),
                style: const TextStyle(fontWeight: FontWeight.w700),
              ),
              Text(
                [
                  server?.name ?? t.builtInLabel,
                  if (theme.published) t.publishedLabel,
                ].join(' · '),
                style: TextStyle(color: c.dim, fontSize: 12.5),
              ),
            ],
          ),
        ),
        if (inUse) ...[
          Tag(t.inUse, fill: c.cyan, degrees: 2),
          const SizedBox(width: 6),
        ],
        KindTag(theme),
      ],
    );
  }
}

enum _Act { use, tryIt, edit, copy, export, link, publish, unpublish, delete }

class ThemesScreen extends StatefulWidget {
  const ThemesScreen({super.key});

  @override
  State<ThemesScreen> createState() => _ThemesScreenState();
}

class _ThemesScreenState extends State<ThemesScreen> {
  String? message;

  @override
  void initState() {
    super.initState();
    // Saved themes have no events: fetched each time the picker opens.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) unawaited(AppScope.read(context).themes.refreshAll());
    });
  }

  ThemesState get themes => AppScope.read(context).themes;

  Future<void> _act(_Act a, ChurrTheme th) async {
    final t = L10n.of(context);
    final s = themes.serverOf(th);
    setState(() => message = null);
    try {
      switch (a) {
        case _Act.use:
          await themes.choose(th);
        case _Act.tryIt:
          themes.tryTheme(th);
        case _Act.edit:
          await openThemeEditor(context, start: th, server: s);
        case _Act.copy:
          await openThemeEditor(
            context,
            start: th.copyWith(
              id: '',
              clearServer: true,
              name: themeName(t, th),
            ),
            server: s ?? AppScope.read(context).selectedServer,
          );
        case _Act.export:
          final file = '${themeName(t, th)}.$churrThemeExtension';
          final where = await saveDownload(
            file,
            utf8.encode(encodeThemeFile(th)),
            'application/json',
          );
          if (where != null) setState(() => message = t.themeExported(file));
        case _Act.link:
          await Clipboard.setData(
            ClipboardData(text: themeLink(th.serverAddress!, th.id)),
          );
          setState(() => message = t.themeLinkCopied);
        case _Act.publish || _Act.unpublish:
          await themes.setPublished(th, a == _Act.publish);
        case _Act.delete:
          await themes.delete(th);
      }
    } on ApiException catch (e) {
      setState(() => message = e.message);
    }
  }

  Future<void> _import() async {
    final t = L10n.of(context);
    final f = await openFile(
      acceptedTypeGroups: [
        XTypeGroup(
          label: t.themeFileLabel,
          extensions: const [churrThemeExtension],
          uniformTypeIdentifiers: const ['public.json', 'public.data'],
        ),
      ],
    );
    if (f == null || !mounted) return;
    try {
      final th = decodeThemeFile(await f.readAsString());
      if (!mounted) return;
      await openThemeEditor(
        context,
        start: th,
        server: AppScope.read(context).selectedServer,
      );
    } on ThemeFileError catch (e) {
      setState(() => message = e.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final app = AppScope.of(context);
    return ListenableBuilder(
      listenable: Listenable.merge([themes, app.prefs]),
      builder: (context, _) {
        final inUse = {
          themes.chosen(dark: true).ref,
          themes.chosen(dark: false).ref,
        };
        Widget row(ChurrTheme th) {
          final s = themes.serverOf(th);
          final own = s != null;
          return Container(
            key: Key('theme-row-${th.ref}'),
            margin: const EdgeInsets.only(bottom: 2),
            color: c.panel,
            child: InkWell(
              onTap: th.usable ? () => _act(_Act.use, th) : null,
              child: Padding(
                padding: const EdgeInsets.fromLTRB(10, 8, 0, 8),
                child: Row(
                  children: [
                    Expanded(
                      child: _ThemeLine(
                        theme: th,
                        server: s,
                        inUse: inUse.contains(th.ref),
                      ),
                    ),
                    PopupMenuButton<_Act>(
                      key: Key('theme-menu-${th.ref}'),
                      popUpAnimationStyle: AnimationStyle.noAnimation,
                      icon: const Glyph(ChurrIcons.moreV, size: 18),
                      tooltip: t.moreActions,
                      onSelected: (a) => _act(a, th),
                      itemBuilder: (_) => [
                        if (th.usable)
                          PopupMenuItem(
                            value: _Act.use,
                            child: Text(t.themeUse),
                          ),
                        if (th.usable)
                          PopupMenuItem(
                            value: _Act.tryIt,
                            child: Text(t.themeTry),
                          ),
                        if (own)
                          PopupMenuItem(
                            value: _Act.edit,
                            child: Text(t.themeEdit),
                          ),
                        PopupMenuItem(
                          value: _Act.copy,
                          child: Text(own ? t.copy : t.themeEditACopy),
                        ),
                        PopupMenuItem(
                          value: _Act.export,
                          child: Text(t.themeExport),
                        ),
                        if (own)
                          PopupMenuItem(
                            value: _Act.link,
                            child: Text(t.themeShareLink),
                          ),
                        if (own && !th.published)
                          PopupMenuItem(
                            value: _Act.publish,
                            child: Text(t.themePublish),
                          ),
                        if (own && th.published)
                          PopupMenuItem(
                            value: _Act.unpublish,
                            child: Text(t.themeUnpublish),
                          ),
                        if (own)
                          PopupMenuItem(
                            value: _Act.delete,
                            child: Text(
                              t.themeDelete,
                              style: TextStyle(color: c.pink),
                            ),
                          ),
                      ],
                    ),
                  ],
                ),
              ),
            ),
          );
        }

        return ListView(
          padding: const EdgeInsets.fromLTRB(20, 4, 20, 24),
          children: [
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                FilledButton.icon(
                  key: const Key('new-theme'),
                  icon: const Glyph(ChurrIcons.add, size: 16),
                  label: Text(t.newTheme),
                  onPressed: () => openThemeEditor(
                    context,
                    start: themes.current(
                      dark: Theme.of(context).brightness == Brightness.dark,
                    ),
                    server: app.selectedServer,
                  ),
                ),
                OutlinedButton.icon(
                  key: const Key('import-theme'),
                  icon: const Glyph(ChurrIcons.download, size: 16),
                  label: Text(t.themeImport),
                  onPressed: _import,
                ),
              ],
            ),
            if (message != null) NoteLine(message!),
            SectionHead(t.builtInThemes),
            for (final th in BuiltInThemes.all) row(th),
            for (final s in app.servers) ...[
              SectionHead(t.savedOn(s.name)),
              if (themes.errors[s.localId] != null)
                ErrorLine(t.themesCouldNotLoad)
              else if (themes.mine[s.localId] == null)
                Text(t.loading, style: TextStyle(color: c.dim))
              else if (themes.mine[s.localId]!.isEmpty)
                Text(t.noSavedThemes, style: TextStyle(color: c.dim)),
              for (final th in themes.mine[s.localId] ?? const <ChurrTheme>[])
                row(th),
              Align(
                alignment: AlignmentDirectional.centerStart,
                child: TextButton.icon(
                  key: Key('library-${s.localId}'),
                  icon: const Glyph(ChurrIcons.compass, size: 16),
                  label: Text(t.themeLibraryOf(s.name)),
                  onPressed: () => openThemeLibrary(context, s),
                ),
              ),
            ],
          ],
        );
      },
    );
  }
}
