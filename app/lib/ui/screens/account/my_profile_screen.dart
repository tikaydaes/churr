import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../../core/models/account.dart';
import '../../../core/models/personal.dart';
import '../../../l10n/app_localizations.dart';
import '../../../platform/device.dart' as device;
import '../../../state/account_hub.dart';
import '../../../state/server_state.dart';
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../../theme/churr_icons.dart';
import '../../widgets/avatar.dart';
import 'common.dart';

/// One's own profile (docs/CLIENTS.md 9.11 and 8.9): the same fields as
/// someone else's, editable, one tab per server, since a person can be
/// someone slightly different on each. Saving asks which servers to
/// apply the change to, all ticked to begin with.
class MyProfileScreen extends StatefulWidget {
  const MyProfileScreen({
    super.key,
    this.pickImage,
    this.deviceZone,
    this.initialServer,
  });

  /// Replaced in tests, which have no file picker.
  final Future<(String, List<int>)?> Function()? pickImage;
  final Future<String?> Function()? deviceZone;
  final ServerState? initialServer;

  @override
  State<MyProfileScreen> createState() => _MyProfileScreenState();
}

class _MyProfileScreenState extends State<MyProfileScreen>
    with TickerProviderStateMixin {
  late TabController tabs;
  late List<ServerState> servers;
  String? zone;

  @override
  void initState() {
    super.initState();
    servers = AppScope.read(context).servers.toList();
    tabs = TabController(
      length: servers.length,
      vsync: this,
      initialIndex: widget.initialServer == null
          ? 0
          : servers.indexOf(widget.initialServer!).clamp(0, servers.length - 1),
    );
    (widget.deviceZone ?? device.deviceTimeZone)().then((z) {
      if (mounted) setState(() => zone = z);
    });
  }

  @override
  void dispose() {
    tabs.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (servers.isEmpty) return const SizedBox.shrink();
    final c = ChurrColors.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (servers.length > 1)
          Container(
            color: c.panel,
            child: TabBar(
              controller: tabs,
              isScrollable: true,
              tabs: [for (final s in servers) Tab(text: s.name)],
            ),
          ),
        Expanded(
          child: TabBarView(
            controller: tabs,
            children: [
              for (final s in servers)
                _ProfileForm(
                  key: ValueKey(s.localId),
                  server: s,
                  allServers: servers,
                  zone: zone,
                  pickImage: widget.pickImage ?? device.pickImage,
                ),
            ],
          ),
        ),
      ],
    );
  }
}

class _ProfileForm extends StatefulWidget {
  const _ProfileForm({
    super.key,
    required this.server,
    required this.allServers,
    required this.zone,
    required this.pickImage,
  });
  final ServerState server;
  final List<ServerState> allServers;
  final String? zone;
  final Future<(String, List<int>)?> Function() pickImage;

  @override
  State<_ProfileForm> createState() => _ProfileFormState();
}

class _ProfileFormState extends State<_ProfileForm> {
  late final TextEditingController name, title, pronouns, timeZone;
  (String, List<int>)? avatar;
  bool removeAvatar = false;
  List<String> problems = const [];
  String? note;
  bool busy = false;

  Me? get me => widget.server.me;

  @override
  void initState() {
    super.initState();
    name = TextEditingController(text: me?.displayName ?? '');
    title = TextEditingController(text: me?.title ?? '');
    pronouns = TextEditingController(text: me?.pronouns ?? '');
    timeZone = TextEditingController(text: me?.timeZone ?? '');
  }

  @override
  void dispose() {
    for (final c in [name, title, pronouns, timeZone]) {
      c.dispose();
    }
    super.dispose();
  }

  /// The fields changed in this tab against this server's values.
  Map<String, dynamic> get changes => {
    if (name.text.trim() != (me?.displayName ?? ''))
      'display_name': name.text.trim(),
    if (title.text.trim() != (me?.title ?? '')) 'title': title.text.trim(),
    if (pronouns.text.trim() != (me?.pronouns ?? ''))
      'pronouns': pronouns.text.trim(),
    if (timeZone.text.trim() != (me?.timeZone ?? ''))
      'time_zone': timeZone.text.trim(),
  };

