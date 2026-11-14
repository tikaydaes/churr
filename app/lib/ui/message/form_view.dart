import 'dart:io';

import 'package:flutter/material.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

import '../../core/api/errors.dart';
import '../../core/api/forms.dart';
import '../../core/models/form.dart';
import '../../core/models/json.dart';
import '../../core/models/message.dart';
import '../../core/themes/palette.dart' show Rgb, ratio, textRatio;
import '../../state/app_state.dart';
import '../../state/form_actions.dart';
import '../../state/server_state.dart';
import '../forms/answer_dialog.dart';
import '../scopes.dart';
import '../theme/tokens.dart';
import '../widgets/controls.dart';
import '../theme/churr_icons.dart';
import '../widgets/avatar.dart';
import 'checklist.dart' show shortWhen;

/// A message's form, drawn from its object (docs/MESSAGE-FORMAT.md
/// section 7, docs/FEATURES.md 7):
///
/// - a single choice with up to five options as rows in the message,
///   with more as a menu; every other form opens an answer dialog;
/// - buttons in their styles;
/// - the tally as bars with counts, and voters' faces where the server
///   sent `voters`: exactly what came, since the server applies
///   `results` and `anonymous` for each reader;
/// - changing, withdrawing, closing, the results list and CSV for those
///   allowed;
/// - for a form with an unknown field type, action or preset, the
///   fallback text and a line saying this app is too old.
class FormView extends StatelessWidget {
  const FormView({
    super.key,
    required this.server,
    required this.message,
    this.now,
  });

  final ServerState server;
  final Message message;

  /// For tests.
  final DateTime Function()? now;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final raw = message.form;
    if (raw == null) return const SizedBox.shrink();
    final form = FormModel.fromJson(raw);
    if (!form.drawable) {
      return _Frame(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (message.fallback.isNotEmpty) Text(message.fallback),
            Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Text(
                'This app is too old to answer this form. Update Churr to take part.',
                key: const Key('form-too-old'),
                style: monoStyle(context, color: c.yellowInk),
              ),
            ),
          ],
        ),
      );
    }
    return _FormBody(
      server: server,
      message: message,
      form: form,
      now: (now ?? DateTime.now)(),
    );
  }
}

class _Frame extends StatelessWidget {
  const _Frame({required this.child});
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Container(
      margin: const EdgeInsets.only(top: 6),
      constraints: const BoxConstraints(maxWidth: 420),
      // A panel on the ground of the log: a fill step, no frame.
      color: c.panel,
      padding: const EdgeInsets.all(14),
      child: child,
    );
  }
}

class _FormBody extends StatefulWidget {
  const _FormBody({
    required this.server,
    required this.message,
    required this.form,
    required this.now,
  });
  final ServerState server;
  final Message message;
  final FormModel form;
  final DateTime now;

  @override
  State<_FormBody> createState() => _FormBodyState();
}

class _FormBodyState extends State<_FormBody> {
  bool busy = false;

  ServerState get server => widget.server;
  Message get m => widget.message;
  FormModel get form => widget.form;
  FormActions get actions => FormActions(server);

  bool get closed => form.isClosed(widget.now);

  /// Why the reader cannot answer, or null. Anyone in the conversation
  /// may answer, readers included; not someone reading a public channel
  /// without joining, bots, or anyone once it is closed.
  String? get blocked {
    final conv = server.conversation(m.conversationId);
    if (closed) return null;
    if (m.isDeleted) return 'This message was deleted.';
    if (conv?.membership == null) return 'Join to take part.';
    if (conv?.isArchived ?? false) return 'This channel is archived.';
    if (server.me?.isBot ?? false) return 'Bots cannot answer forms.';
    if (form.answererGroupId != null &&
        !(server.groups[form.answererGroupId]?.isMember ?? false)) {
      final handle = server.groups[form.answererGroupId]?.handle;
      return handle == null
          ? 'Only some people may answer.'
          : 'Only @$handle may answer.';
    }
    return null;
  }

  /// Answers to `thread` and `channel` forms cannot be changed, and with
  /// one answer per account a second is refused.
  bool get answeredForGood =>
      !form.changeable &&
      form.action != 'bot' &&
      form.oneAnswer &&
      form.hasAnswer;

