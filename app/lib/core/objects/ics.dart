// An event as a calendar file (ruling R73): "Add to calendar" makes it on
// the reader's device, with nothing sent anywhere. RFC 5545, one event,
// times in UTC. testdata/objects/ics.json holds the cases both clients
// pass.

import 'dart:convert';

String _stamp(String iso) => iso
    .replaceFirst(RegExp(r'\.\d+'), '')
    .replaceAll('-', '')
    .replaceAll(':', '');

String _text(String s) => s
    .replaceAll(r'\', r'\\')
    .replaceAll(';', r'\;')
    .replaceAll(',', r'\,')
    .replaceAll(RegExp(r'\r?\n'), r'\n');

/// A content line in lines of at most 75 octets, each after the first
/// starting with a space, never inside a character.
String _fold(String line) {
  final out = <String>[];
  var cur = StringBuffer();
  var size = 0;
  for (final r in line.runes) {
    final ch = String.fromCharCode(r);
    final n = utf8.encode(ch).length;
    if (size + n > (out.isEmpty ? 75 : 74)) {
      out.add('$cur');
      cur = StringBuffer();
      size = 0;
    }
    cur.write(ch);
    size += n;
  }
  out.add('$cur');
  return out.join('\r\n ');
}

/// The calendar file for an event; the times are RFC 3339 in UTC.
String eventIcs({
  required String uid,
  required String start,
  required String end,
  required String title,
  required String link,
  required String now,
}) {
  final lines = [
    'BEGIN:VCALENDAR',
    'VERSION:2.0',
    'PRODID:-//Churr//Churr//EN',
    'CALSCALE:GREGORIAN',
    'METHOD:PUBLISH',
    'BEGIN:VEVENT',
    'UID:${_text(uid)}',
    'DTSTAMP:${_stamp(now)}',
    'DTSTART:${_stamp(start)}',
    'DTEND:${_stamp(end)}',
    'SUMMARY:${_text(title)}',
    if (link.isNotEmpty) ...['DESCRIPTION:${_text(link)}', 'URL:$link'],
    'END:VEVENT',
    'END:VCALENDAR',
  ];
  return '${lines.map(_fold).join('\r\n')}\r\n';
}

/// The title an event gets: the message's first line, or "Event".
String eventTitle(String plain) {
  final line = plain
      .split('\n')
      .map((l) => l.trim())
      .firstWhere((l) => l.isNotEmpty, orElse: () => '');
  if (line.isEmpty) return 'Event';
  return line.length > 200 ? '${line.substring(0, 199)}…' : line;
}
