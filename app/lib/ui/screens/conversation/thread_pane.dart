import 'package:flutter/material.dart';

import '../../../core/db/conversation_queries.dart';
import '../../../core/models/conversation.dart';
import '../../../core/models/message.dart';
import '../../../platform/media_files.dart';
import '../../../state/composer_controller.dart';
import '../../../state/server_state.dart';
import '../../../state/thread_controller.dart';
import '../../message/markdown_view.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import '../../theme/churr_icons.dart';
import 'composer.dart';
import 'conversation_message.dart';
import 'message_menu.dart';

/// The thread pane (docs/CLIENTS.md 9.4): the parent, then its replies
/// oldest first, each with its quote; a header with Follow or Ignore
/// and "Move to a channel"; a composer at the bottom. After a move the
/// composer gives way to the link to the new channel.
class ThreadPane extends StatefulWidget {
  const ThreadPane({
    super.key,
    required this.server,
    required this.conversationId,
    required this.parentId,
    required this.outer,
    required this.markdown,
    required this.enterSends,
    required this.onClose,
    this.initialQuote,
  });

  final ServerState server;
  final String conversationId;
  final String parentId;

  /// The conversation screen: jumps outside the thread and opening
  /// channels go to it.
  final MessageHost outer;
  final MarkdownContext markdown;
  final bool enterSends;
  final VoidCallback onClose;

  /// A reply to start, quoting this message (and range).
  final (Message, int?, int?)? initialQuote;

  @override
  State<ThreadPane> createState() => ThreadPaneState();
}

class ThreadPaneState extends State<ThreadPane> implements MessageHost {
  late final ThreadController thread;
  late final ComposerController composer;
  final composerKey = GlobalKey<ComposerState>();
  final scroll = ScrollController();
  final _keys = <String, GlobalKey>{};
  String? highlighted;

  @override
  void initState() {
    super.initState();
    thread = ThreadController(
      widget.server,
      widget.conversationId,
      widget.parentId,
    )..start();
    composer = ComposerController(
      widget.server,
      widget.conversationId,
      parentId: widget.parentId,
      posterMaker: takePosterFrame,
    )..start();
    final q = widget.initialQuote;
    if (q != null) composer.setQuote(q.$1, start: q.$2, end: q.$3);
    scroll.addListener(_maybeRead);
  }

  @override
  void dispose() {
    thread.dispose();
    composer.dispose();
    scroll.dispose();
    super.dispose();
  }

  /// Starts a reply quoting [m] (from the conversation's actions).
  void quote(Message m, {int? start, int? end}) {
    composer.setQuote(m, start: start, end: end);
    composerKey.currentState?.focusBox();
  }

  void _maybeRead() {
    if (!scroll.hasClients) return;
    if (scroll.position.pixels >= scroll.position.maxScrollExtent - 40) thread.markRead();
  }

  // ---- MessageHost ----

  @override
  ServerState get server => widget.server;
  @override
  Conversation? get conversation =>
      widget.server.conversation(widget.conversationId);
  @override
  ServerLimits get limits => composer.limits;
  @override
  bool get flat => false;
  @override
  bool get inThread => true;
  @override
  ThreadState? threadState(String parentId) => parentId == widget.parentId
      ? thread.state
      : widget.outer.threadState(parentId);
  @override
  bool isBookmarked(String messageId) => widget.outer.isBookmarked(messageId);

  @override
  void reply(Message m, {int? start, int? end}) =>
      quote(m, start: start, end: end);

  @override
  void edit(Message m) {
    composer.startEdit(m);
    composerKey.currentState?.focusBox();
  }

  @override
  void openThread(String parentId) {
    if (parentId != widget.parentId) widget.outer.openThread(parentId);
  }

