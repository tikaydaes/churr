package preview

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/tikaydaes/churr/internal/blob"
)

// FetchImage fetches one image by address under the link-preview rules of
// docs/FEATURES.md section 23.3 (the guarded dialer, http on 80 and https on
// 443, at most three redirects, five seconds, five megabytes) and returns
// it re-encoded: at most 720 pixels on the long edge, with metadata
// removed, as a JPEG, or a PNG if it has transparency. Anything that does
// not decode as PNG, JPEG, GIF or WebP is an error. A card that names an
// image by address (a converted webhook payload) gets its image this way,
// so no client is ever made to load anything from another site.
func (s *Service) FetchImage(ctx context.Context, address string) ([]byte, error) {
	u, err := url.Parse(strings.TrimSpace(address))
	if err != nil {
		return nil, fmt.Errorf("%w: not an address", ErrRefused)
	}
	if _, err := checkURL(u); err != nil {
		return nil, err
	}
	release, err := s.hold(ctx, strings.ToLower(u.Hostname()))
	if err != nil {
		return nil, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, ImageTimeout)
	defer cancel()
	data, _, _, err := s.get(ctx, u.String(), MaxImageBytes, true)
	if err != nil {
		return nil, err
	}
	thumb, err := blob.MakeThumbnail(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return thumb.Data, nil
}
