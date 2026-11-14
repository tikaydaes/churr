import 'dart:async';

import 'package:flutter/material.dart';

import '../../../core/conversation/threads.dart';
import '../../../core/db/cache_db.dart';
import '../../../core/db/conversation_queries.dart';
import '../../../core/live.dart';
import '../../../core/models/conversation.dart';
import '../../../core/models/message.dart';
import '../../../platform/media_files.dart';
import '../../../state/thread_pane_signal.dart';
import '../../../state/app_state.dart';
import '../../../state/composer_controller.dart';
import '../../../state/conversation_controller.dart';
import '../../../state/conversation_extras.dart';
import '../../../state/conversation_intents.dart';
import '../../../state/message_actions.dart';
import '../../../state/server_state.dart';
import '../../../state/sidebar_model.dart';
import '../../layout/breakpoints.dart';
import '../../message/markdown_view.dart';
import '../../message/server_image.dart';
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../../theme/shapes.dart';
import '../../widgets/controls.dart';
import '../../theme/churr_icons.dart';
import '../themes/theme_link.dart';
import '../channels/channel_settings_screen.dart';
import 'composer.dart';
import 'conversation_message.dart';
import 'message_menu.dart';
import 'thread_pane.dart';

/// Buttons other parts of the app put in the conversation header
/// (search in this conversation, pins, members, settings). Each builder
/// is called with the server and the conversation, and may return null.
///
/// ```dart
/// ConversationHeaderSlots.actions.add((context, server, id) => IconButton(…));
/// ```
abstract final class ConversationHeaderSlots {
  static final actions =
      <
        Widget? Function(
          BuildContext context,
          ServerState server,
          String conversationId,
        )
      >[];
}

/// The conversation screen (docs/CLIENTS.md 9.3): the header, the body
/// in the threaded or the flat view, the thread pane (beside it on wide
/// screens, over it on narrow ones), and the composer.
///
/// It follows the pattern of README.md, "How to add a screen": the
/// [ConversationController] watches the messages and the outbox,
/// [ConversationExtras] the thread states and bookmarks, and a
/// [ComposerController] the draft; everything drawn comes from the
/// local database.
class ConversationScreen extends StatefulWidget {
  const ConversationScreen({
    super.key,
    required this.server,
    required this.conversationId,
    this.onBack,
  });

  final ServerState server;
  final String conversationId;

  /// Set on compact widths, where the conversation is pushed over the
  /// sidebar and needs a way back.
  final VoidCallback? onBack;

  @override
  State<ConversationScreen> createState() => _ConversationScreenState();
}

