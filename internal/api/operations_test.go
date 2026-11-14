package api

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
)

// contractOps reads the operations out of docs/openapi.yaml: each path, the
// methods under it and each one's operationId. The file is written by a
// generator in one fixed layout, so reading it line by line is enough and
// needs no YAML library.
func contractOps(t *testing.T) map[string]string {
	t.Helper()
	f, err := os.Open("../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]string{}
	var path, method string
	inPaths := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "paths:":
			inPaths = true
		case inPaths && len(line) > 0 && line[0] != ' ':
			inPaths = false
		case !inPaths:
		case strings.HasPrefix(line, "  /") && strings.HasSuffix(line, ":"):
			path = strings.TrimSuffix(strings.TrimSpace(line), ":")
		case strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "     ") && strings.HasSuffix(line, ":"):
			method = strings.ToUpper(strings.TrimSuffix(strings.TrimSpace(line), ":"))
		case strings.HasPrefix(line, "      operationId: "):
			out[strings.TrimPrefix(line, "      operationId: ")] = method + " " + path
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestTableMatchesContract fails when operations_gen.go is not what
// docs/openapi.yaml says, so the table cannot go stale unnoticed.
func TestTableMatchesContract(t *testing.T) {
	want := contractOps(t)
	if len(want) < 100 {
		t.Fatalf("read only %d operations from docs/openapi.yaml; the reader has gone wrong", len(want))
	}
	got := map[string]string{}
	for _, op := range Operations {
		if _, dup := got[op.ID]; dup {
			t.Errorf("operation %s is in the table twice", op.ID)
		}
		got[op.ID] = op.Method + " " + op.Path
	}
	for id, sig := range want {
		if got[id] != sig {
			t.Errorf("%s: the contract says %q, the table says %q", id, sig, got[id])
		}
	}
	for id := range got {
		if _, ok := want[id]; !ok {
			t.Errorf("%s is in the table and not in the contract", id)
		}
	}
	if t.Failed() {
		t.Log("regenerate with: python3 tools/openapi-gen/build.py (or tools/openapi-gen/gotable.py for the table alone)")
	}
}

// TestEveryOperationRoutes registers every operation of the contract on one
// mux. net/http panics on two patterns that conflict, so this is what
// proves the contract's paths can all be served.
func TestEveryOperationRoutes(t *testing.T) {
	mux := http.NewServeMux()
	for _, op := range Operations {
		mux.Handle(op.Method+" "+op.Path, http.NotFoundHandler())
	}
}

// TestUnimplemented lists the operations of the contract that have no
// handler yet. It never fails: it prints the count and keeps
// UNIMPLEMENTED.txt current, so progress is visible in review.
func TestUnimplemented(t *testing.T) {
	a := &API{handlers: map[string]handler{}}
	for _, fn := range registrations {
		fn(a)
	}
	missing := a.Unimplemented()
	byArea := map[string][]string{}
	var areas []string
	for _, op := range missing {
		if byArea[op.Area] == nil {
			areas = append(areas, op.Area)
		}
		byArea[op.Area] = append(byArea[op.Area], fmt.Sprintf("  %-28s %-6s %s", op.ID, op.Method, op.Path))
	}
	sort.Strings(areas)
	var b strings.Builder
	fmt.Fprintf(&b, "Operations of docs/openapi.yaml with no handler yet: %d of %d.\n", len(missing), len(Operations))
	b.WriteString("Written by TestUnimplemented in internal/api; do not edit by hand.\n")
	for _, area := range areas {
		fmt.Fprintf(&b, "\n%s (%d)\n%s\n", area, len(byArea[area]), strings.Join(byArea[area], "\n"))
	}
	if err := os.WriteFile("UNIMPLEMENTED.txt", []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d of %d operations implemented; %d remain (see internal/api/UNIMPLEMENTED.txt)",
		len(Operations)-len(missing), len(Operations), len(missing))
}
