// The web chat's reader of Churr Markdown, the fixed subset of Markdown
// that message text is written in (docs/MESSAGE-FORMAT.md sections 2 to 4).
//
// It is a port, function for function, of the server's parser in
// internal/markdown (block.go and inline.go). The two, and the apps' parser
// in Dart, must agree exactly: all three run the cases in testdata/markdown
// (e2e/markdown.mjs runs them against this file), and the README there is
// the precise statement of every rule. If this file and that README
// disagree, one of them has a bug.
//
// parse(text) returns the compact tree the corpus is written in: a list of
// block nodes, each { t, at, len, … }, holding inline nodes of the same
// shape. Positions count UTF-16 code units, which is how JavaScript indexes
// a string, so an index into the text is already the position to report.
// The parser resolves nothing: a mention, channel link or emoji node is a
// candidate, and the renderer draws it from the entities the server sent.

export const MAX_HEADING_LEVEL = 3;
export const MAX_LIST_DEPTH = 3;
export const MAX_QUOTE_DEPTH = 3;
export const MAX_TABLE_COLUMNS = 20;
export const MAX_TABLE_ROWS = 200;

// Every accepted name of a code block language, in lower case, mapped to
// the name it is highlighted with (docs/MESSAGE-FORMAT.md section 2.3).
export const LANGUAGES = (() => {
  const list = {
    bash: ['sh', 'shell', 'zsh'], c: [], cpp: ['c++', 'cc'], csharp: ['cs', 'c#'], css: [], dart: [], diff: ['patch'],
    dockerfile: ['docker'], elixir: [], go: ['golang'], graphql: [], haskell: ['hs'], html: [], ini: [], java: [],
    javascript: ['js'], json: [], kotlin: ['kt'], lua: [], makefile: ['make'], markdown: ['md'], nginx: [],
    objectivec: ['objc'], perl: [], php: [], plaintext: ['text', 'txt'], powershell: ['ps1'], protobuf: ['proto'],
    python: ['py'], r: [], ruby: ['rb'], rust: ['rs'], scala: [], scss: [], sql: [], swift: [], toml: [],
    typescript: ['ts'], xml: [], yaml: ['yml'], zig: [],
  };
  const m = new Map();
  for (const [name, aliases] of Object.entries(list)) {
    m.set(name, name);
    for (const a of aliases) m.set(a, name);
  }
  return m;
})();

// --- characters ---

const C = (ch) => ch.charCodeAt(0);
const BACKSLASH = 0x5c, BACKTICK = 0x60, SPACE = 0x20, TAB = 0x09, LF = 0x0a, CR = 0x0d;

const isHigh = (c) => c >= 0xd800 && c <= 0xdbff;
const isLow = (c) => c >= 0xdc00 && c <= 0xdfff;

// The character that ends just before position i, looking no further back
// than lo, as a code point; -1 at the start.
function runeBefore(src, i, lo) {
  if (i <= lo) return -1;
  const c = src.charCodeAt(i - 1);
  if (isLow(c) && i - 2 >= lo && isHigh(src.charCodeAt(i - 2))) return src.codePointAt(i - 2);
  return c;
}

// The character that starts at position i, looking no further than hi; -1
// at the end.
function runeAt(src, i, hi) {
  if (i >= hi) return -1;
  const c = src.charCodeAt(i);
  if (isHigh(c) && i + 1 < hi && isLow(src.charCodeAt(i + 1))) return src.codePointAt(i);
  return c;
}

// White space is the Unicode White_Space property, spelled out so the
// three parsers agree exactly.
function isWhite(r) {
  return (r >= 0x09 && r <= 0x0d) || r === 0x20 || r === 0x85 || r === 0xa0 || r === 0x1680
    || (r >= 0x2000 && r <= 0x200a) || r === 0x2028 || r === 0x2029 || r === 0x202f || r === 0x205f || r === 0x3000;
}

// Part of a word: a letter or a number in any script. Combining marks are
// deliberately not counted (see chars.go).
const WORD = /^[\p{L}\p{N}]$/u;
function isWord(r) {
  // A lone surrogate is not a character of any kind.
  if (r < 0 || (r >= 0xd800 && r <= 0xdfff)) return false;
  return WORD.test(String.fromCodePoint(r));
}

// One of the 32 ASCII punctuation characters, the ones a backslash can
// escape.
const isPunct = (c) => (c >= 0x21 && c <= 0x2f) || (c >= 0x3a && c <= 0x40) || (c >= 0x5b && c <= 0x60) || (c >= 0x7b && c <= 0x7e);
const isLowerAlnum = (c) => (c >= 0x61 && c <= 0x7a) || (c >= 0x30 && c <= 0x39);
const isDigit = (c) => c >= 0x30 && c <= 0x39;
const isBlank = (c) => c === SPACE || c === TAB;

// Whether src, from i up to hi, starts with the ASCII string prefix (given
// in lower case), without regard to case.
function hasPrefixFold(src, i, hi, prefix) {
  if (hi - i < prefix.length) return false;
  for (let k = 0; k < prefix.length; k++) {
    let c = src.charCodeAt(i + k);
    if (c >= 0x41 && c <= 0x5a) c += 0x20;
    if (c !== prefix.charCodeAt(k)) return false;
  }
  return true;
}

