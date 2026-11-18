import 'package:flutter/material.dart';

import '../../core/api/operations.dart';
import '../../core/models/account.dart';
import '../../core/models/conversation.dart';
import '../../core/models/message.dart';
import '../../state/server_state.dart';
import '../../core/themes/theme.dart';
import '../screens/themes/theme_widgets.dart';
import '../theme/shapes.dart';
import '../theme/tokens.dart';
import '../widgets/controls.dart';
import '../theme/churr_icons.dart';
import '../widgets/avatar.dart';
import 'markdown_view.dart';
import 'media.dart';
import 'message_slots.dart';
import 'server_image.dart';
import '../types/draw.dart';
import '../../core/types/catalogue.dart';
import '../types/fair_mark.dart';

/// One message as docs/MESSAGE-FORMAT.md orders it: text, files, cards,
/// the form, and beside the content its reactions and thread summary.
///
/// Files are drawn by `media.dart` (players, gallery, text previews).
/// A reply's quote comes in through [above]; the hover and long-press
/// actions and the flat view's reply header are the conversation
/// screen's (`conversation_message.dart`). The form and the checklist
/// rules come through `message_slots.dart`.
class MessageTile extends StatelessWidget {
  const MessageTile({
    super.key,
    required this.server,
    required this.message,
    required this.markdown,
    this.showAuthor = true,
    this.onReact,
    this.onOpenThread,
    this.above,
  });

  final ServerState server;
  final Message message;
  final MarkdownContext markdown;

  /// False for a message that follows one by the same author within
  /// five minutes: they share one avatar and name.
  final bool showAuthor;
  final void Function(String emoji)? onReact;
  final VoidCallback? onOpenThread;

