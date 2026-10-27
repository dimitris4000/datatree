package document

import (
	"encoding/json"
	"errors"
	"fmt"
)

const MaxDiffChanges = 200

type DiffCounts struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Changed int `json:"changed"`
}
type DiffMark struct {
	Added    bool `json:"added,omitempty"`
	Changed  bool `json:"changed,omitempty"`
	Contains bool `json:"contains,omitempty"`
}
type DiffValue struct {
	Path     string `json:"path,omitempty"`
	Location string `json:"location"`
	Kind     string `json:"kind"`
	Tag      string `json:"tag,omitempty"`
	Preview  string `json:"preview"`
}
type DiffChange struct {
	Status                        string     `json:"status"`
	Before                        *DiffValue `json:"before,omitempty"`
	After                         *DiffValue `json:"after,omitempty"`
	Target                        string     `json:"target"`
	beforeNode, afterNode, parent *Node
}
type DiffResult struct {
	Changes   []DiffChange        `json:"changes"`
	Counts    DiffCounts          `json:"counts"`
	Marks     map[string]DiffMark `json:"marks"`
	Truncated bool                `json:"truncated"`
}

func diffValue(n *Node, d *Document) *DiffValue {
	if n == nil {
		return nil
	}
	preview := n.Value
	switch n.Kind {
	case "object", "array", "stream":
		preview = fmt.Sprintf("%s with %d entries", n.Kind, len(n.Children))
	case "string":
		b, _ := json.Marshal(shortPreview(preview))
		preview = string(b)
	case "null":
		preview = "null"
	case "alias":
		if target := d.Nodes[n.Target]; target != nil {
			location := target.Path
			if location == "" {
				location = target.Location
			}
			preview += " → " + location
		}
	}
	return &DiffValue{n.Path, n.Location, n.Kind, n.raw.Tag, shortPreview(preview)}
}

// Diff compares parsed structure. IDs and navigation targets always refer to the
// working document; a removed node targets its nearest surviving parent.
func Diff(base, working *Document) (DiffResult, error) {
	r := DiffResult{Changes: []DiffChange{}, Marks: map[string]DiffMark{}}
	if base == nil || working == nil {
		return r, errors.New("open a document and choose a comparison baseline")
	}
	w := &diffWork{left: diffWorkLimit}
	bID, aID := newDiffIdentity(base, w), newDiffIdentity(working, w)
	// Validate and cache identities up front, including numeric/key work bounds.
	if _, err := bID.hash(base.raw, false); err != nil {
		return DiffResult{}, err
	}
	if _, err := aID.hash(working.raw, false); err != nil {
		return DiffResult{}, err
	}
	markParents := func(n *Node) error {
		for ; n != nil; n = n.parent {
			if err := w.spend(1); err != nil {
				return err
			}
			m := r.Marks[n.ID]
			m.Contains = true
			r.Marks[n.ID] = m
		}
		return nil
	}
	var markAdded func(*Node) error
	markAdded = func(n *Node) error {
		if err := w.spend(1); err != nil {
			return err
		}
		m := r.Marks[n.ID]
		m.Added = true
		r.Marks[n.ID] = m
		for _, c := range n.Children {
			if err := markAdded(c); err != nil {
				return err
			}
		}
		return nil
	}
	emit := func(status string, b, a, parent *Node) error {
		target := a
		if target == nil {
			target = parent
		}
		switch status {
		case "added":
			r.Counts.Added++
			if err := markAdded(a); err != nil {
				return err
			}
			if err := markParents(a.parent); err != nil {
				return err
			}
		case "removed":
			r.Counts.Removed++
			if err := markParents(parent); err != nil {
				return err
			}
		case "changed":
			r.Counts.Changed++
			m := r.Marks[a.ID]
			m.Changed = true
			r.Marks[a.ID] = m
			if err := markParents(a.parent); err != nil {
				return err
			}
		}
		if len(r.Changes) == MaxDiffChanges {
			r.Truncated = true
			return nil
		}
		r.Changes = append(r.Changes, DiffChange{Status: status, Before: diffValue(b, base), After: diffValue(a, working), Target: target.ID, beforeNode: b, afterNode: a, parent: parent})
		return nil
	}
	var compare func(*Node, *Node, *Node) error
	compare = func(b, a, parent *Node) error {
		if err := w.spend(1); err != nil {
			return err
		}
		if b == nil {
			return emit("added", nil, a, parent)
		}
		if a == nil {
			return emit("removed", b, nil, parent)
		}
		bh, err := bID.hash(b.raw, false)
		if err != nil {
			return err
		}
		ah, err := aID.hash(a.raw, false)
		if err != nil {
			return err
		}
		if bh == ah {
			return nil
		}
		if b.Kind != a.Kind || ((a.Kind == "object" || a.Kind == "array" || a.Kind == "stream" || a.Kind == "tagged") && b.raw.Tag != a.raw.Tag) {
			return emit("changed", b, a, parent)
		}
		switch a.Kind {
		case "object":
			before := map[string]*Node{}
			used := map[string]bool{}
			for _, c := range b.Children {
				key, err := bID.hash(c.key, true)
				if err != nil {
					return err
				}
				if before[key] != nil {
					return errors.New("ambiguous equivalent mapping keys in diff baseline")
				}
				before[key] = c
			}
			afterKeys := map[string]bool{}
			for _, c := range a.Children {
				key, err := aID.hash(c.key, true)
				if err != nil {
					return err
				}
				if afterKeys[key] {
					return errors.New("ambiguous equivalent mapping keys in working document")
				}
				afterKeys[key] = true
				used[key] = true
				if err := compare(before[key], c, a); err != nil {
					return err
				}
			}
			for _, c := range b.Children {
				key, err := bID.hash(c.key, true)
				if err != nil {
					return err
				}
				if !used[key] {
					if err := compare(c, nil, a); err != nil {
						return err
					}
				}
			}
		case "array", "stream":
			for j := 0; j < max(len(a.Children), len(b.Children)); j++ {
				var bc, ac *Node
				if j < len(b.Children) {
					bc = b.Children[j]
				}
				if j < len(a.Children) {
					ac = a.Children[j]
				}
				if err := compare(bc, ac, a); err != nil {
					return err
				}
			}
		default:
			return emit("changed", b, a, parent)
		}
		return nil
	}
	if err := compare(base.Root, working.Root, nil); err != nil {
		return DiffResult{}, err
	}
	return r, nil
}
