package compact

import (
	"context"
	"datatree/internal/textstore"
	"reflect"
	"testing"
)

func TestPostorderBuilderMatchesValidatedTreeAndFreezesOnFinish(t *testing.T) {
	source, inputs := sampleInputs()
	inputs[0].End = len(source)
	inputs[4].Start = 15
	inputs[4].End = 16
	want, err := New(context.Background(), textstore.New(source), inputs, 1, 100, 8)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewBuilder(context.Background(), textstore.New(source), 100, 8)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{2, 3, 1, 4, 0} {
		if err := b.Add(inputs[i]); err != nil {
			t.Fatal(err)
		}
	}
	got, err := b.Finish(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range inputs {
		a, _ := got.Record(in.ID)
		z, _ := want.Record(in.ID)
		if !reflect.DeepEqual(a, z) {
			t.Fatalf("%+v != %+v", a, z)
		}
	}
	if got.Weight() != want.Weight() || got.Count() != want.Count() {
		t.Fatal("aggregate mismatch")
	}
	if b.Add(inputs[0]) == nil {
		t.Fatal("finished builder is mutable")
	}
	if _, err := b.Finish(1); err == nil {
		t.Fatal("builder published twice")
	}
}

func TestPostorderBuilderRejectsInvalidImports(t *testing.T) {
	for _, name := range []string{"duplicate", "missing-child", "wrong-parent", "repeated-child", "unordered", "outside", "scalar-children", "unreachable", "weight", "depth", "alias", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			source, inputs := sampleInputs()
			inputs[0].End = len(source)
			inputs[4].Start = 15
			inputs[4].End = 16
			maxNodes, maxDepth := 100, 8
			switch name {
			case "duplicate":
				inputs[3].ID = inputs[2].ID
			case "missing-child":
				inputs[1].Children[1] = 99
			case "wrong-parent":
				inputs[2].Parent = 1
			case "repeated-child":
				inputs[1].Children[1] = inputs[2].ID
			case "unordered":
				inputs[1].Children = []uint32{4, 3}
			case "outside":
				inputs[2].Start = 0
			case "scalar-children":
				inputs[1].Kind = "number"
			case "unreachable":
				inputs[0].Children = inputs[0].Children[:1]
			case "weight":
				maxNodes = 6
			case "depth":
				maxDepth = 1
			case "alias":
				inputs[2].Target = 99
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b, err := NewBuilder(ctx, textstore.New(source), maxNodes, maxDepth)
			if err != nil {
				t.Fatal(err)
			}
			if name == "cancelled" {
				cancel()
			}
			for _, i := range []int{2, 3, 1, 4, 0} {
				if err = b.Add(inputs[i]); err != nil {
					break
				}
			}
			tree, finishErr := b.Finish(1)
			if tree != nil || finishErr == nil {
				t.Fatalf("invalid import published: add=%v finish=%v", err, finishErr)
			}
		})
	}
}

func TestPostorderBuilderExactScalarLimitsAndCancelledFinish(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b, err := NewBuilder(ctx, textstore.New([]byte("0")), 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Add(Input{ID: 1, Kind: "number", Value: "0", Start: 0, KeyStart: -1, End: 1, Weight: 1}); err != nil {
		t.Fatal(err)
	}
	tree, err := b.Finish(1)
	if err != nil || tree.Count() != 1 || tree.Weight() != 1 {
		t.Fatal(tree, err)
	}
	cancelled, err := NewBuilder(ctx, textstore.New([]byte("0")), 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := cancelled.Add(Input{ID: 1, Kind: "number", Start: 0, KeyStart: -1, End: 1}); err != nil {
		t.Fatal(err)
	}
	cancel()
	if tree, err := cancelled.Finish(1); tree != nil || err == nil {
		t.Fatal("cancelled Finish published tree")
	}
}
