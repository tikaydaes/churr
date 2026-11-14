// Package janitor removes what has outlived its use: messages past their
// retention period, uploads nobody attached, the old end of the catch-up
// log, expired sessions, codes, links and invites, stale link previews,
// and blobs in the file store that no row owns. docs/SCHEMA.md sections
// 14.1 and 14.4 are the list; docs/FEATURES.md section 6 is retention.
//
// It runs hourly, needs nobody's approval, sends no events and writes
// nothing to the audit log.
package janitor

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// How long things are kept (docs/SCHEMA.md section 14.4).
const (
	// Interval is how often the janitor runs.
	Interval = time.Hour
	// UploadKept is how long an upload may wait to be attached.
	UploadKept = 24 * time.Hour
	// EventsKept is how far back the catch-up log reaches.
	EventsKept = model.EventsKeptDays * 24 * time.Hour
	// DeclinedKept is how long a declined webhook request is kept.
	DeclinedKept = 30 * 24 * time.Hour
	// CodesKept is how long a sign-in code is kept after it expired.
	CodesKept = time.Hour
	// TotpUnconfirmedKept is how long an authenticator set-up may wait for
	// its first code before the stored secret is removed.
	TotpUnconfirmedKept = time.Hour
	// DeviceIdle is how long a device may go without a session before it
	// is retired (docs/SEALED.md section 4.1).
	DeviceIdle = 30 * 24 * time.Hour
	// PushFailing is how long every wake-up to a device may fail before
	// its push address is forgotten (docs/PUSH.md section 6).
	PushFailing = 7 * 24 * time.Hour
	// BlobGrace is how old a blob must be before the sweep will remove it
	// for having no row: a blob is written before its row, and a copy may
	// wait for its transaction.
	BlobGrace = 24 * time.Hour

	batch = 200
)

// Previews is what the janitor needs of the preview package: dropping cache
// rows, and their images, that are past their time.
type Previews interface {
	Expire(ctx context.Context) (int, error)
}

// Sealed is what the janitor needs of the sealed package: dropping the
// records of key epochs that retention has emptied (docs/SEALED.md section
// 13.2). sealed.Service implements it.
type Sealed interface {
	Janitor(ctx context.Context) (epochs, records int64, err error)
}

// Deps is what a Janitor is built from.
type Deps struct {
	Store store.Store
	Blobs blob.Blobs
	// Previews may be nil; the cache is then left alone.
	Previews Previews
	// Sealed may be nil; epoch records are then left alone.
	Sealed Sealed
	// Retired is told which devices a run retired for being idle, so
	// their accounts' other devices can be told. Nil tells nobody.
	Retired func(ctx context.Context, devices []model.Device)
	Log     *slog.Logger
}

// Janitor is the hourly clean-up.
type Janitor struct {
	store    store.Store
	blobs    blob.Blobs
	previews Previews
	sealed   Sealed
	retired  func(ctx context.Context, devices []model.Device)
	log      *slog.Logger
	now      func() model.Time

	mu      sync.Mutex // one run at a time
	lastRun atomic.Int64
}

// New makes the janitor. Nothing runs until Run or RunOnce is called.
func New(d Deps) *Janitor {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Janitor{store: d.Store, blobs: d.Blobs, previews: d.Previews, sealed: d.Sealed, retired: d.Retired, log: d.Log, now: model.Now}
}

// LastRun is when the last run finished, or zero if none has.
func (j *Janitor) LastRun() model.Time { return model.Time(j.lastRun.Load()) }

// Run runs the janitor shortly after start and then every hour, until ctx
// ends.
func (j *Janitor) Run(ctx context.Context) {
	// Not at once: a server that has just started has better things to do,
	// and one that is restarted in a loop should not clean in a loop.
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if rep, err := j.RunOnce(ctx); err != nil && ctx.Err() == nil {
			j.log.Error("the janitor could not finish", "err", err)
		} else if err == nil {
			j.log.Info("janitor", "messages_expired", rep.Messages, "uploads", rep.Uploads, "events", rep.Events,
				"previews", rep.Previews, "blobs", rep.Blobs, "rows", rep.total())
		}
		timer.Reset(Interval)
	}
}

// Report says what one run removed.
type Report struct {
	// Messages past their retention period: deleted, or emptied to a bare
	// record because younger replies still hang from them.
	Messages int
	// Uploads never attached to a message.
	Uploads int
	// Events too old for the catch-up log.
	Events int64
	// Previews dropped from the cache.
	Previews int
	// Blobs found in the file store with no row.
	Blobs int
	// Rows is everything else, by table.
	Rows map[string]int64
}

func (r Report) total() int64 {
	var n int64
	for _, v := range r.Rows {
		n += v
	}
	return n
}

