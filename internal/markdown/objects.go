package markdown

import (
	"regexp"
	"sort"
	"sync"

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

// objectScan is what the resolver keeps for finding objects.
type objectScan struct {
	ready    bool
	patterns []model.ReferencePattern
	hints    []model.ObjectHint
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
	if try(model.EntityReference) {
		if e, ok := res.reference(i, b); ok {
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
