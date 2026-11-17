package markdown

import (
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/tikaydaes/churr/internal/model"
)

// Objects are the things recognised inside plain text beyond instants
// (ruling R73, docs/MESSAGE-FORMAT.md section 3.2): references to tickets
// and the like. Unlike a mention they are not nodes of the tree: each lies
// inside one run of plain text, and a client draws one by cutting that run
// where the entity says. So they are never found in code, in a link's text
// or address, in a bare link, in an escape, across formatting marks or in
// a checklist item, because none of those is a run of plain text.
//
// Every object stands on the same boundaries. Before it comes the start of
// the text, white space, or one of ( [ { " ' ~ * _ | > , ; : = “ ‘ « — –.
// After it comes the end of the text, white space, one of
// ) ] } " ' * _ ~ | < ” ’ » … — – ! ?, or one of . , ; : that no letter or
// digit follows. So `#fff` in `page.html#fff`, `a/PROJ-1` and `PROJ-1/x`
// are all plain text.

// PatternSource may also be implemented by a Resolver. Patterns returns the
// reference patterns that apply where the text is written: the channel's
// own and the server's. Their order does not matter.
type PatternSource interface {
	Patterns() []model.ReferencePattern
}

// HintSource may also be implemented by a Resolver. ObjectHints returns what
// the author chose in the composer for ranges of the text: `plain` for a
// range in which nothing is to be recognised, or the kind of object a range
// is to be read as where it could be more than one (`#123456`, a colour
// where a pattern would make it a reference). A hint that does not fit the
// text is ignored: the text is then read as if it had not been sent.
type HintSource interface {
	ObjectHints() []model.ObjectHint
}

// OriginSource may also be implemented by a Resolver. Origin is the
// server's own origin ("https://chat.example.org"), for links to its
// messages; "" finds none.
type OriginSource interface {
	Origin() string
}

// The ways inline dice are treated (DiceSource).
const (
	DiceOff  = ""     // left as text: a bot, a webhook, no resolver
	DiceRoll = "roll" // rolled, with the source's die: a person posting
	DiceEdit = "edit" // marked removed, nothing rolled: an edit
)

// DiceSource may also be implemented by a Resolver. InlineDice says what
// becomes of `[[2d6+3]]` (ruling R73): DiceOff, DiceRoll with die, which
// gives a whole number from 1 to sides, or DiceEdit.
type DiceSource interface {
	InlineDice() (mode string, die func(sides int) int)
}

// objectScan is what the resolver keeps for finding objects.
type objectScan struct {
	ready    bool
	patterns []model.ReferencePattern
	hints    []model.ObjectHint
	origin   string
}

func (res *resolver) scan() *objectScan {
	o := &res.obj
	if o.ready {
		return o
	}
	o.ready = true
	if ps, ok := res.r.(PatternSource); ok {
		o.patterns = orderPatterns(ps.Patterns())
	}
	if os, ok := res.r.(OriginSource); ok {
		o.origin = strings.ToLower(os.Origin())
	}
	if hs, ok := res.r.(HintSource); ok {
		o.hints = hs.ObjectHints()
		if len(o.hints) > model.MaxObjectHints {
			o.hints = o.hints[:model.MaxObjectHints]
		}
	}
	return o
}

// orderPatterns puts patterns in the order they are tried at a position:
// a channel's before the server's, so that a channel's pattern wins over
// the server's with the same match; then the longer prefix, so that `GH-`
// is not read as `G` and a value; then the older.
func orderPatterns(in []model.ReferencePattern) []model.ReferencePattern {
	out := append([]model.ReferencePattern(nil), in...)
	sort.SliceStable(out, func(a, b int) bool {
		x, y := out[a], out[b]
		if (x.ConversationID != "") != (y.ConversationID != "") {
			return x.ConversationID != ""
		}
		if len(x.Prefix) != len(y.Prefix) {
			return len(x.Prefix) > len(y.Prefix)
		}
		if x.CreatedAt != y.CreatedAt {
			return x.CreatedAt < y.CreatedAt
		}
		return x.ID < y.ID
	})
	return out
}

