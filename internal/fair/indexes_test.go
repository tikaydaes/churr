package fair

import (
	"encoding/json"
	"os"
	"testing"
)

// The canonical object indexes, held to testdata/fair/indexes.json, which
// the web chat and the app run too.
func TestCanonicalIndexes(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/fair/indexes.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Cases []struct {
			Name    string      `json:"name"`
			Edited  bool        `json:"edited"`
			View    *indexCase  `json:"view"`
			Objects []indexCase `json:"objects"`
			OK      bool        `json:"ok"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Cases) < 20 {
		t.Fatalf("%d cases", len(f.Cases))
	}
	for _, c := range f.Cases {
		var view *ObjectIndex
		if c.View != nil {
			v := c.View.index()
			view = &v
		}
		var objs []ObjectIndex
		for _, o := range c.Objects {
			objs = append(objs, o.index())
		}
		if err := CheckIndexes(view, objs, c.Edited); (err == nil) != c.OK {
			t.Errorf("%s: %v, want ok %v", c.Name, err, c.OK)
		}
	}
}

type indexCase struct {
	At      int  `json:"at"`
	Drew    bool `json:"drew"`
	I       int  `json:"i"`
	Removed bool `json:"removed"`
}

func (c indexCase) index() ObjectIndex {
	return ObjectIndex{At: c.At, Drew: c.Drew, I: c.I, Removed: c.Removed}
}

// An object that took a stream and made no value gives its index back, so
// the next object takes it and no gap is left.
func TestAnIndexNotKeptIsGivenBack(t *testing.T) {
	o := NewObjects(make([]byte, 32))
	a, _ := o.Stream()
	b, _ := o.Stream()
	o.Return(a) // not the last given: kept
	if o.Taken() != 2 {
		t.Fatalf("%d taken", o.Taken())
	}
	o.Return(b)
	c, _ := o.Stream()
	if c.Object() != 1 || o.Taken() != 2 {
		t.Fatalf("the next object took %d", c.Object())
	}
}
