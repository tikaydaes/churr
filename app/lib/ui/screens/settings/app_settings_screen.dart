import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../../core/models/conversation.dart';
import '../../../core/objects/measure.dart';
import '../../../l10n/app_localizations.dart';
import '../../../platform/file_cache.dart';
import '../../../platform/secret_storage.dart';
import '../../../state/account_prefs.dart';
import '../../../state/app_prefs.dart';
import '../../../state/hybrid_order.dart';
import '../../layout/account_entries.dart';
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import '../../theme/churr_icons.dart';
import '../account/common.dart';
import '../account/conversation_settings.dart';
import '../account/server_settings_screen.dart';
import '../add_server/add_server_screen.dart';
import '../themes/themes_screen.dart';
import 'notify_settings.dart';

/// The app's version, as in pubspec.yaml.
const appVersion = '0.1.0';

/// App settings (docs/CLIENTS.md 9.23 and 9.12 "For this device"). Every
/// choice here is an app preference on this device (8.2) and no server
/// sees it; each server's own settings are one tap away.
class AppSettingsScreen extends StatelessWidget {
  const AppSettingsScreen({super.key, this.showTitle = false});

  /// Draws a title row with a close button, for use in a dialog.
  final bool showTitle;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final app = AppScope.of(context);
    final prefs = app.prefs;
    final c = ChurrColors.of(context);
    return ListenableBuilder(
      listenable: prefs,
      builder: (context, _) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (showTitle) DialogTitleBar(title: t.appSettings),
          Expanded(
            child: ListView(
              padding: const EdgeInsets.fromLTRB(20, 0, 20, 24),
              children: [
                SectionHead(t.sidebarLayout),
                SegmentedButton<SidebarLayout>(
                  key: const Key('layout-choice'),
                  showSelectedIcon: false,
                  segments: [
                    ButtonSegment(
                      value: SidebarLayout.merged,
                      label: Text(t.layoutMerged),
                    ),
                    ButtonSegment(
                      value: SidebarLayout.rail,
                      label: Text(t.layoutRail),
                    ),
                    ButtonSegment(
                      value: SidebarLayout.hybrid,
                      label: Text(t.layoutHybrid),
                    ),
                  ],
                  selected: {prefs.layout},
                  onSelectionChanged: (s) => prefs.layout = s.first,
                ),
                SectionHead(t.showByDefault),
                // Chips, which wrap on a phone: the three labels are long.
                Wrap(
                  key: const Key('default-show'),
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    for (final v in const [
                      ShowSetting.always,
                      ShowSetting.activity,
                      ShowSetting.notification,
                    ])
                      ChoiceChip(
                        label: Text(showName(t, v)),
                        selected: prefs.defaultShow == v,
                        onSelected: (_) => prefs.defaultShow = v,
                      ),
                  ],
                ),
                ChurrSwitchTile(
                  key: const Key('show-hidden-switch'),
                  contentPadding: EdgeInsets.zero,
                  title: Text(t.showHiddenSwitch),
                  subtitle: Text(t.showHiddenNote),
                  value: prefs.showHidden,
                  onChanged: (v) => prefs.showHidden = v,
                ),
                SectionHead(t.measureSettings),
                Text(t.measureSettingsNote, style: TextStyle(color: c.dim)),
                const SizedBox(height: 8),
                Wrap(
                  key: const Key('measure-system'),
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    for (final (v, label) in [
                      (
                        '',
                        t.measureRegion(
                          regionSystem(
                                    WidgetsBinding
                                        .instance
                                        .platformDispatcher
                                        .locale
                                        .countryCode,
                                  ) ==
                                  'imperial'
                              ? t.measureImperial
                              : t.measureMetric,
                        ),
                      ),
                      ('metric', t.measureMetric),
                      ('imperial', t.measureImperial),
                      ('off', t.measureOff),
                    ])
                      ChoiceChip(
                        label: Text(label),
                        selected: prefs.measureSystem == v,
                        onSelected: (_) => prefs.measureSystem = v,
                      ),
                  ],
                ),
                SectionHead(t.servers),
                // In the person's order; dragged to reorder, which is the
                // order of the rail and the merged tree.
                ReorderableListView(
                  key: const Key('server-order'),
                  shrinkWrap: true,
                  physics: const NeverScrollableScrollPhysics(),
                  buildDefaultDragHandles: true,
                  onReorderItem: (from, to) =>
                      moveServer(app, app.servers[from].localId, to),
                  children: [
                    for (final s in app.servers)
                      Container(
                        key: ValueKey('order-${s.localId}'),
                        margin: const EdgeInsets.only(bottom: 4),
                        child: Material(
                          color: c.raised,
                          child: ListTile(
                            leading: ServerMark(server: s, size: 24),
                            title: Text(
                              s.name,
                              style: const TextStyle(
                                fontWeight: FontWeight.w800,
                              ),
                            ),
                            subtitle: Text(
                              s.entry.address,
                              style: monoStyle(context),
                            ),
                            trailing: Padding(
                              padding: const EdgeInsets.only(right: 24),
                              child: IconButton(
                                key: Key('server-settings-${s.localId}'),
                                tooltip: t.settingsFor,
                                icon: const Glyph(ChurrIcons.settings),
                                onPressed: () => showServerSettings(context, s),
                              ),
                            ),
                          ),
                        ),
                      ),
                  ],
                ),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    OutlinedButton.icon(
                      icon: const Glyph(ChurrIcons.add, size: 16),
                      label: Text(t.addAServer),
                      onPressed: () => Navigator.of(context).push(
                        MaterialPageRoute<void>(
                          builder: (_) => const AddServerScreen(),
                        ),
                      ),
                    ),
                    OutlinedButton.icon(
                      key: const Key('set-up-another-device'),
                      icon: const Glyph(ChurrIcons.qr, size: 16),
                      label: Text(t.setUpAnotherDevice),
                      onPressed: () => openSetupCode(context),
                    ),
                  ],
                ),
                SectionHead(t.theme),
                // Built-in, saved and server themes, the editor and the
                // libraries (lib/ui/screens/themes). The high-distinction
                // pair are built-in themes there.
                const ThemeChoiceSection(),
                SectionHead(t.textSize),
                Slider(
                  value: prefs.textScale.clamp(0.8, 1.6),
                  min: 0.8,
                  max: 1.6,
                  divisions: 8,
                  label: '${(prefs.textScale * 100).round()}%',
                  onChanged: (v) => prefs.textScale = (v * 10).round() / 10,
                ),
                ChurrSwitchTile(
                  contentPadding: EdgeInsets.zero,
                  title: Text(t.enterSends),
                  subtitle: Text(t.enterSendsNote),
                  value: prefs.enterSends,
                  onChanged: (v) => prefs.enterSends = v,
                ),
                SectionHead(t.notificationsSection),
                ChurrSwitchTile(
                  contentPadding: EdgeInsets.zero,
                  title: Text(t.showNotifications),
                  value: prefs.notificationsOn,
                  onChanged: (v) => prefs.notificationsOn = v,
                ),
                ChurrSwitchTile(
                  contentPadding: EdgeInsets.zero,
                  title: Text(t.notificationSoundsSwitch),
                  value: prefs.notificationSounds,
                  onChanged: (v) => prefs.notificationSounds = v,
                ),
                ChurrSwitchTile(
                  contentPadding: EdgeInsets.zero,
                  title: Text(t.notificationTextSwitch),
                  value: prefs.notificationText,
                  onChanged: (v) => prefs.notificationText = v,
                ),
                // Test notification, tray and login, Android push choice.
                const NotifySettingsSection(),
                SectionHead(t.sounds),
                ChurrSwitchTile(
                  key: const Key('startup-sound'),
                  contentPadding: EdgeInsets.zero,
                  title: Text(t.startupSound),
                  value: prefs.startupSound,
                  onChanged: (v) => prefs.startupSound = v,
                ),
                SectionHead(t.language),
                DropdownButton<String?>(
                  elevation: 0,
                  icon: const Glyph(ChurrIcons.chevronDown, size: 16),
                  value: prefs.language,
                  items: [
                    DropdownMenuItem(
                      value: null,
                      child: Text(t.languageSystem),
                    ),
                    DropdownMenuItem(
                      value: 'en',
                      child: Text(t.languageEnglish),
                    ),
                  ],
                  onChanged: (v) => prefs.language = v,
                ),
                SectionHead(t.fileCache),
                const _FileCache(),
                SectionHead(t.about),
                Text(t.appVersion(appVersion), style: monoStyle(context)),
                // Debug desktop builds keep secrets in a plain file
                // (lib/platform/secret_storage.dart).
                if (secretsInFile)
                  Text(
                    t.devSecretsInFile,
                    key: const Key('dev-secrets-in-file'),
                    style: TextStyle(color: ChurrColors.of(context).yellowInk),
                  ),
                Wrap(
                  spacing: 8,
                  children: [
                    TextButton(
                      onPressed: () => showLicensePage(
                        context: context,
                        applicationName: t.appTitle,
                        applicationVersion: appVersion,
                      ),
                      child: Text(t.licences),
                    ),
                    TextButton(
                      onPressed: () => launchUrl(
                        Uri.parse('https://github.com/tikaydaes/churr'),
                        mode: LaunchMode.externalApplication,
                      ),
                      child: Text(t.sourceCode),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// The file cache's size, its cap, and "Clear cache".
class _FileCache extends StatefulWidget {
  const _FileCache();

  @override
  State<_FileCache> createState() => _FileCacheState();
}

class _FileCacheState extends State<_FileCache> {
  int? used;

  List<String> get _dirs => [
    for (final s in AppScope.read(context).servers) ?s.filesDirectory,
  ];

  @override
  void initState() {
    super.initState();
    _measure();
  }

  Future<void> _measure() async {
    final n = await fileCacheSize(_dirs);
    if (mounted) setState(() => used = n);
  }

  String _mb(int bytes) => bytes >= 1 << 30
      ? '${(bytes / (1 << 30)).toStringAsFixed(1)} GB'
      : '${(bytes / (1 << 20)).round()} MB';

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final prefs = AppScope.of(context).prefs;
    final cap = prefs.fileCacheMb;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (used != null) Text(t.fileCacheSize(_mb(used!), _mb(cap << 20))),
        Wrap(
          spacing: 12,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            Text(t.fileCacheCap),
            DropdownButton<int>(
              elevation: 0,
              icon: const Glyph(ChurrIcons.chevronDown, size: 16),
              value: const [256, 512, 1024, 2048, 4096].contains(cap)
                  ? cap
                  : 1024,
              items: [
                for (final mb in const [256, 512, 1024, 2048, 4096])
                  DropdownMenuItem(value: mb, child: Text(_mb(mb << 20))),
              ],
              onChanged: (v) async {
                prefs.fileCacheMb = v!;
                await trimFileCache(_dirs, v << 20);
                await _measure();
              },
            ),
            OutlinedButton(
              onPressed: () async {
                final messenger = ScaffoldMessenger.maybeOf(context);
                await clearFileCache(_dirs);
                await _measure();
                messenger?.showSnackBar(
                  SnackBar(content: Text(t.cacheCleared)),
                );
              },
              child: Text(t.clearCache),
            ),
          ],
        ),
      ],
    );
  }
}
