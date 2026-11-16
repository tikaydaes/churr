// The objects recognised inside runs of plain text (ruling R73,
// docs/MESSAGE-FORMAT.md section 3.2): the server's reading
// (internal/markdown/objects.go) for the two places the app needs it: a
// sealed chat, where the sending device works out the entities, and the
// composer, which offers each object as it is typed. The shared vectors in
// testdata/markdown say what it must find.
//
// Objects are not nodes of the tree. Each lies inside one text span, so
// none is found in code, a link's text or address, a bare link, an escape,
// across formatting marks or in a checklist item.

import '../models/json.dart';
import '../objects/units.dart';
import 'dice.dart';
import 'nodes.dart';
import 'parser.dart';

/// The kinds, in the order they are tried at a position.
const objectKinds = [
  'message_link',
  'reference',
  'measure',
  'colour',
  'phone',
  'email',
  'geo',
  'dice',
];

/// The longest value a reference may have, and the most hints read.
const maxRefValue = 64;
const maxObjectHints = 50;
const _refDigits = '[0-9]{1,12}';

/// The kind of a hint that keeps a range as typed.
const objectPlain = 'plain';

bool _isWhite(int r) =>
    (r >= 0x09 && r <= 0x0D) ||
    r == 0x20 ||
    r == 0x85 ||
    r == 0xA0 ||
    r == 0x1680 ||
    (r >= 0x2000 && r <= 0x200A) ||
    r == 0x2028 ||
    r == 0x2029 ||
    r == 0x202F ||
    r == 0x205F ||
    r == 0x3000;

final _wordRe = RegExp(r'^[\p{L}\p{N}]$', unicode: true);
bool _isWord(int r) => _wordRe.hasMatch(String.fromCharCode(r));

final _left = '([{"\'~*_|>,;:=“‘«—–'.runes.toSet();
final _right = ')]}"\'*_~|<”’»…—–!?'.runes.toSet();
final _soft = '.,;:'.runes.toSet();

int _before(String s, int i) {
  if (i <= 0) return -1;
  final c = s.codeUnitAt(i - 1);
  if (c >= 0xDC00 && c <= 0xDFFF && i >= 2) {
    final hi = s.codeUnitAt(i - 2);
    if (hi >= 0xD800 && hi <= 0xDBFF) {
      return 0x10000 + ((hi - 0xD800) << 10) + (c - 0xDC00);
    }
  }
  return c;
}

int _at(String s, int i) {
  if (i >= s.length) return -1;
  final c = s.codeUnitAt(i);
  if (c >= 0xD800 && c <= 0xDBFF && i + 1 < s.length) {
    final lo = s.codeUnitAt(i + 1);
    if (lo >= 0xDC00 && lo <= 0xDFFF) {
      return 0x10000 + ((c - 0xD800) << 10) + (lo - 0xDC00);
    }
  }
  return c;
}

/// Whether an object may start at [i].
bool leftEdge(String s, int i) {
  final c = _before(s, i);
  return c < 0 || _isWhite(c) || _left.contains(c);
}

/// Whether an object may end at [j].
bool rightEdge(String s, int j) {
  final c = _at(s, j);
  if (c < 0 || _isWhite(c) || _right.contains(c)) return true;
  if (_soft.contains(c)) {
    final n = _at(s, j + 1);
    return n < 0 || !_isWord(n);
  }
  return false;
}

/// A channel's patterns before the server's, then the longer prefix, then
/// the older.
List<Json> orderPatterns(List<Json> list) {
  final out = [...list];
  out.sort((x, y) {
    final cx = (x['conversation_id'] ?? '') != '';
    final cy = (y['conversation_id'] ?? '') != '';
    if (cx != cy) return cx ? -1 : 1;
    final px = '${x['prefix']}', py = '${y['prefix']}';
    if (px.length != py.length) return py.length - px.length;
    final c = '${x['created_at']}'.compareTo('${y['created_at']}');
    if (c != 0) return c;
    return '${x['id']}'.compareTo('${y['id']}');
  });
  return out;
}

