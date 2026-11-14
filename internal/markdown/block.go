package markdown

// Parse reads Churr Markdown into a tree of blocks and inline spans. It never
// fails: text that is not one of the constructs is plain text.
func Parse(text string) Doc {
	return parseUnits(toUnits(text), nil)
}

// parseUnits parses text already in UTF-16. guard, if not nil, marks ranges
// the inline parser must copy as typed (see Plain).
func parseUnits(src []uint16, guard []int) Doc {
	p := &parser{src: src, guard: guard}
	return Doc{Blocks: p.blocks(splitLines(src), 0), src: src}
}

type parser struct {
	src []uint16
	// guard[i], when not zero, is the end of a range starting at i that is
	// to be kept as plain characters.
	guard []int
}

// line is one line of the text as a block sees it. A quote strips its `> `
// from each line by moving start forward, so that the blocks inside it are
// parsed by the same code as blocks at the top.
type line struct {
	start int // first unit of the line, after any prefix a container took
	end   int // after the last unit that is not a trailing space or tab
	eol   int // where the line ending starts
	next  int // where the next line starts (len(src) on the last line)
}

func (l line) blank() bool { return l.start >= l.end }

// splitLines cuts the text at \n, \r\n and \r. Trailing spaces and tabs are
// cut off every line here, once, so no construct has to think about them.
func splitLines(src []uint16) []line {
	var lines []line
	start := 0
	for start < len(src) {
		eol := start
		for eol < len(src) && src[eol] != '\n' && src[eol] != '\r' {
			eol++
		}
		next := eol
		if next < len(src) {
			next++
			if src[eol] == '\r' && next < len(src) && src[next] == '\n' {
				next++
			}
		}
		end := eol
		for end > start && isBlank(src[end-1]) {
			end--
		}
		lines = append(lines, line{start: start, end: end, eol: eol, next: next})
		start = next
	}
	return lines
}

// blocks parses a run of lines into blocks. quotes is how many quotes these
// lines are already inside.
//
// Each line is looked at once and is either the start of a construct or one
// more line of the paragraph being collected. There is no backtracking: a
// construct that turns out not to be one (a fence that is never closed, a
// table that is too large) hands its lines to the paragraph.
func (p *parser) blocks(lines []line, quotes int) []Block {
	var out []Block
	var para []line
	flush := func() {
		if len(para) > 0 {
			out = append(out, p.paragraph(para))
			para = nil
		}
	}

	// closeAt[i] is the first line at or after i that closes a code block,
	// or -1. Worked out once so that many unclosed fences cost nothing.
	closeAt := make([]int, len(lines)+1)
	closeAt[len(lines)] = -1
	for i := len(lines) - 1; i >= 0; i-- {
		closeAt[i] = closeAt[i+1]
		if p.isFenceClose(lines[i]) {
			closeAt[i] = i
		}
	}

	for i := 0; i < len(lines); {
		ln := lines[i]
		if ln.blank() {
			flush()
			i++
			continue
		}
		c := p.src[ln.start]
		switch {
		case c == '`':
			if lang, ok := p.fenceOpen(ln); ok && closeAt[i+1] >= 0 {
				flush()
				end := closeAt[i+1]
				out = append(out, p.codeBlock(lines[i:end+1], lang))
				i = end + 1
				continue
			}

		case c == '-' && p.isRule(ln):
			flush()
			out = append(out, Block{Kind: Rule, At: ln.start, Len: ln.end - ln.start})
			i++
			continue

		case c == '#':
			if h, ok := p.heading(ln); ok {
				flush()
				out = append(out, h)
				i++
				continue
			}

		case c == '>' && p.isQuoteLine(ln):
			j := i
			var inner []line
			for j < len(lines) && !lines[j].blank() && p.isQuoteLine(lines[j]) {
				l := lines[j]
				// Trailing spaces are already gone, so a line is either
				// a bare `>` or `> ` and something.
				if l.end-l.start == 1 {
					l.start++
				} else {
					l.start += 2
				}
				inner = append(inner, l)
				j++
			}
			var bs []Block
			if quotes < MaxQuoteDepth {
				bs = p.blocks(inner, quotes+1)
			}
			if len(bs) > 0 {
				flush()
				last := lines[j-1]
				out = append(out, Block{Kind: Quote, At: ln.start, Len: last.end - ln.start, Blocks: bs})
			} else {
				// A quote of nothing, or one nested too deep: as typed.
				para = append(para, lines[i:j]...)
			}
			i = j
			continue

		case c == '|':
			if n, ok := p.tableAt(lines, i); ok {
				if t, ok := p.table(lines[i : i+n]); ok {
					flush()
					out = append(out, t)
				} else {
					// Too many columns or rows: the whole table is
					// shown as typed.
					para = append(para, lines[i:i+n]...)
				}
				i += n
				continue
			}
		}

		if _, ok := p.item(ln); ok {
			j := i
			var raw []rawItem
			for j < len(lines) && !lines[j].blank() {
				it, ok := p.item(lines[j])
				if !ok {
					break
				}
				raw = append(raw, it)
				j++
			}
			flush()
			out = append(out, p.lists(raw)...)
			i = j
			continue
		}

		para = append(para, ln)
		i++
	}
	flush()
	return out
}

