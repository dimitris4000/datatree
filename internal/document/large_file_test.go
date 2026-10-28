package document

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestIndexedSourceLookup(t *testing.T) {
	d, err := Parse("json", []byte("{\r\n\"😀\": [1,{\"é\":\"😀\"}],\r\n\"z\":2}"))
	if err != nil {
		t.Fatal(err)
	}
	ranges := d.SourceRanges()
	for _, r := range ranges {
		got, ok := d.SourceRange(r.ID)
		if !ok || got != r {
			t.Fatalf("range %s: %v %v", r.ID, got, ok)
		}
	}
	n := d.Root.Children[0].Children[1].Children[0]
	r, _ := d.SourceRange(n.ID)
	got, ok := d.Locate(r.End-2, r.End-1)
	if !ok || got.ID != n.ID {
		t.Fatalf("locate: %v %v", got, ok)
	}
	parents := d.Ancestors(n.ID, 1)
	if len(parents) != 3 || parents[0].ID != d.Root.ID || parents[1].ID != d.Root.Children[0].ID || parents[1].Offset != 1 {
		t.Fatalf("ancestors: %+v", parents)
	}
	ranges[0].End = -9
	root, _ := d.SourceRange(d.Root.ID)
	if root.End < 0 {
		t.Fatal("caller changed cached index")
	}
	if _, ok := d.Locate(-1, 0); ok {
		t.Fatal("negative range accepted")
	}
	if _, ok := d.SourceRange("missing"); ok {
		t.Fatal("missing node found")
	}
}