  bool get canAct => !closed && blocked == null && !busy && !answeredForGood;

  void _say(String text) => ScaffoldMessenger.maybeOf(context)
    ?..hideCurrentSnackBar()
    ..showSnackBar(SnackBar(content: Text(text)));

  String _why(Object e) {
    if (e is ApiException) {
      return switch (e.code) {
        'conflict' => 'This form is closed, or that is no longer possible.',
        'already_exists' => 'You have answered already.',
        'timed_out' => 'You are timed out here.',
        'unavailable' => 'The bot is not connected. Try again later.',
        _ => e.message,
      };
    }
    return '$e';
  }

  Future<String?> _run(Future<FormReply> Function() f) async {
    setState(() => busy = true);
    try {
      final reply = await f();
      if (reply.conversationId != null && mounted) {
        // A ticket: the new channel opens.
        AppScope.read(
          context,
        ).openConversation(OpenTarget(server.localId, reply.conversationId!));
      }
      return null;
    } on Exception catch (e) {
      return _why(e);
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> _quick(Future<FormReply> Function() f) async {
    final err = await _run(f);
    if (err != null) _say(err);
  }

  void _choose(FormFieldDef f, String optionId) {
    if (!canAct) return;
    final mine = form.mine(f.id);
    if (mine.length == 1 && mine.first == optionId) return;
    _quick(
      () => actions.answer(m, {
        f.id: [optionId],
      }),
    );
  }

  Future<void> _openDialog() async {
    await showAnswerDialog(
      context,
      server: server,
      title: form.title,
      fields: form.fields,
      buttons: form.buttons,
      initial: form.myAnswer,
      onSubmit: (answers, button) => _run(
        () => button == null
            ? actions.answer(m, answers)
            : actions.press(m, button, answers: answers),
      ),
    );
  }

  Future<void> _addOption(FormFieldDef f) async {
    final text = TextEditingController();
    final label = await showDialog<String>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Add an option'),
        content: TextField(
          key: const Key('new-option'),
          controller: text,
          autofocus: true,
          maxLength: 100,
          onSubmitted: (v) => Navigator.pop(ctx, v.trim()),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, text.text.trim()),
            child: const Text('Add'),
          ),
        ],
      ),
    );
    if (label == null || label.isEmpty) return;
    if (f.options.any((o) => o.label.toLowerCase() == label.toLowerCase())) {
      _say('That option is there already.');
      return;
    }
    await _quick(() => actions.addOption(m, f.id, label));
  }

  Future<void> _close() async {
    final ok = await showDialog<bool>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Close this form?'),
        content: const Text(
          'Closing is final. The result stays on the message and no more answers are taken.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            key: const Key('confirm-close'),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Close it'),
          ),
        ],
      ),
    );
    if (ok == true) await _quick(() => actions.close(m));
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final inline = form.inlineField;
    final children = <Widget>[];
    if ((form.title ?? '').isNotEmpty) {
      children.add(
        Text(form.title!, style: const TextStyle(fontWeight: FontWeight.w800)),
      );
    }
    if (form.preset == 'rsvp' && form.closesAt != null) {
      children.add(
        Padding(
          padding: const EdgeInsets.only(top: 2),
          child: Row(
            children: [
              Glyph(ChurrIcons.calendar, size: 14, color: c.lavender),
              const SizedBox(width: 6),
              Text(
                shortWhen(form.closesAt!),
                style: monoStyle(context, color: c.ink),
              ),
            ],
          ),
        ),
      );
    }
    if (inline != null) {
      children.add(_inlineChoice(inline));
    } else {
      // Tallies of every choice field the server counted, read only.
      for (final f in form.fields) {
        if (f.isChoice && form.tally?[f.id] != null) {
          children.add(_bars(f, interactive: false));
        }
      }
    }
    if (inline != null &&
        form.allowAddedOptions &&
        canAct &&
        inline.options.length < 50) {
      children.add(
        Align(
          alignment: Alignment.centerLeft,
          child: TextButton.icon(
            key: const Key('form-add-option'),
            icon: const Glyph(ChurrIcons.add, size: 16),
            label: const Text('Add an option'),
            onPressed: () => _addOption(inline),
          ),
        ),
      );
    }
    final needsDialog = form.fields.isNotEmpty && inline == null;
    if (needsDialog) {
      children.add(
        Padding(
          padding: const EdgeInsets.only(top: 8),
          child: Align(
            alignment: Alignment.centerLeft,
            child: FilledButton(
              key: const Key('form-answer'),
              onPressed: canAct ? _openDialog : null,
              child: Text(
                form.hasAnswer && form.changeable ? 'Change answer' : 'Answer',
              ),
            ),
          ),
        ),
      );
    } else if (form.buttons.isNotEmpty) {
      children.add(_buttons());
    }
    children.add(_footer());
    return Semantics(
      container: true,
      label: 'Form${form.title == null ? '' : ': ${form.title}'}',
      child: _Frame(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: children,
        ),
      ),
    );
  }

  Widget _inlineChoice(FormFieldDef f) {
    if (f.options.length <= 5) return _bars(f, interactive: true);
    // More than five: a menu, and the tally under it when sent.
    final mine = form.mine(f.id);
    String? label;
    for (final o in f.options) {
      if (mine.contains(o.id)) label = o.label;
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.only(top: 6),
          child: Align(
            alignment: Alignment.centerLeft,
            child: PopupMenuButton<String>(
              popUpAnimationStyle: AnimationStyle.noAnimation,
              key: const Key('form-menu'),
              enabled: canAct,
              tooltip: f.label,
              onSelected: (id) => _choose(f, id),
              itemBuilder: (_) => [
                for (final o in f.options)
                  checkItem(
                    value: o.id,
                    checked: mine.contains(o.id),
                    enabled: !_full(f, o) || mine.contains(o.id),
                    child: Text(o.label),
                  ),
              ],
              child: Container(
                padding: const EdgeInsets.symmetric(
                  horizontal: 10,
                  vertical: 8,
                ),
                decoration: BoxDecoration(
                  border: Border.all(
                    color: ChurrColors.of(context).line,
                    width: 1.5,
                  ),
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(label ?? 'Choose…'),
                    const SizedBox(width: 6),
                    const Glyph(ChurrIcons.chevronDown, size: 18),
                  ],
                ),
              ),
            ),
          ),
        ),
        if (form.tally?[f.id] != null) _bars(f, interactive: false),
      ],
    );
  }

  bool _full(FormFieldDef f, FormOption o) =>
      o.max != null &&
      form.tally?[f.id] != null &&
      form.count(f.id, o.id) >= o.max!;

  /// Option rows on raised. The reader's own choice is marked by a tick,
  /// a cyan bar at its left edge, bold text and the words "your vote",
  /// never by colour alone. A tally bar sits behind
  /// each when the tally was sent, its count, and voters' faces when
  /// `voters` was sent. Places left for a sign-up slot.
  Widget _bars(FormFieldDef f, {required bool interactive}) {
    final c = ChurrColors.of(context);
    final mine = form.mine(f.id);
    final total = form.fieldTotal(f.id);
    final shown = form.tally?[f.id] != null;
    final multipleInline = interactive && f.multiple;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        for (final o in f.options)
          Builder(
            builder: (context) {
              final n = form.count(f.id, o.id);
              final chosen = mine.contains(o.id);
              final full = _full(f, o) && !chosen;
              final voters = form.voters?[f.id]?[o.id] ?? const <String>[];
              final places = o.max == null
                  ? null
                  : shown
                  ? '$n/${o.max}'
                  : '${o.max} places';
              final row = Stack(
                children: [
                  Positioned.fill(child: ColoredBox(color: c.raised)),
                  if (shown && total > 0)
                    Positioned.fill(
                      child: FractionallySizedBox(
                        alignment: Alignment.centerLeft,
                        widthFactor: (n / total).clamp(0, 1).toDouble(),
                        child: ColoredBox(color: pollBar(c, mine: chosen)),
                      ),
                    ),
                  Container(
                    decoration: BoxDecoration(
                      border: chosen
                          ? Border(left: BorderSide(color: c.cyan, width: 4))
                          : null,
                    ),
                    padding: EdgeInsets.fromLTRB(chosen ? 6 : 10, 8, 10, 8),
                    child: Row(
                      children: [
                        if (chosen)
                          Padding(
                            padding: const EdgeInsets.only(right: 6),
                            child: Glyph(
                              ChurrIcons.check,
                              size: 15,
                              color: c.ink,
                              semanticLabel: 'your vote',
                            ),
                          )
                        else if (interactive && !multipleInline)
                          Padding(
                            padding: const EdgeInsets.only(right: 8),
                            child: Glyph(
                              ChurrIcons.box,
                              size: 15,
                              color: c.dim,
                            ),
                          ),
                        Flexible(
                          child: Text(
                            o.label,
                            // Over the tally bars text is always ink; a full
                            // option is struck through and says "full".
                            style: TextStyle(
                              fontSize: 14,
                              color: c.ink,
                              decoration: full
                                  ? TextDecoration.lineThrough
                                  : null,
                              fontWeight: chosen
                                  ? FontWeight.w700
                                  : FontWeight.w500,
                            ),
                          ),
                        ),
                        if (chosen) ...[
                          const SizedBox(width: 8),
                          Text(
                            'your vote',
                            style: monoStyle(context, size: 10, color: c.ink),
                          ),
                        ],
                        const Spacer(),
                        for (final id in voters.take(5))
                          Padding(
                            padding: const EdgeInsets.only(left: 2),
                            child: Avatar(
                              account: server.accounts[id],
                              server: server,
                              size: 18,
                            ),
                          ),
                        if (voters.length > 5)
                          Padding(
                            padding: const EdgeInsets.only(left: 4),
                            child: Text(
                              '+${voters.length - 5}',
                              style: monoStyle(context, color: c.ink),
                            ),
                          ),
                        if (places != null) ...[
                          const SizedBox(width: 8),
                          Text(
                            full ? '$places full' : places,
                            style: monoStyle(
                              context,
                              color: c.ink,
                              weight: full ? FontWeight.w700 : FontWeight.w400,
                            ),
                          ),
                        ] else if (shown) ...[
                          const SizedBox(width: 8),
                          Text(
                            '$n',
                            style: monoStyle(
                              context,
                              color: c.ink,
                              weight: FontWeight.w600,
                            ),
                          ),
                        ],
                      ],
                    ),
                  ),
                ],
              );
              final names = [
                for (final id in voters) server.accounts[id]?.name ?? 'someone',
              ];
              return Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Semantics(
                  button: interactive,
                  selected: chosen,
                  label: [
                    o.label,
                    if (chosen) 'your vote',
                    if (shown) '$n ${n == 1 ? 'vote' : 'votes'}',
                    if (o.max != null) '${o.max} places',
                    if (full) 'full',
                    if (names.isNotEmpty) names.join(', '),
                  ].join(', '),
                  excludeSemantics: true,
                  child: InkWell(
                    key: Key('form-option-${o.id}'),
                    onTap: interactive && canAct && !full
                        ? () => _choose(f, o.id)
                        : null,
                    child: row,
                  ),
                ),
              );
            },
          ),
      ],
    );
  }

  Widget _buttons() {
    final c = ChurrColors.of(context);
    final counts = form.tally?['_button'];
    final voters = form.voters?['_button'];
    final mine = form.myButton;
    return Padding(
      padding: const EdgeInsets.only(top: 8),
      child: Wrap(
        spacing: 8,
        runSpacing: 8,
        crossAxisAlignment: WrapCrossAlignment.center,
        children: [
          for (final b in form.buttons)
            Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                formButton(
                  context,
                  b,
                  canAct && mine != b.id
                      ? () => _quick(() => actions.press(m, b.id))
                      : null,
                  key: Key('form-button-${b.id}'),
                  chosen: mine == b.id,
                ),
                if (counts != null) ...[
                  const SizedBox(width: 6),
                  Text(
                    '${counts[b.id] ?? 0}',
                    style: monoStyle(context, color: c.ink),
                  ),
                ],
                for (final id in (voters?[b.id] ?? const <String>[]).take(5))
                  Padding(
                    padding: const EdgeInsets.only(left: 2),
                    child: Avatar(
                      account: server.accounts[id],
                      server: server,
                      size: 18,
                    ),
                  ),
              ],
            ),
        ],
      ),
    );
  }

  Widget _footer() {
    final c = ChurrColors.of(context);
    final n = form.answerCount;
    final facts = <String>[
      '$n ${n == 1 ? 'answer' : 'answers'}',
      if (form.anonymous) 'anonymous',
      if (form.action == 'tally' && form.tally == null)
        form.results == 'after_close'
            ? 'results when it closes'
            : form.results == 'after_answer'
            ? 'results after you answer'
            : 'results hidden',
      if (form.action == 'collect') 'answers go to the author',
      if (closed)
        form.closedAt != null ? 'closed ${shortWhen(form.closedAt!)}' : 'closed'
      else if (form.closesAt != null && form.preset != 'rsvp')
        'closes ${shortWhen(form.closesAt!)}',
      if (answeredForGood) 'you answered',
    ];
    final why = blocked;
    final canWithdraw =
        !closed && form.changeable && form.hasAnswer && why == null && !busy;
    final canClose = !closed && actions.canClose(m) && !m.isDeleted;
    final canResults = actions.canReadResults(m);
    return Padding(
      padding: const EdgeInsets.only(top: 6),
      child: Wrap(
        crossAxisAlignment: WrapCrossAlignment.center,
        spacing: 4,
        children: [
          Text(
            facts.join(' · '),
            key: const Key('form-facts'),
            style: monoStyle(context),
          ),
          if (why != null)
            Text('· $why', style: monoStyle(context, color: c.yellowInk)),
          if (canWithdraw)
            TextButton(
              key: const Key('form-withdraw'),
              onPressed: () => _quick(() => actions.withdraw(m)),
              child: const Text('Withdraw'),
            ),
          if (canResults)
            TextButton(
              key: const Key('form-results'),
              onPressed: () => showFormResults(context, server, m),
              child: const Text('Results'),
            ),
          if (canClose)
            TextButton(
              key: const Key('form-close'),
              onPressed: _close,
              child: const Text('Close'),
            ),
          if (busy) const Busy(size: 12),
        ],
      ),
    );
  }
}

