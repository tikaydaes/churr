import 'package:flutter/material.dart';

import '../../../core/api/account.dart';
import '../../../core/api/errors.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/server_state.dart';
import '../../theme/churr_icons.dart';
import '../../theme/tokens.dart';
import 'common.dart';

/// The accounts the person has blocked on one server, with Unblock
/// (docs/FEATURES.md, blocking). A blocked account's messages are
/// collapsed, it cannot start a chat, and its mentions do not notify; it
/// is told nothing. Asked fresh each time.
class BlockedScreen extends StatefulWidget {
  const BlockedScreen({super.key, required this.server});
  final ServerState server;

  @override
  State<BlockedScreen> createState() => _BlockedScreenState();
}

class _BlockedScreenState extends State<BlockedScreen> {
  List<String>? ids;
  final names = <String, String>{};
  String? error;

  ServerState get server => widget.server;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final got = await server.api.listBlocks();
      for (final id in got) {
        if (server.accounts[id] != null || names.containsKey(id)) continue;
        try {
          names[id] = (await server.api.getAccount(id)).name;
        } on ApiException {
          // Shown as an account the person can no longer see.
        }
      }
      if (mounted) {
        setState(() {
          ids = got;
          error = null;
        });
      }
    } on ApiException catch (e) {
      if (mounted) setState(() => error = e.message);
    }
  }

  Future<void> _unblock(String id) async {
    try {
      await server.api.unblockAccount(id);
      await _load();
    } on ApiException catch (e) {
      if (mounted) setState(() => error = e.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final enabled = server.info?.features['blocking'] ?? true;
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 8, 20, 24),
      children: [
        Text(enabled ? t.blockedIntro : t.blockingOff),
        if (error != null) ErrorLine(error!),
        SectionHead(t.blockedTitle),
        if (ids == null) const LinearProgressIndicator(minHeight: 2),
        if (ids != null && ids!.isEmpty) Text(t.blockedNone),
        for (final id in ids ?? const <String>[])
          ListTile(
            key: Key('blocked-$id'),
            contentPadding: EdgeInsets.zero,
            leading: Glyph(ChurrIcons.eyeOff, color: c.dim),
            title: Text(
              server.accounts[id]?.name ?? names[id] ?? t.blockedUnknown,
            ),
            trailing: TextButton(
              key: Key('unblock-$id'),
              onPressed: () => _unblock(id),
              child: Text(t.unblock),
            ),
          ),
      ],
    );
  }
}
