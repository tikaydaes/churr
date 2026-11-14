import 'package:flutter/material.dart';

import '../../l10n/app_localizations.dart';

import 'package:flutter/services.dart';

import '../../core/models/form.dart';
import '../../core/models/json.dart';
import '../../state/server_state.dart';
import '../../state/sidebar_model.dart';
import '../theme/tokens.dart';
import '../widgets/controls.dart';
import '../theme/churr_icons.dart';

/// What the dialog hands back: the answers and the button pressed, if
/// any. The caller sends them and returns a sentence on refusal, which
/// the dialog shows without closing.
typedef AnswerSubmit = Future<String?> Function(Json answers, String? buttonId);

/// The dialog in which a form's fields are filled (docs/MESSAGE-FORMAT.md
/// section 7): every field type, checked as the server will check it.
/// Used for message forms with more than one single choice, and for a
/// bot's dialog shown to one person.
///
/// With buttons, each button submits; without, one "Send" does.
Future<bool?> showAnswerDialog(
  BuildContext context, {
  required ServerState server,
  required String? title,
  required List<FormFieldDef> fields,
  List<FormButtonDef> buttons = const [],
  Json? initial,
  required AnswerSubmit onSubmit,
  String? cancelLabel,
  String? note,
}) {
  final body = AnswerForm(
    server: server,
    title: title,
    fields: fields,
    buttons: buttons,
    initial: initial,
    onSubmit: onSubmit,
    cancelLabel: cancelLabel,
    note: note,
  );
  if (MediaQuery.sizeOf(context).width < 600) {
    return Navigator.of(context).push<bool>(
      MaterialPageRoute(
        fullscreenDialog: true,
        builder: (_) => Scaffold(
          appBar: AppBar(title: Text(title ?? L10n.of(context).answer)),
          body: body,
        ),
      ),
    );
  }
  return showDialog<bool>(
    animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (_) => Dialog(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 520, maxHeight: 680),
        child: body,
      ),
    ),
  );
}

class AnswerForm extends StatefulWidget {
  const AnswerForm({
    super.key,
    required this.server,
    required this.title,
    required this.fields,
    required this.buttons,
    required this.onSubmit,
    this.initial,
    this.cancelLabel,
    this.note,
  });

  final ServerState server;
  final String? title;
  final List<FormFieldDef> fields;
  final List<FormButtonDef> buttons;
  final Json? initial;
  final AnswerSubmit onSubmit;

  /// Null for "Cancel".
  final String? cancelLabel;
  final String? note;

  @override
  State<AnswerForm> createState() => _AnswerFormState();
}

class _AnswerFormState extends State<AnswerForm> {
  late final Json values = {...?widget.initial}..remove('_button');
  final Map<String, String> errors = {};
  final Map<String, TextEditingController> texts = {};
  String? failure;
  bool sending = false;

  @override
  void initState() {
    super.initState();
    for (final f in widget.fields) {
      if (f.type == 'text' || f.type == 'longtext' || f.type == 'number') {
        texts[f.id] = TextEditingController(
          text: values[f.id] == null ? '' : '${values[f.id]}',
        );
      }
    }
  }

  @override
  void dispose() {
    for (final t in texts.values) {
      t.dispose();
    }
    super.dispose();
  }

  Json _collect() {
    final out = <String, dynamic>{};
    for (final f in widget.fields) {
      Object? v = values[f.id];
      if (texts.containsKey(f.id)) {
        final s = texts[f.id]!.text;
        v = f.type == 'number'
            ? (s.trim().isEmpty ? null : num.tryParse(s.trim()) ?? s)
            : s;
        if (v is String && v.isEmpty) v = null;
      }
      if (f.type == 'checkbox') v = v == true;
      if (v != null && !(v is List && v.isEmpty)) out[f.id] = v;
    }
    return out;
  }

