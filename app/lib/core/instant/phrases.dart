// Phrases that name a time ("this Friday at 5pm", "tomorrow 9:30", "in 2
// hours"), which the composer offers to make into an instant (ruling R69).
// Nothing is converted without the author accepting; the words stay the
// message's text and the instant travels as a hint beside it.
//
// The grammar and its rules are the web chat's
// (internal/web/static/shared/instant/phrases.js, where they are written
// out); both run testdata/instants/phrases.json with a fixed "now" and
// zone. The words come from a table per language, so another language is a
// new table, not new code.

import 'zone.dart';

/// The words of one language. Matching ignores case.
class PhraseWords {
  const PhraseWords({
    required this.today,
    required this.tomorrow,
    required this.tonight,
    required this.this_,
    required this.next,
    required this.on,
    required this.at,
    required this.in_,
    required this.weekdays,
    required this.months,
    required this.am,
    required this.pm,
    required this.noon,
    required this.ordinal,
    required this.one,
    required this.half,
    required this.minutes,
    required this.hours,
  });
  final List<String> today, tomorrow, tonight, this_, next, on, at, in_;
  final List<List<String>> weekdays, months;
  final List<String> am, pm, noon, ordinal, one, half, minutes, hours;
}

const phraseWords = {
  'en': PhraseWords(
    today: ['today'],
    tomorrow: ['tomorrow'],
    tonight: ['tonight'],
    this_: ['this'],
    next: ['next'],
    on: ['on'],
    at: ['at'],
    in_: ['in'],
    weekdays: [
      ['monday', 'mon'],
      ['tuesday', 'tue', 'tues'],
      ['wednesday', 'wed'],
      ['thursday', 'thu', 'thur', 'thurs'],
      ['friday', 'fri'],
      ['saturday', 'sat'],
      ['sunday', 'sun'],
    ],
    months: [
      ['january', 'jan'],
      ['february', 'feb'],
      ['march', 'mar'],
      ['april', 'apr'],
      ['may'],
      ['june', 'jun'],
      ['july', 'jul'],
      ['august', 'aug'],
      ['september', 'sep', 'sept'],
      ['october', 'oct'],
      ['november', 'nov'],
      ['december', 'dec'],
    ],
    am: ['am', 'a.m.'],
    pm: ['pm', 'p.m.'],
    noon: ['noon', 'midday'],
    ordinal: ['st', 'nd', 'rd', 'th'],
    one: ['a', 'an'],
    half: ['half an', 'half a'],
    minutes: ['minutes', 'minute', 'mins', 'min'],
    hours: ['hours', 'hour', 'hrs', 'hr', 'h'],
  ),
};

/// A phrase found: its range in UTF-16 code units and the instant hint.
class Phrase {
  const Phrase(this.start, this.end, this.time, this.precision, this.zone);
  final int start, end;
  final String time, precision, zone;

  Map<String, Object?> toJson() => {
    'start': start,
    'end': end,
    'time': time,
    'precision': precision,
    'zone': zone,
  };
}

String _esc(String s) => RegExp.escape(s).replaceAll(' ', r'\s+');
String _alt(Iterable<String> list) {
  final sorted = [...list]..sort((a, b) => b.length.compareTo(a.length));
  return '(?:${sorted.map(_esc).join('|')})';
}

