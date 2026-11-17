import '../instant/zone.dart';
import '../models/json.dart';

/// Recurring check-in forms (docs/FEATURES.md section 11): what a
/// channel's manager sets up, and the scheduled item the server keeps for
/// it. A check-in is a `scheduled` row of kind `form` whose content is a
/// message with a `checkin` form, action `thread`, one answer per person,
/// posted on a repeating schedule (FEATURES.md 9.3). The web chat's
/// `shared/checkin.js` is the same.
class CheckinQuestion {
  CheckinQuestion(this.label, {this.long = false, this.required = false});
  String label;
  bool long;
  bool required;
}

/// How often a check-in runs: daily on chosen days, every N weeks on
/// chosen days, or monthly (on a day, or an nth weekday).
enum CheckinKind { daily, weekly, monthly }

class CheckinSetup {
  CheckinSetup({
    required this.title,
    this.text = '',
    required this.questions,
    this.kind = CheckinKind.daily,
    this.days = '1111100',
    this.interval = 1,
    this.byWeekday = false,
    this.day = 1,
    this.week = 1,
    this.weekday = 1,
    this.at = '09:00',
    required this.zone,
  });

  /// A new one: three questions, weekdays at nine, in [zone].
  factory CheckinSetup.blank(String zone) => CheckinSetup(
    title: 'Check-in',
    questions: [
      CheckinQuestion('What did you do yesterday?'),
      CheckinQuestion('What will you do today?'),
      CheckinQuestion('Anything in your way?'),
    ],
    zone: zone,
  );

  /// Reads a stored one back, in the kind it was set up with. A weekly or
  /// monthly rule that names no day (one made with /remind) is read with
  /// the weekday or day of its [nextRun].
  factory CheckinSetup.fromItem(
    Json content,
    Json? repeat, {
    DateTime? nextRun,
  }) {
    final f = object(content['form']) ?? const {};
    final r = repeat ?? const {};
    final zone = r['time_zone'] as String? ?? 'UTC';
    final next = nextRun == null
        ? null
        : wallIn(nextRun.millisecondsSinceEpoch, zone);
    final kind = switch (r['every']) {
      'week' => CheckinKind.weekly,
      'month' => CheckinKind.monthly,
      _ => CheckinKind.daily,
    };
    var days = r['days'] as String? ?? '';
    if (!RegExp(r'^[01]{7}$').hasMatch(days)) {
      days = kind == CheckinKind.weekly
          ? [for (var i = 0; i < 7; i++) i == (next?.wd ?? 0) ? '1' : '0'].join()
          : '1111111';
    }
    int n(String k) => (r[k] as num?)?.toInt() ?? 0;
    return CheckinSetup(
      title: f['title'] as String? ?? '',
      text: content['text'] as String? ?? '',
      questions: [
        for (final x in (f['fields'] as List? ?? const []))
          if (x is Map)
            CheckinQuestion(
              x['label'] as String? ?? '',
              long: x['type'] == 'longtext',
              required: x['required'] == true,
            ),
      ],
      kind: kind,
      days: days,
      interval: n('interval') > 0 ? n('interval') : 1,
      byWeekday: n('week') != 0,
      day: n('day') > 0 ? n('day') : (next?.d ?? 1),
      week: n('week') != 0 ? n('week') : 1,
      weekday: n('weekday') > 0 ? n('weekday') : 1,
      at: r['at'] as String? ?? '09:00',
      zone: zone,
    );
  }

  String title;
  String text;
  List<CheckinQuestion> questions;

  CheckinKind kind;

  /// Seven 0s and 1s, Monday first: the days a daily or weekly one runs.
  String days;

  /// A weekly one runs every this many weeks, 1 to 52.
  int interval;

  /// A monthly one runs on the [week]th [weekday] (week 1 to 4, or -1 for
  /// the last; weekday 1 Monday to 7) when true, else on [day] (1 to 31).
  bool byWeekday;
  int day;
  int week;
  int weekday;

  /// `HH:MM` in [zone].
  String at;
  String zone;

  static const maxQuestions = 20;

  List<CheckinQuestion> get _asked => [
    for (final q in questions)
      if (q.label.trim().isNotEmpty) q,
  ];

