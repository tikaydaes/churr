package markdown

import (
	"sort"

	"github.com/tikaydaes/churr/internal/model"
)

// The inline parser reads one piece of one line: a paragraph line, a heading,
// a list item, a table cell. It works in three steps, each a single pass, so
// that odd input cannot make it slow or surprising:
//
//  1. Find escapes and inline code. They win over everything else and
//     nothing inside them is looked at again.
//  2. Cut the rest into tokens: finished spans (links, mentions, emoji and
//     so on), marks (`**`, `*`, `_`, `~~`, `||`) and plain characters.
//  3. Pair the marks. A mark that can open pairs with the first later mark
//     of the same kind that can close; what lies between is read the same
//     way. A mark that finds no partner is plain text.

// segment is the state for one piece of a line, [lo, hi) of the text.
type segment struct {
	p      *parser
	src    []uint16
	lo, hi int
	// atomEnd[i-lo], when not zero, is the end of an escape or inline code
	// that starts at i.
	atomEnd []int
	// match[i-lo], when not zero, is the `]` that closes the `[` at i.
	match []int
}

// inline parses [lo, hi) of the text into spans.
func (p *parser) inline(lo, hi int) []Inline {
	if lo >= hi {
		return nil
	}
	s := &segment{p: p, src: p.src, lo: lo, hi: hi}
	s.findAtoms()
	s.matchBrackets()
	return s.build(s.tokens(lo, hi, false))
}

// findAtoms marks escapes and inline code.
//
// Inline code opens with a run of backticks and closes at the next run of
// exactly the same length, so code holding a backtick can be written with
// two around it. A run that is never closed is plain text.
func (s *segment) findAtoms() {
	s.atomEnd = make([]int, s.hi-s.lo)
	// unclosed remembers run lengths already found to have no closing run,
	// so a line of many lone backticks is still read in one pass.
	var unclosed map[int]bool
	for i := s.lo; i < s.hi; {
		c := s.src[i]
		if c == '\\' && i+1 < s.hi && isPunct(s.src[i+1]) {
			s.atomEnd[i-s.lo] = i + 2
			i += 2
			continue
		}
		if c != '`' {
			i++
			continue
		}
		j := i
		for j < s.hi && s.src[j] == '`' {
			j++
		}
		n := j - i
		if !unclosed[n] {
			if end := s.closingRun(j, n); end > 0 {
				s.atomEnd[i-s.lo] = end
				i = end
				continue
			}
			if unclosed == nil {
				unclosed = make(map[int]bool)
			}
			unclosed[n] = true
		}
		i = j
	}
}

// closingRun finds the first run of exactly n backticks at or after from and
// returns the position after it, or 0.
func (s *segment) closingRun(from, n int) int {
	for i := from; i < s.hi; {
		if s.src[i] != '`' {
			i++
			continue
		}
		j := i
		for j < s.hi && s.src[j] == '`' {
			j++
		}
		if j-i == n {
			return j
		}
		i = j
	}
	return 0
}

// matchBrackets pairs each `[` with its `]`, skipping escapes and code, so
// that link text may itself hold brackets.
func (s *segment) matchBrackets() {
	s.match = make([]int, s.hi-s.lo)
	var open []int
	for i := s.lo; i < s.hi; {
		if end := s.atomEnd[i-s.lo]; end > 0 {
			i = end
			continue
		}
		switch s.src[i] {
		case '[':
			open = append(open, i)
		case ']':
			if n := len(open); n > 0 {
				s.match[open[n-1]-s.lo] = i
				open = open[:n-1]
			}
		}
		i++
	}
}

// mark is a kind of formatting mark.
type mark uint8

const (
	markNone   mark = iota
	markBold        // **
	markStar        // *
	markUnder       // _
	markStrike      // ~~
	markSpoil       // ||
	markCount
)