// Whether a link may point at src[a:b]. Only these four schemes are links,
// so a client never has to decide whether an address is safe to open.
function allowedURL(src, a, b) {
  for (const scheme of ['https://', 'http://', 'mailto:', 'tel:']) {
    if (hasPrefixFold(src, a, b, scheme) && b - a > scheme.length) return true;
  }
  return false;
}

// The highlighting name for the word after an opening fence, or ''.
function language(word) {
  let out = '';
  for (let i = 0; i < word.length; i++) {
    let c = word.charCodeAt(i);
    if (c > 0x7f) return '';
    if (c >= 0x41 && c <= 0x5a) c += 0x20;
    out += String.fromCharCode(c);
  }
  return LANGUAGES.get(out) || '';
}

// The username rule: 2 to 32 characters, [a-z0-9] first, then [a-z0-9._-].
function validUsername(s) {
  if (s.length < 2 || s.length > 32) return false;
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (isLowerAlnum(c)) continue;
    if (i === 0 || (c !== C('.') && c !== C('_') && c !== C('-'))) return false;
  }
  return true;
}

// An identifier as identifiers are written everywhere: 36 characters, lower
// case hexadecimal with hyphens.
export function validID(s) {
  if (s.length !== 36) return false;
  for (let i = 0; i < 36; i++) {
    const c = s.charCodeAt(i);
    if (i === 8 || i === 13 || i === 18 || i === 23) { if (c !== 0x2d) return false; }
    else if (!((c >= 0x30 && c <= 0x39) || (c >= 0x61 && c <= 0x66))) return false;
  }
  return true;
}

// --- blocks ---

// A line as a block sees it. A quote strips its `> ` from each line by
// moving start forward, so the blocks inside it are parsed by the same code
// as blocks at the top.
//   start  first unit of the line, after any prefix a container took
//   end    after the last unit that is not a trailing space or tab
//   eol    where the line ending starts
//   next   where the next line starts
function splitLines(src) {
  const lines = [];
  let start = 0;
  while (start < src.length) {
    let eol = start;
    while (eol < src.length && src.charCodeAt(eol) !== LF && src.charCodeAt(eol) !== CR) eol++;
    let next = eol;
    if (next < src.length) {
      next++;
      if (src.charCodeAt(eol) === CR && next < src.length && src.charCodeAt(next) === LF) next++;
    }
    let end = eol;
    while (end > start && isBlank(src.charCodeAt(end - 1))) end--;
    lines.push({ start, end, eol, next });
    start = next;
  }
  return lines;
}

const blank = (l) => l.start >= l.end;

class Parser {
  constructor(src) { this.src = src; }

  // blocks parses a run of lines into blocks. quotes is how many quotes
  // these lines are already inside. Each line is looked at once; a
  // construct that turns out not to be one hands its lines to the
  // paragraph.
  blocks(lines, quotes) {
    const src = this.src;
    const out = [];
    let para = [];
    const flush = () => { if (para.length) { out.push(this.paragraph(para)); para = []; } };

    // closeAt[i] is the first line at or after i that closes a code block,
    // or -1.
    const closeAt = new Array(lines.length + 1);
    closeAt[lines.length] = -1;
    for (let i = lines.length - 1; i >= 0; i--) closeAt[i] = this.isFenceClose(lines[i]) ? i : closeAt[i + 1];

    for (let i = 0; i < lines.length;) {
      const ln = lines[i];
      if (blank(ln)) { flush(); i++; continue; }
      const c = src.charCodeAt(ln.start);

      if (c === BACKTICK) {
        const lang = this.fenceOpen(ln);
        if (lang !== null && closeAt[i + 1] >= 0) {
          flush();
          const end = closeAt[i + 1];
          out.push(this.codeBlock(lines.slice(i, end + 1), lang));
          i = end + 1;
          continue;
        }
      } else if (c === C('-') && this.isRule(ln)) {
        flush();
        out.push({ t: 'rule', at: ln.start, len: ln.end - ln.start });
        i++;
        continue;
      } else if (c === C('#')) {
        const hd = this.heading(ln);
        if (hd) { flush(); out.push(hd); i++; continue; }
      } else if (c === C('>') && this.isQuoteLine(ln)) {
        let j = i;
        const inner = [];
        while (j < lines.length && !blank(lines[j]) && this.isQuoteLine(lines[j])) {
          const l = { ...lines[j] };
          // Trailing spaces are already gone, so a line is either a bare
          // `>` or `> ` and something.
          l.start += l.end - l.start === 1 ? 1 : 2;
          inner.push(l);
          j++;
        }
        const bs = quotes < MAX_QUOTE_DEPTH ? this.blocks(inner, quotes + 1) : [];
        if (bs.length) {
          flush();
          out.push({ t: 'quote', at: ln.start, len: lines[j - 1].end - ln.start, b: bs });
        } else {
          // A quote of nothing, or one nested too deep: as typed.
          para.push(...lines.slice(i, j));
        }
        i = j;
        continue;
      } else if (c === C('|')) {
        const n = this.tableAt(lines, i);
        if (n > 0) {
          const t = this.table(lines.slice(i, i + n));
          if (t) { flush(); out.push(t); } else para.push(...lines.slice(i, i + n));
          i += n;
          continue;
        }
      }

      if (this.item(ln)) {
        let j = i;
        const raw = [];
        while (j < lines.length && !blank(lines[j])) {
          const it = this.item(lines[j]);
          if (!it) break;
          raw.push(it);
          j++;
        }
        flush();
        out.push(...this.lists(raw));
        i = j;
        continue;
      }

      para.push(ln);
      i++;
    }
    flush();
    return out;
  }

