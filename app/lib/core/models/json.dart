/// Small helpers for reading the API's JSON (docs/CONVENTIONS.md, JSON):
/// absent and `null` mean the same, unknown fields are ignored, times are
/// RFC 3339 in UTC.
library;

/// A JSON object.
typedef Json = Map<String, dynamic>;

/// Reads an RFC 3339 time, or null when it is absent or malformed.
DateTime? parseTime(Object? v) =>
    v is String ? DateTime.tryParse(v)?.toUtc() : null;

/// Writes a time the way the API does: UTC with milliseconds.
String formatTime(DateTime t) {
  final u = t.toUtc();
  String p(int n, [int w = 2]) => n.toString().padLeft(w, '0');
  return '${p(u.year, 4)}-${p(u.month)}-${p(u.day)}T'
      '${p(u.hour)}:${p(u.minute)}:${p(u.second)}.${p(u.millisecond, 3)}Z';
}

/// Reads an integer that JSON may have carried as a double.
int asInt(Object? v, [int fallback = 0]) =>
    v is int ? v : (v is num ? v.toInt() : fallback);

/// Reads a list of objects, skipping anything that is not one.
List<T> listOf<T>(Object? v, T Function(Json) parse) => [
  if (v is List)
    for (final e in v)
      if (e is Map) parse(e.cast<String, dynamic>()),
];

/// Reads a list of strings.
List<String> strings(Object? v) => [
  if (v is List)
    for (final e in v)
      if (e is String) e,
];

/// Reads a nested object, or null.
Json? object(Object? v) => v is Map ? v.cast<String, dynamic>() : null;