  Future<void> _save() async {
    final t = L10n.of(context);
    final c = changes;
    if (c.isEmpty && avatar == null && !removeAvatar) return;
    final to = widget.allServers.length == 1
        ? widget.allServers
        : await _askApplyTo();
    if (to == null || to.isEmpty || !mounted) return;
    setState(() {
      busy = true;
      problems = const [];
      note = null;
    });
    final refused = await AccountHub.of(AppScope.read(context)).applyProfile(
      c,
      to: to,
      avatarName: avatar?.$1,
      avatarBytes: avatar?.$2,
      removeAvatar: removeAvatar,
    );
    if (!mounted) return;
    setState(() {
      busy = false;
      problems = [for (final e in refused.entries) t.refusedOn(e.key, e.value)];
      if (refused.isEmpty) note = t.saved;
      avatar = null;
      removeAvatar = false;
    });
  }

  Future<List<ServerState>?> _askApplyTo() {
    final t = L10n.of(context);
    final ticked = {...widget.allServers};
    return showDialog<List<ServerState>>(
      context: context,
      builder: (context) => StatefulBuilder(
        builder: (context, setState) => AlertDialog(
          title: Text(t.applyTo),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(t.applyToNote),
              for (final s in widget.allServers)
                CheckboxListTile(
                  key: Key('apply-${s.localId}'),
                  value: ticked.contains(s),
                  title: Text(s.name),
                  onChanged: (v) =>
                      setState(() => v! ? ticked.add(s) : ticked.remove(s)),
                ),
            ],
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context),
              child: Text(t.cancel),
            ),
            FilledButton(
              key: const Key('apply-profile'),
              onPressed: () => Navigator.pop(context, ticked.toList()),
              child: Text(t.apply),
            ),
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final s = widget.server;
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        Row(
          children: [
            if (avatar != null)
              Container(
                width: 64,
                height: 64,
                color: c.raised,
                child: Glyph(ChurrIcons.image, color: c.dim),
              )
            else
              Avatar(account: removeAvatar ? null : me, server: s, size: 64),
            const SizedBox(width: 16),
            Expanded(
              child: Wrap(
                spacing: 8,
                children: [
                  OutlinedButton(
                    onPressed: () async {
                      final picked = await widget.pickImage();
                      if (picked != null) setState(() => avatar = picked);
                    },
                    child: Text(t.changeAvatar),
                  ),
                  if (me?.avatarUrl != null)
                    TextButton(
                      onPressed: () => setState(() => removeAvatar = true),
                      child: Text(t.removeAvatar),
                    ),
                ],
              ),
            ),
          ],
        ),
        const SizedBox(height: 16),
        TextField(
          key: const Key('profile-name'),
          controller: name,
          maxLength: 64,
          decoration: InputDecoration(labelText: t.displayName),
        ),
        TextField(
          controller: title,
          decoration: InputDecoration(labelText: t.title),
        ),
        TextField(
          controller: pronouns,
          decoration: InputDecoration(labelText: t.pronouns),
        ),
        TextField(
          controller: timeZone,
          decoration: InputDecoration(labelText: t.timeZone),
        ),
        if (widget.zone != null && widget.zone != timeZone.text)
          Align(
            alignment: AlignmentDirectional.centerStart,
            child: TextButton(
              onPressed: () => setState(() => timeZone.text = widget.zone!),
              child: Text(t.useDeviceZone(widget.zone!)),
            ),
          ),
        const SizedBox(height: 8),
        InputDecorator(
          decoration: InputDecoration(
            labelText: t.username,
            helperText: t.usernameNote,
            helperMaxLines: 2,
          ),
          child: Text(
            '@${me?.username ?? ''}',
            style: monoStyle(context, size: 13),
          ),
        ),
        const SizedBox(height: 16),
        FilledButton(
          key: const Key('save-profile'),
          onPressed: busy ? null : _save,
          child: Text(t.save),
        ),
        for (final p in problems) ErrorLine(p),
        if (note != null)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Text(note!, style: TextStyle(color: c.cyan)),
          ),
        const SizedBox(height: 16),
        ListTile(
          contentPadding: EdgeInsets.zero,
          leading: const Glyph(ChurrIcons.external),
          title: Text(t.accountPage),
          subtitle: Text(t.accountPageNote(s.name)),
          onTap: () => launchUrl(
            Uri.parse(s.entry.address).replace(path: '/account'),
            mode: LaunchMode.externalApplication,
          ),
        ),
      ],
    );
  }
}

/// The parts of the profile drawn for the person's own account in
/// headers and menus.
extension MeLine on Me {
  String statusLine(DateTime now) {
    final s = fullStatus;
    if (s == null || !s.existsAt(now)) return '';
    return [?s.emoji, ?s.text].join(' ');
  }
}
