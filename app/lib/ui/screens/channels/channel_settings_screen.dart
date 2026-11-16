import 'package:flutter/material.dart';

import '../../../core/conversation/large.dart';
import '../../../core/api/channels.dart';
import '../../../core/models/conversation.dart';
import '../../../state/app_state.dart';
import '../../../state/channel_actions.dart';
import '../../../state/channel_detail.dart';
import '../../../state/channel_settings_controller.dart';
import '../../../state/conversation_intents.dart';
import '../../../state/server_state.dart';
import '../../message/checklist.dart' show shortWhen;
import '../../scopes.dart';
import '../../theme/shapes.dart';
import '../../theme/theme.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import '../../theme/churr_icons.dart';
import '../../widgets/avatar.dart';
import '../../../l10n/app_localizations.dart';
import '../hubs/hub_screens.dart';
import 'ban_notice.dart';
import 'common.dart';
import 'new_channel_dialog.dart';
import 'reference_patterns_tab.dart';
import 'subchannel_offer.dart';

/// Opens a conversation's members and settings (docs/CLIENTS.md 9.15).
/// [tab] 0 is About and settings, 1 Members, 2 Pins.
Future<void> openChannelSettings(
  BuildContext context,
  ServerState server,
  String conversationId, {
  int tab = 0,
}) {
  final conv = server.conversation(conversationId);
  final title = conv == null
      ? L10n.of(context).channel
      : channelTitle(server, conv);
  return showScreen<void>(
    context,
    title: title,
    width: 640,
    height: 720,
    child: ChannelSettingsScreen(
      server: server,
      conversationId: conversationId,
      initialTab: tab,
    ),
  );
}

Map<String, String> _levels(L10n t) => {
  'reader': t.levelReader,
  'member': t.levelMember,
  'manager': t.levelManager,
};

Map<int, String> _slowModes(L10n t) => {
  0: t.off,
  10: t.seconds10,
  30: t.seconds30,
  60: t.minute1,
  300: t.minutes5,
  900: t.minutes15,
  3600: t.hour1,
};

Map<int, String> _retentions(L10n t) => {
  0: t.retentionServer,
  86400: t.day1,
  604800: t.days7,
  2592000: t.days30,
  7776000: t.days90,
  31536000: t.year1,
};

class ChannelSettingsScreen extends StatefulWidget {
  const ChannelSettingsScreen({
    super.key,
    required this.server,
    required this.conversationId,
    this.initialTab = 0,
  });
  final ServerState server;
  final String conversationId;
  final int initialTab;

  @override
  State<ChannelSettingsScreen> createState() => _ChannelSettingsScreenState();
}

class _ChannelSettingsScreenState extends State<ChannelSettingsScreen> {
  late final ChannelSettingsController controller = ChannelSettingsController(
    widget.server,
    widget.conversationId,
  )..start();

  @override
  void dispose() {
    controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return DefaultTabController(
      length: 4,
      initialIndex: widget.initialTab,
      child: ListenableBuilder(
        listenable: Listenable.merge([controller, widget.server]),
        builder: (context, _) {
          final conv = controller.conversation;
          if (conv == null) {
            return Center(
              child: Text(
                L10n.of(context).noLongerInConversation,
                style: TextStyle(color: c.dim),
              ),
            );
          }
          return Column(
            children: [
              TabBar(
                labelColor: c.ink,
                indicatorColor: c.lavender,
                tabs: [
                  Tab(text: L10n.of(context).tabAbout),
                  Tab(text: L10n.of(context).tabMembers(conv.memberCount)),
                  Tab(text: L10n.of(context).tabPins),
                  Tab(text: L10n.of(context).patternsTab),
                ],
              ),
              Expanded(
                child: TabBarView(
                  children: [
                    _About(controller: controller, conv: conv),
                    _Members(controller: controller, conv: conv),
                    _Pins(controller: controller),
                    ReferencePatternsTab(
                      server: widget.server,
                      conversationId: widget.conversationId,
                      manager: controller.manager,
                    ),
                  ],
                ),
              ),
            ],
          );
        },
      ),
    );
  }
}

// ---------------------------------------------------------------- About

class _About extends StatefulWidget {
  const _About({required this.controller, required this.conv});
  final ChannelSettingsController controller;
  final Conversation conv;

  @override
  State<_About> createState() => _AboutState();
}

class _AboutState extends State<_About> {
  late final name = TextEditingController(text: widget.conv.name ?? '');
  late final topic = TextEditingController(text: widget.conv.topic);
  late final welcome = TextEditingController(
    text: widget.conv.raw['welcome'] as String? ?? '',
  );
  late String defaultLevel =
      widget.conv.raw['default_level'] as String? ?? 'member';
  late int slow = (widget.conv.raw['slow_mode_seconds'] as num?)?.toInt() ?? 0;

  /// Who may use @channel, @here and @everyone here (ruling R64).
  late String broad = widget.conv.broadMentions;

  /// Announcement channel, replies in it, and lazy delivery (ruling R66).
  late bool announcement = isAnnouncement(widget.conv);
  late bool replies = announcementReplies(widget.conv);
  late String lazy = lazyDelivery(widget.conv);
  late int retention =
      (widget.conv.raw['retention_seconds'] as num?)?.toInt() ?? 0;
  late String? sectionId = widget.conv.sectionId;
  bool busy = false;
  String? error;

