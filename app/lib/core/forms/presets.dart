/// The form presets people post from the composer (docs/FEATURES.md 7.5):
/// `/poll`, `/rsvp`, `/signup` and `/approve`, read from the command text
/// with the fixed grammar every client shares, and checked before
/// anything is sent. Plain Dart, so it is tested without Flutter.
library;

import '../models/json.dart';

/// A preset the client refused: the code the shared vectors name
/// (testdata/commands/presets.json, errors) and the sentence to show.
class PresetError implements Exception {
  PresetError(this.code, this.message);
  final String code;
  final String message;
  @override
  String toString() => message;
}

/// Splits command text on `|`, trimming each part; `\|` is a literal bar.
List<String> splitParts(String text) {
  final parts = <String>[];
  final b = StringBuffer();
  for (var i = 0; i < text.length; i++) {
    final ch = text[i];
    if (ch == r'\' && i + 1 < text.length && text[i + 1] == '|') {
      b.write('|');
      i++;
    } else if (ch == '|') {
      parts.add(b.toString().trim());
      b.clear();
    } else {
      b.write(ch);
    }
  }
  parts.add(b.toString().trim());
  return parts;
}

/// The flags of 7.5, and which commands take each.
const _flagsOn = {
  'anonymous': {'poll'},
  'multiple': {'poll', 'signup'},
  'add': {'poll'},
  'results': {'poll'},
  'until': {'poll', 'signup', 'approve'},
};

/// Takes the flags off the end of the last part. Returns the part
/// without them and the flags by name (a flag with no value maps to '').
(String, Map<String, String>) _takeFlags(String command, String last) {
  final m = RegExp(r'(^|\s)--').firstMatch(last);
  if (m == null) return (last, const {});
  final head = last.substring(0, m.start).trim();
  final flags = <String, String>{};
  final tokens = last.substring(m.start).trim().split(RegExp(r'\s+'));
  String? current;
  for (final t in tokens) {
    if (t.startsWith('--')) {
      final name = t.substring(2);
      final on = _flagsOn[name];
      if (on == null || !on.contains(command)) {
        throw PresetError('unknown_flag', '/$command does not know the flag --$name.');
      }
      current = name;
      flags[name] = '';
    } else if (current == 'results' || current == 'until') {
      flags[current!] = flags[current]!.isEmpty ? t : '${flags[current]} $t';
    } else {
      throw PresetError('stray_word', '"$t" is not part of any flag.');
    }
  }
  return (head, flags);
}

const _weekdays = [
  'monday',
  'tuesday',
  'wednesday',
  'thursday',
  'friday',
  'saturday',
  'sunday',
];

int? _weekday(String w) {
  for (var i = 0; i < 7; i++) {
    if (w == _weekdays[i] || w == _weekdays[i].substring(0, 3)) return i + 1;
  }
  return null;
}

/// `HH:MM`, `H am`, `H pm`, `H:MM am`, `9am`. Returns hour and minute.
(int, int)? _time(List<String> words) {
  var s = words.join(' ').toLowerCase().replaceAll(' ', '');
  var pm = false, am = false;
  if (s.endsWith('pm')) {
    pm = true;
    s = s.substring(0, s.length - 2);
  } else if (s.endsWith('am')) {
    am = true;
    s = s.substring(0, s.length - 2);
  }
  final m = RegExp(r'^(\d{1,2})(?::(\d{2}))?$').firstMatch(s);
  if (m == null) return null;
  var h = int.parse(m.group(1)!);
  final min = m.group(2) == null ? 0 : int.parse(m.group(2)!);
  if (min > 59) return null;
  if (am || pm) {
    if (h < 1 || h > 12) return null;
    if (h == 12) h = 0;
    if (pm) h += 12;
  } else if (m.group(2) == null &&
      words.length == 1 &&
      !words.first.contains(':')) {
    // A bare hour, "at 9", reads as that hour.
    if (h > 23) return null;
  } else if (h > 23) {
    return null;
  }
  return (h, min);
}