  /// What is wrong with it, or null.
  String? get problem {
    if (title.trim().isEmpty) return 'Give it a title.';
    if (_asked.isEmpty) return 'Ask at least one question.';
    if (_asked.length > maxQuestions) return 'At most $maxQuestions questions.';
    if (kind != CheckinKind.monthly &&
        (!RegExp(r'^[01]{7}$').hasMatch(days) || !days.contains('1'))) {
      return 'Choose at least one day.';
    }
    if (kind == CheckinKind.weekly && (interval < 1 || interval > 52)) {
      return 'Repeat every 1 to 52 weeks.';
    }
    if (kind == CheckinKind.monthly && !byWeekday && (day < 1 || day > 31)) {
      return 'Choose a day of the month from 1 to 31.';
    }
    if (kind == CheckinKind.monthly &&
        byWeekday &&
        (!checkinWeeks.containsKey(week) || weekday < 1 || weekday > 7)) {
      return 'Choose which weekday of the month.';
    }
    if (!RegExp(r'^([01][0-9]|2[0-3]):[0-5][0-9]$').hasMatch(at)) {
      return 'Give the time as HH:MM.';
    }
    if (zone.isEmpty) return 'Choose a time zone.';
    return null;
  }

  /// The message each run posts.
  Json get content => {
    if (text.trim().isNotEmpty) 'text': text.trim(),
    'form': {
      'preset': 'checkin',
      'title': title.trim(),
      'action': {'type': 'thread'},
      'one_answer': true,
      'fields': [
        for (final (i, q) in _asked.indexed)
          {
            'id': 'q${i + 1}',
            'type': q.long ? 'longtext' : 'text',
            'label': q.label.trim(),
            if (q.required) 'required': true,
          },
      ],
    },
  };

  /// The rule of FEATURES.md 9.3, in its kind.
  Json get repeat => switch (kind) {
    CheckinKind.daily => {
      'every': 'day',
      'days': days,
      'at': at,
      'time_zone': zone,
    },
    CheckinKind.weekly => {
      'every': 'week',
      'days': days,
      if (interval > 1) 'interval': interval,
      'at': at,
      'time_zone': zone,
    },
    CheckinKind.monthly => {
      'every': 'month',
      if (byWeekday) ...{'week': week, 'weekday': weekday} else 'day': day,
      'at': at,
      'time_zone': zone,
    },
  };

  /// The request that sets it up in [conversationId].
  Json createBody(String conversationId) => {
    'conversation_id': conversationId,
    'kind': 'form',
    'content': content,
    'repeat': repeat,
  };
}

const checkinDayNames = [
  'Monday',
  'Tuesday',
  'Wednesday',
  'Thursday',
  'Friday',
  'Saturday',
  'Sunday',
];

/// The nth weekdays a monthly one may run on.
const checkinWeeks = {
  1: 'first',
  2: 'second',
  3: 'third',
  4: 'fourth',
  -1: 'last',
};

String _named(String d) => [
  for (var i = 0; i < 7 && i < d.length; i++)
    if (d[i] == '1') checkinDayNames[i],
].join(', ');

/// When a schedule runs: "Weekdays at 09:30 (Europe/Dublin)".
String repeatWords(Json? r) {
  if (r == null) return 'Once';
  final when = ' at ${r['at']} (${r['time_zone']})';
  int n(String k) => (r[k] as num?)?.toInt() ?? 0;
  final days = r['days'] as String?;
  if (r['every'] == 'week' && days != null && days.isNotEmpty) {
    final every = n('interval') > 1
        ? 'Every ${n('interval')} weeks'
        : 'Every week';
    return '$every on ${_named(days)}$when';
  }
  if (r['every'] == 'week') return 'Every week$when';
  if (r['every'] == 'month' && n('week') != 0) {
    final wd = n('weekday');
    return 'Monthly on the ${checkinWeeks[n('week')]} '
        '${wd >= 1 && wd <= 7 ? checkinDayNames[wd - 1] : ''}$when';
  }
  if (r['every'] == 'month' && n('day') > 0) {
    return 'Monthly on day ${n('day')}$when';
  }
  if (r['every'] == 'month') return 'Every month$when';
  final d = days ?? '1111111';
  if (d == '1111111') return 'Every day$when';
  if (d == '1111100') return 'Weekdays$when';
  if (d == '0000011') return 'Weekends$when';
  return _named(d) + when;
}
