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

class CheckinSetup {
  CheckinSetup({
    required this.title,
    this.text = '',
    required this.questions,
    this.days = '1111100',
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

  /// Reads a stored one back. A schedule that runs every week or month
  /// is read as every day; saving makes it daily on the days chosen.
  factory CheckinSetup.fromItem(Json content, Json? repeat) {
    final f = object(content['form']) ?? const {};
    final r = repeat ?? const {};
    final days = r['days'] as String? ?? '';
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
      days: r['every'] == 'day' && RegExp(r'^[01]{7}$').hasMatch(days)
          ? days
          : '1111111',
      at: r['at'] as String? ?? '09:00',
      zone: r['time_zone'] as String? ?? 'UTC',
    );
  }

  String title;
  String text;
  List<CheckinQuestion> questions;

  /// Seven 0s and 1s, Monday first.
  String days;

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
    if (!RegExp(r'^[01]{7}$').hasMatch(days) || !days.contains('1')) {
      return 'Choose at least one day.';
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

  Json get repeat => {
    'every': 'day',
    'days': days,
    'at': at,
    'time_zone': zone,
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

/// When a schedule runs: "Weekdays at 09:30 (Europe/Dublin)".
String repeatWords(Json? r) {
  if (r == null) return 'Once';
  final when = ' at ${r['at']} (${r['time_zone']})';
  if (r['every'] == 'week') return 'Every week$when';
  if (r['every'] == 'month') return 'Every month$when';
  final d = r['days'] as String? ?? '1111111';
  if (d == '1111111') return 'Every day$when';
  if (d == '1111100') return 'Weekdays$when';
  if (d == '0000011') return 'Weekends$when';
  return [
        for (var i = 0; i < 7 && i < d.length; i++)
          if (d[i] == '1') checkinDayNames[i],
      ].join(', ') +
      when;
}
