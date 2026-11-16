import 'package:flutter/material.dart';

import '../../../core/api/errors.dart';
import '../../../core/api/reference_patterns.dart';
import '../../../core/models/json.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/server_state.dart';
import '../../theme/tokens.dart';
import 'common.dart';

/// A channel's reference patterns (ruling R73): text such as PROJ-123
/// that becomes a link. Its managers add, change and remove them; everyone
/// may read what turns into a link here, with the server's patterns
/// listed after. The server checks every field and says why it refuses.
class ReferencePatternsTab extends StatefulWidget {
  const ReferencePatternsTab({
    super.key,
    required this.server,
    required this.conversationId,
    required this.manager,
  });

  final ServerState server;
  final String conversationId;
  final bool manager;

  @override
  State<ReferencePatternsTab> createState() => _ReferencePatternsTabState();
}

class _ReferencePatternsTabState extends State<ReferencePatternsTab> {
  List<Json>? all;
  String? problem;

  @override
  void initState() {
    super.initState();
    widget.server.addListener(_load);
    _load();
  }

  @override
  void dispose() {
    widget.server.removeListener(_load);
    super.dispose();
  }

  int _seen = -1;

  /// Reads the list again when `reference_patterns.changed` replaced the
  /// server state's copy.
  Future<void> _load() async {
    final now = widget.server.patternsFor(widget.conversationId);
    final stamp = Object.hashAll(
      now.map(
        (p) =>
            '${p['id']}${p['name']}${p['prefix']}${p['expression']}${p['url_template']}',
      ),
    );
    if (all != null && stamp == _seen) return;
    _seen = stamp;
    try {
      final got = await widget.server.api.listReferencePatterns(
        conversationId: widget.conversationId,
      );
      if (mounted) setState(() => all = got);
    } on Object catch (e) {
      if (mounted) setState(() => problem = refusal(e));
    }
  }

  Future<void> _edit([Json? p]) async {
    final saved = await showDialog<bool>(
      context: context,
      animationStyle: AnimationStyle.noAnimation,
      builder: (context) => _PatternDialog(
        pattern: p,
        save: (fields) async {
          final api = widget.server.api;
          if (p == null) {
            await api.createReferencePattern(
              fields,
              conversationId: widget.conversationId,
            );
          } else {
            await api.updateReferencePattern('${p['id']}', fields);
          }
        },
      ),
    );
    if (saved == true) {
      all = null;
      await _load();
    }
  }

  Future<void> _remove(Json p) async {
    final t = L10n.of(context);
    final yes = await showDialog<bool>(
      context: context,
      animationStyle: AnimationStyle.noAnimation,
      builder: (context) => AlertDialog(
        content: Text(t.patternRemoveAsk('${p['name']}')),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: Text(MaterialLocalizations.of(context).cancelButtonLabel),
          ),
          FilledButton(
            key: const Key('pattern-remove-yes'),
            onPressed: () => Navigator.of(context).pop(true),
            child: Text(t.patternRemove),
          ),
        ],
      ),
    );
    if (yes != true) return;
    try {
      await widget.server.api.deleteReferencePattern('${p['id']}');
      all = null;
      await _load();
    } on Object catch (e) {
      if (mounted) say(context, refusal(e));
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final list = all;
    if (list == null) {
      return Center(
        child: problem == null
            ? const CircularProgressIndicator()
            : Text(problem!, style: TextStyle(color: c.dim)),
      );
    }
    final mine = [
      for (final p in list)
        if (p['conversation_id'] == widget.conversationId) p,
    ];
    final servers = [
      for (final p in list)
        if ((p['conversation_id'] ?? '') == '') p,
    ];
    Widget row(Json p, bool editable) => ListTile(
      key: Key('pattern-${p['id']}'),
      dense: true,
      title: Text('${p['name']}'),
      subtitle: Text(
        '${p['prefix']}${(p['expression'] ?? '') == '' ? '123' : '…'} → ${p['url_template']}',
        style: monoStyle(context, size: 12),
      ),
      trailing: editable
          ? Wrap(
              children: [
                TextButton(
                  key: Key('pattern-edit-${p['id']}'),
                  onPressed: () => _edit(p),
                  child: Text(t.patternEdit),
                ),
                TextButton(
                  key: Key('pattern-remove-${p['id']}'),
                  onPressed: () => _remove(p),
                  child: Text(t.patternRemove),
                ),
              ],
            )
          : null,
    );
    return ListView(
      padding: const EdgeInsets.symmetric(vertical: 8),
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 4, 16, 8),
          child: Text(t.patternsAbout, style: TextStyle(color: c.dim)),
        ),
        if (widget.manager)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Align(
              alignment: Alignment.centerLeft,
              child: FilledButton(
                key: const Key('pattern-add'),
                onPressed: () => _edit(),
                child: Text(t.patternAdd),
              ),
            ),
          ),
        if (mine.isEmpty)
          Padding(
            padding: const EdgeInsets.all(16),
            child: Text(t.patternsNone, style: TextStyle(color: c.dim)),
          ),
        for (final p in mine) row(p, widget.manager),
        if (servers.isNotEmpty) ...[
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
            child: Text(
              t.patternsServerHere,
              style: monoStyle(context, size: 10),
            ),
          ),
          for (final p in servers) row(p, false),
        ],
      ],
    );
  }
}