func TestFindContextPagesAndCancellation(t *testing.T) {
	d, err := Parse("json", []byte(`["x","x","x","x","x"]`))
	if err != nil {
		t.Fatal(err)
	}
	options := FindOptions{Mode: "search", Text: "x", Values: true, Limit: 2}
	var ids []string
	for {
		result, err := d.FindContext(context.Background(), options)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range result.Matches {
			ids = append(ids, m.ID)
		}
		if result.NextCursor == "" {
			break
		}
		if !result.Truncated {
			t.Fatal("nonfinal page must be truncated")
		}
		options.Cursor = result.NextCursor
	}
	want := []string{"2", "3", "4", "5", "6"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids=%v", ids)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.FindContext(ctx, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	options.Cursor = "bad"
	if _, err := d.FindContext(context.Background(), options); err == nil {
		t.Fatal("invalid cursor accepted")
	}
}

func TestJSONReplacePreservesUntouchedSource(t *testing.T) {
	source := "{\r\n  \"x\" : [ 1, 2 ], \"unchanged\" : 1e1000\r\n}\r\n"
	d, err := Parse("json", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	next, err := d.Edit(Action{Op: "replace", Node: d.Root.Children[0].ID, Text: `{"é":"😀"}`, Format: "json"})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(source, "[ 1, 2 ]", `{"é":"😀"}`, 1)
	if string(next.Source) != want {
		t.Fatalf("source changed outside replacement: %s", next.Source)
	}
	if string(d.Source) != source {
		t.Fatal("original mutated")
	}
}

func TestJSONAllowsYAMLIncompatibleStrings(t *testing.T) {
	d, err := Parse("json", []byte(`{"\u0000":"\u0001","x":"é😀"}`))
	if err != nil {
		t.Fatal(err)
	}
	if d.Root.Children[0].Label != "\x00" || d.Root.Children[0].Value != "\x01" {
		t.Fatal("string semantics changed")
	}
	n := d.Root.Children[1]
	if got := string(d.Source[n.Start:n.End]); got != `"é😀"` {
		t.Fatalf("span %q", got)
	}
}

func TestQueryContextCancelsTraversal(t *testing.T) {
	d, err := Parse("json", []byte(`[{"x":1},{"x":2}]`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.queryContext(ctx, "$..x"); !errors.Is(err, context.Canceled) {
		t.Fatalf("query cancellation: %v", err)
	}
}

func TestParseContextCancelledAllFormats(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for format, source := range map[string]string{"json": `{"x":1}`, "jsonl": "1\n2\n", "yaml": "x: 1\n", "csv": "x\n1\n"} {
		if _, err := ParseWithLimitsContext(ctx, format, []byte(source), DefaultLimits()); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s: %v", format, err)
		}
	}
}

func TestRepresentationContextBoundedAndExact(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		d, err := Parse("json", []byte(`{"x":[1,"é😀",{"escaped":"<\\\""}],"empty":{}}`))
		if err != nil {
			t.Fatal(err)
		}
		var want string
		if format == "json" {
			want, err = d.JSON(d.Root.ID, true)
		} else {
			want, err = d.YAML(d.Root.ID)
		}
		if err != nil {
			t.Fatal(err)
		}
		got, total, err := d.RepresentationContext(context.Background(), d.Root.ID, format, 13)
		if err != nil {
			t.Fatal(err)
		}
		if total != len(want) || !strings.HasPrefix(want, got) || len(got) > 13 {
			t.Fatalf("%s got %q total %d want %q", format, got, total, want)
		}
		got, total, err = d.RepresentationContext(context.Background(), d.Root.ID, format, 0)
		if err != nil || got != want || total != len(want) {
			t.Fatalf("%s full mismatch %q %v", format, got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d, _ := Parse("json", []byte(`[1]`))
	if _, _, err := d.RepresentationContext(ctx, "1", "json", 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestEditContextCancelled(t *testing.T) {
	d, _ := Parse("json", []byte(`[1]`))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.EditContext(ctx, Action{Op: "replace", Node: "2", Text: "2"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("edit cancellation: %v", err)
	}
}

func TestSameSpanScalarReplacementSharesUnchangedNodes(t *testing.T) {
	d, err := Parse("json", []byte(`{"edit":{"x":123},"untouched":{"value":"old"}}`))
	if err != nil {
		t.Fatal(err)
	}
	oldRanges := d.SourceRanges()
	target := d.Root.Children[0].Children[0]
	next, err := d.Edit(Action{Op: "replace", Node: target.ID, Text: "456", Format: "json"})
	if err != nil {
		t.Fatal(err)
	}
	if next.Root.Children[1] != d.Root.Children[1] {
		t.Fatal("unchanged subtree was rebuilt")
	}
	if next.Nodes[target.ID].Value != "456" || d.Nodes[target.ID].Value != "123" {
		t.Fatal("value or immutable baseline lost")
	}
	if !reflect.DeepEqual(oldRanges, next.SourceRanges()) {
		t.Fatal("same-span source geometry changed")
	}
	// Repeated scalar edits must resolve the current copied ancestors, while a
	// structural edit must canonicalize parent/raw pointers before cloning.
	next, err = next.Edit(Action{Op: "replace", Node: target.ID, Text: "789", Format: "json"})
	if err != nil {
		t.Fatal(err)
	}
	next, err = next.Edit(Action{Op: "rename", Node: d.Root.Children[1].Children[0].ID, Key: "renamed"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(next.Source), `"renamed"`) || !strings.Contains(string(next.Source), "789") {
		t.Fatalf("subsequent edit lost values: %s", next.Source)
	}
}

func TestTryScalarPatchIsAtomicAndConservative(t *testing.T) {
	d, _ := Parse("json", []byte(`[123,"é"]`))
	n := d.Root.Children[0]
	if _, ok, err := d.TryScalarPatchContext(context.Background(), n.Start, n.End, "1"); ok || err != nil {
		t.Fatalf("different length patch: %v %v", ok, err)
	}
	if _, ok, err := d.TryScalarPatchContext(context.Background(), n.Start, n.End, "bad"); ok || err == nil {
		t.Fatalf("invalid patch: %v %v", ok, err)
	}
	if _, ok, err := d.TryScalarPatchContext(context.Background(), n.Start, n.End, " 2 "); ok || err != nil {
		t.Fatalf("whitespace moved token geometry: %v %v", ok, err)
	}
	next, ok, err := d.TryScalarPatchContext(context.Background(), n.Start, n.End, "456")
	if err != nil || !ok || string(next.Source) != `[456,"é"]` {
		t.Fatalf("patch %v %v %v", next, ok, err)
	}
}

func TestScalarPatchRejectsDifferentUTF16Geometry(t *testing.T) {
	d, _ := Parse("json", []byte(`["😀a",1]`))
	before := d.SourceRanges()
	n := d.Root.Children[0]
	if _, ok, err := d.TryScalarPatchContext(context.Background(), n.Start, n.End, `"é€"`); ok || err != nil {
		t.Fatalf("UTF-16 geometry changed in fast path: %v %v", ok, err)
	}
	next, err := d.Edit(Action{Op: "replace", Node: n.ID, Text: `"é€"`, Format: "json"})
	if err != nil {
		t.Fatal(err)
	}
	after := next.SourceRanges()
	if after[2].Start != before[2].Start-1 {
		t.Fatalf("stale source map: before=%v after=%v", before, after)
	}
}

func TestBoundedCSVRepresentationCanExceedInputByteLimit(t *testing.T) {
	source := []byte("a,b\nx,y\n")
	d, err := ParseWithLimits("csv", source, Limits{MaxBytes: len(source), MaxNodes: 30})
	if err != nil {
		t.Fatal(err)
	}
	text, total, err := d.RepresentationContext(context.Background(), d.Root.ID, "native", 4)
	if err != nil || len(text) > 4 || total <= len(source) {
		t.Fatalf("bounded conversion: %q %d %v", text, total, err)
	}
	if _, _, err := d.RepresentationContext(context.Background(), d.Root.ID, "native", 0); err == nil {
		t.Fatal("full conversion byte limit bypassed")
	}
}

func TestStableChangesContainsOnlyScalarAndAncestors(t *testing.T) {
	d, _ := Parse("json", []byte(`{"a":{"x":123},"b":2}`))
	target := d.Root.Children[0].Children[0]
	if d.StableChanges() != nil {
		t.Fatal("full parse claims stable IDs")
	}
	next, err := d.Edit(Action{Op: "replace", Node: target.ID, Text: "456", Format: "json"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{target.ID, d.Root.Children[0].ID, d.Root.ID}
	got := next.StableChanges()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changed IDs %v", got)
	}
	got[0] = "modified"
	if next.StableChanges()[0] != target.ID {
		t.Fatal("caller mutated cached change list")
	}
}