// objects finds the objects in one run of plain text, [a, b).
func (res *resolver) objects(a, b int) {
	src := res.src
	if b > len(src) {
		// Inside a link's text, or a Doc put together by hand, which
		// has no text to read.
		return
	}
	hints := res.scan().hints
	for i := a; i < b; {
		// Inside a range the author made plain, nothing is looked for.
		// A range the author gave a kind is read as that kind when the
		// object of that kind is exactly that range.
		only := ""
		skip := 0
		for _, h := range hints {
			switch {
			case h.Kind == model.ObjectPlain && i >= h.Start && i < h.End:
				skip = h.End
			case h.Kind != model.ObjectPlain && h.Start == i:
				only = h.Kind
			}
		}
		if skip > i {
			i = skip
			continue
		}
		if leftEdge(src, i) {
			e, ok := model.Entity{}, false
			if only != "" {
				if e, ok = res.objectAt(i, b, only); ok && !hinted(hints, e) {
					ok = false
				}
			}
			if !ok {
				e, ok = res.objectAt(i, b, "")
			}
			// An object may not reach into a range made plain.
			for _, h := range hints {
				if ok && h.Kind == model.ObjectPlain && e.At < h.End && h.Start < e.At+e.Len {
					ok = false
				}
			}
			if ok {
				res.out = append(res.out, e)
				i = e.At + e.Len
				continue
			}
		}
		i++
	}
}

// plainOver reports whether the author made any of [a, b) plain.
func (res *resolver) plainOver(a, b int) bool {
	for _, h := range res.scan().hints {
		if h.Kind == model.ObjectPlain && a < h.End && h.Start < b {
			return true
		}
	}
	return false
}

// hinted reports whether a hint names exactly this object.
func hinted(hints []model.ObjectHint, e model.Entity) bool {
	for _, h := range hints {
		if h.Kind == e.Type && h.Start == e.At && h.End == e.At+e.Len {
			return true
		}
	}
	return false
}

// objectAt reads the object that starts at i, if there is one. The kinds
// are tried in a fixed order, which is how `#123` is a reference where a
// pattern says so and a colour elsewhere. only, when set, is the one kind
// to try.
func (res *resolver) objectAt(i, b int, only string) (model.Entity, bool) {
	try := func(kind string) bool { return only == "" || only == kind }
	if try(model.EntityMessageLink) {
		if e, ok := res.churrLink(i, b); ok {
			return e, true
		}
	}
	if try(model.EntityReference) {
		if e, ok := res.reference(i, b); ok {
			return e, true
		}
	}
	if try(model.EntityMeasure) {
		if e, ok := measure(res.src, i, b); ok {
			return e, true
		}
	}
	if try(model.EntityColour) {
		if e, ok := colour(res.src, i, b); ok {
			return e, true
		}
	}
	if try(model.EntityNumber) {
		if e, ok := number(res.src, i, b); ok {
			return e, true
		}
	}
	if try(model.EntityCodePoint) {
		if e, ok := codePoint(res.src, i, b); ok {
			return e, true
		}
	}
	if try(model.EntityPhone) {
		if e, ok := phone(res.src, i, b); ok {
			return e, true
		}
	}
	if try(model.EntityEmail) {
		if e, ok := email(res.src, i, b); ok {
			return e, true
		}
	}
	if try(model.EntityGeo) {
		if e, ok := geo(res.src, i, b); ok {
			return e, true
		}
	}
	if try(model.EntityDice) {
		if e, ok := res.dice(i, b); ok {
			return e, true
		}
	}
	return model.Entity{}, false
}

// leftEdge reports whether an object may start at i.
func leftEdge(src []uint16, i int) bool {
	r, ok := runeBefore(src, i, 0)
	return !ok || isWhite(r) || isRuneIn(r, "([{\"'~*_|>,;:=“‘«—–")
}

// rightEdge reports whether an object may end at j.
func rightEdge(src []uint16, j int) bool {
	r, ok := runeAt(src, j, len(src))
	if !ok || isWhite(r) || isRuneIn(r, ")]}\"'*_~|<”’»…—–!?") {
		return true
	}
	if isRuneIn(r, ".,;:") {
		next, ok := runeAt(src, j+1, len(src))
		return !ok || !isWord(next)
	}
	return false
}