  /// Drawn above the content, under the name: a reply's quote
  /// (the conversation screen).
  final Widget? above;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final m = message;
    if (m.isSystem) {
      final custom = MessageSlots.system?.call(context, server, m);
      if (custom != null) return custom;
      final moved = m.system?['kind'] == 'thread_moved'
          ? m.system!['conversation_id'] as String?
          : null;
      final open = markdown.onOpenChannel;
      return _SystemLine(
        text: systemText(
          m,
          server.accounts,
          conversation: markdown.conversation,
        ),
        onTap: moved == null || open == null ? null : () => open(moved),
        // Someone put in by an owner over the channel's own rules: every
        // member is shown it, in the pink of "wants attention" (STYLE.md).
        attention: isAttentionNotice(m),
      );
    }
    final author = server.accounts[m.authorId];
    final name = m.via?.kind == 'webhook'
        ? (m.via?.name ?? author?.name)
        : author?.name;
    final view = m.isDeleted ? (card: null, newer: false) : drawView(m);
    return Semantics(
      container: true,
      child: Padding(
        padding: EdgeInsets.fromLTRB(16, showAuthor ? 10 : 2, 16, 2),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SizedBox(
              width: 34,
              child: showAuthor
                  ? Avatar(account: author, server: server, size: 34)
                  : null,
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (showAuthor)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 2),
                      child: Row(
                        children: [
                          Flexible(
                            child: Text(
                              name ?? 'Someone',
                              overflow: TextOverflow.ellipsis,
                              style: TextStyle(
                                fontWeight: FontWeight.w700,
                                fontSize: 15,
                                height: 1.2,
                                color: c.ink,
                              ),
                            ),
                          ),
                          const SizedBox(width: 10),
                          Text(
                            clockTime(m.createdAt),
                            style: monoStyle(context, size: 10, color: c.faint),
                          ),
                          if (author?.isBot ?? false) ...[
                            const SizedBox(width: 10),
                            const Tag('bot'),
                          ],
                          if (m.editedAt != null) ...[
                            const SizedBox(width: 6),
                            Text('edited', style: monoStyle(context)),
                          ],
                          if (m.notifiedAnyway) ...[
                            const SizedBox(width: 6),
                            _NotifiedMark(),
                          ],
                          if (m.pinned) ...[
                            const SizedBox(width: 6),
                            Glyph(
                              ChurrIcons.pinOn,
                              size: 12,
                              color: c.dim,
                              semanticLabel: 'Pinned',
                            ),
                          ],
                        ],
                      ),
                    ),
                  ?above,
                  if (m.isDeleted)
                    Text(
                      deletedText(m, server.accounts),
                      style: TextStyle(
                        color: c.dim,
                        fontStyle: FontStyle.italic,
                      ),
                    ),
                  if (m.sealed != null)
                    Text(
                      'Sealed message. This build cannot open sealed chats yet.',
                      style: TextStyle(
                        color: c.dim,
                        fontStyle: FontStyle.italic,
                      ),
                    ),
                  // The message as a value of the catalogue (a roll): its
                  // kind's card in place of the text, which stands when this
                  // client cannot draw it. It plays once when it arrives
                  // live, and never again.
                  if (view.card case final card?)
                    card
                  else if (m.text.isNotEmpty && m.action)
                    ActionText(
                      name: name ?? 'Someone',
                      text: m.text,
                      entities: m.entities,
                      markdown: MessageSlots.markdown(
                        context,
                        server,
                        m,
                        markdown,
                      ),
                    )
                  else if (m.text.isNotEmpty)
                    MarkdownView(
                      text: m.text,
                      entities: m.entities,
                      tasks: m.tasks,
                      context_: MessageSlots.markdown(
                        context,
                        server,
                        m,
                        markdown,
                      ),
                    ),
                  if (view.newer) const NewerNote(),
                  // The check of the message's draw, when anything in it
                  // took chance from one: the shield, from this device's own
                  // check, shown by default for the built-in chance kinds.
                  if (!m.isDeleted && m.raw['fair'] is Map && _chancy(m))
                    FairShield(server: server, message: m),
                  if (m.files.isNotEmpty)
                    MessageFiles(server: server, files: m.files),
                  // A message that shares its author's name line still
                  // shows that it was edited or notified anyway.
                  if (!showAuthor && (m.editedAt != null || m.notifiedAnyway))
                    Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        if (m.editedAt != null)
                          Text('edited', style: monoStyle(context)),
                        if (m.editedAt != null && m.notifiedAnyway)
                          const SizedBox(width: 6),
                        if (m.notifiedAnyway) _NotifiedMark(),
                      ],
                    ),
                  for (final card in m.cards)
                    if (card.theme != null)
                      ThemeCardView(
                        server: server,
                        theme: ChurrTheme.fromJson(
                          card.theme!,
                          serverAddress: server.entry.address,
                        ),
                        author: server.accounts[card.theme!['author_id']]?.name,
                      )
                    else ...[
                      _CardView(server: server, card: card, markdown: markdown),
                      // A card's shield names outcomes and no verdict.
                      if (card.fairMark != null && !m.isDeleted)
                        FairShield(
                          server: server,
                          message: m,
                          objects: card.fairMark,
                        ),
                    ],
                  if (m.form != null)
                    MessageSlots.form(context, server, m, markdown),
                  if (m.reactions.isNotEmpty)
                    _Reactions(server: server, message: m, onReact: onReact),
                  if (m.thread != null)
                    _ThreadLine(
                      server: server,
                      thread: m.thread!,
                      onOpen: onOpenThread,
                    ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// A /me message (ruling R67): the author's name, then what they are
/// doing, in italic. Text of one line runs on from the name; longer text
/// goes under it.
class ActionText extends StatelessWidget {
  const ActionText({
    super.key,
    required this.name,
    required this.text,
    required this.markdown,
    this.entities = const [],
  });
  final String name;
  final String text;
  final List<Entity> entities;
  final MarkdownContext markdown;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final base = DefaultTextStyle.of(context).style
        .copyWith(fontStyle: FontStyle.italic, color: c.ink);
    final who = TextSpan(
      text: '$name ',
      style: base.copyWith(
        fontStyle: FontStyle.normal,
        fontWeight: FontWeight.w800,
      ),
    );
    if (!text.contains('\n')) {
      return MarkdownView(
        text: text,
        entities: entities,
        context_: markdown,
        inlineOnly: true,
        style: base,
        lead: who,
      );
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text.rich(who),
        MarkdownView(
          text: text,
          entities: entities,
          context_: markdown,
          style: base,
        ),
      ],
    );
  }
}

