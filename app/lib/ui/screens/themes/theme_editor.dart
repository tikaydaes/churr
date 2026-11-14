import 'dart:convert';

import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../../core/api/errors.dart';
import '../../../core/themes/palette.dart';
import '../../../core/themes/theme.dart';
import '../../../l10n/app_localizations.dart';
import '../../../platform/media_files.dart';
import '../../../state/server_state.dart';
import '../../../state/themes_state.dart';
import '../../layout/breakpoints.dart';
import '../../scopes.dart';
import '../../theme/churr_icons.dart';
import '../../theme/shapes.dart';
import '../../theme/theme.dart';
import '../../theme/tokens.dart';
import '../account/common.dart';
import 'theme_preview.dart';
import 'theme_widgets.dart';

/// Opens the theme editor on [start] (ruling R61): any theme. A built-in
/// one or someone else's is copied; one of the person's own saved themes
/// is changed where it is saved.
Future<void> openThemeEditor(
  BuildContext context, {
  required ChurrTheme start,
  ServerState? server,
}) {
  final t = L10n.of(context);
  final compact =
      WidthClass.of(MediaQuery.sizeOf(context).width) == WidthClass.compact;
  final editor = ThemeEditorScreen(start: start, server: server);
  if (compact) {
    return Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => Scaffold(
          appBar: AppBar(title: Text(t.themeEditor)),
          body: SafeArea(child: editor),
        ),
      ),
    );
  }
  return showDialog<void>(
    animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (_) => Dialog(
      insetPadding: const EdgeInsets.all(24),
      child: SizedBox(
        width: 1180,
        height: 820,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            DialogTitleBar(title: t.themeEditor),
            Expanded(child: editor),
          ],
        ),
      ),
    ),
  );
}

/// The base colours' names, as the editor labels them.
String baseName(L10n t, String key) => switch (key) {
  'bg' => t.baseBg,
  'panel' => t.basePanel,
  'raise' => t.baseRaise,
  'ink' => t.baseInk,
  'dim' => t.baseDim,
  'lav' => t.baseLav,
  'cyan' => t.baseCyan,
  'pink' => t.basePink,
  _ => t.baseYel,
};

class ThemeEditorScreen extends StatefulWidget {
  const ThemeEditorScreen({super.key, required this.start, this.server});
  final ChurrTheme start;

  /// Where to save, at first; the person may pick another server.
  final ServerState? server;

  @override
  State<ThemeEditorScreen> createState() => _ThemeEditorScreenState();
}

class _ThemeEditorScreenState extends State<ThemeEditorScreen> {
  late final TextEditingController name;
  late String kind;
  late Map<String, String> colours;
  final fields = <String, TextEditingController>{};

  /// The saved theme being changed; null for a new one.
  ChurrTheme? saved;
  ServerState? target;
  String? open;
  String? error;
  String? note;
  bool busy = false;

  bool _own(ChurrTheme s) =>
      !s.builtIn &&
      s.id.isNotEmpty &&
      s.serverAddress != null &&
      AppScope.read(context).themes.mine.values.any(
        (l) => l.any((x) => x.id == s.id && x.serverAddress == s.serverAddress),
      );