final _compiled = <String, RegExp?>{};
RegExp? _expression(String expr) => _compiled.putIfAbsent(expr, () {
  try {
    return RegExp('^(?:$expr)');
  } on FormatException {
    return null;
  }
});

/// The address a value resolves to.
String refURL(Json p, String value) =>
    '${p['url_template']}'.split('{value}').join(value);

String _asciiRun(String s, int i, int b, [int max = maxRefValue]) {
  final end = b < i + max ? b : i + max;
  var k = i;
  while (k < end && s.codeUnitAt(k) < 0x80) {
    k++;
  }
  return s.substring(i, k);
}

Json? _reference(String s, int i, int b, List<Json> patterns) {
  for (final p in patterns) {
    final prefix = '${p['prefix'] ?? ''}';
    final n = prefix.length;
    if (n == 0 || i + n >= b || !s.startsWith(prefix, i)) continue;
    final expr = '${p['expression'] ?? ''}';
    final re = _expression(expr.isEmpty ? _refDigits : expr);
    if (re == null) continue;
    final value = re.firstMatch(_asciiRun(s, i + n, b))?.group(0) ?? '';
    if (value.isEmpty || !rightEdge(s, i + n + value.length)) continue;
    return {
      'type': 'reference',
      'at': i,
      'len': n + value.length,
      'url': refURL(p, value),
      'pattern_id': p['id'],
      'value': value,
    };
  }
  return null;
}

// Message links: the server's origin, or churr:// and its host, then
// /chat/c/{conversation} and /m/{message}, /t/{thread} or
// /t/{thread}/m/{reply}, with nothing after.
final _id = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
bool _validId(String s) => _id.hasMatch(s);

String _slice(String s, int a, int b) =>
    a >= s.length ? '' : s.substring(a, b > s.length ? s.length : b);

(String, String, int)? _messagePath(String p) {
  const c = '/chat/c/';
  if (!p.startsWith(c) || !_validId(_slice(p, c.length, c.length + 36))) {
    return null;
  }
  final conv = p.substring(c.length, c.length + 36);
  final rest = p.substring(c.length + 36);
  String? part(String s, String mark) =>
      s.startsWith(mark) && _validId(_slice(s, mark.length, mark.length + 36))
      ? s.substring(mark.length, mark.length + 36)
      : null;
  const n = c.length + 36;
  final m = part(rest, '/m/');
  if (m != null) return (conv, m, n + 39);
  final t = part(rest, '/t/');
  if (t != null) {
    final reply = part(rest.substring(39), '/m/');
    return reply != null ? (conv, reply, n + 78) : (conv, t, n + 39);
  }
  return null;
}

int _linkStart(String s, String origin) {
  if (origin.isEmpty) return 0;
  if (_slice(s, 0, origin.length).toLowerCase() == origin) return origin.length;
  final k = origin.indexOf('://');
  if (k > 0) {
    final app = 'churr://${origin.substring(k + 3)}';
    if (_slice(s, 0, app.length).toLowerCase() == app) return app.length;
  }
  return 0;
}

/// Reads a whole address as a link to a message of the server at
/// [origin]: (conversation, message), or null.
(String, String)? messageLink(String link, String origin) {
  final o = origin.toLowerCase();
  final at = _linkStart(link, o);
  if (at == 0) return null;
  final p = _messagePath(link.substring(at));
  return p != null && at + p.$3 == link.length ? (p.$1, p.$2) : null;
}

Json? _churrLink(String s, int i, int b, String origin) {
  if (origin.isEmpty ||
      !origin.contains('://') ||
      !s.startsWith('churr://', i) ||
      i + 8 > b) {
    return null;
  }
  final run = _asciiRun(s, i, b, origin.length + 200);
  final at = _linkStart(run, origin);
  if (at == 0) return null;
  final p = _messagePath(run.substring(at));
  if (p == null || !rightEdge(s, i + at + p.$3)) return null;
  return {
    'type': 'message_link',
    'at': i,
    'len': at + p.$3,
    'conversation_id': p.$1,
    'url': run.substring(0, at + p.$3),
    'message_id': p.$2,
  };
}

