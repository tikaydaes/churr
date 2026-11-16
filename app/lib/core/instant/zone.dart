// Instants (ruling R69): turning a typed date and time into the moment it
// names, and a moment into a wall clock in any zone. The rules are the
// server's (internal/instant); this file runs testdata/instants/resolve.json.
// The zone database is the `timezone` package's.

import 'package:timezone/data/latest_all.dart' as tzdata;
import 'package:timezone/timezone.dart' as tz;

import '../markdown/nodes.dart';
import '../markdown/parser.dart';

/// Where the app is: the device's IANA zone, set at start
/// (lib/platform/account_start.dart), and the clock. Tests set both.
class InstantEnv {
  static String? deviceZone;
  static DateTime Function() now = DateTime.now;

  /// The zone a reader sees instants in: the device's, or UTC when the
  /// platform will not say.
  static String get viewerZone {
    final z = deviceZone;
    return z != null && isZoneName(z) ? z : 'UTC';
  }
}

bool _loaded = false;
void _ensureZones() {
  if (_loaded) return;
  tzdata.initializeTimeZones();
  _loaded = true;
}

const _day = 86400000;

tz.Location? _location(String z) {
  _ensureZones();
  try {
    return tz.getLocation(z);
  } on Object {
    return null;
  }
}

/// Whether [z] is an IANA name the zone database knows, held to the
/// server's rule that every part starts with a capital letter.
bool isZoneName(String? z) {
  if (z == null ||
      z.isEmpty ||
      z == 'Local' ||
      z.length > 64 ||
      z.contains('..')) {
    return false;
  }
  final part = RegExp(r'^[A-Z][A-Za-z0-9_+-]*$');
  if (!z.split('/').every(part.hasMatch)) return false;
  return _location(z) != null;
}

/// A zone written as an offset ("Z", "+10:00", "+1000", "-05") in
/// milliseconds east of UTC, or null.
int? offsetOf(String? z) {
  if (z == 'Z') return 0;
  final m = RegExp(r'^([+-])(\d{2})(?::?(\d{2}))?$').firstMatch(z ?? '');
  if (m == null) return null;
  final h = int.parse(m[2]!), mi = m[3] == null ? 0 : int.parse(m[3]!);
  if (h > 14 || mi > 59) return null;
  return (m[1] == '-' ? -1 : 1) * (h * 3600 + mi * 60) * 1000;
}

bool validZone(String? z) => offsetOf(z) != null || isZoneName(z);

/// Every zone name the database knows, for the converter's search.
List<String> allZones() {
  _ensureZones();
  return tz.timeZoneDatabase.locations.keys.where(isZoneNameShape).toList()
    ..sort();
}

bool isZoneNameShape(String z) =>
    z.split('/').every(RegExp(r'^[A-Z][A-Za-z0-9_+-]*$').hasMatch);

/// The offset of [zone] (an IANA name or an offset) at the moment [ms].
int offsetAt(String zone, int ms) {
  final off = offsetOf(zone);
  if (off != null) return off;
  final loc = _location(zone);
  return loc == null ? 0 : loc.timeZone(ms).offset.inMilliseconds;
}

/// A wall clock: the fields of a moment as [zone]'s clocks show it, with
/// [wd] 0 for Monday to 6 for Sunday.
class Wall {
  const Wall(this.y, this.mo, this.d, this.h, this.mi, this.s, this.wd);
  final int y, mo, d, h, mi, s, wd;
}

Wall wallIn(int ms, String zone) {
  final t = DateTime.fromMillisecondsSinceEpoch(
    ms + offsetAt(zone, ms),
    isUtc: true,
  );
  return Wall(
    t.year,
    t.month,
    t.day,
    t.hour,
    t.minute,
    t.second,
    t.weekday - 1,
  );
}

/// The milliseconds of a UTC date and time, any year from 1.
int utcMs(
  int y,
  int mo,
  int d, [
  int h = 0,
  int mi = 0,
  int s = 0,
  int ms = 0,
]) => DateTime.utc(y, mo, d, h, mi, s, ms).millisecondsSinceEpoch;

