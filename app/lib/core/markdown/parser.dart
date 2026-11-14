/// The Churr Markdown parser: a port of the server's `internal/markdown`
/// (block.go, inline.go, chars.go), kept in the same shape so a change to
/// one can be carried to the other line by line. The shared corpus in
/// `testdata/markdown` holds both to the same answers.
///
/// Dart strings are indexed in UTF-16 code units, which is the unit the
/// format counts positions in, so the text is parsed in place.
library;

import '../highlight/languages.dart';
import 'nodes.dart';

/// Parses a message's text into its block tree.
MarkdownDoc parseMarkdown(String text) {
  final p = _Parser(text);
  return MarkdownDoc(p.blocks(_splitLines(text), 0));
}

/// Parses text that may hold inline constructs only (a card field's
/// value): the whole text is one run of spans.
List<Inline> parseInline(String text) => _Parser(text).inline(0, text.length);

const _bt = 0x60; // `
const _bs = 0x5C; // \

bool _isBlank(int c) => c == 0x20 || c == 0x09;
bool _isDigit(int c) => c >= 0x30 && c <= 0x39;
bool _isLowerAlnum(int c) => (c >= 0x61 && c <= 0x7A) || _isDigit(c);
bool _isHigh(int c) => c >= 0xD800 && c <= 0xDBFF;
bool _isLow(int c) => c >= 0xDC00 && c <= 0xDFFF;

/// ASCII punctuation: what a backslash may escape.
bool _isPunct(int c) =>
    (c >= 0x21 && c <= 0x2F) ||
    (c >= 0x3A && c <= 0x40) ||
    (c >= 0x5B && c <= 0x60) ||
    (c >= 0x7B && c <= 0x7E);

/// White space as Go's unicode.IsSpace has it, so the three parsers agree
/// on what opens and closes emphasis.
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

/// A letter or digit in any script.
bool _isWord(int r) {
  if (r < 0x80) {
    return _isDigit(r) || (r >= 0x41 && r <= 0x5A) || (r >= 0x61 && r <= 0x7A);
  }
  // A lone surrogate is not a character and so not a word character.
  if (r >= 0xD800 && r <= 0xDFFF) return false;
  return _wordRe.hasMatch(String.fromCharCode(r));
}

class _Line {
  _Line(this.start, this.end, this.eol, this.next);

  /// First unit of the line, after any prefix a container took.
  int start;

  /// After the last unit that is not a trailing space or tab.
  final int end;

  /// Where the line ending starts, and where the next line starts.
  final int eol;
  final int next;

  bool get blank => start >= end;
  _Line copy() => _Line(start, end, eol, next);
}

List<_Line> _splitLines(String src) {
  final lines = <_Line>[];
  var start = 0;
  while (start < src.length) {
    var eol = start;
    while (eol < src.length &&
        src.codeUnitAt(eol) != 0x0A &&
        src.codeUnitAt(eol) != 0x0D) {
      eol++;
    }
    var next = eol;
    if (next < src.length) {
      next++;
      if (src.codeUnitAt(eol) == 0x0D &&
          next < src.length &&
          src.codeUnitAt(next) == 0x0A) {
        next++;
      }
    }
    var end = eol;
    while (end > start && _isBlank(src.codeUnitAt(end - 1))) {
      end--;
    }
    lines.add(_Line(start, end, eol, next));
    start = next;
  }
  return lines;
}

class _RawItem {
  int indent = 0;
  int depth = 0;
  bool ordered = false;
  int number = 0;
  int at = 0;
  int text = 0;
  int end = 0;
  bool task = false;
  bool checked = false;
}

class _Range {
  _Range(this.start, this.end);
  final int start;
  final int end;
}

class _Parser {
  _Parser(this.src);
  final String src;

  int u(int i) => src.codeUnitAt(i);

