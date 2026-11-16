package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/config"
)

// The migration runner's line goes to the server's own logger, in its
// format (-log-format json), and nothing goes to slog's default logger,
// both on a new database and on one opened again.
func TestMigrationLineUsesTheServersLogger(t *testing.T) {
	var stray bytes.Buffer
	was := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&stray, nil)))
	t.Cleanup(func() { slog.SetDefault(was) })

	dir := t.TempDir()
	cfg, err := config.ParseServe([]string{"-hostname", "chat.churr.test", "-data", filepath.Join(dir, "data"), "-http", "127.0.0.1:0",
		"-media", "off", "-log-format", "json"}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"migrations: new database", "migrations: database verified against this build"} {
		var logs bytes.Buffer
		srv, err := NewWith(cfg, Options{Log: newLoggerTo(cfg, &logs)})
		if err != nil {
			t.Fatal(err)
		}
		srv.Close()
		found := false
		for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
			var rec map[string]any
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				t.Fatalf("a line that is not JSON: %q", line)
			}
			if msg, _ := rec["msg"].(string); strings.HasPrefix(msg, want) {
				found = true
				if rec["dialect"] != "sqlite" {
					t.Errorf("the migration line lacks its dialect: %v", rec)
				}
			}
		}
		if !found {
			t.Errorf("no %q line in the server's log:\n%s", want, logs.String())
		}
	}
	if stray.Len() > 0 {
		t.Errorf("something went to the default logger:\n%s", stray.String())
	}
}