func (m mark) kind() InlineKind {
	switch m {
	case markBold:
		return Bold
	case markStrike:
		return Strike
	case markSpoil:
		return Spoiler
	}
	return Italic
}

// token is a piece of a segment: a finished span, a mark, or plain
// characters (mark == markNone and span.Kind == "").
type token struct {
	at, end     int
	span        Inline // a finished span, if span.Kind is set
	mark        mark
	open, close bool
}

// tokens cuts [a, b) into tokens. inLink is true while reading the text of a
// link, where links, bare links, mentions and channel names are not looked
// for: a link cannot hold another link, and a name inside a link's text is
// part of that link, not a mention.
func (s *segment) tokens(a, b int, inLink bool) []token {
	var toks []token
	plain := func(at, end int) {
		if n := len(toks); n > 0 && toks[n-1].mark == markNone && toks[n-1].span.Kind == "" && toks[n-1].end == at {
			toks[n-1].end = end
			return
		}
		toks = append(toks, token{at: at, end: end})
	}
	span := func(sp Inline) {
		toks = append(toks, token{at: sp.At, end: sp.At + sp.Len, span: sp})
	}
	src := s.src

	for i := a; i < b; {
		if g := s.p.guard; g != nil && g[i] > i && g[i] <= b {
			plain(i, g[i])
			i = g[i]
			continue
		}
		if end := s.atomEnd[i-s.lo]; end > 0 && end <= b {
			if src[i] == '\\' {
				span(Inline{Kind: Escape, At: i, Len: 2, Text: unitsString(src[i+1 : i+2])})
			} else {
				n := 0
				for src[i+n] == '`' {
					n++
				}
				span(Inline{Kind: Code, At: i, Len: end - i, Text: unitsString(src[i+n : end-n])})
			}
			i = end
			continue
		}

		c := src[i]
		switch c {
		case '!':
			if !inLink && i+1 < b && src[i+1] == '[' {
				if l, ok := s.linkAt(i+1, true); ok && l.allowed {
					span(Inline{Kind: Image, At: i, Len: l.end - i, URL: unitsString(src[l.urlAt:l.urlEnd])})
					i = l.end
					continue
				}
			}

		case '[':
			if !inLink {
				if l, ok := s.linkAt(i, false); ok {
					sp := Inline{Kind: NoLink, At: i, Len: l.end - i}
					if l.allowed {
						sp.Kind = Link
						sp.URL = unitsString(src[l.urlAt:l.urlEnd])
					}
					sp.Children = s.build(s.tokens(i+1, l.textEnd, true))
					span(sp)
					i = l.end
					continue
				}
			}

		case 'h', 'H':
			if !inLink {
				if end := s.bareLink(i, a, b); end > 0 {
					span(Inline{Kind: BareLink, At: i, Len: end - i, URL: unitsString(src[i:end])})
					i = end
					continue
				}
			}

		case '@':
			if !inLink {
				if sp, ok := s.mention(i, a, b); ok {
					span(sp)
					i = sp.At + sp.Len
					continue
				}
			}

		case '<':
			// A channel link, `<#conversation-id>`. `#name` typed as
			// plain characters is plain text and is never looked up
			// (ruling R52).
			if !inLink {
				if sp, ok := s.channel(i, b); ok {
					span(sp)
					i = sp.At + sp.Len
					continue
				}
			}

		case ':':
			if sp, ok := s.emoji(i, a, b); ok {
				span(sp)
				i = sp.At + sp.Len
				continue
			}

		case '*', '_', '~', '|':
			j := i
			for j < b && src[j] == c {
				j++
			}
			if !s.marks(&toks, c, i, j, a, b) {
				plain(i, j)
			}
			i = j
			continue
		}

		plain(i, i+1)
		i++
	}
	return toks
}