  List<Block> blocks(List<_Line> lines, int quotes) {
    final out = <Block>[];
    var para = <_Line>[];
    void flush() {
      if (para.isNotEmpty) {
        out.add(paragraph(para));
        para = [];
      }
    }

    // closeAt[i] is the nearest closing fence at or after line i, so an
    // opening fence with no close is found in one pass and shown as typed.
    final closeAt = List<int>.filled(lines.length + 1, -1);
    for (var i = lines.length - 1; i >= 0; i--) {
      closeAt[i] = isFenceClose(lines[i]) ? i : closeAt[i + 1];
    }

    for (var i = 0; i < lines.length;) {
      final ln = lines[i];
      if (ln.blank) {
        flush();
        i++;
        continue;
      }
      final c = u(ln.start);
      if (c == _bt) {
        final lang = fenceOpen(ln);
        if (lang != null && closeAt[i + 1] >= 0) {
          flush();
          final end = closeAt[i + 1];
          out.add(codeBlock(lines.sublist(i, end + 1), lang));
          i = end + 1;
          continue;
        }
      } else if (c == 0x2D && isRule(ln)) {
        flush();
        out.add(Block(BlockKind.rule, ln.start, ln.end - ln.start));
        i++;
        continue;
      } else if (c == 0x23) {
        final h = heading(ln);
        if (h != null) {
          flush();
          out.add(h);
          i++;
          continue;
        }
      } else if (c == 0x3E && isQuoteLine(ln)) {
        var j = i;
        final inner = <_Line>[];
        while (j < lines.length && !lines[j].blank && isQuoteLine(lines[j])) {
          final l = lines[j].copy();
          l.start += (l.end - l.start == 1) ? 1 : 2;
          inner.add(l);
          j++;
        }
        final bs = quotes < maxQuoteDepth
            ? blocks(inner, quotes + 1)
            : const <Block>[];
        if (bs.isNotEmpty) {
          flush();
          final last = lines[j - 1];
          out.add(
            Block(BlockKind.quote, ln.start, last.end - ln.start)..blocks = bs,
          );
        } else {
          para.addAll(lines.sublist(i, j));
        }
        i = j;
        continue;
      } else if (c == 0x7C) {
        final n = tableAt(lines, i);
        if (n > 0) {
          final t = table(lines.sublist(i, i + n));
          if (t != null) {
            flush();
            out.add(t);
          } else {
            para.addAll(lines.sublist(i, i + n));
          }
          i += n;
          continue;
        }
      }

      if (item(ln) != null) {
        var j = i;
        final raw = <_RawItem>[];
        while (j < lines.length && !lines[j].blank) {
          final it = item(lines[j]);
          if (it == null) break;
          raw.add(it);
          j++;
        }
        flush();
        out.addAll(lists(raw));
        i = j;
        continue;
      }

      para.add(ln);
      i++;
    }
    flush();
    return out;
  }

  Block paragraph(List<_Line> lines) {
    final first = lines.first, last = lines.last;
    final spans = <Inline>[];
    for (var i = 0; i < lines.length; i++) {
      if (i > 0) {
        final prev = lines[i - 1];
        spans.add(Inline(InlineKind.lineBreak, prev.eol, prev.next - prev.eol));
      }
      spans.addAll(inline(lines[i].start, lines[i].end));
    }
    return Block(BlockKind.paragraph, first.start, last.end - first.start)
      ..inlines = spans;
  }

  Block? heading(_Line ln) {
    var i = ln.start;
    while (i < ln.end && u(i) == 0x23) {
      i++;
    }
    var level = i - ln.start;
    if (i >= ln.end || !_isBlank(u(i))) return null;
    while (i < ln.end && _isBlank(u(i))) {
      i++;
    }
    if (level > maxHeadingLevel) level = maxHeadingLevel;
    return Block(BlockKind.heading, ln.start, ln.end - ln.start)
      ..level = level
      ..inlines = inline(i, ln.end);
  }

  bool isRule(_Line ln) {
    if (ln.end - ln.start < 3) return false;
    for (var i = ln.start; i < ln.end; i++) {
      if (u(i) != 0x2D) return false;
    }
    return true;
  }