  // A paragraph of consecutive lines. Each line is read on its own, with a
  // line break between: no inline construct crosses a line.
  paragraph(lines) {
    const first = lines[0], last = lines[lines.length - 1];
    const c = [];
    lines.forEach((ln, i) => {
      if (i > 0) { const prev = lines[i - 1]; c.push({ t: 'br', at: prev.eol, len: prev.next - prev.eol }); }
      c.push(...this.inline(ln.start, ln.end));
    });
    return { t: 'p', at: first.start, len: last.end - first.start, c };
  }

  // `# `, `## ` or `### ` and some text. More `#` are level three. A `#`
  // with no space after it is not a heading.
  heading(ln) {
    const src = this.src;
    let i = ln.start;
    while (i < ln.end && src.charCodeAt(i) === C('#')) i++;
    let level = i - ln.start;
    if (i >= ln.end || !isBlank(src.charCodeAt(i))) return null;
    while (i < ln.end && isBlank(src.charCodeAt(i))) i++;
    if (level > MAX_HEADING_LEVEL) level = MAX_HEADING_LEVEL;
    return { t: 'h', at: ln.start, len: ln.end - ln.start, level, c: this.inline(i, ln.end) };
  }

  // Three or more `-` and nothing else.
  isRule(ln) {
    if (ln.end - ln.start < 3) return false;
    for (let i = ln.start; i < ln.end; i++) if (this.src.charCodeAt(i) !== C('-')) return false;
    return true;
  }

  // `>` alone, or `> ` and something.
  isQuoteLine(ln) {
    if (blank(ln) || this.src.charCodeAt(ln.start) !== C('>')) return false;
    return ln.end - ln.start === 1 || this.src.charCodeAt(ln.start + 1) === SPACE;
  }

  // Whether the line opens a code block: three backticks, then optionally
  // a word naming the language. Returns the language ('' for none), or
  // null if the line is not a fence. A backtick later in the line means it
  // is inline code written with three backticks.
  fenceOpen(ln) {
    const s = this.src.slice(ln.start, ln.end);
    if (s.length < 3 || !s.startsWith('```')) return null;
    if (s.indexOf('`', 3) >= 0) return null;
    let i = 3;
    while (i < s.length && isBlank(s.charCodeAt(i))) i++;
    let j = i;
    while (j < s.length && !isBlank(s.charCodeAt(j))) j++;
    return language(s.slice(i, j));
  }

  // Exactly three backticks.
  isFenceClose(ln) {
    return ln.end - ln.start === 3 && this.src.startsWith('```', ln.start);
  }

  // The code is kept exactly, trailing spaces included, with the line
  // endings made "\n".
  codeBlock(lines, lang) {
    const first = lines[0], last = lines[lines.length - 1];
    const code = lines.slice(1, -1).map((ln) => this.src.slice(ln.start, ln.eol)).join('\n');
    const node = { t: 'codeblock', at: first.start, len: last.end - first.start };
    if (lang) node.lang = lang;
    if (code) node.text = code;
    return node;
  }

  // Whether the line is a list item: spaces, a marker, at least one space,
  // and some text. Returns the raw item, or null.
  item(ln) {
    const s = this.src;
    let i = ln.start;
    while (i < ln.end && s.charCodeAt(i) === SPACE) i++;
    const it = { indent: i - ln.start, depth: 0, ordered: false, number: 0, at: i, text: 0, end: ln.end, task: false, checked: false };
    if (i < ln.end && (s.charCodeAt(i) === C('-') || s.charCodeAt(i) === C('*'))) {
      i++;
    } else if (i < ln.end && isDigit(s.charCodeAt(i))) {
      // Nine digits at most, so the number fits in every language's
      // integer without care.
      while (i < ln.end && isDigit(s.charCodeAt(i)) && i - it.at < 9) {
        it.number = it.number * 10 + (s.charCodeAt(i) - 0x30);
        i++;
      }
      if (i >= ln.end || s.charCodeAt(i) !== C('.')) return null;
      i++;
      it.ordered = true;
    } else {
      return null;
    }
    if (i >= ln.end || !isBlank(s.charCodeAt(i))) return null;
    while (i < ln.end && isBlank(s.charCodeAt(i))) i++;
    it.text = i;

    // A box makes a bullet a checklist item, but only with text after it.
    if (!it.ordered && ln.end - i > 4 && s.charCodeAt(i) === C('[') && s.charCodeAt(i + 2) === C(']') && isBlank(s.charCodeAt(i + 3))
      && (s.charCodeAt(i + 1) === SPACE || s.charCodeAt(i + 1) === C('x') || s.charCodeAt(i + 1) === C('X'))) {
      let j = i + 3;
      while (j < ln.end && isBlank(s.charCodeAt(j))) j++;
      it.task = true;
      it.checked = s.charCodeAt(i + 1) !== SPACE;
      it.text = j;
    }
    return it;
  }

  // Arranges consecutive list lines into lists. One run can give several
  // lists, because a numbered item after bullets at the same level starts
  // a new list.
  lists(raw) {
    // The first line sets the left edge. From there two spaces of indent
    // are one level. An item can be at most one level deeper than the item
    // before it, and never deeper than the third level.
    const base = raw[0].indent;
    let prev = -1;
    for (const r of raw) {
      let d = r.indent > base ? Math.floor((r.indent - base) / 2) : 0;
      if (d > prev + 1) d = prev + 1;
      if (d > MAX_LIST_DEPTH - 1) d = MAX_LIST_DEPTH - 1;
      r.depth = d;
      prev = d;
    }
    const out = [];
    const pos = { i: 0 };
    while (pos.i < raw.length) out.push(this.list(raw, pos, raw[pos.i].depth));
    return out;
  }

