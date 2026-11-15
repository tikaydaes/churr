package perm

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// Roster is Recipients as the store keeps it in memory: who receives a
// conversation's live events, with a quick answer to "is this account one of
// them". The live hub asks this for every event, so it must not cost a read
// of every member each time. The roster is shared: read it only.
func (c *Checker) Roster(ctx context.Context, conv model.ConversationID) (*store.Roster, error) {
	return c.s.Members().Roster(ctx, conv)
}
