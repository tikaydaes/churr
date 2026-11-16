// Colours in messages (ruling R73): the value a `colour` entity carries,
// #rrggbb or #rrggbbaa, as a colour to draw.

import 'dart:ui';

/// The colour of `#rrggbb` or `#rrggbbaa`, or null.
Color? parseColour(String v) {
  if (!RegExp(r'^#([0-9a-f]{6}|[0-9a-f]{8})$').hasMatch(v)) return null;
  final rgb = int.parse(v.substring(1, 7), radix: 16);
  final a = v.length == 9 ? int.parse(v.substring(7), radix: 16) : 0xff;
  return Color((a << 24) | rgb);
}
