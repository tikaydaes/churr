import 'package:flutter/material.dart';

import '../../../core/api/account.dart';
import '../../../core/db/account_tables.dart';
import '../../../core/models/personal.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/account_hub.dart';
import '../../../state/account_prefs.dart';
import '../../../state/server_state.dart';
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import '../../theme/churr_icons.dart';
import 'common.dart';

/// Status, availability and appear away (docs/CLIENTS.md 9.11), set once
/// for every server that has not been opted out (8.6). The screen names
/// any server where the value differs, with a button to make them the
/// same.
class StatusScreen extends StatefulWidget {
  const StatusScreen({super.key});

  @override
  State<StatusScreen> createState() => _StatusScreenState();
}

class _StatusScreenState extends State<StatusScreen> {
  final emoji = TextEditingController();
  final text = TextEditingController();
  DateTime? until;
  bool pause = false;
  bool away = false;
  List<String> problems = const [];
  bool busy = false;
  final presets = <ServerState, List<StatusPreset>>{};

  AccountHub get hub => AccountHub.of(AppScope.read(context));

  @override
  void initState() {
    super.initState();
    final first =
        hub.participants().firstOrNull ??
        AppScope.read(context).servers.firstOrNull;
    final s = first?.me?.fullStatus;
    if (s != null && s.existsAt(DateTime.now())) {
      emoji.text = s.emoji ?? '';
      text.text = s.text ?? '';
      until = s.until;
      pause = s.pauseNotifications;
      away = s.appearAway;
    }
    _loadPresets();
  }

  /// Presets have no event: the list is fetched as the picker opens.
  Future<void> _loadPresets() async {
    final servers = AppScope.read(context).servers.toList();
    for (final s in servers) {
      presets[s] = await s.db.statusPresets();
    }
    if (mounted) setState(() {});
    for (final s in servers) {
      try {
        await s.db.replaceStatusPresets(await s.api.listStatusPresets());
        presets[s] = await s.db.statusPresets();
      } on Exception {
        // The cached list stays.
      }
    }
    if (mounted) setState(() {});
  }

  @override
  void dispose() {
    emoji.dispose();
    text.dispose();
    super.dispose();
  }

  Future<void> _run(Future<SharedResult> Function() action) async {
    setState(() => busy = true);
    try {
      final r = await action();
      if (mounted) problems = sharedResultLines(L10n.of(context), r);
    } on Exception catch (e) {
      if (mounted) problems = [L10n.of(context).didNotWork('$e')];
    }
    if (mounted) setState(() => busy = false);
  }

  FullStatus get _current => FullStatus(
    emoji: emoji.text.trim().isEmpty ? null : emoji.text.trim(),
    text: text.text.trim().isEmpty ? null : text.text.trim(),
    until: until,
    pauseNotifications: pause,
    appearAway: away,
  );