/// A single moment in the "once" part of the time grammar of
/// FEATURES.md 9.2: `in 2h 30m`, `tomorrow at 9`, `friday`,
/// `2026-12-01 at 14:30`, `at 17:00`. Wall clock in the device's time
/// zone. A missing time means 09:00; `at` alone means the next time the
/// clock shows it; a weekday means its next occurrence after today.
/// Returns null when the text does not parse.
DateTime? parseMoment(String text, DateTime now) {
  final words = text
      .trim()
      .toLowerCase()
      .split(RegExp(r'\s+'))
      .where((w) => w.isNotEmpty)
      .toList();
  if (words.isEmpty) return null;
  final local = now.toLocal();
  if (words.first == 'in') {
    final rest = words
        .skip(1)
        .join(' ')
        .replaceAllMapped(
          RegExp(r'(\d+)\s*([a-z]+)'),
          (m) => '${m[1]}${m[2]} ',
        );
    final spans = RegExp(r'(\d+)([a-z]+)').allMatches(rest).toList();
    if (spans.isEmpty || rest.replaceAll(RegExp(r'(\d+)([a-z]+)'), '').trim().isNotEmpty) return null;
    var d = Duration.zero;
    for (final s in spans) {
      final n = int.parse(s[1]!);
      d += switch (s[2]) {
        'm' || 'min' || 'mins' || 'minute' || 'minutes' => Duration(minutes: n),
        'h' || 'hour' || 'hours' => Duration(hours: n),
        'd' || 'day' || 'days' => Duration(days: n),
        'w' || 'week' || 'weeks' => Duration(days: 7 * n),
        _ => const Duration(days: -100000),
      };
    }
    if (d <= Duration.zero) return null;
    return now.add(d);
  }
  int at = words.indexOf('at');
  final dayWords = at < 0 ? words : words.sublist(0, at);
  (int, int)? hm = (9, 0);
  if (at >= 0) {
    hm = _time(words.sublist(at + 1));
    if (hm == null) return null;
  }
  DateTime? day;
  if (dayWords.isEmpty) {
    // `at` alone: the next time the clock shows it.
    final today = DateTime(local.year, local.month, local.day, hm.$1, hm.$2);
    return today.isAfter(local)
        ? today
        : DateTime(local.year, local.month, local.day + 1, hm.$1, hm.$2);
  }
  if (dayWords.length != 1) return null;
  final w = dayWords.first;
  if (w == 'today') {
    day = DateTime(local.year, local.month, local.day);
  } else if (w == 'tomorrow') {
    day = DateTime(local.year, local.month, local.day + 1);
  } else if (_weekday(w) != null) {
    var add = (_weekday(w)! - local.weekday) % 7;
    if (add == 0) add = 7;
    day = DateTime(local.year, local.month, local.day + add);
  } else {
    final m = RegExp(r'^(\d{4})-(\d{2})-(\d{2})$').firstMatch(w);
    if (m == null) return null;
    final y = int.parse(m[1]!), mo = int.parse(m[2]!), d = int.parse(m[3]!);
    day = DateTime(y, mo, d);
    if (day.month != mo || day.day != d) return null;
  }
  return DateTime(day.year, day.month, day.day, hm.$1, hm.$2);
}

DateTime _future(String what, String text, DateTime now) {
  final t = parseMoment(text, now);
  if (t == null) throw PresetError('bad_time', 'Could not read "$text" as a time.');
  if (!t.isAfter(now)) throw PresetError('in_past', '$what is in the past.');
  return t;
}

List<Json> _options(List<String> labels, {bool places = false}) {
  final out = <Json>[];
  for (var i = 0; i < labels.length; i++) {
    var label = labels[i];
    int? max;
    if (places) {
      final m = RegExp(r'^(.*?)\s*\((\d+)\)$').firstMatch(label);
      if (m != null) {
        label = m[1]!.trim();
        max = int.parse(m[2]!);
        if (max < 1) throw PresetError('no_places', '"${labels[i]}" needs at least one place.');
      }
    }
    if (label.isEmpty) throw PresetError('empty_option', 'Option ${i + 1} is empty.');
    out.add({'id': 'o${i + 1}', 'label': label, 'max': ?max});
  }
  return out;
}

void _checkClose(
  DateTime? closesAt,
  DateTime now, [
  String what = 'The closing time',
]) {
  if (closesAt != null && !closesAt.isAfter(now)) throw PresetError('in_past', '$what is in the past.');
}