// paragraph makes a paragraph of consecutive lines. Each line is read on its
// own, with a line break between: no inline construct crosses a line.
func (p *parser) paragraph(lines []line) Block {
	first, last := lines[0], lines[len(lines)-1]
	b := Block{Kind: Paragraph, At: first.start, Len: last.end - first.start}
	for i, ln := range lines {
		if i > 0 {
			prev := lines[i-1]
			b.Inlines = append(b.Inlines, Inline{Kind: Break, At: prev.eol, Len: prev.next - prev.eol})
		}
		b.Inlines = append(b.Inlines, p.inline(ln.start, ln.end)...)
	}
	return b
}

// heading reads `# `, `## ` or `### ` and some text. More `#` are level
// three. A `#` with no space after it is not a heading, which is what lets
// `#channel` start a line.
func (p *parser) heading(ln line) (Block, bool) {
	i := ln.start
	for i < ln.end && p.src[i] == '#' {
		i++
	}
	level := i - ln.start
	if i >= ln.end || !isBlank(p.src[i]) {
		return Block{}, false
	}
	for i < ln.end && isBlank(p.src[i]) {
		i++
	}
	// Trailing spaces are cut, so after the spaces there is always text.
	if level > MaxHeadingLevel {
		level = MaxHeadingLevel
	}
	return Block{Kind: Heading, At: ln.start, Len: ln.end - ln.start, Level: level, Inlines: p.inline(i, ln.end)}, true
}

// isRule reports whether the line is three or more `-` and nothing else.
func (p *parser) isRule(ln line) bool {
	if ln.end-ln.start < 3 {
		return false
	}
	for i := ln.start; i < ln.end; i++ {
		if p.src[i] != '-' {
			return false
		}
	}
	return true
}

// isQuoteLine reports whether the line is `>` alone or starts with `> `.
func (p *parser) isQuoteLine(ln line) bool {
	if ln.blank() || p.src[ln.start] != '>' {
		return false
	}
	return ln.end-ln.start == 1 || p.src[ln.start+1] == ' '
}

// fenceOpen reports whether the line opens a code block: three backticks,
// then optionally a word naming the language. A backtick later in the line
// means it is inline code written with three backticks, not a fence.
func (p *parser) fenceOpen(ln line) (lang string, ok bool) {
	s := p.src[ln.start:ln.end]
	if len(s) < 3 || s[0] != '`' || s[1] != '`' || s[2] != '`' {
		return "", false
	}
	for _, c := range s[3:] {
		if c == '`' {
			return "", false
		}
	}
	i := 3
	for i < len(s) && isBlank(s[i]) {
		i++
	}
	j := i
	for j < len(s) && !isBlank(s[j]) {
		j++
	}
	return language(s[i:j]), true
}

// isFenceClose reports whether the line is exactly three backticks.
func (p *parser) isFenceClose(ln line) bool {
	s := p.src[ln.start:ln.end]
	return len(s) == 3 && s[0] == '`' && s[1] == '`' && s[2] == '`'
}

