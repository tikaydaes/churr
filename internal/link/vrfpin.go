package link

import (
	"context"
	"encoding/json"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/store"
)

// The VRF key pin of a direct link (docs/DESIGN-FAIR.md section 1): the
// other server's VRF key history is pinned when the link is made, beside
// its signing and box keys. On every later handshake the history shown must
// be the pinned one, entry for entry, with nothing or more keys after it,
// each signed by the pinned signing key (the hello's checks have seen to the
// signatures). A history that grew is pinned again and audited; anything
// else is refused and audited, because it would let the server prove a
// chance outcome under a key nobody saw it begin.

// checkVRFPin compares the history a server shows with the one pinned for
// it, and pins what it shows when that is sound.
func (s *Service) checkVRFPin(ctx context.Context, sv store.Server, shown []wire.VRFKey) error {
	var pinned []wire.VRFKey
	if len(sv.VRFKeys) > 0 {
		if err := json.Unmarshal(sv.VRFKeys, &pinned); err != nil {
			return err
		}
	}
	added, ok := wire.VRFKeysExtend(pinned, shown)
	if !ok {
		s.audit(ctx, s.store, "", "link.vrf_key_mismatch", "server", string(sv.ID), map[string]any{"name": sv.Name,
			"pinned": len(pinned), "shown": len(shown)})
		return wire.Errf(wire.EKeyMismatch, "%s shows a VRF key history other than the pinned one with keys added after it.", sv.Name)
	}
	if added == 0 {
		return nil
	}
	b, err := json.Marshal(shown)
	if err != nil {
		return err
	}
	if err := s.store.Peers().SetServerVRFKeys(ctx, sv.ID, b); err != nil {
		return err
	}
	if len(pinned) > 0 {
		s.audit(ctx, s.store, "", "link.vrf_key_added", "server", string(sv.ID), map[string]any{"name": sv.Name, "added": added})
	}
	return nil
}
