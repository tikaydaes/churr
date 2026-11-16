import 'package:flutter/material.dart';

import '../../../core/api/errors.dart';
import '../../../core/api/scheduled.dart';
import '../../../core/api/watches.dart';
import '../../../core/forms/checkin.dart';
import '../../../l10n/app_localizations.dart';
import '../../../platform/device.dart';
import '../../../state/server_state.dart';
import '../../theme/churr_icons.dart';
import '../../theme/tokens.dart';
import '../account/common.dart';
import '../conversation/message_menu.dart' show confirm;

/// A channel's recurring check-ins (docs/FEATURES.md section 11), for its
/// managers: listed with when they run, set up, changed (whoever changes
/// one becomes its owner) and stopped. Asked fresh each time.
class CheckinsScreen extends StatefulWidget {
  const CheckinsScreen({
    super.key,
    required this.server,
    required this.conversationId,
    this.zone,
  });
  final ServerState server;
  final String conversationId;

  /// The time zone a new one starts in; the device's when null.
  final String? zone;

  @override
  State<CheckinsScreen> createState() => _CheckinsScreenState();
}

class _CheckinsScreenState extends State<CheckinsScreen> {
  List<ScheduledItem>? items;
  String? error;

  ServerState get server => widget.server;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final v = await server.api.listScheduledItems(
        kind: 'form',
        conversationId: widget.conversationId,
      );
      if (mounted) {
        setState(() {
          items = v;
          error = null;
        });
      }
    } on ApiException catch (e) {
      if (mounted) setState(() => error = e.message);
    }
  }

  Future<void> _new() async {
    final zone = widget.zone ?? await deviceTimeZone() ?? 'UTC';
    if (!mounted) return;
    final saved = await _edit(
      L10n.of(context).newCheckin,
      CheckinSetup.blank(zone),
      (v) =>
          server.api.createScheduledItem(v.createBody(widget.conversationId)),
    );
    if (saved) await _load();
  }

  Future<void> _change(ScheduledItem s) async {
    final saved = await _edit(
      (s.form?['title'] as String?) ?? L10n.of(context).checkinsTitle,
      CheckinSetup.fromItem(s.content, s.repeat),
      (v) => server.api.updateScheduled(
        s.id,
        content: v.content,
        repeat: v.repeat,
      ),
    );
    if (saved) await _load();
  }

  Future<bool> _edit(
    String title,
    CheckinSetup start,
    Future<void> Function(CheckinSetup) save,
  ) async =>
      await showDialog<bool>(
        animationStyle: AnimationStyle.noAnimation,
        context: context,
        builder: (_) => _CheckinEditor(title: title, setup: start, save: save),
      ) ??
      false;

  Future<void> _stop(ScheduledItem s) async {
    final t = L10n.of(context);
    if (!await confirm(
      context,
      t.stopCheckinTitle,
      t.stopCheckinBody,
      yes: t.stopCheckin,
      danger: true,
    )) {
      return;
    }
    try {
      await server.api.cancelScheduled(s.id);
      await _load();
    } on ApiException catch (e) {
      if (mounted) setState(() => error = e.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 8, 20, 24),
      children: [
        Text(t.checkinsIntro),
        const SizedBox(height: 12),
        Align(
          alignment: Alignment.centerLeft,
          child: FilledButton(
            key: const Key('new-checkin'),
            onPressed: _new,
            child: Text(t.newCheckin),
          ),
        ),
        if (error != null) ErrorLine(error!),
        SectionHead(t.checkinsTitle),
        if (items == null) const LinearProgressIndicator(minHeight: 2),
        if (items != null && items!.isEmpty) Text(t.checkinsNone),
        for (final s in items ?? const <ScheduledItem>[])
          ListTile(
            key: Key('checkin-${s.id}'),
            contentPadding: EdgeInsets.zero,
            title: Text(s.form?['title'] as String? ?? t.checkinsTitle),
            subtitle: Text(
              [
                repeatWords(s.repeat),
                t.checkinQuestions(
                  (s.form?['fields'] as List? ?? const []).length,
                ),
                if (s.failedAt != null)
                  t.checkinFailed(s.failure)
                else if (s.nextRunAt != null)
                  t.checkinNext(formatUntil(context, s.nextRunAt!)),
                t.checkinBy(server.accounts[s.accountId]?.name ?? '…'),
              ].join(' · '),
              style: TextStyle(color: s.failedAt != null ? c.pink : null),
            ),
            trailing: Wrap(
              children: [
                TextButton(
                  key: Key('edit-checkin-${s.id}'),
                  onPressed: () => _change(s),
                  child: Text(t.editScheduled),
                ),
                TextButton(
                  key: Key('stop-checkin-${s.id}'),
                  style: TextButton.styleFrom(foregroundColor: c.pink),
                  onPressed: () => _stop(s),
                  child: Text(t.stopCheckin),
                ),
              ],
            ),
          ),
      ],
    );
  }
}

