package document

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSessionContextCancellationNeverCommits(t *testing.T) {
	s := NewSession()
	if err := s.Import("json", []byte(`{"a":1}`), 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, operation := range map[string]func() error{
		"import":  func() error { return s.ImportContext(ctx, "json", []byte(`{}`), 1) },
		"apply":   func() error { return s.ApplyContext(ctx, "json", []byte(`{}`), 1) },
		"patch":   func() error { return s.PatchContext(ctx, []Patch{{Start: 5, End: 6, Text: "2"}}, 1) },
		"mutate":  func() error { return s.MutateContext(ctx, Action{Op: "replace", Node: "2", Text: "2"}, 1) },
		"history": func() error { return s.HistoryContext(ctx, "undo", 1) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := operation(); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel error %v", err)
			}
			if s.Revision != 1 || s.CanUndo() || string(s.Doc.Source) != `{"a":1}` {
				t.Fatal("cancelled operation changed session")
			}
		})
	}
}

func TestSessionCloneDoesNotPublishMutations(t *testing.T) {
	s := NewSession()
	if err := s.Import("json", []byte(`{"a":1}`), 0); err != nil {
		t.Fatal(err)
	}
	copy := s.Clone()
	if err := copy.Apply("json", []byte(`{"a":2}`), 1); err != nil {
		t.Fatal(err)
	}
	if s.Revision != 1 || string(s.Doc.Source) != `{"a":1}` || s.CanUndo() {
		t.Fatal("clone mutated published session")
	}
	if copy.Revision != 2 || !copy.CanUndo() {
		t.Fatal("clone did not advance")
	}
	if err := copy.History("undo", 2); err != nil {
		t.Fatal(err)
	}
	if string(copy.Doc.Source) != string(s.Doc.Source) {
		t.Fatal("clone undo differs")
	}
}

func TestSmallChangesKeepHistoryBeyondFullSnapshotBudget(t *testing.T) {
	s := NewSession()
	source := `{"value":0,"padding":"` + strings.Repeat("x", 8*1024*1024) + `"}`
	if err := s.Import("json", []byte(source), 0); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		value := "0"
		if i%2 == 0 {
			value = "1"
		}
		if err := s.Patch([]Patch{{Start: 9, End: 10, Text: value}}, s.Revision); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 12; i++ {
		if err := s.History("undo", s.Revision); err != nil {
			t.Fatalf("undo %d: %v", i, err)
		}
	}
	if string(s.Doc.Source) != source {
		t.Fatal("undo did not preserve source")
	}
}

func TestSessionPatchIsAtomicAndPreservesCRLF(t *testing.T) {
	s := NewSession()
	if err := s.Import("json", []byte("{\r\n\"😀\":1,\r\n\"z\":2}"), 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Patch([]Patch{{Start: 7, End: 8, Text: "3"}, {Start: 14, End: 15, Text: "4"}}, 1); err != nil {
		t.Fatal(err)
	}
	if string(s.Doc.Source) != "{\r\n\"😀\":3,\r\n\"z\":4}" {
		t.Fatalf("patched: %q", s.Doc.Source)
	}
	before := s.Doc
	for _, patches := range [][]Patch{
		{{Start: 3, End: 4, Text: "x"}}, // splits the emoji surrogate pair
		{{Start: 9, End: 10, Text: "x"}, {Start: 0, End: 1, Text: "{"}},
		{{Start: 7, End: 8, Text: "invalid"}},
	} {
		if err := s.Patch(patches, s.Revision); err == nil {
			t.Fatal("accepted invalid patch", patches)
		}
		if s.Doc != before || s.Revision != 2 {
			t.Fatal("failed patch mutated session")
		}
	}
}

func TestScalarSourcePatchKeepsStableIDsAndSharesUnchangedNodes(t *testing.T) {
	s := NewSession()
	if err := s.Import("json", []byte(`{"a":1,"b":{"c":2}}`), 0); err != nil {
		t.Fatal(err)
	}
	before := s.Doc
	untouched := before.Root.Children[1]
	span, _ := before.SourceRange(before.Root.Children[0].ID)
	if err := s.Patch([]Patch{{Start: span.End - 1, End: span.End, Text: "3"}}, s.Revision); err != nil {
		t.Fatal(err)
	}
	if s.Doc.Nodes[untouched.ID] != untouched {
		t.Fatal("unchanged subtree was rebuilt")
	}
	if string(before.Source) != `{"a":1,"b":{"c":2}}` {
		t.Fatal("previous document was mutated")
	}
	if s.Doc.Root.Children[0].ID != before.Root.Children[0].ID {
		t.Fatal("scalar edit changed stable ID")
	}
	if err := s.Mutate(Action{Op: "delete", Node: untouched.Children[0].ID}, s.Revision); err != nil {
		t.Fatal("structural edit after shared patch", err)
	}
}
