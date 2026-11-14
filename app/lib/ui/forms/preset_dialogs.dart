import 'package:flutter/material.dart';

import '../../l10n/app_localizations.dart';
import '../../core/api/errors.dart';
import '../../core/forms/presets.dart';
import '../../core/models/json.dart';
import '../../state/channel_actions.dart';
import '../../state/form_actions.dart';
import '../../state/server_state.dart';
import '../message/checklist.dart' show shortWhen;
import '../theme/tokens.dart';
import '../widgets/controls.dart';
import '../theme/churr_icons.dart';

/// The built-in commands that post a form, with the hint the composer
/// shows beside each (docs/CLIENTS.md 9.5).
const formCommands = {
  'poll': 'Question? | Option | Option',
  'rsvp': 'What | When',
  'signup': 'What | Slot | Slot (3)',
  'approve': 'What',
  'form': 'Open the form builder',
};

/// Carries out `/poll`, `/rsvp`, `/signup`, `/approve` or `/form` typed
/// in the composer of [conversationId].
///
/// Command text that reads completely is posted at once (FEATURES.md
/// 7.5). With nothing after the command, or for `/form`, the preset's
/// dialog opens; text that does not read opens the dialog too, with the
/// reason shown, so nothing typed is lost. Returns whether a form was
/// posted.
Future<bool> runFormCommand(
  BuildContext context, {
  required ServerState server,
  required String conversationId,
  required String command,
  String args = '',
  String? parentId,
  DateTime Function() now = DateTime.now,
}) async {
  String? problem;
  Json? form;
  final groupOf = _groupResolver(server);
  if (args.trim().isNotEmpty && command != 'form') {
    try {
      form = switch (command) {
        'poll' => pollForm(args, now()),
        'rsvp' => rsvpForm(args, now()),
        'signup' => signupForm(args, now()),
        'approve' => approveForm(args, now(), groupIdOf: groupOf),
        _ => throw PresetError('/$command is not a form command.'),
      };
    } on PresetError catch (e) {
      problem = e.message;
    }
  }
  if (form == null) {
    if (!context.mounted) return false;
    form = await showPresetDialog(
      context,
      server: server,
      conversationId: conversationId,
      command: command,
      args: args,
      problem: problem,
      now: now,
    );
  }
  if (form == null) return false;
  try {
    await FormActions(server).post(conversationId, form, parentId: parentId);
    return true;
  } on ApiException catch (e) {
    if (context.mounted) {
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(
        SnackBar(content: Text(L10n.of(context).formNotPosted(e.message))),
      );
    }
    return false;
  }
}

String? Function(String) _groupResolver(ServerState server) => (handle) {
  for (final g in server.groups.values) {
    if (g.handle == handle) return g.id;
  }
  return null;
};

/// Opens the dialog for [command] and returns the form built, or null.
Future<Json?> showPresetDialog(
  BuildContext context, {
  required ServerState server,
  required String conversationId,
  required String command,
  String args = '',
  String? problem,
  DateTime Function() now = DateTime.now,
}) {
  final body = _PresetBuilder(
    server: server,
    conversationId: conversationId,
    command: command,
    args: args,
    problem: problem,
    now: now,
  );
  if (MediaQuery.sizeOf(context).width < 600) {
    return Navigator.of(context).push<Json>(
      MaterialPageRoute(
        fullscreenDialog: true,
        builder: (_) => Scaffold(body: SafeArea(child: body)),
      ),
    );
  }
  return showDialog<Json>(
    animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (_) => Dialog(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 560, maxHeight: 720),
        child: body,
      ),
    ),
  );
}

/// One field being made in the free form builder.
class _DraftField {
  _DraftField(this.type);
  String type;
  final label = TextEditingController();
  final help = TextEditingController();
  final options = TextEditingController();
  bool required = false;
  bool multiple = false;

  void dispose() {
    label.dispose();
    help.dispose();
    options.dispose();
  }
}

class _DraftButton {
  final label = TextEditingController();
  String style = 'default';
}

