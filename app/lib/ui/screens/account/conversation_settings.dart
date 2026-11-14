import 'package:flutter/material.dart';

import '../../../core/models/conversation.dart';
import '../../../core/models/json.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/server_state.dart';
import '../../../state/sidebar_model.dart';
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import '../../theme/churr_icons.dart';
import 'common.dart';

/// Opens one conversation's notification and sidebar settings
/// (docs/CLIENTS.md 9.12, "For one conversation").
Future<void> showConversationSettings(
  BuildContext context,
  ServerState server,
  String conversationId,
) {
  final c = server.conversation(conversationId);
  final title = c == null
      ? L10n.of(context).conversationSettings
      : conversationName(c, server.accounts, server.me?.id);
  return showAccountScreen(
    context,
    title: title,
    width: 520,
    child: ConversationSettings(server: server, conversationId: conversationId),
  );
}

/// The words for a `show` value.
String showName(L10n t, ShowSetting s) => switch (s) {
  ShowSetting.always => t.showAlways,
  ShowSetting.activity => t.showActivity,
  ShowSetting.notification => t.showNotification,
  ShowSetting.hidden => t.showHidden,
};

String modeName(L10n t, NotifyMode m) => switch (m) {
  NotifyMode.any => t.modeAny,
  NotifyMode.mentions => t.modeMentions,
  NotifyMode.nothing => t.modeNothing,
};

/// The one line that puts the pair into words, for example "Mentions and
/// replies in threads you follow will notify you. It appears in the
/// sidebar only while it has something unread."
String settingsSummary(
  L10n t,
  Membership m,
  ShowSetting defaultShow, {
  required DateTime now,
  String Function(DateTime)? until,
}) {
  String modeWords(NotifyMode mode) => switch (mode) {
    NotifyMode.any => t.sumAny,
    NotifyMode.mentions => t.sumMentions,
    NotifyMode.nothing => t.sumNothing,
  };
  final muted = m.mutedAt(now);
  final mode = muted
      ? t.sumMuted(
          until?.call(m.mutedUntil!) ?? m.mutedUntil!.toLocal().toString(),
          modeWords(m.mode == NotifyMode.nothing ? NotifyMode.nothing : m.mode),
        )
      : modeWords(m.mode);
  final show = m.show ?? defaultShow;
  final showWords = switch (show) {
    ShowSetting.always => t.sumAlways,
    ShowSetting.activity => t.sumActivity,
    ShowSetting.notification => t.sumNotification,
    ShowSetting.hidden => t.sumHidden,
  };
  final notifies = !muted && m.mode != NotifyMode.nothing;
  return [
    mode,
    showWords,
    if (show == ShowSetting.hidden && notifies) t.sumHiddenNotifies,
  ].join(' ');
}

/// Changes a membership: the database first, so the screen and the
/// sidebar change at once, then the server, whose answer (and
/// `membership.updated`) confirms it. One `updateMembership` call per
/// change.
Future<void> changeMembership(
  ServerState server,
  String conversationId,
  Json changes,
) async {
  final row = await server.db.conversation(conversationId);
  final m = row?.conversation.membership;
  if (row != null && m != null) {
    final next = {...m.toJson(), ...changes}..removeWhere((_, v) => v == null);
    await server.db.upsertConversation(
      row.conversation.withMembership(Membership.fromJson(next)),
    );
  }
  try {
    await server.sync.updateMembership(conversationId, changes);
  } on Exception {
    // Put back what the server has.
    if (row != null) await server.db.upsertConversation(row.conversation);
    rethrow;
  }
}

class ConversationSettings extends StatefulWidget {
  const ConversationSettings({
    super.key,
    required this.server,
    required this.conversationId,
  });
  final ServerState server;
  final String conversationId;

  @override
  State<ConversationSettings> createState() => _ConversationSettingsState();
}

class _ConversationSettingsState extends State<ConversationSettings> {
  String? error;

  Future<void> _change(Json changes) async {
    setState(() => error = null);
    try {
      await changeMembership(widget.server, widget.conversationId, changes);
    } on Exception catch (e) {
      if (mounted) setState(() => error = L10n.of(context).didNotWork('$e'));
    }
  }