  Future<void> _submit(String? buttonId) async {
    final answers = _collect();
    errors.clear();
    for (final f in widget.fields) {
      final e = checkValue(f, answers[f.id]);
      if (e != null) errors[f.id] = e;
    }
    if (errors.isNotEmpty) {
      setState(() {});
      return;
    }
    setState(() {
      sending = true;
      failure = null;
    });
    final err = await widget.onSubmit(answers, buttonId);
    if (!mounted) return;
    if (err == null) {
      Navigator.of(context).pop(true);
    } else {
      setState(() {
        sending = false;
        failure = err;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final inDialog = ModalRoute.of(context) is DialogRoute;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (inDialog && widget.title != null)
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 18, 20, 4),
            child: Text(
              widget.title!,
              style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w800),
            ),
          ),
        if (widget.note != null)
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 4, 20, 4),
            child: Text(
              widget.note!,
              style: TextStyle(fontSize: 13, color: c.dim),
            ),
          ),
        Flexible(
          child: ListView(
            shrinkWrap: true,
            padding: const EdgeInsets.fromLTRB(20, 8, 20, 8),
            children: [for (final f in widget.fields) _field(f)],
          ),
        ),
        if (failure != null)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 20),
            child: Text(failure!, style: TextStyle(color: c.pink)),
          ),
        Padding(
          padding: const EdgeInsets.fromLTRB(12, 8, 12, 12),
          child: Wrap(
            alignment: WrapAlignment.end,
            spacing: 8,
            runSpacing: 8,
            children: [
              TextButton(
                onPressed: sending
                    ? null
                    : () => Navigator.of(context).pop(false),
                child: Text(widget.cancelLabel ?? L10n.of(context).cancel),
              ),
              if (widget.buttons.isEmpty)
                FilledButton(
                  key: const Key('answer-send'),
                  onPressed: sending ? null : () => _submit(null),
                  child: Text(L10n.of(context).send),
                )
              else
                for (final b in widget.buttons)
                  formButton(
                    context,
                    b,
                    sending ? null : () => _submit(b.id),
                    key: Key('answer-button-${b.id}'),
                  ),
            ],
          ),
        ),
      ],
    );
  }

  Widget _field(FormFieldDef f) {
    final c = ChurrColors.of(context);
    final err = errors[f.id];
    Widget input;
    switch (f.type) {
      case 'choice':
        input = _choice(f);
      case 'text' || 'longtext' || 'number':
        input = TextField(
          key: Key('field-${f.id}'),
          controller: texts[f.id],
          minLines: f.type == 'longtext' ? 3 : 1,
          maxLines: f.type == 'longtext' ? 8 : 1,
          maxLength: f.type == 'number'
              ? null
              : (f.maxLength ?? (f.type == 'text' ? 500 : 4000)),
          keyboardType: f.type == 'number'
              ? const TextInputType.numberWithOptions(
                  decimal: true,
                  signed: true,
                )
              : f.type == 'longtext'
              ? TextInputType.multiline
              : TextInputType.text,
          inputFormatters: f.type == 'text'
              ? [FilteringTextInputFormatter.deny(RegExp('\n'))]
              : null,
          decoration: InputDecoration(hintText: f.placeholder, errorText: err),
        );
      case 'date':
        final v = values[f.id] as String?;
        input = OutlinedButton.icon(
          key: Key('field-${f.id}'),
          icon: const Glyph(ChurrIcons.calendar, size: 16),
          label: Text(v ?? L10n.of(context).pickADate),
          onPressed: () async {
            final now = DateTime.now();
            DateTime? parse(Object? s) =>
                s is String ? DateTime.tryParse(s) : null;
            final first = parse(f.min) ?? DateTime(now.year - 100);
            final last = parse(f.max) ?? DateTime(now.year + 100);
            var init = parse(v) ?? now;
            if (init.isBefore(first)) init = first;
            if (init.isAfter(last)) init = last;
            final d = await showDatePicker(
              context: context,
              firstDate: first,
              lastDate: last,
              initialDate: init,
            );
            if (d != null) {
              setState(
                () => values[f.id] =
                    '${d.year.toString().padLeft(4, '0')}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}',
              );
            }
          },
        );
      case 'time':
        final v = values[f.id] as String?;
        input = OutlinedButton.icon(
          key: Key('field-${f.id}'),
          icon: const Glyph(ChurrIcons.clock, size: 16),
          label: Text(v ?? L10n.of(context).pickATime),
          onPressed: () async {
            final parts = v?.split(':');
            final t = await showTimePicker(
              context: context,
              initialTime: parts == null
                  ? TimeOfDay.now()
                  : TimeOfDay(
                      hour: int.parse(parts[0]),
                      minute: int.parse(parts[1]),
                    ),
            );
            if (t != null) {
              setState(
                () => values[f.id] =
                    '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}',
              );
            }
          },
        );
      case 'account' || 'conversation':
        input = _picker(f);
      case 'checkbox':
        input = CheckboxListTile(
          key: Key('field-${f.id}'),
          contentPadding: EdgeInsets.zero,
          controlAffinity: ListTileControlAffinity.leading,
          value: values[f.id] == true,
          title: Text(f.label),
          onChanged: (v) => setState(() => values[f.id] = v == true),
        );
      default:
        input = const SizedBox.shrink();
    }
    return Padding(
      padding: const EdgeInsets.only(bottom: 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (f.type != 'checkbox')
            Text.rich(
              TextSpan(
                children: [
                  TextSpan(text: f.label),
                  if (f.required)
                    TextSpan(
                      text: ' *',
                      style: TextStyle(color: c.pink),
                    ),
                ],
              ),
              style: const TextStyle(fontWeight: FontWeight.w700),
            ),
          if (f.help != null)
            Padding(
              padding: const EdgeInsets.only(top: 2),
              child: Text(
                f.help!,
                style: TextStyle(fontSize: 12, color: c.dim),
              ),
            ),
          const SizedBox(height: 6),
          input,
          if (err != null && !texts.containsKey(f.id))
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text(err, style: TextStyle(color: c.pink, fontSize: 12)),
            ),
        ],
      ),
    );
  }

  Widget _choice(FormFieldDef f) {
    final chosen = strings(values[f.id]);
    void set(List<String> ids) => setState(() => values[f.id] = ids);
    if (f.multiple) {
      return Column(
        children: [
          for (final o in f.options)
            CheckboxListTile(
              key: Key('option-${f.id}-${o.id}'),
              dense: true,
              contentPadding: EdgeInsets.zero,
              controlAffinity: ListTileControlAffinity.leading,
              value: chosen.contains(o.id),
              title: Text(o.label),
              onChanged: (v) => set([
                for (final x in chosen)
                  if (x != o.id) x,
                if (v == true) o.id,
              ]),
            ),
        ],
      );
    }
    if (f.options.length > 5) {
      return DropdownButtonFormField<String>(
        elevation: 0,
        icon: const Glyph(ChurrIcons.chevronDown, size: 16),
        key: Key('field-${f.id}'),
        initialValue: chosen.isEmpty ? null : chosen.first,
        isExpanded: true,
        hint: Text(L10n.of(context).choose),
        items: [
          for (final o in f.options)
            DropdownMenuItem(value: o.id, child: Text(o.label)),
        ],
        onChanged: (v) => set(v == null ? [] : [v]),
      );
    }
    return Column(
      children: [
        for (final o in f.options)
          ChoiceTile(
            key: Key('option-${f.id}-${o.id}'),
            contentPadding: EdgeInsets.zero,
            selected: chosen.isNotEmpty && chosen.first == o.id,
            onTap: () => set([o.id]),
            title: Text(o.label),
          ),
      ],
    );
  }

  /// People or channels: chips of those picked, and a searchable list.
  Widget _picker(FormFieldDef f) {
    final s = widget.server;
    final people = f.type == 'account';
    final all = people
        ? {
            for (final a in s.accounts.values)
              if (a.removedAt == null) a.id: a.name,
          }
        : {
            for (final r in s.conversations)
              if (!r.conversation.isSealed)
                r.conversation.id: conversationName(
                  r.conversation,
                  s.accounts,
                  s.me?.id,
                ),
          };
    final chosen = strings(values[f.id]);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Wrap(
          spacing: 6,
          runSpacing: 6,
          children: [
            for (final id in chosen)
              InputChip(
                label: Text(
                  all[id] ??
                      (people
                          ? L10n.of(context).someone
                          : L10n.of(context).aChannel),
                ),
                onDeleted: () => setState(
                  () => values[f.id] = [
                    for (final x in chosen)
                      if (x != id) x,
                  ],
                ),
              ),
          ],
        ),
        Autocomplete<MapEntry<String, String>>(
          key: Key('field-${f.id}'),
          displayStringForOption: (e) => e.value,
          optionsBuilder: (v) {
            final q = v.text.toLowerCase();
            if (q.isEmpty) return const [];
            return all.entries
                .where(
                  (e) =>
                      e.value.toLowerCase().contains(q) &&
                      !chosen.contains(e.key),
                )
                .take(8);
          },
          onSelected: (e) => setState(
            () => values[f.id] = f.multiple ? [...chosen, e.key] : [e.key],
          ),
          fieldViewBuilder: (context, controller, focus, submit) => TextField(
            controller: controller,
            focusNode: focus,
            decoration: InputDecoration(
              hintText: people
                  ? L10n.of(context).typeAName
                  : L10n.of(context).typeAChannelName,
            ),
            onSubmitted: (_) => submit(),
          ),
        ),
      ],
    );
  }
}

/// A form button in its style: `primary` filled, `danger` in red,
/// `default` outlined.
Widget formButton(
  BuildContext context,
  FormButtonDef b,
  VoidCallback? onPressed, {
  Key? key,
  bool chosen = false,
}) {
  final c = ChurrColors.of(context);
  final label = Row(
    mainAxisSize: MainAxisSize.min,
    children: [
      if (chosen)
        const Padding(
          padding: EdgeInsets.only(right: 6),
          child: Glyph(ChurrIcons.check, size: 14),
        ),
      Text(b.label),
    ],
  );
  return switch (b.style) {
    'primary' => FilledButton(key: key, onPressed: onPressed, child: label),
    'danger' => OutlinedButton(
      key: key,
      onPressed: onPressed,
      style: OutlinedButton.styleFrom(
        foregroundColor: c.pink,
        side: BorderSide(
          color: chosen ? c.pink : c.line,
          width: chosen ? 2 : 1.5,
        ),
      ),
      child: label,
    ),
    _ => OutlinedButton(
      key: key,
      onPressed: onPressed,
      style: chosen
          ? OutlinedButton.styleFrom(
              side: BorderSide(color: c.lavender, width: 2),
            )
          : null,
      child: label,
    ),
  };
}