class _PresetBuilder extends StatefulWidget {
  const _PresetBuilder({
    required this.server,
    required this.conversationId,
    required this.command,
    required this.args,
    required this.now,
    this.problem,
  });
  final ServerState server;
  final String conversationId;
  final String command;
  final String args;
  final String? problem;
  final DateTime Function() now;

  @override
  State<_PresetBuilder> createState() => _PresetBuilderState();
}

class _PresetBuilderState extends State<_PresetBuilder> {
  final title = TextEditingController();
  final options = <TextEditingController>[];
  bool anonymous = false, multiple = false, addOptions = false;
  String results = 'live';
  DateTime? closesAt;
  DateTime? when;
  String? groupId;
  final fields = <_DraftField>[];
  final buttons = <_DraftButton>[];
  String? error;

  String get command => widget.command;

  @override
  void initState() {
    super.initState();
    error = widget.problem;
    // Whatever was typed is carried into the dialog.
    final parts = widget.args.trim().isEmpty
        ? <String>[]
        : splitParts(widget.args);
    if (parts.isNotEmpty) {
      title.text = parts.first.replaceAll(RegExp(r'\s--.*$'), '');
    }
    final rest = parts.length > 1 ? parts.sublist(1) : <String>[];
    if (command == 'poll' || command == 'signup') {
      for (final r in rest) {
        options.add(
          TextEditingController(text: r.replaceAll(RegExp(r'\s--.*$'), '')),
        );
      }
      while (options.length < 2) {
        options.add(TextEditingController());
      }
    }
    if (command == 'form') {
      fields.add(_DraftField('text'));
    }
  }

  @override
  void dispose() {
    title.dispose();
    for (final o in options) {
      o.dispose();
    }
    for (final f in fields) {
      f.dispose();
    }
    for (final b in buttons) {
      b.label.dispose();
    }
    super.dispose();
  }

  String get heading => switch (command) {
    'poll' => L10n.of(context).newPoll,
    'rsvp' => L10n.of(context).newRsvp,
    'signup' => L10n.of(context).newSignup,
    'approve' => L10n.of(context).askApproval,
    _ => L10n.of(context).newForm,
  };

  bool get _manager {
    final conv = widget.server.conversation(widget.conversationId);
    return conv != null && ChannelActions(widget.server).actsAsManager(conv);
  }

  Json _build() {
    final now = widget.now();
    switch (command) {
      case 'poll':
        return buildPoll(
          question: title.text,
          options: [for (final o in options) o.text],
          anonymous: anonymous,
          multiple: multiple,
          addOptions: addOptions,
          results: results,
          closesAt: closesAt,
          now: now,
        );
      case 'rsvp':
        return buildRsvp(what: title.text, when: when, now: now);
      case 'signup':
        return buildSignup(
          what: title.text,
          slots: [for (final o in options) o.text],
          multiple: multiple,
          closesAt: closesAt,
          now: now,
        );
      case 'approve':
        return buildApprove(
          what: title.text,
          groupId: groupId,
          closesAt: closesAt,
          now: now,
        );
    }
    // The free builder: any fields, `collect`.
    final out = <Json>[];
    for (var i = 0; i < fields.length; i++) {
      final f = fields[i];
      final opts = [
        for (final line in f.options.text.split('\n'))
          if (line.trim().isNotEmpty) line.trim(),
      ];
      out.add({
        'id': 'f${i + 1}',
        'type': f.type,
        'label': f.label.text.trim(),
        if (f.help.text.trim().isNotEmpty) 'help': f.help.text.trim(),
        if (f.required) 'required': true,
        if (f.multiple &&
            {'choice', 'account', 'conversation'}.contains(f.type))
          'multiple': true,
        if (f.type == 'choice')
          'options': [
            for (var j = 0; j < opts.length; j++)
              {'id': 'o${j + 1}', 'label': opts[j]},
          ],
      });
    }
    final form = <String, dynamic>{
      if (title.text.trim().isNotEmpty) 'title': title.text.trim(),
      'fields': out,
      if (buttons.isNotEmpty)
        'buttons': [
          for (var i = 0; i < buttons.length; i++)
            {
              'id': 'b${i + 1}',
              'label': buttons[i].label.text.trim(),
              'style': buttons[i].style,
            },
        ],
      'action': {'type': 'collect'},
      if (anonymous) 'anonymous': true,
      if (closesAt != null) 'closes_at': formatTime(closesAt!),
    };
    checkBuiltForm(form);
    return form;
  }