/// The answers list and the CSV download, for those allowed: the
/// author and the channel's managers for `collect`, anyone shown the
/// tally of a named poll.
Future<void> showFormResults(
  BuildContext context,
  ServerState server,
  Message m,
) {
  final body = _Results(server: server, message: m);
  if (MediaQuery.sizeOf(context).width < 600) {
    return Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => Scaffold(
          appBar: AppBar(title: const Text('Results')),
          body: body,
        ),
      ),
    );
  }
  return showDialog<void>(
    animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (_) =>
        Dialog(child: SizedBox(width: 560, height: 560, child: body)),
  );
}

class _Results extends StatefulWidget {
  const _Results({required this.server, required this.message});
  final ServerState server;
  final Message message;

  @override
  State<_Results> createState() => _ResultsState();
}

class _ResultsState extends State<_Results> {
  List<FormAnswerRow>? rows;
  String? error;
  String? saved;

  FormModel get form => FormModel.fromJson(widget.message.form!);

  @override
  void initState() {
    super.initState();
    FormActions(widget.server)
        .results(widget.message)
        .then((v) {
          if (mounted) setState(() => rows = v);
        })
        .catchError((Object e) {
          if (mounted) {
            setState(() => error = 'Could not load the answers. ($e)');
          }
        });
  }