// marks turns a run of one mark character, [i, j), into mark tokens. It
// reports false if a run of this length is not a mark at all: only `*`,
// `**`, `***` (bold and italic together), `_`, `~~` and `||` are.
func (s *segment) marks(toks *[]token, c uint16, i, j, a, b int) bool {
	n := j - i
	prev, hasPrev := runeBefore(s.src, i, a)
	next, hasNext := runeAt(s.src, j, b)
	// A mark opens only if text follows it directly and closes only if text
	// comes directly before it, so `2 * 3` and `a ** b` stay as typed.
	open := hasNext && !isWhite(next)
	shut := hasPrev && !isWhite(prev)

	add := func(m mark, at, end int) {
		*toks = append(*toks, token{at: at, end: end, mark: m, open: open, close: shut})
	}
	switch c {
	case '*':
		switch n {
		case 1:
			add(markStar, i, j)
		case 2:
			add(markBold, i, j)
		case 3:
			// Bold outside, italic inside: `***a***`. When the run can
			// close, the inner italic closes first.
			if shut {
				add(markStar, i, i+1)
				add(markBold, i+1, j)
			} else {
				add(markBold, i, i+2)
				add(markStar, i+2, j)
			}
		default:
			return false
		}
	case '_':
		if n != 1 {
			return false
		}
		// `_` only at word edges, so snake_case names are left alone.
		open = open && !(hasPrev && isWord(prev))
		shut = shut && !(hasNext && isWord(next))
		add(markUnder, i, j)
	case '~':
		if n != 2 {
			return false
		}
		add(markStrike, i, j)
	case '|':
		if n != 2 {
			return false
		}
		add(markSpoil, i, j)
	}
	return true
}

// build pairs the marks among toks and returns the finished spans.
func (s *segment) build(toks []token) []Inline {
	// closers[m] lists the tokens of mark m that can close, in order, so
	// that finding a partner is a search rather than a scan.
	var closers [markCount][]int
	for i, t := range toks {
		if t.mark != markNone && t.close {
			closers[t.mark] = append(closers[t.mark], i)
		}
	}
	return s.pair(toks, 0, len(toks), &closers, 0)
}

// pair builds spans from toks[lo:hi]. active is the set of marks already
// open around this range: a mark does not open inside itself, which both
// matches what a reader expects and bounds how deep spans can nest.
func (s *segment) pair(toks []token, lo, hi int, closers *[markCount][]int, active uint) []Inline {
	var out []Inline
	plain := func(at, end int) {
		if n := len(out); n > 0 && out[n-1].Kind == Text && out[n-1].At+out[n-1].Len == at {
			out[n-1].Len = end - out[n-1].At
			return
		}
		out = append(out, Inline{Kind: Text, At: at, Len: end - at})
	}
	for i := lo; i < hi; i++ {
		t := toks[i]
		switch {
		case t.span.Kind != "":
			out = append(out, t.span)
		case t.mark != markNone && t.open && active&(1<<t.mark) == 0:
			// The partner is the first closing mark of this kind with
			// at least one token between, inside the range being read.
			cs := closers[t.mark]
			k := sort.SearchInts(cs, i+2)
			if k < len(cs) && cs[k] < hi {
				j := cs[k]
				out = append(out, Inline{
					Kind:     t.mark.kind(),
					At:       t.at,
					Len:      toks[j].end - t.at,
					Children: s.pair(toks, i+1, j, closers, active|1<<t.mark),
				})
				i = j
				continue
			}
			plain(t.at, t.end)
		default:
			plain(t.at, t.end)
		}
	}
	for i := range out {
		if out[i].Kind == Text {
			out[i].Text = unitsString(s.src[out[i].At : out[i].At+out[i].Len])
		}
	}
	return out
}

// link is what linkAt found: `[text](address)`.
type link struct {
	textEnd       int // the `]`
	urlAt, urlEnd int
	end           int // after the `)`
	allowed       bool
}

