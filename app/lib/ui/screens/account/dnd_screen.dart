import 'package:flutter/material.dart';

import '../../../core/models/personal.dart';
import '../../../l10n/app_localizations.dart';
import '../../../state/account_hub.dart';
import '../../../state/server_state.dart';
import '../../scopes.dart';
import '../../theme/tokens.dart';
import '../../widgets/controls.dart';
import 'common.dart';

/// Do not disturb and quiet hours (docs/CLIENTS.md 9.13). Shows whether
/// the person is in do not disturb now, on which servers, why and until
/// when; turns it on for a while or until turned off, and off, on every
/// server taking part (8.6). Quiet hours are per server (8.7).
class DndScreen extends StatefulWidget {
  const DndScreen({super.key});

  @override
  State<DndScreen> createState() => _DndScreenState();
}

class _DndScreenState extends State<DndScreen> {
  List<String> problems = const [];

  Future<void> _run(Future<Object?> Function() action) async {
    try {
      final r = await action();
      if (!mounted) return;
      setState(() {
        problems = switch (r) {
          SharedResult x => sharedResultLines(L10n.of(context), x),
          Map<String, String> m => [
            for (final e in m.entries)
              L10n.of(context).refusedOn(e.key, e.value),
          ],
          _ => const [],
        };
      });
    } on Exception catch (e) {
      if (mounted) setState(() => problems = [L10n.of(context).didNotWork('$e')]);
    }
  }

  String _why(L10n t, String source) => switch (source) {
    'manual' => t.dndWhyManual,
    'status' => t.dndWhyStatus,
    'meeting' => t.dndWhyMeeting,
    'quiet' => t.dndWhyQuiet,
    _ => source,
  };

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final app = AppScope.of(context);
    final hub = AccountHub.of(app);
    return ListenableBuilder(
      listenable: Listenable.merge([hub, app.prefs]),
      builder: (context, _) {
        final on = [
          for (final s in app.servers)
            if (s.me?.dndOn ?? false) s,
        ];
        final sources = {
          for (final s in hub.participants(forDnd: true))
            ...?s.me?.dndState.sources,
        };
        final differs = hub.dndDiffers();
        return ListView(
          padding: const EdgeInsets.fromLTRB(20, 8, 20, 24),
          children: [
            Container(
              key: const Key('dnd-now'),
              padding: const EdgeInsets.all(12),
              decoration: BoxDecoration(
                color: c.panel,
              ),
              child: on.isEmpty
                  ? Text(t.dndOff)
                  : Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          t.dndOnServers(on.map((s) => s.name).join(', ')),
                          style: const TextStyle(fontWeight: FontWeight.w800),
                        ),
                        for (final s in on)
                          Padding(
                            padding: const EdgeInsets.only(top: 4),
                            child: Row(
                              children: [
                                ServerMark(server: s),
                                const SizedBox(width: 8),
                                Expanded(
                                  child: Text(
                                    [
                                      if (hub.isMuted(s)) t.dndWhyMuted,
                                      for (final src in s.me!.dndState.sources)
                                        _why(t, src),
                                      if (s.me!.dndState.until != null)
                                        t.untilTime(
                                          formatUntil(
                                            context,
                                            s.me!.dndState.until!,
                                          ),
                                        ),
                                    ].join(' · '),
                                    style: TextStyle(color: c.dim),
                                  ),
                                ),
                              ],
                            ),
                          ),
                      ],
                    ),
            ),
            if (differs.isNotEmpty)
              NoteLine(
                t.differsOn(differs.map((s) => s.name).join(', ')),
                action: TextButton(
                  onPressed: () => _run(hub.alignDnd),
                  child: Text(t.makeSame),
                ),
              ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                FilledButton(
                  key: const Key('dnd-turn-on'),
                  onPressed: () async {
                    final v = await pickUntil(
                      context,
                      title: t.dndTurnOnTitle,
                      choices: const [
                        UntilChoice.min30,
                        UntilChoice.hour,
                        UntilChoice.tomorrow,
                        UntilChoice.chosen,
                        UntilChoice.forever,
                      ],
                    );
                    if (v != null) await _run(() => hub.setDnd(v == untilForever ? null : v),);
                  },
                  child: Text(t.turnOn),
                ),
                OutlinedButton(
                  key: const Key('dnd-turn-off'),
                  onPressed: on.isEmpty ? null : () => _run(hub.clearDnd),
                  child: Text(t.turnOff),
                ),
              ],
            ),
            if (sources.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  t.dndTurnOffClears(sources.map((s) => _why(t, s)).join(', ')),
                  style: TextStyle(color: c.dim),
                ),
              ),
            for (final p in problems) ErrorLine(p),
            SectionHead(t.quietHours),
            for (final s in app.servers)
              _QuietHoursCard(
                key: ValueKey('quiet-${s.localId}'),
                server: s,
                onRun: _run,
              ),
          ],
        );
      },
    );
  }
}

/// One server's quiet hours: a switch, start and end, the days, the time
/// zone (kept current from the device), "Skip tonight" and "Copy to…".
class _QuietHoursCard extends StatelessWidget {
  const _QuietHoursCard({super.key, required this.server, required this.onRun});
  final ServerState server;
  final Future<void> Function(Future<Object?> Function()) onRun;

