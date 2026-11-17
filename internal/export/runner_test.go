package export

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// A finished archive is kept for a day and then deleted (review finding
// C1: it holds every private channel and chat); a download address works
// once, for the Owner it was made for, and not for a later export.
func TestArchiveLapsesAndTicketsWorkOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "export-x.zip")
	if err := os.WriteFile(path, []byte("zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &Runner{dir: dir, path: path, latest: &Status{ID: "x", State: StateReady, FinishedAt: model.Now()}}
	ann, bob := model.AccountID("ann"), model.AccountID("bob")

	v, _, _, err := r.Ticket(ann)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Redeem(v, bob); err == nil {
		t.Fatal("someone else spent the address")
	}
	v, _, _, _ = r.Ticket(ann)
	f, _, err := r.Redeem(v, ann)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, _, err := r.Redeem(v, ann); err == nil {
		t.Fatal("the address worked twice")
	}

	v, _, _, _ = r.Ticket(ann)
	r.latest.FinishedAt = model.FromTime(time.Now().Add(-KeepFor - time.Minute))
	if st, _ := r.Latest(); st.State != StateExpired {
		t.Fatalf("after a day: %s", st.State)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the archive is still there: %v", err)
	}
	if _, _, err := r.Redeem(v, ann); err == nil {
		t.Fatal("an address outlived the archive")
	}
	if _, _, _, err := r.Ticket(ann); err == nil {
		t.Fatal("an address for a deleted archive")
	}
}
