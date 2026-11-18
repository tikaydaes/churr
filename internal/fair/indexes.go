package fair

import (
	"fmt"
	"sort"

	"github.com/tikaydaes/churr/internal/model"
)

// The canonical object indexes (docs/FAIR.md section 3). A message's chance
// values are numbered by one rule, so that a server cannot try indexes until
// an outcome suits it: the command's own value (the message's view), when it
// took chance, is object 0; the object entities that took chance follow in
// the order of their position `at` in the text, from 1 if the view took
// chance and 0 if not, with no gap and no repeat. An edit never adds a chance
// object: a roll it changes is marked removed and keeps its entity and its
// index; one whose characters it deletes is gone, and leaves its index
// unused. So on an edited message the indexes still rise with the position
// and none repeats, and gaps are left only by deletions; on a message never
// edited there is no gap and nothing is removed.

// ObjectIndex is one value of a message as the rule sees it: whether it took
// chance (Drew) and with which index, and whether it is marked removed.
type ObjectIndex struct {
	At      int
	Drew    bool
	I       int
	Removed bool
}

// ErrIndexes is a message whose object indexes are not the canonical ones.
var ErrIndexes = fmt.Errorf("fair: the object indexes are not the canonical ones")

// IndexesOf lists a message's view and object entities for CheckIndexes.
func IndexesOf(view *model.ValueEnvelope, ents []model.Entity) (*ObjectIndex, []ObjectIndex) {
	of := func(at int, v *model.ValueEnvelope) ObjectIndex {
		o := ObjectIndex{At: at, Removed: v.Removed}
		if v.Draw != nil {
			o.Drew, o.I = true, v.Draw.I
		}
		return o
	}
	var vi *ObjectIndex
	if view != nil {
		v := of(-1, view)
		vi = &v
	}
	var objs []ObjectIndex
	for _, e := range ents {
		if e.Type == model.EntityObject && e.Object != nil {
			objs = append(objs, of(e.At, e.Object))
		}
	}
	return vi, objs
}

// CheckIndexes holds a message's chance values to the canonical indexes:
// view is the message's view (nil for none), objects its object entities,
// and edited whether the message has been edited.
func CheckIndexes(view *ObjectIndex, objects []ObjectIndex, edited bool) error {
	bad := func(why string) error { return fmt.Errorf("%w: %s", ErrIndexes, why) }
	next := 0
	if view != nil && view.Removed {
		return bad("the message's own value is marked removed")
	}
	if view != nil && view.Drew {
		if view.I != 0 {
			return bad("the command's own value is not object 0")
		}
		next = 1
	}
	objs := append([]ObjectIndex(nil), objects...)
	sort.SliceStable(objs, func(a, b int) bool { return objs[a].At < objs[b].At })
	for _, o := range objs {
		if o.Removed && !edited {
			return bad("a message never edited has a value marked removed")
		}
		if !o.Drew {
			continue
		}
		switch {
		case o.I < next:
			return bad(fmt.Sprintf("object index %d is out of order or repeated", o.I))
		case o.I > next && !edited:
			return bad(fmt.Sprintf("object index %d leaves a gap", o.I))
		}
		next = o.I + 1
	}
	return nil
}