  String _value(FormFieldDef f, Object? v) {
    if (v == null) return '—';
    switch (f.type) {
      case 'choice':
        final ids = strings(v);
        return [
          for (final id in ids)
            f.options
                    .where((o) => o.id == id)
                    .map((o) => o.label)
                    .firstOrNull ??
                id,
        ].join(', ');
      case 'account':
        return [
          for (final id in strings(v))
            widget.server.accounts[id]?.name ?? 'someone',
        ].join(', ');
      case 'conversation':
        return [
          for (final id in strings(v))
            widget.server.conversation(id)?.name ?? 'a channel',
        ].join(', ');
      case 'checkbox':
        return v == true ? 'Yes' : 'No';
    }
    return '$v';
  }

  /// Saves the CSV to the Downloads folder (or the app's documents
  /// folder where there is none) and says where.
  Future<void> _download() async {
    try {
      final bytes = await FormActions(widget.server).csv(widget.message);
      Directory? dir;
      try {
        dir = await getDownloadsDirectory();
      } on Object {
        dir = null;
      }
      dir ??= await getApplicationDocumentsDirectory();
      final name =
          '${(form.title ?? 'form').replaceAll(RegExp(r'[^\w\- ]'), '').trim()}-answers.csv';
      final file = File(
        p.join(dir.path, name.isEmpty ? 'form-answers.csv' : name),
      );
      await file.writeAsBytes(bytes);
      if (mounted) setState(() => saved = 'Saved to ${file.path}');
    } on Exception catch (e) {
      if (mounted) setState(() => saved = 'Could not download. ($e)');
    }
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final f = form;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 8, 4),
          child: Row(
            children: [
              Expanded(
                child: Text(
                  f.title ?? 'Answers',
                  style: const TextStyle(
                    fontSize: 16,
                    fontWeight: FontWeight.w800,
                  ),
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              TextButton.icon(
                key: const Key('form-csv'),
                icon: const Glyph(ChurrIcons.download, size: 16),
                label: const Text('CSV'),
                onPressed: _download,
              ),
              if (ModalRoute.of(context) is DialogRoute)
                IconButton(
                  tooltip: 'Close',
                  icon: const Glyph(ChurrIcons.close),
                  onPressed: () => Navigator.pop(context),
                ),
            ],
          ),
        ),
        if (saved != null)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Text(
              saved!,
              style: TextStyle(
                fontSize: 13,
                color: ChurrColors.of(context).dim,
              ),
            ),
          ),
        if (f.anonymous)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Text(
              'Anonymous: nobody can see who answered what.',
              style: monoStyle(context, color: c.dim),
            ),
          ),
        Divider(height: 12, thickness: 1.5, color: c.line),
        Expanded(
          child: error != null
              ? Center(
                  child: Text(error!, style: TextStyle(color: c.pink)),
                )
              : rows == null
              ? const Center(child: Busy())
              : rows!.isEmpty
              ? Center(
                  child: Text(
                    'No answers yet.',
                    style: TextStyle(color: c.dim),
                  ),
                )
              : ListView.separated(
                  itemCount: rows!.length,
                  separatorBuilder: (_, _) => const SizedBox(height: 2),
                  itemBuilder: (context, i) {
                    final r = rows![i];
                    final who = r.accountId == null
                        ? 'Anonymous'
                        : widget.server.accounts[r.accountId]?.name ??
                              'Someone';
                    final button = f.buttons
                        .where((b) => b.id == r.buttonId)
                        .map((b) => b.label)
                        .firstOrNull;
                    return Padding(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 16,
                        vertical: 8,
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Row(
                            children: [
                              Expanded(
                                child: Text(
                                  who,
                                  style: const TextStyle(
                                    fontWeight: FontWeight.w700,
                                  ),
                                ),
                              ),
                              if (r.updatedAt ?? r.createdAt case final t?)
                                Text(shortWhen(t), style: monoStyle(context)),
                            ],
                          ),
                          if (button != null)
                            Text(button, style: TextStyle(color: c.dim)),
                          for (final field in f.fields)
                            Text.rich(
                              TextSpan(
                                children: [
                                  TextSpan(
                                    text: '${field.label}: ',
                                    style: TextStyle(color: c.dim),
                                  ),
                                  TextSpan(
                                    text: _value(field, r.answers[field.id]),
                                  ),
                                ],
                              ),
                            ),
                        ],
                      ),
                    );
                  },
                ),
        ),
      ],
    );
  }
}

/// The tally bar behind a poll option: lavender at 28% for others, cyan
/// for the reader's own (the guide's `.opt i`). Text on it stays ink.
/// With a custom theme the tint is lightened, in steps of 2%, until ink
/// on it reaches 4.5:1; ink on the raised step itself always does
/// (docs/STYLE.md "Themes"). The built-in themes keep 28%.
Color pollBar(ChurrColors c, {required bool mine}) {
  final tint = mine ? c.cyan : c.lavender;
  Rgb rgb(Color x) =>
      Rgb((x.r * 255).round(), (x.g * 255).round(), (x.b * 255).round());
  for (var a = 28; a > 0; a -= 2) {
    final bar = Color.alphaBlend(tint.withValues(alpha: a / 100), c.raised);
    if (ratio(rgb(c.ink), rgb(bar)) >= textRatio) return bar;
  }
  return c.raised;
}