/// What the scanner reads with.
class _Ctx {
  _Ctx(this.patterns, this.origin, [this.dice = true]);
  final List<Json> patterns;
  final String origin;

  /// False where inline dice are not rolled (a sealed chat).
  final bool dice;
}

Json? _objectAt(String s, int i, int b, _Ctx ctx, String only) {
  bool tryKind(String k) => only.isEmpty || only == k;
  final patterns = ctx.patterns;
  if (tryKind('message_link')) {
    final e = _churrLink(s, i, b, ctx.origin);
    if (e != null) return e;
  }
  if (tryKind('reference')) {
    final e = _reference(s, i, b, patterns);
    if (e != null) return e;
  }
  if (tryKind('measure')) {
    final e = _measure(s, i, b);
    if (e != null) return e;
  }
  if (tryKind('colour')) {
    final e = _colour(s, i, b);
    if (e != null) return e;
  }
  if (tryKind('phone')) {
    final e = _phone(s, i, b);
    if (e != null) return e;
  }
  if (tryKind('email')) {
    final e = _email(s, i, b);
    if (e != null) return e;
  }
  if (tryKind('geo')) {
    final e = _geo(s, i, b);
    if (e != null) return e;
  }
  if (tryKind('dice') && ctx.dice) {
    final e = _dice(s, i, b);
    if (e != null) return e;
  }
  return null;
}

/// Inline dice: `[[`, an expression of /roll's grammar with no bracket or
/// line break, and `]]`. The server rolls them; the app only reads the
/// shape (value: the roll written out in full).
Json? _dice(String s, int i, int b) {
  if (!s.startsWith('[[', i)) return null;
  var k = i + 2;
  while (k < b &&
      k - i - 2 <= 100 &&
      s[k] != ']' &&
      s[k] != '[' &&
      s[k] != '\n') {
    k++;
  }
  final expr = s.substring(i + 2, k);
  if (k + 1 >= b ||
      s[k] != ']' ||
      s[k + 1] != ']' ||
      expr.trim().isEmpty ||
      !rightEdge(s, k + 2)) {
    return null;
  }
  final value = parseDice(expr);
  return value == null
      ? null
      : {'type': 'dice', 'at': i, 'len': k + 2 - i, 'value': value};
}

/// [-]d.d at [j]: at most [whole] digits before the point, [min] to 8
/// after, no more than [max].
(String, int)? _degrees(String s, int j, int b, int whole, int min, num max) {
  var k = j;
  if (k < b && s[k] == '-') k++;
  final d = k;
  while (k < b && _digit(s, k)) {
    k++;
  }
  if (k == d || k - d > whole || k + 1 >= b || s[k] != '.') return null;
  k++;
  final f = k;
  while (k < b && _digit(s, k)) {
    k++;
  }
  if (k - f < min || k - f > 8) return null;
  final v = s.substring(j, k);
  return double.parse(v).abs() <= max ? (v, k) : null;
}

/// Map coordinates: `lat, lon` with at least three decimals each, or
/// `geo:lat,lon` with any; latitude at most 90, longitude 180.
Json? _geo(String s, int i, int b) {
  var j = i, min = 3;
  if (s.startsWith('geo:', i) && i + 4 <= b) {
    j = i + 4;
    min = 1;
  } else {
    var p = i;
    if (p > 0 && s[p - 1] == ' ') p--;
    if (p >= 2 && (s[p - 1] == '.' || s[p - 1] == ',') && _digit(s, p - 2)) {
      return null;
    }
  }
  final lat = _degrees(s, j, b, 2, min, 90);
  if (lat == null || lat.$2 >= b || s[lat.$2] != ',') return null;
  var k = lat.$2 + 1;
  if (min == 3 && k < b && s[k] == ' ') k++;
  final lon = _degrees(s, k, b, 3, min, 180);
  if (lon == null || !rightEdge(s, lon.$2)) return null;
  if (lon.$2 < b && s[lon.$2] == ',') {
    var n = lon.$2 + 1;
    if (n < b && s[n] == ' ') n++;
    if (n < b && (_digit(s, n) || s[n] == '-')) return null;
  }
  return {
    'type': 'geo',
    'at': i,
    'len': lon.$2 - i,
    'lat': lat.$1,
    'lon': lon.$1,
  };
}