  @override
  Widget build(BuildContext context) {
    final t = L10n.of(context);
    final c = ChurrColors.of(context);
    final app = AppScope.of(context);
    final hub = AccountHub.of(app);
    final q = server.me?.quietHours;
    final kept = hub.prefs.raw('old_quiet_hours') == null ? null : _old(hub);
    final shown =
        q ??
        kept ??
        const QuietHours(start: '22:00', end: '07:00', days: '1111111');
    final days = [
      t.dayMon,
      t.dayTue,
      t.dayWed,
      t.dayThu,
      t.dayFri,
      t.daySat,
      t.daySun,
    ];
    Future<void> save(QuietHours next) =>
        onRun(() => hub.setQuietHours(server, next));
    Future<String?> pickTime(String hhmm) async {
      final parts = hhmm.split(':');
      final v = await showTimePicker(
        context: context,
        initialTime: TimeOfDay(
          hour: int.parse(parts[0]),
          minute: int.parse(parts[1]),
        ),
      );
      if (v == null) return null;
      return '${v.hour.toString().padLeft(2, '0')}:${v.minute.toString().padLeft(2, '0')}';
    }

    return Container(
      margin: const EdgeInsets.only(bottom: 10),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(color: c.raised),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              ServerMark(server: server),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  t.quietOn(server.name),
                  style: const TextStyle(fontWeight: FontWeight.w800),
                ),
              ),
              ChurrSwitch(
                key: Key('quiet-switch-${server.localId}'),
                value: q != null,
                onChanged: (v) =>
                    v ? save(shown) : onRun(() => hub.removeQuietHours(server)),
              ),
            ],
          ),
          if (q != null) ...[
            Wrap(
              spacing: 8,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                TextButton(
                  onPressed: () async {
                    final v = await pickTime(q.start);
                    if (v != null && v != q.end) await save(QuietHours(start: v, end: q.end, days: q.days),);
                  },
                  child: Text('${t.quietFrom} ${q.start}'),
                ),
                TextButton(
                  onPressed: () async {
                    final v = await pickTime(q.end);
                    if (v != null && v != q.start) await save(QuietHours(start: q.start, end: v, days: q.days),);
                  },
                  child: Text('${t.quietTo} ${q.end}'),
                ),
              ],
            ),
            Wrap(
              spacing: 4,
              runSpacing: 4,
              children: [
                for (var i = 0; i < 7; i++)
                  FilterChip(
                    label: Text(days[i]),
                    selected: q.days[i] == '1',
                    onSelected: (v) {
                      final d = q.days.split('');
                      d[i] = v ? '1' : '0';
                      save(
                        QuietHours(start: q.start, end: q.end, days: d.join()),
                      );
                    },
                  ),
              ],
            ),
            if (q.timeZone.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Text(
                  t.quietZone(q.timeZone),
                  style: monoStyle(context, size: 10),
                ),
              ),
            Wrap(
              spacing: 8,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                if (q.skipUntil != null &&
                    q.skipUntil!.isAfter(DateTime.now())) ...[
                  Text(
                    t.skipped(formatUntil(context, q.skipUntil!)),
                    style: TextStyle(color: c.dim),
                  ),
                  TextButton(
                    onPressed: () =>
                        onRun(() => hub.skipQuietHours(server, skip: false)),
                    child: Text(t.unskip),
                  ),
                ] else
                  TextButton(
                    onPressed: () => onRun(() => hub.skipQuietHours(server)),
                    child: Text(t.skipTonight),
                  ),
                if (app.servers.length > 1)
                  TextButton(
                    onPressed: () async {
                      final to = await _chooseServers(
                        context,
                        app.servers.where((s) => s != server).toList(),
                      );
                      if (to != null && to.isNotEmpty) await onRun(() => hub.copyQuietHours(q, to));
                    },
                    child: Text(t.copyTo),
                  ),
              ],
            ),
          ],
        ],
      ),
    );
  }

  QuietHours? _old(AccountHub hub) {
    final raw = (hub.prefs.raw('old_quiet_hours') ?? '').isEmpty
        ? null
        : hub.prefs.raw('old_quiet_hours');
    if (raw == null) return null;
    final m = RegExp('"${RegExp.escape(server.entry.address)}":"([^"]+)"')
        .firstMatch(raw);
    if (m == null) return null;
    final p = m.group(1)!.split('|');
    return p.length == 3
        ? QuietHours(start: p[0], end: p[1], days: p[2])
        : null;
  }
}

/// Asks which of [servers] to apply something to; all ticked to begin.
Future<List<ServerState>?> _chooseServers(
  BuildContext context,
  List<ServerState> servers,
) {
  final t = L10n.of(context);
  final ticked = {...servers};
  return showDialog<List<ServerState>>(
    context: context,
    builder: (context) => StatefulBuilder(
      builder: (context, setState) => AlertDialog(
        title: Text(t.copyToTitle),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            for (final s in servers)
              CheckboxListTile(
                value: ticked.contains(s),
                title: Text(s.name),
                onChanged: (v) =>
                    setState(() => v! ? ticked.add(s) : ticked.remove(s)),
              ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: Text(t.cancel),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, ticked.toList()),
            child: Text(t.copy),
          ),
        ],
      ),
    ),
  );
}