// RunOnce does one whole run. Each part is tried even if an earlier one
// failed; the first error is returned.
func (j *Janitor) RunOnce(ctx context.Context) (Report, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	var rep Report
	var first error
	keep := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}
	var err error
	rep.Messages, err = j.retention(ctx)
	keep(err)
	rep.Uploads, err = j.uploads(ctx)
	keep(err)
	rep.Events, err = j.store.Events().DeleteBefore(ctx, j.now().Add(-EventsKept))
	keep(err)
	rep.Rows, err = j.store.Janitor().Tidy(ctx, j.now())
	keep(err)
	// Declined webhook requests, 30 days after the decision.
	if n, err := j.store.Bots().DeleteDeclinedWebhooks(ctx, j.now().Add(-DeclinedKept)); err != nil {
		keep(err)
	} else if n > 0 {
		if rep.Rows == nil {
			rep.Rows = map[string]int64{}
		}
		rep.Rows["webhooks"] = n
	}
	// Sign-in codes that ran out, and authenticator set-ups that were
	// begun and never confirmed.
	count := func(table string, n int64, err error) {
		keep(err)
		if n > 0 {
			if rep.Rows == nil {
				rep.Rows = map[string]int64{}
			}
			rep.Rows[table] += n
		}
	}
	n, err := j.store.Identities().DeleteCodesEnded(ctx, j.now().Add(-CodesKept))
	count("oauth_codes", n, err)
	n, err = j.store.Identities().DeleteTotpUnconfirmed(ctx, j.now().Add(-TotpUnconfirmedKept))
	count("totp", n, err)
	// Devices: one with no session for thirty days is retired, and one
	// whose wake-ups have failed for seven days running has its push
	// address forgotten.
	idle, err := j.store.Devices().RetireIdle(ctx, j.now().Add(-DeviceIdle), j.now())
	count("devices_retired", int64(len(idle)), err)
	if len(idle) > 0 && j.retired != nil {
		j.retired(ctx, idle)
	}
	n, err = j.store.Devices().ForgetFailedPush(ctx, j.now().Add(-PushFailing))
	count("push_addresses", n, err)
	// Sealed chats: the records of key epochs retention has emptied.
	if j.sealed != nil {
		epochs, records, err := j.sealed.Janitor(ctx)
		count("sealed_epochs", epochs, err)
		count("backup_records", records, nil)
	}
	if j.previews != nil {
		rep.Previews, err = j.previews.Expire(ctx)
		keep(err)
	}
	rep.Blobs, err = j.sweep(ctx)
	keep(err)
	if first == nil {
		j.lastRun.Store(int64(j.now()))
	}
	return rep, first
}

// drop removes blobs whose rows are gone. One that cannot be removed now is
// found again by the sweep.
func (j *Janitor) drop(ctx context.Context, keys []string) {
	for _, k := range keys {
		if err := j.blobs.Delete(ctx, k); err != nil {
			j.log.Warn("a blob could not be removed", "err", err)
		}
	}
}

// retention deletes each message whose created_at is older than its
// conversation's period, with its history, files, search entry and log
// rows. The stored override of a named channel is what applies to it;
// everything else follows the server's period.
func (j *Janitor) retention(ctx context.Context) (int, error) {
	server, _, err := store.SettingInt(ctx, j.store, store.SettingRetention)
	if err != nil {
		return 0, err
	}
	now := j.now()
	done := 0
	for {
		if err := ctx.Err(); err != nil {
			return done, err
		}
		ids, err := j.store.Janitor().ExpiredMessages(ctx, server, now, batch)
		if err != nil {
			return done, err
		}
		progressed := false
		for _, id := range ids {
			keys, _, err := j.store.Janitor().ExpireMessage(ctx, id, now)
			if errors.Is(err, model.ErrNotFound) {
				continue // went with its parent a moment ago
			}
			if err != nil {
				return done, err
			}
			j.drop(ctx, keys)
			done++
			progressed = true
		}
		// A full batch means there may be more. A batch in which nothing
		// could be done must not be asked for again.
		if len(ids) < batch || !progressed {
			return done, nil
		}
	}
}

// uploads deletes uploads still on no message a day after they were made.
func (j *Janitor) uploads(ctx context.Context) (int, error) {
	done := 0
	for {
		if err := ctx.Err(); err != nil {
			return done, err
		}
		list, err := j.store.Files().Unattached(ctx, j.now().Add(-UploadKept), batch)
		if err != nil || len(list) == 0 {
			return done, err
		}
		for _, f := range list {
			gone, err := j.store.Files().Delete(ctx, f.ID)
			if errors.Is(err, model.ErrNotFound) {
				continue
			}
			if err != nil {
				return done, err
			}
			j.drop(ctx, store.BlobKeys(gone))
			done++
		}
		if len(list) < batch {
			return done, nil
		}
	}
}

// sweep looks through the file store for blobs that no row owns and
// removes them. They are left by a transaction that deleted rows and then
// could not reach the store, or by a crash between writing a blob and its
// row.
func (j *Janitor) sweep(ctx context.Context) (int, error) {
	lister, ok := j.blobs.(blob.Lister)
	if !ok {
		return 0, nil
	}
	cutoff := j.now().Add(-BlobGrace).Time()
	var orphans []string
	err := lister.List(ctx, func(key string, modified time.Time) error {
		if modified.After(cutoff) {
			return nil
		}
		known, err := j.store.Files().KeyKnown(ctx, key)
		if err != nil {
			return err
		}
		if !known {
			orphans = append(orphans, key)
		}
		return nil
	})
	// What was found before a failure is still safe to remove.
	j.drop(ctx, orphans)
	return len(orphans), err
}

// SetSealed gives the janitor the sealed package and who to tell about
// retired devices, for a server that builds them after the janitor. Call it
// before Run. Either may be nil.
func (j *Janitor) SetSealed(s Sealed, retired func(ctx context.Context, devices []model.Device)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.sealed, j.retired = s, retired
}