class _Grammar {
  _Grammar(this.w) {
    final wk = w.weekdays.expand((x) => x), mo = w.months.expand((x) => x);
    final ord = _alt(w.ordinal);
    const s = r'\s+';
    RegExp re(String p) => RegExp(p, caseSensitive: false, unicode: true);
    day = re(
      '(${_alt(w.today)})|(${_alt(w.tomorrow)})|(${_alt(w.tonight)})|(?:(${_alt(w.this_)}|${_alt(w.next)})$s|${_alt(w.on)}$s)?(${_alt(wk)})(?![\\p{L}\\p{N}])|(?:${_alt(w.on)}$s)?(?:(\\d{1,2})(?:$ord)?$s(${_alt(mo)})(?:$s(\\d{4}))?|(${_alt(mo)})$s(\\d{1,2})(?:$ord)?(?:,?$s(\\d{4}))?)',
    );
    joinDT = re(',?$s(?:${_alt(w.at)}$s)?');
    joinTD = re('$s(?:${_alt(w.on)}$s)?');
    time = re(
      '(\\d{1,2})(?::(\\d{2}))?(?:\\s?(${_alt(w.am)}|${_alt(w.pm)}))?|(${_alt(w.noon)})',
    );
    inRe = re(
      '${_alt(w.in_)}$s(\\d{1,3}|${_alt(w.half)}|${_alt(w.one)})$s(${_alt(w.minutes)}|${_alt(w.hours)})',
    );
  }
  final PhraseWords w;
  late final RegExp day, joinDT, joinTD, time, inRe;

  bool isIn(List<String> list, String s) =>
      list.contains(s.toLowerCase().replaceAll(RegExp(r'\s+'), ' '));
}

final _grammars = <String, _Grammar>{};
_Grammar _grammarOf(String lang) => _grammars.putIfAbsent(
  lang,
  () => _Grammar(phraseWords[lang] ?? phraseWords['en']!),
);

final _wordChar = RegExp(r'[\p{L}\p{N}]', unicode: true);
bool _isWordAt(String text, int i) =>
    i >= 0 && i < text.length && _wordChar.hasMatch(text[i]);

class _Time {
  _Time(this.h, this.mi, {this.bare = false, this.ampm = false});
  int h;
  final int mi;
  final bool bare, ampm;
}

class _Day {
  const _Day(this.y, this.mo, this.d);
  final int y, mo, d;
}

_Time? _readTime(_Grammar g, Match m) {
  if (m[4] != null) return _Time(12, 0);
  var h = int.parse(m[1]!);
  final mi = m[2] == null ? 0 : int.parse(m[2]!);
  if (mi > 59) return null;
  if (m[3] != null) {
    if (h < 1 || h > 12) return null;
    h = (h % 12) + (g.isIn(g.w.pm, m[3]!) ? 12 : 0);
    return _Time(h, mi, ampm: true);
  }
  if (m[2] == null) return h >= 1 && h <= 12 ? _Time(h, 0, bare: true) : null;
  return h <= 23 ? _Time(h, mi) : null;
}

_Day _addDays(int y, int mo, int d, int n) {
  final t = DateTime.utc(y, mo, d).add(Duration(days: n));
  return _Day(t.year, t.month, t.day);
}

bool _validDay(int y, int mo, int d) {
  final leap = (y % 4 == 0 && y % 100 != 0) || y % 400 == 0;
  return d >= 1 &&
      d <= [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][mo - 1];
}

_Day? _readDay(_Grammar g, Match m, _Time time, Wall now) {
  bool later(_Day day) =>
      utcMs(day.y, day.mo, day.d, time.h, time.mi) >
      utcMs(now.y, now.mo, now.d, now.h, now.mi);
  final today = _Day(now.y, now.mo, now.d);
  if (m[1] != null) return time.bare ? null : today;
  if (m[2] != null) return time.bare ? null : _addDays(now.y, now.mo, now.d, 1);
  if (m[3] != null) {
    // Tonight is the evening: an hour before noon without am or pm is
    // after it.
    if (!time.ampm && time.h < 12) {
      time.h += 12;
    } else if (time.bare) {
      return null;
    }
    return today;
  }
  if (time.bare) return null;
  if (m[5] != null) {
    final target = g.w.weekdays.indexWhere((names) => g.isIn(names, m[5]!));
    var delta = (target - now.wd + 7) % 7;
    if (delta == 0 && !later(today)) delta = 7;
    if (m[4] != null && g.isIn(g.w.next, m[4]!) && now.wd + delta <= 6) {
      delta += 7;
    }
    return _addDays(now.y, now.mo, now.d, delta);
  }
  final d = int.parse((m[6] ?? m[10])!);
  final monthWord = (m[7] ?? m[9])!;
  final mo = g.w.months.indexWhere((names) => g.isIn(names, monthWord)) + 1;
  final year = m[8] ?? m[11];
  if (year != null) {
    final y = int.parse(year);
    return _validDay(y, mo, d) ? _Day(y, mo, d) : null;
  }
  var day = _Day(now.y, mo, d);
  if (!_validDay(day.y, mo, d) || !later(day)) day = _Day(now.y + 1, mo, d);
  return _validDay(day.y, mo, d) ? day : null;
}