// codeBlock makes a code block of the lines from the opening fence to the
// closing one. The code is kept exactly, trailing spaces included, with the
// line endings made "\n".
func (p *parser) codeBlock(lines []line, lang string) Block {
	first, last := lines[0], lines[len(lines)-1]
	var code []uint16
	for i, ln := range lines[1 : len(lines)-1] {
		if i > 0 {
			code = append(code, '\n')
		}
		code = append(code, p.src[ln.start:ln.eol]...)
	}
	return Block{Kind: CodeBlock, At: first.start, Len: last.end - first.start, Lang: lang, Code: unitsString(code)}
}

// rawItem is one list line before the lines are arranged into nested lists.
type rawItem struct {
	indent  int  // spaces before the marker
	depth   int  // nesting level, 0 to MaxListDepth-1, set by lists
	ordered bool // `1.` rather than `-` or `*`
	number  int
	at      int // the marker
	text    int // where the item's text starts
	end     int
	task    bool
	checked bool
}

// item reports whether the line is a list item: spaces, a marker, at least
// one space, and some text.
func (p *parser) item(ln line) (rawItem, bool) {
	s := p.src
	i := ln.start
	for i < ln.end && s[i] == ' ' {
		i++
	}
	it := rawItem{indent: i - ln.start, at: i, end: ln.end}
	switch {
	case i < ln.end && (s[i] == '-' || s[i] == '*'):
		i++
	case i < ln.end && isDigit(s[i]):
		// Nine digits at most, so the number fits in every language's
		// integer without care.
		for i < ln.end && isDigit(s[i]) && i-it.at < 9 {
			it.number = it.number*10 + int(s[i]-'0')
			i++
		}
		if i >= ln.end || s[i] != '.' {
			return rawItem{}, false
		}
		i++
		it.ordered = true
	default:
		return rawItem{}, false
	}
	if i >= ln.end || !isBlank(s[i]) {
		return rawItem{}, false
	}
	for i < ln.end && isBlank(s[i]) {
		i++
	}
	it.text = i

	// A box makes a bullet a checklist item, but only with text after it:
	// an item with nothing to tick is just a bullet that says "[ ]".
	if !it.ordered && ln.end-i > 4 && s[i] == '[' && s[i+2] == ']' && isBlank(s[i+3]) &&
		(s[i+1] == ' ' || s[i+1] == 'x' || s[i+1] == 'X') {
		j := i + 3
		for j < ln.end && isBlank(s[j]) {
			j++
		}
		it.task = true
		it.checked = s[i+1] != ' '
		it.text = j
	}
	return it, true
}

// lists arranges consecutive list lines into lists. One run of lines can
// give several lists, because a numbered item after bullets (or the other
// way round) at the same level starts a new list.
func (p *parser) lists(raw []rawItem) []Block {
	// The first line sets the left edge. From there two spaces of indent
	// are one level. An item can be at most one level deeper than the item
	// before it, and never deeper than the third level: anything further
	// in joins the deepest level allowed.
	base := raw[0].indent
	prev := -1
	for i := range raw {
		d := 0
		if raw[i].indent > base {
			d = (raw[i].indent - base) / 2
		}
		if d > prev+1 {
			d = prev + 1
		}
		if d > MaxListDepth-1 {
			d = MaxListDepth - 1
		}
		raw[i].depth = d
		prev = d
	}
	var out []Block
	for i := 0; i < len(raw); {
		out = append(out, p.list(raw, &i, raw[i].depth))
	}
	return out
}

// list reads one list starting at raw[*i]: the items at this depth of the
// same kind, each with the deeper lists that follow it.
func (p *parser) list(raw []rawItem, i *int, depth int) Block {
	first := raw[*i]
	b := Block{Kind: List, At: first.at, Ordered: first.ordered}
	if first.ordered {
		b.Start = first.number
	}
	end := first.end
	for *i < len(raw) && raw[*i].depth == depth && raw[*i].ordered == b.Ordered {
		r := raw[*i]
		*i++
		it := Item{At: r.at, Len: r.end - r.at, Task: r.task, Checked: r.checked, Inlines: p.inline(r.text, r.end)}
		end = r.end
		for *i < len(raw) && raw[*i].depth > depth {
			sub := p.list(raw, i, depth+1)
			end = sub.At + sub.Len
			it.Lists = append(it.Lists, sub)
		}
		b.Items = append(b.Items, it)
	}
	b.Len = end - b.At
	return b
}