/// `09:30`, in the device's time zone.
String clockTime(DateTime t) {
  final l = t.toLocal();
  return '${l.hour.toString().padLeft(2, '0')}:${l.minute.toString().padLeft(2, '0')}';
}

/// The wording of a message the server posted itself. Clients word these
/// themselves; an unknown kind is "Something happened here".
String systemText(
  Message m,
  Map<String, Account> accounts, {
  Conversation? Function(String id)? conversation,
}) {
  final sys = m.system ?? const {};
  final who = accounts[m.authorId]?.name ?? 'Someone';
  final by = sys['by_id'] is String ? accounts[sys['by_id']]?.name : null;
  switch (sys['kind']) {
    case 'member_joined':
      return by == null ? '$who joined' : '$who was added by $by';
    case 'member_left':
      return by == null ? '$who left' : '$by removed $who';
    case 'owner_override':
      return '$who was added by override, by ${by ?? 'an owner or an admin'}';
    case 'manager_dismissed':
      return '$who is no longer a manager, by ${by ?? 'an owner or an admin'}';
    case 'call_started':
      return '$who started a call';
    case 'call_ended':
      final secs = sys['duration_seconds'];
      if (secs is num) {
        final mins = (secs / 60).round();
        return 'The call ended after ${mins < 1 ? 'less than a minute' : '$mins ${mins == 1 ? 'minute' : 'minutes'}'}';
      }
      return 'The call ended';
    case 'thread_moved':
      final id = sys['conversation_id'] as String?;
      final name = id == null ? null : conversation?.call(id)?.name;
      return name == null
          ? '$who moved this thread to its own channel'
          : '$who moved this thread to #$name';
    case 'channel_renamed':
      final name = sys['name'] as String?;
      return name == null
          ? '$who renamed the channel'
          : '$who renamed the channel to "$name"';
    case 'pinned':
      return '$who pinned a message';
    case 'bot_token_made':
      return by == null
          ? 'A new token was made for $who. Whoever holds it reads here as $who.'
          : 'A new token was made for $who by $by. Whoever holds it reads here as $who.';
    case 'account_recovered':
      return by == null
          ? '$who’s account was recovered with a link made on the server. Whoever used it reads here as $who.'
          : '$who’s account was recovered with a link made by $by. Whoever used it reads here as $who.';
    case 'account_claimed':
      return by == null
          ? '$who’s imported account was claimed.'
          : '$who’s imported account was claimed with a link made by $by.';
    case 'group_access':
      final group = sys['name'] as String?;
      final via = group == null
          ? ' through a group'
          : ' through the group $group';
      return by == null || sys['by_id'] == m.authorId
          ? '$who added themself$via'
          : '$who was added$via, by $by';
    case 'channel_shared':
      final name = sys['name'] as String?;
      return name == null
          ? '$who shared this channel with another instance.'
          : '$who shared this channel with $name. Its people there read what is said here from now on.';
    default:
      return 'Something happened here';
  }
}

/// The system notice kinds that want attention: an owner override and the
/// access alerts (PERMISSIONS.md section 9.2), which nobody can turn off.
const attentionKinds = {
  'owner_override',
  'manager_dismissed',
  'bot_token_made',
  'account_recovered',
  'account_claimed',
  'group_access',
  'channel_shared',
};

/// Whether a notice is one that wants attention, drawn in pink rather
/// than the quiet grey of the others: an override, a manager dismissed
/// from the console (FEATURES.md 21.12) or an access alert.
bool isAttentionNotice(Message m) => attentionKinds.contains(m.system?['kind']);

/// The marker a deleted message leaves: who deleted it and when.
String deletedText(Message m, Map<String, Account> accounts) {
  final by = m.deletedById;
  final when = m.deletedAt == null ? '' : ' at ${clockTime(m.deletedAt!)}';
  if (by == null || by == m.authorId) return 'Deleted by its author$when.';
  return 'Deleted by ${accounts[by]?.name ?? 'someone'}$when.';
}

