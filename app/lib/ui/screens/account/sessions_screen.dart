import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../../core/api/account.dart';
import '../../../core/api/errors.dart';
import '../../../core/models/personal.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/server_state.dart';
import '../../theme/tokens.dart';
import '../../theme/churr_icons.dart';
import '../conversation/message_menu.dart' show confirm;
import 'common.dart';

/// Sessions and devices on one server (docs/AUTH.md 10.5): every browser,
/// app and token signed in, last seen, and signing any of them out. Not
/// kept in the cache: what is signed in is asked fresh each time.
class SessionsScreen extends StatefulWidget {
  const SessionsScreen({super.key, required this.server});
  final ServerState server;

  @override
  State<SessionsScreen> createState() => _SessionsScreenState();
}

class _SessionsScreenState extends State<SessionsScreen> {
  List<SessionInfo>? sessions;
  List<DeviceInfo>? devices;
  String? error;
  bool freshCheck = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final s = await widget.server.api.listSessions();
      final d = await widget.server.api.listDevices();
      if (mounted) {
        setState(() {
          sessions = s;
          devices = d;
        });
      }
    } on ApiException catch (e) {
      if (mounted) setState(() => error = e.message);
    }
  }

  Future<void> _run(Future<void> Function() action) async {
    try {
      await action();
      await _load();
    } on ApiException catch (e) {
      if (!mounted) return;
      // The apps hold no passkey: what needs a fresh check is done on the
      // account page in the browser (docs/CLIENTS.md 9.22).
      setState(() {
        freshCheck = e.code == 'fresh_check_required';
        error = freshCheck ? null : e.message;
      });
    }
  }

  Future<void> _endOthers() async {
    final t = L10n.of(context);
    if (!await confirm(
      context,
      t.endOtherSessionsTitle,
      t.endOtherSessionsBody,
      yes: t.endOtherSessions,
      danger: true,
    )) {
      return;
    }
    await _run(widget.server.api.endOtherSessions);
    if (mounted && error == null && !freshCheck) {
      ScaffoldMessenger.maybeOf(context)
        ?..hideCurrentSnackBar()
        ..showSnackBar(SnackBar(content: Text(t.endOtherSessionsDone)));
    }
  }

  void _openAccountPage() => launchUrl(
    Uri.parse(widget.server.entry.address).replace(path: '/account'),
    mode: LaunchMode.externalApplication,
  );

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    String kind(String k) => switch (k) {
      'app' => t.kindApp,
      'personal' => t.kindPersonal,
      'bot' => t.kindBot,
      _ => t.kindWeb,
    };
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 8, 20, 24),
      children: [
        if (freshCheck)
          NoteLine(
            t.freshCheckNeeded,
            action: TextButton(
              onPressed: _openAccountPage,
              child: Text(t.openAccountPage),
            ),
          ),
        if (error != null) ErrorLine(error!),
        SectionHead(t.sessions),
        if ((sessions ?? const <SessionInfo>[]).any((s) => !s.current))
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton(
              key: const Key('end-other-sessions'),
              style: TextButton.styleFrom(foregroundColor: c.pink),
              onPressed: _endOthers,
              child: Text(t.endOtherSessions),
            ),
          ),
        if (sessions == null) const LinearProgressIndicator(minHeight: 2),
        for (final s in sessions ?? const <SessionInfo>[])
          ListTile(
            contentPadding: EdgeInsets.zero,
            leading: Glyph(
              s.kind == 'app' ? ChurrIcons.phone : ChurrIcons.globe,
              color: c.dim,
            ),
            title: Text(s.label.isEmpty ? kind(s.kind) : s.label),
            subtitle: Text(
              [
                kind(s.kind),
                if (s.current) t.thisDevice,
                if (s.lastSeenAt != null)
                  t.lastSeen(formatWhen(context, s.lastSeenAt!)),
              ].join(' · '),
            ),
            trailing: s.current
                ? null
                : TextButton(
                    onPressed: () =>
                        _run(() => widget.server.api.endSession(s.id)),
                    child: Text(t.endSession),
                  ),
          ),
        SectionHead(t.devices),
        for (final d in devices ?? const <DeviceInfo>[])
          ListTile(
            contentPadding: EdgeInsets.zero,
            leading: Glyph(ChurrIcons.devices, color: c.dim),
            title: Text(d.name),
            subtitle: Text(
              [
                d.platform,
                if (d.current) t.thisDevice,
                if (d.retiredAt != null) t.retired,
                if (d.lastSeenAt != null)
                  t.lastSeen(formatWhen(context, d.lastSeenAt!)),
              ].join(' · '),
            ),
            trailing: d.current || d.retiredAt != null
                ? null
                : TextButton(
                    onPressed: () =>
                        _run(() => widget.server.api.removeDevice(d.id)),
                    child: Text(t.removeDevice),
                  ),
          ),
      ],
    );
  }
}