/// Adds or changes one pattern. The dialog stays open, saying why, while
/// the server refuses.
class _PatternDialog extends StatefulWidget {
  const _PatternDialog({required this.pattern, required this.save});
  final Json? pattern;
  final Future<void> Function(Json fields) save;

  @override
  State<_PatternDialog> createState() => _PatternDialogState();
}

class _PatternDialogState extends State<_PatternDialog> {
  late final name = TextEditingController(
    text: '${widget.pattern?['name'] ?? ''}',
  );
  late final prefix = TextEditingController(
    text: '${widget.pattern?['prefix'] ?? ''}',
  );
  late final expression = TextEditingController(
    text: '${widget.pattern?['expression'] ?? ''}',
  );
  late final url = TextEditingController(
    text: '${widget.pattern?['url_template'] ?? ''}',
  );
  String? problem;
  bool busy = false;

  @override
  void dispose() {
    for (final c in [name, prefix, expression, url]) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _save() async {
    setState(() {
      busy = true;
      problem = null;
    });
    try {
      await widget.save({
        'name': name.text.trim(),
        'prefix': prefix.text,
        'expression': expression.text,
        'url_template': url.text.trim(),
      });
      if (mounted) Navigator.of(context).pop(true);
    } on ApiException catch (e) {
      if (mounted) setState(() => problem = e.message);
    } on Object catch (e) {
      if (mounted) setState(() => problem = refusal(e));
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    Widget field(
      Key key,
      TextEditingController ctl,
      String label, {
      String? help,
      int max = 64,
    }) => Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: TextField(
        key: key,
        controller: ctl,
        maxLength: max,
        autocorrect: false,
        decoration: InputDecoration(
          labelText: label,
          helperText: help,
          helperMaxLines: 3,
          counterText: '',
        ),
      ),
    );
    return AlertDialog(
      title: Text(widget.pattern == null ? t.patternAdd : t.patternEdit),
      content: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 440),
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              field(const Key('pattern-name'), name, t.patternName),
              field(
                const Key('pattern-prefix'),
                prefix,
                t.patternPrefix,
                help: t.patternPrefixHelp,
                max: 16,
              ),
              field(
                const Key('pattern-expression'),
                expression,
                t.patternExpression,
                help: t.patternExpressionHelp,
              ),
              field(
                const Key('pattern-url'),
                url,
                t.patternURL,
                help: t.patternURLHelp('{value}'),
                max: 500,
              ),
              if (problem != null)
                Semantics(
                  liveRegion: true,
                  child: Text(problem!, style: TextStyle(color: c.pink)),
                ),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: Text(MaterialLocalizations.of(context).cancelButtonLabel),
        ),
        FilledButton(
          key: const Key('pattern-save'),
          onPressed: busy ? null : _save,
          child: Text(t.patternSave),
        ),
      ],
    );
  }
}