  bool isQuoteLine(_Line ln) {
    if (ln.blank || u(ln.start) != 0x3E) return false;
    return ln.end - ln.start == 1 || u(ln.start + 1) == 0x20;
  }

  /// The language of an opening fence ('' for none), or null when the
  /// line is not one.
  String? fenceOpen(_Line ln) {
    final n = ln.end - ln.start;
    if (n < 3 ||
        u(ln.start) != _bt ||
        u(ln.start + 1) != _bt ||
        u(ln.start + 2) != _bt) {
      return null;
    }
    for (var i = ln.start + 3; i < ln.end; i++) {
      if (u(i) == _bt) return null;
    }
    var i = ln.start + 3;
    while (i < ln.end && _isBlank(u(i))) {
      i++;
    }
    var j = i;
    while (j < ln.end && !_isBlank(u(j))) {
      j++;
    }
    for (var k = i; k < j; k++) {
      // Only ASCII names are languages; this also keeps characters whose
      // lower-case form is ASCII (the Kelvin sign) from matching.
      if (u(k) > 0x7F) return '';
    }
    return canonicalLanguage(src.substring(i, j));
  }

  bool isFenceClose(_Line ln) =>
      ln.end - ln.start == 3 &&
      u(ln.start) == _bt &&
      u(ln.start + 1) == _bt &&
      u(ln.start + 2) == _bt;

  Block codeBlock(List<_Line> lines, String lang) {
    final first = lines.first, last = lines.last;
    final code = StringBuffer();
    for (var i = 1; i < lines.length - 1; i++) {
      if (i > 1) code.write('\n');
      code.write(src.substring(lines[i].start, lines[i].eol));
    }
    return Block(BlockKind.codeBlock, first.start, last.end - first.start)
      ..lang = lang
      ..code = code.toString();
  }

  _RawItem? item(_Line ln) {
    var i = ln.start;
    while (i < ln.end && u(i) == 0x20) {
      i++;
    }
    final it = _RawItem()
      ..indent = i - ln.start
      ..at = i
      ..end = ln.end;
    if (i < ln.end && (u(i) == 0x2D || u(i) == 0x2A)) {
      i++;
    } else if (i < ln.end && _isDigit(u(i))) {
      while (i < ln.end && _isDigit(u(i)) && i - it.at < 9) {
        it.number = it.number * 10 + (u(i) - 0x30);
        i++;
      }
      if (i >= ln.end || u(i) != 0x2E) return null;
      i++;
      it.ordered = true;
    } else {
      return null;
    }
    if (i >= ln.end || !_isBlank(u(i))) return null;
    while (i < ln.end && _isBlank(u(i))) {
      i++;
    }
    it.text = i;

    // A box needs text after it; `- [ ]` alone is shown as typed.
    if (!it.ordered &&
        ln.end - i > 4 &&
        u(i) == 0x5B &&
        u(i + 2) == 0x5D &&
        _isBlank(u(i + 3)) &&
        (u(i + 1) == 0x20 || u(i + 1) == 0x78 || u(i + 1) == 0x58)) {
      var j = i + 3;
      while (j < ln.end && _isBlank(u(j))) {
        j++;
      }
      it.task = true;
      it.checked = u(i + 1) != 0x20;
      it.text = j;
    }
    return it;
  }

  List<Block> lists(List<_RawItem> raw) {
    final base = raw.first.indent;
    var prev = -1;
    for (final r in raw) {
      var d = r.indent > base ? (r.indent - base) ~/ 2 : 0;
      if (d > prev + 1) d = prev + 1;
      if (d > maxListDepth - 1) d = maxListDepth - 1;
      r.depth = d;
      prev = d;
    }
    final out = <Block>[];
    final pos = [0];
    while (pos[0] < raw.length) {
      out.add(list(raw, pos, raw[pos[0]].depth));
    }
    return out;
  }

