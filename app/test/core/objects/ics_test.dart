// The calendar file of an event (ruling R73), against the cases the web
// chat passes too.

import 'dart:convert';
import 'dart:io';

import 'package:churr/core/objects/ics.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  final cases = jsonDecode(
    File('../testdata/objects/ics.json').readAsStringSync(),
  ) as Map<String, dynamic>;
  for (final c in cases['cases'] as List) {
    test('${c['name']}', () {
      final i = c['in'] as Map;
      expect(
        eventIcs(
          uid: i['uid'],
          start: i['start'],
          end: i['end'],
          title: i['title'],
          link: i['link'],
          now: i['now'],
        ),
        c['ics'],
      );
    });
  }
  for (final c in cases['titles'] as List) {
    test('the title of ${jsonEncode(c['plain'])}', () {
      expect(eventTitle('${c['plain']}'), c['title']);
    });
  }
}
