package storetest

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("files", testFiles) }

// filesNew stores an upload row.
func filesNew(t testing.TB, s store.Areas, conv model.ConversationID, by model.AccountID, name string) model.File {
	t.Helper()
	f := model.File{ConversationID: conv, UploaderID: by, Name: name, Size: 3, MediaType: "text/plain", Kind: model.FileText,
		SHA256: []byte{1, 2, 3}, BlobKey: "disk:aa/bb/" + name, ThumbKey: "disk:aa/bb/" + name + ".thumb"}
	must(t, s.Files().Create(ctx, &f))
	return f
}

func testFiles(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	c := newChannel(t, s, "files", true, ann.ID)
	other := newChannel(t, s, "elsewhere", true, ann.ID)
	m := post(t, s, c.ID, ann.ID, "", "with files")

	a := filesNew(t, s, c.ID, ann.ID, "a.txt")
	b := filesNew(t, s, c.ID, ann.ID, "b.txt")
	got, err := s.Files().Get(ctx, a.ID)
	must(t, err)
	eq(t, "url", got.URL, "/api/v1/files/"+string(a.ID)+"/a.txt")
	eq(t, "thumb url", got.ThumbURL, "/api/v1/files/"+string(a.ID)+"/thumb")
	eq(t, "ready is not spelled out", got.State, "")

	// Only the uploader's own unattached uploads to that conversation.
	bobs := filesNew(t, s, c.ID, bob.ID, "bobs.txt")
	wantErr(t, s.Files().Attach(ctx, m.ID, c.ID, ann.ID, []model.FileID{a.ID, bobs.ID}), model.ErrNotFound)
	elsewhere := filesNew(t, s, other.ID, ann.ID, "else.txt")
	wantErr(t, s.Files().Attach(ctx, m.ID, c.ID, ann.ID, []model.FileID{elsewhere.ID}), model.ErrNotFound)
	list, _ := s.Files().ForMessage(ctx, m.ID, true)
	eq(t, "nothing attached by a refused call", len(list), 0)

	must(t, s.Files().Attach(ctx, m.ID, c.ID, ann.ID, []model.FileID{b.ID, a.ID}))
	list, err = s.Files().ForMessage(ctx, m.ID, false)
	must(t, err)
	eq(t, "order given", len(list) == 2 && list[0].ID == b.ID && list[1].ID == a.ID, true)
	wantErr(t, s.Files().Attach(ctx, m.ID, c.ID, ann.ID, []model.FileID{a.ID}), model.ErrNotFound)

	msgs := []model.Message{m}
	must(t, s.Messages().Hydrate(ctx, msgs, ann.ID))
	eq(t, "hydrated in order", len(msgs[0].Files) == 2 && msgs[0].Files[0].ID == b.ID, true)

	gone, err := s.Files().Detach(ctx, m.ID, []model.FileID{a.ID}, model.Now())
	must(t, err)
	eq(t, "detached", len(gone) == 1 && gone[0].ID == b.ID, true)
	list, _ = s.Files().ForMessage(ctx, m.ID, false)
	eq(t, "one left", len(list), 1)
	list, _ = s.Files().ForMessage(ctx, m.ID, true)
	eq(t, "history still has both", len(list), 2)

	orphans, err := s.Files().Unattached(ctx, model.Now().Add(time.Hour), 10)
	must(t, err)
	eq(t, "unattached uploads", len(orphans), 2)
	del, err := s.Files().Delete(ctx, bobs.ID)
	must(t, err)
	eq(t, "blob keys to remove", len(store.BlobKeys(del)), 2)
	_, err = s.Files().Get(ctx, bobs.ID)
	wantErr(t, err, model.ErrNotFound)

	// A conversation's files' blobs are handed back when it is deleted.
	out, err := s.Conversations().Delete(ctx, c.ID)
	must(t, err)
	eq(t, "keys of both files", len(out.BlobKeys), 4)
}