  Block list(List<_RawItem> raw, List<int> pos, int depth) {
    final first = raw[pos[0]];
    final b = Block(BlockKind.list, first.at, 0)..ordered = first.ordered;
    if (first.ordered) b.start = first.number;
    var end = first.end;
    while (pos[0] < raw.length &&
        raw[pos[0]].depth == depth &&
        raw[pos[0]].ordered == b.ordered) {
      final r = raw[pos[0]];
      pos[0]++;
      final it = ListItem(
        at: r.at,
        len: r.end - r.at,
        task: r.task,
        checked: r.checked,
        textAt: r.text,
        inlines: inline(r.text, r.end),
      );
      end = r.end;
      while (pos[0] < raw.length && raw[pos[0]].depth > depth) {
        final sub = list(raw, pos, depth + 1);
        end = sub.at + sub.len;
        it.lists.add(sub);
      }
      b.items.add(it);
    }
    b.len = end - b.at;
    return b;
  }

  List<_Range>? row(_Line ln) {
    if (ln.blank || u(ln.start) != 0x7C) return null;
    final cells = <_Range>[];
    void add(int a, int b) {
      while (a < b && _isBlank(u(a))) {
        a++;
      }
      while (b > a && _isBlank(u(b - 1))) {
        b--;
      }
      cells.add(_Range(a, b));
    }

    var start = ln.start + 1;
    for (var i = start; i < ln.end; i++) {
      final c = u(i);
      if (c == _bs) {
        i++; // whatever follows a backslash is not a cell boundary
      } else if (c == 0x7C) {
        add(start, i);
        start = i + 1;
      }
    }
    if (start < ln.end) add(start, ln.end);
    return cells.isEmpty ? null : cells;
  }

  List<CellAlign>? alignments(_Line ln, int columns) {
    final cells = row(ln);
    if (cells == null || cells.length != columns) return null;
    final out = <CellAlign>[];
    for (final c in cells) {
      var a = c.start, b = c.end;
      final left = a < b && u(a) == 0x3A;
      if (left) a++;
      final right = b > a && u(b - 1) == 0x3A;
      if (right) b--;
      if (a >= b) return null;
      for (var i = a; i < b; i++) {
        if (u(i) != 0x2D) return null;
      }
      out.add(
        left && right
            ? CellAlign.center
            : left
            ? CellAlign.left
            : right
            ? CellAlign.right
            : CellAlign.none,
      );
    }
    return out;
  }

  /// How many lines the table starting at line [i] takes, or 0.
  int tableAt(List<_Line> lines, int i) {
    final head = row(lines[i]);
    if (head == null || i + 1 >= lines.length) return 0;
    if (alignments(lines[i + 1], head.length) == null) return 0;
    var j = i + 2;
    while (j < lines.length) {
      final cells = row(lines[j]);
      if (cells == null || cells.length > head.length) break;
      j++;
    }
    return j - i;
  }

  Block? table(List<_Line> lines) {
    final head = row(lines[0])!;
    if (head.length > maxTableColumns || lines.length - 1 > maxTableRows) {
      return null;
    }
    final first = lines.first, last = lines.last;
    List<Cell> cells(_Line ln, List<_Range> rs) {
      final out = [
        for (final r in rs)
          Cell(r.start, r.end - r.start, inline(r.start, r.end)),
      ];
      while (out.length < head.length) {
        out.add(Cell(ln.end, 0, const []));
      }
      return out;
    }

    final b = Block(BlockKind.table, first.start, last.end - first.start)
      ..align = alignments(lines[1], head.length)!
      ..head = cells(first, head);
    for (final ln in lines.sublist(2)) {
      b.rows.add(cells(ln, row(ln)!));
    }
    return b;
  }

  List<Inline> inline(int lo, int hi) {
    if (lo >= hi) return [];
    final s = _Segment(this, lo, hi)
      ..findAtoms()
      ..matchBrackets();
    return s.build(s.tokens(lo, hi, false));
  }
}

enum _Mark { none, bold, star, under, strike, spoil }

InlineKind _markKind(_Mark m) => switch (m) {
  _Mark.bold => InlineKind.bold,
  _Mark.strike => InlineKind.strike,
  _Mark.spoil => InlineKind.spoiler,
  _ => InlineKind.italic,
};

