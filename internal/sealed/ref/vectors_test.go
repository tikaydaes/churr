package ref

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// update rewrites testdata/sealed from the reference:
//
//	go test ./internal/sealed/ref -run TestVectors -update
//
// A change to docs/SEALED.md that changes any byte comes with new vectors
// in the same change.
var update = flag.Bool("update", false, "rewrite testdata/sealed from the reference")

// vectorsDir is testdata/sealed at the root of the repository.
const vectorsDir = "../../../testdata/sealed"

// TestVectors writes the vectors again and fails if any byte differs from
// the committed files, if a file is missing, or if a file is there that
// the reference does not write. The Dart, JavaScript and Swift
// implementations are written against these files alone, so they must be
// exactly what this reference produces.
func TestVectors(t *testing.T) {
	files, err := Vectors()
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := WriteVectors(vectorsDir); err != nil {
			t.Fatal(err)
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		want, err := EncodeVector(files[name])
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, err := os.ReadFile(filepath.Join(vectorsDir, name))
		if err != nil {
			t.Errorf("%s is not in testdata/sealed; run with -update", name)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from what the reference writes; if SEALED.md changed, run with -update and commit the result", name)
		}
	}
	entries, err := os.ReadDir(vectorsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" && files[e.Name()] == nil {
			t.Errorf("%s is in testdata/sealed and the reference does not write it", e.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(vectorsDir, "README.md")); err != nil {
		t.Error("testdata/sealed/README.md, which defines the file format, is missing")
	}
}

// The vectors are the same every time: nothing in them comes from the
// clock or the random generator.
func TestVectorsAreRepeatable(t *testing.T) {
	a, err := Vectors()
	if err != nil {
		t.Fatal(err)
	}
	b, err := Vectors()
	if err != nil {
		t.Fatal(err)
	}
	for name := range a {
		x, _ := EncodeVector(a[name])
		y, _ := EncodeVector(b[name])
		if !bytes.Equal(x, y) {
			t.Errorf("%s is not the same on a second run", name)
		}
	}
}