/// The phrases in [text], read in [zone] at the moment [now].
List<Phrase> findPhrases(
  String text, {
  required DateTime now,
  required String? zone,
  String lang = 'en',
}) {
  final out = <Phrase>[];
  if (zone == null || zone.isEmpty) return out;
  final g = _grammarOf(lang);
  final nowMs = now.millisecondsSinceEpoch;
  final wall = wallIn(nowMs, zone);
  for (var i = 0; i < text.length; i++) {
    if (_isWordAt(text, i - 1) || !_isWordAt(text, i)) continue;
    final hit = _phraseAt(g, text, i, wall, nowMs, zone);
    if (hit != null) {
      out.add(hit);
      i = hit.end - 1;
    }
  }
  return out;
}

Phrase? _phraseAt(
  _Grammar g,
  String text,
  int i,
  Wall now,
  int nowMs,
  String zone,
) {
  bool bounded(int end) => !_isWordAt(text, end);
  Phrase? done(int end, int? ms) =>
      ms == null ? null : Phrase(i, end, iso(ms), 'minute', zone);
  int? wall(_Day day, _Time t) =>
      fromWall(utcMs(day.y, day.mo, day.d, t.h, t.mi), zone);

  final inM = g.inRe.matchAsPrefix(text, i);
  if (inM != null && bounded(inM.end)) {
    final amount = inM[1]!;
    final n = RegExp(r'^\d').hasMatch(amount)
        ? int.parse(amount).toDouble()
        : g.isIn(g.w.half, amount)
        ? 0.5
        : 1.0;
    final minutes = n * (g.isIn(g.w.hours, inM[2]!) ? 60 : 1);
    if (minutes != minutes.roundToDouble() || minutes < 1) return null;
    return done(inM.end, (nowMs ~/ 60000) * 60000 + minutes.round() * 60000);
  }
  // Day first, then the time.
  final dm = g.day.matchAsPrefix(text, i);
  if (dm != null) {
    final j = g.joinDT.matchAsPrefix(text, dm.end);
    final t = j == null ? null : g.time.matchAsPrefix(text, j.end);
    if (t != null && bounded(t.end)) {
      final time = _readTime(g, t);
      final day = time == null ? null : _readDay(g, dm, time, now);
      return day == null ? null : done(t.end, wall(day, time!));
    }
    return null;
  }
  // The time first, then the day.
  final t = g.time.matchAsPrefix(text, i);
  if (t != null && t[0]!.isNotEmpty) {
    final j = g.joinTD.matchAsPrefix(text, t.end);
    final d = j == null ? null : g.day.matchAsPrefix(text, j.end);
    if (d != null && bounded(d.end)) {
      final time = _readTime(g, t);
      final day = time == null ? null : _readDay(g, d, time, now);
      return day == null ? null : done(d.end, wall(day, time!));
    }
  }
  return null;
}

/// The phrase that ends exactly at [end] of [text], as the composer asks
/// while the author types; null if there is none.
Phrase? phraseEndingAt(
  String text,
  int end, {
  required DateTime now,
  required String? zone,
  String lang = 'en',
}) {
  for (final p in findPhrases(
    text.substring(0, end),
    now: now,
    zone: zone,
    lang: lang,
  )) {
    if (p.end == end) return p;
  }
  return null;
}