class _ConversationScreenState extends State<ConversationScreen>
    implements MessageHost {
  late final ConversationController controller;
  late final ConversationExtras extras;
  late final ComposerController composer;
  late final MessageActions actions;
  final composerKey = GlobalKey<ComposerState>();
  final threadKey = GlobalKey<ThreadPaneState>();
  final scroll = ScrollController();
  final _keys = <String, GlobalKey>{};

  /// The read position when the conversation was opened: where the
  /// "new" line is drawn. It stays put while the person reads.
  int? readSeqAtOpen;
  int lastSeenCount = -1;

  /// The open thread, and the quote to start a reply with.
  String? threadParentId;
  (Message, int?, int?)? threadQuote;
  String? highlighted;
  bool dropping = false;
  bool scrolledUp = false;

  OpenTarget get _target =>
      OpenTarget(widget.server.localId, widget.conversationId);

  @override
  void initState() {
    super.initState();
    controller = ConversationController(
      widget.server,
      widget.conversationId,
      keepsDraft: false,
    )..start();
    extras = ConversationExtras(widget.server, widget.conversationId)..start();
    composer = ComposerController(
      widget.server,
      widget.conversationId,
      posterMaker: takePosterFrame,
    )..start();
    actions = MessageActions(widget.server);
    readSeqAtOpen = widget.server
        .conversation(widget.conversationId)
        ?.membership
        ?.readSeq;
    scroll.addListener(_onScroll);
    // A shared channel may be one a hub owns: its record says who are
    // moderators ("Delete for everyone").
    if (widget.server.conversation(widget.conversationId)?.shared ?? false) {
      unawaited(widget.server.loadHubChannel(widget.conversationId));
    }
    ConversationIntents.instance.addListener(_takeIntent);
    WidgetsBinding.instance.addPostFrameCallback((_) => _takeIntent());
    // "Open in place" from a notification, the notifications list,
    // Threads or Bookmarks (AppState.openMessage): read once, then
    // brought into view as soon as the message is loaded.
    final app = AppScope.read(context);
    if (app.focusMessageId != null &&
        app.open == _target) {
      _focusMessage = app.focusMessageId;
      _focusThread = app.focusThreadId;
      app.clearFocus();
      controller.addListener(_tryFocus);
      WidgetsBinding.instance.addPostFrameCallback((_) => _tryFocus());
    }
  }

  String? _focusMessage;
  String? _focusThread;
  int _focusTries = 0;

  /// Scrolls to the message [AppState.openMessage] asked for, or opens
  /// its thread on it, once it is in the loaded run; older pages are
  /// fetched a few times to find it.
  void _tryFocus() {
    final id = _focusMessage;
    if (id == null || !mounted) return;
    final thread = _focusThread;
    if (thread != null && !flat) {
      _focusMessage = null;
      controller.removeListener(_tryFocus);
      openThread(thread);
      WidgetsBinding.instance.addPostFrameCallback(
        (_) => threadKey.currentState?.jumpTo(id),
      );
      return;
    }
    if (controller.messages.any((m) => m.id == id)) {
      _focusMessage = null;
      controller.removeListener(_tryFocus);
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) jumpTo(id);
      });
    } else if (controller.hasOlder && _focusTries++ < 5) {
      controller.loadOlder();
    }
  }

  void _takeIntent() {
    final intent = ConversationIntents.instance.take(_target);
    if (intent == null || !mounted) return;
    if (intent.threadParentId != null) openThread(intent.threadParentId!);
    if (intent.messageId != null) jumpTo(intent.messageId!);
  }

  void _onScroll() {
    // The list is drawn bottom-up, so its far end is the oldest message.
    if (scroll.position.pixels > scroll.position.maxScrollExtent - 400 &&
        controller.hasOlder) {
      controller.loadOlder();
    }
    final up = scroll.position.pixels > 400;
    if (up != scrolledUp) setState(() => scrolledUp = up);
    if (!up) _maybeMarkRead(force: true);
  }

  @override
  void dispose() {
    ConversationIntents.instance.removeListener(_takeIntent);
    controller.removeListener(_tryFocus);
    ThreadPaneSignal.report(false, closeThread);
    controller.dispose();
    extras.dispose();
    composer.dispose();
    scroll.dispose();
    super.dispose();
  }

  /// The read position moves when the newest message is on screen: the
  /// list is at its bottom and something new has arrived.
  void _maybeMarkRead({bool force = false}) {
    if (!force && controller.messages.length == lastSeenCount) return;
    if (scroll.hasClients && scroll.position.pixels > 80) return;
    lastSeenCount = controller.messages.length;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) controller.markRead();
    });
  }

  int _lastAuthorCheck = 0;

  /// A message from someone ends their typing line.
  void _noteArrivals() {
    final ms = controller.messages;
    if (ms.length <= _lastAuthorCheck) {
      _lastAuthorCheck = ms.length;
      return;
    }
    for (final m in ms.skip(_lastAuthorCheck)) {
      composer.messageArrived(m.authorId);
    }
    _lastAuthorCheck = ms.length;
  }

  // ---- MessageHost ----

  @override
  ServerState get server => widget.server;

  @override
  Conversation? get conversation =>
      controller.row?.conversation ??
      widget.server.conversation(widget.conversationId);

  @override
  ServerLimits get limits => extras.limits;

  @override
  bool get flat => isFlatView(
    conversationOverride: conversation?.membership?.threadView,
    personDefault: server.me?.threadView ?? 'threaded',
  );

  @override
  bool get inThread => false;

  @override
  ThreadState? threadState(String parentId) => extras.threadStates[parentId];

  @override
  bool isBookmarked(String messageId) => extras.bookmarked.contains(messageId);

  /// Replying happens in the thread (FEATURES.md 1.1): the pane opens
  /// with the quote above its box.
  @override
  void reply(Message m, {int? start, int? end}) {
    final parentId = m.parentId ?? m.id;
    if (threadParentId == parentId && threadKey.currentState != null) {
      threadKey.currentState!.quote(m, start: start, end: end);
      return;
    }
    setState(() {
      threadParentId = parentId;
      threadQuote = (m, start, end);
    });
  }

  @override
  void edit(Message m) {
    if (m.parentId != null && !flat) {
      openThread(m.parentId!);
      WidgetsBinding.instance.addPostFrameCallback(
        (_) => threadKey.currentState?.edit(m),
      );
      return;
    }
    composer.startEdit(m);
    composerKey.currentState?.focusBox();
  }

  @override
  void openThread(String parentId) => setState(() {
    threadParentId = parentId;
    threadQuote = null;
  });

  void closeThread() => setState(() {
    threadParentId = null;
    threadQuote = null;
  });

  @override
  void jumpTo(String messageId) {
    final key = _keys[messageId];
    if (key?.currentContext != null) {
      Scrollable.ensureVisible(
        key!.currentContext!,
        alignment: 0.5,
        duration: const Duration(milliseconds: 200),
      );
    } else {
      // Not built yet: scroll towards it by its place in the list, then
      // bring it into view once it is built.
      final i = _entryIds.indexOf(messageId);
      if (i < 0) {
        _say('That message is not loaded here.');
        return;
      }
      if (scroll.hasClients) {
        scroll.jumpTo((i * 72.0).clamp(0, scroll.position.maxScrollExtent));
      }
      WidgetsBinding.instance.addPostFrameCallback((_) {
        final k = _keys[messageId];
        if (k?.currentContext != null) {
          Scrollable.ensureVisible(k!.currentContext!, alignment: 0.5);
        }
      });
    }
    setState(() => highlighted = messageId);
    Future<void>.delayed(const Duration(seconds: 2), () {
      if (mounted && highlighted == messageId) {
        setState(() => highlighted = null);
      }
    });
  }

  @override
  void openChannel(String conversationId) =>
      AppScope.read(context)
          .openConversation(OpenTarget(server.localId, conversationId));

  void _say(String s) => ScaffoldMessenger.maybeOf(context)
    ?..hideCurrentSnackBar()
    ..showSnackBar(SnackBar(content: Text(s)));

  Future<void> _setView(String choice) async {
    try {
      switch (choice) {
        case 'threaded' || 'flat':
          await actions.setConversationThreadView(
            widget.conversationId,
            choice,
          );
        case 'default':
          await actions.setConversationThreadView(widget.conversationId, null);
        case 'default-threaded':
          await actions.setDefaultThreadView('threaded');
        case 'default-flat':
          await actions.setDefaultThreadView('flat');
      }
    } on Exception catch (e) {
      _say('Could not change the thread view. ($e)');
    }
  }

  // ---- drawing ----

  MarkdownContext _markdown(AppState app) => MarkdownContext(
    accounts: server.accounts,
    groups: server.groups,
    emoji: server.emoji,
    conversation: server.conversation,
    myId: server.me?.id,
    // A theme link of a signed-in server opens its theme card here.
    onOpenLink: (url) => openMessageLink(context, app, url),
    onOpenChannel: (id) => app.openConversation(OpenTarget(server.localId, id)),
    emojiImage: (e, size) => ServerImage(
      server: server,
      path: e.url,
      width: size,
      height: size,
      label: ':${e.name}:',
    ),
  );

  @override
  Widget build(BuildContext context) {
    final app = AppScope.of(context);
    final c = ChurrColors.of(context);
    final cls = WidthClass.of(MediaQuery.sizeOf(context).width);
    return ListenableBuilder(
      listenable: Listenable.merge([controller, extras, server]),
      builder: (context, _) {
        final conv = conversation;
        _maybeMarkRead();
        _noteArrivals();
        final markdown = _markdown(app);
        final parent = conv?.parentId == null
            ? null
            : server.conversation(conv!.parentId!);
        final title = conv == null
            ? ''
            : conversationName(conv, server.accounts, server.me?.id);
        final main = Container(
          color: c.bg,
          child: Column(
            children: [
              _Header(
                title: title,
                parentName: parent?.name,
                topic: conv?.topic ?? '',
                memberCount: conv?.memberCount ?? 0,
                private: conv?.private ?? false,
                locked: conv?.isLocked ?? false,
                named: conv?.isNamed ?? true,
                onBack: widget.onBack,
                flat: flat,
                viewOverride: conv?.membership?.threadView,
                personDefault: server.me?.threadView ?? 'threaded',
                canChooseView:
                    conv?.membership != null && !(conv?.isSealed ?? false),
                onView: _setView,
                onSettings: conv == null || conv.isSealed
                    ? null
                    : (tab) => openChannelSettings(
                        context,
                        server,
                        widget.conversationId,
                        tab: tab,
                      ),
                actions: [
                  for (final b in ConversationHeaderSlots.actions)
                    ?b(context, server, widget.conversationId),
                ],
              ),
              if (server.liveState != LiveState.ready)
                Container(
                  width: double.infinity,
                  color: c.raised,
                  padding: const EdgeInsets.symmetric(
                    horizontal: 16,
                    vertical: 4,
                  ),
                  child: Text(
                    server.liveState == LiveState.signedOut
                        ? 'Signed out. Sign in again to carry on.'
                        : server.liveState == LiveState.offline
                        ? 'Not connected. Messages you send will go when it is back.'
                        : 'Catching up…',
                    style: TextStyle(fontSize: 13, color: c.yellowInk),
                  ),
                ),
              Expanded(
                child: Stack(
                  children: [
                    Positioned.fill(child: _body(markdown)),
                    if (scrolledUp)
                      Positioned(
                        right: 16,
                        bottom: 12,
                        child: FloatingActionButton.small(
                          key: const Key('jump-newest'),
                          tooltip: 'Jump to the newest message',
                          onPressed: () => scroll.jumpTo(0),
                          child: const Glyph(ChurrIcons.arrowDown),
                        ),
                      ),
                    if (dropping)
                      Positioned.fill(
                        child: IgnorePointer(
                          // A drop target is provisional: dashed.
                          child: DashedBox(
                            color: c.lavender,
                            width: 3,
                            child: Container(
                              color: c.bg.withValues(alpha: 0.85),
                              alignment: Alignment.center,
                              child: Text(
                                'Drop files to attach them',
                                style: TextStyle(
                                  color: c.lavender,
                                  fontWeight: FontWeight.w800,
                                ),
                              ),
                            ),
                          ),
                        ),
                      ),
                  ],
                ),
              ),
              if (controller.error != null)
                Padding(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 16,
                    vertical: 4,
                  ),
                  child: Row(
                    children: [
                      Glyph(ChurrIcons.error, size: 14, color: c.pink),
                      const SizedBox(width: 6),
                      Expanded(
                        child: Text(
                          controller.error!,
                          style: TextStyle(color: c.pink, fontSize: 12),
                        ),
                      ),
                    ],
                  ),
                ),
              Composer(
                key: composerKey,
                controller: composer,
                enterSends: app.prefs.enterSends,
                hint: title.isEmpty ? 'Message' : 'Message $title',
                markdown: markdown,
                recentAuthors: [
                  for (final m in controller.messages.reversed.take(50))
                    m.authorId,
                ],
              ),
            ],
          ),
        );
        final withDrop = fileDropTarget(
          child: main,
          onHover: (v) => setState(() => dropping = v),
          onDrop: (files) => composerKey.currentState?.addDropped(files),
        );
        // The shell narrows the sidebar beside a thread pane, and the
        // back gesture closes the thread first.
        final isOpen = threadParentId != null;
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted) ThreadPaneSignal.report(isOpen, closeThread);
        });
        if (threadParentId == null) return withDrop;
        final pane = ThreadPane(
          key: threadKey,
          server: server,
          conversationId: widget.conversationId,
          parentId: threadParentId!,
          outer: this,
          markdown: markdown,
          enterSends: app.prefs.enterSends,
          initialQuote: threadQuote,
          onClose: closeThread,
        );
        // Beside the conversation on expanded and wide screens, over it
        // on narrower ones (docs/CLIENTS.md section 10).
        if (cls == WidthClass.expanded || cls == WidthClass.wide) {
          return Row(
            children: [
              Expanded(child: withDrop),
              Container(width: 1.5, color: c.line),
              SizedBox(width: cls == WidthClass.wide ? 480 : 400, child: pane),
            ],
          );
        }
        return Stack(
          children: [
            withDrop,
            Positioned.fill(child: pane),
          ],
        );
      },
    );
  }

  List<String> _entryIds = const [];

  Widget _body(MarkdownContext markdown) {
    final myId = server.me?.id;
    final body = buildBody(
      messages: controller.messages,
      states: extras.threadStates,
      flat: flat,
      conversationReadSeq: conversation?.membership?.readSeq ?? 0,
      myId: myId,
    );
    final byId = {for (final m in controller.messages) m.id: m};
    final pending = controller.pending;
    // Entries newest first, because the list is drawn from the bottom:
    // it then stays at the newest message as others arrive.
    final entries = <Widget>[];
    final ids = <String>[];
    void add(Widget w, [String id = '']) {
      entries.add(w);
      ids.add(id);
    }

    for (final p in pending.reversed) {
      if (p.parentId != null && !flat) continue;
      add(_PendingTile(item: p, controller: controller));
    }
    final messageEntries = [
      for (final e in body)
        if (e is MessageEntry) e,
    ];
    final firstNewId = readSeqAtOpen == null
        ? null
        : firstNewMessageId(
            [for (final e in messageEntries) e.message],
            readSeqAtOpen!,
            myId: myId,
          );
    for (var i = body.length - 1; i >= 0; i--) {
      final e = body[i];
      if (e is MarkerEntry) {
        add(
          MarkerLine(
            key: ValueKey('markerline-${e.parent.id}'),
            marker: e,
            host: this,
            following: extras.threadStates[e.parent.id]?.state == 'following',
          ),
        );
        continue;
      }
      final entry = e as MessageEntry;
      final m = entry.message;
      final at = messageEntries.indexOf(entry);
      final prev = at > 0 ? messageEntries[at - 1].message : null;
      final newDay = prev == null || !_sameDay(prev.createdAt, m.createdAt);
      final firstNew = m.id == firstNewId;
      // Consecutive messages by one author within five minutes share
      // one avatar and name.
      final grouped =
          prev != null &&
          !newDay &&
          !firstNew &&
          !prev.isSystem &&
          entry.reply == null &&
          prev.authorId == m.authorId &&
          (i == 0 || body[i - 1] is MessageEntry) &&
          m.createdAt.difference(prev.createdAt) < const Duration(minutes: 5);
      final key = _keys.putIfAbsent(m.id, GlobalKey.new);
      add(
        KeyedSubtree(
          key: key,
          child: ConversationMessage(
            host: this,
            message: m,
            markdown: _markdownFor(m, markdown),
            showAuthor: !grouped,
            replyHeader: entry.reply,
            quoted: m.quote == null
                ? null
                : extras.quoted(m.quote!.messageId, byId),
            highlighted: highlighted == m.id,
            showThreadLine: !flat,
          ),
        ),
        m.id,
      );
      if (firstNew) add(const _Divider.newLine());
      if (newDay) add(_Divider(label: _dayLabel(m.createdAt)));
    }
    if (controller.loadingOlder) {
      add(
        const Padding(
          padding: EdgeInsets.all(12),
          child: LinearProgressIndicator(minHeight: 2),
        ),
      );
    } else if (!controller.hasOlder && controller.messages.isNotEmpty) {
      add(
        Padding(
          padding: const EdgeInsets.all(16),
          child: Center(
            child: Text(
              'This is the start of the conversation.',
              style: monoStyle(context),
            ),
          ),
        ),
      );
    }
    _entryIds = ids;
    if (body.isEmpty && pending.isEmpty && !controller.loadingOlder) {
      return const EmptyState(
        title: 'Nothing here yet',
        body: 'Say something to start it off.',
      );
    }
    return Align(
      alignment: Alignment.topCenter,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: maxTextColumn),
        child: ListView.builder(
          key: const Key('message-list'),
          controller: scroll,
          reverse: true,
          padding: const EdgeInsets.symmetric(vertical: 8),
          itemCount: entries.length,
          itemBuilder: (context, i) => entries[i],
        ),
      ),
    );
  }

  MarkdownContext _markdownFor(Message m, MarkdownContext b) {
    final canTick = conversation?.membership != null;
    return MarkdownContext(
      accounts: b.accounts,
      groups: b.groups,
      emoji: b.emoji,
      conversation: b.conversation,
      myId: b.myId,
      onOpenLink: b.onOpenLink,
      onOpenChannel: b.onOpenChannel,
      emojiImage: b.emojiImage,
      onToggleTask: canTick
          ? (item, checked) => controller.setChecklistItem(m, item, checked)
          : null,
    );
  }
}

