package store

import (
	"encoding/base64"
	"encoding/json"

	"github.com/tikaydaes/churr/internal/model"
)

// Page asks for one page of a list that is not ordered by seq
// (docs/CONVENTIONS.md, Paging). The list method returns the cursor for the
// next page, or "" when there is no more.
type Page struct {
	// Limit is how many to return. Clamp fixes 0 and out-of-range values.
	Limit int
	// Cursor is the next cursor of the previous page, or "" for the first.
	Cursor string
}

// Clamp returns the limit to use: the default for 0, and never more than the
// cap.
func (p Page) Clamp() int {
	switch {
	case p.Limit <= 0:
		return model.DefaultLimit
	case p.Limit > model.MaxLimit:
		return model.MaxLimit
	}
	return p.Limit
}

// SeqPage asks for one page of a list ordered by seq. Before and After are
// exclusive bounds; 0 means none. With neither, the newest page is returned.
// Results are always in ascending seq order.
type SeqPage struct {
	Before int64
	After  int64
	Limit  int
}

// Clamp returns the limit to use.
func (p SeqPage) Clamp() int { return Page{Limit: p.Limit}.Clamp() }

// EncodeCursor makes an opaque cursor out of the sort key of the last row of
// a page. Implementations choose what goes in; callers never look inside.
func EncodeCursor(key any) string {
	b, err := json.Marshal(key)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCursor reads a cursor back into key. A cursor that was not made by
// EncodeCursor for the same list is the client's mistake.
func DecodeCursor(cursor string, key any) error {
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err == nil {
		err = json.Unmarshal(b, key)
	}
	if err != nil {
		return model.Invalid("cursor", "That cursor is not one this list gave out.")
	}
	return nil
}
