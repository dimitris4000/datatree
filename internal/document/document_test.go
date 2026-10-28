package document

import (
	"bytes"
	"fmt"
	"reflect"

	"go.yaml.in/yaml/v3"
	"strings"
	"testing"
)

func mustParse(t *testing.T, format, source string) *Document {
	t.Helper()
	d, err := Parse(format, []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestFormatsAndOrder(t *testing.T) {
	for _, tc := range []struct {
		format, source string
		count          int
	}{
		{"json", `{"z":9007199254740993,"a":{"q":true}}`, 2},
		{"jsonl", "{\"x\":1}\n\n{\"x\":2}\n", 2},
		{"yaml", "# hi\nz: &thing {x: 1}\na: *thing\n", 2},
		{"csv", "name,age\r\nAlice,23\r\nBob,29\r\n", 2},
	} {
		t.Run(tc.format, func(t *testing.T) {
			d := mustParse(t, tc.format, tc.source)
			if len(d.Root.Children) != tc.count {
				t.Fatalf("children=%d", len(d.Root.Children))
			}
			if !bytes.Equal(d.Source, []byte(tc.source)) {
				t.Fatal("source changed")
			}
		})
	}
	d := mustParse(t, "json", `{"z":9007199254740993,"a":1}`)
	if d.Root.Children[0].Label != "z" {
		t.Fatal("order lost")
	}
	out, err := d.JSON(d.Root.ID, false)
	if err != nil || !strings.Contains(out, "9007199254740993") {
		t.Fatalf("precision: %s %v", out, err)
	}
}
func TestInvalidFormatsAndLimits(t *testing.T) {
	for _, tc := range []struct{ f, s string }{{"json", `{"x":1} garbage`}, {"json", `{"a":1,"a":2}`}, {"jsonl", "{}\nnope\n"}, {"csv", "x,x\n1,2"}, {"csv", ",y\n1,2"}, {"csv", "x,y\n1"}, {"yaml", "x: *missing"}, {"yaml", "x: ["}} {
		if _, err := Parse(tc.f, []byte(tc.s)); err == nil {
			t.Fatalf("accepted %s %s", tc.f, tc.s)
		}
	}
	if _, err := Parse("json", bytes.Repeat([]byte(" "), MaxBytes+1)); err == nil {
		t.Fatal("size limit")
	}
	if _, err := Parse("json", []byte(strings.Repeat("[", 257)+"0"+strings.Repeat("]", 257))); err == nil {
		t.Fatal("depth limit")
	}
}
func TestYAMLStreamsReferencesAndPaths(t *testing.T) {
	d := mustParse(t, "yaml", "---\na: &x [1, 2]\nb: *x\n---\n'odd.key': yes\n")
	if d.Root.Kind != "stream" || len(d.Root.Children) != 2 {
		t.Fatal("stream missing")
	}
	alias := d.Root.Children[0].Children[1]
	if alias.Kind != "alias" || alias.Target == "" || len(alias.Children) != 0 {
		t.Fatal("alias expanded")
	}
	j := mustParse(t, "json", `{"odd.key":{"x'y":[null]}}`)
	if p := j.Root.Children[0].Children[0].Children[0].Path; p != `$['odd.key']['x\'y'][0]` {
		t.Fatal(p)
	}
}
func TestYAMLPatchesPreserveUntouchedBytes(t *testing.T) {
	for _, source := range []string{"# intro\r\nname: 'Αθήνα' # stay\r\nother:  2\r\n", "# intro\nname: 'Αθήνα' # stay\nother:  2\n"} {
		d := mustParse(t, "yaml", source)
		n := d.Root.Children[0]
		next, err := d.Edit(Action{Op: "replace", Node: n.ID, Text: "'Paris'", Format: "yaml"})
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Replace(source, "'Αθήνα'", "'Paris'", 1)
		if string(next.Source) != want {
			t.Fatalf("\ngot %q\nwant%q", next.Source, want)
		}
	}
	d := mustParse(t, "yaml", "first:\n  x: 1\n  y: 2\nlast:   true # untouched\n")
	next, err := d.Edit(Action{Op: "add", Node: d.Root.Children[0].ID, Key: "z", Text: "3", Format: "yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(next.Source), "last:   true # untouched\n") {
		t.Fatalf("outside patch changed: %q", next.Source)
	}
}
func TestEditingCloningAndCSVShape(t *testing.T) {
	d := mustParse(t, "json", `{"items":[{"n":1}],"flag":true}`)
	item := d.Root.Children[0].Children[0]
	next, err := d.Edit(Action{Op: "clone", Node: item.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Root.Children[0].Children) != 2 {
		t.Fatal("clone missing")
	}
	if _, err := d.Edit(Action{Op: "clone", Node: d.Root.Children[1].ID, Key: "items"}); err == nil {
		t.Fatal("duplicate key accepted")
	}
	if _, err := d.Edit(Action{Op: "replace", Node: item.ID, Text: "{", Format: "json"}); err == nil {
		t.Fatal("bad edit")
	}
	csv := mustParse(t, "csv", "x,y\n1,2\n")
	if _, err := csv.Edit(Action{Op: "replace", Node: csv.Root.Children[0].Children[0].ID, Text: "3", Format: "json"}); err == nil {
		t.Fatal("CSV numeric cell accepted")
	}
	next, err = csv.Edit(Action{Op: "clone", Node: csv.Root.Children[0].ID})
	if err != nil || len(next.Root.Children) != 2 {
		t.Fatalf("CSV clone: %v", err)
	}
}
func TestAnchorSafetyAndExplicitConversion(t *testing.T) {
	d := mustParse(t, "yaml", "items:\n  - &a {x: 1}\n  - *a\n")
	if _, err := d.Edit(Action{Op: "delete", Node: d.Root.Children[0].Children[0].ID}); err == nil {
		t.Fatal("dangling alias accepted")
	}
	next, err := d.Edit(Action{Op: "clone", Node: d.Root.Children[0].Children[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(next.Source), "&a_copy") {
		t.Fatalf("anchor not renamed: %s", next.Source)
	}
	if _, err := d.JSON(d.Root.ID, false); err == nil {
		t.Fatal("lossy conversion wasn't flagged")
	}
	if _, err := d.JSON(d.Root.ID, true); err != nil {
		t.Fatal(err)
	}
	cyclic := mustParse(t, "yaml", "a: &a [*a]\n")
	if _, err := cyclic.JSON(cyclic.Root.ID, true); err == nil {
		t.Fatal("cycle converted")
	}
}
func TestHistoryAndRevision(t *testing.T) {
	s := NewSession()
	if err := s.Import("json", []byte(`{"x":1}`), 0); err != nil {
		t.Fatal(err)
	}
	rev := s.Revision
	id := s.Doc.Root.Children[0].ID
	if err := s.Mutate(Action{Op: "replace", Node: id, Text: "2", Format: "json"}, rev-1); err == nil {
		t.Fatal("stale accepted")
	}
	if err := s.Mutate(Action{Op: "replace", Node: id, Text: "2", Format: "json"}, rev); err != nil {
		t.Fatal(err)
	}
	if err := s.History("undo", s.Revision); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(s.Doc.Source), "1") {
		t.Fatal("undo")
	}
	if err := s.History("redo", s.Revision); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(s.Doc.Source), "2") {
		t.Fatal("redo")
	}
	if string(s.Original) != `{"x":1}` {
		t.Fatal("original mutated")
	}
}
func TestLargeDocument(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"rows":[`)
	for i := 0; i < 15000; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"n":%d,"text":"%s"}`, i, strings.Repeat("a", 620))
	}
	b.WriteString("]}")
	d := mustParse(t, "json", b.String())
	children, total := d.ChildPage(d.Root.ID, 0, 1)
	if total != 1 || len(children) != 1 {
		t.Fatal("rows collection lost")
	}
	_, total = d.ChildPage(children[0].ID, 0, 1)
	if total != 15000 {
		t.Fatal("rows lost")
	}
}

func TestYAMLScalarSpanVariants(t *testing.T) {
	for _, tc := range []struct{ source, old, replacement string }{
		{"a: hello, world\nz:  1\n", "hello, world", "'next'"},
		{"a: first\n  second\nz:  1\n", "first\n  second", "'next'"},
		{"a:\n  first\n  second\nz:  1\n", "first\n  second", "'next'"},
		{"a: |\n  first\n  second\nz:  1\n", "|\n  first\n  second\n", "'next'\n"},
		{"a: {b: 'x', c: 2}\nz:  1\n", "{b: 'x', c: 2}", "{b: 'next'}"},
	} {
		t.Run(tc.old, func(t *testing.T) {
			d := mustParse(t, "yaml", tc.source)
			next, err := d.Edit(Action{Op: "replace", Node: d.Root.Children[0].ID, Text: strings.TrimRight(tc.replacement, "\n"), Format: "yaml"})
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Replace(tc.source, tc.old, tc.replacement, 1)
			if string(next.Source) != want {
				t.Fatalf("got %q want %q", next.Source, want)
			}
		})
	}
	d := mustParse(t, "yaml", "a:\nb: 1\n")
	next, err := d.Edit(Action{Op: "replace", Node: d.Root.Children[0].ID, Text: "2", Format: "yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Root.Children) != 2 || next.Root.Children[0].Value != "2" {
		t.Fatalf("wrong empty value edit: %q", next.Source)
	}
}
func TestYAMLCollectionReplacementAndComments(t *testing.T) {
	d := mustParse(t, "yaml", "# top\na: 1 # trailing\nb:    2\n")
	next, err := d.Edit(Action{Op: "replace", Node: d.Root.Children[0].ID, Text: "x: 3\ny: 4", Format: "yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(next.Source), "b:    2\n") || len(next.Root.Children[0].Children) != 2 {
		t.Fatalf("%s", next.Source)
	}
	d = mustParse(t, "yaml", "top:\n  # first comment\n  a: 1 # inline\n  b: 2 # last comment\nlast:   true\n")
	next, err = d.Edit(Action{Op: "add", Node: d.Root.Children[0].ID, Key: "c", Text: "3", Format: "yaml"})
	if err != nil {
		t.Fatal(err)
	}
	for _, comment := range []string{"# first comment", "# inline", "# last comment"} {
		if strings.Count(string(next.Source), comment) != 1 {
			t.Fatalf("comment duplicated/lost: %s", next.Source)
		}
	}
}

func TestYAMLSubtreeEditorRoundTrip(t *testing.T) {
	d := mustParse(t, "yaml", "outer:\n  child:\n    first: 1\n    second: 2\nlast:   true\n")
	n := d.Root.Children[0].Children[0]
	text, err := d.YAML(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse("yaml", []byte(text)); err != nil {
		t.Fatalf("inspector emits invalid YAML: %q: %v", text, err)
	}
	next, err := d.Edit(Action{Op: "replace", Node: n.ID, Text: text, Format: "yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(d.Source, next.Source) {
		t.Fatalf("unchanged subtree was reformatted: %q", next.Source)
	}
}
func TestStructuredYAMLEditRetainsMetadata(t *testing.T) {
	d := mustParse(t, "yaml", "name: &n 'old' # note\nref: *n\nlast:   2\n")
	next, err := d.Edit(Action{Op: "replace", Node: d.Root.Children[0].ID, Text: `"new"`, Format: "json", Structured: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(next.Source) != "name: &n 'new' # note\nref: *n\nlast:   2\n" {
		t.Fatalf("metadata lost: %q", next.Source)
	}
}
func TestJSONMergeRequiresConfirmation(t *testing.T) {
	d := mustParse(t, "yaml", "a:\n  <<: {x: 1}\n  y: 2\n")
	if _, err := d.JSON(d.Root.ID, false); err == nil {
		t.Fatal("merge was silently converted")
	}
	if j, err := d.JSON(d.Root.ID, true); err != nil || !strings.Contains(j, `"x": 1`) {
		t.Fatalf("%s %v", j, err)
	}
}
func TestYAMLBlockScalarDoesNotConsumeSibling(t *testing.T) {
	d := mustParse(t, "yaml", "- a: |\n    text\n  b: 2\n- c: 3\n")
	n := d.Root.Children[0].Children[0]
	next, err := d.Edit(Action{Op: "replace", Node: n.ID, Text: "'next'", Format: "yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Root.Children[0].Children) != 2 || !strings.Contains(string(next.Source), "  b: 2\n") {
		t.Fatalf("sibling removed: %q", next.Source)
	}
}
func TestYAMLExplicitIndentAndInvalidFragment(t *testing.T) {
	for _, source := range []string{"a: |2\n  first\n  second\nz: 3\n", "top:\n  a: |2\n    first\n    second\nz: 3\n"} {
		d := mustParse(t, "yaml", source)
		n := d.Root.Children[0]
		if n.Kind == "object" {
			n = n.Children[0]
		}
		text, err := d.YAML(n.ID)
		if err != nil {
			t.Fatal(err)
		}
		next, err := d.Edit(Action{Op: "replace", Node: n.ID, Text: text, Format: "yaml"})
		if err != nil {
			t.Fatal(err)
		}
		if string(next.Source) != source {
			t.Fatalf("scalar changed: %q", next.Source)
		}
		modified := strings.Replace(text, "first", "changed", 1)
		next, err = d.Edit(Action{Op: "replace", Node: n.ID, Text: modified, Format: "yaml"})
		if err != nil {
			t.Fatal(err)
		}
		nn := next.Root.Children[0]
		if nn.Kind == "object" {
			nn = nn.Children[0]
		}
		if nn.Value != "changed\nsecond\n" {
			t.Fatalf("indent changed content: %q", nn.Value)
		}
	}
	d := mustParse(t, "yaml", "a: 1\nb: 2\n")
	if _, err := d.Edit(Action{Op: "replace", Node: d.Root.Children[0].ID, Text: "3\ninjected: 4", Format: "yaml"}); err == nil {
		t.Fatal("invalid fragment injected a sibling")
	}
}
func TestPlainYAMLScalarDoesNotConsumeSibling(t *testing.T) {
	d := mustParse(t, "yaml", "- a: first\n  b: 2\n- c: 3\n")
	next, err := d.Edit(Action{Op: "replace", Node: d.Root.Children[0].Children[0].ID, Text: "'next'", Format: "yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Root.Children[0].Children) != 2 {
		t.Fatalf("sibling removed: %s", next.Source)
	}
}
func TestRepeatedMergeAliasesAreMemoized(t *testing.T) {
	var b strings.Builder
	b.WriteString("base: &a0 {x: 1}\n")
	for i := 1; i <= 24; i++ {
		fmt.Fprintf(&b, "n%d: &a%d {<<: [*a%d, *a%d]}\n", i, i, i-1, i-1)
	}
	b.WriteString("<<: *a24\n")
	d := mustParse(t, "yaml", b.String())
	j, err := d.JSON(d.Root.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(j, `"x": 1`) {
		t.Fatal("merge result missing")
	}
}
func TestYAMLPatchMetadataBoundaries(t *testing.T) {
	for _, source := range []string{
		"top:\n  a: 1\n  b: 2\n  # foot\n\nlast: 3\n",
		"top: &x\n  a: 1\n  b: 2\nref: *x\nlast: 3\n",
		"# head\ntop: [1, 2] # comment\nlast: 3\n",
	} {
		d := mustParse(t, "yaml", source)
		target := d.Root.Children[0]
		key := "c"
		if target.Kind == "array" {
			key = ""
		}
		next, err := d.Edit(Action{Op: "add", Node: target.ID, Key: key, Text: "4", Format: "yaml"})
		if err != nil {
			t.Fatal(err)
		}
		for _, comment := range []string{"# foot", "# head", "# comment"} {
			if strings.Count(string(next.Source), comment) != strings.Count(source, comment) {
				t.Fatalf("metadata changed: %q", next.Source)
			}
		}
	}
}

func TestYAMLPlainMultilineWithBlankLines(t *testing.T) {
	for _, source := range []string{"a: first\n\n  second\nb: 2\n", "first\nsecond\n"} {
		d := mustParse(t, "yaml", source)
		n := d.Root
		if len(n.Children) > 0 {
			n = n.Children[0]
		}
		next, err := d.Edit(Action{Op: "replace", Node: n.ID, Text: "'next'", Format: "yaml"})
		if err != nil {
			t.Fatal(err)
		}
		nn := next.Root
		if len(nn.Children) > 0 {
			nn = nn.Children[0]
		}
		if nn.Value != "next" {
			t.Fatalf("%q", nn.Value)
		}
	}
}
func TestYAMLChompingPreservesEditedValue(t *testing.T) {
	d := mustParse(t, "yaml", "a: |+\n  first\n\nb: 2\n")
	n := d.Root.Children[0]
	text, _ := d.YAML(n.ID)
	next, err := d.Edit(Action{Op: "replace", Node: n.ID, Text: strings.Replace(text, "first", "next", 1), Format: "yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if next.Root.Children[0].Value != "next\n\n" {
		t.Fatalf("chomp changed value: %q", next.Root.Children[0].Value)
	}
	d = mustParse(t, "yaml", "a: old\nb: 2\n")
	next, err = d.Edit(Action{Op: "replace", Node: d.Root.Children[0].ID, Text: `"first\n\n"`, Format: "json", Structured: true})
	if err != nil {
		t.Fatal(err)
	}
	if next.Root.Children[0].Value != "first\n\n" {
		t.Fatalf("structured chomp: %q", next.Root.Children[0].Value)
	}
}
func TestPathsEscapeControlCharacters(t *testing.T) {
	d := mustParse(t, "json", `{"line\nbreak\tkey":1}`)
	if path := d.Root.Children[0].Path; path != `$['line\nbreak\tkey']` {
		t.Fatalf("invalid JSONPath %q", path)
	}
}

func TestJSONToYAMLBlockFormatting(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"nested", `{"z":{"name":"Alice","items":[{"active":true},[1,2]]},"a":null}`, "z:\n  name: Alice\n  items:\n    - active: true\n    - - 1\n      - 2\na: null\n"},
		{"empty", `{"object":{},"array":[]}`, "object: {}\narray: []\n"},
		{"multiline", `{"text":"first\nsecond\n"}`, "text: |\n  first\n  second\n"},
		{"strings", `{"true":"true","null":"null","number":"123","date":"2026-10-26","ordinary":"hello"}`, "\"true\": \"true\"\n\"null\": \"null\"\nnumber: \"123\"\ndate: \"2026-10-26\"\nordinary: hello\n"},
		{"numbers", `{"large":90071992547409931234567890,"decimal":1.234567890123456789,"exponent":1e100}`, "large: !!int 90071992547409931234567890\ndecimal: 1.234567890123456789\nexponent: 1e100\n"},
		{"root array", `[{"name":"Alice"},"true"]`, "- name: Alice\n- \"true\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewSession()
			if err := s.Import("json", []byte(tc.source), 0); err != nil {
				t.Fatal(err)
			}
			d := s.Doc
			before := clone(d.raw, map[*yaml.Node]*yaml.Node{})
			preview, err := s.Preview("yaml")
			if err != nil {
				t.Fatal(err)
			}
			if preview != tc.want {
				t.Fatalf("preview:\n%s\nwant:\n%s", preview, tc.want)
			}
			inspected, err := d.YAML(d.Root.ID)
			if err != nil || inspected != preview {
				t.Fatalf("inspector = %q, %v", inspected, err)
			}
			if !reflect.DeepEqual(before, d.raw) || string(d.Source) != tc.source || s.CanUndo() || s.Revision != 1 {
				t.Fatal("serialization mutated document or history")
			}
			if err := s.Convert("yaml", 1); err != nil {
				t.Fatal(err)
			}
			if string(s.Doc.Source) != preview {
				t.Fatal("conversion differs from preview")
			}
			originalJSON, _ := d.JSON(d.Root.ID, false)
			roundTrip, err := s.Doc.JSON(s.Doc.Root.ID, true)
			if err != nil || roundTrip != originalJSON {
				t.Fatalf("semantics changed: %s, %v", roundTrip, err)
			}
			if err := s.History("undo", 2); err != nil {
				t.Fatal(err)
			}
			if s.Doc.Format != "json" || string(s.Doc.Source) != tc.source || string(s.Original) != tc.source {
				t.Fatal("original JSON lost")
			}
		})
	}
}

func TestJSONSubtreeYAMLFormatting(t *testing.T) {
	d := mustParse(t, "json", `{"outer":{"items":["hello","true"]}}`)
	got, err := d.YAML(d.Root.Children[0].ID)
	if err != nil || got != "items:\n  - hello\n  - \"true\"\n" {
		t.Fatalf("%q, %v", got, err)
	}
}

func TestYAMLFlowStylePreserved(t *testing.T) {
	d := mustParse(t, "yaml", "outer: {name: 'Alice', items: [1, 2]}\nlast:   true\n")
	next, err := d.Edit(Action{Op: "add", Node: d.Root.Children[0].ID, Key: "extra", Text: "3", Format: "yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if string(next.Source) != "outer: {name: 'Alice', items: [1, 2], extra: 3}\nlast:   true\n" {
		t.Fatalf("styles changed: %q", next.Source)
	}
}
