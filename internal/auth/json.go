package auth

import (
	"context"
	"encoding/json"
	"net/http"
)

func jsonMarshal(v any) (json.RawMessage, error) { return json.Marshal(v) }

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// newGet builds a GET request.
func newGet(ctx context.Context, addr string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
}
