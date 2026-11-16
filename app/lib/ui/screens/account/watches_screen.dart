import 'package:flutter/material.dart';

import '../../../core/api/errors.dart';
import '../../../core/api/watches.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/server_state.dart';
import '../../theme/churr_icons.dart';
import '../../theme/tokens.dart';
import 'common.dart';

/// The person's waiting watches on one server, "tell me when Ada is
/// around" (ruling R76, docs/CLIENTS.md 9.11): who each waits for and until
/// when, and stopping one. Not kept in the cache: asked fresh each time, as
/// the sessions are.
class WatchesScreen extends StatefulWidget {
  const WatchesScreen({super.key, required this.server});
  final ServerState server;

  @override
  State<WatchesScreen> createState() => _WatchesScreenState();
}

class _WatchesScreenState extends State<WatchesScreen> {
  List<Watch>? watches;
  String? error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final w = await widget.server.api.listWatches();
      if (mounted) setState(() => watches = w);
    } on ApiException catch (e) {
      if (mounted) setState(() => error = e.message);
    }
  }

  Future<void> _stop(Watch w) async {
    try {
      await widget.server.api.cancelScheduled(w.id);
      await _load();
    } on ApiException catch (e) {
      if (mounted) setState(() => error = e.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    return ListenableBuilder(
      listenable: widget.server,
      builder: (context, _) => ListView(
        padding: const EdgeInsets.fromLTRB(20, 8, 20, 24),
        children: [
          if (error != null) ErrorLine(error!),
          SectionHead(t.watchingTitle),
          if (watches == null) const LinearProgressIndicator(minHeight: 2),
          for (final w in watches ?? const <Watch>[])
            ListTile(
              key: Key('watch-${w.accountId}'),
              contentPadding: EdgeInsets.zero,
              leading: Glyph(ChurrIcons.notify, color: c.dim),
              title: Text(
                t.watchFor(widget.server.accounts[w.accountId]?.name ?? '…'),
              ),
              subtitle: Text(t.watchUntil(formatWhen(context, w.expiresAt!))),
              trailing: TextButton(
                onPressed: () => _stop(w),
                child: Text(t.stopWatching),
              ),
            ),
        ],
      ),
    );
  }
}