// linkAt reports whether a link starts at the `[` at i. The address runs to
// the `)` that balances the opening one, so an address may hold balanced
// brackets, and it holds no white space, backtick, backslash, `<` or `>`.
// Whether the address is one a link may point at is a separate question,
// answered in allowed.
//
// A link needs text to show. An image does not (its text is never shown),
// so emptyText lets `![](address)` through.
func (s *segment) linkAt(i int, emptyText bool) (link, bool) {
	src := s.src
	m := s.match[i-s.lo]
	if m <= i || m == i+1 && !emptyText || m+1 >= s.hi || src[m+1] != '(' {
		return link{}, false
	}
	depth := 0
	for j := m + 2; j < s.hi; j++ {
		c := src[j]
		switch {
		case c == '(':
			depth++
		case c == ')':
			if depth == 0 {
				return link{textEnd: m, urlAt: m + 2, urlEnd: j, end: j + 1, allowed: allowedURL(src[m+2 : j])}, true
			}
			depth--
		case c < 0x20, c == 0x7F, c == '`', c == '\\', c == '<', c == '>', isWhite(rune(c)):
			return link{}, false
		}
	}
	return link{}, false
}

// bareLink reports where a bare link starting at i ends, or 0 if there is
// none. A bare link is `http://` or `https://` not in the middle of a word,
// followed by everything up to the next white space, `<`, `>`, `"`, backtick
// or backslash. Then punctuation that almost always belongs to the sentence
// is given back: trailing `. , ! ? : ; ' * _ ~ |`, and a closing bracket
// that has no opening partner inside the address.
func (s *segment) bareLink(i, a, b int) int {
	src := s.src
	scheme := 0
	switch {
	case hasPrefixFold(src[i:b], "https://"):
		scheme = 8
	case hasPrefixFold(src[i:b], "http://"):
		scheme = 7
	default:
		return 0
	}
	if prev, ok := runeBefore(src, i, a); ok && isWord(prev) {
		return 0
	}
	j := i + scheme
	var opens, closes [3]int // ( [ {
	for j < b {
		c := src[j]
		if c < 0x20 || c == 0x7F || c == '<' || c == '>' || c == '"' || c == '`' || c == '\\' || isWhite(rune(c)) {
			break
		}
		switch c {
		case '(':
			opens[0]++
		case '[':
			opens[1]++
		case '{':
			opens[2]++
		case ')':
			closes[0]++
		case ']':
			closes[1]++
		case '}':
			closes[2]++
		}
		j++
	}
trim:
	for j > i+scheme {
		k := -1
		switch src[j-1] {
		case '.', ',', '!', '?', ':', ';', '\'', '*', '_', '~', '|':
			j--
			continue
		case ')':
			k = 0
		case ']':
			k = 1
		case '}':
			k = 2
		default:
			break trim
		}
		if closes[k] <= opens[k] {
			break
		}
		closes[k]--
		j--
	}
	if j <= i+scheme {
		return 0
	}
	return j
}

// nameRun returns the end of the longest run of name characters starting at
// i: lowercase letters and digits, plus the characters in extra after the
// first. Then it gives back trailing characters from extra, because a `.`
// or `-` at the end of a name is nearly always the sentence's punctuation.
func nameRun(src []uint16, i, b int, extra string) int {
	j := i
	for j < b && (isLowerAlnum(src[j]) || j > i && isExtra(src[j], extra)) {
		j++
	}
	for j > i && !isLowerAlnum(src[j-1]) {
		j--
	}
	return j
}

func isExtra(c uint16, extra string) bool {
	for k := 0; k < len(extra); k++ {
		if c == uint16(extra[k]) {
			return true
		}
	}
	return false
}

// serverRun returns the end of a server name starting at i: a host name in
// lowercase. No check is made that it is a real one; a name that is not
// simply does not resolve.
func serverRun(src []uint16, i, b int) int {
	j := nameRun(src, i, b, ".-")
	if j-i > 253 {
		return i
	}
	return j
}

