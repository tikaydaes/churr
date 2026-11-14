import 'package:flutter/material.dart';

import '../../layout/breakpoints.dart';
import '../../theme/tokens.dart';
import '../../theme/churr_icons.dart';

/// The suggested times of "Send later" (docs/CLIENTS.md 9.5), worked out
/// from [now]: in an hour, this evening at 18:00 (while it is still
/// ahead), tomorrow at 09:00, and next Monday at 09:00.
List<(String, DateTime)> suggestedTimes(DateTime now) {
  final out = <(String, DateTime)>[
    ('In 1 hour', now.add(const Duration(hours: 1))),
  ];
  final evening = DateTime(now.year, now.month, now.day, 18);
  if (evening.difference(now) > const Duration(minutes: 90)) out.add(('This evening, 18:00', evening));
  final tomorrow = DateTime(now.year, now.month, now.day + 1, 9);
  out.add(('Tomorrow, 09:00', tomorrow));
  final daysToMonday = (DateTime.monday - now.weekday + 7) % 7;
  final monday = DateTime(
    now.year,
    now.month,
    now.day + (daysToMonday == 0 ? 7 : daysToMonday),
    9,
  );
  if (monday != tomorrow) out.add(('Monday, 09:00', monday));
  return out;
}

/// `Tomorrow, 09:00` / `Mon 17 Nov, 09:00`, for saying when it goes.
String scheduleLabel(DateTime t, {DateTime? now}) {
  final n = now ?? DateTime.now();
  final l = t.toLocal();
  final hm =
      '${l.hour.toString().padLeft(2, '0')}:${l.minute.toString().padLeft(2, '0')}';
  final today = DateTime(n.year, n.month, n.day);
  final day = DateTime(l.year, l.month, l.day);
  final diff = day.difference(today).inDays;
  if (diff == 0) return 'today, $hm';
  if (diff == 1) return 'tomorrow, $hm';
  const days = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];
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
  return '${days[l.weekday - 1]} ${l.day} ${months[l.month - 1]}, $hm';
}

/// The schedule sheet: a few suggested times, or a date and time picked.
/// A bottom sheet on phones, a dialog elsewhere. Returns the time, or
/// null if the person cancelled.
Future<DateTime?> showScheduleSheet(BuildContext context, {DateTime? now}) {
  final compact =
      WidthClass.of(MediaQuery.sizeOf(context).width) == WidthClass.compact;
  Widget body(BuildContext context) =>
      _ScheduleSheet(now: now ?? DateTime.now());
  if (compact) {
    return showModalBottomSheet<DateTime>(
  sheetAnimationStyle: AnimationStyle.noAnimation,
      context: context,
      builder: body,
      showDragHandle: true,
    );
  }
  return showDialog<DateTime>(
  animationStyle: AnimationStyle.noAnimation,
    context: context,
    builder: (context) =>
        Dialog(child: SizedBox(width: 380, child: body(context))),
  );
}

class _ScheduleSheet extends StatelessWidget {
  const _ScheduleSheet({required this.now});
  final DateTime now;

  Future<void> _pick(BuildContext context) async {
    final date = await showDatePicker(
      context: context,
      firstDate: DateTime(now.year, now.month, now.day),
      lastDate: now.add(const Duration(days: 365)),
      initialDate: now,
    );
    if (date == null || !context.mounted) return;
    final time = await showTimePicker(
      context: context,
      initialTime: TimeOfDay.fromDateTime(now.add(const Duration(hours: 1))),
    );
    if (time == null || !context.mounted) return;
    final at = DateTime(
      date.year,
      date.month,
      date.day,
      time.hour,
      time.minute,
    );
    if (!at.isAfter(DateTime.now())) {
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(
        const SnackBar(content: Text('Pick a time that is still ahead.')),
      );
      return;
    }
    Navigator.of(context).pop(at);
  }

  @override
  Widget build(BuildContext context) {
    final c = ChurrColors.of(context);
    return SafeArea(
      child: Column(
        key: const Key('schedule-sheet'),
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
            child: Text(
              'Send later',
              style: Theme.of(context).textTheme.titleMedium
                  ?.copyWith(fontWeight: FontWeight.w800),
            ),
          ),
          for (final (label, at) in suggestedTimes(now))
            ListTile(
              title: Text(label),
              trailing: Text(
                scheduleLabel(at, now: now),
                style: monoStyle(context, color: c.dim),
              ),
              onTap: () => Navigator.of(context).pop(at),
            ),
          ListTile(
            leading: const Glyph(ChurrIcons.calendar),
            title: const Text('Pick a date and time…'),
            onTap: () => _pick(context),
          ),
          const SizedBox(height: 8),
        ],
      ),
    );
  }
}