// cellRange is the content of one table cell, spaces around it removed.
type cellRange struct{ start, end int }

// row splits a table row into cells. A row starts with `|`. Cells end at
// each further `|` that has no backslash before it; a last cell does not
// need a closing `|`.
func (p *parser) row(ln line) ([]cellRange, bool) {
	s := p.src
	if ln.blank() || s[ln.start] != '|' {
		return nil, false
	}
	var cells []cellRange
	add := func(a, b int) {
		for a < b && isBlank(s[a]) {
			a++
		}
		for b > a && isBlank(s[b-1]) {
			b--
		}
		cells = append(cells, cellRange{a, b})
	}
	start := ln.start + 1
	for i := start; i < ln.end; i++ {
		switch s[i] {
		case '\\':
			i++ // whatever follows a backslash is not a cell boundary
		case '|':
			add(start, i)
			start = i + 1
		}
	}
	if start < ln.end {
		add(start, ln.end)
	}
	return cells, len(cells) > 0
}

// alignments reads the row under the header: one cell of dashes per header
// cell, with colons saying which way the column is aligned.
func (p *parser) alignments(ln line, columns int) ([]Align, bool) {
	cells, ok := p.row(ln)
	if !ok || len(cells) != columns {
		return nil, false
	}
	out := make([]Align, 0, len(cells))
	for _, c := range cells {
		a, b := c.start, c.end
		left := a < b && p.src[a] == ':'
		if left {
			a++
		}
		right := b > a && p.src[b-1] == ':'
		if right {
			b--
		}
		if a >= b {
			return nil, false
		}
		for i := a; i < b; i++ {
			if p.src[i] != '-' {
				return nil, false
			}
		}
		switch {
		case left && right:
			out = append(out, AlignCenter)
		case left:
			out = append(out, AlignLeft)
		case right:
			out = append(out, AlignRight)
		default:
			out = append(out, AlignNone)
		}
	}
	return out, true
}

// tableAt reports whether a table starts at lines[i], and how many lines it
// takes: the header, the row of dashes, and every following row that starts
// with `|` and has no more cells than the header.
func (p *parser) tableAt(lines []line, i int) (n int, ok bool) {
	head, ok := p.row(lines[i])
	if !ok || i+1 >= len(lines) {
		return 0, false
	}
	if _, ok := p.alignments(lines[i+1], len(head)); !ok {
		return 0, false
	}
	j := i + 2
	for j < len(lines) {
		cells, ok := p.row(lines[j])
		if !ok || len(cells) > len(head) {
			break
		}
		j++
	}
	return j - i, true
}

// table makes a table of the lines tableAt found. ok is false if it is
// larger than the format allows.
func (p *parser) table(lines []line) (Block, bool) {
	head, _ := p.row(lines[0])
	if len(head) > MaxTableColumns || len(lines)-1 > MaxTableRows {
		return Block{}, false
	}
	align, _ := p.alignments(lines[1], len(head))
	first, last := lines[0], lines[len(lines)-1]
	b := Block{Kind: Table, At: first.start, Len: last.end - first.start, Align: align}
	cells := func(ln line, rs []cellRange) []Cell {
		out := make([]Cell, 0, len(head))
		for _, r := range rs {
			out = append(out, Cell{At: r.start, Len: r.end - r.start, Inlines: p.inline(r.start, r.end)})
		}
		// A short row is filled out with empty cells at the end of its line.
		for len(out) < len(head) {
			out = append(out, Cell{At: ln.end})
		}
		return out
	}
	b.Head = cells(first, head)
	for _, ln := range lines[2:] {
		rs, _ := p.row(ln)
		b.Rows = append(b.Rows, cells(ln, rs))
	}
	return b, true
}