// wordFollows reports whether a word character starts at i. A name directly
// followed by one is not a name: `@samantha` must not mention `sam`.
func wordFollows(src []uint16, i, b int) bool {
	r, ok := runeAt(src, i, b)
	return ok && isWord(r)
}

// wordBefore reports whether a word character ends at i.
func wordBefore(src []uint16, i, a int) bool {
	r, ok := runeBefore(src, i, a)
	return ok && isWord(r)
}

// mention reads `@name` or `@name@server.name` at i. It only decides that
// the text has the shape of a mention; Resolve decides whether it is one.
func (s *segment) mention(i, a, b int) (Inline, bool) {
	src := s.src
	if wordBefore(src, i, a) {
		return Inline{}, false // an email address, or the middle of a word
	}
	end := nameRun(src, i+1, b, "._-")
	name := unitsString(src[i+1 : end])
	if !model.ValidUsername(name) {
		return Inline{}, false
	}
	if end < b && src[end] == '@' {
		// `@name@…` is a mention of someone on another server or it is
		// nothing. Falling back to a local `@name` here would notify
		// the wrong person.
		srvEnd := serverRun(src, end+1, b)
		if srvEnd == end+1 || wordFollows(src, srvEnd, b) || srvEnd < b && src[srvEnd] == '@' {
			return Inline{}, false
		}
		return Inline{Kind: Mention, At: i, Len: srvEnd - i, Name: name, Server: unitsString(src[end+1 : srvEnd])}, true
	}
	if wordFollows(src, end, b) {
		return Inline{}, false
	}
	return Inline{Kind: Mention, At: i, Len: end - i, Name: name}, true
}

// channelLinkLen is the length of a channel link: `<#`, the 36 characters
// of an identifier, and `>`.
const channelLinkLen = 2 + 36 + 1

// channel reads a channel link, `<#conversation-id>`, at i: the identifier
// exactly as identifiers are written everywhere, lower case with hyphens.
// Anything else that starts `<#` is plain text. A link needs no space
// around it: it is delimited by its own brackets.
func (s *segment) channel(i, b int) (Inline, bool) {
	src := s.src
	if i+channelLinkLen > b || src[i+1] != '#' || src[i+channelLinkLen-1] != '>' {
		return Inline{}, false
	}
	id := unitsString(src[i+2 : i+channelLinkLen-1])
	if !model.ValidID(id) {
		return Inline{}, false
	}
	return Inline{Kind: Channel, At: i, Len: channelLinkLen, ID: id}, true
}

// emoji reads `:name:` or `:name@server.name:` at i. The name may be any
// short code, standard or custom: 1 to 32 of `a-z 0-9 _ + -`. Which of the
// two it is, if either, is not this function's business.
func (s *segment) emoji(i, a, b int) (Inline, bool) {
	src := s.src
	if wordBefore(src, i, a) {
		return Inline{}, false // `10:30:00`, `std::string`
	}
	j := i + 1
	for j < b && (isLowerAlnum(src[j]) || src[j] == '_' || src[j] == '+' || src[j] == '-') {
		j++
	}
	if j == i+1 || j-(i+1) > 32 || j >= b {
		return Inline{}, false
	}
	sp := Inline{Kind: Emoji, At: i, Name: unitsString(src[i+1 : j])}
	if src[j] == '@' {
		k := j + 1
		for k < b && (isLowerAlnum(src[k]) || k > j+1 && (src[k] == '.' || src[k] == '-')) {
			k++
		}
		if k == j+1 || k-(j+1) > 253 || !isLowerAlnum(src[k-1]) || k >= b {
			return Inline{}, false
		}
		sp.Server = unitsString(src[j+1 : k])
		j = k
	}
	if src[j] != ':' || wordFollows(src, j+1, b) {
		return Inline{}, false
	}
	sp.Len = j + 1 - i
	return sp, true
}