  void _post() {
    try {
      Navigator.of(context).pop(_build());
    } on PresetError catch (e) {
      setState(() => error = e.message);
    }
  }

  Future<DateTime?> _pickMoment(DateTime? current) async {
    final now = widget.now();
    final d = await showDatePicker(
      context: context,
      firstDate: DateTime(now.year, now.month, now.day),
      lastDate: DateTime(now.year + 5),
      initialDate: current ?? now,
    );
    if (d == null || !mounted) return null;
    final t = await showTimePicker(
      context: context,
      initialTime: current == null
          ? const TimeOfDay(hour: 9, minute: 0)
          : TimeOfDay.fromDateTime(current),
    );
    if (t == null) return null;
    return DateTime(d.year, d.month, d.day, t.hour, t.minute);
  }

  Widget _momentRow(
    String label,
    DateTime? value,
    ValueChanged<DateTime?> set, {
    bool clearable = true,
  }) {
    return Row(
      children: [
        Expanded(child: Text(label)),
        TextButton.icon(
          key: Key('moment-$label'),
          icon: const Glyph(ChurrIcons.calendar, size: 16),
          label: Text(value == null ? L10n.of(context).pick : shortWhen(value)),
          onPressed: () async {
            final v = await _pickMoment(value);
            if (v != null) setState(() => set(v));
          },
        ),
        if (clearable && value != null)
          IconButton(
            tooltip: L10n.of(context).noValue(label),
            icon: const Glyph(ChurrIcons.close, size: 16),
            onPressed: () => setState(() => set(null)),
          ),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final rows = <Widget>[
      TextField(
        key: const Key('preset-title'),
        controller: title,
        autofocus: true,
        decoration: InputDecoration(
          labelText: switch (command) {
            'poll' => L10n.of(context).question,
            'form' => L10n.of(context).titleOptional,
            _ => L10n.of(context).what,
          },
        ),
      ),
      const SizedBox(height: 12),
    ];
    if (command == 'poll' || command == 'signup') {
      for (var i = 0; i < options.length; i++) {
        rows.add(
          Row(
            children: [
              Expanded(
                child: TextField(
                  key: Key('preset-option-$i'),
                  controller: options[i],
                  decoration: InputDecoration(
                    labelText: command == 'poll'
                        ? L10n.of(context).optionN(i + 1)
                        : L10n.of(context).slotN(i + 1),
                    helperText: command == 'signup' && i == 0
                        ? L10n.of(context).slotPlacesHelp
                        : null,
                  ),
                ),
              ),
              if (options.length > (command == 'poll' ? 2 : 1))
                IconButton(
                  tooltip: L10n.of(context).remove,
                  icon: const Glyph(ChurrIcons.remove, size: 18),
                  onPressed: () =>
                      setState(() => options.removeAt(i).dispose()),
                ),
            ],
          ),
        );
      }
      if (options.length < 50) {
        rows.add(
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton.icon(
              key: const Key('preset-add-option'),
              icon: const Glyph(ChurrIcons.add, size: 16),
              label: Text(
                command == 'poll'
                    ? L10n.of(context).addOption
                    : L10n.of(context).addSlot,
              ),
              onPressed: () =>
                  setState(() => options.add(TextEditingController())),
            ),
          ),
        );
      }
      rows.add(
        ChurrSwitchTile(
          contentPadding: EdgeInsets.zero,
          title: Text(L10n.of(context).pickMoreThanOne),
          value: multiple,
          onChanged: (v) => setState(() => multiple = v),
        ),
      );
    }
    if (command == 'poll') {
      rows.addAll([
        ChurrSwitchTile(
          key: const Key('preset-anonymous'),
          contentPadding: EdgeInsets.zero,
          title: Text(L10n.of(context).anonymous),
          subtitle: Text(L10n.of(context).pollAnonymousNote),
          value: anonymous,
          onChanged: (v) => setState(() => anonymous = v),
        ),
        ChurrSwitchTile(
          contentPadding: EdgeInsets.zero,
          title: Text(L10n.of(context).mayAddOptions),
          value: addOptions,
          onChanged: (v) => setState(() => addOptions = v),
        ),
        Row(
          children: [
            Expanded(child: Text(L10n.of(context).showResults)),
            Flexible(
              child: DropdownButton<String>(
                elevation: 0,
                icon: const Glyph(ChurrIcons.chevronDown, size: 16),
                isExpanded: true,
                value: results,
                items: [
                  DropdownMenuItem(
                    value: 'live',
                    child: Text(L10n.of(context).resultsLive),
                  ),
                  DropdownMenuItem(
                    value: 'after_answer',
                    child: Text(L10n.of(context).resultsAfterAnswer),
                  ),
                  DropdownMenuItem(
                    value: 'after_close',
                    child: Text(L10n.of(context).resultsAfterClose),
                  ),
                ],
                onChanged: (v) => setState(() => results = v ?? 'live'),
              ),
            ),
          ],
        ),
      ]);
    }
    if (command == 'rsvp') {
      rows.add(
        _momentRow(
          L10n.of(context).when,
          when,
          (v) => when = v,
          clearable: false,
        ),
      );
    }
    if (command == 'approve' && _manager) {
      final groups = widget.server.groups.values.toList()
        ..sort((a, b) => a.handle.compareTo(b.handle));
      rows.add(
        Row(
          children: [
            Expanded(child: Text(L10n.of(context).whoMayAnswer)),
            Flexible(
              child: DropdownButton<String?>(
                elevation: 0,
                icon: const Glyph(ChurrIcons.chevronDown, size: 16),
                isExpanded: true,
                value: groupId,
                items: [
                  DropdownMenuItem(
                    value: null,
                    child: Text(L10n.of(context).anyoneHere),
                  ),
                  for (final g in groups)
                    DropdownMenuItem(value: g.id, child: Text('@${g.handle}')),
                ],
                onChanged: (v) => setState(() => groupId = v),
              ),
            ),
          ],
        ),
      );
    }
    if (command == 'form') rows.addAll(_formBuilder());
    if (command != 'rsvp') {
      rows.add(
        _momentRow(L10n.of(context).closingTime, closesAt, (v) => closesAt = v),
      );
    }
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(20, 16, 8, 4),
          child: Row(
            children: [
              Expanded(
                child: Text(
                  heading,
                  style: const TextStyle(
                    fontSize: 17,
                    fontWeight: FontWeight.w800,
                  ),
                ),
              ),
              IconButton(
                tooltip: L10n.of(context).cancel,
                icon: const Glyph(ChurrIcons.close),
                onPressed: () => Navigator.pop(context),
              ),
            ],
          ),
        ),
        Flexible(
          child: ListView(
            shrinkWrap: true,
            padding: const EdgeInsets.fromLTRB(20, 4, 20, 8),
            children: rows,
          ),
        ),
        if (error != null)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 20),
            child: Text(
              error!,
              key: const Key('preset-error'),
              style: TextStyle(color: c.pink),
            ),
          ),
        Padding(
          padding: const EdgeInsets.fromLTRB(12, 8, 12, 12),
          child: Row(
            mainAxisAlignment: MainAxisAlignment.end,
            children: [
              TextButton(
                onPressed: () => Navigator.pop(context),
                child: Text(L10n.of(context).cancel),
              ),
              const SizedBox(width: 8),
              FilledButton(
                key: const Key('preset-post'),
                onPressed: _post,
                child: Text(L10n.of(context).post),
              ),
            ],
          ),
        ),
      ],
    );
  }

  List<Widget> _formBuilder() {
    final c = ChurrColors.of(context);
    final t = L10n.of(context);
    final types = {
      'text': t.fieldOneLine,
      'longtext': t.fieldSeveralLines,
      'choice': t.fieldChoice,
      'number': t.fieldNumber,
      'date': t.fieldDate,
      'time': t.fieldTime,
      'account': t.fieldPeople,
      'conversation': t.fieldChannels,
      'checkbox': t.fieldTickBox,
    };
    return [
      for (var i = 0; i < fields.length; i++)
        Container(
          margin: const EdgeInsets.only(bottom: 10),
          padding: const EdgeInsets.all(10),
          decoration: BoxDecoration(color: c.raised),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Row(
                children: [
                  Text(t.fieldN(i + 1), style: monoStyle(context)),
                  const Spacer(),
                  DropdownButton<String>(
                    elevation: 0,
                    icon: const Glyph(ChurrIcons.chevronDown, size: 16),
                    key: Key('builder-type-$i'),
                    value: fields[i].type,
                    items: [
                      for (final e in types.entries)
                        DropdownMenuItem(value: e.key, child: Text(e.value)),
                    ],
                    onChanged: (v) =>
                        setState(() => fields[i].type = v ?? 'text'),
                  ),
                  IconButton(
                    tooltip: t.removeField,
                    icon: const Glyph(ChurrIcons.delete, size: 18),
                    onPressed: () =>
                        setState(() => fields.removeAt(i).dispose()),
                  ),
                ],
              ),
              TextField(
                key: Key('builder-label-$i'),
                controller: fields[i].label,
                decoration: InputDecoration(labelText: t.fieldLabel),
              ),
              TextField(
                controller: fields[i].help,
                decoration: InputDecoration(labelText: t.helpOptional),
              ),
              if (fields[i].type == 'choice')
                TextField(
                  key: Key('builder-options-$i'),
                  controller: fields[i].options,
                  minLines: 2,
                  maxLines: 6,
                  decoration: InputDecoration(labelText: t.optionsPerLine),
                ),
              Wrap(
                spacing: 12,
                children: [
                  FilterChip(
                    label: Text(t.required),
                    selected: fields[i].required,
                    onSelected: (v) => setState(() => fields[i].required = v),
                  ),
                  if ({
                    'choice',
                    'account',
                    'conversation',
                  }.contains(fields[i].type))
                    FilterChip(
                      label: Text(t.moreThanOne),
                      selected: fields[i].multiple,
                      onSelected: (v) => setState(() => fields[i].multiple = v),
                    ),
                ],
              ),
            ],
          ),
        ),
      if (fields.length < 20)
        Align(
          alignment: Alignment.centerLeft,
          child: TextButton.icon(
            key: const Key('builder-add-field'),
            icon: const Glyph(ChurrIcons.add, size: 16),
            label: Text(t.addField),
            onPressed: () => setState(() => fields.add(_DraftField('text'))),
          ),
        ),
      for (var i = 0; i < buttons.length; i++)
        Row(
          children: [
            Expanded(
              child: TextField(
                controller: buttons[i].label,
                decoration: InputDecoration(labelText: t.buttonN(i + 1)),
              ),
            ),
            DropdownButton<String>(
              elevation: 0,
              icon: const Glyph(ChurrIcons.chevronDown, size: 16),
              value: buttons[i].style,
              items: [
                DropdownMenuItem(value: 'default', child: Text(t.buttonPlain)),
                DropdownMenuItem(value: 'primary', child: Text(t.buttonMain)),
                DropdownMenuItem(value: 'danger', child: Text(t.buttonDanger)),
              ],
              onChanged: (v) =>
                  setState(() => buttons[i].style = v ?? 'default'),
            ),
            IconButton(
              tooltip: t.removeButton,
              icon: const Glyph(ChurrIcons.delete, size: 18),
              onPressed: () =>
                  setState(() => buttons.removeAt(i).label.dispose()),
            ),
          ],
        ),
      if (buttons.length < 5)
        Align(
          alignment: Alignment.centerLeft,
          child: TextButton.icon(
            icon: const Glyph(ChurrIcons.button, size: 16),
            label: Text(t.addButton),
            onPressed: () => setState(() => buttons.add(_DraftButton())),
          ),
        ),
      ChurrSwitchTile(
        contentPadding: EdgeInsets.zero,
        title: Text(t.anonymous),
        subtitle: Text(t.formAnonymousNote),
        value: anonymous,
        onChanged: (v) => setState(() => anonymous = v),
      ),
      Text(t.formAnswersGo, style: monoStyle(context)),
    ];
  }
}