// asciiRun returns src[i:b] as a string, up to the first character outside
// ASCII and no longer than max.
func asciiRun(src []uint16, i, b, max int) string {
	if b > i+max {
		b = i + max
	}
	buf := make([]byte, 0, b-i)
	for k := i; k < b && src[k] < 0x80; k++ {
		buf = append(buf, byte(src[k]))
	}
	return string(buf)
}

// refExpressions holds each value expression compiled, anchored at the
// start. Expressions are checked when a pattern is saved
// (model.CheckRefExpression); one that does not compile matches nothing.
var refExpressions sync.Map // string → *regexp.Regexp, or nil

func refExpression(expr string) *regexp.Regexp {
	if v, ok := refExpressions.Load(expr); ok {
		re, _ := v.(*regexp.Regexp)
		return re
	}
	var re *regexp.Regexp
	if ok, _ := model.CheckRefExpression(expr); ok {
		re, _ = regexp.Compile(`^(?:` + expr + `)`)
	}
	refExpressions.Store(expr, re)
	return re
}

// reference reads a reference at i: a pattern's prefix and, at once after
// it, the longest value its expression matches there.
func (res *resolver) reference(i, b int) (model.Entity, bool) {
	src := res.src
	for _, p := range res.scan().patterns {
		n := len(p.Prefix)
		if n == 0 || i+n >= b || !hasPrefixExact(src[i:b], p.Prefix) {
			continue
		}
		re := refExpression(p.ValueExpression())
		if re == nil {
			continue
		}
		value := re.FindString(asciiRun(src, i+n, b, model.MaxRefValue))
		if value == "" || !rightEdge(src, i+n+len(value)) {
			continue
		}
		return model.Entity{Type: model.EntityReference, At: i, Len: n + len(value),
			PatternID: p.ID, Value: value, URL: p.URL(value)}, true
	}
	return model.Entity{}, false
}

// hasPrefixExact reports whether the units start with the ASCII string
// prefix, case and all.
func hasPrefixExact(u []uint16, prefix string) bool {
	if len(u) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if u[i] != uint16(prefix[i]) {
			return false
		}
	}
	return true
}

// Message links (docs/MESSAGE-FORMAT.md section 3.2). "Copy link" writes
// a message's address as the server's origin and one of
//
//	/chat/c/{conversation}/m/{message}
//	/chat/c/{conversation}/t/{thread}             the thread's first message
//	/chat/c/{conversation}/t/{thread}/m/{reply}   a reply in a thread
//
// and the apps take churr://{host} and the same path. Identifiers are
// written lower case with hyphens. Nothing may follow: no query, no
// fragment, no slash.

// messagePath reads the path after the origin and reports the conversation
// and the message it names, and how many characters it took.
func messagePath(p string) (conv, msg string, n int) {
	const c = "/chat/c/"
	if !strings.HasPrefix(p, c) || len(p) < len(c)+36 || !model.ValidID(p[len(c):len(c)+36]) {
		return "", "", 0
	}
	conv = p[len(c) : len(c)+36]
	rest := p[len(c)+36:]
	part := func(s, mark string) (string, bool) {
		if len(s) >= len(mark)+36 && strings.HasPrefix(s, mark) && model.ValidID(s[len(mark):len(mark)+36]) {
			return s[len(mark) : len(mark)+36], true
		}
		return "", false
	}
	n = len(c) + 36
	if id, ok := part(rest, "/m/"); ok {
		return conv, id, n + 39
	}
	if id, ok := part(rest, "/t/"); ok {
		if reply, ok := part(rest[39:], "/m/"); ok {
			return conv, reply, n + 78
		}
		return conv, id, n + 39
	}
	return "", "", 0
}

// MessageLink reads a whole address as a link to a message of the server
// at origin, https or churr://. It reports the conversation and the
// message.
func MessageLink(link, origin string) (conv, msg string, ok bool) {
	at := linkStart(link, strings.ToLower(origin))
	if at == 0 {
		return "", "", false
	}
	conv, msg, n := messagePath(link[at:])
	if n == 0 || at+n != len(link) {
		return "", "", false
	}
	return conv, msg, true
}

