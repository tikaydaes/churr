// Showing an instant (ruling R69): in the reader's zone and locale,
// compact in the text ("Fri, 20 Nov, 2:30 pm", the year only when it is
// not this one, "Today" where true), in full for screen readers and the
// converter. Dates come from intl in the reader's locale; the few words
// ("Today", "in 3 days") from the app's l10n, handed in as [InstantWords].

import 'package:intl/intl.dart';

import 'zone.dart';

/// The words formatting needs from l10n.
class InstantWords {
  const InstantWords({
    required this.today,
    required this.tomorrow,
    required this.yesterday,
    required this.relative,
  });
  final String today, tomorrow, yesterday;

  /// "in 3 days", "2 hours ago": [n] whole units ("second", "minute",
  /// "hour", "day", "month", "year"), [future] whether it is to come.
  final String Function(int n, String unit, bool future) relative;

  /// English, for tests and as the fallback.
  static final english = InstantWords(
    today: 'Today',
    tomorrow: 'Tomorrow',
    yesterday: 'Yesterday',
    relative: (n, unit, future) {
      final u = n == 1 ? unit : '${unit}s';
      return future ? 'in $n $u' : '$n $u ago';
    },
  );
}

const _day = 86400000;

/// The wall clock of [ms] in [zone] as a UTC DateTime with those fields,
/// which intl formats as they stand.
DateTime _wallTime(int ms, String zone) =>
    DateTime.fromMillisecondsSinceEpoch(ms + offsetAt(zone, ms), isUtc: true);

/// The time of day, with seconds when they were typed.
String timeOfDay(
  int ms,
  String zone,
  String locale, {
  String precision = 'minute',
}) => (precision == 'second' ? DateFormat.jms(locale) : DateFormat.jm(locale))
    .format(_wallTime(ms, zone));

/// "Today", "Tomorrow" or "Yesterday" where true, else the weekday, day
/// and month, and the year when it is not this one.
String dayWord(
  int ms,
  String zone,
  String locale,
  InstantWords words, {
  DateTime? now,
}) {
  final a = _wallTime(ms, zone);
  final b = _wallTime((now ?? InstantEnv.now()).millisecondsSinceEpoch, zone);
  final diff =
      ((DateTime.utc(a.year, a.month, a.day).millisecondsSinceEpoch -
                  DateTime.utc(b.year, b.month, b.day).millisecondsSinceEpoch) /
              _day)
          .round();
  if (diff == 0) return words.today;
  if (diff == 1) return words.tomorrow;
  if (diff == -1) return words.yesterday;
  return (a.year == b.year
          ? DateFormat.MMMEd(locale)
          : DateFormat.yMMMEd(locale))
      .format(a);
}

/// How an instant is shown in a message.
String compactInstant(
  int ms,
  String zone,
  String locale,
  InstantWords words, {
  String precision = 'minute',
  DateTime? now,
}) =>
    '${dayWord(ms, zone, locale, words, now: now)}, ${timeOfDay(ms, zone, locale, precision: precision)}';

/// The whole date and time, for screen readers and the converter.
String fullInstant(
  int ms,
  String zone,
  String locale, {
  String precision = 'minute',
}) =>
    '${DateFormat.yMMMMEEEEd(locale).format(_wallTime(ms, zone))}, ${timeOfDay(ms, zone, locale, precision: precision)}';

/// A zone's offset at a moment: "UTC+10", "UTC−3:30".
String offsetText(String zone, int ms) {
  final off = offsetAt(zone, ms) ~/ 60000;
  if (off == 0) return 'UTC';
  final a = off.abs();
  final mins = a % 60 == 0 ? '' : ':${(a % 60).toString().padLeft(2, '0')}';
  return 'UTC${off < 0 ? '−' : '+'}${a ~/ 60}$mins';
}

/// A zone as a person reads it: "Europe/Berlin (UTC+1)", or "UTC+10:00"
/// for an offset.
String zoneName(String zone, int ms) {
  if (zone == 'Z') return 'UTC';
  if (offsetOf(zone) != null) return 'UTC$zone';
  return '${zone.replaceAll('_', ' ')} (${offsetText(zone, ms)})';
}

/// How long until or since.
String relativeInstant(int ms, InstantWords words, {DateTime? now}) {
  final diff = ms - (now ?? InstantEnv.now()).millisecondsSinceEpoch;
  final a = diff.abs();
  const steps = [
    (60000, 1000, 'second'),
    (3600000, 60000, 'minute'),
    (_day, 3600000, 'hour'),
    (60 * _day, _day, 'day'),
    (365 * _day, 30 * _day, 'month'),
  ];
  for (final (limit, unit, name) in steps) {
    if (a < limit) return words.relative(a ~/ unit, name, diff >= 0);
  }
  return words.relative(a ~/ (365 * _day), 'year', diff >= 0);
}