  // One list starting at raw[pos.i]: the items at this depth of the same
  // kind, each with the deeper lists that follow it.
  list(raw, pos, depth) {
    const first = raw[pos.i];
    const b = { t: 'list', at: first.at, len: 0, kind: first.ordered ? 'numbered' : 'bullet' };
    if (first.ordered) b.start = first.number;
    b.items = [];
    let end = first.end;
    while (pos.i < raw.length && raw[pos.i].depth === depth && raw[pos.i].ordered === first.ordered) {
      const r = raw[pos.i++];
      const it = { t: 'item', at: r.at, len: r.end - r.at };
      if (r.task) it.checked = r.checked;
      it.c = this.inline(r.text, r.end);
      end = r.end;
      const nested = [];
      while (pos.i < raw.length && raw[pos.i].depth > depth) {
        const sub = this.list(raw, pos, depth + 1);
        end = sub.at + sub.len;
        nested.push(sub);
      }
      if (nested.length) it.b = nested;
      b.items.push(it);
    }
    b.len = end - b.at;
    return b;
  }

  // Splits a table row into cells. A row starts with `|`. Cells end at each
  // further `|` that has no backslash before it; a last cell does not need
  // a closing `|`. Returns the cells as [start, end] pairs, or null.
  row(ln) {
    const s = this.src;
    if (blank(ln) || s.charCodeAt(ln.start) !== C('|')) return null;
    const cells = [];
    const add = (a, b) => {
      while (a < b && isBlank(s.charCodeAt(a))) a++;
      while (b > a && isBlank(s.charCodeAt(b - 1))) b--;
      cells.push([a, b]);
    };
    let start = ln.start + 1;
    for (let i = start; i < ln.end; i++) {
      const c = s.charCodeAt(i);
      if (c === BACKSLASH) i++; // whatever follows a backslash is not a cell boundary
      else if (c === C('|')) { add(start, i); start = i + 1; }
    }
    if (start < ln.end) add(start, ln.end);
    return cells.length ? cells : null;
  }

  // The row under the header: one cell of dashes per header cell, with
  // colons saying which way the column is aligned.
  alignments(ln, columns) {
    const cells = this.row(ln);
    if (!cells || cells.length !== columns) return null;
    const out = [];
    for (let [a, b] of cells) {
      const left = a < b && this.src.charCodeAt(a) === C(':');
      if (left) a++;
      const right = b > a && this.src.charCodeAt(b - 1) === C(':');
      if (right) b--;
      if (a >= b) return null;
      for (let i = a; i < b; i++) if (this.src.charCodeAt(i) !== C('-')) return null;
      out.push(left && right ? 'center' : left ? 'left' : right ? 'right' : 'none');
    }
    return out;
  }

  // Whether a table starts at lines[i], and how many lines it takes: the
  // header, the row of dashes, and every following row that starts with
  // `|` and has no more cells than the header. 0 means no table.
  tableAt(lines, i) {
    const head = this.row(lines[i]);
    if (!head || i + 1 >= lines.length) return 0;
    if (!this.alignments(lines[i + 1], head.length)) return 0;
    let j = i + 2;
    while (j < lines.length) {
      const cells = this.row(lines[j]);
      if (!cells || cells.length > head.length) break;
      j++;
    }
    return j - i;
  }

  // Makes a table of the lines tableAt found, or null if it is larger than
  // the format allows.
  table(lines) {
    const head = this.row(lines[0]);
    if (head.length > MAX_TABLE_COLUMNS || lines.length - 1 > MAX_TABLE_ROWS) return null;
    const align = this.alignments(lines[1], head.length);
    const first = lines[0], last = lines[lines.length - 1];
    const cells = (ln, rs) => {
      const out = rs.map(([a, b]) => ({ t: 'cell', at: a, len: b - a, c: this.inline(a, b) }));
      // A short row is filled out with empty cells at the end of its line.
      while (out.length < head.length) out.push({ t: 'cell', at: ln.end, len: 0, c: [] });
      return out;
    };
    const t = { t: 'table', at: first.start, len: last.end - first.start, align, head: cells(first, head), rows: [] };
    for (const ln of lines.slice(2)) t.rows.push(cells(ln, this.row(ln)));
    return t;
  }

  // inline parses [lo, hi) of the text into spans.
  inline(lo, hi) {
    if (lo >= hi) return [];
    const s = new Segment(this.src, lo, hi);
    s.findAtoms();
    s.matchBrackets();
    return s.build(s.tokens(lo, hi, false));
  }
}

// --- inline ---
//
// The inline parser reads one piece of one line. It works in three steps,
// each a single pass:
//  1. Find escapes and inline code. They win over everything else.
//  2. Cut the rest into tokens: finished spans, marks and plain characters.
//  3. Pair the marks. A mark that finds no partner is plain text.

const MARK_BOLD = 1, MARK_STAR = 2, MARK_UNDER = 3, MARK_STRIKE = 4, MARK_SPOIL = 5, MARK_COUNT = 6;
const MARK_KIND = { [MARK_BOLD]: 'bold', [MARK_STAR]: 'italic', [MARK_UNDER]: 'italic', [MARK_STRIKE]: 'strike', [MARK_SPOIL]: 'spoiler' };