class _Token {
  _Token(this.at, this.end, {this.span, this.mark = _Mark.none});
  final int at;
  int end;
  final Inline? span;
  final _Mark mark;
  bool open = false;
  bool close = false;
}

class _Link {
  _Link(this.textEnd, this.urlAt, this.urlEnd, this.end, this.allowed);
  final int textEnd, urlAt, urlEnd, end;
  final bool allowed;
}

/// One run of inline text: a line of a paragraph, a heading's text, an
/// item's text or a table cell.
class _Segment {
  _Segment(this.p, this.lo, this.hi) : src = p.src;
  final _Parser p;
  final String src;
  final int lo, hi;

  /// For each position that starts an atom (a code span or an escape),
  /// where it ends. Atoms are found first so nothing inside one is read
  /// as a mark or a bracket.
  late final List<int> atomEnd = List<int>.filled(hi - lo, 0);

  /// For each `[`, the position of its `]`.
  late final List<int> match = List<int>.filled(hi - lo, 0);

  int u(int i) => src.codeUnitAt(i);

  void findAtoms() {
    final unclosed = <int>{};
    for (var i = lo; i < hi;) {
      final c = u(i);
      if (c == _bs && i + 1 < hi && _isPunct(u(i + 1))) {
        atomEnd[i - lo] = i + 2;
        i += 2;
        continue;
      }
      if (c != _bt) {
        i++;
        continue;
      }
      var j = i;
      while (j < hi && u(j) == _bt) {
        j++;
      }
      final n = j - i;
      if (!unclosed.contains(n)) {
        final end = closingRun(j, n);
        if (end > 0) {
          atomEnd[i - lo] = end;
          i = end;
          continue;
        }
        unclosed.add(n);
      }
      i = j;
    }
  }

  int closingRun(int from, int n) {
    for (var i = from; i < hi;) {
      if (u(i) != _bt) {
        i++;
        continue;
      }
      var j = i;
      while (j < hi && u(j) == _bt) {
        j++;
      }
      if (j - i == n) return j;
      i = j;
    }
    return 0;
  }

  void matchBrackets() {
    final open = <int>[];
    for (var i = lo; i < hi;) {
      final end = atomEnd[i - lo];
      if (end > 0) {
        i = end;
        continue;
      }
      final c = u(i);
      if (c == 0x5B) {
        open.add(i);
      } else if (c == 0x5D && open.isNotEmpty) {
        match[open.removeLast() - lo] = i;
      }
      i++;
    }
  }

  (int, bool) runeBefore(int i, int a) {
    if (i <= a) return (0, false);
    final c = u(i - 1);
    if (_isLow(c) && i - 2 >= a && _isHigh(u(i - 2))) {
      return (0x10000 + ((u(i - 2) - 0xD800) << 10) + (c - 0xDC00), true);
    }
    return (c, true);
  }

  (int, bool) runeAt(int i, int b) {
    if (i >= b) return (0, false);
    final c = u(i);
    if (_isHigh(c) && i + 1 < b && _isLow(u(i + 1))) {
      return (0x10000 + ((c - 0xD800) << 10) + (u(i + 1) - 0xDC00), true);
    }
    return (c, true);
  }

  bool wordFollows(int i, int b) {
    final (r, ok) = runeAt(i, b);
    return ok && _isWord(r);
  }

  bool wordBefore(int i, int a) {
    final (r, ok) = runeBefore(i, a);
    return ok && _isWord(r);
  }

  bool hasPrefixFold(int i, int b, String prefix) {
    if (b - i < prefix.length) return false;
    for (var k = 0; k < prefix.length; k++) {
      var c = u(i + k);
      if (c >= 0x41 && c <= 0x5A) c += 0x20;
      if (c != prefix.codeUnitAt(k)) return false;
    }
    return true;
  }

  bool allowedUrl(int a, int b) {
    for (final scheme in const ['https://', 'http://', 'mailto:', 'tel:']) {
      if (hasPrefixFold(a, b, scheme) && b - a > scheme.length) return true;
    }
    return false;
  }