bool _sameDay(DateTime a, DateTime b) {
  final x = a.toLocal(), y = b.toLocal();
  return x.year == y.year && x.month == y.month && x.day == y.day;
}

String _dayLabel(DateTime t) {
  final l = t.toLocal(), now = DateTime.now();
  if (_sameDay(l, now)) return 'Today';
  if (_sameDay(l, now.subtract(const Duration(days: 1)))) return 'Yesterday';
  const months = [
    'Jan',
    'Feb',
    'Mar',
    'Apr',
    'May',
    'Jun',
    'Jul',
    'Aug',
    'Sep',
    'Oct',
    'Nov',
    'Dec',
  ];
  return '${l.day} ${months[l.month - 1]} ${l.year}';
}

/// A day's date chip, or the "new" marker (a pink line and a crooked
/// NEW tag).
class _Divider extends StatelessWidget {
  const _Divider({required this.label}) : isNew = false;
  const _Divider.newLine() : label = 'New', isNew = true;
  final String label;
  final bool isNew;

  @override
  Widget build(BuildContext context) => Semantics(
    header: true,
    label: label,
    child: Padding(
      padding: EdgeInsets.fromLTRB(16, isNew ? 10 : 14, 16, isNew ? 4 : 6),
      child: isNew ? const NewMarker() : Center(child: DateChip(label)),
    ),
  );
}

