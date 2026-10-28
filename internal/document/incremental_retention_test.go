package document

import (
	"fmt"
	"testing"
)

func TestFastScalarParentsDoNotRetainRevisionGraphs(t *testing.T) {
	base, err := Parse("json", []byte(`{"group":{"x":100,"y":200},"keep":{"z":300}}`))
	if err != nil {
		t.Fatal(err)
	}
	x, y := base.Root.Children[0].Children[0].ID, base.Root.Children[0].Children[1].ID
	d := base
	for i := 0; i < 40; i++ {
		id := x
		if i%2 != 0 {
			id = y
		}
		next, err := d.Edit(Action{Op: "replace", Node: id, Text: fmt.Sprint(400 + i), Format: "json"})
		if err != nil {
			t.Fatal(err)
		}
		for _, changed := range next.StableChanges() {
			for parent := next.Nodes[changed].parent; parent != nil; parent = parent.parent {
				if parent.raw != nil || parent.key != nil || len(parent.Children) != 0 {
					t.Fatalf("revision %d parent %s retains a revision graph", i, parent.ID)
				}
			}
		}
		d = next
	}
	if base.Nodes[x].Value != "100" || base.Nodes[y].Value != "200" {
		t.Fatal("baseline mutated")
	}
	schema, err := CompileSchema([]byte(`{"properties":{"group":{"properties":{"x":{"const":100},"y":{"const":200}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := ValidateSchema(d, schema)
	if err != nil || result.Valid || !result.Marks[d.Root.ID].Contains || !result.Marks[d.Root.Children[0].ID].Contains {
		t.Fatalf("schema lost ancestor marks: %+v %v", result, err)
	}
	diff, err := Diff(base, d)
	if err != nil || len(diff.Changes) != 2 || !diff.Marks[d.Root.ID].Contains {
		t.Fatalf("diff lost ancestor marks: %+v %v", diff, err)
	}
	reverted, err := d.RevertDiff(base, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reverted.Edit(Action{Op: "delete", Node: y}); err != nil {
		t.Fatalf("structural edit after revert: %v", err)
	}
	changed, err := d.Edit(Action{Op: "delete", Node: y})
	if err != nil {
		t.Fatalf("structural edit after shared revisions: %v", err)
	}
	if len(changed.Root.Children[0].Children) != 1 {
		t.Fatal("structural delete failed")
	}
}