class Segment {
  constructor(src, lo, hi) {
    this.src = src; this.lo = lo; this.hi = hi;
    // atomEnd[i-lo], when not zero, is the end of an escape or inline code
    // that starts at i. match[i-lo], when not zero, is the `]` that closes
    // the `[` at i.
    this.atomEnd = new Int32Array(hi - lo);
    this.match = new Int32Array(hi - lo);
  }

  // Marks escapes and inline code. Inline code opens with a run of
  // backticks and closes at the next run of exactly the same length. A run
  // that is never closed is plain text.
  findAtoms() {
    const { src, lo, hi } = this;
    const unclosed = new Set();
    for (let i = lo; i < hi;) {
      const c = src.charCodeAt(i);
      if (c === BACKSLASH && i + 1 < hi && isPunct(src.charCodeAt(i + 1))) { this.atomEnd[i - lo] = i + 2; i += 2; continue; }
      if (c !== BACKTICK) { i++; continue; }
      let j = i;
      while (j < hi && src.charCodeAt(j) === BACKTICK) j++;
      const n = j - i;
      if (!unclosed.has(n)) {
        const end = this.closingRun(j, n);
        if (end > 0) { this.atomEnd[i - lo] = end; i = end; continue; }
        unclosed.add(n);
      }
      i = j;
    }
  }

  // The position after the first run of exactly n backticks at or after
  // from, or 0.
  closingRun(from, n) {
    const { src, hi } = this;
    for (let i = from; i < hi;) {
      if (src.charCodeAt(i) !== BACKTICK) { i++; continue; }
      let j = i;
      while (j < hi && src.charCodeAt(j) === BACKTICK) j++;
      if (j - i === n) return j;
      i = j;
    }
    return 0;
  }

  // Pairs each `[` with its `]`, skipping escapes and code, so that link
  // text may itself hold brackets.
  matchBrackets() {
    const { src, lo, hi } = this;
    const open = [];
    for (let i = lo; i < hi;) {
      const end = this.atomEnd[i - lo];
      if (end > 0) { i = end; continue; }
      const c = src.charCodeAt(i);
      if (c === C('[')) open.push(i);
      else if (c === C(']') && open.length) this.match[open.pop() - lo] = i;
      i++;
    }
  }

  // Cuts [a, b) into tokens: { at, end, span } for a finished span,
  // { at, end, mark, open, close } for a mark, { at, end } for plain
  // characters. inLink is true while reading the text of a link, where
  // links, bare links, mentions and channel links are not looked for.
  tokens(a, b, inLink) {
    const { src, lo } = this;
    const toks = [];
    const plain = (at, end) => {
      const last = toks[toks.length - 1];
      if (last && !last.mark && !last.span && last.end === at) { last.end = end; return; }
      toks.push({ at, end });
    };
    const span = (sp) => toks.push({ at: sp.at, end: sp.at + sp.len, span: sp });

    for (let i = a; i < b;) {
      const end = this.atomEnd[i - lo];
      if (end > 0 && end <= b) {
        if (src.charCodeAt(i) === BACKSLASH) {
          span({ t: 'esc', at: i, len: 2, ch: src.slice(i + 1, i + 2) });
        } else {
          let n = 0;
          while (src.charCodeAt(i + n) === BACKTICK) n++;
          const node = { t: 'code', at: i, len: end - i };
          const text = src.slice(i + n, end - n);
          if (text) node.text = text;
          span(node);
        }
        i = end;
        continue;
      }

      const c = src.charCodeAt(i);
      if (c === C('!')) {
        if (!inLink && i + 1 < b && src.charCodeAt(i + 1) === C('[')) {
          const l = this.linkAt(i + 1, true);
          if (l && l.allowed) { span({ t: 'image', at: i, len: l.end - i, url: src.slice(l.urlAt, l.urlEnd) }); i = l.end; continue; }
        }
      } else if (c === C('[')) {
        if (!inLink) {
          const l = this.linkAt(i, false);
          if (l) {
            const sp = { t: l.allowed ? 'link' : 'nolink', at: i, len: l.end - i };
            if (l.allowed) sp.url = src.slice(l.urlAt, l.urlEnd);
            sp.c = this.build(this.tokens(i + 1, l.textEnd, true));
            span(sp);
            i = l.end;
            continue;
          }
        }
      } else if (c === C('h') || c === C('H')) {
        if (!inLink) {
          const e = this.bareLink(i, a, b);
          if (e > 0) { span({ t: 'url', at: i, len: e - i, url: src.slice(i, e) }); i = e; continue; }
        }
      } else if (c === C('@')) {
        if (!inLink) {
          const sp = this.mention(i, a, b);
          if (sp) { span(sp); i = sp.at + sp.len; continue; }
        }
      } else if (c === C('<')) {
        // A channel link, `<#conversation-id>`. `#name` typed as plain
        // characters is plain text and is never looked up.
        if (!inLink) {
          const sp = this.channel(i, b);
          if (sp) { span(sp); i = sp.at + sp.len; continue; }
        }
      } else if (c >= 0x30 && c <= 0x39) {
        // A typed instant (ruling R69). Not inside a link's text.
        if (!inLink) {
          const sp = this.instant(i, a, b);
          if (sp) { span(sp); i = sp.at + sp.len; continue; }
        }
      } else if (c === C(':')) {
        const sp = this.emoji(i, a, b);
        if (sp) { span(sp); i = sp.at + sp.len; continue; }
      } else if (c === C('*') || c === C('_') || c === C('~') || c === C('|')) {
        let j = i;
        while (j < b && src.charCodeAt(j) === c) j++;
        if (!this.marks(toks, c, i, j, a, b)) plain(i, j);
        i = j;
        continue;
      }

      plain(i, i + 1);
      i++;
    }
    return toks;
  }

