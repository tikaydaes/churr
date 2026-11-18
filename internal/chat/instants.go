package chat

import (
	"sort"
	"unicode/utf16"

	"github.com/tikaydaes/churr/internal/instant"
	"github.com/tikaydaes/churr/internal/markdown"
	"github.com/tikaydaes/churr/internal/model"
)

// Instants is what a posting client says about the dates and times in a
// message's text (ruling R69, docs/MESSAGE-FORMAT.md section 3.1).
type Instants struct {
	// TimeZone is the author's IANA zone, in which a date and time typed
	// without a zone is read. Empty (bots, older clients) leaves such text
	// plain.
	TimeZone string
	// Hints are phrases the author accepted as instants in the composer
	// ("this Friday at 5pm"), which the server cannot read for itself.
	Hints []model.InstantHint
	// Objects are the author's choices for ranges of the text: plain
	// text, or one kind of object (ruling R73, references.go).
	Objects []model.ObjectHint
	// Claims is how objects that modules claim, such as inline dice, are
	// treated (claims.go): resolved for a person posting, marked for an
	// edit, else left as text.
	Claims string
}

// maxHints bounds the hints one message may carry; the rest are dropped.
const maxHints = 50

// withHints adds an instant entity for each hint that holds up, and drops
// the rest without a word: a hint is the client's reading of the author's
// words, and a wrong one must never stop the message. A hint holds up when
// its range lies inside the text on character boundaries, overlaps no other
// entity, code or link, its time is a sane RFC 3339 instant, its precision
// minute or second, and its zone an offset or a known IANA name. The range
// must also lie inside one run of plain text (markdown.TextSpans), so that
// it reaches into no code, link or formatting and every client can draw it.
func withHints(text string, doc markdown.Doc, entities []model.Entity, hints []model.InstantHint) []model.Entity {
	if len(hints) == 0 {
		return entities
	}
	units := utf16.Encode([]rune(text))
	runs := markdown.TextSpans(doc)
	inRun := func(at, end int) bool {
		for _, r := range runs {
			if at >= r.At && end <= r.At+r.Len {
				return true
			}
		}
		return false
	}
	var taken []markdown.Span
	for _, e := range entities {
		taken = append(taken, markdown.Span{At: e.At, Len: e.Len})
	}
	overlaps := func(at, end int) bool {
		for _, s := range taken {
			if at < s.At+s.Len && s.At < end {
				return true
			}
		}
		return false
	}
	low := func(i int) bool { return i < len(units) && units[i] >= 0xDC00 && units[i] <= 0xDFFF }
	added := false
	for i, h := range hints {
		if i == maxHints {
			break
		}
		if h.Start < 0 || h.End <= h.Start || h.End > len(units) || low(h.Start) || low(h.End) {
			continue
		}
		if h.Precision != instant.Minute && h.Precision != instant.Second {
			continue
		}
		t, ok := instant.ParseTime(h.Time)
		if !ok || !instant.ValidZone(h.Zone) || !inRun(h.Start, h.End) || overlaps(h.Start, h.End) {
			continue
		}
		entities = append(entities, model.Entity{Type: model.EntityInstant, At: h.Start, Len: h.End - h.Start,
			Time: instant.Format(t), Precision: h.Precision, Zone: h.Zone})
		taken = append(taken, markdown.Span{At: h.Start, Len: h.End - h.Start})
		added = true
	}
	if added {
		sort.SliceStable(entities, func(a, b int) bool { return entities[a].At < entities[b].At })
	}
	return entities
}

// keptInstants is the instant and reference entities of a message whose
// text an edit leaves alone: they were resolved with the zone, the hints
// and the patterns of the moment the text was written, which an edit of
// its cards or files does not repeat. So is the absence of a reference:
// one the author kept as plain text, or one no pattern matched then.
func keptInstants(old, now []model.Entity) []model.Entity {
	kept := func(e model.Entity) bool {
		return e.Type == model.EntityInstant || e.Type == model.EntityEvent || e.Type == model.EntityReference || isResolved(e)
	}
	var keep []model.Entity
	for _, e := range old {
		if kept(e) {
			keep = append(keep, e)
		}
	}
	out := make([]model.Entity, 0, len(now)+len(keep))
	for _, e := range now {
		if !kept(e) {
			out = append(out, e)
		}
	}
	out = append(out, keep...)
	sort.SliceStable(out, func(a, b int) bool { return out[a].At < out[b].At })
	return out
}
