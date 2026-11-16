package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheckHealth(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")
	_, port, _ := strings.Cut(addr, ":")

	if err := checkHealth(addr, time.Second); err != nil {
		t.Errorf("a healthy server: %v", err)
	}
	// A wildcard host, as -http is usually given in a container, means
	// this machine.
	if err := checkHealth(":"+port, time.Second); err != nil {
		t.Errorf("a wildcard host: %v", err)
	}
	if err := checkHealth("0.0.0.0:"+port, time.Second); err != nil {
		t.Errorf("0.0.0.0: %v", err)
	}
	status = http.StatusServiceUnavailable
	if err := checkHealth(addr, time.Second); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("a server answering 503: %v", err)
	}
	srv.Close()
	if err := checkHealth(addr, time.Second); err == nil {
		t.Error("nothing listening counted as healthy")
	}
	if err := checkHealth("nonsense", time.Second); err == nil {
		t.Error("an address without a port was accepted")
	}
}