  @override
  void initState() {
    super.initState();
    final s = widget.start;
    kind = s.kind;
    colours = {for (final k in themeKeys) k: s.colours[k] ?? '#000000'};
    for (final k in themeKeys) {
      fields[k] = TextEditingController(text: colours[k]);
    }
    name = TextEditingController();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      final t = L10n.of(context);
      final app = AppScope.read(context);
      setState(() {
        if (_own(s)) {
          saved = s;
          name.text = s.name;
          target = app.themes.serverOf(s);
        } else {
          name.text = t.themeCopyName(themeName(t, s));
          target = widget.server ?? app.selectedServer;
        }
      });
    });
  }

  @override
  void dispose() {
    name.dispose();
    for (final c in fields.values) {
      c.dispose();
    }
    super.dispose();
  }

  ChurrTheme get draft => ChurrTheme(
    id: saved?.id ?? '',
    name: name.text.trim(),
    kind: kind,
    colours: Map.of(colours),
    published: saved?.published ?? false,
    ownerId: saved?.ownerId,
    authorId: saved?.authorId,
    serverAddress: saved?.serverAddress,
  );

  void _set(String key, Rgb c, {bool fromField = false}) {
    setState(() {
      colours[key] = c.hex;
      if (!fromField) fields[key]!.text = c.hex;
      error = null;
    });
  }

  void _setAll(Map<String, String> next) {
    setState(() {
      colours = {for (final k in themeKeys) k: next[k]!};
      for (final k in themeKeys) {
        fields[k]!.text = colours[k]!;
      }
      error = null;
    });
  }

  ThemesState get themes => AppScope.read(context).themes;

  Future<void> _run(Future<void> Function() f, {String? done}) async {
    setState(() {
      busy = true;
      error = null;
      note = null;
    });
    try {
      await f();
      if (mounted && done != null) setState(() => note = done);
    } on ApiException catch (e) {
      // The server's own words when it refuses.
      if (mounted) setState(() => error = e.message);
    } on Exception catch (e) {
      if (mounted) setState(() => error = '$e');
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> _save() async {
    final t = L10n.of(context);
    final s = target;
    if (s == null) return;
    var d = draft;
    // Saving to another server than its own makes a new theme there.
    if (saved != null && saved!.serverAddress != s.entry.address) {
      d = d.copyWith(id: '', clearServer: true);
    }
    await _run(() async {
      final next = await themes.save(s, d);
      if (mounted) setState(() => saved = next);
    }, done: t.themeSavedOn(s.name));
  }

  void _copy() {
    final t = L10n.of(context);
    setState(() {
      saved = null;
      name.text = t.themeCopyName(name.text.trim());
      note = t.themeCopyMade;
    });
  }

  Future<void> _delete() async {
    final t = L10n.of(context);
    final s = saved;
    if (s == null) return;
    final ok = await showDialog<bool>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(t.themeDeleteTitle(s.name)),
        content: Text(t.themeDeleteBody),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: Text(t.cancel),
          ),
          FilledButton(
            key: const Key('confirm-delete-theme'),
            style: dangerButton(ChurrColors.of(ctx)),
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(t.themeDelete),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    await _run(() async {
      await themes.delete(s);
      if (mounted) setState(() => saved = null);
    }, done: t.themeDeleted);
  }

  Future<void> _publish(bool on) async {
    final t = L10n.of(context);
    final s = saved;
    if (s == null) return;
    await _run(() async {
      final next = await themes.setPublished(s, on);
      if (mounted) setState(() => saved = next);
    }, done: on ? t.themePublishedNote : t.themeUnpublishedNote);
  }

  Future<void> _export() async {
    final t = L10n.of(context);
    final d = draft;
    await _run(() async {
      final file = '${d.name.isEmpty ? 'theme' : d.name}.$churrThemeExtension';
      final where = await saveDownload(
        file,
        utf8.encode(encodeThemeFile(d)),
        'application/json',
      );
      if (where != null && mounted) {
        setState(() => note = t.themeExported(file));
      }
    });
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
      setState(() {
        kind = th.kind;
        name.text = th.name;
        saved = null;
      });
      _setAll(th.colours);
      setState(() => note = t.themeImported(th.name));
    } on ThemeFileError catch (e) {
      setState(() => error = e.message);
    }
  }

  Future<void> _shareLink() async {
    final t = L10n.of(context);
    final s = saved;
    if (s == null || s.serverAddress == null) return;
    await Clipboard.setData(
      ClipboardData(text: themeLink(s.serverAddress!, s.id)),
    );
    if (mounted) setState(() => note = t.themeLinkCopied);
  }

  @override
  Widget build(BuildContext context) {
    final app = AppScope.of(context);
    return ListenableBuilder(
      listenable: app.themes,
      builder: (context, _) => LayoutBuilder(
        builder: (context, box) {
          final preview = ThemePreview(theme: draft);
          final controls = _controls(context);
          if (box.maxWidth >= 900) {
            return Row(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                SizedBox(
                  width: 440,
                  child: ListView(
                    padding: const EdgeInsets.fromLTRB(20, 4, 16, 24),
                    children: controls,
                  ),
                ),
                Expanded(child: SingleChildScrollView(child: preview)),
              ],
            );
          }
          return ListView(
            padding: const EdgeInsets.fromLTRB(16, 4, 16, 24),
            children: [preview, const SizedBox(height: 12), ...controls],
          );
        },
      ),
    );
  }

  List<Widget> _controls(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final app = AppScope.of(context);
    final parsed = parseColours(colours).$1;
    final fits = parsed != null && kindFits(kind, parsed);
    final measures = parsed == null
        ? const <Measure>[]
        : measureAll(kind, parsed);
    final failing = measures.where((m) => !m.passes).toList();
    final ok =
        parsed != null &&
        fits &&
        failing.isEmpty &&
        name.text.trim().isNotEmpty;
    final s = saved;
    return [
      TextField(
        key: const Key('theme-name'),
        controller: name,
        maxLength: 64,
        decoration: InputDecoration(labelText: t.themeNameLabel),
        onChanged: (_) => setState(() {}),
      ),
      SectionHead(t.themeKind),
      SegmentedButton<String>(
        key: const Key('theme-kind'),
        showSelectedIcon: false,
        segments: [
          ButtonSegment(value: kindDark, label: Text(t.themeDark)),
          ButtonSegment(value: kindLight, label: Text(t.themeLight)),
        ],
        selected: {kind},
        onSelectionChanged: (v) => setState(() => kind = v.first),
      ),
      if (parsed != null && !fits)
        ErrorLine(
          kind == kindDark ? t.themeKindDarkProblem : t.themeKindLightProblem,
        ),
      SectionHead(t.themeColours),
      for (final k in themeKeys) _colourRow(context, k),
      SectionHead(t.themeContrast),
      Text(
        failing.isEmpty
            ? t.themeAllPass(measures.length)
            : t.themeSomeFail(failing.length),
        key: const Key('theme-contrast-summary'),
        style: TextStyle(
          color: failing.isEmpty ? c.cyan : c.pink,
          fontWeight: FontWeight.w700,
        ),
      ),
      const SizedBox(height: 6),
      // Failing pairings first, then the rest, in the check's order.
      for (final m in [...failing, ...measures.where((m) => m.passes)])
        _pairRow(context, m, parsed!),
      SectionHead(t.themeSaveSection),
      Row(
        children: [
          Text(t.themeSaveTo),
          const SizedBox(width: 10),
          Expanded(
            child: DropdownButton<ServerState>(
              key: const Key('theme-save-to'),
              isExpanded: true,
              elevation: 0,
              icon: const Glyph(ChurrIcons.chevronDown, size: 16),
              value: app.servers.contains(target) ? target : null,
              hint: Text(t.themeNoServer),
              items: [
                for (final sv in app.servers)
                  DropdownMenuItem(value: sv, child: Text(sv.name)),
              ],
              onChanged: (v) => setState(() => target = v),
            ),
          ),
        ],
      ),
      if (s != null)
        Padding(
          padding: const EdgeInsets.only(top: 4),
          child: Text(
            s.published ? t.themeIsPublished : t.themeIsSaved,
            style: monoStyle(context, color: c.dim),
          ),
        ),
      if (error != null) ErrorLine(error!),
      if (note != null) NoteLine(note!),
      const SizedBox(height: 8),
      Wrap(
        spacing: 8,
        runSpacing: 8,
        children: [
          FilledButton(
            key: const Key('theme-save'),
            onPressed: ok && target != null && !busy ? _save : null,
            child: Text(t.save),
          ),
          OutlinedButton(
            key: const Key('theme-try'),
            onPressed: parsed != null && fits && failing.isEmpty
                ? () => app.themes.tryTheme(draft)
                : null,
            child: Text(t.themeTry),
          ),
          OutlinedButton(
            key: const Key('theme-copy'),
            onPressed: busy ? null : _copy,
            child: Text(t.copy),
          ),
          OutlinedButton(
            key: const Key('theme-export'),
            onPressed: ok && !busy ? _export : null,
            child: Text(t.themeExport),
          ),
          OutlinedButton(
            key: const Key('theme-import'),
            onPressed: busy ? null : _import,
            child: Text(t.themeImport),
          ),
          if (s != null) ...[
            OutlinedButton(
              key: const Key('theme-share'),
              onPressed: _shareLink,
              child: Text(t.themeShareLink),
            ),
            OutlinedButton(
              key: const Key('theme-publish'),
              onPressed: busy ? null : () => _publish(!s.published),
              child: Text(s.published ? t.themeUnpublish : t.themePublish),
            ),
            FilledButton(
              key: const Key('theme-delete'),
              style: dangerButton(c),
              onPressed: busy ? null : _delete,
              child: Text(t.themeDelete),
            ),
          ],
        ],
      ),
      if (!ok)
        Padding(
          padding: const EdgeInsets.only(top: 8),
          child: Text(
            t.themeCannotSaveYet,
            style: TextStyle(color: c.dim, fontSize: 13),
          ),
        ),
    ];
  }

  Widget _colourRow(BuildContext context, String key) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final rgb = Rgb.parse(colours[key]!) ?? Rgb.black;
    final expanded = open == key;
    final (h, s, l) = toHsl(rgb);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.symmetric(vertical: 3),
          child: Row(
            children: [
              // The swatch, with an edge so it stands on any ground.
              Container(
                width: 30,
                height: 30,
                decoration: BoxDecoration(
                  color: Color(rgb.argb),
                  border: Border.all(color: c.edge, width: 2),
                ),
              ),
              const SizedBox(width: 10),
              Expanded(child: Text(baseName(t, key))),
              SizedBox(
                width: 116,
                child: TextField(
                  key: Key('hex-$key'),
                  controller: fields[key],
                  style: monoStyle(context, size: 13, color: c.ink),
                  inputFormatters: [
                    FilteringTextInputFormatter.allow(RegExp('[#0-9a-fA-F]')),
                    LengthLimitingTextInputFormatter(7),
                  ],
                  decoration: InputDecoration(
                    hintText: '#rrggbb',
                    errorText: Rgb.parse(fields[key]!.text) == null
                        ? t.hexInvalid
                        : null,
                  ),
                  onChanged: (v) {
                    final p = Rgb.parse(v.startsWith('#') ? v : '#$v');
                    if (p != null) {
                      _set(key, p, fromField: true);
                    } else {
                      setState(() {});
                    }
                  },
                ),
              ),
              IconButton(
                key: Key('hsl-$key'),
                tooltip: t.hslPicker,
                isSelected: expanded,
                icon: Glyph(
                  expanded ? ChurrIcons.chevronUp : ChurrIcons.chevronDown,
                  size: 18,
                ),
                onPressed: () => setState(() => open = expanded ? null : key),
              ),
            ],
          ),
        ),
        if (expanded)
          Container(
            color: c.raised,
            padding: const EdgeInsets.fromLTRB(10, 6, 10, 6),
            child: Column(
              children: [
                _slider(
                  context,
                  t.hue,
                  h,
                  359,
                  '°',
                  (v) => _set(key, fromHsl(v, s, l)),
                  Key('hue-$key'),
                ),
                _slider(
                  context,
                  t.saturation,
                  s,
                  100,
                  '%',
                  (v) => _set(key, fromHsl(h, v, l)),
                  Key('sat-$key'),
                ),
                _slider(
                  context,
                  t.lightness,
                  l,
                  100,
                  '%',
                  (v) => _set(key, fromHsl(h, s, v)),
                  Key('light-$key'),
                ),
              ],
            ),
          ),
      ],
    );
  }

  Widget _slider(
    BuildContext context,
    String label,
    int value,
    int max,
    String unit,
    ValueChanged<int> on,
    Key key,
  ) {
    final c = ChurrColors.of(context);
    return Row(
      children: [
        SizedBox(
          width: 92,
          child: Text(label, style: TextStyle(color: c.ink, fontSize: 13)),
        ),
        Expanded(
          child: Slider(
            key: key,
            value: value.clamp(0, max).toDouble(),
            min: 0,
            max: max.toDouble(),
            divisions: max,
            label: '$value$unit',
            onChanged: (v) => on(v.round()),
          ),
        ),
        SizedBox(
          width: 44,
          child: Text(
            '$value$unit',
            textAlign: TextAlign.end,
            style: monoStyle(context, color: c.ink),
          ),
        ),
      ],
    );
  }

  Widget _pairRow(BuildContext context, Measure m, Colours parsed) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final d = derive(parsed);
    final fore = Color(token(parsed, d, m.pairing.fore).argb);
    final back = Color(token(parsed, d, m.pairing.back).argb);
    final need = m.pairing.isText ? t.needText : t.needEdge;
    return Container(
      key: Key('pair-${m.pairing.fore}-${m.pairing.back}-${m.pairing.need}'),
      margin: const EdgeInsets.only(bottom: 2),
      color: m.passes ? c.panel : c.raised,
      padding: const EdgeInsets.fromLTRB(8, 4, 4, 4),
      child: Row(
        children: [
          // The pairing itself, drawn in the theme's colours.
          Container(
            width: 34,
            height: 24,
            color: back,
            alignment: Alignment.center,
            child: Text(
              'Aa',
              style: TextStyle(
                color: fore,
                fontWeight: FontWeight.w800,
                fontSize: 12,
              ),
            ),
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  m.pairing.name,
                  style: TextStyle(
                    color: c.ink,
                    fontSize: 13,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                Text(
                  need,
                  style: TextStyle(
                    color: m.passes ? c.faint : c.dim,
                    fontSize: 11.5,
                  ),
                ),
              ],
            ),
          ),
          Text('${m.shown}:1', style: monoStyle(context, color: c.ink)),
          const SizedBox(width: 8),
          Tag(
            m.passes ? t.pass : t.fail,
            fill: m.passes ? c.cyan : c.pink,
            degrees: m.passes ? 2 : -2,
          ),
          SizedBox(
            width: 64,
            child: m.passes
                ? null
                : TextButton(
                    key: Key('fix-${m.pairing.fore}-${m.pairing.back}'),
                    onPressed: () {
                      final k = fixKey(m.pairing);
                      final fixed = fix(kind, parsed, k);
                      if (fixed == null) {
                        setState(() => error = t.cannotFix(baseName(t, k)));
                      } else {
                        _setAll(hexes(fixed));
                        setState(
                          () => note = t.fixedColour(
                            baseName(t, k),
                            fixed[k]!.hex,
                          ),
                        );
                      }
                    },
                    child: Text(t.fix),
                  ),
          ),
        ],
      ),
    );
  }
}
