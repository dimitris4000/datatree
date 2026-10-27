package document

import (
	"strings"
	"testing"
)

func TestConfiguredParserLimitsAreIndependent(t *testing.T) {
	for _, fixture := range []struct {
		format, source string
		nodes          int
	}{{"json", `{"a":1,"b":2}`, 5}, {"yaml", "a: 1\nb: 2\n", 5}, {"jsonl", "1\n2\n", 3}, {"csv", "a,b\nx,y\n", 6}} {
		t.Run(fixture.format, func(t *testing.T) {
			strict := Limits{MaxBytes: 1024, MaxNodes: fixture.nodes - 1}
			if _, err := ParseWithLimits(fixture.format, []byte(fixture.source), strict); err == nil || !strings.Contains(err.Error(), "node") {
				t.Fatalf("node limit ignored: %v", err)
			}
			if _, err := ParseWithLimits(fixture.format, []byte(fixture.source), Limits{MaxBytes: 1024, MaxNodes: fixture.nodes}); err != nil {
				t.Fatal(err)
			}
			if _, err := ParseWithLimits(fixture.format, []byte(fixture.source), strict); err == nil {
				t.Fatal("another parse changed configured limits")
			}
		})
	}
}

func TestConfiguredLimitsPersistThroughEditsConversionAndHistory(t *testing.T) {
	s := NewSession(Limits{MaxBytes: 80, MaxNodes: 6})
	if err := s.Import("json", []byte(`{"a":1}`), 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Mutate(Action{Op: "replace", Node: s.Doc.Root.Children[0].ID, Format: "json", Text: `[1,2,3,4]`}, 1); err == nil {
		t.Fatal("edit bypassed configured node limit")
	}
	if s.Revision != 1 || s.CanUndo() {
		t.Fatal("failed edit changed history")
	}
	if err := s.Convert("yaml", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.History("undo", 2); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply("json", []byte(`"`+strings.Repeat("x", 79)+`"`), 3); err == nil {
		t.Fatal("undo lost byte limit")
	}
	if err := s.Apply("json", []byte(`[1,2,3,4,5,6]`), 3); err == nil {
		t.Fatal("undo lost node limit")
	}
}

func TestLargerDefaultDocumentLimits(t *testing.T) {
	// This previously failed with the user's exact parsed-node-limit error.
	data := []byte("[" + strings.Repeat("0,", 100000) + "0]")
	if _, err := Parse("json", data); err != nil {
		t.Fatal(err)
	}
	large := []byte(`"` + strings.Repeat("x", 20*1024*1024-2) + `"`)
	if _, err := Parse("json", large); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseWithLimits("json", large, Limits{MaxBytes: 10 * 1024 * 1024, MaxNodes: 1000000}); err == nil {
		t.Fatal("configured byte limit ignored")
	}
}