  @override
  void jumpTo(String messageId) {
    final key = _keys[messageId];
    if (key?.currentContext == null) {
      widget.outer.jumpTo(messageId);
      return;
    }
    Scrollable.ensureVisible(
      key!.currentContext!,
      duration: const Duration(milliseconds: 200),
    );
    setState(() => highlighted = messageId);
    Future<void>.delayed(const Duration(seconds: 2), () {
      if (mounted && highlighted == messageId) setState(() => highlighted = null);
    });
  }

  @override
  void openChannel(String conversationId) =>
      widget.outer.openChannel(conversationId);

  // ---- drawing ----

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return ListenableBuilder(
      listenable: Listenable.merge([thread, widget.server]),
      builder: (context, _) {
        final parent = thread.parent;
        final replies = thread.replies;
        final movedTo = thread.movedToId;
        final byId = <String, Message>{
          for (final r in [?parent, ...replies]) r.id: r,
        };
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted && scroll.hasClients && scroll.position.maxScrollExtent <= 0) thread.markRead();
        });
        final c2 = conversation;
        final canMove =
            parent != null &&
            movedTo == null &&
            !(c2?.isSealed ?? true) &&
            !(c2?.shared ?? true) &&
            !(c2?.isArchived ?? true) &&
            replies.isNotEmpty &&
            (parent.authorId == server.me?.id || thread.actions.isManager(c2));
        return Container(
          key: const Key('thread-pane'),
          color: c.bg,
          child: Column(
            children: [
              _ThreadHeader(
                state: thread.followState,
                canMove: canMove,
                member: c2?.membership != null && !(c2?.isSealed ?? false),
                onFollow: thread.setFollow,
                onMove: parent == null
                    ? null
                    : () => runMessageAction(
                        context,
                        this,
                        parent,
                        MsgAction.moveThread,
                      ),
                onClose: widget.onClose,
              ),
              if (thread.error != null)
                Padding(
                  padding: const EdgeInsets.all(8),
                  child: Text(
                    thread.error!,
                    style: TextStyle(color: c.pink, fontSize: 12),
                  ),
                ),
              Expanded(
                child: parent == null
                    ? Center(
                        child: thread.loading
                            ? const Busy()
                            : Text(
                                'This thread could not be found.',
                                style: TextStyle(color: c.dim),
                              ),
                      )
                    : ListView(
                        key: const Key('thread-list'),
                        controller: scroll,
                        padding: const EdgeInsets.symmetric(vertical: 8),
                        children: [
                          _item(parent, byId, showThreadLine: false),
                          Padding(
                            padding: const EdgeInsets.fromLTRB(16, 8, 16, 4),
                            child: Row(
                              children: [
                                Text(
                                  '${replies.length} ${replies.length == 1 ? 'REPLY' : 'REPLIES'}',
                                  style: monoStyle(context, size: 10),
                                ),
                                const SizedBox(width: 8),
                                Expanded(
                                  child: Container(height: 1.5, color: c.line),
                                ),
                              ],
                            ),
                          ),
                          if (thread.loading && replies.isEmpty)
                            const Padding(
                              padding: EdgeInsets.all(12),
                              child: LinearProgressIndicator(minHeight: 2),
                            ),
                          for (var i = 0; i < replies.length; i++)
                            _item(
                              replies[i],
                              byId,
                              showAuthor:
                                  i == 0 ||
                                  replies[i - 1].authorId !=
                                      replies[i].authorId ||
                                  replies[i].quote != null ||
                                  replies[i].createdAt.difference(
                                        replies[i - 1].createdAt,
                                      ) >
                                      const Duration(minutes: 5),
                            ),
                        ],
                      ),
              ),
              if (movedTo != null)
                _MovedBar(
                  name: server.conversation(movedTo)?.name,
                  onOpen: () => openChannel(movedTo),
                )
              else
                Composer(
                  key: composerKey,
                  controller: composer,
                  enterSends: widget.enterSends,
                  hint: 'Reply in thread',
                  markdown: widget.markdown,
                  recentAuthors: [for (final r in replies) r.authorId],
                ),
            ],
          ),
        );
      },
    );
  }

  Widget _item(
    Message m,
    Map<String, Message> byId, {
    bool showAuthor = true,
    bool showThreadLine = false,
  }) {
    final key = _keys.putIfAbsent(m.id, GlobalKey.new);
    return KeyedSubtree(
      key: key,
      child: ConversationMessage(
        host: this,
        message: m,
        markdown: _markdownFor(m),
        showAuthor: showAuthor,
        quoted: m.quote == null ? null : byId[m.quote!.messageId],
        highlighted: highlighted == m.id,
        showThreadLine: showThreadLine,
      ),
    );
  }

  MarkdownContext _markdownFor(Message m) {
    final b = widget.markdown;
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
      taskDetail: b.taskDetail,
      onToggleTask: canTick
          ? (item, checked) => server.sync.setChecklistItem(m, item, checked)
          : null,
    );
  }
}