  Future<void> _pickClears() async {
    final t = L10n.of(context);
    final v = await pickUntil(
      context,
      title: t.statusClears,
      choices: const [
        UntilChoice.min30,
        UntilChoice.hour,
        UntilChoice.tomorrow,
        UntilChoice.week,
        UntilChoice.chosen,
        UntilChoice.forever,
      ],
    );
    if (v != null) setState(() => until = v == untilForever ? null : v);
  }

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final app = AppScope.of(context);
    final hub = AccountHub.of(app);
    return ListenableBuilder(
      listenable: Listenable.merge([hub, app.prefs]),
      builder: (context, _) {
        final optedOut = [
          for (final s in app.servers)
            if (!app.prefs.takesPartInShared(s.entry.address)) s.name,
        ];
        final differs = hub.statusDiffers();
        final first = hub.participants().firstOrNull;
        final me = first?.me;
        final availability = me?.availability;
        final shared = app.prefs.sharedPresets;
        final allPresets = [for (final l in presets.values) ...l];
        final sharedList = <StatusPreset>[];
        for (final p in allPresets) {
          if (!p.community &&
              shared.contains(p.signature) &&
              !sharedList.any((q) => q.signature == p.signature)) {
            sharedList.add(p);
          }
        }
        return ListView(
          padding: const EdgeInsets.fromLTRB(20, 8, 20, 24),
          children: [
            if (differs.isNotEmpty)
              NoteLine(
                t.differsOn(differs.map((s) => s.name).join(', ')),
                action: TextButton(
                  onPressed: () => _run(hub.alignStatus),
                  child: Text(t.makeSame),
                ),
              ),
            if (optedOut.isNotEmpty)
              NoteLine(t.optedOutNote(optedOut.join(', '))),
            SectionHead(t.statusTitle),
            Row(
              children: [
                SizedBox(
                  width: 72,
                  child: TextField(
                    key: const Key('status-emoji'),
                    controller: emoji,
                    textAlign: TextAlign.center,
                    decoration: InputDecoration(labelText: t.statusEmoji),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: TextField(
                    key: const Key('status-text'),
                    controller: text,
                    maxLength: 100,
                    decoration: InputDecoration(labelText: t.statusText),
                  ),
                ),
              ],
            ),
            Align(
              alignment: AlignmentDirectional.centerStart,
              child: TextButton.icon(
                onPressed: _pickClears,
                icon: const Glyph(ChurrIcons.clock, size: 16),
                label: Text(
                  until == null
                      ? t.statusClearsNever
                      : t.statusClearsAt(formatUntil(context, until!)),
                ),
              ),
            ),
            ChurrSwitchTile(
              contentPadding: EdgeInsets.zero,
              title: Text(t.pauseNotifications),
              value: pause,
              onChanged: (v) => setState(() => pause = v),
            ),
            ChurrSwitchTile(
              contentPadding: EdgeInsets.zero,
              title: Text(t.appearAwaySwitch),
              value: away,
              onChanged: (v) => setState(() => away = v),
            ),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                FilledButton(
                  key: const Key('set-status'),
                  onPressed: busy
                      ? null
                      : () => _run(() => hub.setStatus(_current)),
                  child: Text(t.setStatus),
                ),
                OutlinedButton(
                  key: const Key('clear-status'),
                  onPressed: busy
                      ? null
                      : () {
                          emoji.clear();
                          text.clear();
                          setState(() {
                            until = null;
                            pause = false;
                            away = false;
                          });
                          _run(hub.clearStatus);
                        },
                  child: Text(t.clearStatus),
                ),
                TextButton(
                  onPressed:
                      busy ||
                          text.text.trim().isEmpty && emoji.text.trim().isEmpty
                      ? null
                      : () => _run(() async {
                          final s = _current;
                          final r = await hub.savePreset(
                            StatusPreset(
                              id: '',
                              community: false,
                              text: s.text ?? '',
                              emoji: s.emoji,
                              durationSeconds: s.until
                                  ?.difference(DateTime.now())
                                  .inSeconds,
                              pauseNotifications: s.pauseNotifications,
                              appearAway: s.appearAway,
                            ),
                          );
                          await _loadPresets();
                          return r;
                        }),
                  child: Text(t.saveAsPreset),
                ),
              ],
            ),
            for (final p in problems) ErrorLine(p),
            SectionHead(t.presets),
            if (sharedList.isNotEmpty) ...[
              Text(t.sharedPresets, style: monoStyle(context, size: 10)),
              for (final p in sharedList)
                _PresetTile(
                  preset: p,
                  onApply: () => _run(() => hub.applyPreset(p)),
                  onDelete: () =>
                      hub.deletePreset(p).then((_) => _loadPresets()),
                ),
            ],
            for (final e in presets.entries) ...[
              if (e.value.any((p) => p.community)) ...[
                Padding(
                  padding: const EdgeInsets.only(top: 8),
                  child: Text(
                    '${t.communityPresets} · ${e.key.name}',
                    style: monoStyle(context, size: 10),
                  ),
                ),
                for (final p in e.value.where((p) => p.community))
                  _PresetTile(
                    preset: p,
                    onApply: () => _run(() => hub.applyPreset(p)),
                  ),
              ],
              if (e.value.any(
                (p) => !p.community && !shared.contains(p.signature),
              )) ...[
                Padding(
                  padding: const EdgeInsets.only(top: 8),
                  child: Text(
                    t.yourPresets(e.key.name),
                    style: monoStyle(context, size: 10),
                  ),
                ),
                for (final p in e.value.where(
                  (p) => !p.community && !shared.contains(p.signature),
                ))
                  _PresetTile(
                    preset: p,
                    onApply: () => _run(() => hub.applyPreset(p)),
                  ),
              ],
            ],
            SectionHead(t.availability),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                ChoiceChip(
                  label: Text(t.availableNow),
                  selected:
                      availability == null ||
                      !availability.activeAt(DateTime.now()),
                  onSelected: (_) => _run(hub.clearAvailability),
                ),
                for (final (value, label) in [
                  ('meeting', t.inMeeting),
                  ('out_of_office', t.outOfOffice),
                ])
                  ChoiceChip(
                    key: Key('availability-$value'),
                    label: Text(
                      availability?.value == value &&
                              availability!.until != null
                          ? '$label · ${t.untilTime(formatUntil(context, availability.until!))}'
                          : label,
                    ),
                    selected:
                        availability?.value == value &&
                        availability!.activeAt(DateTime.now()),
                    onSelected: (_) async {
                      final v = await pickUntil(
                        context,
                        title: label,
                        choices: const [
                          UntilChoice.hour,
                          UntilChoice.tomorrow,
                          UntilChoice.week,
                          UntilChoice.chosen,
                          UntilChoice.forever,
                        ],
                      );
                      if (v != null) await _run(() => hub.setAvailability(value, v == untilForever ? null : v,),);
                    },
                  ),
              ],
            ),
            ChurrSwitchTile(
              contentPadding: EdgeInsets.zero,
              title: Text(t.meetingPauses),
              value: me?.meetingPauses ?? false,
              onChanged: (v) => _run(() => hub.setMeetingPauses(v)),
            ),
            SectionHead(t.appearAway),
            Row(
              children: [
                Expanded(
                  child: Text(
                    me == null || !me.appearAwayOn
                        ? t.appearAwayOff
                        : me.appearAwayUntil == null
                        ? t.appearAwayForever
                        : t.appearAwayOn(
                            t.untilTime(
                              formatUntil(context, me.appearAwayUntil!),
                            ),
                          ),
                    style: TextStyle(color: c.dim),
                  ),
                ),
                if (me != null && me.appearAwayOn)
                  TextButton(
                    onPressed: () => _run(hub.clearAway),
                    child: Text(t.turnOff),
                  ),
                TextButton(
                  key: const Key('appear-away-on'),
                  onPressed: () async {
                    final v = await pickUntil(context, title: t.appearAway);
                    if (v != null) await _run(() => hub.setAway(v == untilForever ? null : v),);
                  },
                  child: Text(t.turnOn),
                ),
              ],
            ),
          ],
        );
      },
    );
  }
}

class _PresetTile extends StatelessWidget {
  const _PresetTile({
    required this.preset,
    required this.onApply,
    this.onDelete,
  });
  final StatusPreset preset;
  final VoidCallback onApply;
  final VoidCallback? onDelete;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    return ListTile(
      contentPadding: EdgeInsets.zero,
      dense: true,
      leading: Text(preset.emoji ?? '·', style: const TextStyle(fontSize: 20)),
      title: Text(preset.text),
      subtitle: preset.durationSeconds == null
          ? null
          : Text(
              Duration(seconds: preset.durationSeconds!)
                  .toString()
                  .split('.')
                  .first,
            ),
      onTap: onApply,
      trailing: onDelete == null
          ? null
          : IconButton(
              tooltip: t.deletePreset,
              icon: const Glyph(ChurrIcons.delete, size: 18),
              onPressed: onDelete,
            ),
    );
  }
}