  List<_Token> tokens(int a, int b, bool inLink) {
    final toks = <_Token>[];
    void plain(int at, int end) {
      if (toks.isNotEmpty) {
        final t = toks.last;
        if (t.mark == _Mark.none && t.span == null && t.end == at) {
          t.end = end;
          return;
        }
      }
      toks.add(_Token(at, end));
    }

    void span(Inline sp) => toks.add(_Token(sp.at, sp.end, span: sp));

    for (var i = a; i < b;) {
      final end = atomEnd[i - lo];
      if (end > 0 && end <= b) {
        if (u(i) == _bs) {
          span(
            Inline(InlineKind.escape, i, 2, text: src.substring(i + 1, i + 2)),
          );
        } else {
          var n = 0;
          while (u(i + n) == _bt) {
            n++;
          }
          span(
            Inline(
              InlineKind.code,
              i,
              end - i,
              text: src.substring(i + n, end - n),
            ),
          );
        }
        i = end;
        continue;
      }

      final c = u(i);
      switch (c) {
        case 0x21: // !
          if (!inLink && i + 1 < b && u(i + 1) == 0x5B) {
            final l = linkAt(i + 1, true);
            if (l != null && l.allowed) {
              span(
                Inline(
                  InlineKind.image,
                  i,
                  l.end - i,
                  url: src.substring(l.urlAt, l.urlEnd),
                ),
              );
              i = l.end;
              continue;
            }
          }
        case 0x5B: // [
          if (!inLink) {
            final l = linkAt(i, false);
            if (l != null) {
              span(
                Inline(
                  l.allowed ? InlineKind.link : InlineKind.noLink,
                  i,
                  l.end - i,
                  url: l.allowed ? src.substring(l.urlAt, l.urlEnd) : '',
                  children: build(tokens(i + 1, l.textEnd, true)),
                ),
              );
              i = l.end;
              continue;
            }
          }
        case 0x68 || 0x48: // h H
          if (!inLink) {
            final e = bareLink(i, a, b);
            if (e > 0) {
              span(
                Inline(InlineKind.bareLink, i, e - i, url: src.substring(i, e)),
              );
              i = e;
              continue;
            }
          }
        case 0x40: // @
          if (!inLink) {
            final sp = mention(i, a, b);
            if (sp != null) {
              span(sp);
              i = sp.end;
              continue;
            }
          }
        case 0x3C: // <
          if (!inLink) {
            final sp = channel(i, b);
            if (sp != null) {
              span(sp);
              i = sp.end;
              continue;
            }
          }
        case 0x3A: // :
          final sp = emoji(i, a, b);
          if (sp != null) {
            span(sp);
            i = sp.end;
            continue;
          }
        case 0x2A || 0x5F || 0x7E || 0x7C: // * _ ~ |
          var j = i;
          while (j < b && u(j) == c) {
            j++;
          }
          if (!marks(toks, c, i, j, a, b)) plain(i, j);
          i = j;
          continue;
      }

      plain(i, i + 1);
      i++;
    }
    return toks;
  }

  bool marks(List<_Token> toks, int c, int i, int j, int a, int b) {
    final n = j - i;
    final (prev, hasPrev) = runeBefore(i, a);
    final (next, hasNext) = runeAt(j, b);
    var open = hasNext && !_isWhite(next);
    var shut = hasPrev && !_isWhite(prev);

    void add(_Mark m, int at, int end) {
      toks.add(
        _Token(at, end, mark: m)
          ..open = open
          ..close = shut,
      );
    }

    switch (c) {
      case 0x2A:
        switch (n) {
          case 1:
            add(_Mark.star, i, j);
          case 2:
            add(_Mark.bold, i, j);
          case 3:
            // `***` is bold and italic together; which comes first
            // depends on whether the run closes or opens.
            if (shut) {
              add(_Mark.star, i, i + 1);
              add(_Mark.bold, i + 1, j);
            } else {
              add(_Mark.bold, i, i + 2);
              add(_Mark.star, i + 2, j);
            }
          default:
            return false;
        }
      case 0x5F:
        if (n != 1) return false;
        // `_` works only at word edges, so snake_case stays as typed.
        open = open && !(hasPrev && _isWord(prev));
        shut = shut && !(hasNext && _isWord(next));
        add(_Mark.under, i, j);
      case 0x7E:
        if (n != 2) return false;
        add(_Mark.strike, i, j);
      case 0x7C:
        if (n != 2) return false;
        add(_Mark.spoil, i, j);
    }
    return true;
  }

