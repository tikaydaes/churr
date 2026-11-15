package push

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
)

// One message that wakes many phones sends them at the rate and not all at
// once (ruling R64): a share at once, the rest a share every tick.
func TestWakeUpsOfOneMessageAreSpread(t *testing.T) {
	var mu sync.Mutex
	var at []time.Time
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		at = append(at, time.Now())
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(relay.Close)
	// 100 a second is ten every tenth of a second.
	w := newWorld(t, relay.URL, Deps{WakeRate: 100})
	const people = 35
	for i := 0; i < people; i++ {
		acc := model.Account{Username: fmt.Sprintf("p%d", i), DisplayName: "P", Role: model.RoleMember}
		if _, err := w.store.Accounts().Create(ctx, &acc); err != nil {
			t.Fatal(err)
		}
		if _, err := w.store.Members().AddDirect(ctx, w.conv.ID, acc.ID, model.LevelMember, model.Now()); err != nil {
			t.Fatal(err)
		}
		ses := model.Session{AccountID: acc.ID, Kind: model.SessionApp, Label: "phone"}
		if err := w.store.Sessions().Create(ctx, &ses, secret.RandomBytes(32), secret.RandomBytes(32)); err != nil {
			t.Fatal(err)
		}
		if _, err := w.s.Register(ctx, acc, ses, DeviceInput{Name: ptr("Phone"), Platform: ptr("ios"), PushKind: ptr(model.PushAPNs),
			PushAddress: ptr(fmt.Sprintf("addr-%d", i))}); err != nil {
			t.Fatal(err)
		}
		w.notify.wake[acc.ID] = true
	}
	m := model.Message{ID: model.NewID[model.MessageID](), ConversationID: w.conv.ID, Seq: 5, AuthorID: w.ann.ID}
	began := time.Now()
	w.s.MessageCreated(m)
	if d := time.Since(began); d > 50*time.Millisecond {
		t.Errorf("the caller waited %v for the wake-ups", d)
	}
	w.s.wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(at) != people {
		t.Fatalf("%d wake-ups arrived, want %d", len(at), people)
	}
	first, last := at[0], at[0]
	early := 0
	for _, x := range at {
		if x.Before(first) {
			first = x
		}
		if x.After(last) {
			last = x
		}
	}
	for _, x := range at {
		if x.Sub(first) < wakeTick/2 {
			early++
		}
	}
	// 35 at ten a tick: three waits between the first and the last.
	if spread := last.Sub(first); spread < 3*wakeTick-wakeTick/2 {
		t.Errorf("the wake-ups arrived within %v; they should be spread over about %v", spread, 3*wakeTick)
	}
	if early > 12 {
		t.Errorf("%d wake-ups went in the first tick, want about 10", early)
	}

	// A message that wakes a few phones is not held back.
	mu.Unlock()
	for id := range w.notify.wake {
		w.notify.wake[id] = false
	}
	w.notify.wake[w.ann.ID] = true
	w.phone(model.PushAPNs, "ann-phone")
	mu.Lock()
	at = nil
	mu.Unlock()
	began = time.Now()
	m.ID, m.AuthorID = model.NewID[model.MessageID](), model.NewID[model.AccountID]()
	w.s.MessageCreated(m)
	w.s.wg.Wait()
	mu.Lock()
	if len(at) != 1 || time.Since(began) > wakeTick {
		t.Errorf("one wake-up: %d arrived in %v", len(at), time.Since(began))
	}
}
