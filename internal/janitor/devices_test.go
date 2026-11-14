package janitor

import (
	"context"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store/storetest"
)

type fakeSealed struct{ calls int }

func (f *fakeSealed) Janitor(context.Context) (int64, int64, error) { f.calls++; return 2, 3, nil }

// The janitor retires a device that has had no session for thirty days and
// says which, forgets a push address that has failed for seven days running,
// and asks the sealed package to drop the records of emptied key epochs.
// With nothing set for the last, it does the rest and nothing more.
func TestDevicesAndSealed(t *testing.T) {
	ctx := context.Background()
	st := storetest.OpenSQLite(t)
	blobs, err := blob.Disk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := model.Now()
	acc := model.Account{Username: "ann", DisplayName: "Ann", Role: model.RoleMember}
	if _, err := st.Accounts().Create(ctx, &acc); err != nil {
		t.Fatal(err)
	}
	device := func(name string, seen time.Duration, push bool) model.Device {
		d := model.Device{AccountID: acc.ID, Name: name, Platform: "android", SignKey: []byte("sign-" + name), BoxKey: []byte("box-" + name),
			CreatedAt: now.Add(-100 * 24 * time.Hour), LastSeenAt: now.Add(-seen)}
		if push {
			d.PushKind, d.PushAddress = "fcm", "address-of-"+name
			// A device in use: it has a session that has not ended.
			ses := model.Session{AccountID: acc.ID, Kind: model.SessionApp, Label: name, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
			if err := st.Sessions().Create(ctx, &ses, []byte("token-"+name), []byte("refresh-"+name)); err != nil {
				t.Fatal(err)
			}
			d.SessionID = ses.ID
		}
		if err := st.Devices().Create(ctx, &d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	// One has had no session for forty days; the other two are in use.
	idle := device("idle", 40*24*time.Hour, false)
	recent := device("recent", 24*time.Hour, true)
	failing := device("failing", time.Hour, true)
	if err := st.Devices().PushFailed(ctx, failing.ID, now.Add(-8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := st.Devices().PushFailed(ctx, recent.ID, now.Add(-2*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	sealed := &fakeSealed{}
	var told []model.Device
	j := New(Deps{Store: st, Blobs: blobs, Sealed: sealed, Retired: func(_ context.Context, ds []model.Device) { told = append(told, ds...) }})
	rep, err := j.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	get := func(d model.Device) model.Device {
		got, err := st.Devices().Get(ctx, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if get(idle).RetiredAt.IsZero() || !get(recent).RetiredAt.IsZero() || !get(failing).RetiredAt.IsZero() {
		t.Fatalf("which devices were retired: idle %v, recent %v, failing %v", get(idle).RetiredAt, get(recent).RetiredAt, get(failing).RetiredAt)
	}
	if len(told) != 1 || told[0].ID != idle.ID {
		t.Fatalf("the retired devices handed on: %+v", told)
	}
	if get(failing).PushAddress != "" || get(recent).PushAddress == "" {
		t.Fatalf("push addresses: failing %q, recent %q", get(failing).PushAddress, get(recent).PushAddress)
	}
	if sealed.calls != 1 || rep.Rows["sealed_epochs"] != 2 || rep.Rows["backup_records"] != 3 || rep.Rows["devices_retired"] != 1 || rep.Rows["push_addresses"] != 1 {
		t.Fatalf("the report: %v, sealed asked %d times", rep.Rows, sealed.calls)
	}

	// With no sealed package and nobody to tell, a run still works.
	device("idle-too", 50*24*time.Hour, false)
	if rep, err := New(Deps{Store: st, Blobs: blobs}).RunOnce(ctx); err != nil || rep.Rows["devices_retired"] != 1 || rep.Rows["sealed_epochs"] != 0 {
		t.Fatalf("a run with nothing set: %v %v", rep.Rows, err)
	}
}
