import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../../../l10n/app_localizations.dart';
import '../../../state/account_hub.dart';
import '../../../state/server_state.dart';
import '../../layout/breakpoints.dart';
import '../../theme/tokens.dart';
import '../../theme/churr_icons.dart';
import '../../widgets/avatar.dart';

/// Pieces the account screens share: how a screen is placed (a page on a
/// phone, a dialog where there is room, as App settings is), a server's
/// mark, headings, the "for how long" choices and how an end time reads.

/// Shows [child] as a page on compact widths and as a dialog elsewhere
/// (docs/CLIENTS.md section 10: "Settings and profiles are dialogs").
Future<T?> showAccountScreen<T>(
  BuildContext context, {
  required String title,
  required Widget child,
  double width = 560,
}) {
  final compact =
      WidthClass.of(MediaQuery.sizeOf(context).width) == WidthClass.compact;
  if (compact) {
    return Navigator.of(context).push<T>(
      MaterialPageRoute(
        builder: (_) => Scaffold(
          appBar: AppBar(title: Text(title)),
          body: SafeArea(child: child),
        ),
      ),
    );
  }
  return showDialog<T>(
  animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (context) => Dialog(
      child: SizedBox(
        width: width,
        height: 680,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            DialogTitleBar(title: title),
            Expanded(child: child),
          ],
        ),
      ),
    ),
  );
}

/// A dialog's title row with a close button.
class DialogTitleBar extends StatelessWidget {
  const DialogTitleBar({super.key, required this.title});
  final String title;

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
      ),
      padding: const EdgeInsets.fromLTRB(20, 8, 8, 8),
      child: Row(
        children: [
          Expanded(
            child: Semantics(
              header: true,
              child: Text(
                title,
                style: const TextStyle(
                  fontSize: 18,
                  fontWeight: FontWeight.w800,
                ),
              ),
            ),
          ),
          IconButton(
            tooltip: L10n.of(context).close,
            icon: const Glyph(ChurrIcons.close),
            onPressed: () => Navigator.of(context).pop(),
          ),
        ],
      ),
    );
  }
}

/// A server's small notched mark: its initials on its fill. Rows from
/// different servers sit side by side, so each says where it is from.
class ServerMark extends StatelessWidget {
  const ServerMark({super.key, required this.server, this.size = 16});
  final ServerState server;
  final double size;

  @override
  Widget build(BuildContext context) => Tooltip(
    message: server.name,
    child: Semantics(
      label: server.name,
      excludeSemantics: true,
      child: InitialsTile(name: server.name, id: server.localId, size: size),
    ),
  );
}

/// A heading in a settings list, in the mono face.
class SectionHead extends StatelessWidget {
  const SectionHead(this.text, {super.key});
  final String text;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.fromLTRB(0, 20, 0, 8),
    child: Semantics(
      header: true,
      child: Text(
        text.toUpperCase(),
        // The guide's label: Archivo, narrow and heavy, never mono.
        style: ChurrType.label(ChurrColors.of(context).dim, size: 11.5),
      ),
    ),
  );
}

/// A line in the error colour with an icon, so it is not told by colour
/// alone.
class ErrorLine extends StatelessWidget {
  const ErrorLine(this.text, {super.key});
  final String text;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Glyph(ChurrIcons.error, size: 16, color: c.pink),
          const SizedBox(width: 6),
          Expanded(
            child: Text(text, style: TextStyle(color: c.pink)),
          ),
        ],
      ),
    );
  }
}

/// A note line in the dim colour with an icon.
class NoteLine extends StatelessWidget {
  const NoteLine(
    this.text, {
    super.key,
    this.icon = ChurrIcons.info,
    this.action,
  });
  final String text;
  final ChurrGlyph icon;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return Container(
      margin: const EdgeInsets.symmetric(vertical: 4),
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: c.raised,
      ),
      child: Row(
        children: [
          Glyph(icon, size: 16, color: c.yellowInk),
          const SizedBox(width: 8),
          Expanded(child: Text(text)),
          ?action,
        ],
      ),
    );
  }
}

/// The words for what [SharedResult] says went wrong, one line each.
List<String> sharedResultLines(L10n t, SharedResult r) => [
  for (final e in r.refused.entries) t.refusedOn(e.key, e.value),
  for (final s in r.waiting) t.waitingFor(s),
];

