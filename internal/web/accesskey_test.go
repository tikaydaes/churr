package web

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The pages' access key grammar (static/shared/accesskey.js) is run against
// the shared vectors of testdata/accesskey, with Node standing in for the
// browser: both have Web Crypto and BigInt. Skipped where there is no Node;
// the browser run is e2e/accesskey.mjs.
func TestAccessKeyAgainstVectors(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; the pages' access key grammar is not checked against the vectors here")
	}
	script, _ := filepath.Abs("testdata/accesskey_vectors.mjs")
	mod, _ := filepath.Abs("static/shared/accesskey.js")
	vectors, _ := filepath.Abs("../../testdata/accesskey/vectors.json")
	out, err := exec.Command(node, script, mod, vectors).CombinedOutput()
	if err != nil {
		t.Fatalf("the pages' access key grammar does not match the vectors: %v\n%s", err, out)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(out)), "ok ") {
		t.Fatalf("unexpected output: %s", out)
	}
	t.Logf("node: %s", strings.TrimSpace(string(out)))
}