/// Telephone numbers: `+` and 7 to 15 digits, the first not 0, in groups
/// one space, hyphen or dot may separate, a group optionally in brackets;
/// a bracketed lone 0 is not dialled. The value is `+` and the digits.
Json? _phone(String s, int i, int b) {
  bool dig(int k) => k < b && _digit(s, k);
  if (s[i] != '+' || !dig(i + 1) || s[i + 1] == '0') return null;
  final digits = StringBuffer('+');
  var j = i + 1, last = i + 1;
  while (j < b && j - i <= 32) {
    var k = j;
    if (k > i + 1 && (s[k] == ' ' || s[k] == '-' || s[k] == '.')) k++;
    final open = k < b && s[k] == '(' && k > i + 1;
    if (open) k++;
    final start = k;
    while (dig(k)) {
      k++;
    }
    if (k == start || (open && (k >= b || s[k] != ')'))) break;
    var group = s.substring(start, k);
    if (open) {
      k++;
      if (group == '0') group = '';
    }
    digits.write(group);
    j = k;
    last = k;
  }
  final n = digits.length - 1;
  if (n < 7 || n > 15 || !rightEdge(s, last)) return null;
  return {'type': 'phone', 'at': i, 'len': last - i, 'value': '$digits'};
}

final _localRe = RegExp(r'^[A-Za-z0-9._%+-]$');
final _labelRe = RegExp(r'^[A-Za-z0-9-]$');
final _alnumRe = RegExp(r'^[A-Za-z0-9]$');

/// Email addresses: an ASCII local part, `@`, and a domain of two or more
/// labels, the last all letters and at least two of them.
Json? _email(String s, int i, int b) {
  var k = i;
  while (k < b && _localRe.hasMatch(s[k])) {
    k++;
  }
  final local = s.substring(i, k);
  if (k == i ||
      k - i > 64 ||
      k >= b ||
      s[k] != '@' ||
      local.startsWith('.') ||
      local.endsWith('.') ||
      local.contains('..')) {
    return null;
  }
  final d = k + 1;
  var end = d, labels = 0, lastStart = d;
  for (;;) {
    final st = end;
    while (end < b && _labelRe.hasMatch(s[end])) {
      end++;
    }
    if (end == st || s[st] == '-' || s[end - 1] == '-') return null;
    labels++;
    lastStart = st;
    if (end + 1 < b && s[end] == '.' && _alnumRe.hasMatch(s[end + 1])) {
      end++;
      continue;
    }
    break;
  }
  final tld = s.substring(lastStart, end);
  if (labels < 2 ||
      tld.length < 2 ||
      end - d > 253 ||
      !RegExp(r'^[A-Za-z]+$').hasMatch(tld) ||
      !rightEdge(s, end)) {
    return null;
  }
  return {
    'type': 'email',
    'at': i,
    'len': end - i,
    'value': s.substring(i, end),
  };
}

final _hex = RegExp(r'^[0-9a-fA-F]$');

/// Colours: `#` and 3, 6 or 8 hex digits; the value is #rrggbb[aa] in
/// lower case.
Json? _colour(String s, int i, int b) {
  if (s[i] != '#') return null;
  var k = i + 1;
  while (k < b && k - i <= 9 && _hex.hasMatch(s[k])) {
    k++;
  }
  final n = k - i - 1;
  if ((n != 3 && n != 6 && n != 8) || !rightEdge(s, k)) return null;
  var hex = s.substring(i + 1, k).toLowerCase();
  // Three decimal digits are an issue number far more often than a colour.
  if (n == 3 && RegExp(r'^[0-9]+$').hasMatch(hex)) return null;
  if (n == 3) hex = '${hex[0]}${hex[0]}${hex[1]}${hex[1]}${hex[2]}${hex[2]}';
  return {'type': 'colour', 'at': i, 'len': k - i, 'value': '#$hex'};
}

