import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:intl/intl.dart';

import '../../../core/conversation/large.dart';
import '../../../core/commands/commands.dart';
import '../../../core/conversation/posting_gate.dart';
import '../../../state/posting_gate_controller.dart';
import '../../../core/conversation/composer_text.dart';
import '../../../core/conversation/emoji_data.dart';
import '../../../core/conversation/threads.dart';
import '../../../core/db/conversation_queries.dart';
import '../../../core/instant/format.dart';
import '../../../core/instant/phrases.dart';
import '../../../core/instant/zone.dart';
import '../../../core/models/conversation.dart';
import '../../../core/models/message.dart';
import '../../../l10n/app_localizations.dart';
import '../../../platform/media_files.dart';
import '../../../state/channel_actions.dart';
import '../../../state/chip_text_controller.dart';
import '../../../state/composer_controller.dart';
import '../../../state/server_state.dart';
import '../../layout/breakpoints.dart';
import '../../message/instant_view.dart';
import '../../message/markdown_view.dart';
import '../../theme/tokens.dart';
import '../../theme/shapes.dart';
import '../../widgets/controls.dart';
import '../../theme/churr_icons.dart';
import '../../widgets/avatar.dart';
import '../channels/subchannel_offer.dart';
import 'chat_commands.dart';
import 'command_handlers.dart';
import 'pickers.dart';
import 'schedule_sheet.dart';

/// The composer (docs/CLIENTS.md 9.5): a growing box that takes Churr
/// Markdown, with a formatting bar and a preview, pickers for people,
/// channels, emoji and commands, attachments, send later, editing in
/// place, the quote of a reply, who is typing, and "notify anyway"
/// offers for what was just sent.
class Composer extends StatefulWidget {
  const Composer({
    super.key,
    required this.controller,
    required this.enterSends,
    required this.hint,
    this.markdown = const MarkdownContext(),
    this.recentAuthors = const [],
    this.focus,
  });

  final ComposerController controller;
  final bool enterSends;
  final String hint;

  /// For the preview and the quote.
  final MarkdownContext markdown;

  /// Who wrote here lately: listed first after `@`.
  final List<String> recentAuthors;
  final FocusNode? focus;

  @override
  State<Composer> createState() => ComposerState();
}

class ComposerState extends State<Composer> {
  late final FocusNode focus = widget.focus ?? FocusNode();
  bool preview = false;
  bool formatting = false;
  EmojiData emoji = EmojiData.empty;

  Trigger? trigger;

  /// While the server is being asked about a broad mention, or the person
  /// about notifying many people, a second Enter does nothing.
  bool _asking = false;

  /// The start of a trigger the person dismissed: it stays plain text
  /// until they type a new one.
  int? dismissedAt;
  int selected = 0;

  /// A phrase naming a time that ends at the cursor, offered as a time
  /// (ruling R69), and the start of one the person dismissed.
  Phrase? phrase;
  int? phraseDismissedAt;

  ComposerController get cc => widget.controller;
  ServerState get server => cc.server;

  /// Timed out, or slow mode counting down: the box gives way to a notice.
  late final PostingGateController gate = PostingGateController(
    cc.server,
    cc.conversationId,
  );

  @override
  void initState() {
    super.initState();
    gate.start();
    cc.text.chipBuilder = _chip;
    cc.text.addListener(_onText);
    loadEmojiData().then((d) {
      if (mounted) setState(() => emoji = d);
    });
  }

  @override
  void dispose() {
    cc.text.removeListener(_onText);
    gate.dispose();
    if (widget.focus == null) focus.dispose();
    super.dispose();
  }

  /// Gives the box the focus (after "Reply" or "Edit").
  void focusBox() => focus.requestFocus();

  void _onText() {
    final sel = cc.text.selection;
    final t = sel.isValid && sel.isCollapsed
        ? findTrigger(cc.text.text, sel.baseOffset)
        : null;
    if (t?.start != dismissedAt) dismissedAt = null;
    final next = t != null && t.start != dismissedAt ? t : null;
    if (next?.kind != trigger?.kind || next?.start != trigger?.start) selected = 0;
    cc.trackInstants();
    final p = next == null && sel.isValid && sel.isCollapsed
        ? cc.phraseAt(sel.baseOffset)
        : null;
    if (p?.start != phraseDismissedAt) phraseDismissedAt = null;
    setState(() {
      trigger = next;
      phrase = p != null && p.start != phraseDismissedAt ? p : null;
    });
  }

  void _acceptPhrase() {
    final p = phrase;
    if (p == null) return;
    cc.acceptPhrase(p);
    setState(() => phrase = null);
    focus.requestFocus();
  }

  void _dismissPhrase() => setState(() {
    phraseDismissedAt = phrase?.start;
    phrase = null;
  });

