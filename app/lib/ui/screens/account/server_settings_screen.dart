import 'package:flutter/material.dart';

import '../sealed/sealed_devices_screen.dart';

import 'package:url_launcher/url_launcher.dart';

import '../../../core/api/account.dart';
import '../../../core/api/errors.dart';
import '../../../core/api/me.dart';
import '../../../core/models/conversation.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/account_hub.dart';
import '../../../state/account_prefs.dart';
import '../../../state/server_state.dart';
import '../../../state/sidebar_model.dart';
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import '../../theme/churr_icons.dart';
import '../hubs/hub_screens.dart';
import 'common.dart';
import 'conversation_settings.dart';
import 'dnd_screen.dart';
import 'sessions_screen.dart';
import '../conversation/add_emoji.dart';
import 'saved_replies_screen.dart';
import 'scheduled_screen.dart';
import 'watches_screen.dart';

/// Opens one server's settings (docs/CLIENTS.md 9.12, "For one server").
Future<void> showServerSettings(BuildContext context, ServerState server) =>
    showAccountScreen(
      context,
      title: L10n.of(context).serverSettingsOf(server.name),
      width: 640,
      child: ServerSettingsScreen(server: server),
    );

/// Asks, with the plain statement that people there will see the mark,
/// and mutes a whole server (8.8).
Future<void> confirmMuteServer(BuildContext context, ServerState server) async {
  final t = L10n.of(context);
  final hub = AccountHub.of(AppScope.read(context));
  final until = await pickUntil(
    context,
    title: t.muteServerTitle(server.name),
    choices: const [
      UntilChoice.hour,
      UntilChoice.hours8,
      UntilChoice.day,
      UntilChoice.week,
      UntilChoice.chosen,
    ],
  );
  if (until == null || !context.mounted) return;
  final ok = await showDialog<bool>(
    animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (context) => AlertDialog(
      title: Text(t.muteServerTitle(server.name)),
      content: Text(
        t.muteServerWarning(server.name),
        key: const Key('mute-server-warning'),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context, false),
          child: Text(t.cancel),
        ),
        FilledButton(
          key: const Key('confirm-mute-server'),
          onPressed: () => Navigator.pop(context, true),
          child: Text(t.mute),
        ),
      ],
    ),
  );
  if (ok == true) await hub.muteServer(server, until);
}

/// Asks and signs out of a server.
Future<bool> confirmSignOut(BuildContext context, ServerState server) async {
  final t = L10n.of(context);
  final app = AppScope.read(context);
  final ok = await showDialog<bool>(
    animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (context) => AlertDialog(
      content: Text(t.signOutConfirm(server.name)),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context, false),
          child: Text(t.cancel),
        ),
        FilledButton(
          key: const Key('confirm-sign-out'),
          onPressed: () => Navigator.pop(context, true),
          child: Text(t.signOut),
        ),
      ],
    ),
  );
  if (ok != true) return false;
  await app.removeServer(server.localId);
  return true;
}

class ServerSettingsScreen extends StatefulWidget {
  const ServerSettingsScreen({super.key, required this.server});
  final ServerState server;

  @override
  State<ServerSettingsScreen> createState() => _ServerSettingsScreenState();
}

class _ServerSettingsScreenState extends State<ServerSettingsScreen> {
  final selected = <String>{};
  String? error;

  ServerState get s => widget.server;

  Future<void> _run(Future<void> Function() action) async {
    setState(() => error = null);
    try {
      await action();
    } on ApiException catch (e) {
      if (mounted) {
        setState(() => error = L10n.of(context).didNotWork(e.message));
      }
    } on Exception catch (e) {
      if (mounted) setState(() => error = L10n.of(context).didNotWork('$e'));
    }
  }

  Future<void> _prefs(Map<String, dynamic> changes) => _run(() async {
    await s.api.updateMyPrefs(changes);
    await s.db.putMe(await s.api.getMe());
  });