/// The poll preset from its parts; the command and the dialog both
/// build through here, so they make the same form.
Json buildPoll({
  required String question,
  required List<String> options,
  bool anonymous = false,
  bool multiple = false,
  bool addOptions = false,
  String results = 'live',
  DateTime? closesAt,
  required DateTime now,
}) {
  final labels = [
    for (final o in options)
      if (o.trim().isNotEmpty) o.trim(),
  ];
  if (question.trim().isEmpty) throw PresetError('needs_question', 'A poll needs a question.');
  if (labels.length < 2) throw PresetError('too_few_options', 'A poll needs at least two options.');
  if (labels.length > 50) throw PresetError('too_many_options', 'A poll has at most 50 options.');
  if (!{'live', 'after_answer', 'after_close'}.contains(results)) {
    throw PresetError(
      'bad_results',
      'Results are shown live, after answering, or after closing.',
    );
  }
  _checkClose(closesAt, now);
  return {
    'preset': 'poll',
    'title': question.trim(),
    'fields': [
      {
        'id': 'choice',
        'type': 'choice',
        'label': question.trim(),
        'required': true,
        if (multiple) 'multiple': true,
        'options': _options(labels),
      },
    ],
    'action': {'type': 'tally'},
    if (anonymous) 'anonymous': true,
    'results': results,
    if (addOptions) 'allow_added_options': true,
    if (closesAt != null) 'closes_at': formatTime(closesAt),
  };
}

/// The RSVP preset: Going, Maybe, Can't; `closes_at` is the event's time.
Json buildRsvp({
  required String what,
  required DateTime? when,
  required DateTime now,
}) {
  if (what.trim().isEmpty) throw PresetError('needs_what', 'An RSVP needs a what.');
  if (when == null) throw PresetError('needs_when', 'An RSVP needs a when.');
  _checkClose(when, now, 'The time');
  return {
    'preset': 'rsvp',
    'title': what.trim(),
    'fields': [
      {
        'id': 'choice',
        'type': 'choice',
        'label': what.trim(),
        'required': true,
        'options': [
          {'id': 'going', 'label': 'Going'},
          {'id': 'maybe', 'label': 'Maybe'},
          {'id': 'cant', 'label': "Can't"},
        ],
      },
    ],
    'action': {'type': 'tally'},
    'results': 'live',
    'closes_at': formatTime(when),
  };
}

/// The sign-up preset. A slot written `Morning (3)` has three places.
Json buildSignup({
  required String what,
  required List<String> slots,
  bool multiple = false,
  DateTime? closesAt,
  required DateTime now,
}) {
  final labels = [
    for (final s in slots)
      if (s.trim().isNotEmpty) s.trim(),
  ];
  if (what.trim().isEmpty) throw PresetError('needs_what', 'A sign-up needs a what.');
  if (labels.isEmpty) throw PresetError('needs_slot', 'A sign-up needs at least one slot.');
  if (labels.length > 50) throw PresetError('too_many_options', 'A sign-up has at most 50 slots.');
  _checkClose(closesAt, now);
  return {
    'preset': 'signup',
    'title': what.trim(),
    'fields': [
      {
        'id': 'choice',
        'type': 'choice',
        'label': what.trim(),
        'required': true,
        if (multiple) 'multiple': true,
        'options': _options(labels, places: true),
      },
    ],
    'action': {'type': 'tally'},
    if (closesAt != null) 'closes_at': formatTime(closesAt),
  };
}

/// The approve preset: Approve and Decline buttons, named, one answer
/// each; [groupId] limits who may press.
Json buildApprove({
  required String what,
  String? groupId,
  DateTime? closesAt,
  required DateTime now,
}) {
  if (what.trim().isEmpty) throw PresetError('needs_what', 'Say what is to be approved.');
  _checkClose(closesAt, now);
  return {
    'preset': 'approve',
    'title': what.trim(),
    'fields': <Json>[],
    'buttons': [
      {'id': 'approve', 'label': 'Approve', 'style': 'primary'},
      {'id': 'decline', 'label': 'Decline', 'style': 'danger'},
    ],
    'action': {'type': 'tally'},
    'answerer_group_id': ?groupId,
    if (closesAt != null) 'closes_at': formatTime(closesAt),
  };
}

DateTime? _until(Map<String, String> flags, DateTime now) =>
    flags['until'] == null
    ? null
    : _future('The closing time', flags['until']!, now);