  InlineSpan _chip(String id, TextStyle? style) {
    final c = ChurrColors.of(context);
    final conv = server.conversation(id);
    final name = conv?.name ?? 'channel';
    return WidgetSpan(
      alignment: PlaceholderAlignment.middle,
      child: Semantics(
        label: 'Link to channel $name',
        child: Container(
          color: c.raised,
          padding: const EdgeInsets.symmetric(horizontal: 4),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Glyph(
                conv?.private ?? false ? ChurrIcons.private : ChurrIcons.hash,
                size: 14,
                color: c.lavender,
              ),
              const SizedBox(width: 2),
              Flexible(
                child: Text(
                  name,
                  overflow: TextOverflow.ellipsis,
                  style: (style ?? const TextStyle()).copyWith(
                    color: c.lavender,
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  // ---- the pickers ----

  List<Suggestion> _items() {
    final t = trigger;
    if (t == null) return const [];
    final c = ChurrColors.of(context);
    switch (t.kind) {
      case TriggerKind.mention:
        return [
          for (final o in cc.mentionOptions(
            t.query,
            recentAuthors: widget.recentAuthors,
          ))
            Suggestion(
              title: o.label,
              detail: o.detail,
              notice: o.notice,
              leading: o.account == null
                  ? Glyph(ChurrIcons.people, color: c.dim)
                  : Avatar(account: o.account, server: server, size: 24),
            ),
        ];
      case TriggerKind.channel:
        return [
          for (final ch in cc.channelOptions(t.query))
            Suggestion(
              key: Key('channel-option-${ch.id}'),
              title: ch.name ?? '',
              detail: _detailLine(ch),
              leading: Glyph(
                ch.isLocked
                    ? ChurrIcons.locked
                    : ch.private
                    ? ChurrIcons.private
                    : ChurrIcons.hash,
                color: c.dim,
              ),
            ),
        ];
      case TriggerKind.emoji:
        return [
          for (final e in server.emoji.values)
            if (e.name.contains(t.query))
              Suggestion(
                title: ':${e.name}:',
                leading: emojiGlyph(server, ':${e.name}:'),
              ),
          for (final e in emoji.search(t.query, limit: 12))
            Suggestion(
              title: ':${e.code}:',
              leading: Text(e.char, style: const TextStyle(fontSize: 20)),
            ),
        ];
      case TriggerKind.command:
        return [
          for (final cmd in cc.commandOptions(t.query))
            Suggestion(
              title:
                  '/${cmd.name}${cmd.hint == null || cmd.hint!.isEmpty ? '' : ' ${cmd.hint}'}',
              detail: cmd.description.isEmpty ? null : cmd.description,
              leading: Glyph(
                cmd.kind == 'bot'
                    ? ChurrIcons.bot
                    : cmd.kind == 'saved_reply'
                    ? ChurrIcons.bolt
                    : ChurrIcons.bolt,
                color: c.dim,
              ),
            ),
        ];
    }
  }

  String _detailLine(Conversation ch) {
    final app = server;
    final parent = ch.parentId == null ? null : app.conversation(ch.parentId!);
    String? section;
    for (final s in app.sections) {
      if (s.id == ch.sectionId) section = s.name;
    }
    return channelDetailLine(
      ch,
      parentName: parent?.name,
      sectionName: section,
    );
  }

  void _pick(int i) {
    final t = trigger;
    if (t == null) return;
    switch (t.kind) {
      case TriggerKind.mention:
        final o = cc.mentionOptions(
          t.query,
          recentAuthors: widget.recentAuthors,
        )[i];
        cc.text.replaceRange(t.start, t.end, '@${o.insert} ');
      case TriggerKind.channel:
        cc.text.insertChip(t.start, t.end, cc.channelOptions(t.query)[i].id);
      case TriggerKind.emoji:
        final custom = [
          for (final e in server.emoji.values)
            if (e.name.contains(t.query)) e,
        ];
        if (i < custom.length) {
          cc.text.replaceRange(t.start, t.end, ':${custom[i].name}: ');
        } else {
          final e = emoji.search(t.query, limit: 12)[i - custom.length];
          cc.text.replaceRange(t.start, t.end, e.char);
          unawaited(server.db.useEmoji(e.char));
        }
      case TriggerKind.command:
        final picked = cc.commandOptions(t.query)[i];
        // A built-in about a person opens the people picker at once.
        final person =
            picked.kind == 'builtin' &&
            (builtinCommands[picked.name]?.$1.startsWith('person') ?? false);
        cc.text.replaceRange(
          t.start,
          t.end,
          '/${picked.name} ${person ? '@' : ''}',
        );
    }
    focus.requestFocus();
  }

  void _dismiss() => setState(() {
    dismissedAt = trigger?.start;
    trigger = null;
  });

  // ---- keys ----

  KeyEventResult _onKey(FocusNode node, KeyEvent e) {
    if (e is! KeyDownEvent && e is! KeyRepeatEvent) return KeyEventResult.ignored;
    final items = trigger == null ? const <Suggestion>[] : _items();
    final key = e.logicalKey;
    final shift = HardwareKeyboard.instance.isShiftPressed;
    if (items.isNotEmpty) {
      if (key == LogicalKeyboardKey.arrowDown) {
        setState(() => selected = (selected + 1) % items.length);
        return KeyEventResult.handled;
      }
      if (key == LogicalKeyboardKey.arrowUp) {
        setState(() => selected = (selected - 1 + items.length) % items.length);
        return KeyEventResult.handled;
      }
      if (key == LogicalKeyboardKey.tab ||
          (key == LogicalKeyboardKey.enter && !shift)) {
        _pick(selected.clamp(0, items.length - 1));
        return KeyEventResult.handled;
      }
      if (key == LogicalKeyboardKey.escape) {
        _dismiss();
        return KeyEventResult.handled;
      }
    }
    if (items.isEmpty && phrase != null) {
      if (key == LogicalKeyboardKey.tab ||
          (key == LogicalKeyboardKey.enter && !shift)) {
        _acceptPhrase();
        return KeyEventResult.handled;
      }
      if (key == LogicalKeyboardKey.escape) {
        _dismissPhrase();
        return KeyEventResult.handled;
      }
    }
    if (key == LogicalKeyboardKey.escape) {
      if (cc.editing != null) {
        cc.cancelEdit();
        return KeyEventResult.handled;
      }
      if (cc.quoted != null) {
        cc.clearQuote();
        return KeyEventResult.handled;
      }
    }
    if (key == LogicalKeyboardKey.enter &&
        !shift &&
        widget.enterSends &&
        !_touch) {
      _send();
      return KeyEventResult.handled;
    }
    final mod =
        HardwareKeyboard.instance.isMetaPressed ||
        HardwareKeyboard.instance.isControlPressed;
    if (mod) {
      if (key == LogicalKeyboardKey.keyB) return _format(Format.bold);
      if (key == LogicalKeyboardKey.keyI) return _format(Format.italic);
      if (key == LogicalKeyboardKey.keyK) return _format(Format.link);
      if (key == LogicalKeyboardKey.keyV) {
        _paste();
        return KeyEventResult.handled;
      }
    }
    return KeyEventResult.ignored;
  }

  bool get _touch => switch (Theme.of(context).platform) {
    TargetPlatform.android || TargetPlatform.iOS => true,
    _ => false,
  };

  KeyEventResult _format(Format f) {
    final t = cc.text;
    final (text, a, b) = applyFormat(
      t.text,
      t.selection.start,
      t.selection.end,
      f,
    );
    t.value = TextEditingValue(
      text: text,
      selection: TextSelection(baseOffset: a, extentOffset: b),
    );
    focus.requestFocus();
    return KeyEventResult.handled;
  }

  /// Pasting: an image on the clipboard is attached; text is inserted,
  /// with `<#id>` of a channel the person can see shown as a chip.
  Future<void> _paste() async {
    final image = await pastedImage();
    if (image != null) {
      await _addFiles([image]);
      return;
    }
    final data = await Clipboard.getData(Clipboard.kTextPlain);
    final pasted = data?.text;
    if (pasted == null || pasted.isEmpty) return;
    final encoded = cc.text.chips.encode(pasted, cc.canSee);
    final sel = cc.text.selection;
    final start = sel.isValid ? sel.start : cc.text.text.length;
    final end = sel.isValid ? sel.end : start;
    cc.text.replaceRange(start, end, encoded);
  }

  // ---- actions ----

  Future<void> _addFiles(List<(String, Uint8List)> files) async {
    final refused = await cc.addFiles([for (final (n, b) in files) (n, b)]);
    if (refused.isNotEmpty && mounted) {
      ScaffoldMessenger.maybeOf(context)
          ?.showSnackBar(SnackBar(content: Text(refused.join('\n'))));
    }
  }

  /// Files dropped on the conversation.
  Future<void> addDropped(List<(String, Uint8List)> files) => _addFiles(files);

  Future<void> _attach() async {
    try {
      final files = await pickFiles();
      if (files.isNotEmpty) await _addFiles(files);
    } on Object catch (e) {
      _say('Could not open the file picker. ($e)');
    }
  }

  Future<void> _insertEmoji() async {
    final picked = await showEmojiPicker(
      context,
      server,
      recent: cc.recentEmoji,
    );
    if (picked == null) return;
    final sel = cc.text.selection;
    final at = sel.isValid ? sel.start : cc.text.text.length;
    final end = sel.isValid ? sel.end : at;
    cc.text.replaceRange(at, end, picked.startsWith(':') ? '$picked ' : picked);
    focus.requestFocus();
  }

  void _say(String s) {
    if (!mounted) return;
    ScaffoldMessenger.maybeOf(context)
      ?..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(s)));
  }

  /// "This will notify N people", with the number the server gave.
  Future<bool?> _confirmBroad(int recipients) {
    final t = L10n.of(context);
    return showDialog<bool>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (context) => AlertDialog(
        key: const Key('broad-confirm'),
        title: Text(t.broadConfirmTitle),
        content: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 420),
          child: Text(t.broadConfirmBody(recipients)),
        ),
        actions: [
          TextButton(
            key: const Key('broad-confirm-cancel'),
            onPressed: () => Navigator.of(context).pop(false),
            child: Text(t.broadConfirmCancel),
          ),
          FilledButton(
            key: const Key('broad-confirm-send'),
            onPressed: () => Navigator.of(context).pop(true),
            child: Text(t.broadConfirmSend),
          ),
        ],
      ),
    );
  }

  Future<void> _send() async {
    // A broad mention that would notify many people is asked about first.
    if (_asking) return;
    _asking = true;
    try {
      if (!await cc.broadOK(_confirmBroad)) {
        if (mounted) focus.requestFocus();
        return;
      }
    } finally {
      _asking = false;
    }
    final out = await cc.send();
    if (!mounted) return;
    switch (out) {
      case RunBuiltin(:final name, :final args, :final parsed):
        // A command that does not work in a sealed chat, or that was
        // mistyped, is refused here before anything is sent (ruling R67).
        final t = L10n.of(context);
        if ((cc.conversation?.isSealed ?? false) &&
            !(builtinCommands[name]?.$2 ?? false)) {
          _say(notInSealed(t, name));
          return;
        }
        if (parsed != null && parsed['error'] != null) {
          _say(commandProblem(t, parsed));
          return;
        }
        final handler = BuiltinCommands.handlerFor(name);
        if (handler == null) {
          _say('/$name is not available in this build yet.');
          return;
        }
        final done = await handler(
          context,
          CommandRequest(
            server: server,
            conversationId: cc.conversationId,
            parentId: cc.parentId,
            name: name,
            args: args,
            parsed: parsed,
          ),
        );
        if (done) cc.text.clear();
      case SendFailed(:final message):
        _say(message);
      case Sent() || NothingToSend():
        break;
    }
    focus.requestFocus();
  }

  Future<void> _sendLater() async {
    final at = await showScheduleSheet(context);
    if (at == null) return;
    final out = await cc.schedule(at);
    if (out is SendFailed) _say(out.message);
    if (out is Sent) _say('Scheduled for ${scheduleLabel(at)}.');
  }

  // ---- drawing ----

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return ListenableBuilder(
      listenable: Listenable.merge([cc, gate]),
      builder: (context, _) {
        final conv = cc.conversation;
        final notice = _cannotPost(context, conv) ?? _gateNotice(context);
        final items = _items();
        return Container(
          padding: EdgeInsets.fromLTRB(
            16,
            6,
            16,
            14 + MediaQuery.viewPaddingOf(context).bottom,
          ),
          child: Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: maxTextColumn),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  for (final o in cc.offers)
                    _OfferBar(offer: o, controller: cc),
                  if (cc.notNotified.isNotEmpty)
                    _NoticeBar(
                      text:
                          '${cc.notNotified.map((a) => a.name).join(', ')} '
                          '${cc.notNotified.length == 1 ? 'is' : 'are'} not in this conversation and will not be notified.',
                      onClose: cc.dismissNotNotified,
                    ),
                  if (cc.broadRefused)
                    _NoticeBar(
                      key: const Key('broad-went-plain'),
                      text: L10n.of(context).broadWentPlain,
                      onClose: cc.dismissBroadRefused,
                    ),
                  if (notice == null && cc.broadGoesPlainNow)
                    _BroadHint(text: L10n.of(context).broadGoesPlain),
                  if (notice != null)
                    _JoinOrNotice(
                      text: notice,
                      onJoin: conv?.membership == null ? _join : null,
                    )
                  else ...[
                    if (items.isNotEmpty)
                      Padding(
                        padding: const EdgeInsets.only(bottom: 6),
                        child: SuggestionList(
                          title: switch (trigger!.kind) {
                            TriggerKind.mention => 'MENTION',
                            TriggerKind.channel => 'LINK A CHANNEL?',
                            TriggerKind.emoji => 'EMOJI',
                            TriggerKind.command => 'COMMANDS',
                          },
                          items: items,
                          selected: selected.clamp(0, items.length - 1),
                          onPick: _pick,
                          onDismiss: _dismiss,
                        ),
                      ),
                    if (items.isEmpty && phrase != null)
                      Padding(
                        padding: const EdgeInsets.only(bottom: 6),
                        child: SuggestionList(
                          key: const Key('instant-offer'),
                          title: L10n.of(context).instantMakeTime,
                          items: [
                            Suggestion(
                              key: const Key('instant-offer-accept'),
                              title: L10n.of(context).instantMakeTimeLine(
                                compactInstant(
                                  DateTime.parse(phrase!.time)
                                      .millisecondsSinceEpoch,
                                  InstantEnv.viewerZone,
                                  Localizations.localeOf(context).toString(),
                                  instantWords(context),
                                ),
                              ),
                              leading: Glyph(ChurrIcons.clock, color: c.cyan),
                            ),
                          ],
                          selected: 0,
                          onPick: (_) => _acceptPhrase(),
                          onDismiss: _dismissPhrase,
                        ),
                      ),
                    _InstantStrip(controller: cc),
                    if (cc.editing != null)
                      _ContextBar(
                        icon: ChurrIcons.edit,
                        text: 'Editing a message. Escape to cancel.',
                        onClose: cc.cancelEdit,
                      ),
                    if (cc.quoted != null)
                      _ContextBar(
                        key: const Key('composer-quote'),
                        icon: ChurrIcons.quote,
                        text:
                            quoteText(
                              cc.quoted,
                              Quote(
                                messageId: cc.quoted!.id,
                                start: cc.quoteStart,
                                end: cc.quoteEnd,
                              ),
                            ) ??
                            'original message changed',
                        label:
                            'Replying to ${server.accounts[cc.quoted!.authorId]?.name ?? 'a message'}',
                        onClose: cc.clearQuote,
                      ),
                    if (cc.attachments.isNotEmpty) _Attachments(controller: cc),
                    if (formatting)
                      _FormatBar(
                        onFormat: _format,
                        preview: preview,
                        onPreview: _togglePreview,
                      ),
                    if (preview)
                      Container(
                        key: const Key('composer-preview'),
                        constraints: const BoxConstraints(
                          minHeight: 48,
                          maxHeight: 240,
                        ),
                        decoration: BoxDecoration(
                          color: c.panel,
                          border: Border(
                            bottom: BorderSide(color: c.edge, width: 2),
                          ),
                        ),
                        padding: const EdgeInsets.all(10),
                        child: SingleChildScrollView(
                          child: cc.text.text.trim().isEmpty
                              ? Text(
                                  'Nothing to preview.',
                                  style: TextStyle(color: c.dim),
                                )
                              : MarkdownView(
                                  text: cc.text.wireText,
                                  context_: widget.markdown,
                                ),
                        ),
                      )
                    else
                      _box(c),
                    if (cc.typingLine != null)
                      Padding(
                        padding: const EdgeInsets.only(top: 4),
                        child: Semantics(
                          liveRegion: true,
                          child: Text(
                            cc.typingLine!,
                            key: const Key('typing'),
                            style: monoStyle(context),
                          ),
                        ),
                      ),
                  ],
                ],
              ),
            ),
          ),
        );
      },
    );
  }

  /// Joins the channel being read, then offers its public sub-channels
  /// (docs/CLIENTS.md 9.25).
  Future<void> _join() async {
    try {
      final joined = await ChannelActions(server).join(cc.conversationId);
      if (mounted) await offerSubChannels(context, server, joined);
    } on Exception catch (e) {
      _say('Could not join. ($e)');
    }
  }

  void _togglePreview() => setState(() => preview = !preview);

  /// Timed out until a time, or slow mode's countdown (9.5).
  String? _gateNotice(BuildContext context) {
    final t = L10n.of(context);
    final g = gate.gate;
    return switch (g.kind) {
      GateKind.open => null,
      GateKind.timedOut => t.timedOutUntil(
        DateFormat.yMMMd(
          Localizations.localeOf(context).toString(),
        ).add_Hm().format(g.until!.toLocal()),
      ),
      GateKind.slowMode => t.slowModeWait(countdown(g.left)),
    };
  }

  /// Why the person cannot post here, or null (9.3, 9.5).
  String? _cannotPost(BuildContext context, Conversation? conv) {
    if (conv == null) return null;
    final why = cannotPost(
      conv,
      manager: ChannelActions(server).actsAsManager(conv),
      reply: cc.parentId != null,
    );
    return switch (why) {
      null => null,
      NoPost.notMember => 'Join to take part.',
      NoPost.archived => 'This channel is archived. Nothing more can be posted.',
      NoPost.reader => 'Only some members post in this channel. You can read and react.',
      NoPost.announcement => L10n.of(context).announcementNoPost,
      NoPost.announcementReplyOnly => L10n.of(context).announcementReplyOnly,
    };
  }

  Widget _box(ChurrColors c) {
    final sendKey = cc.editing != null ? 'Save' : 'Send';
    final attach = IconButton(
      key: const Key('attach'),
      tooltip: 'Attach files',
      icon: const Glyph(ChurrIcons.attach),
      onPressed:
          cc.editing != null ||
              cc.attachments.length >= ComposerController.maxAttachments
          ? null
          : _attach,
    );
    final field = Focus(
      onKeyEvent: _onKey,
      child: TextField(
        key: const Key('composer'),
        controller: cc.text,
        focusNode: focus,
        minLines: 1,
        maxLines: 10,
        textInputAction: TextInputAction.newline,
        keyboardType: TextInputType.multiline,
        inputFormatters: [ChipPasteFormatter(cc.text, cc.canSee)],
        style: TextStyle(fontSize: 16, height: 1.4, color: c.ink),
        // Empty, the box shows a blinking block cursor after the hint:
        // the guide's one terminal touch. Typing, the usual caret.
        showCursor: cc.text.text.isNotEmpty,
        decoration: InputDecoration(
          hint: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Flexible(
                child: Text(
                  widget.hint,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 16, color: c.faint),
                ),
              ),
              const SizedBox(width: 2),
              const BlockCursor(),
            ],
          ),
          filled: false,
          border: InputBorder.none,
          enabledBorder: InputBorder.none,
          focusedBorder: InputBorder.none,
          contentPadding: const EdgeInsets.symmetric(
            horizontal: 4,
            vertical: 12,
          ),
        ),
      ),
    );
    final format = IconButton(
      tooltip: formatting ? 'Hide formatting' : 'Formatting',
      icon: Glyph(ChurrIcons.textFormat, color: formatting ? c.lavender : null),
      onPressed: () => setState(() => formatting = !formatting),
    );
    final emojiButton = IconButton(
      tooltip: 'Emoji',
      icon: const Glyph(ChurrIcons.emoji),
      onPressed: _insertEmoji,
    );
    final send = cc.sending
        ? const Padding(padding: EdgeInsets.all(12), child: Busy(size: 20))
        : Builder(
            builder: (context) => FilledButton(
              key: const Key('send'),
              onPressed: _send,
              onLongPress: cc.editing == null ? _sendLater : null,
              child: Semantics(
                label: sendKey,
                excludeSemantics: true,
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(sendKey.toUpperCase()),
                    const SizedBox(width: 8),
                    Glyph(
                      cc.editing != null
                          ? ChurrIcons.check
                          : ChurrIcons.arrowRight,
                      size: 16,
                    ),
                  ],
                ),
              ),
            ),
          );
    final menu = cc.editing != null
        ? null
        : PopupMenuButton<String>(
  popUpAnimationStyle: AnimationStyle.noAnimation,
            key: const Key('send-menu'),
            tooltip: 'More ways to send',
            icon: const Glyph(ChurrIcons.chevronDown),
            onSelected: (_) => _sendLater(),
            itemBuilder: (_) => const [
              PopupMenuItem(value: 'later', child: Text('Send later…')),
            ],
          );
    // The box: panel, with the edge as a line under it (yellow while it
    // has the focus). No frame.
    Widget block(Widget child) => ListenableBuilder(
      listenable: focus,
      builder: (context, _) => Container(
        decoration: BoxDecoration(
          color: c.panel,
          border: Border(
            bottom: BorderSide(
              color: focus.hasFocus ? c.focus : c.edge,
              width: focus.hasFocus ? 3 : 2,
            ),
          ),
        ),
        padding: const EdgeInsets.fromLTRB(4, 4, 8, 4),
        child: child,
      ),
    );
    return LayoutBuilder(
      builder: (context, box) {
        // On a narrow width the box gets the whole row; the tools sit
        // under it.
        if (box.maxWidth < 560) {
          return Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              block(
                Row(
                  crossAxisAlignment: CrossAxisAlignment.center,
                  children: [
                    Expanded(child: field),
                    const SizedBox(width: 6),
                    send,
                  ],
                ),
              ),
              Row(
                children: [attach, format, emojiButton, const Spacer(), ?menu],
              ),
            ],
          );
        }
        return block(
          Row(
            crossAxisAlignment: CrossAxisAlignment.center,
            children: [
              attach,
              Expanded(child: field),
              format,
              emojiButton,
              const SizedBox(width: 6),
              send,
              ?menu,
            ],
          ),
        );
      },
    );
  }
}