  ChannelSettingsController get ctl => widget.controller;
  ServerState get s => ctl.server;
  Conversation get conv => widget.conv;

  @override
  void dispose() {
    name.dispose();
    topic.dispose();
    welcome.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final changes = <String, dynamic>{};
    final n = name.text;
    if ((conv.isNamed || n.isNotEmpty) && n != (conv.name ?? '')) {
      final problem = checkChannelName(n);
      if (problem != null) {
        setState(() => error = problem);
        return;
      }
      // Names need not be unique; channels already called this are shown
      // and the person carries on if they like (9.26).
      final want = foldName(n);
      final same = [
        for (final r in s.conversations)
          if (r.conversation.id != conv.id &&
              r.conversation.isNamed &&
              foldName(r.conversation.name!) == want)
            r.conversation,
      ];
      if (same.isNotEmpty) {
        final go = await showDialog<bool>(
          animationStyle: AnimationStyle.noAnimation,
          context: context,
          builder: (ctx) => AlertDialog(
            title: Text(L10n.of(ctx).nameInUse),
            content: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(L10n.of(ctx).nameInUseNote),
                for (final x in same)
                  Padding(
                    padding: const EdgeInsets.only(top: 6),
                    child: Text(
                      '${x.name} — ${channelDetail(s, x)}',
                      style: monoStyle(ctx),
                    ),
                  ),
              ],
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(ctx, false),
                child: Text(L10n.of(ctx).back),
              ),
              FilledButton(
                onPressed: () => Navigator.pop(ctx, true),
                child: Text(L10n.of(ctx).useItAnyway),
              ),
            ],
          ),
        );
        if (go != true) return;
      }
      changes['name'] = n;
    }
    if (topic.text != conv.topic) changes['topic'] = topic.text;
    if (conv.isNamed) {
      if (welcome.text != (conv.raw['welcome'] as String? ?? '')) {
        changes['welcome'] = welcome.text;
      }
      if (sectionId != conv.sectionId && sectionId != null) {
        changes['section_id'] = sectionId;
      }
      if (retention !=
          ((conv.raw['retention_seconds'] as num?)?.toInt() ?? 0)) {
        changes['retention_seconds'] = retention;
      }
    }
    if (defaultLevel != (conv.raw['default_level'] as String? ?? 'member')) {
      changes['default_level'] = defaultLevel;
    }
    if (slow != ((conv.raw['slow_mode_seconds'] as num?)?.toInt() ?? 0)) {
      changes['slow_mode_seconds'] = slow;
    }
    if (broad != conv.broadMentions) changes['broad_mentions'] = broad;
    if (announcement != isAnnouncement(conv)) {
      changes['announcement'] = announcement;
    }
    if (announcement && replies != announcementReplies(conv)) {
      changes['announcement_replies'] = replies;
    }
    if (lazy != lazyDelivery(conv)) changes['lazy_delivery'] = lazy;
    if (changes.isEmpty) return;
    setState(() {
      busy = true;
      error = null;
    });
    final err = await ctl.run(
      () => ctl.actions.update(conv.id, changes),
      reloadMembers: false,
    );
    if (!mounted) return;
    setState(() {
      busy = false;
      error = err == null ? null : refusal(err);
    });
    if (err == null) say(context, L10n.of(context).saved);
  }

  Future<void> _parent() async {
    final choices = ctl.actions.parentChoices(conv);
    final picked = await showDialog<(bool, String?)>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (ctx) => SimpleDialog(
        title: Text(L10n.of(ctx).parentChannel),
        children: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 4),
            child: Text(L10n.of(ctx).parentChangeNote, style: monoStyle(ctx)),
          ),
          SimpleDialogOption(
            key: const Key('parent-none'),
            onPressed: () => Navigator.pop(ctx, (true, null)),
            child: Text(L10n.of(ctx).noneTopLevel),
          ),
          for (final p in choices)
            SimpleDialogOption(
              key: Key('parent-${p.id}'),
              onPressed: () => Navigator.pop(ctx, (true, p.id)),
              child: ListTile(
                dense: true,
                title: Text(p.name!),
                subtitle: Text(channelDetail(s, p), style: monoStyle(ctx)),
              ),
            ),
        ],
      ),
    );
    if (picked == null || picked.$2 == conv.parentId) return;
    final err = await ctl.run(
      () => ctl.actions.setParent(conv.id, picked.$2),
      reloadMembers: false,
    );
    if (mounted && err != null) say(context, refusal(err));
  }

  Future<bool> _typed({
    required String title,
    required List<String> lines,
    required String word,
    required String action,
    bool danger = true,
  }) async {
    final box = TextEditingController();
    final ok = await showDialog<bool>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, set) => AlertDialog(
          title: Text(title),
          content: SizedBox(
            width: (MediaQuery.sizeOf(ctx).width - 160)
                .clamp(160, 440)
                .toDouble(),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                for (final l in lines)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 8),
                    child: Text(l),
                  ),
                Text(
                  L10n.of(ctx).typeToConfirm(word),
                  style: const TextStyle(fontWeight: FontWeight.w700),
                ),
                TextField(
                  key: const Key('confirm-word'),
                  controller: box,
                  autofocus: true,
                  onChanged: (_) => set(() {}),
                ),
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: Text(L10n.of(ctx).cancel),
            ),
            FilledButton(
              key: const Key('confirm-go'),
              style: danger ? dangerButton(ChurrColors.of(ctx)) : null,
              onPressed: box.text.trim() == word
                  ? () => Navigator.pop(ctx, true)
                  : null,
              child: Text(action),
            ),
          ],
        ),
      ),
    );
    // Not disposed here: the dialog may still be animating out.
    return ok == true;
  }

  Future<void> _makePrivate() async {
    final ok = await _typed(
      title: L10n.of(context).makePrivateTitle,
      lines: [
        L10n.of(context).makePrivateLine1,
        L10n.of(context).makePrivateLine2,
      ],
      word: conv.name ?? '',
      action: L10n.of(context).makePrivate,
    );
    if (!ok) return;
    final err = await ctl.run(
      () => ctl.actions.makePrivate(conv.id),
      reloadMembers: false,
    );
    if (mounted && err != null) say(context, refusal(err));
  }

  Future<void> _lock() async {
    final ok = await _typed(
      title: L10n.of(context).lockTitle,
      lines: [
        L10n.of(context).lockLine1,
        L10n.of(context).lockLine2,
        L10n.of(context).lockLine3,
      ],
      word: L10n.of(context).lockWord,
      action: L10n.of(context).lock,
    );
    if (!ok) return;
    final err = await ctl.run(
      () => ctl.actions.lock(conv.id),
      reloadMembers: false,
    );
    if (mounted && err != null) say(context, refusal(err));
  }

  Future<void> _archive() async {
    final archived = conv.isArchived;
    final ok = await showDialog<bool>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(
          archived ? L10n.of(ctx).unarchiveTitle : L10n.of(ctx).archiveTitle,
        ),
        content: Text(
          archived ? L10n.of(ctx).unarchiveNote : L10n.of(ctx).archiveNote,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: Text(L10n.of(ctx).cancel),
          ),
          FilledButton(
            key: const Key('confirm-archive'),
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(
              archived ? L10n.of(ctx).unarchive : L10n.of(ctx).archive,
            ),
          ),
        ],
      ),
    );
    if (ok != true) return;
    final err = await ctl.run(
      () => archived
          ? ctl.actions.unarchive(conv.id)
          : ctl.actions.archive(conv.id),
      reloadMembers: false,
    );
    if (mounted && err != null) say(context, refusal(err));
  }

  Future<void> _leave() async {
    if (ctl.inThroughGroupOnly) {
      await showDialog<void>(
        animationStyle: AnimationStyle.noAnimation,
        context: context,
        builder: (ctx) => AlertDialog(
          title: Text(L10n.of(ctx).cannotLeaveTitle),
          content: Text(L10n.of(ctx).cannotLeaveNote),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx),
              child: Text(L10n.of(ctx).ok),
            ),
          ],
        ),
      );
      return;
    }
    final last = conv.memberCount <= 1 && (conv.private || conv.isAdHoc);
    final ok = await showDialog<bool>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(L10n.of(ctx).leaveTitle),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (conv.isLocked)
              Text(L10n.of(ctx).leaveLocked)
            else if (conv.private || conv.isAdHoc)
              Text(L10n.of(ctx).leavePrivate),
            if (last)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  L10n.of(ctx).leaveLast,
                  style: TextStyle(color: ChurrColors.of(ctx).pink),
                ),
              ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: Text(L10n.of(ctx).cancel),
          ),
          FilledButton(
            key: const Key('confirm-leave'),
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(L10n.of(ctx).leave),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    final app = AppScope.read(context);
    final nav = Navigator.of(context);
    final err = await ctl.run(
      () => ctl.actions.leave(conv.id),
      reloadMembers: false,
    );
    if (err != null) {
      if (mounted) say(context, refusal(err));
      return;
    }
    if (app.open == OpenTarget(s.localId, conv.id)) app.openConversation(null);
    nav.maybePop();
  }

  Future<void> _webhook() async {
    final n = TextEditingController(), purpose = TextEditingController();
    final ok = await showDialog<bool>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(L10n.of(ctx).requestWebhook),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(L10n.of(ctx).webhookNote),
            TextField(
              key: const Key('webhook-name'),
              controller: n,
              decoration: InputDecoration(
                labelText: L10n.of(ctx).channelNameLabel,
              ),
            ),
            TextField(
              controller: purpose,
              decoration: InputDecoration(
                labelText: L10n.of(ctx).webhookPurpose,
              ),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: Text(L10n.of(ctx).cancel),
          ),
          FilledButton(
            key: const Key('webhook-send'),
            onPressed: () => Navigator.pop(ctx, n.text.trim().isNotEmpty),
            child: Text(L10n.of(ctx).request),
          ),
        ],
      ),
    );
    final name = n.text.trim(), why = purpose.text.trim();
    if (ok != true) return;
    final err = await ctl.run(
      () => ctl.actions.requestWebhook(conv.id, name, why),
      reloadMembers: false,
    );
    if (!mounted) return;
    say(
      context,
      err == null
          ? L10n.of(context).webhookRequested
          : refusal(err).contains('conflict')
          ? L10n.of(context).webhookTooMany
          : refusal(err),
    );
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final mgr = ctl.manager;
    final named = conv.isNamed;
    final app = AppScope.of(context);
    final parentName = conv.parentId == null
        ? null
        : s.conversation(conv.parentId!)?.name;
    final refusalText = ctl.actions.parentRefusal(conv);
    final rows = <Widget>[
      Row(
        children: [
          Glyph(
            channelIcon(named, conv.private, conv.isLocked),
            size: 18,
            color: c.dim,
          ),
          const SizedBox(width: 6),
          Expanded(
            child: Text(
              named
                  ? channelDetail(s, conv, showServer: app.servers.length > 1)
                  : L10n.of(context).chatPeople(conv.memberCount),
              style: monoStyle(context),
            ),
          ),
        ],
      ),
      if (conv.isLocked)
        Padding(
          padding: const EdgeInsets.only(top: 4),
          child: Text(
            L10n.of(context).lockedNote,
            style: monoStyle(context, color: c.yellowInk),
          ),
        ),
      if (conv.isArchived)
        Padding(
          padding: const EdgeInsets.only(top: 4),
          child: Text(
            L10n.of(context).archivedNote,
            style: monoStyle(context, color: c.yellowInk),
          ),
        ),
      const SizedBox(height: 12),
      // A channel a hub owns: its hub, listing mode, servers and
      // moderators, and the moderators' tools.
      if (conv.shared) HubChannelSection(server: s, conversation: conv),
    ];
    if (mgr) {
      rows.addAll([
        TextField(
          key: const Key('settings-name'),
          controller: name,
          maxLength: 64,
          decoration: InputDecoration(
            labelText: named
                ? L10n.of(context).channelNameLabel
                : L10n.of(context).nameChatNote,
          ),
        ),
        TextField(
          key: const Key('settings-topic'),
          controller: topic,
          maxLength: 250,
          decoration: InputDecoration(labelText: L10n.of(context).topic),
        ),
        if (named)
          TextField(
            controller: welcome,
            minLines: 2,
            maxLines: 6,
            decoration: InputDecoration(
              labelText: L10n.of(context).welcomeText,
            ),
          ),
        _dropdown<String>(L10n.of(context).peopleJoinAs, defaultLevel, {
          'member': L10n.of(context).joinAsMembers,
          'reader': L10n.of(context).joinAsReaders,
        }, (v) => setState(() => defaultLevel = v)),
        _dropdown<int>(
          L10n.of(context).slowMode,
          _slowModes(L10n.of(context)).containsKey(slow) ? slow : 0,
          _slowModes(L10n.of(context)),
          (v) => setState(() => slow = v),
        ),
        _dropdown<String>(
          L10n.of(context).broadMentionsSetting,
          const ['server', 'anyone', 'managers'].contains(broad)
              ? broad
              : 'server',
          {
            'server': L10n.of(context).broadMentionsServer,
            'anyone': L10n.of(context).broadMentionsAnyone,
            'managers': L10n.of(context).broadMentionsManagers,
          },
          (v) => setState(() => broad = v),
          key: const Key('settings-broad-mentions'),
        ),
        Text(
          L10n.of(context).broadMentionsSettingHint,
          style: monoStyle(context),
        ),
        // A named channel that is not shared with other servers can be
        // an announcement channel.
        if (named && conv.raw['shared'] == null)
          ChurrSwitchTile(
            key: const Key('settings-announcement'),
            contentPadding: EdgeInsets.zero,
            title: Text(L10n.of(context).announcementSetting),
            subtitle: Text(L10n.of(context).announcementSettingHint),
            value: announcement,
            onChanged: (v) => setState(() => announcement = v),
          ),
        if (named && announcement)
          ChurrSwitchTile(
            key: const Key('settings-announcement-replies'),
            contentPadding: EdgeInsets.zero,
            title: Text(L10n.of(context).announcementRepliesSetting),
            subtitle: Text(L10n.of(context).announcementRepliesHint),
            value: replies,
            onChanged: (v) => setState(() => replies = v),
          ),
        if (named) ...[
          _dropdown<String>(
            L10n.of(context).lazyDeliverySetting,
            const ['server', 'on', 'off'].contains(lazy) ? lazy : 'server',
            {
              'server': L10n.of(context).lazyDeliveryServer,
              'on': L10n.of(context).lazyDeliveryOn,
              'off': L10n.of(context).lazyDeliveryOff,
            },
            (v) => setState(() => lazy = v),
            key: const Key('settings-lazy-delivery'),
          ),
          Text(L10n.of(context).lazyDeliveryHint, style: monoStyle(context)),
        ],
        if (named)
          _dropdown<int>(
            L10n.of(context).keepMessagesFor,
            _retentions(L10n.of(context)).containsKey(retention)
                ? retention
                : 0,
            _retentions(L10n.of(context)),
            (v) => setState(() => retention = v),
          ),
        if (named && conv.parentId == null)
          _dropdown<String?>(L10n.of(context).channelSection, sectionId, {
            null: L10n.of(context).none,
            for (final sec in s.sections) sec.id: sec.name,
          }, (v) => setState(() => sectionId = v)),
        if (named && conv.parentId != null)
          Text(L10n.of(context).subInParentSection, style: monoStyle(context)),
        if (error != null) Text(error!, style: TextStyle(color: c.pink)),
        Align(
          alignment: Alignment.centerRight,
          child: FilledButton(
            key: const Key('settings-save'),
            onPressed: busy ? null : _save,
            child: Text(L10n.of(context).save),
          ),
        ),
        const Divider(height: 24),
      ]);
    } else {
      rows.addAll([
        if (conv.topic.isNotEmpty) Text(conv.topic),
        if ((conv.raw['welcome'] as String? ?? '').isNotEmpty)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Text(conv.raw['welcome'] as String),
          ),
        const Divider(height: 24),
      ]);
    }
    if (ctl.actions.canSetParent(conv)) {
      rows.add(
        ListTile(
          key: const Key('settings-parent'),
          contentPadding: EdgeInsets.zero,
          leading: const Glyph(ChurrIcons.subArrow),
          title: Text(L10n.of(context).parentChannel),
          subtitle: Text(refusalText ?? (parentName ?? L10n.of(context).none)),
          onTap: refusalText == null ? _parent : null,
        ),
      );
    }
    if (named && conv.parentId == null && conv.membership != null) {
      rows.addAll([
        ListTile(
          key: const Key('settings-new-sub'),
          contentPadding: EdgeInsets.zero,
          leading: const Glyph(ChurrIcons.add),
          title: Text(L10n.of(context).newSubchannelTitle),
          onTap: () => openNewChannel(context, server: s, parentId: conv.id),
        ),
        ListTile(
          contentPadding: EdgeInsets.zero,
          leading: const Glyph(ChurrIcons.tree),
          title: Text(L10n.of(context).subchannels),
          subtitle: Text(L10n.of(context).subchannelsNote),
          onTap: () => offerSubChannels(context, s, conv, always: true),
        ),
      ]);
    }
    if (named &&
        conv.membership != null &&
        !conv.isLocked &&
        s.me?.role != 'guest') {
      rows.add(
        ListTile(
          key: const Key('settings-webhook'),
          contentPadding: EdgeInsets.zero,
          leading: const Glyph(ChurrIcons.link),
          title: Text(L10n.of(context).requestWebhook),
          onTap: _webhook,
        ),
      );
    }
    if (named &&
        !conv.shared &&
        s.me?.role == 'owner' &&
        conv.parentId == null) {
      rows.add(
        ListTile(
          key: const Key('settings-share-hub'),
          contentPadding: EdgeInsets.zero,
          leading: const Glyph(ChurrIcons.globe),
          title: Text(L10n.of(context).shareToHub),
          subtitle: Text(L10n.of(context).shareToHubNote),
          onTap: () => openShareToHub(context, s, conv),
        ),
      );
    }
    if (mgr && named && !conv.private) {
      rows.add(
        ListTile(
          key: const Key('settings-make-private'),
          contentPadding: EdgeInsets.zero,
          leading: const Glyph(ChurrIcons.private),
          title: Text(L10n.of(context).makePrivate),
          subtitle: Text(L10n.of(context).makePrivateNote),
          onTap: _makePrivate,
        ),
      );
    }
    // Lock: managers of a private channel, named or ad-hoc (R2, R44).
    if (conv.private &&
        !conv.isLocked &&
        !conv.isArchived &&
        !conv.shared &&
        conv.membership?.level == 'manager') {
      rows.add(
        ListTile(
          key: const Key('settings-lock'),
          contentPadding: EdgeInsets.zero,
          leading: const Glyph(ChurrIcons.locked),
          title: Text(L10n.of(context).lock),
          subtitle: Text(L10n.of(context).lockNote),
          onTap: _lock,
        ),
      );
    }
    if (mgr && named) {
      rows.add(
        ListTile(
          key: const Key('settings-archive'),
          contentPadding: EdgeInsets.zero,
          leading: const Glyph(ChurrIcons.archive),
          title: Text(
            conv.isArchived
                ? L10n.of(context).unarchive
                : L10n.of(context).archive,
          ),
          onTap: _archive,
        ),
      );
    }
    if (conv.membership != null && !conv.isSealed) {
      rows.add(
        ListTile(
          key: const Key('settings-leave'),
          contentPadding: EdgeInsets.zero,
          leading: Glyph(ChurrIcons.logout, color: c.pink),
          title: Text(L10n.of(context).leave, style: TextStyle(color: c.pink)),
          subtitle: ctl.inThroughGroupOnly
              ? Text(L10n.of(context).throughGroup)
              : null,
          onTap: _leave,
        ),
      );
    }
    return ListView(
      key: const Key('about-list'),
      padding: const EdgeInsets.all(16),
      children: rows,
    );
  }

  Widget _dropdown<T>(
    String label,
    T value,
    Map<T, String> items,
    ValueChanged<T> set, {
    Key? key,
  }) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 4),
    child: Row(
      children: [
        Expanded(child: Text(label)),
        Flexible(
          child: DropdownButton<T>(
            key: key,
            elevation: 0,
            icon: const Glyph(ChurrIcons.chevronDown, size: 16),
            value: value,
            isExpanded: true,
            items: [
              for (final e in items.entries)
                DropdownMenuItem<T>(
                  value: e.key,
                  child: Text(e.value, overflow: TextOverflow.ellipsis),
                ),
            ],
            onChanged: (v) {
              if (v != null || null is T) set(v as T);
            },
          ),
        ),
      ],
    ),
  );
}