class _Header extends StatelessWidget {
  const _Header({
    required this.title,
    required this.topic,
    required this.memberCount,
    required this.private,
    required this.locked,
    required this.named,
    required this.flat,
    required this.viewOverride,
    required this.personDefault,
    required this.canChooseView,
    required this.onView,
    this.onSettings,
    this.actions = const [],
    this.parentName,
    this.onBack,
  });
  final String title;
  final String? parentName;
  final String topic;
  final int memberCount;
  final bool private;
  final bool locked;
  final bool named;
  final bool flat;
  final String? viewOverride;
  final String personDefault;
  final bool canChooseView;
  final void Function(String) onView;

  /// Opens members and settings at a tab: 0 settings, 1 members, 2 pins.
  final void Function(int tab)? onSettings;
  final List<Widget> actions;
  final VoidCallback? onBack;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Container(
      constraints: const BoxConstraints(minHeight: 52),
      // Panel over the ground of the log: a fill step, no rule.
      color: c.panel,
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
      child: Row(
        children: [
          if (onBack != null)
            IconButton(
              key: const Key('back'),
              tooltip: 'Back',
              icon: const Glyph(ChurrIcons.arrowLeft),
              onPressed: onBack,
            )
          else
            const SizedBox(width: 8),
          if (named && !private && !locked)
            Text(
              '#',
              style: monoStyle(
                context,
                size: 16,
                color: c.lavender,
                weight: FontWeight.w700,
              ),
            )
          else
            Glyph(
              !named
                  ? ChurrIcons.chat
                  : locked
                  ? ChurrIcons.locked
                  : ChurrIcons.private,
              size: 16,
              color: c.lavender,
            ),
          const SizedBox(width: 8),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Tooltip(
                  message: parentName == null ? title : '$parentName › $title',
                  child: Text.rich(
                    TextSpan(
                      children: [
                        // The header names a sub-channel as `parent › name`.
                        if (parentName != null)
                          TextSpan(
                            text: '$parentName › ',
                            style: TextStyle(color: c.dim),
                          ),
                        TextSpan(text: title),
                      ],
                    ),
                    key: const Key('conversation-title'),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: ChurrType.title(c.ink, size: 18),
                  ),
                ),
                if (topic.isNotEmpty)
                  Text(
                    topic,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: monoStyle(context, size: 10.5),
                  ),
              ],
            ),
          ),
          const SizedBox(width: 8),
          Semantics(
            button: onSettings != null,
            label: '$memberCount members',
            excludeSemantics: true,
            child: InkWell(
              key: const Key('header-members'),
              onTap: onSettings == null ? null : () => onSettings!(1),
              child: ConstrainedBox(
                constraints: const BoxConstraints(minHeight: 48, minWidth: 48),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    Glyph(ChurrIcons.people, size: 14, color: c.dim),
                    const SizedBox(width: 4),
                    Text('$memberCount', style: monoStyle(context)),
                  ],
                ),
              ),
            ),
          ),
          if (onSettings != null) ...[
            IconButton(
              key: const Key('header-pins'),
              tooltip: 'Pins',
              icon: Glyph(ChurrIcons.pin, color: c.dim),
              onPressed: () => onSettings!(2),
            ),
            IconButton(
              key: const Key('header-settings'),
              tooltip: 'Conversation settings',
              icon: Glyph(ChurrIcons.settings, color: c.dim),
              onPressed: () => onSettings!(0),
            ),
          ],
          ...actions,
          if (canChooseView)
            PopupMenuButton<String>(
              popUpAnimationStyle: AnimationStyle.noAnimation,
              key: const Key('thread-view'),
              tooltip: 'Thread view',
              icon: Glyph(
                flat ? ChurrIcons.rows : ChurrIcons.thread,
                color: c.dim,
              ),
              onSelected: onView,
              itemBuilder: (_) => [
                checkItem(
                  value: 'threaded',
                  checked: viewOverride == 'threaded',
                  child: const Text('Threaded here'),
                ),
                checkItem(
                  value: 'flat',
                  checked: viewOverride == 'flat',
                  child: const Text('Flat here'),
                ),
                checkItem(
                  value: 'default',
                  checked: viewOverride == null,
                  child: Text(
                    'My default (${personDefault == 'flat' ? 'flat' : 'threaded'})',
                  ),
                ),
                const PopupMenuDivider(),
                checkItem(
                  value: 'default-threaded',
                  checked: personDefault != 'flat',
                  child: const Text('Default everywhere: threaded'),
                ),
                checkItem(
                  value: 'default-flat',
                  checked: personDefault == 'flat',
                  child: const Text('Default everywhere: flat'),
                ),
              ],
            ),
          const SizedBox(width: 4),
        ],
      ),
    );
  }
}