/// Measurements: a number, at most one space, and a unit of the fixed
/// table written exactly as listed; only a temperature may be negative.
/// The table is tools/units-gen/gen.py's (../objects/units.dart).

bool _digit(String s, int i) {
  if (i < 0 || i >= s.length) return false;
  final c = s.codeUnitAt(i);
  return c >= 0x30 && c <= 0x39;
}

Json? _measure(String s, int i, int b) {
  var j = i;
  final neg = j < b && (s[j] == '-' || s[j] == '\u2212');
  if (neg) j++;
  if (i >= 2 && (s[i - 1] == '.' || s[i - 1] == ',') && _digit(s, i - 2)) {
    return null;
  }
  var k = j;
  while (k < b && _digit(s, k)) {
    k++;
  }
  if (k == j || k - j > 9) return null;
  if (k + 1 < b && s[k] == '.' && _digit(s, k + 1)) {
    var f = k + 1;
    while (f < b && _digit(s, f)) {
      f++;
    }
    if (f - k - 1 > 6) return null;
    k = f;
  }
  if (k < b &&
      (_digit(s, k) ||
          ((s[k] == '.' || s[k] == ',') && k + 1 < b && _digit(s, k + 1)))) {
    return null;
  }
  final value = '${neg ? '-' : ''}${s.substring(j, k)}';
  var u = k;
  final spaced = u < b && (s[u] == ' ' || s[u] == '\u00a0' || s[u] == '\u202f');
  if (spaced) u++;
  for (final (typed, code, sp, temp) in measureUnits) {
    if ((spaced && !sp) || (neg && !temp)) continue;
    if (u + typed.length > b ||
        !s.startsWith(typed, u) ||
        !rightEdge(s, u + typed.length)) {
      continue;
    }
    return {
      'type': 'measure',
      'at': i,
      'len': u + typed.length - i,
      'value': value,
      'unit': code,
    };
  }
  return null;
}

/// The runs of plain text objects may be found in, as [start, end).
List<(int, int)> textRuns(MarkdownDoc doc) {
  final out = <(int, int)>[];
  void inl(List<Inline> xs) {
    for (final n in xs) {
      if (n.kind == InlineKind.text) {
        out.add((n.at, n.at + n.len));
      } else if (n.kind != InlineKind.link && n.kind != InlineKind.noLink) {
        inl(n.children);
      }
    }
  }

  void blk(List<Block> bs) {
    for (final b in bs) {
      switch (b.kind) {
        case BlockKind.paragraph:
        case BlockKind.heading:
          inl(b.inlines);
        case BlockKind.quote:
          blk(b.blocks);
        case BlockKind.list:
          for (final it in b.items) {
            if (!it.task) inl(it.inlines);
            blk(it.lists);
          }
        case BlockKind.table:
          for (final c in b.head) {
            inl(c.inlines);
          }
          for (final row in b.rows) {
            for (final c in row) {
              inl(c.inlines);
            }
          }
        default:
      }
    }
  }

  blk(doc.blocks);
  return out;
}

/// Hints are { start, end, kind } maps.
bool _hinted(List<Json> hints, Json e) => hints.any(
  (h) =>
      h['kind'] == e['type'] &&
      h['start'] == e['at'] &&
      h['end'] == (e['at'] as int) + (e['len'] as int),
);