// -------------------------------------------------------------- Members

class _Members extends StatelessWidget {
  const _Members({required this.controller, required this.conv});
  final ChannelSettingsController controller;
  final Conversation conv;

  ServerState get s => controller.server;

  Future<void> _addPeople(BuildContext context) async {
    final existing = {
      for (final m in controller.members ?? const <Member>[]) m.accountId,
    };
    var level = conv.raw['default_level'] as String? ?? 'member';
    await showScreen<void>(
      context,
      title: L10n.of(context).addPeople,
      width: 520,
      height: 620,
      child: StatefulBuilder(
        builder: (context, set) => Column(
          children: [
            if (conv.isNamed)
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
                child: Row(
                  children: [
                    Expanded(child: Text(L10n.of(context).addAs)),
                    Flexible(
                      child: DropdownButton<String>(
                        elevation: 0,
                        icon: const Glyph(ChurrIcons.chevronDown, size: 16),
                        isExpanded: true,
                        value: level,
                        items: [
                          for (final e in _levels(L10n.of(context)).entries)
                            DropdownMenuItem(
                              value: e.key,
                              child: Text(e.value),
                            ),
                        ],
                        onChanged: (v) => set(() => level = v ?? level),
                      ),
                    ),
                  ],
                ),
              ),
            Expanded(
              child: PeoplePicker(
                server: s,
                action: L10n.of(context).add,
                exclude: existing,
                notice: conv.isAdHoc
                    ? L10n.of(context).addedSeesHistoryChat
                    : L10n.of(context).addedSeesHistory,
                onDone: (ids) async {
                  await controller.actions.addMembers(
                    conv.id,
                    ids,
                    level: conv.isNamed ? level : null,
                  );
                  await controller.loadMembers();
                  return true;
                },
              ),
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _addGroup(BuildContext context) async {
    final there = {
      for (final g in controller.groups ?? const <ChannelGroup>[]) g.groupId,
    };
    final choices = [
      for (final g in s.groups.values)
        if (!there.contains(g.id)) g,
    ]..sort((a, b) => a.handle.compareTo(b.handle));
    String? groupId;
    var level = 'member';
    final ok = await showDialog<bool>(
      animationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, set) => AlertDialog(
          title: Text(L10n.of(ctx).addGroup),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(L10n.of(ctx).addGroupNote),
              DropdownButton<String>(
                elevation: 0,
                icon: const Glyph(ChurrIcons.chevronDown, size: 16),
                key: const Key('group-pick'),
                isExpanded: true,
                hint: Text(L10n.of(ctx).pickGroup),
                value: groupId,
                items: [
                  for (final g in choices)
                    DropdownMenuItem(value: g.id, child: Text('@${g.handle}')),
                ],
                onChanged: (v) => set(() => groupId = v),
              ),
              DropdownButton<String>(
                elevation: 0,
                icon: const Glyph(ChurrIcons.chevronDown, size: 16),
                isExpanded: true,
                value: level,
                items: [
                  for (final e in _levels(L10n.of(ctx)).entries)
                    DropdownMenuItem(value: e.key, child: Text(e.value)),
                ],
                onChanged: (v) => set(() => level = v ?? level),
              ),
            ],
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: Text(L10n.of(ctx).cancel),
            ),
            FilledButton(
              onPressed: groupId == null
                  ? null
                  : () => Navigator.pop(ctx, true),
              child: Text(L10n.of(ctx).add),
            ),
          ],
        ),
      ),
    );
    if (ok != true || groupId == null) return;
    final err = await controller.run(
      () => controller.actions.addGroup(conv.id, groupId!, level),
    );
    if (context.mounted && err != null) say(context, refusal(err));
  }

