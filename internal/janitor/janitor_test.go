package janitor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/storetest"
)

type fakePreviews struct{ calls int }

func (f *fakePreviews) Expire(context.Context) (int, error) { f.calls++; return 2, nil }

func TestRunOnce(t *testing.T) {
	ctx := context.Background()
	st := storetest.OpenSQLite(t)
	dir := t.TempDir()
	blobs, err := blob.Disk(dir)
	if err != nil {
		t.Fatal(err)
	}
	previews := &fakePreviews{}
	j := New(Deps{Store: st, Blobs: blobs, Previews: previews})
	now := model.Now()
	const day = 24 * time.Hour

	ann := model.Account{Username: "ann", DisplayName: "Ann", Role: model.RoleOwner}
	if _, err := st.Accounts().Create(ctx, &ann); err != nil {
		t.Fatal(err)
	}
	channel := func(name string, retention *int64) model.Conversation {
		c := model.Conversation{Kind: model.KindChannel, Name: name, CreatedByID: ann.ID, RetentionSeconds: retention}
		if err := st.Conversations().Create(ctx, &c); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Members().AddDirect(ctx, c.ID, ann.ID, model.LevelManager, now); err != nil {
			t.Fatal(err)
		}
		return c
	}
	put := func(content string) string {
		key, _, _, err := blobs.Put(ctx, strings.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	message := func(c model.Conversation, parent model.MessageID, age time.Duration, withFile bool) (model.Message, string) {
		m := model.Message{ID: model.NewID[model.MessageID](), ConversationID: c.ID, AuthorID: ann.ID, ParentID: parent, Text: "words", CreatedAt: now.Add(-age)}
		key := ""
		err := st.Tx(ctx, func(tx store.Tx) error {
			seq, err := tx.Conversations().NextSeq(ctx, c.ID)
			if err != nil {
				return err
			}
			m.Seq = seq
			if err := tx.Messages().Insert(ctx, &m, nil); err != nil {
				return err
			}
			if withFile {
				key = put("file of " + string(m.ID))
				f := model.File{ConversationID: c.ID, MessageID: m.ID, UploaderID: ann.ID, Name: "f.txt", Size: 1, MediaType: "text/plain", Kind: model.FileText, BlobKey: key}
				if err := tx.Files().Insert(ctx, &f); err != nil {
					return err
				}
			}
			return tx.Events().Append(ctx, model.Event{Type: model.EventMessageCreated, At: m.CreatedAt, ConversationID: c.ID, Seq: seq, MessageID: m.ID, Payload: map[string]any{}})
		})
		if err != nil {
			t.Fatal(err)
		}
		return m, key
	}
	exists := func(id model.MessageID) bool {
		_, err := st.Messages().Get(ctx, id)
		return err == nil
	}
	blobExists := func(key string) bool {
		r, err := blobs.Get(ctx, key)
		if err == nil {
			r.Close()
		}
		return err == nil
	}

	// Retention, with a per-channel override (docs/FEATURES.md section 6).
	week := int64(7 * 86400)
	if err := st.Settings().Set(ctx, store.SettingRetention, week, ""); err != nil {
		t.Fatal(err)
	}
	short, forever := int64(86400), int64(0)
	follows, quick, keeps := channel("follows", nil), channel("quick", &short), channel("keeps", &forever)
	oldMsg, oldKey := message(follows, "", 8*day, true)
	youngMsg, youngKey := message(follows, "", 6*day, true)
	quickOld, _ := message(quick, "", 2*day, false)
	quickYoung, _ := message(quick, "", time.Hour, false)
	kept, _ := message(keeps, "", 400*day, false)
	parent, parentKey := message(follows, "", 10*day, true)
	reply, _ := message(follows, parent.ID, time.Hour, false)

	// An upload nobody attached, old and new.
	upload := func(age time.Duration) (model.File, string) {
		key := put("upload")
		f := model.File{ConversationID: follows.ID, UploaderID: ann.ID, Name: "u.txt", Size: 6, MediaType: "text/plain", Kind: model.FileText, BlobKey: key, CreatedAt: now.Add(-age)}
		if err := st.Files().Create(ctx, &f); err != nil {
			t.Fatal(err)
		}
		return f, key
	}
	staleUpload, staleKey := upload(25 * time.Hour)
	freshUpload, freshKey := upload(time.Hour)
	// An avatar is on no message either, for good, and must be left.
	lib, _ := st.Conversations().EnsureLibrary(ctx)
	avatarKey := put("avatar")
	avatar := model.File{ConversationID: lib.ID, UploaderID: ann.ID, Purpose: model.PurposeAvatar, Name: "a.png", Size: 6, MediaType: "image/png", Kind: model.FileImage, BlobKey: avatarKey, CreatedAt: now.Add(-90 * day)}
	if err := st.Files().Insert(ctx, &avatar); err != nil {
		t.Fatal(err)
	}
	// Blobs with no row: one old, one that was written a moment ago.
	age := func(key string, d time.Duration) {
		path := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(key, "disk:")))
		when := time.Now().Add(-d)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	orphanOld, orphanNew := put("nobody owns this"), put("its row is about to be written")
	age(orphanOld, 25*time.Hour)
	for _, k := range []string{oldKey, youngKey, parentKey, staleKey, freshKey, avatarKey} {
		age(k, 30*day) // old enough for the sweep to look at; all but the removed ones have rows
	}
	// An old event of a message that stays.
	if err := st.Events().Append(ctx, model.Event{Type: "pin.added", At: now.Add(-31 * day), ConversationID: keeps.ID, Seq: 900, Payload: map[string]any{}}); err != nil {
		t.Fatal(err)
	}

	rep, err := j.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Two old events: the one appended above, and the event that created
	// the 400-day-old message, which itself is kept for ever.
	if rep.Messages != 3 || rep.Uploads != 1 || rep.Events != 2 || rep.Previews != 2 || rep.Blobs != 1 || previews.calls != 1 {
		t.Errorf("report: %+v", rep)
	}
	if exists(oldMsg.ID) || blobExists(oldKey) {
		t.Error("a message past the server's period, or its file, is still there")
	}
	if !exists(youngMsg.ID) || !blobExists(youngKey) {
		t.Error("a message inside the period went")
	}
	if exists(quickOld.ID) || !exists(quickYoung.ID) {
		t.Error("the channel's own shorter period was not what applied")
	}
	if !exists(kept.ID) {
		t.Error("a channel set to keep for ever lost a message")
	}
	// A parent with a younger reply is emptied to a bare record.
	bare, err := st.Messages().Get(ctx, parent.ID)
	if err != nil || bare.Text != "" || !bare.Deleted() || blobExists(parentKey) || !exists(reply.ID) {
		t.Errorf("the parent of a younger reply: %+v %v", bare, err)
	}
	if _, err := st.Files().Get(ctx, staleUpload.ID); !errors.Is(err, model.ErrNotFound) || blobExists(staleKey) {
		t.Error("an upload nobody attached is still there a day later")
	}
	if _, err := st.Files().Get(ctx, freshUpload.ID); err != nil || !blobExists(freshKey) {
		t.Error("a fresh upload went")
	}
	if _, err := st.Files().Get(ctx, avatar.ID); err != nil || !blobExists(avatarKey) {
		t.Error("an avatar was taken for an orphaned upload")
	}
	if blobExists(orphanOld) || !blobExists(orphanNew) {
		t.Error("the sweep removes old blobs with no row, and only those")
	}
	if j.LastRun().IsZero() {
		t.Error("the time of the last run was not kept")
	}
	// A second run finds nothing to do.
	rep, err = j.RunOnce(ctx)
	if err != nil || rep.Messages != 0 || rep.Uploads != 0 || rep.Events != 0 || rep.Blobs != 0 {
		t.Errorf("second run: %+v %v", rep, err)
	}
	// With no server period, only channels with their own lose anything.
	if err := st.Settings().Set(ctx, store.SettingRetention, nil, ""); err != nil {
		t.Fatal(err)
	}
	later, _ := message(follows, "", 3000*day, false)
	if rep, _ := j.RunOnce(ctx); rep.Messages != 0 || !exists(later.ID) {
		t.Errorf("no server period: %+v", rep)
	}
}