class _CheckinEditor extends StatefulWidget {
  const _CheckinEditor({
    required this.title,
    required this.setup,
    required this.save,
  });
  final String title;
  final CheckinSetup setup;
  final Future<void> Function(CheckinSetup) save;

  @override
  State<_CheckinEditor> createState() => _CheckinEditorState();
}

class _CheckinEditorState extends State<_CheckinEditor> {
  CheckinSetup get v => widget.setup;
  String? error;
  bool busy = false;

  Future<void> _save() async {
    final bad = v.problem;
    if (bad != null) {
      setState(() => error = bad);
      return;
    }
    setState(() => busy = true);
    try {
      await widget.save(v);
      if (mounted) Navigator.pop(context, true);
    } on ApiException catch (e) {
      if (mounted) {
        setState(() {
          error = e.message;
          busy = false;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    return AlertDialog(
      scrollable: true,
      title: Text(widget.title),
      content: SizedBox(
        width: 480,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            TextFormField(
              key: const Key('checkin-title'),
              initialValue: v.title,
              maxLength: 100,
              decoration: InputDecoration(labelText: t.checkinTitleLabel),
              onChanged: (x) => v.title = x,
            ),
            TextFormField(
              key: const Key('checkin-text'),
              initialValue: v.text,
              minLines: 1,
              maxLines: 3,
              decoration: InputDecoration(labelText: t.checkinTextLabel),
              onChanged: (x) => v.text = x,
            ),
            SectionHead(t.checkinQuestionsHead),
            for (final (i, q) in v.questions.indexed) ...[
              Row(
                key: ObjectKey(q),
                children: [
                  Expanded(
                    child: TextFormField(
                      key: Key('checkin-q$i'),
                      initialValue: q.label,
                      maxLength: 200,
                      decoration: InputDecoration(
                        labelText: t.checkinQuestion(i + 1),
                        counterText: '',
                      ),
                      onChanged: (x) => q.label = x,
                    ),
                  ),
                  IconButton(
                    tooltip: t.checkinRemoveQuestion,
                    onPressed: () => setState(() => v.questions.removeAt(i)),
                    icon: const Glyph(ChurrIcons.close, size: 16),
                  ),
                ],
              ),
              Wrap(
                children: [
                  _Check(
                    label: t.checkinLong,
                    value: q.long,
                    onChanged: (x) => setState(() => q.long = x),
                  ),
                  _Check(
                    label: t.checkinRequired,
                    value: q.required,
                    onChanged: (x) => setState(() => q.required = x),
                  ),
                ],
              ),
            ],
            if (v.questions.length < CheckinSetup.maxQuestions)
              TextButton(
                key: const Key('checkin-add-question'),
                onPressed: () =>
                    setState(() => v.questions.add(CheckinQuestion(''))),
                child: Text(t.checkinAddQuestion),
              ),
            SectionHead(t.checkinWhen),
            Wrap(
              children: [
                for (var i = 0; i < 7; i++)
                  _Check(
                    key: Key('checkin-day-$i'),
                    label: checkinDayNames[i].substring(0, 3),
                    semantics: checkinDayNames[i],
                    value: v.days[i] == '1',
                    onChanged: (x) => setState(
                      () => v.days =
                          v.days.substring(0, i) +
                          (x ? '1' : '0') +
                          v.days.substring(i + 1),
                    ),
                  ),
              ],
            ),
            Row(
              children: [
                SizedBox(
                  width: 110,
                  child: TextFormField(
                    key: const Key('checkin-at'),
                    initialValue: v.at,
                    decoration: InputDecoration(labelText: t.checkinAt),
                    onChanged: (x) => v.at = x.trim(),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: TextFormField(
                    key: const Key('checkin-zone'),
                    initialValue: v.zone,
                    decoration: InputDecoration(labelText: t.checkinZone),
                    onChanged: (x) => v.zone = x.trim(),
                  ),
                ),
              ],
            ),
            if (error != null) ErrorLine(error!),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context, false),
          child: Text(t.cancel),
        ),
        FilledButton(
          key: const Key('checkin-save'),
          onPressed: busy ? null : _save,
          child: Text(t.save),
        ),
      ],
    );
  }
}

class _Check extends StatelessWidget {
  const _Check({
    super.key,
    required this.label,
    required this.value,
    required this.onChanged,
    this.semantics,
  });
  final String label;
  final String? semantics;
  final bool value;
  final ValueChanged<bool> onChanged;

  @override
  Widget build(BuildContext context) => Semantics(
    label: semantics,
    child: InkWell(
      onTap: () => onChanged(!value),
      child: Padding(
        padding: const EdgeInsets.only(right: 8),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Checkbox(value: value, onChanged: (x) => onChanged(x ?? false)),
            Text(label),
          ],
        ),
      ),
    ),
  );
}