  Future<void> _memberMenu(
    BuildContext context,
    Member m,
    String action,
  ) async {
    Exception? err;
    if (action == 'remove') {
      if (m.viaGroup && m.directLevel == null) {
        say(context, L10n.of(context).removeViaGroup);
        return;
      }
      final ok = await showDialog<bool>(
        animationStyle: AnimationStyle.noAnimation,
        context: context,
        builder: (ctx) => AlertDialog(
          title: Text(
            L10n.of(ctx)
                .removeName(s.accounts[m.accountId]?.name ?? L10n.of(ctx).them),
          ),
          content: conv.isLocked ? Text(L10n.of(ctx).removeLocked) : null,
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: Text(L10n.of(ctx).cancel),
            ),
            FilledButton(
              key: const Key('confirm-remove'),
              onPressed: () => Navigator.pop(ctx, true),
              child: Text(L10n.of(ctx).remove),
            ),
          ],
        ),
      );
      if (ok != true) return;
      err = await controller.run(
        () => controller.actions.removeMember(conv.id, m.accountId),
      );
    } else if (action == 'ban') {
      if (await showBanDialog(context, s, conv.id, m.accountId)) {
        await controller.loadMembers();
        await controller.loadBans();
      }
      return;
    } else if (action == 'may-post') {
      err = await controller.run(
        () => controller.actions.setMayPost(conv.id, m.accountId, !m.mayPost),
      );
    } else {
      err = await controller.run(
        () => controller.actions.setLevel(conv.id, m.accountId, action),
      );
    }
    if (context.mounted && err != null) say(context, refusal(err));
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final mgr = controller.manager;
    final members = [...?controller.members]
      ..sort((a, b) {
        const rank = {'manager': 0, 'member': 1, 'reader': 2};
        final r = (rank[a.level] ?? 3).compareTo(rank[b.level] ?? 3);
        if (r != 0) return r;
        return foldName(s.accounts[a.accountId]?.name ?? '')
            .compareTo(foldName(s.accounts[b.accountId]?.name ?? ''));
      });
    final canAdd =
        (mgr || conv.isAdHoc) &&
        !conv.isLocked &&
        !conv.isArchived &&
        !conv.isSealed;
    // An announcement channel shows who is in it to its managers only.
    if (!showsMembers(conv, manager: mgr)) {
      return Padding(
        padding: const EdgeInsets.all(24),
        child: Text(
          L10n.of(context).membersHidden(conv.memberCount),
          key: const Key('members-hidden'),
          style: TextStyle(color: c.dim),
        ),
      );
    }
    return ListView(
      padding: const EdgeInsets.symmetric(vertical: 8),
      children: [
        if (conv.isLocked)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
            child: Text(
              L10n.of(context).membersLocked,
              key: const Key('members-locked'),
              style: monoStyle(context, color: c.yellowInk),
            ),
          ),
        if (canAdd)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 12),
            child: Wrap(
              spacing: 8,
              children: [
                OutlinedButton.icon(
                  key: const Key('members-add'),
                  icon: const Glyph(ChurrIcons.personAdd, size: 16),
                  label: Text(L10n.of(context).addPeople),
                  onPressed: () => _addPeople(context),
                ),
                if (conv.isNamed && mgr && s.me?.role != 'guest')
                  OutlinedButton.icon(
                    key: const Key('members-add-group'),
                    icon: const Glyph(ChurrIcons.peopleAdd, size: 16),
                    label: Text(L10n.of(context).addGroup),
                    onPressed: () => _addGroup(context),
                  ),
              ],
            ),
          ),
        if (controller.error != null)
          Padding(
            padding: const EdgeInsets.all(16),
            child: Text(controller.error!, style: TextStyle(color: c.pink)),
          ),
        if ((controller.groups ?? const []).isNotEmpty) ...[
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
            child: Text(
              L10n.of(context).groupsHeading,
              style: monoStyle(context, size: 10),
            ),
          ),
          for (final g in controller.groups!)
            ListTile(
              dense: true,
              leading: const Glyph(ChurrIcons.people),
              title: Text(
                '@${s.groups[g.groupId]?.handle ?? L10n.of(context).group}',
              ),
              subtitle: Text(_levels(L10n.of(context))[g.level] ?? g.level),
              trailing: mgr
                  ? PopupMenuButton<String>(
                      popUpAnimationStyle: AnimationStyle.noAnimation,
                      tooltip: L10n.of(context).groupOptions,
                      icon: const Glyph(ChurrIcons.moreV, size: 18),
                      onSelected: (v) async {
                        final err = await controller.run(
                          () => v == 'remove'
                              ? controller.actions.removeGroup(
                                  conv.id,
                                  g.groupId,
                                )
                              : controller.actions.addGroup(
                                  conv.id,
                                  g.groupId,
                                  v,
                                ),
                        );
                        if (context.mounted && err != null) {
                          say(context, refusal(err));
                        }
                      },
                      itemBuilder: (_) => [
                        for (final e in _levels(L10n.of(context)).entries)
                          if (e.key != g.level)
                            PopupMenuItem(
                              value: e.key,
                              child: Text(
                                L10n.of(context)
                                    .makeGroupLevel(e.value.toLowerCase()),
                              ),
                            ),
                        PopupMenuItem(
                          value: 'remove',
                          child: Text(L10n.of(context).removeGroup),
                        ),
                      ],
                    )
                  : null,
            ),
        ],
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
          child: Text(
            L10n.of(context).peopleHeading,
            style: monoStyle(context, size: 10),
          ),
        ),
        if (controller.members == null && controller.error == null)
          const Padding(
            padding: EdgeInsets.all(24),
            child: Center(child: Busy()),
          ),
        for (final m in members)
          ListTile(
            key: Key('member-${m.accountId}'),
            dense: true,
            leading: Avatar(
              account: s.accounts[m.accountId],
              server: s,
              size: 28,
            ),
            title: Row(
              children: [
                Flexible(
                  child: Text(
                    s.accounts[m.accountId]?.name ?? L10n.of(context).someone,
                  ),
                ),
                // A moderator of the hub that owns this channel.
                if (s.hubChannels[conv.id]?.isModerator(m.accountId, null) ??
                    false) ...[
                  const SizedBox(width: 8),
                  Tag(
                    L10n.of(context).moderatorBadge,
                    key: Key('moderator-badge-${m.accountId}'),
                    fill: c.yellow,
                    degrees: 2,
                  ),
                ],
              ],
            ),
            subtitle: Text(
              [
                _levels(L10n.of(context))[m.level] ?? m.level,
                if (isAnnouncement(conv) && m.mayPost && m.level == 'member')
                  L10n.of(context).mayPostTag,
                if (m.viaGroup)
                  m.directLevel == null
                      ? L10n.of(context).hereThroughGroup
                      : L10n.of(context).alsoThroughGroup,
              ].join(' · '),
            ),
            trailing: mgr && m.accountId != s.me?.id && !conv.isAdHoc
                ? PopupMenuButton<String>(
                    popUpAnimationStyle: AnimationStyle.noAnimation,
                    key: Key('member-menu-${m.accountId}'),
                    icon: const Glyph(ChurrIcons.moreV, size: 18),
                    tooltip: L10n.of(context).memberOptions,
                    onSelected: (v) => _memberMenu(context, m, v),
                    itemBuilder: (_) => [
                      for (final e in _levels(L10n.of(context)).entries)
                        if (e.key != (m.directLevel ?? ''))
                          PopupMenuItem(
                            value: e.key,
                            child: Text(
                              L10n.of(context).makeLevel(e.value.toLowerCase()),
                            ),
                          ),
                      // Who posts in an announcement channel besides
                      // its managers.
                      if (isAnnouncement(conv) && m.level == 'member')
                        PopupMenuItem(
                          value: 'may-post',
                          child: Text(
                            m.mayPost
                                ? L10n.of(context).mayNotPostHere
                                : L10n.of(context).mayPostHere,
                          ),
                        ),
                      PopupMenuItem(
                        value: 'remove',
                        child: Text(L10n.of(context).remove),
                      ),
                      // A channel ban (ruling R72); none in a shared
                      // channel yet.
                      if (!conv.shared)
                        PopupMenuItem(
                          key: Key('member-ban-${m.accountId}'),
                          value: 'ban',
                          child: Text(L10n.of(context).banFromChannel),
                        ),
                    ],
                  )
                : null,
          ),
        if (mgr && (controller.bans ?? const []).isNotEmpty) ...[
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
            child: Text(
              L10n.of(context).bannedHeading,
              style: monoStyle(context, size: 10),
            ),
          ),
          for (final b in controller.bans!)
            ListTile(
              key: Key('ban-${b.accountId}'),
              dense: true,
              leading: Avatar(
                account: s.accounts[b.accountId],
                server: s,
                size: 28,
              ),
              title: Text(
                s.accounts[b.accountId]?.name ?? L10n.of(context).someone,
              ),
              subtitle: Text(
                [
                  L10n.of(context).bannedBy(
                    s.accounts[b.createdById]?.name ??
                        L10n.of(context).someoneLower,
                    b.createdAt == null ? '' : shortWhen(b.createdAt!),
                  ),
                  if (b.expiresAt != null)
                    L10n.of(context).bannedUntil(shortWhen(b.expiresAt!)),
                  if (b.reason.isNotEmpty) '“${b.reason}”',
                ].join(' · '),
              ),
              trailing: TextButton(
                key: Key('lift-ban-${b.accountId}'),
                onPressed: () async {
                  final err = await controller.liftBan(b.accountId);
                  if (context.mounted) {
                    say(
                      context,
                      err == null ? L10n.of(context).banLifted : refusal(err),
                    );
                  }
                },
                child: Text(L10n.of(context).liftBan),
              ),
            ),
        ],
      ],
    );
  }
}