/// A message written here and not yet confirmed: drawn after everything
/// that has a sequence number, marked as sending, or as failed with a
/// retry.
class _PendingTile extends StatelessWidget {
  const _PendingTile({required this.item, required this.controller});
  final OutboxItem item;
  final ConversationController controller;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final files = item.extra?['files'];
    return Padding(
      padding: const EdgeInsets.fromLTRB(62, 4, 16, 2),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (item.text.isNotEmpty)
            Opacity(opacity: 0.6, child: Text(item.text)),
          if (files is List)
            for (final f in files)
              if (f is Map)
                Opacity(
                  opacity: 0.6,
                  child: Row(
                    children: [
                      Glyph(ChurrIcons.attach, size: 14, color: c.dim),
                      Flexible(
                        child: Text(
                          '${f['name']}  ${formatBytes((f['size'] as num?)?.toInt() ?? 0)}',
                        ),
                      ),
                    ],
                  ),
                ),
          Row(
            children: [
              Glyph(
                item.failed ? ChurrIcons.error : ChurrIcons.clock,
                size: 12,
                color: item.failed ? c.pink : c.dim,
              ),
              const SizedBox(width: 4),
              Flexible(
                child: Text(
                  item.failed ? 'Not sent. ${item.error ?? ''}' : 'Sending…',
                  style: monoStyle(
                    context,
                    color: item.failed ? c.pink : c.dim,
                  ),
                ),
              ),
              if (item.failed) ...[
                TextButton(
                  onPressed: () => controller.retry(item),
                  child: const Text('Retry'),
                ),
                TextButton(
                  onPressed: () => controller.discard(item),
                  child: const Text('Discard'),
                ),
              ],
            ],
          ),
        ],
      ),
    );
  }
}
