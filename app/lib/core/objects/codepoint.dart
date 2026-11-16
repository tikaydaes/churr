// Numbers in another base and code points (MESSAGE-FORMAT.md section
// 3.2): what their converters show. testdata/objects/codepoints.json holds
// the cases both clients pass.

import 'dart:convert';

String _group(String s, int n, String sep) {
  final out = StringBuffer();
  for (var i = 0; i < s.length; i++) {
    if (i > 0 && (s.length - i) % n == 0) out.write(sep);
    out.write(s[i]);
  }
  return out.toString();
}

/// A number's rows: decimal grouped with "," (before the locale's marks),
/// hexadecimal, octal, binary in groups of four, what each copies, and the
/// code point it is as a character, or null.
typedef NumberRows = ({
  String decimal,
  String hex,
  String octal,
  String binary,
  Map<String, String> copy,
  String? codepoint,
});

NumberRows numberRows(String value) {
  final n = BigInt.parse(value);
  var bin = n.toRadixString(2);
  bin = bin.padLeft((bin.length + 3) ~/ 4 * 4, '0');
  final hex = '0x${n.toRadixString(16).toUpperCase()}';
  final oct = '0o${n.toRadixString(8)}';
  final scalar =
      n <= BigInt.from(0x10ffff) &&
      !(n >= BigInt.from(0xd800) && n <= BigInt.from(0xdfff));
  return (
    decimal: _group(n.toString(), 3, ','),
    hex: hex,
    octal: oct,
    binary: '0b${_group(bin, 4, ' ')}',
    copy: {
      'decimal': n.toString(),
      'hex': hex,
      'octal': oct,
      'binary': '0b${n.toRadixString(2)}',
    },
    codepoint: scalar
        ? 'U+${n.toRadixString(16).toUpperCase().padLeft(4, '0')}'
        : null,
  );
}

const _c0 = [
  'NUL', 'SOH', 'STX', 'ETX', 'EOT', 'ENQ', 'ACK', 'BEL', 'BS', 'HT', 'LF', //
  'VT', 'FF', 'CR', 'SO', 'SI', 'DLE', 'DC1', 'DC2', 'DC3', 'DC4', 'NAK',
  'SYN', 'ETB', 'CAN', 'EM', 'SUB', 'ESC', 'FS', 'GS', 'RS', 'US',
];
const _short = {
  0x20: 'SP', 0x7f: 'DEL', 0x85: 'NEL', 0xa0: 'NBSP', 0xad: 'SHY', //
  0x200b: 'ZWSP', 0x200c: 'ZWNJ', 0x200d: 'ZWJ', 0x2028: 'LSEP',
  0x2029: 'PSEP', 0x202f: 'NNBSP', 0x2060: 'WJ', 0xfeff: 'BOM',
};
final _unseen = RegExp(r'^[\p{Cc}\p{Cf}\p{Z}\p{Co}\p{Cn}]$', unicode: true);
final _mark = RegExp(r'^\p{M}$', unicode: true);
String _hex(int v, int w) => v.toRadixString(16).toUpperCase().padLeft(w, '0');

/// What the converter shows of a code point (`U+1F600`). [shownKind] is
/// `char`, `mark` (a dotted circle and the mark) or `box` (a dotted box
/// holding [shown], a short name).
typedef CodePointFacts = ({
  String value,
  String char,
  String decimal,
  String utf8,
  String utf16,
  String html,
  String json,
  String js,
  String python,
  String shownKind,
  String shown,
});

CodePointFacts codePointFacts(String value) {
  final v = int.parse(value.replaceFirst('U+', ''), radix: 16);
  final ch = String.fromCharCode(v);
  final u16 = ch.codeUnits;
  final u8 = utf8.encode(ch);
  String kind, shown;
  if (_mark.hasMatch(ch)) {
    kind = 'mark';
    shown = '◌$ch';
  } else if (_unseen.hasMatch(ch)) {
    kind = 'box';
    shown = _short[v] ?? (v < 32 ? _c0[v] : 'U+${_hex(v, 4)}');
  } else {
    kind = 'char';
    shown = ch;
  }
  return (
    value: 'U+${_hex(v, 4)}',
    char: ch,
    decimal: '$v',
    utf8: u8.map((b) => _hex(b, 2)).join(' '),
    utf16: u16.map((u) => _hex(u, 4)).join(' '),
    html: '&#x${_hex(v, 1)};',
    json: u16.map((u) => '\\u${_hex(u, 4)}').join(),
    js: '\\u{${_hex(v, 1)}}',
    python: v < 0x10000 ? '\\u${_hex(v, 4)}' : '\\U${_hex(v, 8)}',
    shownKind: kind,
    shown: shown,
  );
}

/// A code point's name and general category, as the server gave them.
typedef CodePointName = ({String name, String category, String categoryName});

final Map<String, Future<CodePointName?>> _names = {};

/// Asks [lookup] (the server's lookupCodePoints) once a session for a code
/// point's name; null when it fails.
Future<CodePointName?> codePointName(
  String value,
  Future<CodePointName?> Function(String value)? lookup,
) {
  if (lookup == null) return Future.value(null);
  return _names.putIfAbsent(value, () {
    Future<CodePointName?> ask() async {
      try {
        final r = await lookup(value);
        if (r == null) _names.remove(value);
        return r;
      } on Object {
        _names.remove(value);
        return null;
      }
    }

    return ask();
  });
}