// linkStart reports how much of s is the origin, as https or as churr://
// with the same host, or 0.
func linkStart(s, origin string) int {
	if origin == "" {
		return 0
	}
	if len(s) >= len(origin) && strings.EqualFold(s[:len(origin)], origin) {
		return len(origin)
	}
	if k := strings.Index(origin, "://"); k > 0 {
		app := "churr://" + origin[k+3:]
		if len(s) >= len(app) && strings.EqualFold(s[:len(app)], app) {
			return len(app)
		}
	}
	return 0
}

// churrLink reads a churr:// message link at i in a run of text. (An https
// one is a bare link, which Resolve reads.)
func (res *resolver) churrLink(i, b int) (model.Entity, bool) {
	origin := res.scan().origin
	k := strings.Index(origin, "://")
	if k < 0 || !hasPrefixExact(res.src[i:b], "churr://") {
		return model.Entity{}, false
	}
	s := asciiRun(res.src, i, b, len(origin)+200)
	at := linkStart(s, origin)
	if at == 0 || !strings.HasPrefix(s, "churr://") {
		return model.Entity{}, false
	}
	conv, msg, n := messagePath(s[at:])
	if n == 0 || !rightEdge(res.src, i+at+n) {
		return model.Entity{}, false
	}
	return model.Entity{Type: model.EntityMessageLink, At: i, Len: at + n, URL: s[:at+n],
		ConversationID: model.ConversationID(conv), MessageID: model.MessageID(msg)}, true
}

// bareMessageLink turns a bare link into a message link when it is one.
func (res *resolver) bareMessageLink(e *model.Entity) {
	if res.plainOver(e.At, e.At+e.Len) {
		return
	}
	if conv, msg, ok := MessageLink(e.URL, res.scan().origin); ok {
		e.Type, e.ConversationID, e.MessageID = model.EntityMessageLink, model.ConversationID(conv), model.MessageID(msg)
	}
}

// Measurements (docs/MESSAGE-FORMAT.md section 3.2): a number, at most one
// space (U+0020, U+00A0 or U+202F), and a unit of the fixed table, written
// exactly as listed. Only a temperature may be negative. A number is 1 to
// 9 digits and optionally a point and 1 to 6 more; one that a digit and a
// point or comma come straight before (`1,000 km`, `1.2.3 m`) is not read.

// measureUnits, the table, is written by tools/units-gen/gen.py
// (units_gen.go).

// measure reads a measurement at i.
func measure(src []uint16, i, b int) (model.Entity, bool) {
	j := i
	neg := j < b && (src[j] == '-' || src[j] == 0x2212)
	if neg {
		j++
	}
	if before, ok := runeBefore(src, i, 0); ok && (before == '.' || before == ',') {
		if b2, ok := runeBefore(src, i-1, 0); ok && b2 >= '0' && b2 <= '9' {
			return model.Entity{}, false
		}
	}
	k := j
	for k < b && isDigit(src[k]) {
		k++
	}
	if k == j || k-j > 9 {
		return model.Entity{}, false
	}
	if k+1 < b && src[k] == '.' && isDigit(src[k+1]) {
		f := k + 1
		for f < b && isDigit(src[f]) {
			f++
		}
		if f-k-1 > 6 {
			return model.Entity{}, false
		}
		k = f
	}
	if k < b && isDigit(src[k]) || k < b && (src[k] == '.' || src[k] == ',') && k+1 < b && isDigit(src[k+1]) {
		return model.Entity{}, false
	}
	value := unitsString(src[j:k])
	if neg {
		value = "-" + value
	}
	u := k
	spaced := u < b && (src[u] == ' ' || src[u] == 0xA0 || src[u] == 0x202F)
	if spaced {
		u++
	}
	for _, unit := range measureUnits {
		if spaced && !unit.spaced || neg && !unit.temp {
			continue
		}
		typed := utf16.Encode([]rune(unit.typed))
		if u+len(typed) > b || !sameUnits(src[u:u+len(typed)], typed) || !rightEdge(src, u+len(typed)) {
			continue
		}
		return model.Entity{Type: model.EntityMeasure, At: i, Len: u + len(typed) - i, Value: value, Unit: unit.code}, true
	}
	return model.Entity{}, false
}