// ----------------------------------------------------------------- Pins

class _Pins extends StatelessWidget {
  const _Pins({required this.controller});
  final ChannelSettingsController controller;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    final pins = controller.pins;
    final s = controller.server;
    if (pins == null) return const Center(child: Busy());
    if (pins.isEmpty) {
      return Center(
        child: Text(
          L10n.of(context).nothingPinned,
          style: TextStyle(color: c.dim),
        ),
      );
    }
    return ListView.separated(
      itemCount: pins.length,
      separatorBuilder: (_, _) => const SizedBox(height: 2),
      itemBuilder: (context, i) {
        final p = pins[i];
        final m = p.message;
        final text = m == null ? '' : (m.text.isNotEmpty ? m.text : m.fallback);
        return ListTile(
          key: Key('pin-${p.messageId}'),
          title: Text(
            m == null
                ? L10n.of(context).aMessage
                : s.accounts[m.authorId]?.name ?? L10n.of(context).someone,
          ),
          subtitle: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(text, maxLines: 3, overflow: TextOverflow.ellipsis),
              Text(
                p.pinnedAt == null
                    ? L10n.of(context).pinnedBy(
                        s.accounts[p.pinnedById]?.name ??
                            L10n.of(context).someoneLower,
                      )
                    : L10n.of(context).pinnedByAt(
                        s.accounts[p.pinnedById]?.name ??
                            L10n.of(context).someoneLower,
                        shortWhen(p.pinnedAt!),
                      ),
                style: monoStyle(context),
              ),
            ],
          ),
          onTap: () {
            final app = AppScope.read(context);
            Navigator.of(context).maybePop();
            ConversationIntents.instance.openAt(
              app,
              OpenTarget(s.localId, controller.conversationId),
              p.messageId,
            );
          },
        );
      },
    );
  }
}
