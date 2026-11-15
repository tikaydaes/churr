package server

import (
	"context"
	"fmt"
	"os"

	"github.com/tikaydaes/churr/internal/memguard"
)

// This file connects the memory guard (internal/memguard) to what it
// guards: the hub's live connections, which are what a server's memory
// goes on (docs/OPERATIONS.md section 16, "At the memory limit").

// newMemoryGuard makes the guard from the system and the operator's
// setting. It returns nil where there is nothing to guard against: not
// Linux, or switched off.
func (s *Server) newMemoryGuard() *memguard.Guard {
	src := s.opts.MemorySource
	if src == nil {
		limit, off, err := memguard.ParseLimit(os.Getenv(memguard.EnvLimit))
		if err != nil {
			s.Log.Warn(memguard.EnvLimit+" is neither a size such as 700M nor \"off\"; it is ignored", "value", os.Getenv(memguard.EnvLimit))
		}
		if off {
			s.Log.Info("memory guard: switched off by " + memguard.EnvLimit)
			return nil
		}
		src = memguard.System(limit)
	}
	// A test's made-up machine may come with a made-up runtime.
	rt, _ := src.(memguard.Runtime)
	return memguard.New(memguard.Options{
		Log: s.Log, Source: src, Runtime: rt,
		// An operator who set the Go runtime's limit keeps it.
		KeepLimit: os.Getenv("GOMEMLIMIT") != "",
		Trim:      func() { s.Live.Trim() },
		// A crowd leaving is collected after, so that what it held
		// is free for it to come back to (memguard).
		Connections: func() int { return s.Live.Connected() },
		Shed: func(need int64) int {
			// As many as would free what is needed, at what a
			// connection is taken to cost; but in one second never
			// more than a fiftieth of them (or eight), since what a
			// closed connection held is free only a moment later
			// and the next look may find it was enough.
			n := int(need/memguard.PerConnection) + 1
			return s.Live.Shed(min(n, max(s.Live.Connected()/50, 8)))
		},
	})
}

// runMemory starts the guard's once-a-second look and tells the admin
// Overview when memory is short.
func (s *Server) runMemory(ctx context.Context) {
	if s.Memory == nil {
		return
	}
	earlier := s.API.Overview.Warnings
	s.API.Overview.Warnings = func(ctx context.Context) []string {
		var out []string
		if earlier != nil {
			out = earlier(ctx)
		}
		if w := memoryWarning(s.Memory.Snapshot()); w != "" {
			out = append(out, w)
		}
		return out
	}
	go s.Memory.Run(ctx)
}

// memoryWarning is the sentence the Overview shows while memory is short.
func memoryWarning(m memguard.Snapshot) string {
	if !m.Active || !m.Refusing {
		return ""
	}
	if m.State == memguard.Normal {
		// Not under the line, but at it: only as many are let in each
		// second as there is room for, and the rest are turned away.
		return fmt.Sprintf("The server is close to its memory limit: about %d MiB of %d MiB would be left after tidying up, and new live connections are let in only as there is room. The rest are refused and try again by themselves. %d refused and %d closed since the server started. A machine with more memory, or fewer people connected at once, ends this.",
			m.AfterCollection>>20, m.Limit>>20, m.Refused, m.Shed)
	}
	doing := "New live connections are being refused until there is room"
	if m.State == memguard.Critical {
		doing = "New live connections are being refused and the connections furthest behind are being closed"
	}
	return fmt.Sprintf("The server is short of memory (%s): about %d MiB of %d MiB would be left after tidying up. %s; clients try again by themselves. %d refused and %d closed since the server started. A machine with more memory, or fewer people connected at once, ends this.",
		m.State, m.AfterCollection>>20, m.Limit>>20, doing, m.Refused, m.Shed)
}