func sameUnits(a, b []uint16) bool {
	for k := range a {
		if a[k] != b[k] {
			return false
		}
	}
	return true
}

// Colours (docs/MESSAGE-FORMAT.md section 3.2): `#` and 3, 6 or 8 hex
// digits, in either case, on the edges. Value is the colour as #rrggbb or
// #rrggbbaa in lower case.
func colour(src []uint16, i, b int) (model.Entity, bool) {
	if src[i] != '#' {
		return model.Entity{}, false
	}
	k := i + 1
	for k < b && k-i <= 9 && isHex(src[k]) {
		k++
	}
	n := k - i - 1
	if n != 3 && n != 6 && n != 8 || !rightEdge(src, k) {
		return model.Entity{}, false
	}
	hex := strings.ToLower(unitsString(src[i+1 : k]))
	if n == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	return model.Entity{Type: model.EntityColour, At: i, Len: k - i, Value: "#" + hex}, true
}

// Numbers in another base (docs/MESSAGE-FORMAT.md section 3.2): `0x`,
// `0b` or `0o` (either case) and digits of that base, with single `_`
// between digits, on the edges, below 2^128. Value is the number in
// decimal; Base is 16, 2 or 8.
func number(src []uint16, i, b int) (model.Entity, bool) {
	if i+2 >= b || src[i] != '0' {
		return model.Entity{}, false
	}
	base := 0
	switch src[i+1] {
	case 'x', 'X':
		base = 16
	case 'b', 'B':
		base = 2
	case 'o', 'O':
		base = 8
	default:
		return model.Entity{}, false
	}
	k := i + 2
	var digits []byte
	for k < b {
		c := src[k]
		if c == '_' && len(digits) > 0 && k+1 < b && digitOf(src[k+1], base) {
			k++
			continue
		}
		if !digitOf(c, base) {
			break
		}
		digits = append(digits, byte(c))
		k++
	}
	if len(digits) == 0 || k < b && src[k] == '_' || !rightEdge(src, k) {
		return model.Entity{}, false
	}
	n, ok := new(big.Int).SetString(string(digits), base)
	if !ok || n.BitLen() > 128 {
		return model.Entity{}, false
	}
	return model.Entity{Type: model.EntityNumber, At: i, Len: k - i, Value: n.String(), Base: base}, true
}

func digitOf(c uint16, base int) bool {
	switch base {
	case 2:
		return c == '0' || c == '1'
	case 8:
		return c >= '0' && c <= '7'
	}
	return isHex(c)
}

// Code points (docs/MESSAGE-FORMAT.md section 3.2): `U+` and 4 to 6 hex
// digits, `&#x` 1 to 6 and `;`, `\u{` 1 to 6 and `}`, `\u` and 4, or
// `\U` and 8, naming a Unicode scalar value (not a surrogate, at most
// 10FFFF), on the edges. Value is `U+` and at least 4 upper-case digits.
func codePoint(src []uint16, i, b int) (model.Entity, bool) {
	at := func(k int, s string) bool {
		if k+len(s) > b {
			return false
		}
		for j := 0; j < len(s); j++ {
			if src[k+j] != uint16(s[j]) {
				return false
			}
		}
		return true
	}
	hexRun := func(k, max int) int {
		n := 0
		for k+n < b && n <= max && isHex(src[k+n]) {
			n++
		}
		return n
	}
	var start, n, end int
	switch {
	case at(i, "U+"):
		start = i + 2
		n = hexRun(start, 6)
		if n < 4 || n > 6 {
			return model.Entity{}, false
		}
		end = start + n
	case at(i, "&#x") || at(i, "&#X"):
		start = i + 3
		n = hexRun(start, 6)
		if n < 1 || n > 6 || !at(start+n, ";") {
			return model.Entity{}, false
		}
		end = start + n + 1
	case at(i, "\\u{"):
		start = i + 3
		n = hexRun(start, 6)
		if n < 1 || n > 6 || !at(start+n, "}") {
			return model.Entity{}, false
		}
		end = start + n + 1
	case at(i, "\\u"):
		start = i + 2
		n = hexRun(start, 4)
		if n != 4 {
			return model.Entity{}, false
		}
		end = start + n
	case at(i, "\\U"):
		start = i + 2
		n = hexRun(start, 8)
		if n != 8 {
			return model.Entity{}, false
		}
		end = start + n
	default:
		return model.Entity{}, false
	}
	if !rightEdge(src, end) {
		return model.Entity{}, false
	}
	v, err := strconv.ParseUint(unitsString(src[start:start+n]), 16, 32)
	if err != nil || v > 0x10FFFF || v >= 0xD800 && v <= 0xDFFF {
		return model.Entity{}, false
	}
	return model.Entity{Type: model.EntityCodePoint, At: i, Len: end - i, Value: fmt.Sprintf("U+%04X", v)}, true
}