  // Turns a run of one mark character, [i, j), into mark tokens. It reports
  // false if a run of this length is not a mark at all: only `*`, `**`,
  // `***`, `_`, `~~` and `||` are.
  marks(toks, c, i, j, a, b) {
    const n = j - i;
    const prev = runeBefore(this.src, i, a), next = runeAt(this.src, j, b);
    const hasPrev = prev >= 0, hasNext = next >= 0;
    // A mark opens only if text follows it directly and closes only if
    // text comes directly before it, so `2 * 3` and `a ** b` stay as typed.
    let open = hasNext && !isWhite(next);
    let shut = hasPrev && !isWhite(prev);
    const add = (mark, at, end) => toks.push({ at, end, mark, open, close: shut });
    if (c === C('*')) {
      if (n === 1) add(MARK_STAR, i, j);
      else if (n === 2) add(MARK_BOLD, i, j);
      else if (n === 3) {
        // Bold outside, italic inside. When the run can close, the inner
        // italic closes first.
        if (shut) { add(MARK_STAR, i, i + 1); add(MARK_BOLD, i + 1, j); } else { add(MARK_BOLD, i, i + 2); add(MARK_STAR, i + 2, j); }
      } else return false;
    } else if (c === C('_')) {
      if (n !== 1) return false;
      // `_` only at word edges, so snake_case names are left alone.
      open = open && !(hasPrev && isWord(prev));
      shut = shut && !(hasNext && isWord(next));
      add(MARK_UNDER, i, j);
    } else if (c === C('~')) {
      if (n !== 2) return false;
      add(MARK_STRIKE, i, j);
    } else {
      if (n !== 2) return false;
      add(MARK_SPOIL, i, j);
    }
    return true;
  }

  // Pairs the marks among toks and returns the finished spans.
  build(toks) {
    // closers[m] lists the tokens of mark m that can close, in order.
    const closers = Array.from({ length: MARK_COUNT }, () => []);
    toks.forEach((t, i) => { if (t.mark && t.close) closers[t.mark].push(i); });
    return this.pair(toks, 0, toks.length, closers, 0);
  }

  // Builds spans from toks[lo:hi]. active is the set of marks already open
  // around this range: a mark does not open inside itself.
  pair(toks, lo, hi, closers, active) {
    const out = [];
    const plain = (at, end) => {
      const last = out[out.length - 1];
      if (last && last.t === 'text' && last.at + last.len === at) { last.len = end - last.at; return; }
      out.push({ t: 'text', at, len: end - at });
    };
    for (let i = lo; i < hi; i++) {
      const t = toks[i];
      if (t.span) { out.push(t.span); continue; }
      if (t.mark && t.open && (active & (1 << t.mark)) === 0) {
        // The partner is the first closing mark of this kind with at
        // least one token between, inside the range being read.
        const cs = closers[t.mark];
        let k = lowerBound(cs, i + 2);
        if (k < cs.length && cs[k] < hi) {
          const j = cs[k];
          out.push({ t: MARK_KIND[t.mark], at: t.at, len: toks[j].end - t.at, c: this.pair(toks, i + 1, j, closers, active | (1 << t.mark)) });
          i = j;
          continue;
        }
      }
      plain(t.at, t.end);
    }
    return out;
  }

  // Whether a link starts at the `[` at i: `[text](address)`. The address
  // runs to the `)` that balances the opening one and holds no white
  // space, backtick, backslash, `<` or `>`. A link needs text to show; an
  // image does not, so emptyText lets `![](address)` through.
  linkAt(i, emptyText) {
    const { src, lo, hi } = this;
    const m = this.match[i - lo];
    if (m <= i || (m === i + 1 && !emptyText) || m + 1 >= hi || src.charCodeAt(m + 1) !== C('(')) return null;
    let depth = 0;
    for (let j = m + 2; j < hi; j++) {
      const c = src.charCodeAt(j);
      if (c === C('(')) depth++;
      else if (c === C(')')) {
        if (depth === 0) return { textEnd: m, urlAt: m + 2, urlEnd: j, end: j + 1, allowed: allowedURL(src, m + 2, j) };
        depth--;
      } else if (c < 0x20 || c === 0x7f || c === BACKTICK || c === BACKSLASH || c === C('<') || c === C('>') || isWhite(c)) return null;
    }
    return null;
  }