/// The object entities of [text], in order of `at`, given the reference
/// [patterns] that apply, the author's object [hints] and the server's
/// own [origin], for links to its messages.
List<Json> findObjects(
  String text, {
  List<Json> patterns = const [],
  List<Json> hints = const [],
  String origin = '',
  bool dice = true,
  MarkdownDoc? doc,
}) {
  final d = doc ?? parseMarkdown(text);
  final ps = _Ctx(orderPatterns(patterns), origin.toLowerCase(), dice);
  final hs = hints.length > maxObjectHints
      ? hints.sublist(0, maxObjectHints)
      : hints;
  final out = <Json>[];
  for (final (a, b) in textRuns(d)) {
    var i = a;
    while (i < b) {
      var only = '';
      var skip = 0;
      for (final h in hs) {
        final s = h['start'] as int, e = h['end'] as int;
        if (h['kind'] == objectPlain && i >= s && i < e) {
          skip = e;
        } else if (h['kind'] != objectPlain && s == i) {
          only = '${h['kind']}';
        }
      }
      if (skip > i) {
        i = skip;
        continue;
      }
      if (leftEdge(text, i)) {
        Json? e;
        if (only.isNotEmpty) {
          e = _objectAt(text, i, b, ps, only);
          if (e != null && !_hinted(hs, e)) e = null;
        }
        e ??= _objectAt(text, i, b, ps, '');
        if (e != null &&
            plainCovers(
              hs,
              e['at'] as int,
              (e['at'] as int) + (e['len'] as int),
            )) {
          e = null;
        }
        if (e != null) {
          out.add(e);
          i = (e['at'] as int) + (e['len'] as int);
          continue;
        }
      }
      i++;
    }
  }
  // A bare link to one of the server's messages is a message link, unless
  // the author kept it plain.
  for (final n in _bareLinks(d)) {
    final m = messageLink(n.url, ps.origin);
    if (m != null && !plainCovers(hs, n.at, n.at + n.len)) {
      out.add({
        'type': 'message_link',
        'at': n.at,
        'len': n.len,
        'conversation_id': m.$1,
        'url': n.url,
        'message_id': m.$2,
      });
    }
  }
  out.sort((x, y) => (x['at'] as int).compareTo(y['at'] as int));
  return out;
}

/// The bare links of a document, where the server finds them.
List<Inline> _bareLinks(MarkdownDoc doc) {
  final out = <Inline>[];
  void inl(List<Inline> xs) {
    for (final n in xs) {
      if (n.kind == InlineKind.bareLink) {
        out.add(n);
      } else if (n.kind != InlineKind.link &&
          n.kind != InlineKind.noLink &&
          n.kind != InlineKind.code) {
        inl(n.children);
      }
    }
  }

  void blk(List<Block> bs) {
    for (final b in bs) {
      switch (b.kind) {
        case BlockKind.paragraph:
        case BlockKind.heading:
          inl(b.inlines);
        case BlockKind.quote:
          blk(b.blocks);
        case BlockKind.list:
          for (final it in b.items) {
            if (!it.task) inl(it.inlines);
            blk(it.lists);
          }
        case BlockKind.table:
          for (final c in b.head) {
            inl(c.inlines);
          }
          for (final row in b.rows) {
            for (final c in row) {
              inl(c.inlines);
            }
          }
        default:
      }
    }
  }

  blk(doc.blocks);
  return out;
}

/// Every reading of the text at [i], one per kind, in the order the kinds
/// are tried: the composer offers each as a row.
List<Json> readingsAt(
  String text,
  int i, {
  List<Json> patterns = const [],
  String origin = '',
  bool dice = true,
}) {
  final run = textRuns(parseMarkdown(text))
      .where((r) => i >= r.$1 && i < r.$2)
      .firstOrNull;
  if (run == null || !leftEdge(text, i)) return const [];
  final ps = _Ctx(orderPatterns(patterns), origin.toLowerCase(), dice);
  return [for (final k in objectKinds) _objectAt(text, i, run.$2, ps, k)]
      .whereType<Json>()
      .toList();
}

/// Whether a plain hint reaches into [a, b).
bool plainCovers(List<Json> hints, int a, int b) => hints.any(
  (h) =>
      h['kind'] == objectPlain &&
      a < (h['end'] as int) &&
      (h['start'] as int) < b,
);