func isHex(c uint16) bool { return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

// Telephone numbers (docs/MESSAGE-FORMAT.md section 3.2): only the
// international form, `+` and 7 to 15 digits, the first not 0, in groups
// that one space, hyphen or dot may separate, a group optionally in
// brackets. A bracketed lone 0 (`+44 (0)20 …`) is not dialled. Value is
// `+` and the digits.
func phone(src []uint16, i, b int) (model.Entity, bool) {
	if src[i] != '+' || i+1 >= b || !isDigit(src[i+1]) || src[i+1] == '0' {
		return model.Entity{}, false
	}
	digits := []byte{'+'}
	j, last := i+1, i+1
	for j < b && j-i <= 32 {
		k := j
		if k > i+1 && (src[k] == ' ' || src[k] == '-' || src[k] == '.') {
			k++
		}
		open := k < b && src[k] == '(' && k > i+1
		if open {
			k++
		}
		start := k
		for k < b && isDigit(src[k]) {
			k++
		}
		if k == start || open && (k >= b || src[k] != ')') {
			break
		}
		group := unitsString(src[start:k])
		if open {
			k++
			if group == "0" {
				group = ""
			}
		}
		digits = append(digits, group...)
		j, last = k, k
	}
	n := len(digits) - 1
	if n < 7 || n > 15 || !rightEdge(src, last) {
		return model.Entity{}, false
	}
	return model.Entity{Type: model.EntityPhone, At: i, Len: last - i, Value: string(digits)}, true
}

// Email addresses: an ASCII local part of letters, digits and `. _ % + -`
// (not starting or ending with a dot, no two dots together), `@`, and a
// domain of two or more labels of letters, digits and hyphens (not at
// either end), the last all letters and at least two of them. At most 64
// characters before the `@` and 253 after. Value is the address as typed.
func email(src []uint16, i, b int) (model.Entity, bool) {
	isLocal := func(c uint16) bool {
		return isASCIILetter(c) || isDigit(c) || c == '.' || c == '_' || c == '%' || c == '+' || c == '-'
	}
	k := i
	for k < b && isLocal(src[k]) {
		k++
	}
	local := unitsString(src[i:k])
	if k == i || k-i > 64 || k >= b || src[k] != '@' || local[0] == '.' || local[len(local)-1] == '.' || strings.Contains(local, "..") {
		return model.Entity{}, false
	}
	d := k + 1
	end := d
	labels, lastStart := 0, d
	for {
		s := end
		for end < b && (isASCIILetter(src[end]) || isDigit(src[end]) || src[end] == '-') {
			end++
		}
		if end == s || src[s] == '-' || src[end-1] == '-' {
			return model.Entity{}, false
		}
		labels++
		lastStart = s
		if end+1 < b && src[end] == '.' && (isASCIILetter(src[end+1]) || isDigit(src[end+1])) {
			end++
			continue
		}
		break
	}
	tld := src[lastStart:end]
	if labels < 2 || len(tld) < 2 || end-d > 253 || !rightEdge(src, end) {
		return model.Entity{}, false
	}
	for _, c := range tld {
		if !isASCIILetter(c) {
			return model.Entity{}, false
		}
	}
	return model.Entity{Type: model.EntityEmail, At: i, Len: end - i, Value: unitsString(src[i:end])}, true
}

// Map coordinates (docs/MESSAGE-FORMAT.md section 3.2): a decimal
// latitude and longitude, `lat, lon` with at least three decimals each so
// that a list of numbers is not a place, or `geo:lat,lon` with any. The
// latitude is at most 90 and the longitude 180 either way.
func geo(src []uint16, i, b int) (model.Entity, bool) {
	j, min := i, 3
	if hasPrefixExact(src[i:b], "geo:") {
		j, min = i+4, 1
	} else {
		// Not after a number and a point or comma, with or without a
		// space: that is a version or a list.
		p := i
		if p > 0 && src[p-1] == ' ' {
			p--
		}
		if p >= 2 && (src[p-1] == '.' || src[p-1] == ',') && isDigit(src[p-2]) {
			return model.Entity{}, false
		}
	}
	lat, k, ok := degrees(src, j, b, 2, min, 90)
	if !ok || k >= b || src[k] != ',' {
		return model.Entity{}, false
	}
	k++
	if min == 3 && k < b && src[k] == ' ' {
		k++
	}
	lon, end, ok := degrees(src, k, b, 3, min, 180)
	if !ok || !rightEdge(src, end) {
		return model.Entity{}, false
	}
	// A third number after it makes it a list, not a place.
	if n := end + 1; end < b && src[end] == ',' {
		if n < b && src[n] == ' ' {
			n++
		}
		if n < b && (isDigit(src[n]) || src[n] == '-') {
			return model.Entity{}, false
		}
	}
	return model.Entity{Type: model.EntityGeo, At: i, Len: end - i, Lat: lat, Lon: lon}, true
}

// degrees reads [-]d.d at j: at most whole digits before the point, min
// to 8 after, no more than max. It reports the number and where it ends.
func degrees(src []uint16, j, b, whole, min int, max float64) (string, int, bool) {
	k := j
	if k < b && src[k] == '-' {
		k++
	}
	d := k
	for k < b && isDigit(src[k]) {
		k++
	}
	if k == d || k-d > whole || k+1 >= b || src[k] != '.' {
		return "", 0, false
	}
	k++
	f := k
	for k < b && isDigit(src[k]) {
		k++
	}
	if k-f < min || k-f > 8 {
		return "", 0, false
	}
	v := unitsString(src[j:k])
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n > max || n < -max {
		return "", 0, false
	}
	return v, k, true
}

// Inline dice (docs/MESSAGE-FORMAT.md section 3.2): `[[`, an expression
// of /roll's grammar (model.ParseDice) of 1 to 100 characters with no
// bracket or line break, and `]]`, on the edges. Value is the expression
// written out in full.
func (res *resolver) dice(i, b int) (model.Entity, bool) {
	mode, die := DiceOff, func(int) int { return 1 }
	if ds, ok := res.r.(DiceSource); ok {
		mode, die = ds.InlineDice()
	}
	if mode == DiceOff {
		return model.Entity{}, false
	}
	src := res.src
	if !hasPrefixExact(src[i:b], "[[") {
		return model.Entity{}, false
	}
	k := i + 2
	for k < b && k-i-2 <= model.MaxDiceText && src[k] != ']' && src[k] != '[' && src[k] != '\n' {
		k++
	}
	expr := unitsString(src[i+2 : k])
	if k+1 >= b || src[k] != ']' || src[k+1] != ']' || strings.TrimSpace(expr) == "" || !rightEdge(src, k+2) {
		return model.Entity{}, false
	}
	terms, written, err := model.ParseDice(expr)
	if err != nil {
		return model.Entity{}, false
	}
	e := model.Entity{Type: model.EntityDice, At: i, Len: k + 2 - i, Value: written}
	if mode == DiceEdit {
		e.Removed = true
		return e, true
	}
	rolled, total := model.RollDice(terms, die)
	e.Terms, e.Total = rolled, &total
	return e, true
}