  /// Several rows changed together: one `updateMembership` call each.
  Future<void> _bulk(Map<String, dynamic> change) => _run(() async {
    for (final id in selected) {
      await changeMembership(s, id, change);
    }
  });

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final app = AppScope.of(context);
    final hub = AccountHub.of(app);
    return ListenableBuilder(
      listenable: Listenable.merge([s, hub, app.prefs]),
      builder: (context, _) {
        final me = s.me;
        final muted = hub.isMuted(s);
        final rows =
            [
              ...s.conversations
                  .map((r) => r.conversation)
                  .where((c) => c.membership != null),
            ]..sort(
              (a, b) => conversationName(a, s.accounts, me?.id)
                  .toLowerCase()
                  .compareTo(
                    conversationName(b, s.accounts, me?.id).toLowerCase(),
                  ),
            );
        return ListView(
          padding: const EdgeInsets.fromLTRB(20, 8, 20, 24),
          children: [
            Text(s.entry.address, style: monoStyle(context)),
            SectionHead(t.defaultThreadView),
            SegmentedButton<String>(
              showSelectedIcon: false,
              segments: [
                ButtonSegment(
                  value: 'threaded',
                  label: Text(t.threadViewThreaded),
                ),
                ButtonSegment(value: 'flat', label: Text(t.threadViewFlat)),
              ],
              selected: {me?.threadView ?? 'threaded'},
              onSelectionChanged: (v) => _prefs({'thread_view': v.first}),
            ),
            ChurrSwitchTile(
              contentPadding: EdgeInsets.zero,
              title: Text(t.allowNotifyAnyway),
              value: me?.allowNotifyAnyway ?? true,
              onChanged: (v) => _prefs({'allow_notify_anyway': v}),
            ),
            ChurrSwitchTile(
              key: const Key('hide-last-seen'),
              contentPadding: EdgeInsets.zero,
              title: Text(t.hideLastSeen),
              subtitle: Text(t.hideLastSeenHint),
              value: (me?.raw['prefs'] as Map?)?['hide_last_seen'] == true,
              onChanged: (v) => _prefs({'hide_last_seen': v}),
            ),
            // Ruling R78: both off by default, so a new chat with the
            // person is a request they answer.
            ChurrSwitchTile(
              key: const Key('auto-approve-direct'),
              contentPadding: EdgeInsets.zero,
              title: Text(t.autoApproveDirect),
              subtitle: Text(t.autoApproveDirectHint),
              value: (me?.raw['prefs'] as Map?)?['auto_approve_direct'] == true,
              onChanged: (v) => _prefs({'auto_approve_direct': v}),
            ),
            ChurrSwitchTile(
              key: const Key('auto-approve-sealed'),
              contentPadding: EdgeInsets.zero,
              title: Text(t.autoApproveSealed),
              subtitle: Text(t.autoApproveSealedHint),
              value: (me?.raw['prefs'] as Map?)?['auto_approve_sealed'] == true,
              onChanged: (v) => _prefs({'auto_approve_sealed': v}),
            ),
            const SizedBox(height: 8),
            if (muted)
              Row(
                children: [
                  Expanded(
                    child: Text(
                      t.serverMutedUntil(
                        formatUntil(
                          context,
                          app.prefs.serverMutedUntil(s.entry.address)!,
                        ),
                      ),
                    ),
                  ),
                  OutlinedButton(
                    onPressed: () => _run(() => hub.unmuteServer(s)),
                    child: Text(t.unmuteServer),
                  ),
                ],
              )
            else
              Align(
                alignment: AlignmentDirectional.centerStart,
                child: OutlinedButton.icon(
                  key: const Key('mute-server'),
                  icon: const Glyph(ChurrIcons.notifyOff, size: 16),
                  onPressed: () => _run(() => confirmMuteServer(context, s)),
                  label: Text(t.muteServer),
                ),
              ),
            ChurrSwitchTile(
              key: const Key('take-part-shared'),
              contentPadding: EdgeInsets.zero,
              title: Text(t.takePartShared),
              subtitle: Text(t.takePartSharedNote),
              value: app.prefs.takesPartInShared(s.entry.address),
              onChanged: (v) =>
                  app.prefs.setTakesPartInShared(s.entry.address, v),
            ),
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: const Glyph(ChurrIcons.moon),
              title: Text(t.quietHoursLink),
              onTap: () => showAccountScreen(
                context,
                title: t.dndTitle,
                child: const DndScreen(),
              ),
            ),
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: const Glyph(ChurrIcons.devices),
              title: Text(t.sessionsAndDevices),
              onTap: () => showAccountScreen(
                context,
                title: t.sessions,
                child: SessionsScreen(server: s),
              ),
            ),
            ListTile(
              key: const Key('open-scheduled'),
              contentPadding: EdgeInsets.zero,
              leading: const Glyph(ChurrIcons.clock),
              title: Text(t.scheduledTitle),
              onTap: () => showAccountScreen(
                context,
                title: t.scheduledTitle,
                child: ScheduledScreen(server: s),
              ),
            ),
            ListTile(
              key: const Key('open-saved-replies'),
              contentPadding: EdgeInsets.zero,
              leading: const Glyph(ChurrIcons.reply),
              title: Text(t.savedRepliesTitle),
              onTap: () => showAccountScreen(
                context,
                title: t.savedRepliesTitle,
                child: SavedRepliesScreen(server: s),
              ),
            ),
            // Members add emoji where the server allows it (PERMISSIONS.md
            // note 7); otherwise the entry is not shown.
            if (mayAddEmoji(s))
              ListTile(
                key: const Key('add-emoji'),
                contentPadding: EdgeInsets.zero,
                leading: const Glyph(ChurrIcons.emoji),
                title: Text(t.addEmoji),
                subtitle: Text(t.addEmojiNote),
                onTap: () async {
                  final e = await showAddEmoji(context, s);
                  if (e != null && context.mounted) {
                    ScaffoldMessenger.maybeOf(context)
                      ?..hideCurrentSnackBar()
                      ..showSnackBar(
                        SnackBar(content: Text(t.emojiAdded(e.name))),
                      );
                  }
                },
              ),
            // Watches: "tell me when Ada is around" (ruling R76).
            ListTile(
              key: const Key('open-watches'),
              contentPadding: EdgeInsets.zero,
              leading: const Glyph(ChurrIcons.notify),
              title: Text(t.watchingTitle),
              onTap: () => showAccountScreen(
                context,
                title: t.watchingTitle,
                child: WatchesScreen(server: s),
              ),
            ),
            ListTile(
              key: const Key('open-sealed-devices'),
              contentPadding: EdgeInsets.zero,
              leading: const Glyph(ChurrIcons.sealed),
              title: Text(t.sealedChatsAndDevices),
              onTap: () => openSealedDevices(context, s),
            ),
            if (me?.role == 'owner')
              ListTile(
                key: const Key('open-hubs'),
                contentPadding: EdgeInsets.zero,
                leading: const Glyph(ChurrIcons.globe),
                title: Text(t.hubsEntry),
                subtitle: Text(t.hubsEntryNote),
                onTap: () => openHubs(context, s),
              ),
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: const Glyph(ChurrIcons.external),
              title: Text(t.accountPage),
              onTap: () => launchUrl(
                Uri.parse(s.entry.address).replace(path: '/account'),
                mode: LaunchMode.externalApplication,
              ),
            ),
            if (error != null) ErrorLine(error!),
            SectionHead(t.conversationsTable),
            if (selected.isNotEmpty)
              Wrap(
                spacing: 8,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  Text(t.selectedCount(selected.length)),
                  PopupMenuButton<String>(
                    popUpAnimationStyle: AnimationStyle.noAnimation,
                    tooltip: t.setModeFor,
                    onSelected: (v) => _bulk({'mode': v}),
                    itemBuilder: (_) => [
                      for (final m in NotifyMode.values)
                        PopupMenuItem(
                          value: m.name,
                          child: Text(modeName(t, m)),
                        ),
                    ],
                    child: Padding(
                      padding: const EdgeInsets.all(8),
                      child: Text(
                        t.setModeFor,
                        style: TextStyle(color: c.lavender),
                      ),
                    ),
                  ),
                  PopupMenuButton<String>(
                    popUpAnimationStyle: AnimationStyle.noAnimation,
                    tooltip: t.setShowFor,
                    onSelected: (v) => _bulk({'show': v.isEmpty ? null : v}),
                    itemBuilder: (_) => [
                      PopupMenuItem(
                        value: '',
                        child: Text(
                          t.showDefault(showName(t, app.prefs.defaultShow)),
                        ),
                      ),
                      for (final v in ShowSetting.values)
                        PopupMenuItem(
                          value: v.name,
                          child: Text(showName(t, v)),
                        ),
                    ],
                    child: Padding(
                      padding: const EdgeInsets.all(8),
                      child: Text(
                        t.setShowFor,
                        style: TextStyle(color: c.lavender),
                      ),
                    ),
                  ),
                ],
              ),
            // Every conversation with its two settings, hidden ones
            // included; either can be changed in place.
            for (final conv in rows)
              Container(
                key: Key('conv-row-${conv.id}'),
                decoration: BoxDecoration(),
                child: Row(
                  children: [
                    Checkbox(
                      value: selected.contains(conv.id),
                      onChanged: (v) => setState(
                        () => v!
                            ? selected.add(conv.id)
                            : selected.remove(conv.id),
                      ),
                    ),
                    // The name, then the two settings, which wrap under it
                    // on a narrow screen.
                    Expanded(
                      child: Wrap(
                        crossAxisAlignment: WrapCrossAlignment.center,
                        spacing: 8,
                        children: [
                          ConstrainedBox(
                            constraints: const BoxConstraints(
                              minWidth: 160,
                              maxWidth: 260,
                            ),
                            child: Text(
                              conversationName(conv, s.accounts, me?.id),
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                            ),
                          ),
                          DropdownButton<NotifyMode>(
                            elevation: 0,
                            icon: const Glyph(ChurrIcons.chevronDown, size: 16),
                            value: conv.membership!.mode,
                            underline: const SizedBox.shrink(),
                            items: [
                              for (final m in NotifyMode.values)
                                if (!(conv.isSealed &&
                                    m == NotifyMode.mentions))
                                  DropdownMenuItem(
                                    value: m,
                                    child: Text(modeName(t, m)),
                                  ),
                            ],
                            onChanged: (v) => _run(
                              () => changeMembership(s, conv.id, {
                                'mode': v!.name,
                              }),
                            ),
                          ),
                          // At most 220 wide, so it fits beside nothing on a phone.
                          ConstrainedBox(
                            constraints: const BoxConstraints(maxWidth: 220),
                            child: DropdownButton<ShowSetting?>(
                              elevation: 0,
                              icon: const Glyph(
                                ChurrIcons.chevronDown,
                                size: 16,
                              ),
                              isExpanded: true,
                              value: conv.membership!.show,
                              underline: const SizedBox.shrink(),
                              items: [
                                DropdownMenuItem(
                                  value: null,
                                  child: Text(
                                    t.showDefault(
                                      showName(t, app.prefs.defaultShow),
                                    ),
                                    overflow: TextOverflow.ellipsis,
                                  ),
                                ),
                                for (final v in ShowSetting.values)
                                  DropdownMenuItem(
                                    value: v,
                                    child: Text(
                                      showName(t, v),
                                      overflow: TextOverflow.ellipsis,
                                    ),
                                  ),
                              ],
                              onChanged: (v) => _run(
                                () => changeMembership(s, conv.id, {
                                  'show': v?.name,
                                }),
                              ),
                            ),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            const SizedBox(height: 24),
            Align(
              alignment: AlignmentDirectional.centerStart,
              child: OutlinedButton(
                key: const Key('server-sign-out'),
                style: OutlinedButton.styleFrom(foregroundColor: c.pink),
                onPressed: () async {
                  final nav = Navigator.of(context);
                  if (await confirmSignOut(context, s) && nav.canPop()) {
                    nav.pop();
                  }
                },
                child: Text(t.signOut),
              ),
            ),
          ],
        );
      },
    );
  }
}