/// A plain line above the box: the message being edited, or the quote of
/// a reply, with a way to dismiss it.
class _ContextBar extends StatelessWidget {
  const _ContextBar({
    super.key,
    required this.icon,
    required this.text,
    required this.onClose,
    this.label,
  });
  final ChurrGlyph icon;
  final String text;
  final String? label;
  final VoidCallback onClose;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Container(
      margin: const EdgeInsets.only(bottom: 6),
      decoration: BoxDecoration(
        color: c.raised,
        border: Border(left: BorderSide(color: c.lavender, width: 4)),
      ),
      padding: const EdgeInsets.only(left: 10),
      child: Row(
        children: [
          Glyph(icon, size: 16, color: c.dim),
          const SizedBox(width: 8),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (label != null)
                  Text(label!, style: monoStyle(context, size: 10)),
                Text(
                  text,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(color: c.ink, fontSize: 13),
                ),
              ],
            ),
          ),
          IconButton(
            tooltip: 'Dismiss',
            icon: const Glyph(ChurrIcons.close, size: 18),
            onPressed: onClose,
          ),
        ],
      ),
    );
  }
}

/// Said while the text holds a broad mention the person may not use
/// here: it will go as plain text (ruling R64).
class _BroadHint extends StatelessWidget {
  const _BroadHint({required this.text});
  final String text;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Semantics(
      liveRegion: true,
      child: Container(
        key: const Key('broad-hint'),
        margin: const EdgeInsets.only(bottom: 6),
        color: c.raised,
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Glyph(ChurrIcons.info, size: 16, color: c.yellowInk),
            const SizedBox(width: 8),
            Expanded(child: Text(text, style: const TextStyle(fontSize: 13))),
          ],
        ),
      ),
    );
  }
}