  List<Inline> build(List<_Token> toks) {
    final closers = {for (final m in _Mark.values) m: <int>[]};
    for (var i = 0; i < toks.length; i++) {
      final t = toks[i];
      if (t.mark != _Mark.none && t.close) closers[t.mark]!.add(i);
    }
    return pair(toks, 0, toks.length, closers, 0);
  }

  List<Inline> pair(
    List<_Token> toks,
    int lo,
    int hi,
    Map<_Mark, List<int>> closers,
    int active,
  ) {
    final out = <Inline>[];
    void plain(int at, int end) {
      if (out.isNotEmpty &&
          out.last.kind == InlineKind.text &&
          out.last.end == at) {
        out.last.len = end - out.last.at;
        return;
      }
      out.add(Inline(InlineKind.text, at, end - at));
    }

    for (var i = lo; i < hi; i++) {
      final t = toks[i];
      if (t.span != null) {
        out.add(t.span!);
      } else if (t.mark != _Mark.none &&
          t.open &&
          active & (1 << t.mark.index) == 0) {
        // The nearest closer of the same mark that leaves at least one
        // token between: an empty pair (`****`) is not emphasis.
        final cs = closers[t.mark]!;
        var k = 0;
        while (k < cs.length && cs[k] < i + 2) {
          k++;
        }
        if (k < cs.length && cs[k] < hi) {
          final j = cs[k];
          out.add(
            Inline(
              _markKind(t.mark),
              t.at,
              toks[j].end - t.at,
              children: pair(
                toks,
                i + 1,
                j,
                closers,
                active | (1 << t.mark.index),
              ),
            ),
          );
          i = j;
          continue;
        }
        plain(t.at, t.end);
      } else {
        plain(t.at, t.end);
      }
    }
    for (final s in out) {
      if (s.kind == InlineKind.text) s.text = src.substring(s.at, s.end);
    }
    return out;
  }

  _Link? linkAt(int i, bool emptyText) {
    final m = match[i - lo];
    if (m <= i ||
        (m == i + 1 && !emptyText) ||
        m + 1 >= hi ||
        u(m + 1) != 0x28) {
      return null;
    }
    var depth = 0;
    for (var j = m + 2; j < hi; j++) {
      final c = u(j);
      if (c == 0x28) {
        depth++;
      } else if (c == 0x29) {
        if (depth == 0) {
          return _Link(m, m + 2, j, j + 1, allowedUrl(m + 2, j));
        }
        depth--;
      } else if (c < 0x20 ||
          c == 0x7F ||
          c == _bt ||
          c == _bs ||
          c == 0x3C ||
          c == 0x3E ||
          _isWhite(c)) {
        return null;
      }
    }
    return null;
  }

