package document

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestMegabyteYAMLUsesLocalScalarEdits(t *testing.T) {
	source := []byte("value: 3\npadding: \"" + strings.Repeat("x", 1024*1024) + "\"\n")
	d, err := Parse("yaml", source)
	if err != nil {
		t.Fatal(err)
	}
	if d.compact == nil {
		t.Fatal("megabyte YAML retains the full-parse edit path")
	}
	result, err := d.Find(FindOptions{Mode: "query", Text: "$.value"})
	if err != nil || len(result.Matches) != 1 {
		t.Fatal(result, err)
	}
	n, _ := d.LookupNode(result.Matches[0].ID)
	next, handled, err := d.TryScalarPatchContext(context.Background(), n.Start, n.End, "4")
	if err != nil || !handled {
		t.Fatal("local scalar edit fell back", handled, err)
	}
	got, _ := next.LookupNode(n.ID)
	if got.Value != "4" || next.raw != nil {
		t.Fatal("local compact edit was not retained")
	}
}

func TestDirectCompactImportMatchesLegacyCompaction(t *testing.T) {
	for _, tc := range []struct{ format, source string }{
		{"json", "  {\"é😀\":[null,{\"quoted\\\"\":1e+30},true]}\r\n"},
		{"jsonl", "\r\n{\"é\":1}\r\n\n[true,\"😀\"]\r\n"},
		{"yaml", "# heading\nfirst: &a\n  text: |\n    hello\n    world\ncopy: *a\nflow: {a: [true, null]}\n"},
		{"yaml", "? [a, b]\n: !custom value\n---\n- é😀\n- next\n"},
		{"yaml", "a:\n  b: one\n  c:\n  - two\n  - three\n"},
	} {
		t.Run(tc.format+tc.source[:min(8, len(tc.source))], func(t *testing.T) {
			legacy, err := parseLegacyWithLimitsContext(context.Background(), tc.format, []byte(tc.source), DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			want, err := legacy.compactify(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			got, err := ParseCompactWithLimitsContext(context.Background(), tc.format, []byte(tc.source), DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			if got.compactYAMLMetadata != want.compactYAMLMetadata || got.compactComplexKeys != want.compactComplexKeys || got.NodeCount() != want.NodeCount() {
				t.Fatal("compact import flags/count differ")
			}
			for id := range legacy.Nodes {
				a, aok := got.compact.Record(compactID(id))
				b, bok := want.compact.Record(compactID(id))
				if aok != bok || !reflect.DeepEqual(a, b) {
					t.Fatalf("record %s: got %+v want %+v", id, a, b)
				}
			}
		})
	}
}

func TestCompactAdapterDiscardsLegacyGraphAndPreservesReads(t *testing.T) {
	for _, tc := range []struct{ format, source string }{
		{"json", `{"a":{"name":"old","n":123456789012345678901234567890},"b":[true,null]}`},
		{"jsonl", "{\"name\":\"old\"}\n{\"name\":\"other\"}\n"},
		{"yaml", "a:\n  name: old\n  n: 123456789012345678901234567890\nb: [true, null]\n"},
		{"csv", "name,description\r\nold,\"line1\r\nline2\"\r\nother,last\r\n"},
	} {
		t.Run(tc.format, func(t *testing.T) {
			legacy, err := Parse(tc.format, []byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			d, err := ParseCompactWithLimitsContext(context.Background(), tc.format, []byte(tc.source), DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			if d.compact == nil || d.raw != nil || len(d.Nodes) != 0 || len(d.Root.Children) != 0 {
				t.Fatal("compact document retains legacy graphs")
			}
			if d.NodeCount() != len(legacy.Nodes) {
				t.Fatal(d.NodeCount(), len(legacy.Nodes))
			}
			for id, expected := range legacy.Nodes {
				got, ok := d.LookupNode(id)
				if !ok || got.Value != expected.Value || got.Kind != expected.Kind || got.Path != expected.Path {
					t.Fatalf("lookup %s: %+v expected %+v", id, got, expected)
				}
				children, total := d.ChildPage(id, 0, 2)
				if total != len(expected.Children) || len(children) != min(2, total) {
					t.Fatal("page", id, total, len(children))
				}
				span, ok := d.SourceRange(id)
				want, _ := legacy.SourceRange(id)
				if !ok || span != want {
					t.Fatalf("span %s got %+v want %+v", id, span, want)
				}
			}
			for _, format := range []string{"native", "json", "yaml"} {
				got, gt, ge := d.RepresentationContext(context.Background(), d.Root.ID, format, 65536)
				want, wt, we := legacy.RepresentationContext(context.Background(), legacy.Root.ID, format, 65536)
				if (ge == nil) != (we == nil) || (ge == nil && (got != want || gt != wt)) {
					t.Fatalf("%s representation differs: %q %q %v %v", format, got, want, ge, we)
				}
			}
			found, err := d.Find(FindOptions{Mode: "search", Text: "old", Values: true})
			if err != nil || len(found.Matches) != 1 {
				t.Fatal(found, err)
			}
		})
	}
}

func TestCompactVariableLengthScalarPatchesAndHistory(t *testing.T) {
	for _, tc := range []struct{ format, source, path, replacement string }{
		{"json", `{"name":"old","other":"keep"}`, "$.name", `"much longer 😀"`},
		{"jsonl", "{\"name\":\"old\",\"other\":\"keep\"}\n", "$[0].name", `"much longer 😀"`},
		{"yaml", "name: old\nother: keep\n", "$.name", "much_longer"},
		{"csv", "name,other\r\nold,keep\r\n", "$[0].name", `"much,longer"`},
	} {
		t.Run(tc.format, func(t *testing.T) {
			d, err := ParseCompactWithLimitsContext(context.Background(), tc.format, []byte(tc.source), DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			result, err := d.Find(FindOptions{Mode: "query", Text: tc.path})
			if err != nil || len(result.Matches) != 1 {
				t.Fatal(result, err)
			}
			id := result.Matches[0].ID
			n, _ := d.LookupNode(id)
			a, _ := d.compact.Text().Position(n.Start)
			b, _ := d.compact.Text().Position(n.End)
			next, handled, err := d.TryScalarPatchContext(context.Background(), n.Start, n.End, tc.replacement)
			if err != nil || !handled {
				t.Fatal("local patch fallback", handled, err)
			}
			if next.compact == nil || next.raw != nil || len(next.Nodes) != 0 {
				t.Fatal("local edit retained legacy graph")
			}
			if next.NodeCount() != d.NodeCount() || next.Root.ID != d.Root.ID {
				t.Fatal("unstable count/root")
			}
			got, _ := next.LookupNode(id)
			if got.Value == n.Value {
				t.Fatal("scalar unchanged")
			}
			if next.StableChanges() != nil {
				t.Fatal("variable length edit promised stable source geometry")
			}
			s := NewSession()
			s.Doc = d
			s.Revision = 1
			s.Original = d.Source
			s.OriginalFormat = d.Format
			if err := s.Patch([]Patch{{Start: a.UTF16, End: b.UTF16, Text: tc.replacement}}, 1); err != nil {
				t.Fatal(err)
			}
			if err := s.History("undo", 2); err != nil {
				t.Fatal(err)
			}
			if s.Doc.compact == nil || string(s.Doc.Source) != tc.source {
				t.Fatal("undo lost compact state/source")
			}
			if err := s.History("redo", 3); err != nil {
				t.Fatal(err)
			}
			if s.Doc.compact == nil {
				t.Fatal("redo lost compact state")
			}
		})
	}
}
func TestCompactFallbackKeepsStableIDsAndDoesNotCacheMaterialization(t *testing.T) {
	d, err := ParseCompactWithLimitsContext(context.Background(), "yaml", []byte("name: &a old\ncopy: *a\n"), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	text, err := d.JSON(d.Root.ID, true)
	if err != nil || !strings.Contains(text, "old") {
		t.Fatal(text, err)
	}
	if d.raw != nil || len(d.Nodes) != 0 {
		t.Fatal("full representation cached AST")
	}
}

func TestCompactLocalEditsRemainIndependentAndFallbackRemapsIDs(t *testing.T) {
	d, err := ParseCompactWithLimitsContext(context.Background(), "json", []byte(`{"a":["one","two"],"b":"keep"}`), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	keep, _ := d.Find(FindOptions{Mode: "query", Text: "$.b"})
	keepID := keep.Matches[0].ID
	target, _ := d.Find(FindOptions{Mode: "query", Text: "$.a[0]"})
	targetID := target.Matches[0].ID
	for _, text := range []string{`"longer 😀"`, `"x"`, `"another, different string"`} {
		next, err := d.Edit(Action{Op: "replace", Node: targetID, Format: "json", Text: text})
		if err != nil {
			t.Fatal(err)
		}
		n, ok := next.LookupNode(keepID)
		if !ok || n.Value != "keep" || next.raw != nil || len(next.Nodes) != 0 {
			t.Fatal("unrelated IDs/compact retention changed")
		}
		d = next
	}
	// Replacing a container gives descendants new/reused IDs. Materialization
	// must match topology, not assume fresh parser preorder IDs still coincide.
	array, _ := d.Find(FindOptions{Mode: "query", Text: "$.a"})
	d, err = d.Edit(Action{Op: "replace", Node: array.Matches[0].ID, Format: "json", Text: `{"new":[1,2,3]}`})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := d.materialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for id, n := range legacy.Nodes {
		got, ok := d.LookupNode(id)
		if !ok || got.Path != n.Path || got.Value != n.Value {
			t.Fatalf("unstable remap %s", id)
		}
		gotSpan, ok := d.SourceRange(id)
		wantSpan, _ := legacy.SourceRange(id)
		if !ok || gotSpan != wantSpan {
			t.Fatalf("source remap %s: %+v %+v", id, gotSpan, wantSpan)
		}
	}
	schema, err := CompileSchema([]byte(`{"properties":{"b":{"const":"changed"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	validation, err := ValidateSchema(d, schema)
	if err != nil || validation.Valid || len(validation.Diagnostics) == 0 {
		t.Fatal(validation, err)
	}
	if _, ok := validation.Marks[keepID]; !ok {
		t.Fatal("schema lost stable IDs", validation)
	}
	if _, err := d.Edit(Action{Op: "clone", Node: keepID, Key: "copy"}); err != nil {
		t.Fatal("structural fallback", err)
	}
	if _, ok := d.LookupNode("01"); ok {
		t.Fatal("noncanonical ID accepted")
	}
}

func TestCompactYAMLMetadataQueriesAndSourceParity(t *testing.T) {
	for _, source := range []string{
		"# head\nroot: &a {x: 1}\ncopy: *a\n",
		"? [a, b]\n: value\nplain: other\n",
		"a: 1\n---\nb: 2\n",
		"n: 0xDEAD\nf: .5\ntruth: yes\n",
	} {
		d, err := ParseCompactWithLimitsContext(context.Background(), "yaml", []byte(source), DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		legacy, err := Parse("yaml", []byte(source))
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range []FindOptions{{Mode: "search", Text: "a", Keys: true, Values: true}, {Mode: "query", Text: "$..plain"}} {
			got, ge := d.Find(o)
			want, we := legacy.Find(o)
			if (ge == nil) != (we == nil) || ge == nil && len(got.Matches) != len(want.Matches) {
				t.Fatal("find differs", source, got, want, ge, we)
			}
		}
		for _, format := range []string{"json", "yaml", "native"} {
			got, _, ge := d.RepresentationContext(context.Background(), d.Root.ID, format, 1024)
			want, _, we := legacy.RepresentationContext(context.Background(), legacy.Root.ID, format, 1024)
			if (ge == nil) != (we == nil) || ge == nil && got != want {
				t.Fatal("representation differs", format, got, want, ge, we)
			}
		}
		if d.raw != nil || len(d.Nodes) != 0 {
			t.Fatal("materialization cached")
		}
		total, _ := d.compact.Text().Position(len(source))
		span, ok := d.Locate(total.UTF16, total.UTF16)
		if !ok || span.ID != d.Root.ID {
			t.Fatal("EOF lookup lost root", span, ok)
		}
	}
}

func TestCompactCSVEmptyAndQuotedLocalCells(t *testing.T) {
	d, err := ParseCompactWithLimitsContext(context.Background(), "csv", []byte("a,b\r\n,keep\r\n"), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	found, err := d.Find(FindOptions{Mode: "query", Text: "$[0].a"})
	if err != nil || len(found.Matches) != 1 {
		t.Fatal(found, err)
	}
	id := found.Matches[0].ID
	for _, text := range []string{`"new,quoted"`, `"line1\nline2"`, `""`, `"last"`} {
		next, err := d.Edit(Action{Op: "replace", Node: id, Format: "json", Text: text})
		if err != nil {
			t.Fatal(err)
		}
		if next.compact == nil || !strings.Contains(string(next.Source), ",keep\r\n") {
			t.Fatal("unrelated CSV bytes changed")
		}
		d = next
	}
}

func TestCompactInvalidFragmentsLimitsAndCancellationAreAtomic(t *testing.T) {
	limits := Limits{MaxBytes: 1024, MaxNodes: 6}
	d, err := ParseCompactWithLimitsContext(context.Background(), "json", []byte(`{"a":1,"b":2}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	result, _ := d.Find(FindOptions{Mode: "query", Text: "$.a"})
	id := result.Matches[0].ID
	for _, text := range []string{`{"x":1,"x":2}`, `{"nested":[0,1]}`, `not_json`} {
		if _, err := d.Edit(Action{Op: "replace", Node: id, Format: "json", Text: text}); err == nil {
			t.Fatal("invalid fragment accepted", text)
		}
		if string(d.Source) != `{"a":1,"b":2}` {
			t.Fatal("failed edit changed source")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.EditContext(ctx, Action{Op: "replace", Node: id, Format: "json", Text: "3"}); err == nil {
		t.Fatal("cancelled edit accepted")
	}
	if _, err := d.FindContext(ctx, FindOptions{Mode: "search", Text: "a", Keys: true}); err == nil {
		t.Fatal("cancelled find accepted")
	}
	if _, _, err := d.RepresentationContext(ctx, d.Root.ID, "native", 100); err == nil {
		t.Fatal("cancelled representation accepted")
	}
}

func TestCompactPreservesLegacyComplexKeyDepthBoundary(t *testing.T) {
	source := "? " + strings.Repeat("[", MaxDepth) + "x" + strings.Repeat("]", MaxDepth) + "\n: value\n"
	if _, err := parseLegacyWithLimitsContext(context.Background(), "yaml", []byte(source), DefaultLimits()); err != nil {
		t.Fatal("legacy fixture", err)
	}
	if _, err := ParseCompactWithLimitsContext(context.Background(), "yaml", []byte(source), DefaultLimits()); err != nil {
		t.Fatal("compact changed accepted depth", err)
	}
}
func TestCompactLocalReplacementEnforcesValueDepth(t *testing.T) {
	source := strings.Repeat("[", MaxDepth-1) + "0" + strings.Repeat("]", MaxDepth-1)
	d, err := ParseCompactWithLimitsContext(context.Background(), "json", []byte(source), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	found, err := d.Find(FindOptions{Mode: "query", Text: "$" + strings.Repeat("[0]", MaxDepth-1)})
	if err != nil || len(found.Matches) != 1 {
		t.Fatal(found, err)
	}
	if _, err := d.Edit(Action{Op: "replace", Node: found.Matches[0].ID, Format: "json", Text: "[[0]]"}); err == nil {
		t.Fatal("accepted value depth exceeding legacy bound")
	}
}

func TestCompactCSVSingleEmptyCellActionAndSourcePatchParity(t *testing.T) {
	parse := func() *Document {
		t.Helper()
		d, err := ParseCompactWithLimitsContext(context.Background(), "csv", []byte("h\nx\n"), DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	d := parse()
	found, _ := d.Find(FindOptions{Mode: "query", Text: "$[0].h"})
	id := found.Matches[0].ID
	next, err := d.Edit(Action{Op: "replace", Node: id, Format: "json", Text: `""`})
	if err != nil {
		t.Fatal(err)
	}
	if string(next.Source) != "h\n\"\"\n" {
		t.Fatal("empty sole cell became skipped blank row", string(next.Source))
	}
	if _, err := next.materialize(context.Background()); err != nil {
		t.Fatal("action source/tree mismatch", err)
	}
	d = parse()
	n, _ := d.LookupNode(id)
	a, _ := d.compact.Text().Position(n.Start)
	b, _ := d.compact.Text().Position(n.End)
	for _, replacement := range []string{"", `""`} {
		s := NewSession()
		s.Doc = d
		s.Revision = 1
		if err := s.Patch([]Patch{{Start: a.UTF16, End: b.UTF16, Text: replacement}}, 1); err != nil {
			t.Fatal(err)
		}
		_, total := s.Doc.ChildPage(s.Doc.Root.ID, 0, 0)
		want := 0
		if replacement != "" {
			want = 1
		}
		if total != want {
			t.Fatal("source patch retained skipped row", replacement, total, want)
		}
		if _, err := s.Doc.materialize(context.Background()); err != nil {
			t.Fatal("patch source/tree mismatch", replacement, err)
		}
	}
}
func TestCompactSourcePatchMayExpandCSVRecordContext(t *testing.T) {
	d, err := ParseCompactWithLimitsContext(context.Background(), "csv", []byte("h\nx\n"), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	s := NewSession()
	s.Doc = d
	s.Revision = 1
	if err := s.Patch([]Patch{{Start: 2, End: 3, Text: "first\nsecond"}}, 1); err != nil {
		t.Fatal("valid record expansion rejected", err)
	}
	_, rows := s.Doc.ChildPage(s.Doc.Root.ID, 0, 0)
	if rows != 2 {
		t.Fatal("CSV record context was not expanded", rows)
	}
	if _, err := s.Doc.materialize(context.Background()); err != nil {
		t.Fatal("source/tree mismatch", err)
	}
}
func TestCompactSinglePathQueryAndCursorParity(t *testing.T) {
	source := []byte(`{"a":["first","second"],"b":"keep"}`)
	d, err := ParseCompactWithLimitsContext(context.Background(), "json", source, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := Parse("json", source)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"$.a[0]", "$.b", "$.missing"} {
		for _, cursor := range []string{"", "0", "2", "5"} {
			options := FindOptions{Mode: "query", Text: path, Cursor: cursor, Limit: 1}
			got, ge := d.Find(options)
			want, we := legacy.Find(options)
			if (ge == nil) != (we == nil) || len(got.Matches) != len(want.Matches) || got.Truncated != want.Truncated || got.NextCursor != want.NextCursor {
				t.Fatal(path, cursor, got, want, ge, we)
			}
			if len(got.Matches) > 0 && got.Matches[0].ID != want.Matches[0].ID {
				t.Fatal("query changed match identity")
			}
		}
	}
}
func TestCompactRootValueSpansPreserveJSONWhitespace(t *testing.T) {
	source := []byte("\n  {\"a\":1}\n ")
	d, err := ParseCompactWithLimitsContext(context.Background(), "json", source, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := Parse("json", source)
	if err != nil {
		t.Fatal(err)
	}
	if d.Root.Start != legacy.Root.Start || d.Root.End != legacy.Root.End || d.Root.Location != legacy.Root.Location {
		t.Fatal("root value location changed", d.Root, legacy.Root)
	}
	next, err := d.Edit(Action{Op: "replace", Node: d.Root.ID, Format: "json", Text: `{"b":2}`})
	if err != nil {
		t.Fatal(err)
	}
	if string(next.Source) != "\n  {\"b\":2}\n " {
		t.Fatal("root edit normalized unrelated whitespace", string(next.Source))
	}
}
func TestCompactJSONKeySourcePatchUsesContainingContext(t *testing.T) {
	source := `{"left":{"old":1,"keep":2},"right":{"x":3}}`
	for _, format := range []string{"json", "jsonl"} {
		t.Run(format, func(t *testing.T) {
			input := source
			pathPrefix := "$"
			if format == "jsonl" {
				input += "\n"
				pathPrefix = "$[0]"
			}
			d, err := ParseCompactWithLimitsContext(context.Background(), format, []byte(input), DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			right, _ := d.Find(FindOptions{Mode: "query", Text: pathPrefix + ".right"})
			rightID := right.Matches[0].ID
			start := strings.Index(input, "old")
			next, handled, err := d.TryScalarPatchContext(context.Background(), start, start+3, "new_long")
			if err != nil || !handled {
				t.Fatal("key edit required full parse", handled, err)
			}
			if string(next.Source) != strings.Replace(input, "old", "new_long", 1) {
				t.Fatal("unrelated source changed", string(next.Source))
			}
			found, _ := next.Find(FindOptions{Mode: "query", Text: pathPrefix + ".right"})
			if len(found.Matches) != 1 || found.Matches[0].ID != rightID {
				t.Fatal("unrelated IDs changed", found)
			}
			if next.raw != nil || len(next.Nodes) != 0 {
				t.Fatal("local key edit retained full graph")
			}
		})
	}
}
func TestCompactJSONStructuralActionsUseLocalContext(t *testing.T) {
	source := `{"left":{"old":1,"keep":2},"right":{"x":3}}`
	for _, format := range []string{"json", "jsonl"} {
		for _, op := range []string{"add", "rename", "clone", "delete"} {
			t.Run(format+"/"+op, func(t *testing.T) {
				input := source
				prefix := "$"
				if format == "jsonl" {
					input += "\n"
					prefix = "$[0]"
				}
				d, err := ParseCompactWithLimitsContext(context.Background(), format, []byte(input), DefaultLimits())
				if err != nil {
					t.Fatal(err)
				}
				right, _ := d.Find(FindOptions{Mode: "query", Text: prefix + ".right"})
				rightID := right.Matches[0].ID
				path := prefix + ".left.old"
				if op == "add" {
					path = prefix + ".left"
				}
				target, _ := d.Find(FindOptions{Mode: "query", Text: path})
				action := Action{Op: op, Node: target.Matches[0].ID, Key: "new", Format: "json", Text: "123456789012345678901234567890"}
				next, handled, err := d.compactEdit(context.Background(), action)
				if err != nil || !handled {
					t.Fatal("structural action required global fallback", handled, err)
				}
				legacy, _ := d.materialize(context.Background())
				expected, err := legacy.Edit(action)
				if err != nil {
					t.Fatal(err)
				}
				got, err := next.JSON(next.Root.ID, true)
				if err != nil {
					t.Fatal(err)
				}
				want, err := expected.JSON(expected.Root.ID, true)
				if err != nil || got != want {
					t.Fatal("structural semantics changed", got, want, err)
				}
				found, _ := next.Find(FindOptions{Mode: "query", Text: prefix + ".right"})
				if len(found.Matches) != 1 || found.Matches[0].ID != rightID {
					t.Fatal("unrelated IDs changed", found)
				}
				if !strings.Contains(string(next.Source), `,"right":{"x":3}}`) {
					t.Fatal("unrelated formatting changed", string(next.Source))
				}
			})
		}
	}
}