class _NoticeBar extends StatelessWidget {
  const _NoticeBar({super.key, required this.text, required this.onClose});
  final String text;
  final VoidCallback onClose;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Container(
      margin: const EdgeInsets.only(bottom: 6),
      color: c.raised,
      padding: const EdgeInsets.only(left: 10),
      child: Row(
        children: [
          Glyph(ChurrIcons.info, size: 16, color: c.yellowInk),
          const SizedBox(width: 8),
          Expanded(child: Text(text, style: const TextStyle(fontSize: 13))),
          IconButton(
            tooltip: 'Dismiss',
            icon: const Glyph(ChurrIcons.close, size: 18),
            onPressed: onClose,
          ),
        ],
      ),
    );
  }
}

/// "Notify anyway" for one person in do not disturb (FEATURES.md 3.8).
class _OfferBar extends StatelessWidget {
  const _OfferBar({required this.offer, required this.controller});
  final NotifyOffer offer;
  final ComposerController controller;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Container(
      key: Key('notify-offer-${offer.account.id}'),
      margin: const EdgeInsets.only(bottom: 6),
      color: c.raised,
      padding: const EdgeInsets.only(left: 10),
      child: Row(
        children: [
          Glyph(ChurrIcons.dnd, size: 16, color: c.dim),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              '${offer.account.name} is in do not disturb.',
              style: const TextStyle(fontSize: 13),
            ),
          ),
          TextButton(
            onPressed: () => controller.notifyAnyway(offer),
            child: const Text('Notify anyway'),
          ),
          IconButton(
            tooltip: 'Dismiss',
            icon: const Glyph(ChurrIcons.close, size: 18),
            onPressed: () => controller.dismissOffer(offer),
          ),
        ],
      ),
    );
  }
}