/// The notify-anyway mark: the author pushed this through someone's do
/// not disturb. It says nothing about whether anyone was reached.
class _NotifiedMark extends StatelessWidget {
  @override
  Widget build(BuildContext context) => Tooltip(
    message: 'The author used notify anyway',
    child: Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Glyph(
          ChurrIcons.notifyActive,
          size: 12,
          color: ChurrColors.of(context).dim,
        ),
        const SizedBox(width: 2),
        Text('notified anyway', style: monoStyle(context)),
      ],
    ),
  );
}

class _SystemLine extends StatelessWidget {
  const _SystemLine({required this.text, this.onTap, this.attention = false});
  final String text;

  /// Drawn in pink, bold, with a pink bar to each side: an owner
  /// override, which every member should notice.
  final bool attention;

  /// Set for a line that leads somewhere (a moved thread's new channel).
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final line = Text(
      text,
      textAlign: TextAlign.center,
      style: TextStyle(
        fontSize: 12,
        color: attention ? c.pink : (onTap == null ? c.dim : c.link),
        fontWeight: attention ? FontWeight.w700 : null,
        decoration: onTap == null ? null : TextDecoration.underline,
      ),
    );
    if (attention) {
      return Padding(
        key: const Key('notice-attention'),
        padding: const EdgeInsets.symmetric(vertical: 6, horizontal: 16),
        child: Container(
          padding: const EdgeInsets.symmetric(vertical: 4, horizontal: 10),
          decoration: BoxDecoration(
            border: Border(
              left: BorderSide(color: c.pink, width: 3),
              right: BorderSide(color: c.pink, width: 3),
            ),
          ),
          child: Center(child: line),
        ),
      );
    }
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6, horizontal: 16),
      child: Center(
        child: onTap == null
            ? line
            : Semantics(
                link: true,
                child: InkWell(onTap: onTap, child: line),
              ),
      ),
    );
  }
}

/// Whether a message holds a value of a built-in chance kind that took
/// chance from its draw, whose card shows the shield by default.
bool _chancy(Message m) {
  bool shows(Object? v) {
    if (v is! Map || v['draw'] is! Map) return false;
    final r = readValue(v);
    return r.drawn && !r.removed && catalogueKinds[r.kind]!.chance;
  }

  return shows(m.raw['view']) ||
      m.entities.any((e) => e.type == 'object' && shows(e.raw['object']));
}