class _ThreadHeader extends StatelessWidget {
  const _ThreadHeader({
    required this.state,
    required this.canMove,
    required this.member,
    required this.onFollow,
    required this.onMove,
    required this.onClose,
  });
  final String state;
  final bool canMove;
  final bool member;
  final void Function(String) onFollow;
  final VoidCallback? onMove;
  final VoidCallback onClose;

  @override
  Widget build(BuildContext context) {
    return Container(
      constraints: const BoxConstraints(minHeight: 52),
      decoration: BoxDecoration(
      ),
      padding: const EdgeInsets.symmetric(horizontal: 8),
      child: Row(
        children: [
          IconButton(
            key: const Key('close-thread'),
            tooltip: 'Close thread',
            icon: const Glyph(ChurrIcons.close),
            onPressed: onClose,
          ),
          Expanded(
            child: Semantics(
              header: true,
              child: Text(
                'Thread',
                style: TextStyle(fontSize: 16, fontWeight: FontWeight.w800),
              ),
            ),
          ),
          if (member) ...[
            if (state == 'following')
              TextButton.icon(
                key: const Key('thread-unfollow'),
                onPressed: () => onFollow('none'),
                icon: const Glyph(ChurrIcons.notifyActive, size: 18),
                label: const Text('Following'),
              )
            else
              TextButton.icon(
                key: const Key('thread-follow'),
                onPressed: () => onFollow('following'),
                icon: const Glyph(ChurrIcons.notify, size: 18),
                label: const Text('Follow'),
              ),
            PopupMenuButton<String>(
  popUpAnimationStyle: AnimationStyle.noAnimation,
              key: const Key('thread-menu'),
              icon: const Glyph(ChurrIcons.moreV, size: 18),
              tooltip: 'Thread options',
              onSelected: (v) => v == 'move' ? onMove?.call() : onFollow(v),
              itemBuilder: (_) => [
                if (state == 'ignored')
                  const PopupMenuItem(
                    value: 'none',
                    child: Text('Stop ignoring'),
                  )
                else
                  const PopupMenuItem(
                    value: 'ignored',
                    child: Text('Ignore thread'),
                  ),
                if (canMove)
                  const PopupMenuItem(
                    value: 'move',
                    child: Text('Move to a channel…'),
                  ),
              ],
            ),
          ],
        ],
      ),
    );
  }
}

/// In place of the composer after a move (ruling R20): the old thread
/// takes no more replies.
class _MovedBar extends StatelessWidget {
  const _MovedBar({required this.name, required this.onOpen});
  final String? name;
  final VoidCallback onOpen;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Container(
      key: const Key('thread-moved'),
      decoration: BoxDecoration(
        color: c.raised,
      ),
      padding: EdgeInsets.fromLTRB(
        16,
        10,
        16,
        10 + MediaQuery.viewPaddingOf(context).bottom,
      ),
      child: Row(
        children: [
          Expanded(
            child: Text(
              'This thread was moved${name == null ? ' to its own channel' : ' to #$name'}. It takes no more replies here.',
            ),
          ),
          TextButton(onPressed: onOpen, child: const Text('Go there')),
        ],
      ),
    );
  }
}