/// Where the box would be when the person cannot post.
class _JoinOrNotice extends StatelessWidget {
  const _JoinOrNotice({required this.text, this.onJoin});
  final String text;

  /// Set for a public channel read without joining.
  final VoidCallback? onJoin;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Container(
      key: const Key('cannot-post'),
      constraints: const BoxConstraints(minHeight: 48),
      color: c.raised,
      alignment: Alignment.center,
      padding: const EdgeInsets.all(10),
      child: onJoin == null
          ? Text(
              text,
              textAlign: TextAlign.center,
              style: TextStyle(color: c.dim),
            )
          : Row(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Flexible(
                  child: Text(text, style: TextStyle(color: c.dim)),
                ),
                const SizedBox(width: 12),
                FilledButton(
                  key: const Key('join'),
                  onPressed: onJoin,
                  child: const Text('Join'),
                ),
              ],
            ),
    );
  }
}

class _FormatBar extends StatelessWidget {
  const _FormatBar({
    required this.onFormat,
    required this.preview,
    required this.onPreview,
  });
  final KeyEventResult Function(Format) onFormat;
  final bool preview;
  final VoidCallback onPreview;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    Widget b(Format f, ChurrGlyph icon, String label) => IconButton(
      tooltip: label,
      icon: Glyph(icon, size: 18),
      onPressed: preview ? null : () => onFormat(f),
    );
    return Container(
      key: const Key('format-bar'),
      margin: const EdgeInsets.only(bottom: 4),
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: Row(
          children: [
            b(Format.bold, ChurrIcons.bold, 'Bold'),
            b(Format.italic, ChurrIcons.italic, 'Italic'),
            b(Format.strike, ChurrIcons.strike, 'Strikethrough'),
            b(Format.code, ChurrIcons.code, 'Code'),
            b(Format.link, ChurrIcons.link, 'Link'),
            b(Format.bullets, ChurrIcons.bullets, 'Bulleted list'),
            b(Format.numbers, ChurrIcons.numbers, 'Numbered list'),
            b(Format.checklist, ChurrIcons.checklist, 'Checklist'),
            b(Format.quote, ChurrIcons.quote, 'Quote'),
            b(Format.codeBlock, ChurrIcons.codeBlock, 'Code block'),
            const SizedBox(width: 8),
            TextButton.icon(
              key: const Key('preview-toggle'),
              onPressed: onPreview,
              icon: Glyph(
                preview ? ChurrIcons.edit : ChurrIcons.eye,
                size: 18,
                color: c.lavender,
              ),
              label: Text(preview ? 'Write' : 'Preview'),
            ),
          ],
        ),
      ),
    );
  }
}