/// A card: a bar of its colour down the edge, then author, title, text,
/// fields (short ones two to a row), image and footer. A card never
/// loads anything from another site: its image is a file on this server.
class _CardView extends StatelessWidget {
  const _CardView({
    required this.server,
    required this.card,
    required this.markdown,
  });
  final ServerState server;
  final MessageCard card;
  final MarkdownContext markdown;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final open = markdown.onOpenLink;
    final small = TextStyle(fontSize: 12, color: c.dim);
    // A card from a bot: one of the things that matter, so it has the
    // notch; its colour is a bar down its left edge.
    return Container(
      margin: const EdgeInsets.only(top: 6),
      constraints: const BoxConstraints(maxWidth: 520),
      child: ClipPath(
        clipper: const CutCornerClipper(8),
        child: Container(
          decoration: BoxDecoration(
            color: c.panel,
            border: Border(
              left: BorderSide(color: c.label(card.color), width: 4),
            ),
          ),
          padding: const EdgeInsets.all(12),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (card.authorName != null) Text(card.authorName!, style: small),
              if (card.title != null)
                InkWell(
                  onTap: card.url == null || open == null
                      ? null
                      : () => open(card.url!),
                  child: Text(
                    card.title!,
                    style: TextStyle(
                      fontWeight: FontWeight.w800,
                      color: card.url == null ? c.ink : c.link,
                    ),
                  ),
                ),
              if (card.text != null)
                Padding(
                  padding: const EdgeInsets.only(top: 4),
                  // Card text holds inline constructs and lists; mentions in
                  // it resolve to nothing, and a card has no tick-boxes.
                  child: MarkdownView(
                    text: card.text!,
                    context_: MarkdownContext(onOpenLink: open),
                  ),
                ),
              if (card.fields.isNotEmpty)
                Padding(
                  padding: const EdgeInsets.only(top: 6),
                  child: Wrap(
                    spacing: 16,
                    runSpacing: 6,
                    children: [
                      for (final f in card.fields)
                        SizedBox(
                          width: f.short ? 150 : 480,
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                f.label.toUpperCase(),
                                style: ChurrType.label(c.dim, size: 10),
                              ),
                              MarkdownView(
                                text: f.value,
                                inlineOnly: true,
                                context_: MarkdownContext(onOpenLink: open),
                              ),
                            ],
                          ),
                        ),
                    ],
                  ),
                ),
              if (card.imageFileId != null)
                Padding(
                  padding: const EdgeInsets.only(top: 8),
                  child: ServerImage(
                    server: server,
                    path: thumbnailPath(card.imageFileId!),
                    width: 360,
                    height: 120,
                    fit: BoxFit.contain,
                  ),
                ),
              if (card.footer != null || card.at != null)
                Padding(
                  padding: const EdgeInsets.only(top: 6),
                  child: Text(
                    [
                      ?card.footer,
                      if (card.at != null) clockTime(card.at!),
                    ].join(' · '),
                    style: small,
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}

/// The reactions under a message. The reader's own are outlined in the
/// accent; pressing one adds or takes back the reader's reaction.
class _Reactions extends StatelessWidget {
  const _Reactions({required this.server, required this.message, this.onReact});
  final ServerState server;
  final Message message;
  final void Function(String emoji)? onReact;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Padding(
      padding: const EdgeInsets.only(top: 6),
      child: Wrap(
        spacing: 6,
        runSpacing: 6,
        children: [
          for (final r in message.reactions)
            Semantics(
              button: true,
              label: '${r.emoji}, ${r.count}${r.me ? ', including you' : ''}',
              excludeSemantics: true,
              child: InkWell(
                onTap: onReact == null ? null : () => onReact!(r.emoji),
                // The reader's own: a lavender edge ("yours") and bold
                // count; others are a plain raised block.
                child: Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 8,
                    vertical: 3,
                  ),
                  decoration: BoxDecoration(
                    color: c.raised,
                    border: Border.all(
                      color: r.me ? c.lavender : c.raised,
                      width: 2,
                    ),
                  ),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      _emoji(r.emoji),
                      const SizedBox(width: 6),
                      Text(
                        '${r.count}',
                        style: monoStyle(
                          context,
                          color: c.ink,
                          weight: r.me ? FontWeight.w800 : FontWeight.w400,
                        ),
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

  /// A standard emoji is its characters; a custom one is `:name:` and
  /// is drawn as the server's image.
  Widget _emoji(String emoji) {
    if (emoji.startsWith(':') && emoji.endsWith(':') && emoji.length > 2) {
      final name = emoji.substring(1, emoji.length - 1);
      for (final e in server.emoji.values) {
        if (e.name == name) {
          return ServerImage(
            server: server,
            path: e.url,
            width: 18,
            height: 18,
            label: emoji,
          );
        }
      }
    }
    return Text(emoji, style: const TextStyle(fontSize: 15, height: 1.2));
  }
}

/// The summary line under a message with replies: how many, who, when.
class _ThreadLine extends StatelessWidget {
  const _ThreadLine({required this.server, required this.thread, this.onOpen});
  final ServerState server;
  final ThreadSummary thread;
  final VoidCallback? onOpen;

  @override
  Widget build(BuildContext context) {
    final n = thread.replyCount;
    return Padding(
      padding: const EdgeInsets.only(top: 6),
      child: Align(
        alignment: AlignmentDirectional.centerStart,
        child: ThreadSummaryChip(
          onTap: onOpen,
          leading: [
            for (final id in thread.participantIds.take(3))
              Padding(
                padding: const EdgeInsets.only(right: 4),
                child: Avatar(
                  account: server.accounts[id],
                  server: server,
                  size: 18,
                ),
              ),
            if (thread.participantIds.isNotEmpty) const SizedBox(width: 4),
          ],
          text:
              '${['$n ${n == 1 ? 'reply' : 'replies'}', if (thread.lastReplyAt != null) 'last ${clockTime(thread.lastReplyAt!)}'].join(' · ')} →',
        ),
      ),
    );
  }
}
