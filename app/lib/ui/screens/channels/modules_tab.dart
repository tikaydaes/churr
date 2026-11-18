import 'package:flutter/material.dart';

import '../../../core/api/conversation_ops.dart';
import '../../../core/api/modules.dart';
import '../../../core/db/conversation_queries.dart';
import '../../../core/models/json.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/server_state.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import 'common.dart';

/// A channel's built-in modules (docs/DESIGN-MODULES.md section 6.5), each
/// with a switch. Its managers switch one off here, or on again; one the
/// server has off cannot be switched on. Everyone else sees the switches
/// and cannot use them.
class ModulesTab extends StatefulWidget {
  const ModulesTab({
    super.key,
    required this.server,
    required this.conversationId,
    required this.manager,
  });

  final ServerState server;
  final String conversationId;
  final bool manager;

  @override
  State<ModulesTab> createState() => _ModulesTabState();
}

class _ModulesTabState extends State<ModulesTab> {
  List<Json>? modules;
  String? problem;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final got = await widget.server.api.listConversationModules(
        widget.conversationId,
      );
      if (mounted) setState(() => modules = got);
    } on Object catch (e) {
      if (mounted) setState(() => problem = refusal(e));
    }
  }

  Future<void> _switch(Json m, bool on) async {
    final api = widget.server.api;
    try {
      await api.setConversationModule(
        widget.conversationId,
        '${m['name']}',
        on: on,
      );
      // The composer's commands are kept per conversation; they change
      // with the switch.
      await widget.server.db.putCommands(
        widget.conversationId,
        await api.listCommands(widget.conversationId),
      );
    } on Object catch (e) {
      if (mounted) say(context, refusal(e));
    }
    await _load();
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final list = modules;
    if (list == null) {
      return Center(
        child: problem == null
            ? const CircularProgressIndicator()
            : Text(problem!, style: TextStyle(color: c.dim)),
      );
    }
    Widget row(Json m) {
      final serverOn = m['server_on'] == true;
      final commands = [
        for (final x in (m['commands'] as List?) ?? const [])
          if (x is Map) '/${x['name']}',
      ].join(', ');
      return ChurrSwitchTile(
        key: Key('module-${m['name']}'),
        title: Text('${m['title']}'),
        subtitle: Text(serverOn ? commands : t.modulesServerOff),
        value: m['on'] == true,
        onChanged: widget.manager && serverOn ? (on) => _switch(m, on) : null,
      );
    }

    return ListView(
      padding: const EdgeInsets.symmetric(vertical: 8),
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 4, 16, 8),
          child: Text(t.modulesAbout, style: TextStyle(color: c.dim)),
        ),
        if (!widget.manager)
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
            child: Text(t.modulesManagersOnly, style: TextStyle(color: c.dim)),
          ),
        if (list.isEmpty)
          Padding(
            padding: const EdgeInsets.all(16),
            child: Text(t.modulesNone, style: TextStyle(color: c.dim)),
          ),
        for (final m in list) row(m),
      ],
    );
  }
}