/// The moment at which [zone]'s clocks show the wall time [w] (UTC
/// milliseconds of the wall fields): null for a time the clocks skip, the
/// first of two for a time they show twice.
int? fromWall(int w, String zone) {
  int? best;
  final seen = <int>{};
  for (final probe in [w - _day, w, w + _day]) {
    final off = offsetAt(zone, probe);
    if (!seen.add(off)) continue;
    final u = w - off;
    if (offsetAt(zone, u) != off) continue;
    if (best == null || u < best) best = u;
  }
  return best;
}

String _pad(int n, [int k = 2]) => n.toString().padLeft(k, '0');

/// A moment as the API writes times: UTC with milliseconds.
String iso(int ms) {
  final d = DateTime.fromMillisecondsSinceEpoch(ms, isUtc: true);
  return '${_pad(d.year, 4)}-${_pad(d.month)}-${_pad(d.day)}T${_pad(d.hour)}:${_pad(d.minute)}:${_pad(d.second)}.${_pad(d.millisecond, 3)}Z';
}

/// An instant entity's fields.
class Resolved {
  const Resolved(this.time, this.precision, this.zone);
  final String time, precision, zone;
  Map<String, Object?> toJson() => {
    'time': time,
    'precision': precision,
    'zone': zone,
  };
}

/// The instant of a typed local date and time ("YYYY-MM-DDTHH:MM[:SS[.f]]")
/// in [zone] as written, or in [authorZone] when none was; null when it
/// stays text.
Resolved? resolveInstant(String local, String zone, String? authorZone) {
  final m = RegExp(
    r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2})(?:\.(\d{1,9}))?)?$',
  ).firstMatch(local);
  if (m == null) return null;
  final y = int.parse(m[1]!), mo = int.parse(m[2]!), d = int.parse(m[3]!);
  final h = int.parse(m[4]!), mi = int.parse(m[5]!);
  final s = m[6] == null ? 0 : int.parse(m[6]!);
  final ms = m[7] == null ? 0 : int.parse('${m[7]}00'.substring(0, 3));
  if (y < 1 || mo < 1 || mo > 12 || d < 1) return null;
  final leap = (y % 4 == 0 && y % 100 != 0) || y % 400 == 0;
  final days = [
    31,
    leap ? 29 : 28,
    31,
    30,
    31,
    30,
    31,
    31,
    30,
    31,
    30,
    31,
  ][mo - 1];
  if (d > days || h > 23 || mi > 59 || s > 59) return null;
  final precision = m[6] == null ? 'minute' : 'second';
  var z = zone;
  if (z.isEmpty) {
    if (!isZoneName(authorZone)) return null;
    z = authorZone!;
  }
  final wall = utcMs(y, mo, d, h, mi, s, ms);
  final off = offsetOf(z);
  if (off != null) return Resolved(iso(wall - off), precision, z);
  if (!isZoneName(z)) return null;
  final u = fromWall(wall, z);
  return u == null ? null : Resolved(iso(u), precision, z);
}

/// The typed instants of a text that may become entities: every instant
/// node of its tree outside checklist items and link text.
List<Inline> instantNodes(String text) {
  final out = <Inline>[];
  void inl(List<Inline> xs) {
    for (final s in xs) {
      if (s.kind == InlineKind.instant) {
        out.add(s);
      } else if (s.kind != InlineKind.link && s.kind != InlineKind.noLink) {
        inl(s.children);
      }
    }
  }

  void blk(List<Block> bs) {
    for (final b in bs) {
      inl(b.inlines);
      for (final it in b.items) {
        if (!it.task) inl(it.inlines);
        blk(it.lists);
      }
      for (final row in [b.head, ...b.rows]) {
        for (final c in row) {
          inl(c.inlines);
        }
      }
      blk(b.blocks);
    }
  }

  blk(parseMarkdown(text).blocks);
  return out;
}

/// The typed instants of [text] resolved as the server would, for a sealed
/// chat, where the sending device works out the entities. One the author
/// kept as plain text (an object hint of kind `plain`, ruling R73) stays
/// text.
List<Map<String, Object?>> instantEntities(
  String text,
  String? authorZone, {
  List<Map<String, dynamic>> objectHints = const [],
}) => [
  for (final n in instantNodes(text))
    if (!objectHints.any(
      (h) =>
          h['kind'] == 'plain' &&
          n.at < (h['end'] as int) &&
          (h['start'] as int) < n.end,
    ))
      if (resolveInstant(n.local, n.zone, authorZone) case final r?)
        {'type': 'instant', 'at': n.at, 'len': n.len, ...r.toJson()},
];
