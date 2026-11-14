package web

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The unlock page's cryptography (static/unlock/sealed.js) is run against
// the test vectors of testdata/sealed, with Node standing in for the
// browser: both have Web Crypto. The test is skipped where there is no
// Node; the browser run of the same vectors is the web builder's
// e2e/sealed-vectors.mjs.
func TestUnlockPageAgainstVectors(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; the unlock page's cryptography is not checked against the vectors here")
	}
	script, _ := filepath.Abs("testdata/unlock_vectors.mjs")
	sealed, _ := filepath.Abs("static/unlock/sealed.js")
	vectors, _ := filepath.Abs("../../testdata/sealed")
	out, err := exec.Command(node, script, sealed, vectors).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Unrecognized algorithm name") || strings.Contains(string(out), "NotSupportedError") {
			t.Skipf("this Node has no X25519 or Ed25519 in Web Crypto:\n%s", out)
		}
		t.Fatalf("the unlock page's cryptography does not match the vectors: %v\n%s", err, out)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(out)), "ok ") {
		t.Fatalf("unexpected output: %s", out)
	}
	t.Logf("node: %s", strings.TrimSpace(string(out)))
}