/// `/poll Question? | Option | Option … [--anonymous] [--multiple] [--add]
/// [--results live|after_answer|after_close] [--until when]`.
Json pollForm(String args, DateTime now) {
  final parts = splitParts(args);
  final (last, flags) = _takeFlags('poll', parts.last);
  parts[parts.length - 1] = last;
  final results = flags['results'];
  if (results != null &&
      !{'live', 'after_answer', 'after_close'}.contains(results)) {
    throw PresetError('bad_results', '--results takes live, after_answer or after_close.');
  }
  return buildPoll(
    question: parts.first,
    options: parts.skip(1).toList(),
    anonymous: flags.containsKey('anonymous'),
    multiple: flags.containsKey('multiple'),
    addOptions: flags.containsKey('add'),
    results: results ?? 'live',
    closesAt: _until(flags, now),
    now: now,
  );
}

/// `/rsvp What | When`.
Json rsvpForm(String args, DateTime now) {
  final parts = splitParts(args);
  final (last, _) = _takeFlags('rsvp', parts.last);
  parts[parts.length - 1] = last;
  if (parts.first.isEmpty) throw PresetError('needs_what', 'An RSVP needs a what.');
  if (parts.length < 2 || parts[1].isEmpty) throw PresetError('needs_when', 'An RSVP needs a when.');
  if (parts.length > 2) throw PresetError('too_many_parts', 'An RSVP takes a what and a when, nothing more.');
  return buildRsvp(
    what: parts.first,
    when: _future('The time', parts[1], now),
    now: now,
  );
}

/// `/signup What | Slot | Slot (3) … [--multiple] [--until when]`.
Json signupForm(String args, DateTime now) {
  final parts = splitParts(args);
  final (last, flags) = _takeFlags('signup', parts.last);
  parts[parts.length - 1] = last;
  return buildSignup(
    what: parts.first,
    slots: parts.skip(1).toList(),
    multiple: flags.containsKey('multiple'),
    closesAt: _until(flags, now),
    now: now,
  );
}

/// `/approve What [| @group] [--until when]`. The second part names a
/// group whose members alone may press; the server refuses it from
/// anyone not acting as manager. [groupIdOf] resolves the handle.
Json approveForm(
  String args,
  DateTime now, {
  String? Function(String handle)? groupIdOf,
}) {
  final parts = splitParts(args);
  final (last, flags) = _takeFlags('approve', parts.last);
  parts[parts.length - 1] = last;
  if (parts.length > 2) throw PresetError('too_many_parts', '/approve takes a what and at most one group.');
  String? group;
  if (parts.length == 2 && parts[1].isNotEmpty) {
    final handle = parts[1].startsWith('@') ? parts[1].substring(1) : parts[1];
    group = groupIdOf?.call(handle);
    if (group == null) throw PresetError('unknown_group', 'No group @$handle here.');
  }
  return buildApprove(
    what: parts.first,
    groupId: group,
    closesAt: _until(flags, now),
    now: now,
  );
}

/// Checks a form made with the builder as the server will (FEATURES.md
/// 7.1), so mistakes are caught before sending.
void checkBuiltForm(Json form) {
  final fields = (form['fields'] as List?) ?? const [];
  final buttons = (form['buttons'] as List?) ?? const [];
  if (fields.isEmpty && buttons.isEmpty) throw PresetError('bad_form', 'Add at least one field or button.');
  if (fields.length > 20) throw PresetError('bad_form', 'At most 20 fields.');
  if (buttons.length > 5) throw PresetError('bad_form', 'At most five buttons.');
  final ids = <String>{};
  final idRule = RegExp(r'^[a-z0-9_-]{1,32}$');
  for (final f in [...fields, ...buttons]) {
    final m = f as Map;
    final id = '${m['id']}';
    if (!idRule.hasMatch(id) || id == '_button') throw PresetError('bad_form', '"$id" is not a usable id.');
    if (!ids.add(id)) throw PresetError('bad_form', 'Two parts share the id "$id".');
    if ('${m['label'] ?? ''}'.trim().isEmpty) throw PresetError('bad_form', 'Every field and button needs a label.');
    if (m['type'] == 'choice') {
      final opts = (m['options'] as List?) ?? const [];
      if (opts.isEmpty) throw PresetError('bad_form', '"${m['label']}" needs options.');
      if (opts.length > 50) throw PresetError('bad_form', '"${m['label']}" has more than 50 options.');
    }
  }
  final closes = parseTime(form['closes_at']);
  if (closes != null && !closes.isAfter(DateTime.now())) throw PresetError('bad_form', 'The closing time is in the past.');
}
