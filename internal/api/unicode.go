package api

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/unicodename"
)

// The names of Unicode code points, for the codepoint object
// (docs/MESSAGE-FORMAT.md section 3.2). Clients ask only when a person
// opens one; entities carry the code point alone.

func init() {
	register(func(a *API) {
		a.handle("lookupCodePoints", a.lookupCodePoints)
	})
}

// codePointsAsked is the most a request may ask for.
const codePointsAsked = 16

func (a *API) lookupCodePoints(r *Request) error {
	raw := r.R.URL.Query().Get("code_points")
	parts := strings.Split(raw, ",")
	if raw == "" || len(parts) > codePointsAsked {
		return model.Invalid("code_points", fmt.Sprintf("Give 1 to %d code points.", codePointsAsked))
	}
	out := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		// An unencoded `+` arrives as a space.
		p = strings.ToUpper(strings.TrimSpace(p))
		p = strings.TrimPrefix(strings.TrimPrefix(p, "U+"), "U ")
		v, err := strconv.ParseUint(p, 16, 32)
		if err != nil || p == "" || len(p) > 6 || v > 0x10FFFF {
			return model.Invalid("code_points", "Each code point is 1 to 6 hex digits, up to 10FFFF.")
		}
		name, cat := unicodename.Lookup(rune(v))
		out = append(out, map[string]any{
			"code_point": fmt.Sprintf("U+%04X", v), "name": name, "category": cat,
			"category_name": unicodename.CategoryName(cat),
		})
	}
	// The answer is fixed for a version of Unicode.
	r.W.Header().Set("Cache-Control", "private, max-age=31536000")
	return r.JSON(map[string]any{"version": unicodename.Version, "code_points": out})
}
