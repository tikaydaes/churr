package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("notices", testNotices) }

// testNotices holds the notices kept for an account: each account reads
// only its own, newest first in pages, and marking them seen counts them
// out of the unseen number.
func testNotices(t *testing.T, s store.Store) {
	ctx := context.Background()
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	base := model.Now()
	for i := 0; i < 3; i++ {
		must(t, s.Notices().Add(ctx, &model.Notice{AccountID: ann.ID, Kind: model.NoticeOwnerMade, ByID: bob.ID, SubjectID: bob.ID, CreatedAt: base.Add(time.Duration(i) * time.Second)}))
	}
	must(t, s.Notices().Add(ctx, &model.Notice{AccountID: bob.ID, Kind: model.NoticeRecoveryLinkUsed}))

	first, next, err := s.Notices().List(ctx, ann.ID, store.Page{Limit: 2})
	must(t, err)
	eq(t, "a page", len(first), 2)
	eq(t, "newest first", first[0].CreatedAt > first[1].CreatedAt, true)
	eq(t, "the fields", first[0].ByID == bob.ID && first[0].SubjectID == bob.ID && first[0].Kind == model.NoticeOwnerMade && first[0].SeenAt.IsZero(), true)
	rest, next2, err := s.Notices().List(ctx, ann.ID, store.Page{Limit: 2, Cursor: next})
	must(t, err)
	eq(t, "the rest", len(rest) == 1 && next2 == "", true)
	n, err := s.Notices().Unseen(ctx, ann.ID)
	must(t, err)
	eq(t, "unseen", n, 3)

	must(t, s.Notices().MarkSeen(ctx, ann.ID, base.Add(9*time.Second)))
	n, _ = s.Notices().Unseen(ctx, ann.ID)
	eq(t, "seen", n, 0)
	n, _ = s.Notices().Unseen(ctx, bob.ID)
	eq(t, "another's are not touched", n, 1)
	got, _, _ := s.Notices().List(ctx, bob.ID, store.Page{})
	eq(t, "only one's own, with no one who did it", len(got) == 1 && got[0].ByID == "" && got[0].SubjectID == "" && got[0].ConversationID == "", true)
}
