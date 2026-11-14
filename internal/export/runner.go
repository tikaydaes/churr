package export

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// The states of an export started from the admin console.
const (
	StateRunning = "running"
	StateReady   = "ready"
	StateFailed  = "failed"
)

// Status is an export started from the admin console (schema Export).
type Status struct {
	ID          string          `json:"id"`
	State       string          `json:"state"`
	StartedByID model.AccountID `json:"started_by_id"`
	StartedAt   model.Time      `json:"started_at"`
	FinishedAt  model.Time      `json:"finished_at,omitempty"`
	Size        int64           `json:"size,omitempty"`
	Error       string          `json:"error,omitempty"`
}

// Runner runs the export the owner starts from the admin console: one at a
// time, written to a file in the server's temporary folder, kept until the
// next one replaces it or the server restarts. What it knows of the latest
// export is in memory; after a restart there is none, and the owner starts
// another.
type Runner struct {
	opts Options
	dir  string

	mu     sync.Mutex
	latest *Status
	path   string
	wg     sync.WaitGroup
	// ctx ends when the server stops, which stops an export in progress.
	ctx  context.Context
	stop context.CancelFunc
}

// NewRunner makes a runner that keeps its archive in dir.
func NewRunner(opts Options, dir string) *Runner {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	r := &Runner{opts: opts, dir: dir}
	r.ctx, r.stop = context.WithCancel(context.Background())
	return r
}

// Close stops an export in progress and waits for it to let go of the
// database. The server calls it before closing the store.
func (r *Runner) Close() {
	r.stop()
	r.wg.Wait()
}

// Start begins an export in the background and returns its record. It is
// model.ErrConflict while one is running. The caller has already decided
// that by may: only the owner. The start is written to the audit log.
func (r *Runner) Start(ctx context.Context, by model.Account) (Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.latest != nil && r.latest.State == StateRunning {
		return Status{}, model.ErrConflict.With("An export is already running.")
	}
	st := Status{ID: model.NewID[string](), State: StateRunning, StartedByID: by.ID, StartedAt: model.Now()}
	if err := Audit(ctx, r.opts.Store, "account", string(by.ID), map[string]any{"export_id": st.ID, "files": !r.opts.NoFiles}); err != nil {
		return Status{}, err
	}
	// The one before is replaced: there is one "latest export".
	if r.path != "" {
		os.Remove(r.path)
		r.path = ""
	}
	r.latest = &st
	r.wg.Add(1)
	go r.run(st.ID)
	return st, nil
}

// Audit writes the line of the audit log that says an export was made,
// from the admin console (actor kind `account`) or the command line (`cli`).
func Audit(ctx context.Context, s store.Areas, actorKind, actorID string, detail map[string]any) error {
	return s.Audit().Append(ctx, model.AuditEntry{At: model.Now(), ActorKind: actorKind, ActorID: actorID, Action: "export.started", Detail: detail})
}

func (r *Runner) run(id string) {
	defer r.wg.Done()
	// Not the request's context: the export outlives the request that
	// started it, and ends early only if the server stops.
	ctx := r.ctx
	path := filepath.Join(r.dir, "export-"+id+".zip")
	size, err := r.write(ctx, path)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.latest == nil || r.latest.ID != id {
		os.Remove(path)
		return
	}
	r.latest.FinishedAt = model.Now()
	if err != nil {
		os.Remove(path)
		r.latest.State = StateFailed
		// The reason is for the log; the owner is told that it failed.
		r.latest.Error = "The export could not be written. The server's log says why."
		r.opts.Log.Error("the export failed", "err", err)
		return
	}
	r.latest.State, r.latest.Size, r.path = StateReady, size, path
	r.opts.Log.Info("export ready", "bytes", size)
}

func (r *Runner) write(ctx context.Context, path string) (int64, error) {
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	if err := Write(ctx, f, r.opts); err != nil {
		f.Close()
		return 0, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return 0, err
	}
	return st.Size(), f.Close()
}

// Latest returns the latest export, or false if none has been started since
// the server began.
func (r *Runner) Latest() (Status, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.latest == nil {
		return Status{}, false
	}
	return *r.latest, true
}

// Open opens the latest finished export for reading. It is
// model.ErrNotFound when there is none: never started, still running, or
// failed.
func (r *Runner) Open() (*os.File, Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.latest == nil || r.latest.State != StateReady || r.path == "" {
		return nil, Status{}, model.ErrNotFound.With("There is no finished export to download.")
	}
	f, err := os.Open(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, Status{}, model.ErrNotFound.With("There is no finished export to download.")
	}
	return f, *r.latest, err
}

// Wait returns when no export is running. Tests use it.
func (r *Runner) Wait() { r.wg.Wait() }
