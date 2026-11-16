/// The /remind grammar (docs/FEATURES.md 9.2 and 9.3):
///
///     /remind [target] when | what
///
/// read into who it is for, when it runs (one moment, or a repeat rule)
/// and what it says. The web chat reads by the same rules
/// (internal/web/static/shared/remind.js); both are held to
/// testdata/commands/remind.json. Plain Dart, so it is tested without
/// Flutter.
library;

import '../forms/presets.dart';

/// A reminder the client refused: the code the shared vectors name and
/// the sentence to show.
class RemindError implements Exception {
  RemindError(this.code, this.message);
  final String code;
  final String message;
  @override
  String toString() => message;
}

/// What /remind was read as. [target] is `{'kind': 'me'}`,
/// `{'kind': 'person', 'person': username}` or `{'kind': 'channel', 'id':
/// id}`. Either [at] (one run, local wall clock) or [repeat] (the rule of
/// 9.3 without its time zone, which the caller adds) is set.
class Reminder {
  const Reminder({
    required this.target,
    required this.what,
    this.at,
    this.repeat,
  });
  final Map<String, String> target;
  final String what;
  final DateTime? at;
  final Map<String, String>? repeat;
}

final _person = RegExp(r'^@([a-z0-9._-]+(?:@[a-z0-9.-]+)?)$', caseSensitive: false);
final _link = RegExp(r'^<#([0-9a-f-]{36})>$', caseSensitive: false);

Map<String, String>? _repeatOf(List<String> words) {
  final at = words.indexOf('at');
  final unit = (at < 0 ? words : words.sublist(0, at)).join();
  var hm = (9, 0);
  if (at >= 0) {
    final t = clockTime(words.sublist(at + 1));
    if (t == null) return null;
    hm = t;
  }
  final clock =
      '${hm.$1.toString().padLeft(2, '0')}:${hm.$2.toString().padLeft(2, '0')}';
  switch (unit) {
    case 'day':
      return {'every': 'day', 'days': '1111111', 'at': clock};
    case 'weekday':
      return {'every': 'day', 'days': '1111100', 'at': clock};
    case 'week' || 'month':
      return {'every': unit, 'at': clock};
  }
  final days = List.filled(7, '0');
  for (final n in unit.split(',')) {
    final d = weekdayNumber(n);
    if (d == null) return null;
    days[d - 1] = '1';
  }
  return {'every': 'day', 'days': days.join(), 'at': clock};
}

/// Reads what follows /remind; throws a [RemindError] saying what is
/// wrong. Times are wall clock in the device's zone.
Reminder parseRemind(String args, DateTime now) {
  final bar = args.indexOf('|');
  final head = (bar < 0 ? args : args.substring(0, bar)).trim();
  final what = bar < 0 ? '' : args.substring(bar + 1).trim();
  var words = head.split(RegExp(r'\s+')).where((w) => w.isNotEmpty).toList();
  var target = const {'kind': 'me'};
  if (words.isNotEmpty) {
    final first = words.first;
    final p = _person.firstMatch(first), l = _link.firstMatch(first);
    if (first.toLowerCase() == 'me') {
      words = words.sublist(1);
    } else if (p != null) {
      target = {'kind': 'person', 'person': p[1]!.toLowerCase()};
      words = words.sublist(1);
    } else if (l != null) {
      target = {'kind': 'channel', 'id': l[1]!.toLowerCase()};
      words = words.sublist(1);
    } else if (first.startsWith('#')) {
      throw RemindError(
        'bad_channel',
        'Name a channel by typing # and picking it.',
      );
    }
  }
  words = [for (final w in words) w.toLowerCase()];
  if (words.isEmpty) {
    throw RemindError(
      'needs_when',
      'Say when, before the |: /remind tomorrow at 9 | Send the report.',
    );
  }
  if (what.isEmpty) {
    throw RemindError(
      'needs_what',
      'Say what to be reminded of, after a |: /remind tomorrow at 9 | Send the report.',
    );
  }
  if (words.first == 'every') {
    final repeat = _repeatOf(words.sublist(1));
    if (repeat == null) {
      throw RemindError(
        'bad_when',
        'Could not read "${words.join(' ')}" as a time. Write every day, every weekday, every week, every month or every monday,thursday, then at and a time if you like.',
      );
    }
    return Reminder(target: target, what: what, repeat: repeat);
  }
  final at = parseMoment(words.join(' '), now);
  if (at == null) {
    throw RemindError(
      'bad_when',
      'Could not read "${words.join(' ')}" as a time. Write in 20m, tomorrow at 9, friday at 14:30, 2026-12-01 or at 17:00.',
    );
  }
  if (!at.isAfter(now)) {
    throw RemindError('in_past', 'That time has already passed.');
  }
  return Reminder(target: target, what: what, at: at);
}

/// A repeat rule in words, for what the app says once it is set:
/// "every weekday at 09:30".
String repeatWords(Map<String, String> r) {
  var unit = r['every']!;
  final days = r['days'];
  if (unit == 'day' && days == '1111100') {
    unit = 'weekday';
  } else if (unit == 'day' && days != null && days != '1111111') {
    unit = [
      for (var i = 0; i < 7; i++)
        if (days[i] == '1') weekdayNames[i],
    ].join(', ');
  }
  return 'every $unit at ${r['at']}';
}