  // Where a bare link starting at i ends, or 0 if there is none: `http://`
  // or `https://` not in the middle of a word, then everything up to the
  // next white space, `<`, `>`, `"`, backtick or backslash. Punctuation
  // that almost always belongs to the sentence is then given back.
  bareLink(i, a, b) {
    const src = this.src;
    let scheme;
    if (hasPrefixFold(src, i, b, 'https://')) scheme = 8;
    else if (hasPrefixFold(src, i, b, 'http://')) scheme = 7;
    else return 0;
    const prev = runeBefore(src, i, a);
    if (prev >= 0 && isWord(prev)) return 0;
    let j = i + scheme;
    const opens = [0, 0, 0], closes = [0, 0, 0]; // ( [ {
    while (j < b) {
      const c = src.charCodeAt(j);
      if (c < 0x20 || c === 0x7f || c === C('<') || c === C('>') || c === C('"') || c === BACKTICK || c === BACKSLASH || isWhite(c)) break;
      if (c === C('(')) opens[0]++; else if (c === C('[')) opens[1]++; else if (c === C('{')) opens[2]++;
      else if (c === C(')')) closes[0]++; else if (c === C(']')) closes[1]++; else if (c === C('}')) closes[2]++;
      j++;
    }
    while (j > i + scheme) {
      const c = src.charCodeAt(j - 1);
      if ('.,!?:;\'*_~|'.includes(String.fromCharCode(c))) { j--; continue; }
      const k = c === C(')') ? 0 : c === C(']') ? 1 : c === C('}') ? 2 : -1;
      if (k < 0 || closes[k] <= opens[k]) break;
      closes[k]--;
      j--;
    }
    return j <= i + scheme ? 0 : j;
  }

  // `@name` or `@name@server.name` at i. It only decides that the text has
  // the shape of a mention.
  mention(i, a, b) {
    const src = this.src;
    if (wordBefore(src, i, a)) return null; // an email address, or the middle of a word
    const end = nameRun(src, i + 1, b, '._-');
    const name = src.slice(i + 1, end);
    if (!validUsername(name)) return null;
    if (end < b && src.charCodeAt(end) === C('@')) {
      // `@name@…` is a mention of someone on another server or it is
      // nothing.
      const srvEnd = serverRun(src, end + 1, b);
      if (srvEnd === end + 1 || wordFollows(src, srvEnd, b) || (srvEnd < b && src.charCodeAt(srvEnd) === C('@'))) return null;
      return { t: 'mention', at: i, len: srvEnd - i, name, server: src.slice(end + 1, srvEnd) };
    }
    if (wordFollows(src, end, b)) return null;
    return { t: 'mention', at: i, len: end - i, name };
  }

  // A channel link, `<#conversation-id>`, at i.
  channel(i, b) {
    const src = this.src;
    if (i + CHANNEL_LINK_LEN > b || src.charCodeAt(i + 1) !== C('#') || src.charCodeAt(i + CHANNEL_LINK_LEN - 1) !== C('>')) return null;
    const id = src.slice(i + 2, i + CHANNEL_LINK_LEN - 1);
    if (!validID(id)) return null;
    return { t: 'channel', at: i, len: CHANNEL_LINK_LEN, id };
  }

  // `:name:` or `:name@server.name:` at i. The name may be any short code,
  // standard or custom: 1 to 32 of `a-z 0-9 _ + -`.
  emoji(i, a, b) {
    const src = this.src;
    if (wordBefore(src, i, a)) return null; // `10:30:00`, `std::string`
    let j = i + 1;
    while (j < b && (isLowerAlnum(src.charCodeAt(j)) || src.charCodeAt(j) === C('_') || src.charCodeAt(j) === C('+') || src.charCodeAt(j) === C('-'))) j++;
    if (j === i + 1 || j - (i + 1) > 32 || j >= b) return null;
    const sp = { t: 'emoji', at: i, len: 0, name: src.slice(i + 1, j) };
    if (src.charCodeAt(j) === C('@')) {
      let k = j + 1;
      while (k < b && (isLowerAlnum(src.charCodeAt(k)) || (k > j + 1 && (src.charCodeAt(k) === C('.') || src.charCodeAt(k) === C('-'))))) k++;
      if (k === j + 1 || k - (j + 1) > 253 || !isLowerAlnum(src.charCodeAt(k - 1)) || k >= b) return null;
      sp.server = src.slice(j + 1, k);
      j = k;
    }
    if (src.charCodeAt(j) !== C(':') || wordFollows(src, j + 1, b)) return null;
    sp.len = j + 1 - i;
    return sp;
  }

