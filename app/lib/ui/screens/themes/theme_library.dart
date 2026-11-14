import 'dart:async';

import 'package:flutter/material.dart';

import '../../../core/api/errors.dart';
import '../../../core/api/themes.dart';
import '../../../core/themes/theme.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/server_state.dart';
import '../../scopes.dart';
import '../../theme/churr_icons.dart';
import '../../theme/tokens.dart';
import '../account/common.dart';
import 'theme_widgets.dart';

/// Opens [server]'s theme library: browse and search its published
/// themes, try one, add one.
Future<void> openThemeLibrary(BuildContext context, ServerState server) =>
    showAccountScreen<void>(
      context,
      title: L10n.of(context).themeLibraryOf(server.name),
      width: 680,
      child: ThemeLibraryScreen(server: server),
    );

class ThemeLibraryScreen extends StatefulWidget {
  const ThemeLibraryScreen({super.key, required this.server});
  final ServerState server;

  @override
  State<ThemeLibraryScreen> createState() => _ThemeLibraryScreenState();
}

class _ThemeLibraryScreenState extends State<ThemeLibraryScreen> {
  final query = TextEditingController();
  List<ChurrTheme>? themes;
  String? next;
  String? error;
  bool loading = false;
  Timer? _debounce;
  StreamSubscription<Object?>? _sub;

  ServerState get s => widget.server;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
    // A theme entering or leaving the library while it is open.
    _sub = s.sync.localEvents.listen((e) {
      if (!mounted || themes == null) return;
      final type = e['type'];
      if (type == 'theme.published' && e['theme'] is Map) {
        final th = ChurrTheme.fromJson(
          (e['theme'] as Map).cast(),
          serverAddress: s.entry.address,
        );
        final q = query.text.trim().toLowerCase();
        if (q.isNotEmpty && !th.name.toLowerCase().contains(q)) return;
        setState(() {
          themes = [th, ...themes!.where((x) => x.id != th.id)];
        });
      } else if (type == 'theme.unpublished') {
        setState(
          () => themes = themes!.where((x) => x.id != e['theme_id']).toList(),
        );
      }
    });
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _sub?.cancel();
    query.dispose();
    super.dispose();
  }

  Future<void> _load({bool more = false}) async {
    setState(() {
      loading = true;
      error = null;
    });
    try {
      final ThemePage page = await s.api.listThemeLibrary(
        q: query.text.trim(),
        cursor: more ? next : null,
      );
      if (!mounted) return;
      setState(() {
        themes = more ? [...?themes, ...page.themes] : page.themes;
        next = page.nextCursor;
      });
    } on ApiException catch (e) {
      if (mounted) setState(() => error = e.message);
    } on Exception catch (e) {
      if (mounted) setState(() => error = '$e');
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final admin = s.me?.role == 'admin' || s.me?.role == 'owner';
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 4, 20, 24),
      children: [
        TextField(
          key: const Key('library-search'),
          controller: query,
          decoration: InputDecoration(
            labelText: t.searchThemes,
            prefixIcon: const Padding(
              padding: EdgeInsets.all(10),
              child: Glyph(ChurrIcons.search, size: 16),
            ),
          ),
          onChanged: (_) {
            _debounce?.cancel();
            _debounce = Timer(const Duration(milliseconds: 300), _load);
          },
        ),
        const SizedBox(height: 8),
        if (error != null) ErrorLine(error!),
        if (themes == null && loading)
          Text(t.loading, style: TextStyle(color: c.dim)),
        if (themes != null && themes!.isEmpty)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 16),
            child: Text(
              query.text.trim().isEmpty ? t.libraryEmpty : t.libraryNoMatch,
              style: TextStyle(color: c.dim),
            ),
          ),
        for (final th in themes ?? const <ChurrTheme>[])
          Container(
            key: Key('library-row-${th.id}'),
            margin: const EdgeInsets.only(bottom: 2),
            color: c.panel,
            padding: const EdgeInsets.fromLTRB(10, 8, 6, 8),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Row(
                  children: [
                    Swatches(theme: th, size: 20),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            th.name,
                            style: const TextStyle(fontWeight: FontWeight.w700),
                          ),
                          Text(
                            t.themeBy(
                              s.accounts[th.ownerId]?.name ?? t.someone,
                            ),
                            style: TextStyle(color: c.dim, fontSize: 12.5),
                          ),
                        ],
                      ),
                    ),
                    KindTag(th),
                  ],
                ),
                const SizedBox(height: 6),
                Row(
                  mainAxisAlignment: MainAxisAlignment.end,
                  children: [
                    TextButton(
                      key: Key('library-try-${th.id}'),
                      onPressed: th.usable
                          ? () => AppScope.read(context).themes.tryTheme(th)
                          : null,
                      child: Text(t.themeTry),
                    ),
                    const SizedBox(width: 6),
                    TextButton(
                      key: Key('library-add-${th.id}'),
                      onPressed: () => addTheme(context, s, th),
                      child: Text(t.themeAdd),
                    ),
                    if (admin)
                      PopupMenuButton<String>(
                        popUpAnimationStyle: AnimationStyle.noAnimation,
                        icon: const Glyph(ChurrIcons.moreV, size: 18),
                        tooltip: t.moreActions,
                        onSelected: (v) => _admin(v, th),
                        itemBuilder: (_) => [
                          PopupMenuItem(
                            value: 'default',
                            child: Text(
                              th.dark ? t.makeDefaultDark : t.makeDefaultLight,
                            ),
                          ),
                          PopupMenuItem(
                            value: 'remove',
                            child: Text(
                              t.removeFromLibrary,
                              style: TextStyle(color: c.pink),
                            ),
                          ),
                        ],
                      ),
                  ],
                ),
              ],
            ),
          ),
        if (next != null)
          Align(
            child: OutlinedButton(
              onPressed: loading ? null : () => _load(more: true),
              child: Text(t.showMore),
            ),
          ),
      ],
    );
  }

  Future<void> _admin(String v, ChurrTheme th) async {
    final t = L10n.of(context);
    try {
      if (v == 'default') {
        await s.api.setDefaultTheme(th.kind, themeId: th.id);
        await s.refreshInfo();
        if (mounted) setState(() => error = null);
        if (mounted) {
          ScaffoldMessenger.maybeOf(context)
              ?.showSnackBar(SnackBar(content: Text(t.defaultSet(th.name))));
        }
      } else {
        await s.api.removeLibraryTheme(th.id);
        if (mounted) {
          setState(() => themes = themes?.where((x) => x.id != th.id).toList());
        }
      }
    } on ApiException catch (e) {
      if (mounted) setState(() => error = e.message);
    }
  }
}