/// The files chosen: a thumbnail or icon, the name and size, upload
/// progress, a caption and a remove button.
class _Attachments extends StatelessWidget {
  const _Attachments({required this.controller});
  final ComposerController controller;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          for (final a in controller.attachments)
            // Not sent yet: provisional, so dashed. A failed one is a
            // solid pink edge.
            Padding(
              key: Key('attachment-${a.localId}'),
              padding: const EdgeInsets.only(bottom: 4),
              child: DashedBox(
                color: a.error != null ? c.pink : c.edge,
                child: Container(
                  color: c.panel,
                  padding: const EdgeInsets.all(8),
                  child: Row(
                    children: [
                      SizedBox(
                        width: 48,
                        height: 48,
                        child: a.isImage
                            ? Image.memory(
                                a.bytes,
                                fit: BoxFit.cover,
                                semanticLabel: a.name,
                                errorBuilder: (_, _, _) =>
                                    Glyph(ChurrIcons.image, color: c.dim),
                              )
                            : a.poster != null
                            ? Image.memory(a.poster!, fit: BoxFit.cover)
                            : Glyph(
                                a.isVideo ? ChurrIcons.movie : ChurrIcons.file,
                                color: c.dim,
                              ),
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Row(
                              children: [
                                Expanded(
                                  child: Text(
                                    a.name,
                                    overflow: TextOverflow.ellipsis,
                                    style: const TextStyle(
                                      fontWeight: FontWeight.w700,
                                    ),
                                  ),
                                ),
                                Text(
                                  formatBytes(a.size),
                                  style: monoStyle(context),
                                ),
                              ],
                            ),
                            if (a.progress != null)
                              Padding(
                                padding: const EdgeInsets.symmetric(
                                  vertical: 4,
                                ),
                                child: LinearProgressIndicator(
                                  value: a.progress,
                                  minHeight: 3,
                                  semanticsLabel: 'Uploading ${a.name}',
                                ),
                              ),
                            if (a.error != null)
                              Text(
                                a.error!,
                                style: TextStyle(color: c.pink, fontSize: 12),
                              )
                            else
                              TextFormField(
                                initialValue: a.caption,
                                onChanged: (v) => controller.setCaption(a, v),
                                enabled:
                                    a.uploaded == null && a.progress == null,
                                style: const TextStyle(fontSize: 13),
                                decoration: const InputDecoration(
                                  hintText: 'Caption (read aloud for images)',
                                  isDense: true,
                                ),
                              ),
                          ],
                        ),
                      ),
                      IconButton(
                        tooltip: 'Remove ${a.name}',
                        icon: const Glyph(ChurrIcons.close, size: 18),
                        onPressed: a.progress != null
                            ? null
                            : () => controller.removeAttachment(a),
                      ),
                    ],
                  ),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

/// The times the text holds, each with what it will mean, and a way to
/// send one as plain text (ruling R69). Provisional, so dashed.
class _InstantStrip extends StatelessWidget {
  const _InstantStrip({required this.controller});
  final ComposerController controller;

  @override
  Widget build(BuildContext context) {
    final cc = controller;
    final typed = cc.typedInstants();
    if (typed.isEmpty && cc.hints.isEmpty) return const SizedBox.shrink();
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final zone = InstantEnv.viewerZone;
    final locale = Localizations.localeOf(context).toString();
    final words = instantWords(context);
    final wire = cc.text.wireText;
    String when(String time, String precision) => compactInstant(
      DateTime.parse(time).millisecondsSinceEpoch,
      zone,
      locale,
      words,
      precision: precision,
    );
    Widget chip(
      Key key,
      String said,
      String action,
      String label,
      VoidCallback onTap,
    ) => Container(
      key: key,
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: c.bg,
        border: Border.all(color: c.cyan, width: 1.5),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Glyph(ChurrIcons.clock, size: 14, color: c.cyan),
          const SizedBox(width: 6),
          Flexible(
            child: Text(
              said,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(fontSize: 13, color: c.ink),
            ),
          ),
          Semantics(
            button: true,
            label: label,
            excludeSemantics: true,
            child: TextButton(onPressed: onTap, child: Text(action)),
          ),
        ],
      ),
    );
    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: Semantics(
        container: true,
        label: t.instantsHere,
        child: Wrap(
          spacing: 6,
          runSpacing: 6,
          children: [
            for (final (n, r) in typed)
              chip(
                Key('instant-chip-${n.at}'),
                t.instantComposerSays(
                  wire.substring(n.at, n.end),
                  when(r.time, r.precision),
                ),
                t.instantKeepAsText,
                t.instantKeepAsTextLabel(wire.substring(n.at, n.end)),
                () => cc.keepAsText(n.at),
              ),
            for (final h in cc.hints)
              chip(
                Key('instant-hint-${h.start}'),
                t.instantComposerPhrase(h.typed, when(h.time, h.precision)),
                t.instantDropTime,
                t.instantDropTimeLabel(h.typed),
                () => cc.dropHint(h),
              ),
          ],
        ),
      ),
    );
  }
}
