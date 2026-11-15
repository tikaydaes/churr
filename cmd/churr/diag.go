//go:build diag

package main

import (
	"log"
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof on the default mux
	"os"
	"runtime"
)

// A binary built with `-tags diag` serves Go's profiles, for finding where
// a running server waits. It listens on CHURR_DIAG_ADDR, or 127.0.0.1:6060:
// never on an address the world reaches, for the profiles show everything
// the process holds. The block and mutex profiles are on, sampled (about
// one blocking event in each 100 microseconds of blocking, one contended
// lock in a hundred), which costs a busy server a few per cent.
//
//	curl -o block.pb.gz  'http://127.0.0.1:6060/debug/pprof/block?seconds=30'
//	curl -o mutex.pb.gz  'http://127.0.0.1:6060/debug/pprof/mutex?seconds=30'
//	curl -o cpu.pb.gz    'http://127.0.0.1:6060/debug/pprof/profile?seconds=30'
//	curl -o trace.out    'http://127.0.0.1:6060/debug/pprof/trace?seconds=5'
//	curl -o goroutines.txt 'http://127.0.0.1:6060/debug/pprof/goroutine?debug=2'
func init() {
	addr := os.Getenv("CHURR_DIAG_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6060"
	}
	runtime.SetBlockProfileRate(100_000) // nanoseconds
	runtime.SetMutexProfileFraction(100)
	go func() {
		log.Printf("diag build: profiles at http://%s/debug/pprof/", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Printf("diag build: the profile listener stopped: %v", err)
		}
	}()
}