  Future<void> _mute() async {
    final until = await pickUntil(
      context,
      title: L10n.of(context).muteTitle,
      choices: const [
        UntilChoice.hour,
        UntilChoice.hours8,
        UntilChoice.day,
        UntilChoice.week,
        UntilChoice.chosen,
      ],
    );
    if (until != null) await _change({'muted_until': formatTime(until)});
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final prefs = AppScope.of(context).prefs;
    return ListenableBuilder(
      listenable: Listenable.merge([widget.server, prefs]),
      builder: (context, _) {
        final conv = widget.server.conversation(widget.conversationId);
        final m = conv?.membership;
        if (conv == null || m == null) return const SizedBox.shrink();
        final now = DateTime.now();
        final muted = m.mutedAt(now);
        final defaultView = widget.server.me?.threadView ?? 'threaded';
        // One single-choice group: each option is (value, label, key).
        Widget group<T>(
          T group,
          ValueChanged<T> on,
          List<(T, String, Key?)> options,
        ) => Column(
          children: [
            for (final (value, label, key) in options)
              ChoiceTile(
                key: key,
                contentPadding: EdgeInsets.zero,
                selected: value == group,
                onTap: () => on(value),
                title: Text(label),
              ),
          ],
        );
        final modes = [
          NotifyMode.any,
          // A sealed chat has no Mentions choice.
          if (!conv.isSealed) NotifyMode.mentions,
          NotifyMode.nothing,
        ];
        final modeColumn = Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SectionHead(t.whatNotifies),
            group<NotifyMode?>(
              muted ? null : m.mode,
              (v) {
                if (v != null) _change({'mode': v.name});
              },
              [
                for (final mode in modes)
                  (mode, modeName(t, mode), Key('mode-${mode.name}')),
              ],
            ),
            if (muted)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 4),
                child: Row(
                  children: [
                    Expanded(
                      child: Text(
                        t.mutedUntil(formatUntil(context, m.mutedUntil!)),
                        style: TextStyle(color: c.dim),
                      ),
                    ),
                    TextButton(
                      onPressed: () => _change({'muted_until': null}),
                      child: Text(t.unmute),
                    ),
                  ],
                ),
              )
            else
              Align(
                alignment: AlignmentDirectional.centerStart,
                child: TextButton.icon(
                  key: const Key('mute-for'),
                  onPressed: _mute,
                  icon: const Glyph(ChurrIcons.notifyOff, size: 16),
                  label: Text(t.muteFor),
                ),
              ),
          ],
        );
        final showColumn = Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SectionHead(t.showInSidebar),
            group<ShowSetting?>(m.show, (v) => _change({'show': v?.name}), [
              (
                null,
                t.showDefault(showName(t, prefs.defaultShow)),
                const Key('show-default'),
              ),
              for (final s in ShowSetting.values)
                (s, showName(t, s), Key('show-${s.name}')),
            ]),
          ],
        );
        return ListView(
          padding: const EdgeInsets.fromLTRB(20, 0, 20, 20),
          children: [
            LayoutBuilder(
              // The two settings sit side by side where there is room:
              // they do not constrain each other.
              builder: (context, box) => box.maxWidth >= 440
                  ? Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Expanded(child: modeColumn),
                        const SizedBox(width: 16),
                        Expanded(child: showColumn),
                      ],
                    )
                  : Column(children: [modeColumn, showColumn]),
            ),
            Container(
              key: const Key('settings-summary'),
              margin: const EdgeInsets.only(top: 12),
              padding: const EdgeInsets.all(12),
              decoration: BoxDecoration(
                color: c.raised,
              ),
              child: Text(
                settingsSummary(
                  t,
                  m,
                  prefs.defaultShow,
                  now: now,
                  until: (d) => formatUntil(context, d),
                ),
              ),
            ),
            if (error != null) ErrorLine(error!),
            ChurrSwitchTile(
              key: const Key('star-switch'),
              contentPadding: EdgeInsets.zero,
              title: Text(t.star),
              value: m.starred,
              onChanged: (v) => _change({'starred': v}),
            ),
            if (!conv.isSealed) ...[
              SectionHead(t.threadView),
              group<String?>(m.threadView, (v) => _change({'thread_view': v}), [
                (
                  null,
                  t.threadViewDefault(
                    defaultView == 'flat'
                        ? t.threadViewFlat
                        : t.threadViewThreaded,
                  ),
                  null,
                ),
                ('threaded', t.threadViewThreaded, null),
                ('flat', t.threadViewFlat, null),
              ]),
            ],
          ],
        );
      },
    );
  }
}
