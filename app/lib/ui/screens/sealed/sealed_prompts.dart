import 'package:flutter/material.dart';

import '../../../l10n/app_localizations.dart';
import '../../../state/app_state.dart';
import '../../../state/sealed_state.dart';
import '../conversation/message_menu.dart' show confirm;
import 'sealed_devices_screen.dart';

/// Asks the person, once each, what only they can answer, on whichever
/// server it comes from: a new device of theirs asking to be unlocked
/// (docs/SEALED.md section 13.7), and a passkey with an unlock key and
/// no copy yet ("Let the passkey unlock sealed chats?", section 13.4).
class SealedPromptHost extends StatefulWidget {
  const SealedPromptHost({super.key, required this.app, required this.child});
  final AppState app;
  final Widget child;

  @override
  State<SealedPromptHost> createState() => _SealedPromptHostState();
}

class _SealedPromptHostState extends State<SealedPromptHost> {
  final _watched = <SealedController>{};
  final _asked = <String>{};
  bool _showing = false;

  @override
  void initState() {
    super.initState();
    widget.app.addListener(_servers);
    _servers();
  }

  void _servers() {
    for (final s in widget.app.servers) {
      final c = SealedHub.instance.of(s);
      if (c != null && _watched.add(c)) c.addListener(() => _check(c));
    }
  }

  Future<void> _check(SealedController c) async {
    final s = c.session;
    if (s == null || _showing || !mounted || !s.unlocked) return;
    final thisId = '${s.device?.id}';
    for (final r in s.requests) {
      if (r.deviceId == thisId || !_asked.add('r${r.id}')) continue;
      _showing = true;
      try {
        if (!mounted) return;
        await showApproveDevice(context, c.server, r);
      } finally {
        _showing = false;
      }
      return;
    }
    for (final p in s.passkeys) {
      if (p.canUnlock || p.unlockPublic == null || !_asked.add('p${p.id}')) {
        continue;
      }
      if (!mounted) return;
      final t = L10n.of(context);
      _showing = true;
      try {
        if (await confirm(
          context,
          t.passkeyApproveTitle(p.name),
          t.passkeyApproveBody,
          yes: t.passkeyApproveYes,
        )) {
          await s.approvePasskey(p);
        }
      } on Object {
        // Offered again from the devices screen.
      } finally {
        _showing = false;
      }
      return;
    }
  }

  @override
  void dispose() {
    widget.app.removeListener(_servers);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