  /// Where the bare link starting at [i] ends, or 0 when there is none.
  int bareLink(int i, int a, int b) {
    int scheme;
    if (hasPrefixFold(i, b, 'https://')) {
      scheme = 8;
    } else if (hasPrefixFold(i, b, 'http://')) {
      scheme = 7;
    } else {
      return 0;
    }
    if (wordBefore(i, a)) return 0;
    var j = i + scheme;
    final opens = [0, 0, 0], closes = [0, 0, 0]; // ( [ {
    while (j < b) {
      final c = u(j);
      if (c < 0x20 ||
          c == 0x7F ||
          c == 0x3C ||
          c == 0x3E ||
          c == 0x22 ||
          c == _bt ||
          c == _bs ||
          _isWhite(c)) {
        break;
      }
      switch (c) {
        case 0x28:
          opens[0]++;
        case 0x5B:
          opens[1]++;
        case 0x7B:
          opens[2]++;
        case 0x29:
          closes[0]++;
        case 0x5D:
          closes[1]++;
        case 0x7D:
          closes[2]++;
      }
      j++;
    }
    // Trailing punctuation is the sentence's, not the address's; a
    // closing bracket is the address's only if it has its opener inside.
    trim:
    while (j > i + scheme) {
      int k;
      switch (u(j - 1)) {
        case 0x2E ||
            0x2C ||
            0x21 ||
            0x3F ||
            0x3A ||
            0x3B ||
            0x27 ||
            0x2A ||
            0x5F ||
            0x7E ||
            0x7C:
          j--;
          continue trim;
        case 0x29:
          k = 0;
        case 0x5D:
          k = 1;
        case 0x7D:
          k = 2;
        default:
          break trim;
      }
      if (closes[k] <= opens[k]) break;
      closes[k]--;
      j--;
    }
    return j <= i + scheme ? 0 : j;
  }

  int nameRun(int i, int b, String extra) {
    var j = i;
    while (j < b &&
        (_isLowerAlnum(u(j)) || (j > i && extra.codeUnits.contains(u(j))))) {
      j++;
    }
    while (j > i && !_isLowerAlnum(u(j - 1))) {
      j--;
    }
    return j;
  }

  Inline? mention(int i, int a, int b) {
    // An email address, or the middle of a word.
    if (wordBefore(i, a)) return null;
    final end = nameRun(i + 1, b, '._-');
    final name = src.substring(i + 1, end);
    if (name.length < 2 || name.length > 32) return null;
    if (end < b && u(end) == 0x40) {
      var srvEnd = nameRun(end + 1, b, '.-');
      if (srvEnd - (end + 1) > 253) srvEnd = end + 1;
      if (srvEnd == end + 1 ||
          wordFollows(srvEnd, b) ||
          (srvEnd < b && u(srvEnd) == 0x40)) {
        return null;
      }
      return Inline(
        InlineKind.mention,
        i,
        srvEnd - i,
        name: name,
        server: src.substring(end + 1, srvEnd),
      );
    }
    if (wordFollows(end, b)) return null;
    return Inline(InlineKind.mention, i, end - i, name: name);
  }

  static const _channelLinkLen = 2 + 36 + 1;
  static final _idRe = RegExp(
    r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
  );

  Inline? channel(int i, int b) {
    if (i + _channelLinkLen > b ||
        u(i + 1) != 0x23 ||
        u(i + _channelLinkLen - 1) != 0x3E) {
      return null;
    }
    final id = src.substring(i + 2, i + _channelLinkLen - 1);
    if (!_idRe.hasMatch(id)) return null;
    return Inline(InlineKind.channel, i, _channelLinkLen, id: id);
  }

  Inline? emoji(int i, int a, int b) {
    // `10:30:00`, `std::string`.
    if (wordBefore(i, a)) return null;
    var j = i + 1;
    while (j < b &&
        (_isLowerAlnum(u(j)) || u(j) == 0x5F || u(j) == 0x2B || u(j) == 0x2D)) {
      j++;
    }
    if (j == i + 1 || j - (i + 1) > 32 || j >= b) return null;
    final name = src.substring(i + 1, j);
    var server = '';
    if (u(j) == 0x40) {
      var k = j + 1;
      while (k < b &&
          (_isLowerAlnum(u(k)) ||
              (k > j + 1 && (u(k) == 0x2E || u(k) == 0x2D)))) {
        k++;
      }
      if (k == j + 1 ||
          k - (j + 1) > 253 ||
          !_isLowerAlnum(u(k - 1)) ||
          k >= b) {
        return null;
      }
      server = src.substring(j + 1, k);
      j = k;
    }
    if (u(j) != 0x3A || wordFollows(j + 1, b)) return null;
    return Inline(InlineKind.emoji, i, j + 1 - i, name: name, server: server);
  }
}