/// "for how long" choices. [forever] is returned for "until turned off".
enum UntilChoice { min30, hour, hours8, tomorrow, day, week, chosen, forever }

/// The sentinel for "until turned off".
final untilForever = DateTime.utc(9999);

/// Asks for how long, and returns the end time ([untilForever] for
/// "until turned off"), or null if the person backed out.
Future<DateTime?> pickUntil(
  BuildContext context, {
  required String title,
  List<UntilChoice> choices = const [
    UntilChoice.min30,
    UntilChoice.hour,
    UntilChoice.tomorrow,
    UntilChoice.chosen,
    UntilChoice.forever,
  ],
  DateTime? now,
}) async {
  final t = L10n.of(context);
  final at = now ?? DateTime.now();
  String label(UntilChoice c) => switch (c) {
    UntilChoice.min30 => t.for30Minutes,
    UntilChoice.hour => t.forAnHour,
    UntilChoice.hours8 => t.for8Hours,
    UntilChoice.tomorrow => t.untilTomorrowMorning,
    UntilChoice.day => t.forADay,
    UntilChoice.week => t.forAWeek,
    UntilChoice.chosen => t.untilChosenTime,
    UntilChoice.forever => t.untilTurnedOff,
  };
  final choice = await showDialog<UntilChoice>(
  animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (context) => SimpleDialog(
      title: Text(title),
      children: [
        for (final c in choices)
          SimpleDialogOption(
            key: Key('until-${c.name}'),
            onPressed: () => Navigator.pop(context, c),
            child: Padding(
              padding: const EdgeInsets.symmetric(vertical: 6),
              child: Text(label(c)),
            ),
          ),
      ],
    ),
  );
  if (choice == null || !context.mounted) return null;
  return switch (choice) {
    UntilChoice.min30 => at.add(const Duration(minutes: 30)),
    UntilChoice.hour => at.add(const Duration(hours: 1)),
    UntilChoice.hours8 => at.add(const Duration(hours: 8)),
    UntilChoice.day => at.add(const Duration(days: 1)),
    UntilChoice.week => at.add(const Duration(days: 7)),
    UntilChoice.tomorrow => DateTime(at.year, at.month, at.day + 1, 8),
    UntilChoice.forever => untilForever,
    UntilChoice.chosen => await pickDateTime(
      context,
      initial: at.add(const Duration(hours: 1)),
    ),
  };
}

/// A day, then a time of day.
Future<DateTime?> pickDateTime(
  BuildContext context, {
  required DateTime initial,
}) async {
  final t = L10n.of(context);
  final day = await showDatePicker(
    context: context,
    helpText: t.chooseDate,
    initialDate: initial,
    firstDate: DateTime.now(),
    lastDate: DateTime.now().add(const Duration(days: 366)),
  );
  if (day == null || !context.mounted) return null;
  final time = await showTimePicker(
    context: context,
    helpText: t.chooseTime,
    initialTime: TimeOfDay.fromDateTime(initial),
  );
  if (time == null) return null;
  return DateTime(day.year, day.month, day.day, time.hour, time.minute);
}

/// An end time as words, in the device's locale: a time today, or a day
/// and a time.
String formatUntil(BuildContext context, DateTime until, {DateTime? now}) {
  final at = (now ?? DateTime.now());
  final local = until.toLocal();
  final locale = Localizations.localeOf(context).toString();
  final sameDay =
      local.year == at.year && local.month == at.month && local.day == at.day;
  return sameDay
      ? DateFormat.jm(locale).format(local)
      : DateFormat('EEE d MMM, ', locale).add_jm().format(local);
}

/// A moment as a short time, for lists.
String formatWhen(BuildContext context, DateTime when, {DateTime? now}) {
  final at = now ?? DateTime.now();
  final local = when.toLocal();
  final locale = Localizations.localeOf(context).toString();
  if (local.year == at.year && local.month == at.month && local.day == at.day) {
    return DateFormat.jm(locale).format(local);
  }
  if (at.difference(local).inDays < 7) return DateFormat('EEE ', locale).add_jm().format(local);
  return DateFormat.yMMMd(locale).format(local);
}
