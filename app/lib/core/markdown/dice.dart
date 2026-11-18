// The grammar of a dice expression (internal/modules/dice Parse;
// docs/FEATURES.md section 12.4), for inline dice (ruling R73), which the
// app recognises in the composer while the server rolls them. /roll and
// /dice are not read here: the server's dice module reads what follows them.
// testdata/commands/dice.json holds the cases.

const _maxText = 100, _maxTerms = 10, _maxDice = 100;
const _maxSides = 1000, _maxNumber = 10000;

class _Term {
  _Term({this.count = 0, this.sides = 0, this.value = 0});
  int sign = 1;
  final int count, sides, value;
  String keep = '';
  int keepN = 0;
}

/// One term at the start of [s], and how many characters it took.
(_Term, int)? _term(String s) {
  for (final (word, keep) in const [('adv', 'kh'), ('dis', 'kl')]) {
    if (s.startsWith(word)) {
      return (
        _Term(count: 2, sides: 20)
          ..keep = keep
          ..keepN = 1,
        word.length,
      );
    }
  }
  var i = 0;
  bool digit(int k) =>
      k < s.length && s.codeUnitAt(k) >= 0x30 && s.codeUnitAt(k) <= 0x39;
  // (has, value), or null for a number too long.
  (bool, int)? num() {
    final start = i;
    while (digit(i)) {
      i++;
    }
    if (i == start) return (false, 0);
    if (i - start > 6) return null;
    return (true, int.parse(s.substring(start, i)));
  }

  final c = num();
  if (c == null) return null;
  if (i >= s.length || s[i] != 'd') {
    if (!c.$1 || c.$2 > _maxNumber) return null;
    return (_Term(value: c.$2), i);
  }
  i++;
  final count = c.$1 ? c.$2 : 1;
  if (count < 1 || count > _maxDice) return null;
  int sides;
  if (i < s.length && s[i] == '%') {
    sides = 100;
    i++;
  } else {
    final x = num();
    if (x == null || !x.$1) return null;
    sides = x.$2;
  }
  if (sides < 2 || sides > _maxSides) return null;
  final t = _Term(count: count, sides: sides);
  if (i < s.length &&
      (s[i] == 'k' ||
          (s[i] == 'd' &&
              i + 1 < s.length &&
              (s[i + 1] == 'h' || s[i + 1] == 'l')))) {
    var mode = s[i];
    i++;
    if (i < s.length && (s[i] == 'h' || s[i] == 'l')) {
      mode += s[i];
      i++;
    } else {
      mode += 'h';
    }
    final x = num();
    if (x == null) return null;
    final n = x.$1 ? x.$2 : 1;
    if (mode[0] == 'k' && (n < 1 || n > count)) return null;
    if (mode[0] == 'd' && (n < 1 || n >= count)) return null;
    t
      ..keep = mode
      ..keepN = n;
  }
  return (t, i);
}

/// An expression written out in full (`d20` is `1d20`), or null when
/// /roll would refuse it.
String? parseDice(String expr) {
  if (expr.runes.length > _maxText) return null;
  final fields = expr
      .toLowerCase()
      .split(RegExp(r'\s+'))
      .where((f) => f.isNotEmpty)
      .toList();
  for (var k = 1; k < fields.length; k++) {
    final a = fields[k - 1], b = fields[k];
    if (!'+-'.contains(a[a.length - 1]) && !'+-'.contains(b[0])) return null;
  }
  var s = fields.join();
  if (s.isEmpty) s = '1d20';
  final terms = <_Term>[];
  var dice = 0, i = 0;
  while (i < s.length) {
    var sign = 1;
    if (s[i] == '+') {
      i++;
    } else if (s[i] == '-') {
      sign = -1;
      i++;
    } else if (terms.isNotEmpty) {
      return null;
    }
    if (i >= s.length) return null;
    final r = _term(s.substring(i));
    if (r == null) return null;
    i += r.$2;
    r.$1.sign = sign;
    terms.add(r.$1);
    if (terms.length > _maxTerms) return null;
    dice += r.$1.count;
    if (dice > _maxDice) return null;
  }
  final out = StringBuffer();
  for (final (k, t) in terms.indexed) {
    out.write(t.sign < 0 ? '-' : (k > 0 ? '+' : ''));
    if (t.count == 0) {
      out.write(t.value);
    } else {
      out.write('${t.count}d${t.sides}');
      if (t.keep.isNotEmpty) out.write('${t.keep}${t.keepN}');
    }
  }
  return '$out';
}