  // A typed instant at i, a digit (ruling R69; the grammar is written out
  // in internal/markdown/instant.go and docs/MESSAGE-FORMAT.md 3.1).
  // local is "YYYY-MM-DDTHH:MM[:SS[.f]]"; zone is as written, if any.
  instant(i, a, b) {
    const src = this.src;
    const prev = runeBefore(src, i, a);
    if (prev >= 0 && (isWord(prev) || '.-+/:@#\\'.includes(String.fromCodePoint(prev)))) return null;
    const dig = (k) => k < b && isDigitUnit(src.charCodeAt(k));
    const digits = (j, n) => { for (let k = j; k < j + n; k++) if (!dig(k)) return false; return j + n <= b; };
    if (!digits(i, 4) || i + 11 > b || src[i + 4] !== '-' || !digits(i + 5, 2) || src[i + 7] !== '-' || !digits(i + 8, 2)) return null;
    let j = i + 10;
    const sepT = src[j] === 'T';
    if (!sepT && src[j] !== ' ') return null;
    j++;
    if (!digits(j, 2) || j + 5 > b || src[j + 2] !== ':' || !digits(j + 3, 2)) return null;
    j += 5;
    let seconds = false;
    if (digits(j + 1, 2) && src[j] === ':') {
      seconds = true;
      j += 3;
      if (j + 1 < b && src[j] === '.' && dig(j + 1)) {
        let k = j + 1;
        while (dig(k)) k++;
        if (k - (j + 1) > 9) return null;
        j = k;
      }
    }
    const local = src.slice(i, i + 10) + 'T' + src.slice(i + 11, j);
    let zone = '';
    if (j < b) {
      const c = src[j];
      if (c === 'Z') { zone = 'Z'; j++; } else if (c === '+' || c === '-') {
        const k = offsetRun(src, j, b);
        if (k > 0) {
          if (c === '-' && !sepT && !seconds) return null;
          zone = src.slice(j, k);
          j = k;
        }
      } else if (c === ' ') {
        const k = j + 1;
        if (k < b && isAsciiLetter(src.charCodeAt(k))) {
          let e = k;
          while (e < b && (isAsciiLetter(src.charCodeAt(e)) || dig(e) || '_+-/'.includes(src[e]))) e++;
          while (e > k && '+-/'.includes(src[e - 1])) e--;
          const name = src.slice(k, e);
          if (zoneShape(name) || name === 'UTC' || name === 'GMT') { zone = name; j = e; } else if (/^[A-Z]{1,4}T$/.test(name)) return null;
        } else if (k + 1 < b && (src[k] === '+' || src[k] === '-') && dig(k + 1)) return null;
      }
    }
    const next = runeAt(src, j, b);
    if (next >= 0) {
      if (isWord(next)) return null;
      if (':-+/.,\u2013'.includes(String.fromCodePoint(next)) && dig(j + 1)) return null;
    }
    if (!validLocal(local)) return null;
    const sp = { t: 'instant', at: i, len: j - i, local };
    if (zone) sp.zone = zone;
    return sp;
  }
}

const isDigitUnit = (c) => c >= 0x30 && c <= 0x39;
const isAsciiLetter = (c) => (c >= 0x61 && c <= 0x7a) || (c >= 0x41 && c <= 0x5a);

// A numeric offset at i: a sign, two digits, then optionally two more with
// or without a colon. The end, or 0 if it is not an offset the grammar
// allows (hours to 14, minutes to 59).
function offsetRun(src, i, b) {
  let j = i + 1;
  const d = (k) => k < b && isDigitUnit(src.charCodeAt(k));
  if (!d(j) || !d(j + 1)) return 0;
  j += 2;
  if (j + 3 <= b && src[j] === ':' && d(j + 1) && d(j + 2)) j += 3;
  else if (d(j) && d(j + 1)) j += 2;
  const digits = src.slice(i + 1, j).replace(':', '');
  const h = +digits.slice(0, 2), m = digits.length === 4 ? +digits.slice(2) : 0;
  return h <= 14 && m <= 59 ? j : 0;
}

// Whether a word looks like an IANA zone name: parts joined by "/", each
// starting with a letter.
function zoneShape(name) {
  const parts = name.split('/');
  return parts.length > 1 && parts.every((p) => p.length > 0 && isAsciiLetter(p.charCodeAt(0)));
}

// Whether "YYYY-MM-DDTHH:MM[:SS…]" names a real day and time.
export function validLocal(s) {
  const n = (a, b) => +s.slice(a, b);
  const y = n(0, 4), mo = n(5, 7), d = n(8, 10);
  if (y < 1 || mo < 1 || mo > 12 || d < 1) return false;
  const leap = (y % 4 === 0 && y % 100 !== 0) || y % 400 === 0;
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][mo - 1];
  if (d > days || n(11, 13) > 23 || n(14, 16) > 59) return false;
  return s.length < 19 || n(17, 19) <= 59;
}

// `<#`, the 36 characters of an identifier, and `>`.
const CHANNEL_LINK_LEN = 2 + 36 + 1;

// The first index in the sorted list xs whose value is at least v.
function lowerBound(xs, v) {
  let lo = 0, hi = xs.length;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (xs[mid] < v) lo = mid + 1; else hi = mid;
  }
  return lo;
}

// The end of the longest run of name characters starting at i: lowercase
// letters and digits, plus the characters in extra after the first. Then
// trailing characters from extra are given back, because a `.` or `-` at
// the end of a name is nearly always the sentence's punctuation.
function nameRun(src, i, b, extra) {
  let j = i;
  while (j < b && (isLowerAlnum(src.charCodeAt(j)) || (j > i && extra.includes(src[j])))) j++;
  while (j > i && !isLowerAlnum(src.charCodeAt(j - 1))) j--;
  return j;
}

// The end of a server name starting at i: a host name in lower case.
function serverRun(src, i, b) {
  const j = nameRun(src, i, b, '.-');
  return j - i > 253 ? i : j;
}

const wordFollows = (src, i, b) => { const r = runeAt(src, i, b); return r >= 0 && isWord(r); };
const wordBefore = (src, i, a) => { const r = runeBefore(src, i, a); return r >= 0 && isWord(r); };

// parse reads Churr Markdown into the compact tree. It never fails: text
// that is not one of the constructs is plain text.
export function parse(text) {
  const src = String(text == null ? '' : text);
  return new Parser(src).blocks(splitLines(src), 0);
}

// parseInline reads text that may hold inline constructs only, as a card's
// field value does. Line breaks stay line breaks.
export function parseInline(text) {
  const src = String(text == null ? '' : text);
  const p = new Parser(src);
  const out = [];
  splitLines(src).forEach((ln, i, lines) => {
    if (i > 0) { const prev = lines[i - 1]; out.push({ t: 'br', at: prev.eol, len: prev.next - prev.eol }); }
    out.push(...p.inline(ln.start, ln.end));
  });
  return out;
}
